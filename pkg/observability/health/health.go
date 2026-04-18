package health

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// HealthChecker defines the interface for health checks.
// Implementations must provide a comprehensive Check and a
// storage-specific CheckStorage. Additional component checks
// (cache, metrics) can be added via optional interfaces in the future.
type HealthChecker interface {
	Check(ctx context.Context) *HealthStatus
	CheckStorage(ctx context.Context) *HealthStatus
}

// HealthStatus represents the health status of a component
type HealthStatus struct {
	Status    string            `json:"status"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details"`
	Error     string            `json:"error,omitempty"`
}

// GCSHealthChecker implements health checks for GCS
type GCSHealthChecker struct {
	client *storage.Client
	bucket string
	config *HealthConfig
}

// Defaults for health check configuration.
const (
	defaultCheckerTimeout = 30 * time.Second
	defaultRetryAttempts  = 3
	defaultRetryDelay     = 5 * time.Second
)

// HealthConfig defines health check configuration
type HealthConfig struct {
	Timeout           time.Duration `json:"timeout"`
	RetryAttempts     int           `json:"retry_attempts"`
	RetryDelay        time.Duration `json:"retry_delay"`
	CheckPermissions  bool          `json:"check_permissions"`
	CheckConnectivity bool          `json:"check_connectivity"`
}

// Compile-time interface guard.
var _ HealthChecker = (*GCSHealthChecker)(nil)

// NewGCSHealthChecker creates a new GCS health checker.
// client may be nil; all methods degrade gracefully (return unhealthy) rather than panicking.
func NewGCSHealthChecker(client *storage.Client, bucket string, config *HealthConfig) *GCSHealthChecker {
	if config == nil {
		config = &HealthConfig{
			Timeout:           defaultCheckerTimeout,
			RetryAttempts:     defaultRetryAttempts,
			RetryDelay:        defaultRetryDelay,
			CheckPermissions:  true,
			CheckConnectivity: true,
		}
	}

	return &GCSHealthChecker{
		client: client,
		bucket: bucket,
		config: config,
	}
}

// Check performs a comprehensive health check
func (h *GCSHealthChecker) Check(ctx context.Context) *HealthStatus {
	if h.client == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "GCS client is not configured",
			Timestamp: time.Now(),
			Details:   map[string]string{"bucket": h.bucket, "error_type": "no_client"},
			Error:     "nil GCS client",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, h.config.Timeout)
	defer cancel()

	// Check connectivity if enabled
	if h.config.CheckConnectivity {
		connectivityStatus := h.CheckConnectivity(ctx)
		if connectivityStatus.Status != "healthy" {
			return connectivityStatus
		}
	}

	// Check storage
	storageStatus := h.CheckStorage(ctx)
	if storageStatus.Status != "healthy" {
		return storageStatus
	}

	// Check permissions if enabled
	if h.config.CheckPermissions {
		permissionStatus := h.CheckPermissions(ctx)
		if permissionStatus.Status != "healthy" {
			return permissionStatus
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Message:   "All health checks passed",
		Timestamp: time.Now(),
		Details: map[string]string{
			"bucket":       h.bucket,
			"connectivity": "ok",
			"storage":      "ok",
			"permissions":  "ok",
			"last_check":   time.Now().Format(time.RFC3339),
		},
	}
}

// CheckStorage checks storage functionality
func (h *GCSHealthChecker) CheckStorage(ctx context.Context) *HealthStatus {
	if h.client == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "GCS client is not configured",
			Timestamp: time.Now(),
			Details:   map[string]string{"bucket": h.bucket, "error_type": "no_client"},
			Error:     "nil GCS client",
		}
	}

	// Try to list objects to check storage access. Set a prefix
	// delimiter so the server only scans top-level entries.
	it := h.client.Bucket(h.bucket).Objects(ctx, &storage.Query{
		Delimiter: "/",
	})

	// Try to get the first object (limit to 1)
	_, err := it.Next()
	if err != nil && !errors.Is(err, iterator.Done) {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "Storage access failed",
			Timestamp: time.Now(),
			Details: map[string]string{
				"bucket":     h.bucket,
				"error_type": "storage_access",
			},
			Error: fmt.Sprintf("failed to access storage: %v", err),
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Message:   "Storage access is working",
		Timestamp: time.Now(),
		Details: map[string]string{
			"bucket": h.bucket,
			"status": "storage_ok",
		},
	}
}

// CheckConnectivity checks GCS connectivity
func (h *GCSHealthChecker) CheckConnectivity(ctx context.Context) *HealthStatus {
	if h.client == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "GCS client is not configured",
			Timestamp: time.Now(),
			Details:   map[string]string{"bucket": h.bucket, "error_type": "no_client"},
			Error:     "nil GCS client",
		}
	}

	// Try to get bucket attributes to check connectivity
	_, err := h.client.Bucket(h.bucket).Attrs(ctx)
	if err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "GCS connectivity failed",
			Timestamp: time.Now(),
			Details: map[string]string{
				"bucket":     h.bucket,
				"error_type": "connectivity",
			},
			Error: fmt.Sprintf("failed to connect to GCS: %v", err),
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Message:   "GCS connectivity is working",
		Timestamp: time.Now(),
		Details: map[string]string{
			"bucket": h.bucket,
			"status": "connectivity_ok",
		},
	}
}

// CheckPermissions checks bucket permissions using read-only operations
func (h *GCSHealthChecker) CheckPermissions(ctx context.Context) *HealthStatus {
	if h.client == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "GCS client is not configured",
			Timestamp: time.Now(),
			Details:   map[string]string{"bucket": h.bucket, "error_type": "no_client"},
			Error:     "nil GCS client",
		}
	}

	// Test read permissions by checking if we can test IAM permissions
	perms, err := h.client.Bucket(h.bucket).IAM().TestPermissions(ctx, []string{"storage.objects.list"})
	if err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "Permission check failed",
			Timestamp: time.Now(),
			Details: map[string]string{
				"bucket":     h.bucket,
				"error_type": "permission_check",
			},
			Error: fmt.Sprintf("failed to check permissions: %v", err),
		}
	}

	if len(perms) == 0 {
		return &HealthStatus{
			Status:    "unhealthy",
			Message:   "Insufficient permissions",
			Timestamp: time.Now(),
			Details: map[string]string{
				"bucket":     h.bucket,
				"error_type": "insufficient_permissions",
			},
			Error: "no permissions granted on bucket",
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Message:   "Permissions are working",
		Timestamp: time.Now(),
		Details: map[string]string{
			"bucket": h.bucket,
			"status": "permissions_ok",
		},
	}
}

// Compile-time interface guard.
var _ HealthChecker = (*CompositeHealthChecker)(nil)

// CompositeHealthChecker combines multiple health checkers
type CompositeHealthChecker struct {
	checkers []HealthChecker
	config   *HealthConfig
}

// NewCompositeHealthChecker creates a new composite health checker
func NewCompositeHealthChecker(checkers []HealthChecker, config *HealthConfig) *CompositeHealthChecker {
	if config == nil {
		config = &HealthConfig{
			Timeout:       30 * time.Second,
			RetryAttempts: 3,
			RetryDelay:    5 * time.Second,
		}
	}

	return &CompositeHealthChecker{
		checkers: checkers,
		config:   config,
	}
}

// Check performs health checks on all components.
// Sub-checkers manage their own timeouts; no additional timeout is added here
// to avoid confusing nested deadline behavior.
func (c *CompositeHealthChecker) Check(ctx context.Context) *HealthStatus {
	allDetails := make(map[string]string)

	// Run all health checks
	for i, checker := range c.checkers {
		status := checker.Check(ctx)

		// Add details from this checker
		for key, value := range status.Details {
			allDetails[fmt.Sprintf("checker_%d_%s", i, key)] = value
		}

		// If any checker is unhealthy, return unhealthy status
		if status.Status != "healthy" {
			return &HealthStatus{
				Status:    "unhealthy",
				Message:   fmt.Sprintf("Health check %d failed: %s", i, status.Message),
				Timestamp: time.Now(),
				Details:   allDetails,
				Error:     status.Error,
			}
		}
	}

	// All checks passed
	return &HealthStatus{
		Status:    "healthy",
		Message:   "All health checks passed",
		Timestamp: time.Now(),
		Details:   allDetails,
	}
}

// CheckStorage checks storage health for all checkers
func (c *CompositeHealthChecker) CheckStorage(ctx context.Context) *HealthStatus {
	allDetails := make(map[string]string)

	for i, checker := range c.checkers {
		status := checker.CheckStorage(ctx)

		for key, value := range status.Details {
			allDetails[fmt.Sprintf("checker_%d_%s", i, key)] = value
		}

		if status.Status != "healthy" {
			return &HealthStatus{
				Status:    "unhealthy",
				Message:   fmt.Sprintf("Storage check %d failed: %s", i, status.Message),
				Timestamp: time.Now(),
				Details:   allDetails,
				Error:     status.Error,
			}
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Message:   "All storage checks passed",
		Timestamp: time.Now(),
		Details:   allDetails,
	}
}
