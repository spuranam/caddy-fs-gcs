package validation

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestValidationEndpointHandler_Creation tests ValidationEndpointHandler creation
func TestValidationEndpointHandler_Creation(t *testing.T) {
	t.Parallel()
	t.Run("create handler with defaults", func(t *testing.T) {
		t.Parallel()
		handler := &ValidationEndpointHandler{}

		// Test CaddyModule
		moduleInfo := handler.CaddyModule()
		assert.Equal(t, caddy.ModuleID("http.handlers.config_validation"), moduleInfo.ID)
		assert.NotNil(t, moduleInfo.New)
	})
}

// TestValidationEndpointHandler_Provision tests handler provisioning
func TestValidationEndpointHandler_Provision(t *testing.T) {
	t.Parallel()
	t.Run("provision with defaults", func(t *testing.T) {
		t.Parallel()
		handler := &ValidationEndpointHandler{}

		// Create a mock Caddy context
		ctx := caddy.Context{}

		err := handler.Provision(ctx)

		assert.NoError(t, err)
		assert.Equal(t, "/validate", handler.ValidationPath)
		assert.NotNil(t, handler.CustomLabels)
		assert.Equal(t, "caddy-fs-gcs", handler.CustomLabels["service"])
		// LocalOnly should default to true.
		assert.NotNil(t, handler.LocalOnly)
		assert.True(t, *handler.LocalOnly)
	})

	t.Run("provision with custom path", func(t *testing.T) {
		t.Parallel()
		handler := &ValidationEndpointHandler{
			ValidationPath: "/custom-validate",
		}

		ctx := caddy.Context{}

		err := handler.Provision(ctx)

		assert.NoError(t, err)
		assert.Equal(t, "/custom-validate", handler.ValidationPath)
	})
}

// TestValidationEndpointHandler_Configuration tests handler configuration
func TestValidationEndpointHandler_Configuration(t *testing.T) {
	t.Parallel()
	handler := &ValidationEndpointHandler{
		ValidationPath: "/validate",
		logger:         zap.NewNop(),
		validator:      NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	t.Run("test handler configuration", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/validate", handler.ValidationPath)
		assert.NotNil(t, handler.logger)
		assert.NotNil(t, handler.validator)
	})

	t.Run("test custom labels", func(t *testing.T) {
		t.Parallel()
		handler.CustomLabels = map[string]string{
			"service": "test-service",
			"version": "1.0.0",
		}

		assert.Equal(t, "test-service", handler.CustomLabels["service"])
		assert.Equal(t, "1.0.0", handler.CustomLabels["version"])
	})
}

// TestValidationEndpointHandler_LocalOnly tests local-only access restriction
func TestValidationEndpointHandler_LocalOnly(t *testing.T) {
	t.Parallel()

	newHandler := func(localOnly bool) *ValidationEndpointHandler {
		h := &ValidationEndpointHandler{
			ValidationPath: "/validate",
			logger:         zap.NewNop(),
			validator:      NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil))),
			CustomLabels:   map[string]string{"service": "test"},
		}
		h.LocalOnly = &localOnly
		return h
	}

	passThrough := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusTeapot) // sentinel to detect pass-through
		return nil
	})

	t.Run("loopback allowed", func(t *testing.T) {
		t.Parallel()
		h := newHandler(true)
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()

		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		// Should hit the validation endpoint, not pass-through.
		assert.NotEqual(t, http.StatusTeapot, w.Code)
	})

	t.Run("remote blocked", func(t *testing.T) {
		t.Parallel()
		h := newHandler(true)
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		// Should return 403 Forbidden, not pass-through.
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("remote allowed when local_only false", func(t *testing.T) {
		t.Parallel()
		h := newHandler(false)
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		// Should serve the validation endpoint.
		assert.NotEqual(t, http.StatusTeapot, w.Code)
	})
}

// TestUnmarshalCaddyfile_LocalOnly tests Caddyfile parsing of local_only
func TestUnmarshalCaddyfile_LocalOnly(t *testing.T) {
	t.Parallel()

	t.Run("local_only true", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		tokens, _ := caddyfile.Tokenize([]byte(`config_validation { local_only true }`), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.NoError(t, err)
		assert.NotNil(t, h.LocalOnly)
		assert.True(t, *h.LocalOnly)
	})

	t.Run("local_only false", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		tokens, _ := caddyfile.Tokenize([]byte(`config_validation { local_only false }`), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.NoError(t, err)
		assert.NotNil(t, h.LocalOnly)
		assert.False(t, *h.LocalOnly)
	})

	t.Run("local_only invalid value", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		tokens, _ := caddyfile.Tokenize([]byte(`config_validation { local_only maybe }`), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "local_only must be 'true' or 'false'")
	})

	t.Run("local_only missing arg", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		tokens, _ := caddyfile.Tokenize([]byte(`config_validation { local_only }`), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.Error(t, err)
	})
}

// TestValidationEndpointHandler_ServeHTTP tests HTTP routing
func TestValidationEndpointHandler_ServeHTTP(t *testing.T) {
	t.Parallel()

	newHandler := func() *ValidationEndpointHandler {
		localOnly := false
		h := &ValidationEndpointHandler{
			ValidationPath: "/validate",
			EnableLive:     true,
			EnableDryRun:   true,
			logger:         zap.NewNop(),
			validator:      NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil))),
			CustomLabels:   map[string]string{"service": "test"},
			LocalOnly:      &localOnly,
		}
		return h
	}

	passThrough := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusTeapot)
		return nil
	})

	t.Run("non-matching path passes through", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		r := httptest.NewRequest("GET", "/other", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTeapot, w.Code)
	})

	t.Run("validate endpoint returns JSON", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	})

	t.Run("validate with valid config body", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		body := strings.NewReader(`{"bucket_name":"my-bucket","project_id":"my-project"}`)
		r := httptest.NewRequest("POST", "/validate", body)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("validate with invalid JSON body", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		body := strings.NewReader(`{invalid json}`)
		r := httptest.NewRequest("POST", "/validate", body)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("status endpoint", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		r := httptest.NewRequest("GET", "/validate/status", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "caddy-fs-gcs")
	})

	t.Run("live validation endpoint", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		body := strings.NewReader(`{"bucket_name":"my-bucket","project_id":"my-project"}`)
		r := httptest.NewRequest("POST", "/validate/live", body)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("dry-run validation endpoint", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		body := strings.NewReader(`{"bucket_name":"my-bucket","project_id":"my-project"}`)
		r := httptest.NewRequest("POST", "/validate/dry-run", body)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "dry_run")
	})

	t.Run("live endpoint disabled passes through", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		h.EnableLive = false
		r := httptest.NewRequest("POST", "/validate/live", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTeapot, w.Code)
	})

	t.Run("dry-run endpoint disabled passes through", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		h.EnableDryRun = false
		r := httptest.NewRequest("POST", "/validate/dry-run", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTeapot, w.Code)
	})

	t.Run("LocalOnly nil defaults to loopback", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		h.LocalOnly = nil
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("custom labels appear as headers", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		h.CustomLabels = map[string]string{"env": "test"}
		r := httptest.NewRequest("GET", "/validate", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		err := h.ServeHTTP(w, r, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, "test", w.Header().Get("X-Validation-env"))
	})

	t.Run("status with last validation", func(t *testing.T) {
		t.Parallel()
		h := newHandler()
		// First do a validation to populate lastValidation
		body := strings.NewReader(`{"bucket_name":"my-bucket"}`)
		r1 := httptest.NewRequest("POST", "/validate", body)
		r1.RemoteAddr = "127.0.0.1:12345"
		w1 := httptest.NewRecorder()
		_ = h.ServeHTTP(w1, r1, passThrough)
		// Now check status
		r2 := httptest.NewRequest("GET", "/validate/status", nil)
		r2.RemoteAddr = "127.0.0.1:12345"
		w2 := httptest.NewRecorder()
		err := h.ServeHTTP(w2, r2, passThrough)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, w2.Code)
		assert.Contains(t, w2.Body.String(), "last_validation")
	})
}

// TestUnmarshalCaddyfile_Directives tests all Caddyfile directives
func TestUnmarshalCaddyfile_Directives(t *testing.T) {
	t.Parallel()

	t.Run("all directives", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		input := `config_validation /custom-path {
			enable_live
			enable_dry_run
			strict_mode
			validate_on_load
			label env production
		}`
		tokens, _ := caddyfile.Tokenize([]byte(input), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.NoError(t, err)
		assert.Equal(t, "/custom-path", h.ValidationPath)
		assert.True(t, h.EnableLive)
		assert.True(t, h.EnableDryRun)
		assert.True(t, h.StrictMode)
		assert.True(t, h.ValidateOnLoad)
		assert.Equal(t, "production", h.CustomLabels["env"])
	})

	t.Run("path directive", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		input := `config_validation { path /my-validate }`
		tokens, _ := caddyfile.Tokenize([]byte(input), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.NoError(t, err)
		assert.Equal(t, "/my-validate", h.ValidationPath)
	})

	t.Run("unknown directive", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		input := `config_validation { bogus_thing }`
		tokens, _ := caddyfile.Tokenize([]byte(input), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unrecognized")
	})

	t.Run("label missing key", func(t *testing.T) {
		t.Parallel()
		h := &ValidationEndpointHandler{}
		input := `config_validation { label }`
		tokens, _ := caddyfile.Tokenize([]byte(input), "test")
		err := h.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		assert.Error(t, err)
	})
}

// TestProvisionLocalOnlyWarning tests the warning log when local_only is false
func TestProvisionLocalOnlyWarning(t *testing.T) {
	t.Parallel()
	localOnly := false
	h := &ValidationEndpointHandler{
		LocalOnly: &localOnly,
	}
	err := h.Provision(caddy.Context{})
	assert.NoError(t, err)
	// local_only=false should still provision without error; warning is just logged
	assert.NotNil(t, h.validator)
}
