package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func resetTracing() {
	initMu.Lock()
	initialized = false
	shutdownFunc = nil
	tp = nil
	tracer = nil
	initMu.Unlock()
}

func TestInit_NoneExporter(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "none", "test", "", "", 1.0)
	require.NoError(t, err)
	assert.NotNil(t, GetTracer())

	initMu.Lock()
	assert.True(t, initialized)
	initMu.Unlock()
}

func TestInit_EmptyExporter(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "", "test", "", "", 1.0)
	require.NoError(t, err)
	assert.NotNil(t, GetTracer())
}

func TestInit_StdoutExporter(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "stdout", "production", "", "", 0.5)
	require.NoError(t, err)
	assert.NotNil(t, GetTracer())

	// Cleanup
	err = Shutdown(context.Background())
	require.NoError(t, err)
}

func TestInit_UnknownExporter(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "invalid", "", "", "", 1.0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown exporter type")
}

func TestInit_Idempotent(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "none", "", "", "", 1.0)
	require.NoError(t, err)

	// Second call should be a no-op
	err = Init(context.Background(), "none", "", "", "", 1.0)
	require.NoError(t, err)
}

func TestInit_OTLPMissingEndpoint(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "otlp", "", "", "grpc", 1.0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OTLP endpoint is required")
}

func TestInit_OTLPUnsupportedProtocol(t *testing.T) {
	t.Cleanup(resetTracing)
	resetTracing()

	err := Init(context.Background(), "otlp", "", "localhost:4317", "websocket", 1.0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTLP protocol")
}

func TestInit_SamplerRates(t *testing.T) {
	tests := []struct {
		name string
		rate float64
	}{
		{"always sample", 1.0},
		{"never sample", 0.0},
		{"ratio sample", 0.5},
		{"over 1.0", 2.0},
		{"negative", -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTracing()
			t.Cleanup(func() {
				Shutdown(context.Background())
				resetTracing()
			})
			err := Init(context.Background(), "stdout", "", "", "", tt.rate)
			require.NoError(t, err)
		})
	}
}

func TestShutdown(t *testing.T) {
	t.Run("shutdown not initialized", func(t *testing.T) {
		resetTracing()
		err := Shutdown(context.Background())
		require.NoError(t, err)
	})

	t.Run("shutdown after init", func(t *testing.T) {
		resetTracing()
		err := Init(context.Background(), "stdout", "", "", "", 1.0)
		require.NoError(t, err)

		err = Shutdown(context.Background())
		require.NoError(t, err)

		initMu.Lock()
		assert.False(t, initialized)
		assert.Nil(t, shutdownFunc)
		assert.Nil(t, tp)
		// tracer is set to a no-op instance (not nil) to prevent panics.
		assert.NotNil(t, tracer)
		initMu.Unlock()
	})
}

func TestStartSpan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-operation",
		attribute.String("key", "value"),
	)
	defer span.End()

	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
}

func TestAddSpanAttributes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Should not panic even without an active recording span
	AddSpanAttributes(ctx, attribute.String("key", "value"))
}

func TestRecordSpanError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Should not panic even without an active recording span
	RecordSpanError(ctx, errors.New("test error"), attribute.String("key", "value"))
}

func TestStartSpan_WithActiveTracer(t *testing.T) {
	resetTracing()
	t.Cleanup(func() {
		Shutdown(context.Background())
		resetTracing()
	})

	err := Init(context.Background(), "stdout", "test", "", "", 1.0)
	require.NoError(t, err)

	ctx, span := StartSpan(context.Background(), "test-op",
		attribute.String("test.key", "test.value"),
	)
	defer span.End()

	assert.NotNil(t, ctx)
	// Span should be a recording span from the SDK provider
	assert.NotNil(t, span)
}

func TestParseOTLPEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpoint     string
		wantHost     string
		wantInsecure bool
	}{
		{"http URL", "http://localhost:4318", "localhost:4318", true},
		{"https URL", "https://otel.example.com:4317", "otel.example.com:4317", false},
		{"plain host:port", "localhost:4317", "localhost:4317", true},
		{"plain host", "otel-collector", "otel-collector", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host, insecure := parseOTLPEndpoint(tt.endpoint)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantInsecure, insecure)
		})
	}
}

func TestServiceInstanceID(t *testing.T) {
	t.Parallel()

	// Without POD_NAME env, should return hostname
	id := serviceInstanceID()
	assert.NotEmpty(t, id)
}

func TestCreateOTLPExporter_EmptyEndpoint(t *testing.T) {
	t.Parallel()

	_, err := createOTLPExporter(context.Background(), "", "grpc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OTLP endpoint is required")
}

func TestInit_WithEnvironment(t *testing.T) {
	resetTracing()
	t.Cleanup(func() {
		Shutdown(context.Background())
		resetTracing()
	})

	err := Init(context.Background(), "stdout", "staging", "", "", 1.0)
	require.NoError(t, err)
	assert.NotNil(t, GetTracer())
}

func TestRecordSpanError_WithRecordingSpan(t *testing.T) {
	resetTracing()
	t.Cleanup(func() {
		Shutdown(context.Background())
		resetTracing()
	})

	err := Init(context.Background(), "stdout", "test", "", "", 1.0)
	require.NoError(t, err)

	ctx, span := StartSpan(context.Background(), "error-test")
	defer span.End()

	// Should record error on the span
	RecordSpanError(ctx, errors.New("test error"),
		attribute.String("error.type", "test"),
	)

	// Should not panic
	AddSpanAttributes(ctx, attribute.Int("retry.count", 3))
}

func TestAddSpanAttributes_NonRecording(t *testing.T) {
	t.Parallel()

	// Background context has a non-recording span
	ctx := context.Background()
	span := trace.SpanFromContext(ctx)
	assert.False(t, span.IsRecording())

	// Should not panic
	AddSpanAttributes(ctx, attribute.String("key", "value"))
}

func TestServiceInstanceID_WithPodName(t *testing.T) {
	t.Setenv("POD_NAME", "my-pod-abc123")
	id := serviceInstanceID()
	assert.Equal(t, "my-pod-abc123", id)
}

func TestCreateOTLPExporter_GRPCInsecure(t *testing.T) {
	t.Parallel()
	// Create exporter with insecure gRPC endpoint
	// Will fail to connect but exercises the code path
	exp, err := createOTLPExporter(context.Background(), "http://localhost:4317", "grpc")
	require.NoError(t, err)
	assert.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}

func TestCreateOTLPExporter_HTTPInsecure(t *testing.T) {
	t.Parallel()
	exp, err := createOTLPExporter(context.Background(), "http://localhost:4318", "http")
	require.NoError(t, err)
	assert.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}
