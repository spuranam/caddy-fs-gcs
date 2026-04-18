package observability

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	metricsMu     sync.Mutex
	initOnce      sync.Once
	meterProvider *sdkmetric.MeterProvider

	// HTTP request instruments
	httpRequestDuration metric.Float64Histogram
	httpRequestTotal    metric.Int64Counter
	httpRequestErrors   metric.Int64Counter
	httpResponseSize    metric.Float64Histogram

	// GCS operation instruments
	gcsOperationDuration metric.Float64Histogram
	gcsOperationsTotal   metric.Int64Counter
	gcsOperationErrors   metric.Int64Counter
	gcsCacheHits         metric.Int64Counter
	gcsCacheMisses       metric.Int64Counter
	gcsStreamingBytes    metric.Int64Counter
	gcsConcurrentReqs    metric.Int64UpDownCounter
)

// Init initializes the OpenTelemetry metrics system with a Prometheus exporter.
// Safe to call multiple times; only the first call takes effect.
func Init() error {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	var initErr error
	initOnce.Do(func() {
		exporter, err := otelprom.New()
		if err != nil {
			initErr = fmt.Errorf("create prometheus exporter: %w", err)
			// Reset so the next Init() call retries instead of
			// silently succeeding with nil instruments.
			initOnce = sync.Once{}
			return
		}

		meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
		otel.SetMeterProvider(meterProvider)

		initErr = createInstruments(meterProvider.Meter("caddy-fs-gcs"))
	})
	return initErr
}

// Shutdown shuts down the meter provider and resets the initialization state
// so that Init() can be called again. Intended for test cleanup.
func Shutdown(ctx context.Context) error {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	if meterProvider != nil {
		if err := meterProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown meter provider: %w", err)
		}
		meterProvider = nil
	}
	initOnce = sync.Once{}
	return nil
}

func createInstruments(m metric.Meter) error {
	var err error

	httpRequestDuration, err = m.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests"),
	)
	if err != nil {
		return fmt.Errorf("create http.server.request.duration: %w", err)
	}

	httpRequestTotal, err = m.Int64Counter("http.server.request.total",
		metric.WithDescription("Total HTTP server requests"),
	)
	if err != nil {
		return fmt.Errorf("create http.server.request.total: %w", err)
	}

	httpRequestErrors, err = m.Int64Counter("http.server.request.errors",
		metric.WithDescription("Total HTTP server request errors"),
	)
	if err != nil {
		return fmt.Errorf("create http.server.request.errors: %w", err)
	}

	httpResponseSize, err = m.Float64Histogram("http.server.response.size",
		metric.WithUnit("By"),
		metric.WithDescription("Size of HTTP server responses"),
	)
	if err != nil {
		return fmt.Errorf("create http.server.response.size: %w", err)
	}

	gcsOperationDuration, err = m.Float64Histogram("gcs.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of GCS operations"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.operation.duration: %w", err)
	}

	gcsOperationsTotal, err = m.Int64Counter("gcs.operation.total",
		metric.WithDescription("Total GCS operations"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.operation.total: %w", err)
	}

	gcsOperationErrors, err = m.Int64Counter("gcs.operation.errors",
		metric.WithDescription("Total GCS operation errors"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.operation.errors: %w", err)
	}

	gcsCacheHits, err = m.Int64Counter("gcs.cache.hits",
		metric.WithDescription("Total GCS cache hits"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.cache.hits: %w", err)
	}

	gcsCacheMisses, err = m.Int64Counter("gcs.cache.misses",
		metric.WithDescription("Total GCS cache misses"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.cache.misses: %w", err)
	}

	gcsStreamingBytes, err = m.Int64Counter("gcs.streaming.bytes",
		metric.WithUnit("By"),
		metric.WithDescription("Total bytes streamed from GCS"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.streaming.bytes: %w", err)
	}

	gcsConcurrentReqs, err = m.Int64UpDownCounter("gcs.concurrent.requests",
		metric.WithDescription("Current number of concurrent GCS requests"),
	)
	if err != nil {
		return fmt.Errorf("create gcs.concurrent.requests: %w", err)
	}

	return nil
}

// --- Recording helpers (nil-safe) ---

// NormalizeRoute reduces a raw URL path to a bounded-cardinality route label
// suitable for use as a metric attribute. It keeps only the first path segment
// (e.g. "/css/style.css" → "/css/") so that attacker-crafted URLs cannot
// explode Prometheus cardinality.
func NormalizeRoute(rawPath string) string {
	if rawPath == "" || rawPath == "/" {
		return "/"
	}
	// Trim leading slash, find the first segment.
	trimmed := strings.TrimPrefix(rawPath, "/")
	if before, _, ok := strings.Cut(trimmed, "/"); ok {
		return "/" + before + "/"
	}
	// Single-segment path (e.g. "/robots.txt").
	return "/" + trimmed
}

// RecordHTTPRequest records HTTP request metrics.
func RecordHTTPRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, respSize int64) {
	if httpRequestTotal == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("http.method", method),
		attribute.String("http.route", NormalizeRoute(path)),
		attribute.Int("http.status_code", statusCode),
	)
	httpRequestTotal.Add(ctx, 1, attrs)
	httpRequestDuration.Record(ctx, duration.Seconds(), attrs)
	httpResponseSize.Record(ctx, float64(respSize), attrs)
	if statusCode >= 400 {
		httpRequestErrors.Add(ctx, 1, attrs)
	}
}

// RecordGCSOperation records a GCS operation.
func RecordGCSOperation(ctx context.Context, operation, bucket, status string, duration time.Duration) {
	if gcsOperationsTotal == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("gcs.operation", operation),
		attribute.String("gcs.bucket", bucket),
		attribute.String("gcs.status", status),
	)
	gcsOperationsTotal.Add(ctx, 1, attrs)
	gcsOperationDuration.Record(ctx, duration.Seconds(), attrs)
}

// RecordGCSError records a GCS operation error.
func RecordGCSError(ctx context.Context, operation, bucket, errorType string) {
	if gcsOperationErrors == nil {
		return
	}
	gcsOperationErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gcs.operation", operation),
		attribute.String("gcs.bucket", bucket),
		attribute.String("gcs.error_type", errorType),
	))
}

// RecordCacheHit records a cache hit.
func RecordCacheHit(ctx context.Context, bucket, cacheType string) {
	if gcsCacheHits == nil {
		return
	}
	gcsCacheHits.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gcs.bucket", bucket),
		attribute.String("cache.type", cacheType),
	))
}

// RecordCacheMiss records a cache miss.
func RecordCacheMiss(ctx context.Context, bucket, cacheType string) {
	if gcsCacheMisses == nil {
		return
	}
	gcsCacheMisses.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gcs.bucket", bucket),
		attribute.String("cache.type", cacheType),
	))
}

// RecordStreamingBytes records streaming bytes.
func RecordStreamingBytes(ctx context.Context, bucket string, bytes int64) {
	if gcsStreamingBytes == nil {
		return
	}
	gcsStreamingBytes.Add(ctx, bytes, metric.WithAttributes(
		attribute.String("gcs.bucket", bucket),
	))
}

// IncConcurrentRequests increments the concurrent GCS request gauge.
func IncConcurrentRequests(ctx context.Context, bucket string) {
	if gcsConcurrentReqs == nil {
		return
	}
	gcsConcurrentReqs.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gcs.bucket", bucket),
	))
}

// DecConcurrentRequests decrements the concurrent GCS request gauge.
func DecConcurrentRequests(ctx context.Context, bucket string) {
	if gcsConcurrentReqs == nil {
		return
	}
	gcsConcurrentReqs.Add(ctx, -1, metric.WithAttributes(
		attribute.String("gcs.bucket", bucket),
	))
}

// --- Caddy middleware ---

// MetricsHandler is a Caddy middleware that records HTTP request metrics using OpenTelemetry.
type MetricsHandler struct {
	MetricsPath string `json:"metrics_path,omitempty"`
}

// Interface guards — compile-time verification that MetricsHandler
// satisfies all expected Caddy interfaces.
var (
	_ caddy.Module                = (*MetricsHandler)(nil)
	_ caddy.Provisioner           = (*MetricsHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*MetricsHandler)(nil)
	_ caddyfile.Unmarshaler       = (*MetricsHandler)(nil)
)

// CaddyModule returns the Caddy module information.
func (MetricsHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.gcs_metrics",
		New: func() caddy.Module { return new(MetricsHandler) },
	}
}

// Provision sets up the metrics handler.
func (m *MetricsHandler) Provision(_ caddy.Context) error {
	if m.MetricsPath == "" {
		m.MetricsPath = "/metrics"
	}
	return Init()
}

// responseWriterPool avoids per-request heap allocation of responseWriter.
var responseWriterPool = sync.Pool{
	New: func() any { return &responseWriter{} },
}

// ServeHTTP records HTTP request metrics and passes the request to the next handler.
func (m *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	start := time.Now()

	wrapped := responseWriterPool.Get().(*responseWriter)
	wrapped.ResponseWriter = w
	wrapped.statusCode = 0
	wrapped.size = 0

	defer func() {
		wrapped.ResponseWriter = nil // release reference before returning to pool
		responseWriterPool.Put(wrapped)
	}()

	err := next.ServeHTTP(wrapped, r)

	// Ensure a valid status code is recorded:
	//  - 0 with nil error → implicit 200 (Go default)
	//  - 0 with non-nil error → 500 (Caddy error-handling convention)
	if wrapped.statusCode == 0 {
		if err != nil {
			wrapped.statusCode = http.StatusInternalServerError
		} else {
			wrapped.statusCode = http.StatusOK
		}
	}

	RecordHTTPRequest(r.Context(), r.Method, r.URL.Path, wrapped.statusCode, time.Since(start), wrapped.size)

	return err
}

// UnmarshalCaddyfile parses Caddyfile syntax for gcs_metrics.
//
//	gcs_metrics [<path>] {
//	    path <metrics_path>
//	}
func (m *MetricsHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		args := d.RemainingArgs()
		if len(args) > 0 {
			m.MetricsPath = args[0]
		}
		for d.NextBlock(0) {
			switch d.Val() {
			case "path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.MetricsPath = d.Val()
			default:
				return d.Errf("unrecognized gcs_metrics directive: %s", d.Val())
			}
		}
	}
	return nil
}

// ParseCaddyfileMetrics is the Caddyfile adapter for the gcs_metrics directive.
func ParseCaddyfileMetrics(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m MetricsHandler
	if err := m.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &m, nil
}

// responseWriter wraps http.ResponseWriter to capture response information.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int64
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	w.size += int64(len(data))
	return w.ResponseWriter.Write(data)
}

// Unwrap returns the underlying ResponseWriter so that Caddy's middleware
// chain (and http.ResponseController) can discover optional interfaces
// such as http.Pusher on the real writer.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// ReadFrom implements io.ReaderFrom so that the kernel sendfile(2)
// optimization is preserved when a metrics-wrapped ResponseWriter is
// used as the write target.
func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		w.size += n
		return n, err
	}
	// Fallback: manual copy.
	n, err := io.Copy(w.ResponseWriter, r)
	w.size += n
	return n, err
}
