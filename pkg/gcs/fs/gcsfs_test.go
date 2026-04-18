package fs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// ---------- unit tests for gcsFileInfo ----------

func TestGcsFileInfoFile(t *testing.T) {
	fi := &gcsFileInfo{
		attrs: &storage.ObjectAttrs{
			Name: "dir/hello.txt",
			Size: 42,
		},
	}

	if fi.Name() != "hello.txt" {
		t.Errorf("Name() = %q, want %q", fi.Name(), "hello.txt")
	}
	if fi.Size() != 42 {
		t.Errorf("Size() = %d, want 42", fi.Size())
	}
	if fi.IsDir() {
		t.Error("IsDir() = true, want false")
	}
	if fi.Mode() != 0o444 {
		t.Errorf("Mode() = %v, want 0o444", fi.Mode())
	}
	if fi.Type() != 0 {
		t.Errorf("Type() = %v, want 0", fi.Type())
	}
	info, err := fi.Info()
	if err != nil || info != fi {
		t.Errorf("Info() = (%v, %v), want (%v, nil)", info, err, fi)
	}
	if fi.Sys().(*storage.ObjectAttrs) != fi.attrs {
		t.Error("Sys() did not return attrs")
	}
}

func TestGcsFileInfoDir(t *testing.T) {
	fi := &gcsFileInfo{
		attrs: &storage.ObjectAttrs{
			Prefix: "subdir/",
		},
	}

	if fi.Name() != "subdir" {
		t.Errorf("Name() = %q, want %q", fi.Name(), "subdir")
	}
	if !fi.IsDir() {
		t.Error("IsDir() = false, want true")
	}
	if fi.Mode() != fs.ModeDir|0o555 {
		t.Errorf("Mode() = %v, want ModeDir|0o555", fi.Mode())
	}
	if fi.Type() != fs.ModeDir {
		t.Errorf("Type() = %v, want ModeDir", fi.Type())
	}
}

// ---------- unit tests for path validation ----------

func TestValidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"root dot", ".", true},
		{"simple file", "hello.txt", true},
		{"nested file", "dir/hello.txt", true},
		{"backslash", `dir\file`, false},
		{"absolute", "/abs", false},
		{"empty", "", false},
		{"dot dot", "..", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validPath(tt.path); got != tt.want {
				t.Errorf("validPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ---------- unit tests for gcsFile.Seek ----------

func TestSeekWithoutRead(t *testing.T) {
	fsys := &gcsFS{} // client is nil; we won't actually call GCS
	f := &gcsFile{
		fs:        fsys,
		name:      "test.txt",
		size:      100,
		sizeKnown: true,
	}

	// SeekStart
	pos, err := f.Seek(10, io.SeekStart)
	if err != nil || pos != 10 {
		t.Fatalf("SeekStart(10): pos=%d, err=%v", pos, err)
	}

	// SeekCurrent
	pos, err = f.Seek(5, io.SeekCurrent)
	if err != nil || pos != 15 {
		t.Fatalf("SeekCurrent(5): pos=%d, err=%v", pos, err)
	}

	// SeekEnd
	pos, err = f.Seek(-20, io.SeekEnd)
	if err != nil || pos != 80 {
		t.Fatalf("SeekEnd(-20): pos=%d, err=%v", pos, err)
	}

	// Seek before start → clamp to 0 + error
	pos, err = f.Seek(-200, io.SeekStart)
	if err == nil {
		t.Fatal("expected error for negative seek")
	}
	if pos != 0 {
		t.Fatalf("expected pos=0, got %d", pos)
	}
}

// ---------- unit tests for translateError ----------

func TestTranslateError(t *testing.T) {
	f := &gcsFile{
		fs:   &gcsFS{},
		name: "test.txt",
	}

	// nil passes through
	if err := f.translateError("read", nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// io.EOF passes through
	if err := f.translateError("read", io.EOF); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	// storage.ErrObjectNotExist becomes fs.ErrNotExist
	err := f.translateError("stat", storage.ErrObjectNotExist)
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected PathError, got %T: %v", err, err)
	}
	if !errors.Is(pe.Err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", pe.Err)
	}

	// storage.ErrBucketNotExist becomes fs.ErrNotExist
	err = f.translateError("stat", storage.ErrBucketNotExist)
	pe2, ok2 := errors.AsType[*fs.PathError](err)
	if !ok2 || !errors.Is(pe2.Err, fs.ErrNotExist) {
		t.Fatalf("bucket not exist: expected PathError wrapping fs.ErrNotExist, got %v", err)
	}
}

// ---------- unit tests for gcsFS.Open validation ----------

func TestOpenInvalidPaths(t *testing.T) {
	fsys := &gcsFS{}

	_, err := fsys.Open(`back\slash`)
	if err == nil {
		t.Error("expected error for backslash path")
	}

	_, err = fsys.Open("/absolute")
	if err == nil {
		t.Error("expected error for absolute path")
	}

	_, err = fsys.Open("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

// ---------- fstest.TestFS placeholder ----------
// This requires a live GCS emulator. The test is skipped when
// STORAGE_EMULATOR_HOST is not set. Run with:
//
//	STORAGE_EMULATOR_HOST=localhost:4443 go test -run TestFSTestWithEmulator ./pkg/gcs/fs/

func TestFSTestWithEmulator(t *testing.T) {
	if os.Getenv("STORAGE_EMULATOR_HOST") == "" {
		t.Skip("requires GCS emulator; run with STORAGE_EMULATOR_HOST set")
	}

	// When implementing integration tests, use fstest.TestFS:
	_ = fstest.TestFS
}

// ---------- helpers ----------

// newTestClient creates a storage.Client pointing at a fake emulator.
// No network connection is established — only the client is configured.
func newTestClient(t *testing.T) *storage.Client {
	t.Helper()
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	client, err := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("storage.NewClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// ---------- newGCSFS ----------

func TestNewGCSFS(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")

	if fsys.client != client {
		t.Fatal("client not stored")
	}
	if fsys.bucket == nil {
		t.Fatal("bucket handle is nil")
	}
	if fsys.prefix != "" {
		t.Fatalf("prefix = %q, want empty", fsys.prefix)
	}
	if fsys.cache != nil {
		t.Fatal("cache should be nil by default")
	}
}

// ---------- gcsFS.Open ----------

func TestOpenValidPaths(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")

	tests := []struct {
		name     string
		path     string
		wantName string
	}{
		{"root", ".", ""},
		{"simple file", "hello.txt", "hello.txt"},
		{"nested file", "dir/sub/file.txt", "dir/sub/file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := fsys.Open(tt.path)
			if err != nil {
				t.Fatalf("Open(%q) error: %v", tt.path, err)
			}
			defer f.Close()
			gf := f.(*gcsFile)
			if gf.name != tt.wantName {
				t.Errorf("name = %q, want %q", gf.name, tt.wantName)
			}
			if gf.fs != fsys {
				t.Error("file does not reference parent FS")
			}
		})
	}
}

// ---------- gcsFS.Sub ----------

func TestSub(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")

	sub, err := fsys.Sub("sites/prod")
	if err != nil {
		t.Fatalf("Sub() error: %v", err)
	}
	gfs := sub.(*gcsFS)
	if gfs.prefix != "sites/prod" {
		t.Errorf("prefix = %q, want %q", gfs.prefix, "sites/prod")
	}
	// Original should be unchanged.
	if fsys.prefix != "" {
		t.Errorf("original prefix changed to %q", fsys.prefix)
	}
	// Nested sub.
	sub2, err := gfs.Sub("v2")
	if err != nil {
		t.Fatalf("nested Sub() error: %v", err)
	}
	if sub2.(*gcsFS).prefix != "sites/prod/v2" {
		t.Errorf("nested prefix = %q, want %q", sub2.(*gcsFS).prefix, "sites/prod/v2")
	}
}

// ---------- gcsFS.object ----------

func TestObject(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")
	fsys.prefix = "sites"

	obj := fsys.object("index.html")
	if obj == nil {
		t.Fatal("object returned nil")
	}
}

// ---------- gcsFile.Close ----------

func TestCloseWithoutReader(t *testing.T) {
	f := &gcsFile{fs: &gcsFS{}, name: "test.txt"}
	if err := f.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

type errReader struct{ closeErr error }

func (r *errReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *errReader) Close() error             { return r.closeErr }

func TestCloseWithReader(t *testing.T) {
	// Close succeeds when reader.Close() succeeds.
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &errReader{closeErr: nil},
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if f.reader != nil {
		t.Fatal("reader not cleared after Close")
	}
}

func TestCloseWithReaderError(t *testing.T) {
	closeErr := errors.New("disk error")
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &errReader{closeErr: closeErr},
	}
	err := f.Close()
	if err == nil {
		t.Fatal("expected error from Close")
	}
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pe.Op != "close" {
		t.Errorf("Op = %q, want %q", pe.Op, "close")
	}
}

// ---------- gcsFile.Seek (additional branches) ----------

func TestSeekInvalidWhence(t *testing.T) {
	f := &gcsFile{fs: &gcsFS{}, name: "test.txt"}
	_, err := f.Seek(0, 99)
	if err == nil {
		t.Fatal("expected error for invalid whence")
	}
}

func TestSeekClosesExistingReader(t *testing.T) {
	closed := false
	f := &gcsFile{
		fs:   &gcsFS{},
		name: "test.txt",
		reader: &errReader{
			closeErr: nil,
		},
		pos: 10,
	}
	// Replace with a tracking closer.
	f.reader = closerFunc(func() error {
		closed = true
		return nil
	})
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek error: %v", err)
	}
	if !closed {
		t.Error("existing reader was not closed")
	}
	if f.reader != nil {
		t.Error("reader not nil after Seek")
	}
}

type closerFunc func() error

func (fn closerFunc) Read([]byte) (int, error) { return 0, io.EOF }
func (fn closerFunc) Close() error             { return fn() }

// ---------- gcsFile.Read with pre-set reader ----------

type dataReader struct {
	data   []byte
	offset int
}

func (r *dataReader) Read(b []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(b, r.data[r.offset:])
	r.offset += n
	if r.offset >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}
func (r *dataReader) Close() error { return nil }

func TestReadWithPresetReader(t *testing.T) {
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &dataReader{data: []byte("hello")},
	}

	buf := make([]byte, 10)
	n, err := f.Read(buf)
	if n != 5 {
		t.Fatalf("Read() n = %d, want 5", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Read() err = %v, want io.EOF", err)
	}
	if f.pos != 5 {
		t.Fatalf("pos = %d, want 5", f.pos)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("data = %q, want %q", buf[:n], "hello")
	}
}

func TestReadWithPresetReaderNonEOFError(t *testing.T) {
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &errReader{closeErr: nil}, // Read returns EOF, but let's use a real error
	}
	// Replace with something that returns a non-EOF error.
	f.reader = readerWithErr{err: errors.New("network error")}

	buf := make([]byte, 10)
	_, err := f.Read(buf)
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T: %v", err, err)
	}
	if pe.Op != "read" {
		t.Errorf("Op = %q, want %q", pe.Op, "read")
	}
}

type readerWithErr struct{ err error }

func (r readerWithErr) Read([]byte) (int, error) { return 0, r.err }
func (r readerWithErr) Close() error             { return nil }

// ---------- gcsFile.Stat with cache hit ----------

func TestFileStatCacheHit(t *testing.T) {
	cache := newAttrCache(time.Minute, 100)
	attrs := &storage.ObjectAttrs{Name: "hello.txt", Size: 42}
	cache.set("hello.txt", attrs)

	fsys := &gcsFS{cache: cache}
	f := &gcsFile{fs: fsys, name: "hello.txt", fullPath: "hello.txt"}

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if fi.Name() != "hello.txt" {
		t.Errorf("Name() = %q, want %q", fi.Name(), "hello.txt")
	}
	if fi.Size() != 42 {
		t.Errorf("Size() = %d, want 42", fi.Size())
	}
}

func TestFileStatCacheHitWithPrefix(t *testing.T) {
	cache := newAttrCache(time.Minute, 100)
	attrs := &storage.ObjectAttrs{Name: "sites/prod/index.html", Size: 100}
	cache.set("sites/prod/index.html", attrs)

	fsys := &gcsFS{prefix: "sites/prod", cache: cache}
	f := &gcsFile{fs: fsys, name: "index.html", fullPath: "sites/prod/index.html"}

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if fi.Name() != "index.html" {
		t.Errorf("Name() = %q, want %q", fi.Name(), "index.html")
	}
}

func TestFileStatNoCacheStillChecks(t *testing.T) {
	// When cache is nil, the code skips the cache check but still
	// calls Attrs. With emulator at closed port, this will error.
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")
	fsys.cache = nil // explicitly no cache

	f, err := fsys.Open("test.txt")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer f.Close()

	_, err = f.Stat()
	if err == nil {
		t.Fatal("expected Stat error with no cache and no emulator")
	}
}

// ---------- gcsFileInfo.ModTime ----------

func TestGcsFileInfoModTime(t *testing.T) {
	now := time.Now()
	fi := &gcsFileInfo{attrs: &storage.ObjectAttrs{Updated: now}}
	if got := fi.ModTime(); !got.Equal(now) {
		t.Errorf("ModTime() = %v, want %v", got, now)
	}
}

// ---------- gcsFS.iteratorFor ----------

func TestIteratorFor(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")
	fsys.prefix = "sites"

	iter := fsys.iteratorFor("docs")
	if iter == nil {
		t.Fatal("iteratorFor returned nil")
	}
}

func TestIteratorForRootPrefix(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")

	iter := fsys.iteratorFor("")
	if iter == nil {
		t.Fatal("iteratorFor('') returned nil")
	}
}

// ---------- translateError: PathError unwrap ----------

func TestTranslateErrorPathError(t *testing.T) {
	f := &gcsFile{fs: &gcsFS{}, name: "test.txt"}
	inner := &fs.PathError{Op: "inner", Path: "x", Err: fs.ErrPermission}
	err := f.translateError("read", inner)
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected PathError, got %T: %v", err, err)
	}
	if !errors.Is(pe.Err, fs.ErrPermission) {
		t.Errorf("inner Err = %v, want fs.ErrPermission", pe.Err)
	}
}

// ---------- gcsFS.Stat (delegates to Open+Stat) ----------

func TestStatInvalidPath(t *testing.T) {
	client := newTestClient(t)
	fsys := newGCSFS(context.Background(), client, "test-bucket")
	_, err := fsys.Stat(`bad\path`)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// ---------- ensure STORAGE_EMULATOR_HOST env not leaked ----------

func TestEmulatorEnvNotSet(t *testing.T) {
	// Verify t.Setenv cleanup works — STORAGE_EMULATOR_HOST should not be set
	// in a fresh test.
	if v := os.Getenv("STORAGE_EMULATOR_HOST"); v != "" {
		t.Skipf("STORAGE_EMULATOR_HOST already set to %q", v)
	}
}

// ---------- Read/Stat/ReadDir error paths (emulator pointing at closed port) ----------
// These tests use a very short context timeout to avoid the GCS client's
// retry backoff when connecting to a non-listening port.

func TestReadError(t *testing.T) {
	client := newTestClient(t) // emulator at localhost:1 (no listener)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")

	f, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err == nil {
		t.Fatal("expected error from Read (no emulator)")
	}
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T: %v", err, err)
	}
	if pe.Op != "read" {
		t.Errorf("Op = %q, want %q", pe.Op, "read")
	}
}

func TestFileStatError(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")

	f, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer f.Close()

	_, err = f.Stat()
	if err == nil {
		t.Fatal("expected error from Stat (no emulator)")
	}
	_, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T: %v", err, err)
	}
}

func TestFSStatError(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")

	_, err := fsys.Stat("hello.txt")
	if err == nil {
		t.Fatal("expected error from Stat (no emulator)")
	}
}

func TestReadDirError(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")

	f, err := fsys.Open("somedir")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer f.Close()

	rdf := f.(fs.ReadDirFile)
	_, err = rdf.ReadDir(-1)
	if err == nil {
		t.Fatal("expected error from ReadDir (no emulator)")
	}
}

func TestSeekEndWithStatError(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "test-bucket")

	f, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer f.Close()

	// SeekEnd needs to Stat to get size; Stat will fail due to no emulator.
	_, err = f.(io.ReadSeeker).Seek(0, io.SeekEnd)
	if err == nil {
		t.Fatal("expected error from Seek(SeekEnd)")
	}
}

// ---------- Event emission tests ----------

func TestTranslateErrorEmitsObjectNotFound(t *testing.T) {
	var emittedName string
	var emittedData map[string]any

	fsys := &gcsFS{
		bucketName: "test-bucket",
		onEvent: func(name string, data map[string]any) {
			emittedName = name
			emittedData = data
		},
	}
	f := &gcsFile{fs: fsys, name: "missing.txt", fullPath: "missing.txt"}

	err := f.translateError("stat", storage.ErrObjectNotExist)
	if err == nil {
		t.Fatal("expected error")
	}
	if emittedName != "gcs.object_not_found" {
		t.Errorf("event name = %q, want %q", emittedName, "gcs.object_not_found")
	}
	if emittedData["bucket"] != "test-bucket" {
		t.Errorf("bucket = %v, want test-bucket", emittedData["bucket"])
	}
	if emittedData["object"] != "missing.txt" {
		t.Errorf("object = %v, want missing.txt", emittedData["object"])
	}
	if emittedData["op"] != "stat" {
		t.Errorf("op = %v, want stat", emittedData["op"])
	}
}

func TestTranslateErrorEmitsBackendError(t *testing.T) {
	var emittedName string
	var emittedData map[string]any

	fsys := &gcsFS{
		bucketName: "test-bucket",
		onEvent: func(name string, data map[string]any) {
			emittedName = name
			emittedData = data
		},
	}
	f := &gcsFile{fs: fsys, name: "broken.txt"}

	err := f.translateError("read", errors.New("connection refused"))
	if err == nil {
		t.Fatal("expected error")
	}
	if emittedName != "gcs.backend_error" {
		t.Errorf("event name = %q, want %q", emittedName, "gcs.backend_error")
	}
	if emittedData["error"] != "connection refused" {
		t.Errorf("error = %v, want connection refused", emittedData["error"])
	}
}

func TestTranslateErrorNilCallbackNoOp(t *testing.T) {
	fsys := &gcsFS{bucketName: "test-bucket"} // no onEvent set
	f := &gcsFile{fs: fsys, name: "test.txt"}

	// Should not panic with nil onEvent.
	err := f.translateError("stat", storage.ErrObjectNotExist)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranslateErrorBucketNotExistEmitsEvent(t *testing.T) {
	var emittedName string

	fsys := &gcsFS{
		bucketName: "no-bucket",
		onEvent: func(name string, data map[string]any) {
			emittedName = name
		},
	}
	f := &gcsFile{fs: fsys, name: "file.txt"}

	err := f.translateError("stat", storage.ErrBucketNotExist)
	if err == nil {
		t.Fatal("expected error")
	}
	if emittedName != "gcs.object_not_found" {
		t.Errorf("event name = %q, want %q", emittedName, "gcs.object_not_found")
	}
}

func TestEmitEventWithPrefix(t *testing.T) {
	var emittedData map[string]any

	fsys := &gcsFS{
		bucketName: "test-bucket",
		prefix:     "sites/prod",
		onEvent: func(name string, data map[string]any) {
			emittedData = data
		},
	}
	f := &gcsFile{fs: fsys, name: "index.html", fullPath: "sites/prod/index.html"}

	f.translateError("stat", storage.ErrObjectNotExist)
	if emittedData["object"] != "sites/prod/index.html" {
		t.Errorf("object = %v, want sites/prod/index.html", emittedData["object"])
	}
}

// ---------- WithContext ----------

func TestWithContext(t *testing.T) {
	t.Parallel()
	orig := &gcsFS{
		ctx:        context.Background(),
		bucketName: "b",
		prefix:     "p",
	}
	newCtx := t.Context()

	cp := orig.WithContext(newCtx)
	if cp.ctx != newCtx {
		t.Fatal("WithContext did not set new context")
	}
	// Ensure original is unchanged.
	if orig.ctx == newCtx {
		t.Fatal("WithContext mutated original")
	}
	// Shared fields should be the same.
	if cp.bucketName != orig.bucketName || cp.prefix != orig.prefix {
		t.Fatal("WithContext did not preserve fields")
	}
}

// ---------- Root Stat short-circuit ----------

func TestRootStatShortCircuit(t *testing.T) {
	t.Parallel()
	// Create a gcsFS without a real client — the root Stat
	// should not make any GCS calls.
	fsys := &gcsFS{
		ctx:        context.Background(),
		bucketName: "b",
	}
	f := &gcsFile{fs: fsys, name: "", fullPath: ""}
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("root Stat() error: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("root should be a directory")
	}
}

// ---------- Stat: negative cache hit ----------

func TestFileStatNegativeCacheHit(t *testing.T) {
	cache := newAttrCache(time.Minute, 100)
	defer cache.stop()
	cache.setNotFound("missing.txt")

	fsys := &gcsFS{cache: cache}
	f := &gcsFile{fs: fsys, name: "missing.txt", fullPath: "missing.txt"}

	_, err := f.Stat()
	if err == nil {
		t.Fatal("expected error for negative cache hit")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

// ---------- cacheNotFound: with and without cache ----------

func TestCacheNotFoundWithCache(t *testing.T) {
	cache := newAttrCache(time.Minute, 100)
	defer cache.stop()

	fsys := &gcsFS{cache: cache}
	f := &gcsFile{fs: fsys, name: "test.txt", fullPath: "test.txt"}

	f.cacheNotFound()

	attrs, ok := cache.get("test.txt")
	if !ok || !isNegative(attrs) {
		t.Fatal("expected negative cache entry after cacheNotFound")
	}
}

func TestCacheNotFoundWithoutCache(t *testing.T) {
	fsys := &gcsFS{cache: nil}
	f := &gcsFile{fs: fsys, name: "test.txt", fullPath: "test.txt"}
	// Should not panic
	f.cacheNotFound()
}

// ---------- Root stat with cache ----------

func TestRootStatWithCache(t *testing.T) {
	cache := newAttrCache(time.Minute, 100)
	defer cache.stop()

	fsys := &gcsFS{
		ctx:        context.Background(),
		bucketName: "b",
		cache:      cache,
	}
	f := &gcsFile{fs: fsys, name: "", fullPath: ""}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("root Stat() error: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("root should be a directory")
	}

	// Verify it's cached
	attrs, ok := cache.get("")
	if !ok {
		t.Fatal("root attrs should be cached")
	}
	if attrs.Prefix != "/" {
		t.Fatalf("expected Prefix = '/', got %q", attrs.Prefix)
	}
}

// ---------- Stat: file extension skips directory check ----------

func TestStatFileExtSkipsDirCheck(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fsys := newGCSFS(ctx, client, "nonexistent-bucket")
	fsys.cache = newAttrCache(time.Minute, 100)
	defer fsys.cache.stop()

	// Stat a path with file extension — should return an error
	// (either ErrNotExist or timeout depending on emulator availability)
	f := &gcsFile{fs: fsys, name: "style.css", fullPath: "style.css"}
	_, err := f.Stat()
	if err == nil {
		t.Fatal("expected error for nonexistent .css path")
	}
}

// ---------- Read: sniff buffer path ----------

func TestReadFromSniffBuffer(t *testing.T) {
	data := []byte("hello world sniff buffer test")
	fsys := &gcsFS{}
	f := &gcsFile{
		fs:       fsys,
		name:     "file.txt",
		sniffBuf: data,
		sniffLen: len(data),
		sniffPos: 0,
		pos:      0,
	}

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("Read() = %d, %q; want 5, hello", n, buf)
	}
	if f.sniffPos != 5 || f.pos != 5 {
		t.Fatalf("sniffPos=%d, pos=%d, want both 5", f.sniffPos, f.pos)
	}
}

// ---------- Seek: small forward skip optimization ----------

func TestSeekSmallForwardSkip(t *testing.T) {
	// Set up a file with a reader that has remaining data.
	data := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &dataReader{data: data, offset: 0},
		pos:    0,
	}

	// Small forward seek (within seekSkipThreshold) should discard bytes.
	pos, err := f.Seek(5, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek() error: %v", err)
	}
	if pos != 5 {
		t.Fatalf("pos = %d, want 5", pos)
	}
	// Reader should still be open (not nil).
	if f.reader == nil {
		t.Fatal("reader should not be closed for small forward skip")
	}
}

func TestSeekLargeForwardClosesReader(t *testing.T) {
	// A large seek (beyond seekSkipThreshold) should close reader.
	data := make([]byte, 100)
	f := &gcsFile{
		fs:        &gcsFS{},
		name:      "test.txt",
		reader:    &dataReader{data: data, offset: 0},
		pos:       0,
		size:      100000,
		sizeKnown: true,
	}

	// Seek beyond threshold.
	pos, err := f.Seek(50000, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error: %v", err)
	}
	if pos != 50000 {
		t.Fatalf("pos = %d, want 50000", pos)
	}
	// Reader should be closed.
	if f.reader != nil {
		t.Fatal("reader should be closed for large forward seek")
	}
}

func TestSeekBackwardClosesReader(t *testing.T) {
	f := &gcsFile{
		fs:     &gcsFS{},
		name:   "test.txt",
		reader: &dataReader{data: []byte("abc"), offset: 0},
		pos:    10,
	}

	// Backward seek should always close reader.
	pos, err := f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error: %v", err)
	}
	if pos != 0 {
		t.Fatalf("pos = %d, want 0", pos)
	}
	if f.reader != nil {
		t.Fatal("reader should be closed for backward seek")
	}
}

func TestSeekIntoSniffBufferClosesDesyncedReader(t *testing.T) {
	// Simulate: sniff buffer captured first 512 bytes, but subsequent
	// reads advanced the underlying reader past sniffLen. Seeking back
	// into the sniff buffer should close the desynchronized reader so
	// that future reads beyond the sniff buffer reopen at the correct
	// GCS offset.
	closed := false
	f := &gcsFile{
		fs:       &gcsFS{},
		name:     "test.txt",
		sniffBuf: make([]byte, 512),
		sniffLen: 512,
		sniffPos: 512,
		pos:      1024, // reads advanced past sniffLen
		reader: closerFunc(func() error {
			closed = true
			return nil
		}),
	}

	pos, err := f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error: %v", err)
	}
	if pos != 0 {
		t.Fatalf("pos = %d, want 0", pos)
	}
	if f.sniffPos != 0 {
		t.Fatalf("sniffPos = %d, want 0", f.sniffPos)
	}
	if !closed {
		t.Error("desynchronized reader was not closed")
	}
	if f.reader != nil {
		t.Error("reader should be nil after closing desynchronized reader")
	}
}

func TestSeekIntoSniffBufferKeepsSyncedReader(t *testing.T) {
	// When the reader has NOT advanced past sniffLen, seeking into the
	// sniff buffer should keep the reader open (no pointless close).
	closed := false
	f := &gcsFile{
		fs:       &gcsFS{},
		name:     "test.txt",
		sniffBuf: make([]byte, 512),
		sniffLen: 512,
		sniffPos: 256,
		pos:      256, // still within sniff range
		reader: closerFunc(func() error {
			closed = true
			return nil
		}),
	}

	pos, err := f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error: %v", err)
	}
	if pos != 0 {
		t.Fatalf("pos = %d, want 0", pos)
	}
	if closed {
		t.Error("synced reader should NOT be closed")
	}
	if f.reader == nil {
		t.Error("reader should still be open")
	}
}

func TestOpCtxUsesDefaultTimeout(t *testing.T) {
	fsys := &gcsFS{ctx: context.Background()}
	ctx, cancel := fsys.opCtx()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}
	remaining := time.Until(deadline)
	// Should be close to defaultOpTimeout (60s).
	if remaining < 50*time.Second || remaining > 65*time.Second {
		t.Fatalf("deadline %v not near defaultOpTimeout", remaining)
	}
}

func TestOpCtxUsesCustomTimeout(t *testing.T) {
	fsys := &gcsFS{ctx: context.Background(), opTimeout: 5 * time.Second}
	ctx, cancel := fsys.opCtx()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 3*time.Second || remaining > 6*time.Second {
		t.Fatalf("deadline %v not near custom timeout", remaining)
	}
}
