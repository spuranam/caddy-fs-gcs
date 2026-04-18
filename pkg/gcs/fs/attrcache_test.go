package fs

import (
	"os"
	"testing"
	"time"

	"cloud.google.com/go/storage"
)

func TestMain(m *testing.M) {
	code := m.Run()
	stopSharedClock()
	os.Exit(code)
}

func TestAttrCacheGetMiss(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	if _, ok := c.get("missing"); ok {
		t.Fatal("expected miss for absent key")
	}
}

func TestAttrCacheSetAndGet(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	attrs := &storage.ObjectAttrs{Name: "hello.txt", Size: 42}
	c.set("hello.txt", attrs)

	got, ok := c.get("hello.txt")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "hello.txt" || got.Size != 42 {
		t.Fatalf("got %+v, want name=hello.txt size=42", got)
	}
}

func TestAttrCacheExpiry(t *testing.T) {
	c := newAttrCache(10*time.Millisecond, 100)
	c.set("short.txt", &storage.ObjectAttrs{Name: "short.txt"})

	// Should hit immediately.
	if _, ok := c.get("short.txt"); !ok {
		t.Fatal("expected hit before expiry")
	}

	// Wait for expiry and force clock update (ticker is 100ms now).
	time.Sleep(20 * time.Millisecond)
	sharedClock.val.Store(time.Now().UnixNano())

	if _, ok := c.get("short.txt"); ok {
		t.Fatal("expected miss after expiry")
	}
}

func TestAttrCacheMaxEntries(t *testing.T) {
	c := newAttrCache(time.Minute, 2)
	c.set("a", &storage.ObjectAttrs{Name: "a"})
	c.set("b", &storage.ObjectAttrs{Name: "b"})
	c.set("c", &storage.ObjectAttrs{Name: "c"}) // evicts oldest to make room

	if c.len() != 2 {
		t.Fatalf("len = %d, want 2", c.len())
	}
	// "c" should now be present since eviction makes room.
	if _, ok := c.get("c"); !ok {
		t.Fatal("expected hit for c (eviction should have made room)")
	}
}

func TestAttrCacheEvictsExpiredOnFull(t *testing.T) {
	c := newAttrCache(10*time.Millisecond, 2)
	c.set("old1", &storage.ObjectAttrs{Name: "old1"})
	c.set("old2", &storage.ObjectAttrs{Name: "old2"})

	// Wait for entries to expire and force clock update.
	time.Sleep(20 * time.Millisecond)
	sharedClock.val.Store(time.Now().UnixNano())

	// Now a set should evict the expired entries and succeed.
	c.set("new", &storage.ObjectAttrs{Name: "new"})

	if _, ok := c.get("new"); !ok {
		t.Fatal("expected hit for new entry after eviction")
	}
	if c.len() != 1 {
		t.Fatalf("len = %d, want 1", c.len())
	}
}

func TestAttrCacheOverwrite(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	c.set("x", &storage.ObjectAttrs{Name: "x", Size: 1})
	c.set("x", &storage.ObjectAttrs{Name: "x", Size: 2})

	got, ok := c.get("x")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Size != 2 {
		t.Fatalf("Size = %d, want 2", got.Size)
	}
}

func TestAttrCacheConcurrent(t *testing.T) {
	c := newAttrCache(time.Minute, 1000)
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		defer close(done)
		for i := range 500 {
			c.set("key", &storage.ObjectAttrs{Name: "key", Size: int64(i)})
		}
	}()

	// Reader goroutine (runs concurrently).
	for range 500 {
		c.get("key")
	}

	<-done
}

func TestAttrCacheStats(t *testing.T) {
	c := newAttrCache(time.Minute, 100)

	// Initially zero.
	hits, misses := c.stats()
	if hits != 0 || misses != 0 {
		t.Fatalf("initial stats = (%d, %d), want (0, 0)", hits, misses)
	}

	// Miss on absent key.
	c.get("absent")
	hits, misses = c.stats()
	if hits != 0 || misses != 1 {
		t.Fatalf("after miss: stats = (%d, %d), want (0, 1)", hits, misses)
	}

	// Hit after set.
	c.set("key", &storage.ObjectAttrs{Name: "key"})
	c.get("key")
	hits, misses = c.stats()
	if hits != 1 || misses != 1 {
		t.Fatalf("after hit: stats = (%d, %d), want (1, 1)", hits, misses)
	}
}

func TestAttrCacheStatsExpiredMiss(t *testing.T) {
	c := newAttrCache(time.Millisecond, 100)
	c.set("expiring", &storage.ObjectAttrs{Name: "expiring"})
	time.Sleep(5 * time.Millisecond)
	sharedClock.val.Store(time.Now().UnixNano())

	c.get("expiring")
	_, misses := c.stats()
	if misses != 1 {
		t.Fatalf("expired miss: misses = %d, want 1", misses)
	}
}

// --- setNotFound coverage ---

func TestAttrCacheSetNotFound(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	defer c.stop()

	c.setNotFound("missing.txt")

	attrs, ok := c.get("missing.txt")
	if !ok {
		t.Fatal("expected to find negative cache entry")
	}
	if !isNegative(attrs) {
		t.Fatal("expected sentinel not-found marker")
	}
}

func TestAttrCacheSetNotFoundWhenFull(t *testing.T) {
	c := newAttrCache(time.Minute, 2)
	defer c.stop()

	c.set("a", &storage.ObjectAttrs{Name: "a"})
	c.set("b", &storage.ObjectAttrs{Name: "b"})
	c.setNotFound("c") // eviction makes room

	attrs, ok := c.get("c")
	if !ok {
		t.Fatal("expected c to be in cache after eviction made room")
	}
	if !isNegative(attrs) {
		t.Fatal("expected negative entry for c")
	}
}

func TestAttrCacheSetNotFoundOverwrite(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	defer c.stop()

	// First set positive, then overwrite with negative
	c.set("key", &storage.ObjectAttrs{Name: "key"})
	c.setNotFound("key")

	attrs, ok := c.get("key")
	if !ok {
		t.Fatal("expected to find entry")
	}
	if !isNegative(attrs) {
		t.Fatal("expected negative entry after overwrite")
	}
}

func TestAttrCacheRemoveKeyNonExistent(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	defer c.stop()

	c.set("a", &storage.ObjectAttrs{Name: "a"})

	// Remove non-existent key — should not panic
	c.mu.Lock()
	c.removeKey("nonexistent")
	c.mu.Unlock()

	// Original key still present
	if _, ok := c.get("a"); !ok {
		t.Fatal("expected a to still be present")
	}
}

func TestAttrCacheRemoveKeyFromMiddle(t *testing.T) {
	c := newAttrCache(time.Minute, 100)
	defer c.stop()

	c.set("a", &storage.ObjectAttrs{Name: "a"})
	c.set("b", &storage.ObjectAttrs{Name: "b"})
	c.set("c", &storage.ObjectAttrs{Name: "c"})

	// removeKey only removes from keys/keyIndex, not entries.
	// Verify keys slice update.
	c.mu.Lock()
	before := len(c.keys)
	c.removeKey("b")
	after := len(c.keys)
	c.mu.Unlock()

	if after != before-1 {
		t.Fatalf("keys len = %d, want %d", after, before-1)
	}
}
