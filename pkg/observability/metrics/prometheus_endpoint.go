package observability

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	pkg "github.com/spuranam/caddy-fs-gcs/pkg"
)

// PrometheusEndpointHandler serves Prometheus metrics with OpenTelemetry integration.
//
// Constants for metrics endpoint configuration.
const (
	defaultMetricsMaxInFlight = 10
	defaultMetricsTimeout     = 30 * time.Second
)

type PrometheusEndpointHandler struct {
	// Configuration
	MetricsPath  string            `json:"metrics_path,omitempty"`
	EnableHealth bool              `json:"enable_health,omitempty"`
	EnableDebug  bool              `json:"enable_debug,omitempty"`
	CustomLabels map[string]string `json:"custom_labels,omitempty"`

	// DebugLocalOnly restricts /debug/metrics to loopback addresses.
	// Defaults to true when nil.
	DebugLocalOnly *bool `json:"debug_local_only,omitempty"`

	// Internal state
	startTime    time.Time
	requestCount atomic.Int64
}

// Interface guards — compile-time verification that PrometheusEndpointHandler
// satisfies all expected Caddy interfaces.
var (
	_ caddy.Module                = (*PrometheusEndpointHandler)(nil)
	_ caddy.Provisioner           = (*PrometheusEndpointHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*PrometheusEndpointHandler)(nil)
	_ caddyfile.Unmarshaler       = (*PrometheusEndpointHandler)(nil)
)

// CaddyModule returns the Caddy module information
func (*PrometheusEndpointHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.prometheus",
		New: func() caddy.Module { return new(PrometheusEndpointHandler) },
	}
}

// Provision sets up the Prometheus endpoint handler.
func (p *PrometheusEndpointHandler) Provision(_ caddy.Context) error {
	if p.MetricsPath == "" {
		p.MetricsPath = "/metrics"
	}
	if p.CustomLabels == nil {
		p.CustomLabels = make(map[string]string)
	}
	if _, ok := p.CustomLabels["service"]; !ok {
		p.CustomLabels["service"] = pkg.ServiceName
	}
	if _, ok := p.CustomLabels["version"]; !ok {
		p.CustomLabels["version"] = pkg.Version
	}
	p.startTime = time.Now()

	return Init()
}

// ServeHTTP handles HTTP requests for Prometheus metrics
func (p *PrometheusEndpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Increment request count atomically — no mutex needed.
	p.requestCount.Add(1)

	// Handle different endpoints
	switch r.URL.Path {
	case p.MetricsPath:
		return p.serveMetrics(w, r)
	case "/health":
		if p.EnableHealth {
			return p.serveHealth(w, r)
		}
	case "/debug/metrics":
		if p.EnableDebug && p.isDebugAllowed(r) {
			return p.serveDebugMetrics(w, r)
		}
	case "/metrics/health":
		return p.serveMetricsHealth(w, r)
	}

	// Pass through to next handler
	return next.ServeHTTP(w, r)
}

// isDebugAllowed returns true when the request is allowed to reach /debug/metrics.
// When DebugLocalOnly is nil (default) or true, only loopback addresses pass.
func (p *PrometheusEndpointHandler) isDebugAllowed(r *http.Request) bool {
	if p.DebugLocalOnly != nil && !*p.DebugLocalOnly {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// serveMetrics serves the main Prometheus metrics endpoint.
func (p *PrometheusEndpointHandler) serveMetrics(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	handler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		EnableOpenMetrics:   true,
		MaxRequestsInFlight: defaultMetricsMaxInFlight,
		Timeout:             defaultMetricsTimeout,
	})

	// Serve metrics
	handler.ServeHTTP(w, r)
	return nil
}

// serveHealth serves a health check endpoint
func (p *PrometheusEndpointHandler) serveHealth(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")

	health := map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(p.startTime).String(),
		"requests":  p.requestCount.Load(),
		"service":   pkg.ServiceName,
	}

	// Only include custom labels for loopback clients to avoid
	// leaking internal infrastructure details.
	if p.isDebugAllowed(r) {
		for key, value := range p.CustomLabels {
			health[key] = value
		}
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		return fmt.Errorf("encode health response: %w", err)
	}
	return nil
}

// serveDebugMetrics serves debug information about metrics
func (p *PrometheusEndpointHandler) serveDebugMetrics(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Content-Type", "application/json")

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return fmt.Errorf("gather debug metrics: %w", err)
	}

	debug := map[string]any{
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"metric_count":  len(metricFamilies),
		"uptime":        time.Since(p.startTime).String(),
		"request_count": p.requestCount.Load(),
		"custom_labels": p.CustomLabels,
		"metrics":       make([]map[string]any, 0, len(metricFamilies)),
	}

	// Add metric information
	for _, mf := range metricFamilies {
		metricInfo := map[string]any{
			"name":  mf.GetName(),
			"help":  mf.GetHelp(),
			"type":  mf.GetType().String(),
			"count": len(mf.GetMetric()),
		}
		debug["metrics"] = append(debug["metrics"].([]map[string]any), metricInfo)
	}

	return json.NewEncoder(w).Encode(debug)
}

// serveMetricsHealth serves a health check specifically for metrics
func (p *PrometheusEndpointHandler) serveMetricsHealth(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")

	_, gatherErr := prometheus.DefaultGatherer.Gather()
	status := "healthy"
	errMsg := ""
	if gatherErr != nil {
		status = "unhealthy"
		// Redact internal error details for non-loopback clients.
		if p.isDebugAllowed(r) {
			errMsg = gatherErr.Error()
		} else {
			errMsg = "internal error (details redacted)"
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	health := map[string]any{
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   "prometheus-metrics",
	}
	if errMsg != "" {
		health["error"] = errMsg
	}

	if encErr := json.NewEncoder(w).Encode(health); encErr != nil {
		return fmt.Errorf("encode metrics health: %w", encErr)
	}
	return nil
}

// UnmarshalCaddyfile parses Caddyfile syntax
func (p *PrometheusEndpointHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		// Parse arguments
		args := d.RemainingArgs()
		if len(args) > 0 {
			p.MetricsPath = args[0]
		}

		// Parse blocks
		for d.NextBlock(0) {
			switch d.Val() {
			case "path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				p.MetricsPath = d.Val()

			case "enable_health":
				p.EnableHealth = true

			case "enable_debug":
				p.EnableDebug = true

			case "debug_local_only":
				if !d.NextArg() {
					return d.ArgErr()
				}
				val := d.Val()
				switch val {
				case "true":
					p.DebugLocalOnly = new(true)
				case "false":
					p.DebugLocalOnly = new(false)
				default:
					return d.Errf("debug_local_only must be 'true' or 'false', got '%s'", val)
				}

			case "label":
				if !d.NextArg() {
					return d.ArgErr()
				}
				key := d.Val()
				if !d.NextArg() {
					return d.ArgErr()
				}
				value := d.Val()
				if p.CustomLabels == nil {
					p.CustomLabels = make(map[string]string)
				}
				p.CustomLabels[key] = value

			default:
				return d.Errf("unrecognized prometheus directive: %s", d.Val())
			}
		}
	}

	return nil
}

// ParseCaddyfilePrometheus is the Caddyfile adapter for the prometheus directive.
func ParseCaddyfilePrometheus(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var p PrometheusEndpointHandler
	if err := p.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &p, nil
}
