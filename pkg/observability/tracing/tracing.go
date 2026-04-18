package tracing

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	pkg "github.com/spuranam/caddy-fs-gcs/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	// tracer is the global OpenTelemetry tracer, protected by initMu.
	// Use GetTracer() for safe read access.
	tracer trace.Tracer

	tp           *sdktrace.TracerProvider
	shutdownFunc func(context.Context) error
	initialized  bool
	initMu       sync.Mutex
)

// GetTracer returns the global OpenTelemetry tracer. Safe for concurrent use.
func GetTracer() trace.Tracer {
	initMu.Lock()
	t := tracer
	initMu.Unlock()
	return t
}

// Init initializes OpenTelemetry tracing. Safe to call multiple times.
func Init(ctx context.Context, exporter, serviceEnv, otlpEndpoint, otlpProtocol string, sampleRate float64) error {
	initMu.Lock()
	defer initMu.Unlock()

	if initialized {
		return nil
	}

	resAttrs := []attribute.KeyValue{
		semconv.ServiceName(pkg.ServiceName),
		semconv.ServiceVersion(pkg.Version),
	}
	if pkg.Commit != "unknown" {
		resAttrs = append(resAttrs, attribute.String("vcs.revision", pkg.Commit))
	}
	if serviceEnv != "" {
		resAttrs = append(resAttrs, attribute.String("deployment.environment.name", serviceEnv))
	}
	if instanceID := serviceInstanceID(); instanceID != "" {
		resAttrs = append(resAttrs, semconv.ServiceInstanceID(instanceID))
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(resAttrs...),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	var exp sdktrace.SpanExporter
	switch exporter {
	case "otlp":
		exp, err = createOTLPExporter(ctx, otlpEndpoint, otlpProtocol)
		if err != nil {
			return fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
	case "stdout":
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return fmt.Errorf("failed to create stdout exporter: %w", err)
		}
	case "none", "":
		tracer = otel.Tracer(pkg.ServiceName)
		initialized = true
		return nil
	default:
		return fmt.Errorf("unknown exporter type: %s (supported: otlp, stdout, none)", exporter)
	}

	var sampler sdktrace.Sampler
	switch {
	case sampleRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
	case sampleRate <= 0.0:
		sampler = sdktrace.NeverSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(sampleRate)
	}

	tp = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = otel.Tracer(pkg.ServiceName)
	shutdownFunc = tp.Shutdown
	initialized = true
	return nil
}

// Shutdown gracefully shuts down the tracer.
func Shutdown(ctx context.Context) error {
	initMu.Lock()
	defer initMu.Unlock()

	if !initialized || shutdownFunc == nil {
		return nil
	}

	err := shutdownFunc(ctx)
	initialized = false
	shutdownFunc = nil
	tp = nil
	// Use a no-op tracer instead of nil to prevent panics if callers
	// access the tracer variable after Shutdown.
	tracer = otel.Tracer(pkg.ServiceName)
	return err
}

// StartSpan starts a new span with the given name and attributes.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(pkg.ServiceName)
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// AddSpanAttributes adds attributes to the current span.
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// RecordSpanError records an error on the current span.
func RecordSpanError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err, trace.WithAttributes(attrs...))
		span.SetStatus(codes.Error, err.Error())
	}
}

func createOTLPExporter(ctx context.Context, endpoint, protocol string) (sdktrace.SpanExporter, error) {
	if endpoint == "" {
		return nil, errors.New("OTLP endpoint is required")
	}

	otlpHost, insecure := parseOTLPEndpoint(endpoint)

	var client otlptrace.Client
	switch protocol {
	case "grpc":
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(otlpHost)}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		client = otlptracegrpc.NewClient(opts...)
	case "http":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(otlpHost)}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		client = otlptracehttp.NewClient(opts...)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol: %s (supported: grpc, http)", protocol)
	}

	return otlptrace.New(ctx, client)
}

func parseOTLPEndpoint(endpoint string) (hostPort string, insecure bool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint, true
	}
	insecure = u.Scheme == "http"
	return u.Host, insecure
}

func serviceInstanceID() string {
	if pod := os.Getenv("POD_NAME"); pod != "" {
		return pod
	}
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return ""
}
