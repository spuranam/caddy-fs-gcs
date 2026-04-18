package fs

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/storage"
)

// Constants for attrCache defaults.
const (
	// clockTickInterval is the resolution of the shared monotonic clock.
	clockTickInterval = 100 * time.Millisecond

	// negTTLDivisor controls how much faster negative entries expire
	// relative to positive entries (negTTL = ttl / negTTLDivisor).
	negTTLDivisor = 10

	// minNegTTL is the floor for negative-entry TTL to avoid excessive
	// re-checks for genuinely missing objects.
	minNegTTL = time.Second
)

// sharedClock is a process-wide monotonic nanosecond clock updated every
// 100ms. All attrCache instances share this single goroutine to avoid
// spawning one ticker per cache (per bucket / config reload).
var sharedClock struct {
	mu   sync.Mutex // protects done, once
	val  atomic.Int64
	once sync.Once
	done chan struct{}
}

func initSharedClock() {
	sharedClock.mu.Lock()
	sharedClock.done = make(chan struct{})
	done := sharedClock.done
	sharedClock.mu.Unlock()

	sharedClock.val.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(clockTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sharedClock.val.Store(time.Now().UnixNano())
			case <-done:
				return
			}
		}
	}()
}

// attrCache is a concurrency-safe, TTL-bounded in-memory cache for GCS
// object attributes. It avoids redundant Stat() round-trips to GCS when
// Caddy's file_server calls Stat() + Open() on the same path per request.
type attrCache struct {
	mu         sync.RWMutex
	entries    map[string]attrEntry
	keys       []string       // tracks keys for sample-based eviction
	keyIndex   map[string]int // O(1) key→position lookup for removeKey
	ttl        time.Duration
	negTTL     time.Duration // TTL for negative (not-found) entries
	maxEntries int
	hits       atomic.Int64
	misses     atomic.Int64
}

// sentinelNotFound is a marker for negative cache entries (object does not exist).
var sentinelNotFound = &storage.ObjectAttrs{Name: "\x00notfound"}

type attrEntry struct {
	attrs     *storage.ObjectAttrs
	expiresAt int64 // monotonic nanoseconds (from clock)
}

func newAttrCache(ttl time.Duration, maxEntries int) *attrCache {
	// Ensure the shared clock goroutine is running (once per process).
	sharedClock.once.Do(initSharedClock)

	c := &attrCache{
		entries:    make(map[string]attrEntry, maxEntries),
		keys:       make([]string, 0, maxEntries),
		keyIndex:   make(map[string]int, maxEntries),
		ttl:        ttl,
		negTTL:     ttl / negTTLDivisor, // negative entries expire 10x faster
		maxEntries: maxEntries,
	}
	if c.negTTL < minNegTTL {
		c.negTTL = minNegTTL
	}
	return c
}

// stop is a no-op for individual caches since the clock is shared.
// Call stopSharedClock() to shut down the background goroutine (tests only).
func (c *attrCache) stop() {
	// Shared clock is process-scoped; individual caches don't stop it.
}

// stopSharedClock stops the shared clock goroutine and resets the sync.Once
// so it can be restarted. Intended for test cleanup to avoid goroutine leaks.
func stopSharedClock() {
	sharedClock.mu.Lock()
	defer sharedClock.mu.Unlock()

	if sharedClock.done != nil {
		select {
		case <-sharedClock.done:
			// Already stopped.
		default:
			close(sharedClock.done)
		}
		sharedClock.done = nil
	}
	// Reset Once so the next newAttrCache call restarts the clock.
	sharedClock.once = sync.Once{}
}

// now returns the cached monotonic nanosecond timestamp from the shared clock.
func (c *attrCache) now() int64 {
	return sharedClock.val.Load()
}

// get returns cached attributes if present and not expired.
// Returns (nil, false) on miss. Returns (sentinelNotFound, true) for
// negative entries — callers must check with isNegative().
func (c *attrCache) get(key string) (*storage.ObjectAttrs, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	if c.now() > entry.expiresAt {
		c.mu.Lock()
		// Re-check under write lock — another goroutine may have refreshed.
		if e, exists := c.entries[key]; exists && c.now() > e.expiresAt {
			delete(c.entries, key)
			c.removeKey(key)
		}
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	return entry.attrs, true
}

// isNegative returns true if attrs is a negative (not-found) sentinel.
func isNegative(attrs *storage.ObjectAttrs) bool {
	return attrs == sentinelNotFound
}

// setNotFound stores a negative cache entry for a missing object.
func (c *attrCache) setNotFound(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeeded()

	if _, exists := c.entries[key]; !exists {
		c.keyIndex[key] = len(c.keys)
		c.keys = append(c.keys, key)
	}
	c.entries[key] = attrEntry{
		attrs:     sentinelNotFound,
		expiresAt: c.now() + c.negTTL.Nanoseconds(),
	}
}

// set stores attributes in the cache.
func (c *attrCache) set(key string, attrs *storage.ObjectAttrs) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeeded()

	if _, exists := c.entries[key]; !exists {
		c.keyIndex[key] = len(c.keys)
		c.keys = append(c.keys, key)
	}
	c.entries[key] = attrEntry{
		attrs:     attrs,
		expiresAt: c.now() + c.ttl.Nanoseconds(),
	}
}

// evictSampleSize is the number of random entries sampled during eviction.
const evictSampleSize = 20

// evictIfNeeded performs sample-based eviction when the cache is at capacity.
// It randomly samples up to evictSampleSize entries and removes expired ones.
// If no expired entries are found, it evicts the entry closest to expiry among
// the sampled set to ensure new entries can always be inserted.
// Must be called with c.mu held for writing.
func (c *attrCache) evictIfNeeded() {
	if len(c.entries) < c.maxEntries {
		return
	}
	now := c.now()
	n := len(c.keys)
	samples := min(evictSampleSize, n)
	evicted := false
	oldestIdx := -1
	var oldestExpiry int64
	for range samples {
		idx := rand.IntN(n) // #nosec G404 -- non-security: cache eviction sampling
		k := c.keys[idx]
		if e, ok := c.entries[k]; ok {
			if now > e.expiresAt {
				delete(c.entries, k)
				delete(c.keyIndex, k)
				last := n - 1
				if idx != last {
					c.keys[idx] = c.keys[last]
					c.keyIndex[c.keys[idx]] = idx
				}
				c.keys = c.keys[:last]
				n--
				evicted = true
				if n == 0 {
					return
				}
			} else if oldestIdx == -1 || e.expiresAt < oldestExpiry {
				oldestIdx = idx
				oldestExpiry = e.expiresAt
			}
		}
	}
	// If no expired entries were found, evict the entry closest to expiry
	// from the sampled set to make room for the new entry.
	if !evicted && oldestIdx >= 0 && n > 0 {
		k := c.keys[oldestIdx]
		delete(c.entries, k)
		delete(c.keyIndex, k)
		last := n - 1
		if oldestIdx != last {
			c.keys[oldestIdx] = c.keys[last]
			c.keyIndex[c.keys[oldestIdx]] = oldestIdx
		}
		c.keys = c.keys[:last]
	}
}

// removeKey removes a key from the keys slice in O(1) via swap-remove.
// Must be called with c.mu held.
func (c *attrCache) removeKey(key string) {
	idx, ok := c.keyIndex[key]
	if !ok {
		return
	}
	last := len(c.keys) - 1
	if idx != last {
		c.keys[idx] = c.keys[last]
		c.keyIndex[c.keys[idx]] = idx
	}
	c.keys = c.keys[:last]
	delete(c.keyIndex, key)
}

// len returns the number of entries (including expired) for testing.
func (c *attrCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// stats returns cumulative cache hit and miss counts.
func (c *attrCache) stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}
