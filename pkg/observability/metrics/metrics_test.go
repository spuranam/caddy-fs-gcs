package observability

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/assert"
)

func TestMetricsHandler_Basic(t *testing.T) {
	t.Parallel()
	t.Run("create metrics handler", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{
			MetricsPath: "/metrics",
		}
		assert.NotNil(t, handler)
		assert.Equal(t, "/metrics", handler.MetricsPath)
	})

	t.Run("caddy module info", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		moduleInfo := handler.CaddyModule()

		assert.NotNil(t, moduleInfo)
		assert.Equal(t, caddy.ModuleID("http.handlers.gcs_metrics"), moduleInfo.ID)
		assert.NotNil(t, moduleInfo.New)

		newHandler := moduleInfo.New()
		assert.NotNil(t, newHandler)
		assert.IsType(t, &MetricsHandler{}, newHandler)
	})

	t.Run("provision with defaults", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		err := handler.Provision(caddy.Context{})
		assert.NoError(t, err)
		assert.Equal(t, "/metrics", handler.MetricsPath)
	})

	t.Run("provision with custom path", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{MetricsPath: "/custom-metrics"}
		err := handler.Provision(caddy.Context{})
		assert.NoError(t, err)
		assert.Equal(t, "/custom-metrics", handler.MetricsPath)
	})
}

func TestMetricsHandler_ServeHTTP(t *testing.T) {
	t.Parallel()
	t.Run("serve http request with metrics", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		handler.Provision(caddy.Context{})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test response"))
			return nil
		})

		err := handler.ServeHTTP(w, req, next)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "test response", w.Body.String())
	})

	t.Run("serve http request with error", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		handler.Provision(caddy.Context{})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			w.WriteHeader(http.StatusInternalServerError)
			return assert.AnError
		})

		err := handler.ServeHTTP(w, req, next)
		assert.Error(t, err)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("serve http request with large content", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		handler.Provision(caddy.Context{})

		req := httptest.NewRequest("POST", "/test", nil)
		req.ContentLength = 1024
		w := httptest.NewRecorder()

		next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("large response"))
			return nil
		})

		err := handler.ServeHTTP(w, req, next)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRecordingHelpers(t *testing.T) {
	t.Parallel()
	// Ensure Init has been called so instruments are non-nil
	_ = Init()

	ctx := context.Background()

	t.Run("RecordHTTPRequest", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordHTTPRequest(ctx, "GET", "/test", 200, 0, 0)
		})
	})

	t.Run("RecordHTTPRequest error status", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordHTTPRequest(ctx, "GET", "/fail", 500, 0, 0)
		})
	})

	t.Run("RecordGCSOperation", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordGCSOperation(ctx, "get", "bucket", "ok", 0)
		})
	})

	t.Run("RecordGCSError", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordGCSError(ctx, "get", "bucket", "not_found")
		})
	})

	t.Run("RecordCacheHit", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordCacheHit(ctx, "bucket", "memory")
		})
	})

	t.Run("RecordCacheMiss", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordCacheMiss(ctx, "bucket", "memory")
		})
	})

	t.Run("RecordStreamingBytes", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			RecordStreamingBytes(ctx, "bucket", 1024)
		})
	})

	t.Run("IncConcurrentRequests", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			IncConcurrentRequests(ctx, "bucket")
		})
	})

	t.Run("DecConcurrentRequests", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			DecConcurrentRequests(ctx, "bucket")
		})
	})
}

func TestResponseWriter(t *testing.T) {
	t.Parallel()
	t.Run("captures status code", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}
		w.WriteHeader(http.StatusNotFound)
		assert.Equal(t, http.StatusNotFound, w.statusCode)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("captures response size", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}

		n, err := w.Write([]byte("hello"))
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, int64(5), w.size)
		assert.Equal(t, http.StatusOK, w.statusCode)
	})

	t.Run("defaults to 200 on write", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}

		w.Write([]byte("data"))
		assert.Equal(t, http.StatusOK, w.statusCode)
	})

	t.Run("write multiple times", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}

		data1 := []byte("first")
		data2 := []byte("second")

		n1, err1 := w.Write(data1)
		n2, err2 := w.Write(data2)

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, len(data1), n1)
		assert.Equal(t, len(data2), n2)
		assert.Equal(t, int64(len(data1)+len(data2)), w.size)
	})

	t.Run("write after write header", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}

		w.WriteHeader(http.StatusAccepted)
		data := []byte("test")
		n, err := w.Write(data)

		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, http.StatusAccepted, w.statusCode)
		assert.Equal(t, int64(len(data)), w.size)
	})

	t.Run("flush delegates", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}
		w.Flush() // should not panic
	})

	t.Run("hijack unsupported", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rec}
		_, _, err := w.Hijack()
		assert.Error(t, err)
	})
}

func TestMetricsHandler_Concurrency(t *testing.T) {
	t.Parallel()
	t.Run("concurrent requests", func(t *testing.T) {
		t.Parallel()
		handler := &MetricsHandler{}
		handler.Provision(caddy.Context{})

		const numRequests = 10
		done := make(chan bool, numRequests)

		for range numRequests {
			go func() {
				req := httptest.NewRequest("GET", "/test", nil)
				w := httptest.NewRecorder()

				next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("response"))
					return nil
				})

				err := handler.ServeHTTP(w, req, next)
				assert.NoError(t, err)
				done <- true
			}()
		}

		for range numRequests {
			<-done
		}
	})
}

func TestMetricsHandler_UnmarshalCaddyfile(t *testing.T) {
	t.Parallel()

	t.Run("empty directive", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`gcs_metrics`)
		var m MetricsHandler
		err := m.UnmarshalCaddyfile(d)
		assert.NoError(t, err)
		assert.Empty(t, m.MetricsPath) // no default set by Unmarshal
	})

	t.Run("inline path arg", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`gcs_metrics /my-metrics`)
		var m MetricsHandler
		err := m.UnmarshalCaddyfile(d)
		assert.NoError(t, err)
		assert.Equal(t, "/my-metrics", m.MetricsPath)
	})

	t.Run("block path directive", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser("gcs_metrics {\n\tpath /prom\n}")
		var m MetricsHandler
		err := m.UnmarshalCaddyfile(d)
		assert.NoError(t, err)
		assert.Equal(t, "/prom", m.MetricsPath)
	})

	t.Run("block path missing arg", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser("gcs_metrics {\n\tpath\n}")
		var m MetricsHandler
		err := m.UnmarshalCaddyfile(d)
		assert.Error(t, err)
	})

	t.Run("unknown directive", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser("gcs_metrics {\n\tunknown val\n}")
		var m MetricsHandler
		err := m.UnmarshalCaddyfile(d)
		assert.Error(t, err)
	})
}

// --- responseWriter ReadFrom coverage ---

func TestResponseWriterReadFrom(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: rec}
	body := "hello world"
	n, err := w.ReadFrom(strings.NewReader(body))
	assert.NoError(t, err)
	assert.Equal(t, int64(len(body)), n)
	assert.Equal(t, int64(len(body)), w.size)
	assert.Equal(t, body, rec.Body.String())
}

// --- Hijack non-hijackable ---

func TestResponseWriterHijack_NotSupported(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: rec}
	_, _, err := w.Hijack()
	assert.ErrorIs(t, err, http.ErrNotSupported)
}

// --- ReadFrom fallback (non-ReaderFrom ResponseWriter) ---

type plainWriter struct {
	buf []byte
}

func (w *plainWriter) Header() http.Header         { return http.Header{} }
func (w *plainWriter) WriteHeader(int)             {}
func (w *plainWriter) Write(b []byte) (int, error) { w.buf = append(w.buf, b...); return len(b), nil }

func TestResponseWriterReadFrom_Fallback(t *testing.T) {
	t.Parallel()
	pw := &plainWriter{}
	w := &responseWriter{ResponseWriter: pw}
	n, err := w.ReadFrom(strings.NewReader("fallback data"))
	assert.NoError(t, err)
	assert.Equal(t, int64(13), n)
	assert.Equal(t, int64(13), w.size)
	assert.Equal(t, "fallback data", string(pw.buf))
}

// --- Write without WriteHeader ---

func TestResponseWriterWrite_ImplicitOK(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: rec}
	_, err := w.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, w.statusCode)
	assert.Equal(t, int64(5), w.size)
}

// --- Flush non-flushable ---

type nonFlushWriter struct {
	http.ResponseWriter
}

func TestResponseWriterFlush_NotFlushable(t *testing.T) {
	t.Parallel()
	w := &responseWriter{ResponseWriter: &nonFlushWriter{}}
	// Should not panic
	w.Flush()
}

// --- Hijack with hijackable writer ---

type hijackableWriter struct {
	http.ResponseWriter
}

func (hw *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func TestResponseWriterHijack_Supported(t *testing.T) {
	t.Parallel()
	hw := &hijackableWriter{ResponseWriter: httptest.NewRecorder()}
	w := &responseWriter{ResponseWriter: hw}
	conn, rw, err := w.Hijack()
	assert.NoError(t, err)
	assert.Nil(t, conn)
	assert.Nil(t, rw)
}

// --- ReadFrom with io.ReaderFrom ---

type readerFromWriter struct {
	http.ResponseWriter
	n int64
}

func (w *readerFromWriter) ReadFrom(r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	w.n = int64(len(data))
	return w.n, err
}

func TestResponseWriterReadFrom_WithReaderFrom(t *testing.T) {
	t.Parallel()
	rfw := &readerFromWriter{ResponseWriter: httptest.NewRecorder()}
	w := &responseWriter{ResponseWriter: rfw}
	n, err := w.ReadFrom(strings.NewReader("readfrom test"))
	assert.NoError(t, err)
	assert.Equal(t, int64(13), n)
	assert.Equal(t, int64(13), w.size)
	assert.Equal(t, int64(13), rfw.n)
}

// --- ParseCaddyfileMetrics ---

func TestParseCaddyfileMetrics(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`gcs_metrics /prom`)
		h := httpcaddyfile.Helper{Dispenser: d}
		handler, err := ParseCaddyfileMetrics(h)
		assert.NoError(t, err)
		assert.NotNil(t, handler)
		assert.IsType(t, &MetricsHandler{}, handler)
	})

	t.Run("error path", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser("gcs_metrics {\n\tunknown val\n}")
		h := httpcaddyfile.Helper{Dispenser: d}
		handler, err := ParseCaddyfileMetrics(h)
		assert.Error(t, err)
		assert.Nil(t, handler)
	})
}

// --- Record helpers with nil instruments ---

func TestRecordingHelpers_NilInstruments(t *testing.T) {
	// NOT parallel — mutates package-level globals.

	// Save originals.
	origTotal := httpRequestTotal
	origDuration := httpRequestDuration
	origErrors := httpRequestErrors
	origRespSize := httpResponseSize
	origOpDuration := gcsOperationDuration
	origOpsTotal := gcsOperationsTotal
	origOpErrors := gcsOperationErrors
	origCacheHits := gcsCacheHits
	origCacheMisses := gcsCacheMisses
	origStreamingBytes := gcsStreamingBytes
	origConcurrentReqs := gcsConcurrentReqs

	// Set all to nil.
	httpRequestTotal = nil
	httpRequestDuration = nil
	httpRequestErrors = nil
	httpResponseSize = nil
	gcsOperationDuration = nil
	gcsOperationsTotal = nil
	gcsOperationErrors = nil
	gcsCacheHits = nil
	gcsCacheMisses = nil
	gcsStreamingBytes = nil
	gcsConcurrentReqs = nil

	defer func() {
		httpRequestTotal = origTotal
		httpRequestDuration = origDuration
		httpRequestErrors = origErrors
		httpResponseSize = origRespSize
		gcsOperationDuration = origOpDuration
		gcsOperationsTotal = origOpsTotal
		gcsOperationErrors = origOpErrors
		gcsCacheHits = origCacheHits
		gcsCacheMisses = origCacheMisses
		gcsStreamingBytes = origStreamingBytes
		gcsConcurrentReqs = origConcurrentReqs
	}()

	ctx := context.Background()

	assert.NotPanics(t, func() { RecordHTTPRequest(ctx, "GET", "/", 200, 0, 0) })
	assert.NotPanics(t, func() { RecordGCSOperation(ctx, "get", "b", "ok", 0) })
	assert.NotPanics(t, func() { RecordGCSError(ctx, "get", "b", "err") })
	assert.NotPanics(t, func() { RecordCacheHit(ctx, "b", "mem") })
	assert.NotPanics(t, func() { RecordCacheMiss(ctx, "b", "mem") })
	assert.NotPanics(t, func() { RecordStreamingBytes(ctx, "b", 100) })
	assert.NotPanics(t, func() { IncConcurrentRequests(ctx, "b") })
	assert.NotPanics(t, func() { DecConcurrentRequests(ctx, "b") })
}

// --- NormalizeRoute ---

func TestNormalizeRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty", "", "/"},
		{"root", "/", "/"},
		{"single segment file", "/robots.txt", "/robots.txt"},
		{"two segments", "/css/style.css", "/css/"},
		{"deep path", "/docs/api/v2/users", "/docs/"},
		{"attacker path", "/random/abc123/../../etc/passwd", "/random/"},
		{"trailing slash", "/assets/", "/assets/"},
		{"no leading slash", "foo/bar", "/foo/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeRoute(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShutdown(t *testing.T) {
	// Shutdown on a fresh state should be a no-op.
	err := Shutdown(context.Background())
	assert.NoError(t, err)

	// Init → Shutdown → re-Init cycle should succeed.
	err = Init()
	assert.NoError(t, err)

	err = Shutdown(context.Background())
	assert.NoError(t, err)

	// Re-init should work after shutdown (sync.Once was reset).
	err = Init()
	assert.NoError(t, err)
}
