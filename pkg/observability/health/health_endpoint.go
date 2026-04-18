package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	pkg "github.com/spuranam/caddy-fs-gcs/pkg"
	"go.uber.org/zap"
)

// defaultHealthTimeout is the per-handler timeout for health check operations.
const defaultHealthTimeout = 30 * time.Second

// HealthEndpointHandler provides comprehensive health check endpoints
type HealthEndpointHandler struct {
	// Configuration
	HealthPath     string            `json:"health_path,omitempty"`
	ReadinessPath  string            `json:"readiness_path,omitempty"`
	LivenessPath   string            `json:"liveness_path,omitempty"`
	StartupPath    string            `json:"startup_path,omitempty"`
	CustomLabels   map[string]string `json:"custom_labels,omitempty"`
	EnableDetailed bool              `json:"enable_detailed,omitempty"`
	EnableMetrics  bool              `json:"enable_metrics,omitempty"`

	// DetailedLocalOnly restricts detailed and metrics health endpoints to
	// loopback addresses (127.0.0.0/8 and ::1). Defaults to true.
	DetailedLocalOnly *bool `json:"detailed_local_only,omitempty"`

	// Health checkers
	checkers         []HealthChecker
	compositeChecker *CompositeHealthChecker

	// State
	startTime       time.Time
	startupComplete bool
	mu              sync.RWMutex
	logger          *zap.Logger
}

// Interface guards — compile-time verification that HealthEndpointHandler
// satisfies all expected Caddy interfaces.
var (
	_ caddy.Module                = (*HealthEndpointHandler)(nil)
	_ caddy.Provisioner           = (*HealthEndpointHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*HealthEndpointHandler)(nil)
	_ caddyfile.Unmarshaler       = (*HealthEndpointHandler)(nil)
)

// CaddyModule returns the Caddy module information
func (*HealthEndpointHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.health",
		New: func() caddy.Module { return new(HealthEndpointHandler) },
	}
}

// Provision sets up the health endpoint handler
func (h *HealthEndpointHandler) Provision(ctx caddy.Context) error {
	// Set defaults
	if h.HealthPath == "" {
		h.HealthPath = "/health"
	}
	if h.ReadinessPath == "" {
		h.ReadinessPath = "/ready"
	}
	if h.LivenessPath == "" {
		h.LivenessPath = "/live"
	}
	if h.StartupPath == "" {
		h.StartupPath = "/startup"
	}
	if h.CustomLabels == nil {
		h.CustomLabels = make(map[string]string)
	}
	// Default detailed endpoints to local-only access.
	if h.DetailedLocalOnly == nil {
		h.DetailedLocalOnly = new(true)
	}

	// Set default labels only when not already set by user config.
	if _, ok := h.CustomLabels["service"]; !ok {
		h.CustomLabels["service"] = pkg.ServiceName
	}
	if _, ok := h.CustomLabels["version"]; !ok {
		h.CustomLabels["version"] = pkg.Version
	}

	// Validate custom labels to prevent HTTP header injection.
	for key, value := range h.CustomLabels {
		if err := validateHeaderValue(key); err != nil {
			return fmt.Errorf("invalid custom label key %q: %w", key, err)
		}
		if err := validateHeaderValue(value); err != nil {
			return fmt.Errorf("invalid custom label value for %q: %w", key, err)
		}
	}

	// Initialize state
	h.startTime = time.Now()
	h.startupComplete = false
	h.logger = ctx.Logger()

	// Create composite checker if we have checkers
	if len(h.checkers) > 0 {
		h.compositeChecker = NewCompositeHealthChecker(h.checkers, &HealthConfig{
			Timeout:           30 * time.Second,
			RetryAttempts:     3,
			RetryDelay:        5 * time.Second,
			CheckPermissions:  true,
			CheckConnectivity: true,
		})
	}

	// When no health checkers are registered, mark startup complete
	// immediately — there is nothing to verify. When checkers exist,
	// startup remains incomplete until the first /startup probe
	// confirms storage connectivity (see tryCompleteStartup).
	if len(h.checkers) == 0 {
		h.mu.Lock()
		h.startupComplete = true
		h.mu.Unlock()
	}

	return nil
}

// ServeHTTP handles HTTP requests for health endpoints
func (h *HealthEndpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Handle different health endpoints
	switch r.URL.Path {
	case h.HealthPath:
		return h.serveHealth(w, r)
	case h.ReadinessPath:
		return h.serveReadiness(w, r)
	case h.LivenessPath:
		return h.serveLiveness(w, r)
	case h.StartupPath:
		return h.serveStartup(w, r)
	case h.HealthPath + "/detailed":
		if h.EnableDetailed && h.isDetailedAllowed(r) {
			return h.serveDetailedHealth(w, r)
		}
	case h.HealthPath + "/metrics":
		if h.EnableMetrics && h.isDetailedAllowed(r) {
			return h.serveHealthMetrics(w, r)
		}
	}

	// Pass through to next handler
	return next.ServeHTTP(w, r)
}

// isDetailedAllowed returns true when the request is permitted to reach
// the /health/detailed and /health/metrics endpoints. When DetailedLocalOnly
// is enabled, only loopback addresses are allowed.
func (h *HealthEndpointHandler) isDetailedAllowed(r *http.Request) bool {
	// Explicit opt-out: only skip loopback check when explicitly set to false.
	if h.DetailedLocalOnly != nil && !*h.DetailedLocalOnly {
		return true
	}
	// Default (nil) and true both enforce loopback-only.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// serveHealth serves the main health endpoint
func (h *HealthEndpointHandler) serveHealth(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), defaultHealthTimeout)
	defer cancel()

	var status *HealthStatus
	if h.compositeChecker != nil {
		status = h.compositeChecker.Check(ctx)
	} else {
		status = &HealthStatus{
			Status:    "healthy",
			Message:   "Health endpoint is working",
			Timestamp: time.Now(),
			Details: map[string]string{
				"service": "caddy-fs-gcs",
				"uptime":  time.Since(h.startTime).String(),
			},
		}
	}

	return h.writeHealthResponse(w, r, status)
}

// serveReadiness serves the readiness endpoint
func (h *HealthEndpointHandler) serveReadiness(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), defaultHealthTimeout)
	defer cancel()

	// Check if startup is complete
	h.mu.RLock()
	startupComplete := h.startupComplete
	h.mu.RUnlock()

	if !startupComplete {
		status := &HealthStatus{
			Status:    "unhealthy",
			Message:   "Service is still starting up",
			Timestamp: time.Now(),
			Details: map[string]string{
				"startup": "in_progress",
				"uptime":  time.Since(h.startTime).String(),
			},
		}
		return h.writeHealthResponse(w, r, status)
	}

	// Check storage readiness
	var status *HealthStatus
	if h.compositeChecker != nil {
		status = h.compositeChecker.CheckStorage(ctx)
	} else {
		status = &HealthStatus{
			Status:    "healthy",
			Message:   "Service is ready",
			Timestamp: time.Now(),
			Details: map[string]string{
				"readiness": "ready",
				"uptime":    time.Since(h.startTime).String(),
			},
		}
	}

	return h.writeHealthResponse(w, r, status)
}

// serveLiveness serves the liveness endpoint
func (h *HealthEndpointHandler) serveLiveness(w http.ResponseWriter, r *http.Request) error {
	// Liveness check is simple - just check if the service is running
	status := &HealthStatus{
		Status:    "healthy",
		Message:   "Service is alive",
		Timestamp: time.Now(),
		Details: map[string]string{
			"liveness": "alive",
			"uptime":   time.Since(h.startTime).String(),
		},
	}

	return h.writeHealthResponse(w, r, status)
}

// serveStartup serves the startup endpoint.
// When health checkers are registered, startup remains incomplete until
// storage connectivity is verified on the first probe request.
func (h *HealthEndpointHandler) serveStartup(w http.ResponseWriter, r *http.Request) error {
	h.mu.RLock()
	startupComplete := h.startupComplete
	h.mu.RUnlock()

	if !startupComplete {
		startupComplete = h.tryCompleteStartup(r.Context())
	}

	status := &HealthStatus{
		Status:    "healthy",
		Message:   "Startup check",
		Timestamp: time.Now(),
		Details: map[string]string{
			"startup_complete": fmt.Sprintf("%t", startupComplete),
			"uptime":           time.Since(h.startTime).String(),
		},
	}

	if !startupComplete {
		status.Status = "unhealthy"
		status.Message = "Service is still starting up"
	}

	return h.writeHealthResponse(w, r, status)
}

// tryCompleteStartup verifies storage connectivity and marks startup
// complete on success. Returns true if startup is now complete.
func (h *HealthEndpointHandler) tryCompleteStartup(ctx context.Context) bool {
	if h.compositeChecker != nil {
		checkCtx, cancel := context.WithTimeout(ctx, defaultHealthTimeout)
		defer cancel()
		status := h.compositeChecker.CheckStorage(checkCtx)
		if status.Status != "healthy" {
			return false
		}
	}
	h.mu.Lock()
	h.startupComplete = true
	h.mu.Unlock()
	return true
}

// serveDetailedHealth serves detailed health information
func (h *HealthEndpointHandler) serveDetailedHealth(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), defaultHealthTimeout)
	defer cancel()

	detailed := map[string]any{
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"uptime":        time.Since(h.startTime).String(),
		"service":       pkg.ServiceName,
		"version":       pkg.Version,
		"build_time":    pkg.BuildTime,
		"commit":        pkg.Commit,
		"custom_labels": h.CustomLabels,
	}

	// Run individual health checks once each and derive overall status.
	overallHealthy := true
	if h.compositeChecker != nil {
		storage := h.compositeChecker.CheckStorage(ctx)

		overallStatus := "healthy"
		overallMsg := "All health checks passed"
		if storage.Status != "healthy" {
			overallStatus = "unhealthy"
			overallMsg = storage.Message
			overallHealthy = false
		}

		detailed["overall"] = &HealthStatus{
			Status:    overallStatus,
			Message:   overallMsg,
			Timestamp: time.Now(),
		}
		detailed["storage"] = storage
	}

	// Add startup status
	h.mu.RLock()
	detailed["startup_complete"] = h.startupComplete
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if !overallHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	return json.NewEncoder(w).Encode(detailed)
}

// serveHealthMetrics serves health metrics
func (h *HealthEndpointHandler) serveHealthMetrics(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), defaultHealthTimeout)
	defer cancel()

	metrics := map[string]any{
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"uptime_seconds": time.Since(h.startTime).Seconds(),
		"service":        pkg.ServiceName,
	}

	// Run individual checks once to avoid duplicate GCS round-trips.
	if h.compositeChecker != nil {
		storage := h.compositeChecker.CheckStorage(ctx)
		overallStatus := storage.Status
		metrics["health_status"] = map[string]any{
			"status":  overallStatus,
			"healthy": overallStatus == "healthy",
		}
		metrics["storage_status"] = map[string]any{
			"status":  storage.Status,
			"healthy": storage.Status == "healthy",
		}
	}

	// Add startup metrics
	h.mu.RLock()
	metrics["startup_complete"] = h.startupComplete
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(metrics)
}

// writeHealthResponse writes a health response. Raw error details are
// redacted for non-loopback clients to avoid leaking internal GCS
// information.
func (h *HealthEndpointHandler) writeHealthResponse(w http.ResponseWriter, r *http.Request, status *HealthStatus) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Add custom headers
	for key, value := range h.CustomLabels {
		w.Header().Set("X-Health-"+key, value)
	}

	// Add health status header
	w.Header().Set("X-Health-Status", status.Status)

	// Set HTTP status code based on health status
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Redact internal error details for non-loopback clients.
	if status.Error != "" && !h.isLoopback(r) {
		status = &HealthStatus{
			Status:    status.Status,
			Message:   status.Message,
			Timestamp: status.Timestamp,
			Details:   status.Details,
			Error:     "internal error (details redacted)",
		}
	}

	return json.NewEncoder(w).Encode(status)
}

// isLoopback returns true when the request originates from a loopback address.
func (*HealthEndpointHandler) isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateHeaderValue rejects strings containing characters that could
// enable HTTP header injection (CR, LF, NUL).
func validateHeaderValue(s string) error {
	if strings.ContainsAny(s, "\r\n\x00") {
		return fmt.Errorf("contains invalid characters (CR, LF, or NUL)")
	}
	return nil
}

// AddHealthChecker adds a health checker to the handler
func (h *HealthEndpointHandler) AddHealthChecker(checker HealthChecker) {
	if checker == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.checkers = append(h.checkers, checker)

	// Recreate composite checker
	if len(h.checkers) > 0 {
		h.compositeChecker = NewCompositeHealthChecker(h.checkers, &HealthConfig{
			Timeout:           30 * time.Second,
			RetryAttempts:     3,
			RetryDelay:        5 * time.Second,
			CheckPermissions:  true,
			CheckConnectivity: true,
		})
	}
}

// UnmarshalCaddyfile parses Caddyfile syntax
func (h *HealthEndpointHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		// Parse arguments
		args := d.RemainingArgs()
		if len(args) > 0 {
			h.HealthPath = args[0]
		}

		// Parse blocks
		for d.NextBlock(0) {
			switch d.Val() {
			case "path", "health_path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.HealthPath = d.Val()

			case "readiness_path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.ReadinessPath = d.Val()

			case "liveness_path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.LivenessPath = d.Val()

			case "startup_path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.StartupPath = d.Val()

			case "enable_detailed":
				h.EnableDetailed = true

			case "enable_metrics":
				h.EnableMetrics = true

			case "label":
				if !d.NextArg() {
					return d.ArgErr()
				}
				key := d.Val()
				if !d.NextArg() {
					return d.ArgErr()
				}
				value := d.Val()
				if h.CustomLabels == nil {
					h.CustomLabels = make(map[string]string)
				}
				h.CustomLabels[key] = value

			case "detailed_local_only":
				if !d.NextArg() {
					return d.ArgErr()
				}
				switch d.Val() {
				case "true":
					v := true
					h.DetailedLocalOnly = &v
				case "false":
					v := false
					h.DetailedLocalOnly = &v
				default:
					return d.Errf("detailed_local_only must be 'true' or 'false'")
				}

			default:
				return d.Errf("unrecognized gcs_health directive: %s", d.Val())
			}
		}
	}

	return nil
}

// ParseCaddyfileHealth is the Caddyfile adapter for the gcs_health directive.
func ParseCaddyfileHealth(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var hh HealthEndpointHandler
	if err := hh.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &hh, nil
}
