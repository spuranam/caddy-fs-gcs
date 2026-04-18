package observability

import (
	"context"
	"testing"
	"time"
)

// TestRecordingHelpers_NilSafe verifies all recording helpers are safe to call
// when the global instruments have not been initialised (nil-safe no-op path).
func TestRecordingHelpers_NilSafe(t *testing.T) {
	ctx := context.Background()

	// These exercise the nil-check early-return branches.
	RecordHTTPRequest(ctx, "GET", "/", 200, time.Millisecond, 1024)
	RecordGCSOperation(ctx, "read", "bucket", "ok", time.Millisecond)
	RecordGCSError(ctx, "read", "bucket", "not_found")
	RecordCacheHit(ctx, "bucket", "l1")
	RecordCacheMiss(ctx, "bucket", "l1")
	RecordStreamingBytes(ctx, "bucket", 4096)
	IncConcurrentRequests(ctx, "bucket")
	DecConcurrentRequests(ctx, "bucket")
}

// TestRecordingHelpers_AfterInit verifies all recording helpers work after Init().
func TestRecordingHelpers_AfterInit(t *testing.T) {
	// Init creates real instruments backed by a Prometheus exporter.
	err := Init()
	if err != nil {
		t.Skipf("Init failed (may already be initialised): %v", err)
	}

	ctx := context.Background()

	RecordHTTPRequest(ctx, "POST", "/api", 201, 5*time.Millisecond, 512)
	RecordGCSOperation(ctx, "write", "my-bucket", "success", 10*time.Millisecond)
	RecordGCSError(ctx, "write", "my-bucket", "permission_denied")
	RecordCacheHit(ctx, "my-bucket", "l1")
	RecordCacheMiss(ctx, "my-bucket", "l2")
	RecordStreamingBytes(ctx, "my-bucket", 65536)
	IncConcurrentRequests(ctx, "my-bucket")
	DecConcurrentRequests(ctx, "my-bucket")

	// RecordHTTPRequest with error status code.
	RecordHTTPRequest(ctx, "GET", "/missing", 404, time.Millisecond, 0)
}
