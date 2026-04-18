package validation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	pkg "github.com/spuranam/caddy-fs-gcs/pkg"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
)

// ValidationEndpointHandler provides configuration validation endpoints
type ValidationEndpointHandler struct {
	// Configuration
	ValidationPath string            `json:"validation_path,omitempty"`
	EnableLive     bool              `json:"enable_live,omitempty"`
	EnableDryRun   bool              `json:"enable_dry_run,omitempty"`
	StrictMode     bool              `json:"strict_mode,omitempty"`
	ValidateOnLoad bool              `json:"validate_on_load,omitempty"`
	CustomLabels   map[string]string `json:"custom_labels,omitempty"`

	// LocalOnly restricts validation endpoints to loopback addresses
	// (127.0.0.0/8 and ::1). Defaults to true. Set to false to allow
	// remote access (not recommended without additional authentication).
	LocalOnly *bool `json:"local_only,omitempty"`

	// Internal state
	validator      *ConfigValidator
	logger         *zap.Logger
	mu             sync.RWMutex
	lastValidation *ValidationResult
}

// Interface guards — compile-time verification that ValidationEndpointHandler
// satisfies all expected Caddy interfaces.
var (
	_ caddy.Module                = (*ValidationEndpointHandler)(nil)
	_ caddy.Provisioner           = (*ValidationEndpointHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*ValidationEndpointHandler)(nil)
	_ caddyfile.Unmarshaler       = (*ValidationEndpointHandler)(nil)
)

// CaddyModule returns the Caddy module information
func (*ValidationEndpointHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.config_validation",
		New: func() caddy.Module { return new(ValidationEndpointHandler) },
	}
}

// Provision sets up the validation endpoint handler
func (v *ValidationEndpointHandler) Provision(ctx caddy.Context) error {
	// Set defaults
	if v.ValidationPath == "" {
		v.ValidationPath = "/validate"
	}
	if v.CustomLabels == nil {
		v.CustomLabels = make(map[string]string)
	}
	// Default to local-only access for security.
	if v.LocalOnly == nil {
		v.LocalOnly = new(true)
	}

	// Set default labels only when not already set by user config.
	if _, ok := v.CustomLabels["service"]; !ok {
		v.CustomLabels["service"] = pkg.ServiceName
	}
	if _, ok := v.CustomLabels["version"]; !ok {
		v.CustomLabels["version"] = pkg.Version
	}

	// Initialize validator — use ctx.Logger() for Caddy integration,
	// bridge to slog for the downstream ConfigValidator.
	v.logger = ctx.Logger()

	if v.LocalOnly != nil && !*v.LocalOnly {
		v.logger.Warn("validation endpoints exposed to remote clients; consider enabling local_only or adding authentication")
	}

	slogger := slog.New(zapslog.NewHandler(v.logger.Core(), zapslog.WithName("config_validation")))
	v.validator = NewConfigValidator(slogger)
	v.validator.StrictMode = v.StrictMode
	v.validator.ValidateOnLoad = v.ValidateOnLoad

	return nil
}

// ServeHTTP handles HTTP requests for validation endpoints
func (v *ValidationEndpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Check if this request targets a validation endpoint.
	isValidationRoute := false
	switch r.URL.Path {
	case v.ValidationPath,
		v.ValidationPath + "/live",
		v.ValidationPath + "/dry-run",
		v.ValidationPath + "/status":
		isValidationRoute = true
	}

	// If the route doesn't match any validation endpoint, pass through immediately.
	if !isValidationRoute {
		return next.ServeHTTP(w, r)
	}

	// Enforce local-only access: reject non-loopback clients with 403.
	if v.LocalOnly == nil || *v.LocalOnly {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return nil
		}
	}

	// Handle different validation endpoints
	switch r.URL.Path {
	case v.ValidationPath:
		return v.serveValidation(w, r)
	case v.ValidationPath + "/live":
		if v.EnableLive {
			return v.serveLiveValidation(w, r)
		}
	case v.ValidationPath + "/dry-run":
		if v.EnableDryRun {
			return v.serveDryRunValidation(w, r)
		}
	case v.ValidationPath + "/status":
		return v.serveValidationStatus(w, r)
	}

	// Pass through to next handler
	return next.ServeHTTP(w, r)
}

// maxValidationBodySize is the maximum size of a validation request body (1 MB).
const maxValidationBodySize = 1 << 20

// validationTimeout bounds the total time spent reading and validating a request.
const validationTimeout = 10 * time.Second

// serveValidation serves the main validation endpoint
func (v *ValidationEndpointHandler) serveValidation(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), validationTimeout)
	defer cancel()

	// Parse request body for configuration
	var config GCSConfig
	hasBody := false
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, maxValidationBodySize)
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&config); err != nil {
			// Empty body (EOF) — treat as no config provided
			if !errors.Is(err, io.EOF) {
				http.Error(w, "Invalid JSON configuration", http.StatusBadRequest)
				return nil
			}
		} else {
			hasBody = true
		}
	}

	// Check context deadline after decoding.
	if ctx.Err() != nil {
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
		return nil
	}

	// Validate configuration
	var result *ValidationResult
	if !hasBody {
		// For empty requests, return a successful validation with default config
		result = &ValidationResult{
			Valid:     true,
			Errors:    make([]ValidationError, 0),
			Warnings:  make([]ValidationWarning, 0),
			Timestamp: time.Now(),
		}
	} else {
		configMap := buildConfigMap(&config)
		result = v.validator.ValidateGCSConfig(configMap)
	}
	v.mu.Lock()
	v.lastValidation = result
	v.mu.Unlock()

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Add custom headers
	for key, value := range v.CustomLabels {
		w.Header().Set("X-Validation-"+key, value)
	}

	// Return appropriate status code based on validation result
	if result.Valid {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	return json.NewEncoder(w).Encode(result)
}

// serveLiveValidation serves live configuration validation
func (v *ValidationEndpointHandler) serveLiveValidation(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), validationTimeout)
	defer cancel()

	// Parse request body for configuration
	var config GCSConfig
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, maxValidationBodySize)
		if !decodeJSON(w, r, &config) {
			return nil
		}
	}

	// Check context deadline after decoding.
	if ctx.Err() != nil {
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
		return nil
	}

	// Perform live validation (includes GCS-specific + runtime checks)
	configMap := buildConfigMap(&config)
	result := v.validator.ValidateGCSConfig(configMap)
	v.mu.Lock()
	v.lastValidation = result
	v.mu.Unlock()

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Add custom headers
	for key, value := range v.CustomLabels {
		w.Header().Set("X-Validation-"+key, value)
	}

	// Return appropriate status code based on validation result
	if result.Valid {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	return json.NewEncoder(w).Encode(result)
}

// serveDryRunValidation serves dry-run configuration validation
func (v *ValidationEndpointHandler) serveDryRunValidation(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), validationTimeout)
	defer cancel()

	// Parse request body for configuration
	var config GCSConfig
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, maxValidationBodySize)
		if !decodeJSON(w, r, &config) {
			return nil
		}
	}

	// Check context deadline after decoding.
	if ctx.Err() != nil {
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
		return nil
	}

	// Perform dry-run validation (simulates configuration without applying)
	configMap := buildConfigMap(&config)
	result := v.validator.ValidateGCSConfig(configMap)

	// Add dry-run specific information
	dryRunResult := map[string]any{
		"validation": result,
		"dry_run": map[string]any{
			"simulated": true,
			"message":   "Configuration validated in dry-run mode",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Add custom headers
	for key, value := range v.CustomLabels {
		w.Header().Set("X-Validation-"+key, value)
	}

	// Set HTTP status code based on validation result
	if !result.Valid {
		w.WriteHeader(http.StatusBadRequest)
	}

	return json.NewEncoder(w).Encode(dryRunResult)
}

// serveValidationStatus serves the current validation status
func (v *ValidationEndpointHandler) serveValidationStatus(w http.ResponseWriter, _ *http.Request) error {
	status := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   "caddy-fs-gcs",
		"validator": map[string]any{
			"strict_mode":      v.validator.StrictMode,
			"validate_on_load": v.validator.ValidateOnLoad,
			"custom_rules":     len(v.validator.CustomRules),
		},
		"endpoints": map[string]any{
			"validation": v.ValidationPath,
			"live":       v.EnableLive,
			"dry_run":    v.EnableDryRun,
		},
		"custom_labels": v.CustomLabels,
	}

	// Add last validation result if available
	v.mu.RLock()
	lastValidation := v.lastValidation
	v.mu.RUnlock()
	if lastValidation != nil {
		status["last_validation"] = map[string]any{
			"timestamp": lastValidation.Timestamp,
			"valid":     lastValidation.Valid,
			"errors":    len(lastValidation.Errors),
			"warnings":  len(lastValidation.Warnings),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(status)
}

// UnmarshalCaddyfile parses Caddyfile syntax
func (v *ValidationEndpointHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		// Set default validation path
		if v.ValidationPath == "" {
			v.ValidationPath = "/validate"
		}

		// Parse arguments
		args := d.RemainingArgs()
		if len(args) > 0 {
			v.ValidationPath = args[0]
		}

		// Parse blocks
		for d.NextBlock(0) {
			switch d.Val() {
			case "path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v.ValidationPath = d.Val()

			case "enable_live":
				v.EnableLive = true

			case "enable_dry_run":
				v.EnableDryRun = true

			case "strict_mode":
				v.StrictMode = true

			case "validate_on_load":
				v.ValidateOnLoad = true

			case "local_only":
				if !d.NextArg() {
					return d.ArgErr()
				}
				switch d.Val() {
				case "true":
					v.LocalOnly = new(true)
				case "false":
					v.LocalOnly = new(false)
				default:
					return d.Errf("local_only must be 'true' or 'false'")
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
				if v.CustomLabels == nil {
					v.CustomLabels = make(map[string]string)
				}
				v.CustomLabels[key] = value

			default:
				return d.Errf("unrecognized config_validation directive: %s", d.Val())
			}
		}
	}

	return nil
}

// decodeJSON decodes the request body into dst. Returns true on success.
// On failure it writes an HTTP 400 error and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "Invalid JSON configuration", http.StatusBadRequest)
		return false
	}
	return true
}

// buildConfigMap converts a GCSConfig to a map for the validator.
func buildConfigMap(config *GCSConfig) map[string]any {
	return map[string]any{
		"project_id":         config.ProjectID,
		"bucket_name":        config.BucketName,
		"credentials_file":   config.CredentialsFile,
		"credentials_config": config.CredentialsConfig,
		"max_connections":    config.MaxConnections,
		"connection_timeout": config.ConnectionTimeout,
	}
}

// ParseCaddyfileValidation is the Caddyfile adapter for the config_validation directive.
func ParseCaddyfileValidation(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var v ValidationEndpointHandler
	if err := v.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &v, nil
}
