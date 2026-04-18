// Package observability provides Prometheus/OpenTelemetry metrics collection for
// the Caddy GCS Proxy plugin.
//
// It includes an HTTP middleware handler that records request duration, count,
// error rate, and response size, as well as GCS-specific operation metrics such
// as operation duration, cache hit/miss ratios, and streaming throughput. A
// dedicated Prometheus endpoint handler is also provided.
package observability
