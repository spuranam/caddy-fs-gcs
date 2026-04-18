// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Adapted from golang.org/x/build/internal/gcsfs (BSD-3-Clause).
// Modified for caddy-fs-gcs: removed write support, added io.ReadSeeker
// (via GCS range reads), added fs.StatFS, and added error translation.

package fs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// errInvalidWhence is pre-allocated to avoid fmt.Errorf allocations on the hot path.
var errInvalidWhence = errors.New("gcsfs: invalid whence")

// defaultOpTimeout is the per-operation timeout for individual GCS API
// calls (Attrs, NewRangeReader, Objects). This prevents unbounded
// blocking when the stored context has no deadline.
const defaultOpTimeout = 60 * time.Second

// gcsFS implements fs.FS, fs.StatFS, and fs.SubFS for Google Cloud Storage.
type gcsFS struct {
	ctx        context.Context
	client     *storage.Client
	bucket     *storage.BucketHandle
	bucketName string
	prefix     string
	opTimeout  time.Duration                          // per-operation timeout for GCS calls
	cache      *attrCache                             // optional; nil disables caching
	onEvent    func(name string, data map[string]any) // optional event callback

	// Metrics callbacks — all nil-safe; callers check before invoking.
	onCacheHit    func()                                        // attribute cache hit
	onCacheMiss   func()                                        // attribute cache miss
	onGCSOp       func(op string, dur time.Duration, err error) // GCS API call timing
	onStreamBytes func(n int64)                                 // bytes read from GCS
}

// WithContext returns a shallow copy of the filesystem that uses the
// given context for all GCS operations. This allows per-request context
// propagation (deadlines, cancellation) through the fs.FS interface.
func (fsys *gcsFS) WithContext(ctx context.Context) *gcsFS {
	cp := *fsys
	cp.ctx = ctx
	return &cp
}

// opCtx returns a context with a per-operation timeout derived from
// the filesystem's stored context. If the stored context already has
// a shorter deadline, that deadline is preserved.
func (fsys *gcsFS) opCtx() (context.Context, context.CancelFunc) {
	timeout := fsys.opTimeout
	if timeout == 0 {
		timeout = defaultOpTimeout
	}
	return context.WithTimeout(fsys.ctx, timeout)
}

// Compile-time interface assertions.
var (
	_ fs.FS     = (*gcsFS)(nil)
	_ fs.StatFS = (*gcsFS)(nil)
	_ fs.SubFS  = (*gcsFS)(nil)
)

// newGCSFS creates a new fs.StatFS backed by a GCS bucket.
// Creating the FS does not access the network. The bucket handle is
// pre-configured with automatic retries (exponential backoff, 3 attempts)
// so transient GCS errors are retried transparently.
func newGCSFS(ctx context.Context, client *storage.Client, bucket string) *gcsFS {
	return &gcsFS{
		ctx:    ctx,
		client: client,
		bucket: client.Bucket(bucket).Retryer(
			storage.WithPolicy(storage.RetryAlways),
			storage.WithMaxAttempts(3),
		),
		bucketName: bucket,
	}
}

func (fsys *gcsFS) object(name string) *storage.ObjectHandle {
	return fsys.bucket.Object(path.Join(fsys.prefix, name))
}

// Open opens the named file.
func (fsys *gcsFS) Open(name string) (fs.File, error) {
	if !validPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		name = ""
	}
	return &gcsFile{
		fs:       fsys,
		name:     strings.TrimSuffix(name, "/"),
		fullPath: path.Join(fsys.prefix, strings.TrimSuffix(name, "/")),
	}, nil
}

// Stat returns a FileInfo describing the named file.
func (fsys *gcsFS) Stat(name string) (fs.FileInfo, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// Sub returns an FS rooted at the given directory.
func (fsys *gcsFS) Sub(dir string) (fs.FS, error) {
	cp := *fsys
	cp.prefix = path.Join(fsys.prefix, dir)
	return &cp, nil
}

// validPath checks that name is a valid fs path (no backslashes).
func validPath(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsRune(name, '\\')
}

// emitEvent fires an event via the onEvent callback, if set.
func (fsys *gcsFS) emitEvent(name string, data map[string]any) {
	if fsys.onEvent != nil {
		fsys.onEvent(name, data)
	}
}

// recordCacheHit records an attribute cache hit metric.
func (fsys *gcsFS) recordCacheHit() {
	if fsys.onCacheHit != nil {
		fsys.onCacheHit()
	}
}

// recordCacheMiss records an attribute cache miss metric.
func (fsys *gcsFS) recordCacheMiss() {
	if fsys.onCacheMiss != nil {
		fsys.onCacheMiss()
	}
}

// recordGCSOp records a GCS API call duration and optional error.
func (fsys *gcsFS) recordGCSOp(op string, start time.Time, err error) {
	if fsys.onGCSOp != nil {
		fsys.onGCSOp(op, time.Since(start), err)
	}
}

// recordStreamBytes records bytes read from GCS.
func (fsys *gcsFS) recordStreamBytes(n int64) {
	if fsys.onStreamBytes != nil && n > 0 {
		fsys.onStreamBytes(n)
	}
}

// gcsFile implements fs.File, fs.ReadDirFile, and io.ReadSeeker for GCS.
type gcsFile struct {
	fs       *gcsFS
	name     string
	fullPath string // cached path.Join(fs.prefix, name)

	// Lazily initialised on first Read.
	reader io.ReadCloser
	// Current logical position (for Seek support).
	pos int64
	// Cached size from Stat, used by SeekEnd.
	size int64
	// Whether size has been resolved.
	sizeKnown bool

	// sniffBuf holds the first 512 bytes read from GCS so that
	// Seek(0, SeekStart) after an initial Read does NOT open a
	// new GCS reader (http.ServeContent reads 512 bytes for MIME
	// sniffing, then seeks back to 0).
	sniffBuf []byte
	// sniffLen is the number of valid bytes in sniffBuf.
	sniffLen int
	// sniffPos tracks how far into sniffBuf we've served (before
	// the reader takes over).
	sniffPos int

	// Directory iteration state.
	iter *storage.ObjectIterator
}

// Compile-time interface assertions.
var (
	_ fs.File        = (*gcsFile)(nil)
	_ fs.ReadDirFile = (*gcsFile)(nil)
	_ io.ReadSeeker  = (*gcsFile)(nil)
)

func (f *gcsFile) Close() error {
	if f.reader != nil {
		err := f.reader.Close()
		f.reader = nil
		if err != nil {
			return f.translateError("close", err)
		}
	}
	return nil
}

func (f *gcsFile) Read(b []byte) (int, error) {
	// Serve from the sniff buffer when the read position is within it.
	// This avoids opening a new GCS reader after http.ServeContent's
	// Seek(0, SeekStart) following its 512-byte MIME sniff read.
	if f.sniffBuf != nil && f.sniffPos < f.sniffLen && f.pos < int64(f.sniffLen) {
		n := copy(b, f.sniffBuf[f.sniffPos:f.sniffLen])
		f.sniffPos += n
		f.pos += int64(n)
		f.fs.recordStreamBytes(int64(n))
		return n, nil
	}

	if f.reader == nil {
		// Use the stored context directly for streaming readers — a
		// per-operation timeout would break large-file transfers.
		readerStart := time.Now()
		r, err := f.fs.object(f.name).NewRangeReader(f.fs.ctx, f.pos, -1)
		f.fs.recordGCSOp("read", readerStart, err)
		if err != nil {
			return 0, f.translateError("read", err)
		}
		f.reader = r

		// On the very first read (pos == 0), capture the first 512 bytes
		// into sniffBuf so a subsequent Seek(0)+Read can be served locally.
		if f.pos == 0 && f.sniffBuf == nil {
			const sniffSize = 512
			f.sniffBuf = make([]byte, sniffSize)
			n, readErr := io.ReadFull(f.reader, f.sniffBuf)
			f.sniffLen = n
			f.sniffBuf = f.sniffBuf[:n]
			if n > 0 {
				copied := copy(b, f.sniffBuf[:n])
				f.sniffPos = copied
				f.pos += int64(copied)
				f.fs.recordStreamBytes(int64(copied))
				return copied, nil
			}
			// n == 0 means immediate EOF or error
			if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
				return 0, f.translateError("read", readErr)
			}
			return 0, io.EOF
		}
	}
	n, err := f.reader.Read(b)
	f.pos += int64(n)
	f.fs.recordStreamBytes(int64(n))
	if err != nil && err != io.EOF {
		return n, f.translateError("read", err)
	}
	return n, err
}

// seekSkipThreshold is the maximum number of bytes that Seek will discard
// from the current reader via io.CopyN instead of closing and reopening
// a new GCS range reader. This avoids an HTTP round-trip for small forward
// seeks (e.g., skipping a few KB in a streaming response).
const seekSkipThreshold = 32 * 1024 // 32 KiB

// Seek implements io.Seeker. On the next Read, the GCS reader is reopened
// at the new offset via NewRangeReader — unless the seek target is within
// the sniff buffer or a small forward skip from the current position, in
// which case no GCS round-trip is needed.
func (f *gcsFile) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = f.pos + offset
	case io.SeekEnd:
		if !f.sizeKnown {
			info, err := f.Stat()
			if err != nil {
				return 0, err
			}
			f.size = info.Size()
			f.sizeKnown = true
		}
		newPos = f.size + offset
	default:
		return 0, errInvalidWhence
	}
	if newPos < 0 {
		f.pos = 0
		return 0, &fs.PathError{Op: "seek", Path: f.name, Err: fs.ErrInvalid}
	}

	// If the new position is within the sniff buffer, just adjust sniffPos
	// and keep the existing reader (avoids a GCS round-trip).
	// However, if reads advanced the underlying reader past sniffLen,
	// close it so subsequent reads after the sniff buffer reopen at the
	// correct offset.
	if f.sniffBuf != nil && newPos < int64(f.sniffLen) {
		if f.reader != nil && f.pos > int64(f.sniffLen) {
			_ = f.reader.Close() // #nosec G104 -- reader is desynchronized
			f.reader = nil
		}
		f.pos = newPos
		f.sniffPos = int(newPos)
		return f.pos, nil
	}

	// For small forward seeks on an open reader, discard bytes instead of
	// closing and reopening a new GCS HTTP connection.
	// Skip this optimisation when the sniff buffer is only partially consumed:
	// the GCS reader is positioned at sniffLen, not at f.pos, so the skip
	// amount would be wrong.
	sniffFullyConsumed := f.sniffBuf == nil || f.sniffPos >= f.sniffLen
	skip := newPos - f.pos
	if f.reader != nil && sniffFullyConsumed && skip > 0 && skip <= seekSkipThreshold {
		discarded, err := io.CopyN(io.Discard, f.reader, skip)
		f.pos += discarded
		if err == nil {
			return f.pos, nil
		}
		// On error (e.g., EOF mid-skip), fall through to close+reopen.
	}

	f.pos = newPos

	// Close existing reader so the next Read reopens at the new position.
	if f.reader != nil {
		_ = f.reader.Close() // #nosec G104 -- best-effort close before reopen at new offset
		f.reader = nil
	}
	return f.pos, nil
}

// ReadDir implements fs.ReadDirFile.
func (f *gcsFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.iter == nil {
		f.iter = f.fs.iteratorFor(f.name)
	}
	initialCap := 16
	if n > 0 {
		initialCap = min(n, 64)
	}
	result := make([]fs.DirEntry, 0, initialCap)
	for {
		attrs, err := f.iter.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				if n <= 0 {
					return result, nil
				}
				return result, io.EOF
			}
			return result, f.translateError("readdir", err)
		}
		result = append(result, &gcsFileInfo{attrs: attrs})
		if n > 0 && len(result) == n {
			break
		}
	}
	return result, nil
}

// Stat returns the FileInfo for this file. The returned FileInfo exposes
// *storage.ObjectAttrs as its Sys() result.
func (f *gcsFile) Stat() (fs.FileInfo, error) {
	cacheKey := f.fullPath

	// Check the attribute cache first.
	if f.fs.cache != nil {
		if attrs, ok := f.fs.cache.get(cacheKey); ok {
			f.fs.recordCacheHit()
			if isNegative(attrs) {
				return nil, f.translateError("stat", storage.ErrObjectNotExist)
			}
			return &gcsFileInfo{attrs: attrs}, nil
		}
		f.fs.recordCacheMiss()
	}

	// Root ("") is always a directory — no GCS call needed.
	// Caddy's file_server calls Stat(".") on every request; short-circuiting
	// here avoids a costly Objects.list round-trip.
	if f.name == "" {
		dirAttrs := &storage.ObjectAttrs{Prefix: "/"}
		if f.fs.cache != nil {
			f.fs.cache.set(cacheKey, dirAttrs)
		}
		return &gcsFileInfo{attrs: dirAttrs}, nil
	}

	// Check for a real object.
	ctx, cancel := f.fs.opCtx()
	defer cancel()
	attrsStart := time.Now()
	attrs, err := f.fs.object(f.name).Attrs(ctx)
	f.fs.recordGCSOp("attrs", attrsStart, err)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return nil, f.translateError("stat", err)
	}
	if err == nil {
		if f.fs.cache != nil {
			f.fs.cache.set(cacheKey, attrs)
		}
		return &gcsFileInfo{attrs: attrs}, nil
	}
	// Skip the directory-prefix check for paths with a file extension (e.g.
	// ".html", ".css", ".js") — these are never GCS directory prefixes, so
	// the Objects.list round-trip is wasted. This halves the latency for 404s
	// on static asset paths.
	if path.Ext(f.name) != "" {
		return nil, f.translateError("stat", storage.ErrObjectNotExist)
	}
	// Check for a "directory" (a common prefix).
	// Use a bounded context for the single-call directory probe so a
	// GCS outage doesn't hang the goroutine indefinitely.
	probeCtx, probeCancel := f.fs.opCtx()
	defer probeCancel()
	dirPrefix := path.Join(f.fs.prefix, f.name)
	if dirPrefix != "" {
		dirPrefix += "/"
	}
	iter := f.fs.bucket.Objects(probeCtx, &storage.Query{
		Delimiter: "/",
		Prefix:    dirPrefix,
	})
	probeStart := time.Now()
	_, iterErr := iter.Next()
	f.fs.recordGCSOp("list", probeStart, iterErr)
	if iterErr == nil {
		dirAttrs := &storage.ObjectAttrs{Prefix: f.name + "/"}
		if f.fs.cache != nil {
			f.fs.cache.set(cacheKey, dirAttrs)
		}
		return &gcsFileInfo{attrs: dirAttrs}, nil
	}
	// Only cache the miss when the iterator confirmed no entries exist
	// (iterator.Done). Transient errors (timeout, network) must not be
	// cached as "not found" — the path may exist as a directory.
	if errors.Is(iterErr, iterator.Done) {
		f.cacheNotFound()
	}
	return nil, f.translateError("stat", storage.ErrObjectNotExist)
}

// cacheNotFound stores a negative entry so repeated 404s don't hit GCS.
func (f *gcsFile) cacheNotFound() {
	if f.fs.cache != nil {
		f.fs.cache.setNotFound(f.fullPath)
	}
}

func (f *gcsFile) translateError(op string, err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	nested := err
	if errors.Is(err, storage.ErrBucketNotExist) || errors.Is(err, storage.ErrObjectNotExist) {
		nested = fs.ErrNotExist
		f.cacheNotFound()
		f.fs.emitEvent("gcs.object_not_found", map[string]any{
			"bucket": f.fs.bucketName,
			"object": f.fullPath,
			"op":     op,
		})
	} else if pe, ok := errors.AsType[*fs.PathError](err); ok {
		nested = pe.Err
	} else {
		f.fs.emitEvent("gcs.backend_error", map[string]any{
			"bucket": f.fs.bucketName,
			"object": f.fullPath,
			"op":     op,
			"error":  err.Error(),
		})
	}
	return &fs.PathError{Op: op, Path: f.name, Err: nested}
}

// iteratorFor returns an object iterator scoped to the given prefix.
// Uses the stored context directly because the iterator is consumed
// incrementally — a per-operation timeout would break large listings.
func (fsys *gcsFS) iteratorFor(name string) *storage.ObjectIterator {
	prefix := path.Join(fsys.prefix, name)
	if prefix != "" {
		prefix += "/"
	}
	return fsys.bucket.Objects(fsys.ctx, &storage.Query{
		Delimiter: "/",
		Prefix:    prefix,
	})
}

// gcsFileInfo implements fs.FileInfo and fs.DirEntry.
type gcsFileInfo struct {
	attrs *storage.ObjectAttrs
}

var (
	_ fs.FileInfo = (*gcsFileInfo)(nil)
	_ fs.DirEntry = (*gcsFileInfo)(nil)
)

func (fi *gcsFileInfo) Name() string {
	if fi.attrs.Prefix != "" {
		return path.Base(fi.attrs.Prefix)
	}
	return path.Base(fi.attrs.Name)
}

func (fi *gcsFileInfo) Size() int64 { return fi.attrs.Size }
func (fi *gcsFileInfo) Mode() fs.FileMode {
	if fi.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (fi *gcsFileInfo) ModTime() time.Time { return fi.attrs.Updated }
func (fi *gcsFileInfo) IsDir() bool        { return fi.attrs.Prefix != "" }
func (fi *gcsFileInfo) Sys() any           { return fi.attrs }

// fs.DirEntry methods.
func (fi *gcsFileInfo) Info() (fs.FileInfo, error) { return fi, nil }
func (fi *gcsFileInfo) Type() fs.FileMode          { return fi.Mode() & fs.ModeType }
