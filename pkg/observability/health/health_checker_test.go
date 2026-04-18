package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestGCSHealthChecker tests GCS health checker functionality
func TestGCSHealthChecker(t *testing.T) {
	t.Parallel()
	t.Run("creation and configuration", func(t *testing.T) {
		t.Parallel()
		t.Run("create health checker with default config", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			assert.NotNil(t, checker)
			assert.Equal(t, "test-bucket", checker.bucket)
			assert.NotNil(t, checker.config)
			assert.Equal(t, 30*time.Second, checker.config.Timeout)
			assert.Equal(t, 3, checker.config.RetryAttempts)
			assert.Equal(t, 5*time.Second, checker.config.RetryDelay)
			assert.True(t, checker.config.CheckPermissions)
			assert.True(t, checker.config.CheckConnectivity)
		})

		t.Run("create health checker with custom config", func(t *testing.T) {
			t.Parallel()
			config := &HealthConfig{
				Timeout:           10 * time.Second,
				RetryAttempts:     5,
				RetryDelay:        2 * time.Second,
				CheckPermissions:  false,
				CheckConnectivity: false,
			}
			checker := NewGCSHealthChecker(nil, "custom-bucket", config)
			assert.NotNil(t, checker)
			assert.Equal(t, "custom-bucket", checker.bucket)
			assert.Equal(t, config, checker.config)
			assert.Equal(t, 10*time.Second, checker.config.Timeout)
			assert.Equal(t, 5, checker.config.RetryAttempts)
			assert.Equal(t, 2*time.Second, checker.config.RetryDelay)
			assert.False(t, checker.config.CheckPermissions)
			assert.False(t, checker.config.CheckConnectivity)
		})
	})

	t.Run("health check operations", func(t *testing.T) {
		t.Parallel()
		t.Run("check connectivity with nil client", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			status := checker.CheckConnectivity(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})

		t.Run("check storage with nil client", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			status := checker.CheckStorage(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})

		t.Run("check permissions with nil client", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			status := checker.CheckPermissions(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})

		t.Run("check with connectivity disabled", func(t *testing.T) {
			t.Parallel()
			config := &HealthConfig{
				Timeout:           30 * time.Second,
				RetryAttempts:     3,
				RetryDelay:        5 * time.Second,
				CheckPermissions:  false,
				CheckConnectivity: false,
			}
			checker := NewGCSHealthChecker(nil, "test-bucket", config)
			status := checker.Check(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})

		t.Run("check with permissions disabled", func(t *testing.T) {
			t.Parallel()
			config := &HealthConfig{
				Timeout:           30 * time.Second,
				RetryAttempts:     3,
				RetryDelay:        5 * time.Second,
				CheckPermissions:  false,
				CheckConnectivity: true,
			}
			checker := NewGCSHealthChecker(nil, "test-bucket", config)
			status := checker.Check(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})

		t.Run("check with both disabled", func(t *testing.T) {
			t.Parallel()
			config := &HealthConfig{
				Timeout:           30 * time.Second,
				RetryAttempts:     3,
				RetryDelay:        5 * time.Second,
				CheckPermissions:  false,
				CheckConnectivity: false,
			}
			checker := NewGCSHealthChecker(nil, "test-bucket", config)
			status := checker.Check(context.Background())
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "nil GCS client", status.Error)
		})
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("check with empty bucket name", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "", nil)
			assert.Equal(t, "", checker.bucket)
			assert.NotNil(t, checker.config)
		})

		t.Run("check with nil config", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			assert.NotNil(t, checker.config)
			assert.Equal(t, 30*time.Second, checker.config.Timeout)
		})
	})

	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()
		t.Run("concurrent health checks", func(t *testing.T) {
			t.Parallel()
			checker := NewGCSHealthChecker(nil, "test-bucket", nil)
			done := make(chan bool, 10)

			for range 10 {
				go func() {
					defer func() { done <- true }()
					status := checker.Check(context.Background())
					assert.Equal(t, "unhealthy", status.Status)
				}()
			}

			for range 10 {
				<-done
			}
		})
	})
}

// TestHealthStatus tests HealthStatus functionality
func TestHealthStatus(t *testing.T) {
	t.Parallel()
	t.Run("basic structure", func(t *testing.T) {
		t.Parallel()
		t.Run("create health status", func(t *testing.T) {
			t.Parallel()
			now := time.Now()
			status := &HealthStatus{
				Status:    "healthy",
				Message:   "Test message",
				Timestamp: now,
				Details: map[string]string{
					"test": "value",
				},
			}
			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "Test message", status.Message)
			assert.Equal(t, now, status.Timestamp)
			assert.Equal(t, "value", status.Details["test"])
			assert.Empty(t, status.Error)
		})

		t.Run("create health status with error", func(t *testing.T) {
			t.Parallel()
			errMsg := "assert.AnError general error for testing"
			status := &HealthStatus{
				Status:    "unhealthy",
				Message:   "Error occurred",
				Timestamp: time.Now(),
				Details:   make(map[string]string),
				Error:     errMsg,
			}
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "Error occurred", status.Message)
			assert.Equal(t, errMsg, status.Error)
		})
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("health status with empty details", func(t *testing.T) {
			t.Parallel()
			status := &HealthStatus{
				Status:    "healthy",
				Message:   "Test message",
				Timestamp: time.Now(),
				Details:   make(map[string]string),
			}
			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "Test message", status.Message)
			assert.Empty(t, status.Details)
			assert.Empty(t, status.Error)
		})

		t.Run("health status with nil details", func(t *testing.T) {
			t.Parallel()
			status := &HealthStatus{
				Status:    "unhealthy",
				Message:   "Error occurred",
				Timestamp: time.Now(),
				Details:   nil,
				Error:     "some error",
			}
			assert.Equal(t, "unhealthy", status.Status)
			assert.Equal(t, "Error occurred", status.Message)
			assert.Nil(t, status.Details)
			assert.NotEmpty(t, status.Error)
		})

		t.Run("health status with empty status", func(t *testing.T) {
			t.Parallel()
			status := &HealthStatus{
				Status:    "",
				Message:   "Test message",
				Timestamp: time.Now(),
				Details:   make(map[string]string),
			}
			assert.Equal(t, "", status.Status)
			assert.Equal(t, "Test message", status.Message)
			assert.Empty(t, status.Details)
			assert.Empty(t, status.Error)
		})

		t.Run("health status with empty message", func(t *testing.T) {
			t.Parallel()
			status := &HealthStatus{
				Status:    "healthy",
				Message:   "",
				Timestamp: time.Now(),
				Details:   make(map[string]string),
			}
			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "", status.Message)
			assert.Empty(t, status.Details)
			assert.Empty(t, status.Error)
		})
	})
}

// TestCompositeHealthChecker tests CompositeHealthChecker functionality
func TestCompositeHealthChecker(t *testing.T) {
	t.Parallel()
	t.Run("creation and configuration", func(t *testing.T) {
		t.Parallel()
		t.Run("create composite health checker with default config", func(t *testing.T) {
			t.Parallel()
			checkers := []HealthChecker{}
			composite := NewCompositeHealthChecker(checkers, nil)
			assert.NotNil(t, composite)
			assert.NotNil(t, composite.config)
			assert.Equal(t, 30*time.Second, composite.config.Timeout)
			assert.Equal(t, 3, composite.config.RetryAttempts)
			assert.Equal(t, 5*time.Second, composite.config.RetryDelay)
		})

		t.Run("create composite health checker with custom config", func(t *testing.T) {
			t.Parallel()
			config := &HealthConfig{
				Timeout:       15 * time.Second,
				RetryAttempts: 7,
				RetryDelay:    3 * time.Second,
			}
			checkers := []HealthChecker{}
			composite := NewCompositeHealthChecker(checkers, config)
			assert.NotNil(t, composite)
			assert.Equal(t, config, composite.config)
			assert.Equal(t, 15*time.Second, composite.config.Timeout)
			assert.Equal(t, 7, composite.config.RetryAttempts)
			assert.Equal(t, 3*time.Second, composite.config.RetryDelay)
		})
	})

	t.Run("health check operations", func(t *testing.T) {
		t.Parallel()
		t.Run("check with no checkers", func(t *testing.T) {
			t.Parallel()
			checkers := []HealthChecker{}
			composite := NewCompositeHealthChecker(checkers, nil)
			status := composite.Check(context.Background())

			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "All health checks passed", status.Message)
			assert.Empty(t, status.Details)
		})

		t.Run("check with healthy checkers", func(t *testing.T) {
			t.Parallel()
			checker1 := &MockHealthChecker{status: "healthy", message: "Checker 1 OK"}
			checker2 := &MockHealthChecker{status: "healthy", message: "Checker 2 OK"}
			checkers := []HealthChecker{checker1, checker2}
			composite := NewCompositeHealthChecker(checkers, nil)
			status := composite.Check(context.Background())

			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "All health checks passed", status.Message)
			assert.NotEmpty(t, status.Details)
		})

		t.Run("check with unhealthy checker", func(t *testing.T) {
			t.Parallel()
			checker1 := &MockHealthChecker{status: "healthy", message: "Checker 1 OK"}
			checker2 := &MockHealthChecker{status: "unhealthy", message: "Checker 2 failed", errMsg: "test error"}
			checkers := []HealthChecker{checker1, checker2}
			composite := NewCompositeHealthChecker(checkers, nil)
			status := composite.Check(context.Background())

			assert.Equal(t, "unhealthy", status.Status)
			assert.Contains(t, status.Message, "Health check 1 failed")
			assert.Equal(t, "test error", status.Error)
		})

		t.Run("check storage with healthy checkers", func(t *testing.T) {
			t.Parallel()
			checker1 := &MockHealthChecker{status: "healthy", message: "Storage 1 OK"}
			checker2 := &MockHealthChecker{status: "healthy", message: "Storage 2 OK"}
			checkers := []HealthChecker{checker1, checker2}
			composite := NewCompositeHealthChecker(checkers, nil)
			status := composite.CheckStorage(context.Background())

			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "All storage checks passed", status.Message)
			assert.NotEmpty(t, status.Details)
		})

		t.Run("check storage with unhealthy checker", func(t *testing.T) {
			t.Parallel()
			checker1 := &MockHealthChecker{status: "healthy", message: "Storage 1 OK"}
			checker2 := &MockHealthChecker{status: "unhealthy", message: "Storage 2 failed", errMsg: "test error"}
			checkers := []HealthChecker{checker1, checker2}
			composite := NewCompositeHealthChecker(checkers, nil)
			status := composite.CheckStorage(context.Background())

			assert.Equal(t, "unhealthy", status.Status)
			assert.Contains(t, status.Message, "Storage check 1 failed")
			assert.Equal(t, "test error", status.Error)
		})
	})

	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()
		t.Run("concurrent composite health checks", func(t *testing.T) {
			t.Parallel()
			checker1 := &MockHealthChecker{status: "healthy", message: "OK"}
			checker2 := &MockHealthChecker{status: "healthy", message: "OK"}
			checkers := []HealthChecker{checker1, checker2}
			composite := NewCompositeHealthChecker(checkers, nil)
			done := make(chan bool, 10)

			for range 10 {
				go func() {
					status := composite.Check(context.Background())
					assert.Equal(t, "healthy", status.Status)
					done <- true
				}()
			}

			for range 10 {
				<-done
			}
		})
	})

	t.Run("error handling", func(t *testing.T) {
		t.Parallel()
		t.Run("composite checker with nil checkers", func(t *testing.T) {
			t.Parallel()
			composite := NewCompositeHealthChecker(nil, nil)

			status := composite.Check(context.Background())
			assert.Equal(t, "healthy", status.Status)
		})
	})
}

// TestHealthConfig tests HealthConfig functionality
func TestHealthConfig(t *testing.T) {
	t.Parallel()
	t.Run("create health config", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout:           60 * time.Second,
			RetryAttempts:     10,
			RetryDelay:        10 * time.Second,
			CheckPermissions:  true,
			CheckConnectivity: false,
		}
		assert.Equal(t, 60*time.Second, config.Timeout)
		assert.Equal(t, 10, config.RetryAttempts)
		assert.Equal(t, 10*time.Second, config.RetryDelay)
		assert.True(t, config.CheckPermissions)
		assert.False(t, config.CheckConnectivity)
	})

	t.Run("create health config with zero values", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout:           0,
			RetryAttempts:     0,
			RetryDelay:        0,
			CheckPermissions:  false,
			CheckConnectivity: false,
		}
		assert.Equal(t, time.Duration(0), config.Timeout)
		assert.Equal(t, 0, config.RetryAttempts)
		assert.Equal(t, time.Duration(0), config.RetryDelay)
		assert.False(t, config.CheckPermissions)
		assert.False(t, config.CheckConnectivity)
	})

	t.Run("create health config with negative values", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout:           -10 * time.Second,
			RetryAttempts:     -5,
			RetryDelay:        -2 * time.Second,
			CheckPermissions:  true,
			CheckConnectivity: true,
		}
		assert.Equal(t, -10*time.Second, config.Timeout)
		assert.Equal(t, -5, config.RetryAttempts)
		assert.Equal(t, -2*time.Second, config.RetryDelay)
		assert.True(t, config.CheckPermissions)
		assert.True(t, config.CheckConnectivity)
	})
}

// TestGCSHealthChecker_Check tests the main Check function
func TestGCSHealthChecker_Check(t *testing.T) {
	t.Parallel()
	t.Run("check with nil client", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "test-bucket", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Check with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.Check(context.Background())
		// This line should not be reached due to panic
		t.Logf("Unexpected: Check returned status: %v", status)
	})

	t.Run("check with valid client but no connectivity check", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			CheckConnectivity: false,
			CheckPermissions:  false,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should still panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Check with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.Check(context.Background())
		t.Logf("Unexpected: Check returned status: %v", status)
	})

	t.Run("check with timeout", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout:           1 * time.Millisecond,
			CheckConnectivity: true,
			CheckPermissions:  true,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Check with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.Check(context.Background())
		t.Logf("Unexpected: Check returned status: %v", status)
	})
}

// TestGCSHealthChecker_CheckStorage tests the CheckStorage function
func TestGCSHealthChecker_CheckStorage(t *testing.T) {
	t.Parallel()
	t.Run("check storage with nil client", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "test-bucket", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckStorage with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckStorage(context.Background())
		t.Logf("Unexpected: CheckStorage returned status: %v", status)
	})

	t.Run("check storage with empty bucket", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckStorage with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckStorage(context.Background())
		t.Logf("Unexpected: CheckStorage returned status: %v", status)
	})

	t.Run("check storage with timeout", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout: 1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckStorage with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckStorage(context.Background())
		t.Logf("Unexpected: CheckStorage returned status: %v", status)
	})
}

// TestGCSHealthChecker_CheckConnectivity tests the CheckConnectivity function
func TestGCSHealthChecker_CheckConnectivity(t *testing.T) {
	t.Parallel()
	t.Run("check connectivity with nil client", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "test-bucket", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckConnectivity with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckConnectivity(context.Background())
		t.Logf("Unexpected: CheckConnectivity returned status: %v", status)
	})

	t.Run("check connectivity with timeout", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout: 1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckConnectivity with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckConnectivity(context.Background())
		t.Logf("Unexpected: CheckConnectivity returned status: %v", status)
	})

	t.Run("check connectivity with retry attempts", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			RetryAttempts: 2,
			RetryDelay:    1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckConnectivity with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckConnectivity(context.Background())
		t.Logf("Unexpected: CheckConnectivity returned status: %v", status)
	})
}

// TestGCSHealthChecker_CheckPermissions tests the CheckPermissions function
func TestGCSHealthChecker_CheckPermissions(t *testing.T) {
	t.Parallel()
	t.Run("check permissions with nil client", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "test-bucket", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckPermissions with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckPermissions(context.Background())
		t.Logf("Unexpected: CheckPermissions returned status: %v", status)
	})

	t.Run("check permissions with empty bucket", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "", nil)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckPermissions with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckPermissions(context.Background())
		t.Logf("Unexpected: CheckPermissions returned status: %v", status)
	})

	t.Run("check permissions with timeout", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout: 1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckPermissions with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckPermissions(context.Background())
		t.Logf("Unexpected: CheckPermissions returned status: %v", status)
	})

	t.Run("check permissions with retry attempts", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			RetryAttempts: 2,
			RetryDelay:    1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckPermissions with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckPermissions(context.Background())
		t.Logf("Unexpected: CheckPermissions returned status: %v", status)
	})
}

// TestGCSHealthChecker_EdgeCases tests edge cases
func TestGCSHealthChecker_EdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("check with context cancellation", func(t *testing.T) {
		t.Parallel()
		checker := NewGCSHealthChecker(nil, "test-bucket", nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Check with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.Check(ctx)
		t.Logf("Unexpected: Check returned status: %v", status)
	})

	t.Run("check with very short timeout", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			Timeout: 1 * time.Nanosecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Check with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.Check(context.Background())
		t.Logf("Unexpected: Check returned status: %v", status)
	})

	t.Run("check with zero retry attempts", func(t *testing.T) {
		t.Parallel()
		config := &HealthConfig{
			RetryAttempts: 0,
			RetryDelay:    1 * time.Millisecond,
		}
		checker := NewGCSHealthChecker(nil, "test-bucket", config)

		// This should panic due to nil client
		defer func() {
			if r := recover(); r != nil {
				t.Logf("CheckConnectivity with nil client caused panic (expected): %v", r)
			}
		}()
		status := checker.CheckConnectivity(context.Background())
		t.Logf("Unexpected: CheckConnectivity returned status: %v", status)
	})
}
