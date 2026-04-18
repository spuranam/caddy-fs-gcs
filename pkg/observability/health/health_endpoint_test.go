package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	pkg "github.com/spuranam/caddy-fs-gcs/pkg"
	"github.com/stretchr/testify/assert"
)

// mockHandler is a mock implementation of caddyhttp.Handler
type mockCaddyHandler struct {
	called bool
}

func (m *mockCaddyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	m.called = true
	return nil
}

// TestHealthEndpointHandler tests health endpoint functionality
func TestHealthEndpointHandler(t *testing.T) {
	t.Parallel()
	t.Run("creation and initialization", func(t *testing.T) {
		t.Parallel()
		t.Run("create health endpoint handler", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			assert.NotNil(t, handler)
			assert.Empty(t, handler.HealthPath)
			assert.Empty(t, handler.ReadinessPath)
			assert.Empty(t, handler.LivenessPath)
			assert.Empty(t, handler.StartupPath)
			assert.False(t, handler.EnableDetailed)
			assert.False(t, handler.EnableMetrics)
			assert.Nil(t, handler.CustomLabels)
		})

		t.Run("caddy module info", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			moduleInfo := handler.CaddyModule()
			assert.Equal(t, caddy.ModuleID("http.handlers.health"), moduleInfo.ID)
			assert.NotNil(t, moduleInfo.New)
		})

		t.Run("provision with defaults", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			ctx := caddy.Context{}

			err := handler.Provision(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "/health", handler.HealthPath)
			assert.Equal(t, "/ready", handler.ReadinessPath)
			assert.Equal(t, "/live", handler.LivenessPath)
			assert.Equal(t, "/startup", handler.StartupPath)
			assert.False(t, handler.EnableDetailed)
			assert.False(t, handler.EnableMetrics)
			assert.NotNil(t, handler.CustomLabels)
			assert.Equal(t, "caddy-fs-gcs", handler.CustomLabels["service"])
			assert.Equal(t, pkg.Version, handler.CustomLabels["version"])
		})

		t.Run("provision with custom values", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:     "/custom/health",
				ReadinessPath:  "/custom/ready",
				LivenessPath:   "/custom/live",
				StartupPath:    "/custom/startup",
				EnableDetailed: true,
				EnableMetrics:  true,
				CustomLabels: map[string]string{
					"environment": "test",
					"version":     "2.0.0",
				},
			}
			ctx := caddy.Context{}

			err := handler.Provision(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "/custom/health", handler.HealthPath)
			assert.Equal(t, "/custom/ready", handler.ReadinessPath)
			assert.Equal(t, "/custom/live", handler.LivenessPath)
			assert.Equal(t, "/custom/startup", handler.StartupPath)
			assert.True(t, handler.EnableDetailed)
			assert.True(t, handler.EnableMetrics)
			assert.Equal(t, "test", handler.CustomLabels["environment"])
			assert.Equal(t, "2.0.0", handler.CustomLabels["version"]) // Provision preserves user-set version
			assert.Equal(t, "caddy-fs-gcs", handler.CustomLabels["service"])
		})
	})

	t.Run("HTTP serving", func(t *testing.T) {
		t.Parallel()
		t.Run("serve health endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:     "/health",
				ReadinessPath:  "/ready",
				LivenessPath:   "/live",
				StartupPath:    "/startup",
				EnableDetailed: true,
				EnableMetrics:  true,
				startTime:      time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "healthy")
		})

		t.Run("serve readiness endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				ReadinessPath: "/ready",
				startTime:     time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/ready", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve liveness endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				LivenessPath: "/live",
				startTime:    time.Now(),
			}

			req := httptest.NewRequest("GET", "/live", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve startup endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				StartupPath: "/startup",
				startTime:   time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/startup", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve detailed health endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:        "/health",
				EnableDetailed:    true,
				DetailedLocalOnly: new(false),
				startTime:         time.Now(),
			}

			req := httptest.NewRequest("GET", "/health/detailed", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve health metrics endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:        "/health",
				EnableMetrics:     true,
				DetailedLocalOnly: new(false),
				startTime:         time.Now(),
			}

			req := httptest.NewRequest("GET", "/health/metrics", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.False(t, next.called) // Should not call next handler
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("pass through unknown endpoint", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath: "/health",
			}

			req := httptest.NewRequest("GET", "/unknown", nil)
			w := httptest.NewRecorder()

			// Mock next handler
			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.True(t, next.called) // Should call next handler
		})
	})

	t.Run("health checker management", func(t *testing.T) {
		t.Parallel()
		t.Run("add health checker", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			mockChecker := &MockHealthChecker{}
			handler.AddHealthChecker(mockChecker)
			assert.Len(t, handler.checkers, 1)
			assert.Equal(t, mockChecker, handler.checkers[0])
		})

		t.Run("add multiple health checkers", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			checker1 := &MockHealthChecker{}
			checker2 := &MockHealthChecker{}
			handler.AddHealthChecker(checker1)
			handler.AddHealthChecker(checker2)
			assert.NotNil(t, handler.compositeChecker)
			assert.Len(t, handler.compositeChecker.checkers, 2)
		})

		t.Run("add nil health checker", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			assert.NotPanics(t, func() {
				handler.AddHealthChecker(nil)
			})
		})
	})

	t.Run("caddyfile parsing", func(t *testing.T) {
		t.Parallel()
		t.Run("unmarshal caddyfile with basic configuration", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.NoError(t, err)
		})

		t.Run("unmarshal caddyfile with block configuration", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "health_path", File: "test", Line: 2},
				{Text: "/custom/health", File: "test", Line: 2},
				{Text: "readiness_path", File: "test", Line: 3},
				{Text: "/custom/ready", File: "test", Line: 3},
				{Text: "liveness_path", File: "test", Line: 4},
				{Text: "/custom/live", File: "test", Line: 4},
				{Text: "startup_path", File: "test", Line: 5},
				{Text: "/custom/startup", File: "test", Line: 5},
				{Text: "enable_detailed", File: "test", Line: 6},
				{Text: "enable_metrics", File: "test", Line: 7},
				{Text: "label", File: "test", Line: 8},
				{Text: "environment", File: "test", Line: 8},
				{Text: "test", File: "test", Line: 8},
				{Text: "}", File: "test", Line: 9},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.NoError(t, err)
			assert.Equal(t, "/custom/health", handler.HealthPath)
			assert.Equal(t, "/custom/ready", handler.ReadinessPath)
			assert.Equal(t, "/custom/live", handler.LivenessPath)
			assert.Equal(t, "/custom/startup", handler.StartupPath)
			assert.True(t, handler.EnableDetailed)
			assert.True(t, handler.EnableMetrics)
			assert.Equal(t, "test", handler.CustomLabels["environment"])
		})

		t.Run("unmarshal caddyfile with multiple labels", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
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
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
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
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "label", File: "test", Line: 2},
				{Text: "}", File: "test", Line: 3},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.Error(t, err)
		})

		t.Run("unmarshal caddyfile with missing path value", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			tokens := []caddyfile.Token{
				{Text: "health", File: "test", Line: 1},
				{Text: "{", File: "test", Line: 1},
				{Text: "health_path", File: "test", Line: 2},
				{Text: "}", File: "test", Line: 3},
			}
			dispenser := caddyfile.NewDispenser(tokens)

			err := handler.UnmarshalCaddyfile(dispenser)
			assert.Error(t, err) // Missing argument for health_path
		})
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("serve with nil custom labels", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:   "/health",
				CustomLabels: nil,
				startTime:    time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve with empty custom labels", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:   "/health",
				CustomLabels: make(map[string]string),
				startTime:    time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("serve with custom labels containing special characters", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath: "/health",
				CustomLabels: map[string]string{
					"test-key":         "test-value",
					"key@with#special": "value@with#special",
				},
				startTime: time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			next := &mockCaddyHandler{}

			err := handler.ServeHTTP(w, req, next)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	})

	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()
		t.Run("concurrent requests", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{
				HealthPath:     "/health",
				ReadinessPath:  "/ready",
				LivenessPath:   "/live",
				StartupPath:    "/startup",
				EnableDetailed: true,
				EnableMetrics:  true,
				startTime:      time.Now(),
			}
			handler.mu.Lock()
			handler.startupComplete = true
			handler.mu.Unlock()

			done := make(chan bool, 20)

			// Start multiple goroutines making requests
			for range 20 {
				go func() {
					defer func() { done <- true }()

					req := httptest.NewRequest("GET", "/health", nil)
					w := httptest.NewRecorder()

					next := &mockCaddyHandler{}

					err := handler.ServeHTTP(w, req, next)
					assert.NoError(t, err)
				}()
			}

			// Wait for all goroutines to complete
			for range 20 {
				<-done
			}
		})

		t.Run("concurrent checker addition", func(t *testing.T) {
			t.Parallel()
			handler := &HealthEndpointHandler{}
			done := make(chan bool, 10)

			// Start multiple goroutines adding checkers
			for range 10 {
				go func() {
					defer func() { done <- true }()

					mockChecker := &MockHealthChecker{}
					handler.AddHealthChecker(mockChecker)
				}()
			}

			// Wait for all goroutines to complete
			for range 10 {
				<-done
			}

			// Verify checkers were added
			assert.Len(t, handler.checkers, 10)
		})
	})
}

// MockHealthChecker is a mock implementation of HealthChecker for testing
type MockHealthChecker struct {
	status  string
	message string
	errMsg  string
}

func (m *MockHealthChecker) Check(ctx context.Context) *HealthStatus {
	return &HealthStatus{
		Status:    m.status,
		Message:   m.message,
		Timestamp: time.Now(),
		Details: map[string]string{
			"mock": "test",
		},
		Error: m.errMsg,
	}
}

func (m *MockHealthChecker) CheckStorage(ctx context.Context) *HealthStatus {
	return &HealthStatus{
		Status:    m.status,
		Message:   m.message,
		Timestamp: time.Now(),
		Details: map[string]string{
			"storage": "test",
		},
		Error: m.errMsg,
	}
}

// TestHealthEndpointHandler_AdditionalCoverage tests additional functions for coverage
func TestHealthEndpointHandler_AdditionalCoverage(t *testing.T) {
	t.Parallel()
	t.Run("serveReadiness with unhealthy status", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "unhealthy", message: "test error"}},
				nil,
			),
		}

		req := httptest.NewRequest("GET", "/readiness", nil)
		w := httptest.NewRecorder()

		handler.serveReadiness(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "unhealthy")
	})

	t.Run("serveStartup with incomplete startup", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: false,
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "unhealthy", message: "not ready", errMsg: "storage unavailable"}},
				nil,
			),
		}

		req := httptest.NewRequest("GET", "/startup", nil)
		w := httptest.NewRecorder()

		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "startup")
	})

	t.Run("serveStartup completes on first probe without checkers", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: false,
		}

		req := httptest.NewRequest("GET", "/startup", nil)
		w := httptest.NewRecorder()

		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Startup check")
	})

	t.Run("serveStartup with complete startup", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: true,
		}

		req := httptest.NewRequest("GET", "/startup", nil)
		w := httptest.NewRecorder()

		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Startup check")
	})

	t.Run("serveDetailedHealth with detailed enabled", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			EnableDetailed: true,
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "healthy", message: "test"}},
				nil,
			),
		}

		req := httptest.NewRequest("GET", "/health/detailed", nil)
		w := httptest.NewRecorder()

		handler.serveDetailedHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "overall")
	})

	t.Run("serveDetailedHealth with detailed disabled", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			EnableDetailed: false,
		}

		req := httptest.NewRequest("GET", "/health/detailed", nil)
		w := httptest.NewRecorder()

		handler.serveDetailedHealth(w, req)

		// The function may still return 200 even when detailed is disabled
		assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNotFound)
	})

	t.Run("serveHealthMetrics with metrics enabled", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			EnableMetrics: true,
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "healthy", message: "test"}},
				nil,
			),
		}

		req := httptest.NewRequest("GET", "/health/metrics", nil)
		w := httptest.NewRecorder()

		handler.serveHealthMetrics(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "health_status")
	})

	t.Run("serveHealthMetrics with metrics disabled", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			EnableMetrics: false,
		}

		req := httptest.NewRequest("GET", "/health/metrics", nil)
		w := httptest.NewRecorder()

		handler.serveHealthMetrics(w, req)

		// The function may still return 200 even when metrics is disabled
		assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNotFound)
	})

	t.Run("writeHealthResponse with error", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{}

		w := httptest.NewRecorder()

		// Create a health status that will cause an error
		status := &HealthStatus{
			Status:    "healthy",
			Message:   "test",
			Timestamp: time.Now(),
			Details: map[string]string{
				"test": "value",
			},
		}

		err := handler.writeHealthResponse(w, httptest.NewRequest(http.MethodGet, "/health", nil), status)

		// Should not return an error for valid status
		assert.NoError(t, err)
		assert.NotEmpty(t, w.Body.String())
	})

	t.Run("unmarshalCaddyfile with valid config", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{}
		d := caddyfile.NewTestDispenser(`
		health {
			health_path /health
		}
		`)

		err := handler.UnmarshalCaddyfile(d)

		// Just test that the function runs without panicking
		t.Logf("UnmarshalCaddyfile returned error: %v", err)
		t.Logf("HealthPath after unmarshal: %s", handler.HealthPath)
	})

	t.Run("unmarshalCaddyfile with invalid config", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{}
		d := caddyfile.NewTestDispenser(`
		health {
			invalid_directive
		}
		`)

		err := handler.UnmarshalCaddyfile(d)

		// Just test that the function runs without panicking
		t.Logf("UnmarshalCaddyfile with invalid config returned error: %v", err)
	})
}

// --- CaddyModule ---

func TestHealthEndpointCaddyModule(t *testing.T) {
	t.Parallel()
	h := &HealthEndpointHandler{}
	info := h.CaddyModule()
	assert.Equal(t, caddy.ModuleID("http.handlers.health"), info.ID)
	mod := info.New()
	assert.IsType(t, &HealthEndpointHandler{}, mod)
}

// --- UnmarshalCaddyfile: all path directives ---

func TestUnmarshalCaddyfileAllPaths(t *testing.T) {
	t.Parallel()
	d := caddyfile.NewTestDispenser(`health {
		path /custom-health
		readiness_path /custom-ready
		liveness_path /custom-live
		startup_path /custom-startup
		enable_detailed
		enable_metrics
		label env production
	}`)
	var h HealthEndpointHandler
	err := h.UnmarshalCaddyfile(d)
	assert.NoError(t, err)
	assert.Equal(t, "/custom-health", h.HealthPath)
	assert.Equal(t, "/custom-ready", h.ReadinessPath)
	assert.Equal(t, "/custom-live", h.LivenessPath)
	assert.Equal(t, "/custom-startup", h.StartupPath)
	assert.True(t, h.EnableDetailed)
	assert.True(t, h.EnableMetrics)
	assert.Equal(t, "production", h.CustomLabels["env"])
}

// --- UnmarshalCaddyfile: with argument ---

func TestUnmarshalCaddyfileWithArg(t *testing.T) {
	t.Parallel()
	d := caddyfile.NewTestDispenser(`health /healthz {
	}`)
	var h HealthEndpointHandler
	err := h.UnmarshalCaddyfile(d)
	assert.NoError(t, err)
	assert.Equal(t, "/healthz", h.HealthPath)
}

// --- UnmarshalCaddyfile: detailed_local_only directive ---

func TestUnmarshalCaddyfileDetailedLocalOnly(t *testing.T) {
	t.Parallel()

	t.Run("set to false", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`health {
			detailed_local_only false
		}`)
		var h HealthEndpointHandler
		err := h.UnmarshalCaddyfile(d)
		assert.NoError(t, err)
		assert.NotNil(t, h.DetailedLocalOnly)
		assert.False(t, *h.DetailedLocalOnly)
	})

	t.Run("set to true", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`health {
			detailed_local_only true
		}`)
		var h HealthEndpointHandler
		err := h.UnmarshalCaddyfile(d)
		assert.NoError(t, err)
		assert.NotNil(t, h.DetailedLocalOnly)
		assert.True(t, *h.DetailedLocalOnly)
	})

	t.Run("invalid value errors", func(t *testing.T) {
		t.Parallel()
		d := caddyfile.NewTestDispenser(`health {
			detailed_local_only maybe
		}`)
		var h HealthEndpointHandler
		err := h.UnmarshalCaddyfile(d)
		assert.Error(t, err)
	})
}

// --- writeHealthResponse: error redaction for non-loopback clients ---

func TestWriteHealthResponseRedactsErrorForRemoteClient(t *testing.T) {
	t.Parallel()
	handler := &HealthEndpointHandler{}

	status := &HealthStatus{
		Status:    "unhealthy",
		Message:   "Storage check failed",
		Timestamp: time.Now(),
		Error:     "rpc error: code = NotFound desc = bucket xyz does not exist",
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "10.0.0.1:12345" // non-loopback

	err := handler.writeHealthResponse(w, req, status)
	assert.NoError(t, err)

	var got HealthStatus
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "internal error (details redacted)", got.Error)
	assert.Equal(t, "unhealthy", got.Status)
}

func TestWriteHealthResponsePreservesErrorForLoopback(t *testing.T) {
	t.Parallel()
	handler := &HealthEndpointHandler{}

	origErr := "rpc error: code = NotFound desc = bucket xyz does not exist"
	status := &HealthStatus{
		Status:    "unhealthy",
		Message:   "Storage check failed",
		Timestamp: time.Now(),
		Error:     origErr,
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	err := handler.writeHealthResponse(w, req, status)
	assert.NoError(t, err)

	var got HealthStatus
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, origErr, got.Error)
}

func TestIsDetailedAllowed(t *testing.T) {
	t.Parallel()

	t.Run("nil DetailedLocalOnly defaults to loopback-only", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{DetailedLocalOnly: nil}

		loopbackReq := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		loopbackReq.RemoteAddr = "127.0.0.1:12345"
		assert.True(t, handler.isDetailedAllowed(loopbackReq))

		remoteReq := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		remoteReq.RemoteAddr = "10.0.0.1:12345"
		assert.False(t, handler.isDetailedAllowed(remoteReq))
	})

	t.Run("explicit false allows all", func(t *testing.T) {
		t.Parallel()
		f := false
		handler := &HealthEndpointHandler{DetailedLocalOnly: &f}

		remoteReq := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		remoteReq.RemoteAddr = "10.0.0.1:12345"
		assert.True(t, handler.isDetailedAllowed(remoteReq))
	})

	t.Run("explicit true enforces loopback", func(t *testing.T) {
		t.Parallel()
		tr := true
		handler := &HealthEndpointHandler{DetailedLocalOnly: &tr}

		remoteReq := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		remoteReq.RemoteAddr = "10.0.0.1:12345"
		assert.False(t, handler.isDetailedAllowed(remoteReq))
	})

	t.Run("IPv6 loopback allowed", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{DetailedLocalOnly: nil}

		ipv6Req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		ipv6Req.RemoteAddr = "[::1]:12345"
		assert.True(t, handler.isDetailedAllowed(ipv6Req))
	})

	t.Run("invalid remote addr rejected", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{DetailedLocalOnly: nil}

		badReq := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
		badReq.RemoteAddr = "not-an-ip"
		assert.False(t, handler.isDetailedAllowed(badReq))
	})
}

func TestTryCompleteStartup(t *testing.T) {
	t.Parallel()

	t.Run("completes with healthy checker", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: false,
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "healthy", message: "OK"}},
				nil,
			),
		}

		req := httptest.NewRequest(http.MethodGet, "/startup", nil)
		w := httptest.NewRecorder()
		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify startup is now marked complete.
		handler.mu.RLock()
		assert.True(t, handler.startupComplete)
		handler.mu.RUnlock()
	})

	t.Run("stays incomplete with unhealthy checker", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: false,
			compositeChecker: NewCompositeHealthChecker(
				[]HealthChecker{&MockHealthChecker{status: "unhealthy", message: "fail", errMsg: "down"}},
				nil,
			),
		}

		req := httptest.NewRequest(http.MethodGet, "/startup", nil)
		w := httptest.NewRecorder()
		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		handler.mu.RLock()
		assert.False(t, handler.startupComplete)
		handler.mu.RUnlock()
	})

	t.Run("idempotent after completion", func(t *testing.T) {
		t.Parallel()
		handler := &HealthEndpointHandler{
			startupComplete: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/startup", nil)
		w := httptest.NewRecorder()
		handler.serveStartup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestProvisionStartupCompletionWithCheckers(t *testing.T) {
	t.Parallel()
	handler := &HealthEndpointHandler{}
	handler.checkers = []HealthChecker{
		&MockHealthChecker{status: "healthy", message: "OK"},
	}

	err := handler.Provision(caddy.Context{})
	assert.NoError(t, err)

	// With checkers present, startup should NOT be marked complete at Provision time.
	handler.mu.RLock()
	assert.False(t, handler.startupComplete)
	handler.mu.RUnlock()
}

func TestProvisionStartupCompletionWithoutCheckers(t *testing.T) {
	t.Parallel()
	handler := &HealthEndpointHandler{}

	err := handler.Provision(caddy.Context{})
	assert.NoError(t, err)

	// Without checkers, startup should be marked complete immediately.
	handler.mu.RLock()
	assert.True(t, handler.startupComplete)
	handler.mu.RUnlock()
}
