package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/stretchr/testify/assert"
)

// mockHandler is a mock implementation of caddyhttp.Handler
type mockHandler struct {
	called bool
}

func (m *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	m.called = true
	return nil
}

// TestPrometheusEndpointHandler tests Prometheus endpoint functionality
func TestPrometheusEndpointHandler(t *testing.T) {
	t.Parallel()
	t.Run("creation and initialization", func(t *testing.T) {
		t.Parallel()
		t.Run("create prometheus endpoint handler", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			assert.NotNil(t, handler)
			assert.Empty(t, handler.MetricsPath)
			assert.False(t, handler.EnableHealth)
			assert.False(t, handler.EnableDebug)
			assert.Nil(t, handler.CustomLabels)
		})

		t.Run("caddy module info", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			moduleInfo := handler.CaddyModule()
			assert.Equal(t, caddy.ModuleID("http.handlers.prometheus"), moduleInfo.ID)
			assert.NotNil(t, moduleInfo.New)
		})

		t.Run("provision with defaults", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			ctx := caddy.Context{}

			err := handler.Provision(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "/metrics", handler.MetricsPath)
			assert.NotNil(t, handler.CustomLabels)
			assert.Equal(t, "caddy-fs-gcs", handler.CustomLabels["service"])
			assert.Equal(t, "1.0.0", handler.CustomLabels["version"])
			assert.False(t, handler.startTime.IsZero())
			assert.Equal(t, int64(0), handler.requestCount.Load())
		})

		t.Run("provision with custom values", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/custom/metrics",
				EnableHealth: true,
				EnableDebug:  true,
				CustomLabels: map[string]string{
					"environment": "test",
					"version":     "2.0.0",
				},
			}
			ctx := caddy.Context{}

			err := handler.Provision(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "/custom/metrics", handler.MetricsPath)
			assert.True(t, handler.EnableHealth)
			assert.True(t, handler.EnableDebug)
			assert.Equal(t, "test", handler.CustomLabels["environment"])
			assert.Equal(t, "2.0.0", handler.CustomLabels["version"]) // Provision preserves user-set version
			assert.Equal(t, "caddy-fs-gcs", handler.CustomLabels["service"])
		})
	})

	t.Run("HTTP serving", func(t *testing.T) {
		t.Parallel()
		t.Run("serve metrics endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
			assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
		})

		t.Run("serve health endpoint when enabled", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/metrics",
				EnableHealth: true,
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var health map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &health)
			assert.NoError(t, err)
			assert.Equal(t, "healthy", health["status"])
			assert.Equal(t, "caddy-fs-gcs", health["service"])
			assert.Contains(t, health, "timestamp")
			assert.Contains(t, health, "uptime")
			assert.Contains(t, health, "requests")
		})

		t.Run("serve health endpoint when disabled", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/metrics",
				EnableHealth: false,
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.True(t, next.called)
		})

		t.Run("serve debug metrics endpoint when enabled", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
				EnableDebug: true,
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/debug/metrics", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var debug map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &debug)
			assert.NoError(t, err)
			assert.Contains(t, debug, "timestamp")
			assert.Contains(t, debug, "metric_count")
			assert.Contains(t, debug, "uptime")
			assert.Contains(t, debug, "request_count")
			assert.Contains(t, debug, "custom_labels")
			assert.Contains(t, debug, "metrics")
		})

		t.Run("serve debug metrics endpoint when disabled", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
				EnableDebug: false,
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/debug/metrics", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.True(t, next.called)
		})

		t.Run("serve metrics health endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics/health", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var health map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &health)
			assert.NoError(t, err)
			assert.Equal(t, "healthy", health["status"])
			assert.Equal(t, "prometheus-metrics", health["service"])
			assert.Contains(t, health, "timestamp")
		})

		t.Run("pass through unknown endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/unknown", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.True(t, next.called)
		})
	})

	t.Run("internal serving methods", func(t *testing.T) {
		t.Parallel()
		t.Run("serve metrics", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
				CustomLabels: map[string]string{
					"test": "value",
				},
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			err := handler.serveMetrics(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
			assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
		})

		t.Run("serve health", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				CustomLabels: map[string]string{
					"environment": "test",
				},
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/health", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()

			err := handler.serveHealth(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var health map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &health)
			assert.NoError(t, err)
			assert.Equal(t, "healthy", health["status"])
			assert.Equal(t, "caddy-fs-gcs", health["service"])
			assert.Equal(t, "test", health["environment"])
			assert.Contains(t, health, "timestamp")
			assert.Contains(t, health, "uptime")
			assert.Contains(t, health, "requests")
		})

		t.Run("serve health non-loopback hides custom labels", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				CustomLabels: map[string]string{
					"environment": "test",
				},
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/health", nil)
			// httptest default RemoteAddr is 192.0.2.1 (non-loopback)
			w := httptest.NewRecorder()

			err := handler.serveHealth(w, req)
			assert.NoError(t, err)

			var health map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &health)
			assert.NoError(t, err)
			assert.Equal(t, "healthy", health["status"])
			assert.NotContains(t, health, "environment")
		})

		t.Run("serve debug metrics", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				CustomLabels: map[string]string{
					"environment": "test",
				},
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/debug/metrics", nil)
			w := httptest.NewRecorder()

			err := handler.serveDebugMetrics(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var debug map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &debug)
			assert.NoError(t, err)
			assert.Contains(t, debug, "timestamp")
			assert.Contains(t, debug, "metric_count")
			assert.Contains(t, debug, "uptime")
			assert.Contains(t, debug, "request_count")
			assert.Contains(t, debug, "custom_labels")
			assert.Contains(t, debug, "metrics")
			assert.Equal(t, "test", debug["custom_labels"].(map[string]any)["environment"])
		})

		t.Run("serve metrics health", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics/health", nil)
			w := httptest.NewRecorder()

			err := handler.serveMetricsHealth(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Parse response
			var health map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &health)
			assert.NoError(t, err)
			assert.Equal(t, "healthy", health["status"])
			assert.Equal(t, "prometheus-metrics", health["service"])
			assert.Contains(t, health, "timestamp")
		})
	})

	t.Run("caddyfile parsing", func(t *testing.T) {
		t.Parallel()
		t.Run("unmarshal caddyfile with path", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "prometheus", File: "test", Line: 1},
				{Text: "/custom/metrics", File: "test", Line: 1},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.NoError(t, err)
			assert.Equal(t, "/custom/metrics", handler.MetricsPath)
		})

		t.Run("unmarshal caddyfile with block", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "prometheus", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "path", File: "test", Line: 2},
				{Text: "/custom/metrics", File: "test", Line: 2},
				{Text: "enable_health", File: "test", Line: 3},
				{Text: "enable_debug", File: "test", Line: 4},
				{Text: "label", File: "test", Line: 5},
				{Text: "environment", File: "test", Line: 5},
				{Text: "test", File: "test", Line: 5},
				{Text: "}", File: "test", Line: 6},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.NoError(t, err)
			assert.Equal(t, "/custom/metrics", handler.MetricsPath)
			assert.True(t, handler.EnableHealth)
			assert.True(t, handler.EnableDebug)
			assert.Equal(t, "test", handler.CustomLabels["environment"])
		})

		t.Run("unmarshal caddyfile with multiple labels", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "prometheus", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "label", File: "test", Line: 2},
				{Text: "env", File: "test", Line: 2},
				{Text: "prod", File: "test", Line: 2},
				{Text: "label", File: "test", Line: 3},
				{Text: "version", File: "test", Line: 3},
				{Text: "2.0.0", File: "test", Line: 3},
				{Text: "}", File: "test", Line: 4},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.NoError(t, err)
			assert.Equal(t, "prod", handler.CustomLabels["env"])
			assert.Equal(t, "2.0.0", handler.CustomLabels["version"])
		})

		t.Run("unmarshal caddyfile with missing label value", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "prometheus", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "label", File: "test", Line: 2},
				{Text: "env", File: "test", Line: 2},
				{Text: "}", File: "test", Line: 3},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.Error(t, err)
		})

		t.Run("unmarshal caddyfile with missing label key", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "prometheus", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "label", File: "test", Line: 2},
				{Text: "}", File: "test", Line: 3},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.Error(t, err)
		})
	})

	t.Run("request counting", func(t *testing.T) {
		t.Parallel()
		t.Run("increment request count", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
			}
			handler.Provision(caddy.Context{})

			// Make multiple requests
			for range 5 {
				req := httptest.NewRequest("GET", "/metrics", nil)
				w := httptest.NewRecorder()

				next := &mockHandler{}

				err := handler.ServeHTTP(w, req, next)
				assert.NoError(t, err)
			}

			assert.Equal(t, int64(5), handler.requestCount.Load())
		})
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("serve with nil custom labels", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/metrics",
				CustomLabels: nil,
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			err := handler.serveMetrics(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve with empty custom labels", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/metrics",
				CustomLabels: make(map[string]string),
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			err := handler.serveMetrics(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve with custom labels containing special characters", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath: "/metrics",
				CustomLabels: map[string]string{
					"test-key":         "test-value",
					"key@with#special": "value@with#special",
				},
			}
			handler.Provision(caddy.Context{})

			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			err := handler.serveMetrics(w, req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	})

	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()
		t.Run("concurrent requests", func(t *testing.T) {
			t.Parallel()
			handler := &PrometheusEndpointHandler{
				MetricsPath:  "/metrics",
				EnableHealth: true,
				EnableDebug:  true,
			}
			handler.Provision(caddy.Context{})

			done := make(chan bool, 20)

			// Start multiple goroutines making requests
			for range 20 {
				go func() {
					defer func() { done <- true }()

					req := httptest.NewRequest("GET", "/metrics", nil)
					w := httptest.NewRecorder()

					next := &mockHandler{}

					err := handler.ServeHTTP(w, req, next)
					assert.NoError(t, err)
				}()
			}

			// Wait for all goroutines to complete
			for range 20 {
				<-done
			}

			// Verify request count
			assert.Equal(t, int64(20), handler.requestCount.Load())
		})
	})
}

// --- CaddyModule ---

func TestPrometheusCaddyModule(t *testing.T) {
	t.Parallel()
	p := &PrometheusEndpointHandler{}
	info := p.CaddyModule()
	assert.Equal(t, caddy.ModuleID("http.handlers.prometheus"), info.ID)
	mod := info.New()
	assert.IsType(t, &PrometheusEndpointHandler{}, mod)
}

// --- serveDebugMetrics ---

func TestServeDebugMetrics(t *testing.T) {
	t.Parallel()
	handler := &PrometheusEndpointHandler{
		MetricsPath: "/metrics",
		startTime:   time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics/debug", nil)
	w := httptest.NewRecorder()
	err := handler.serveDebugMetrics(w, req)
	assert.NoError(t, err)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// --- serveMetricsHealth ---

func TestServeMetricsHealth(t *testing.T) {
	t.Parallel()
	handler := &PrometheusEndpointHandler{
		startTime: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics/health", nil)
	w := httptest.NewRecorder()
	err := handler.serveMetricsHealth(w, req)
	assert.NoError(t, err)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "healthy")
}

// --- UnmarshalCaddyfile: enable_health + enable_debug + label ---

func TestPrometheusUnmarshalCaddyfile_AllDirectives(t *testing.T) {
	t.Parallel()
	d := caddyfile.NewTestDispenser(`prometheus {
		path /metrics
		enable_health
		enable_debug
		label env staging
	}`)
	var p PrometheusEndpointHandler
	err := p.UnmarshalCaddyfile(d)
	assert.NoError(t, err)
	assert.Equal(t, "/metrics", p.MetricsPath)
	assert.True(t, p.EnableHealth)
	assert.True(t, p.EnableDebug)
	assert.Equal(t, "staging", p.CustomLabels["env"])
}

func TestPrometheusUnmarshalCaddyfile_WithArg(t *testing.T) {
	t.Parallel()
	d := caddyfile.NewTestDispenser(`prometheus /prom {
	}`)
	var p PrometheusEndpointHandler
	err := p.UnmarshalCaddyfile(d)
	assert.NoError(t, err)
	assert.Equal(t, "/prom", p.MetricsPath)
}

// --- ServeHTTP: debug and health paths ---

func TestPrometheusServeHTTP_DebugEnabled(t *testing.T) {
	handler := &PrometheusEndpointHandler{
		MetricsPath: "/metrics",
		EnableDebug: true,
		startTime:   time.Now(),
	}
	_ = handler.Provision(caddy.Context{})

	next := &mockHandler{}
	req := httptest.NewRequest(http.MethodGet, "/debug/metrics", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	err := handler.ServeHTTP(w, req, next)
	assert.NoError(t, err)
	assert.Contains(t, w.Body.String(), "metric_count")
}

func TestPrometheusServeHTTP_HealthEnabled(t *testing.T) {
	handler := &PrometheusEndpointHandler{
		MetricsPath:  "/metrics",
		EnableHealth: true,
		startTime:    time.Now(),
	}
	_ = handler.Provision(caddy.Context{})

	next := &mockHandler{}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	err := handler.ServeHTTP(w, req, next)
	assert.NoError(t, err)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestPrometheusServeHTTP_MetricsHealth(t *testing.T) {
	handler := &PrometheusEndpointHandler{
		MetricsPath: "/metrics",
		startTime:   time.Now(),
	}
	_ = handler.Provision(caddy.Context{})

	next := &mockHandler{}
	req := httptest.NewRequest(http.MethodGet, "/metrics/health", nil)
	w := httptest.NewRecorder()
	err := handler.ServeHTTP(w, req, next)
	assert.NoError(t, err)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestPrometheusServeHTTP_Passthrough(t *testing.T) {
	handler := &PrometheusEndpointHandler{
		MetricsPath: "/metrics",
		startTime:   time.Now(),
	}
	_ = handler.Provision(caddy.Context{})

	next := &mockHandler{}
	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	w := httptest.NewRecorder()
	err := handler.ServeHTTP(w, req, next)
	assert.NoError(t, err)
	assert.True(t, next.called)
}

// --- ParseCaddyfilePrometheus ---

func TestParseCaddyfilePrometheus(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`prometheus /prom`)
		h := httpcaddyfile.Helper{Dispenser: d}
		handler, err := ParseCaddyfilePrometheus(h)
		assert.NoError(t, err)
		assert.NotNil(t, handler)
		assert.IsType(t, &PrometheusEndpointHandler{}, handler)
	})

	t.Run("error path", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser("prometheus {\n\tpath\n}")
		h := httpcaddyfile.Helper{Dispenser: d}
		handler, err := ParseCaddyfilePrometheus(h)
		assert.Error(t, err)
		assert.Nil(t, handler)
	})
}
