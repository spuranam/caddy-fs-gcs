// Package logger provides a global slog.Logger with level control and
// OpenTelemetry trace enrichment. It is intentionally slog-based (not
// zap) because it serves as a bridge layer: Caddy modules that use
// zap.Logger (via ctx.Logger()) bridge into slog through zapslog when
// calling shared components like ConfigValidator and ErrorPageHandler
// that accept *slog.Logger.
//
// Use Init() to configure the logger at startup. In Caddy modules,
// prefer ctx.Logger() (zap) directly and only pass slog.Logger to
// components that require it.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/trace"
)

var (
	loggerMu sync.RWMutex
	levelVar = newLevelVarWithDefault()
	// Logger is the global slog logger used throughout the service.
	Logger = newDefaultLogger(levelVar)
)

func newLevelVarWithDefault() *slog.LevelVar {
	v := new(slog.LevelVar)
	v.Set(slog.LevelInfo)
	return v
}

func newDefaultLogger(lv *slog.LevelVar) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     lv,
		AddSource: true,
	}))
}

// Level represents the logging level configured by the user.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Init configures the global logger with the specified level and format.
func Init(level Level, jsonFormat bool) {
	levelVar.Set(toSlogLevel(level))
	opts := &slog.HandlerOptions{Level: levelVar, AddSource: true}
	var handler slog.Handler
	if jsonFormat {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	loggerMu.Lock()
	Logger = slog.New(handler)
	loggerMu.Unlock()
}

// SetLevel updates the runtime log level.
func SetLevel(level Level) {
	levelVar.Set(toSlogLevel(level))
}

// DebugEnabled returns true if debug level logging is enabled.
func DebugEnabled() bool { return levelVar.Level() <= slog.LevelDebug }

// InfoEnabled returns true if info level logging is enabled.
func InfoEnabled() bool { return levelVar.Level() <= slog.LevelInfo }

// Debug logs a debug message.
func Debug(msg string, args ...any) { logWith(func(l *slog.Logger) { l.Debug(msg, args...) }) }

// Info logs an info message.
func Info(msg string, args ...any) { logWith(func(l *slog.Logger) { l.Info(msg, args...) }) }

// Warn logs a warning message.
func Warn(msg string, args ...any) { logWith(func(l *slog.Logger) { l.Warn(msg, args...) }) }

// Error logs an error message.
func Error(msg string, args ...any) { logWith(func(l *slog.Logger) { l.Error(msg, args...) }) }

// DebugContext logs a debug message enriched with request/trace identifiers.
func DebugContext(ctx context.Context, msg string, args ...any) {
	logWith(func(l *slog.Logger) { l.Debug(msg, WithTraceContext(ctx, args...)...) })
}

// InfoContext logs an info message enriched with request/trace identifiers.
func InfoContext(ctx context.Context, msg string, args ...any) {
	logWith(func(l *slog.Logger) { l.Info(msg, WithTraceContext(ctx, args...)...) })
}

// WarnContext logs a warning message enriched with request/trace identifiers.
func WarnContext(ctx context.Context, msg string, args ...any) {
	logWith(func(l *slog.Logger) { l.Warn(msg, WithTraceContext(ctx, args...)...) })
}

// ErrorContext logs an error message enriched with request/trace identifiers.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	logWith(func(l *slog.Logger) { l.Error(msg, WithTraceContext(ctx, args...)...) })
}

// With returns a child logger with additional attributes.
func With(args ...any) *slog.Logger {
	loggerMu.RLock()
	l := Logger
	loggerMu.RUnlock()
	return l.With(args...)
}

// FromContext returns a logger enriched with trace context if present.
func FromContext(ctx context.Context) *slog.Logger {
	loggerMu.RLock()
	l := Logger
	loggerMu.RUnlock()
	if ctx == nil {
		return l
	}
	if span := trace.SpanFromContext(ctx); span != nil {
		if sc := span.SpanContext(); sc.IsValid() {
			return l.With(
				"trace_id", sc.TraceID().String(),
				"span_id", sc.SpanID().String(),
			)
		}
	}
	return l
}

// Nop returns a no-op logger that discards all output. Useful for tests.
func Nop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// WithTraceContext appends trace_id and span_id attributes when available.
func WithTraceContext(ctx context.Context, args ...any) []any {
	if ctx == nil {
		return args
	}
	if span := trace.SpanFromContext(ctx); span != nil {
		if sc := span.SpanContext(); sc.IsValid() {
			args = appendKeyIfAbsent(args, "trace_id", sc.TraceID().String())
			args = appendKeyIfAbsent(args, "span_id", sc.SpanID().String())
		}
	}
	return args
}

func logWith(fn func(*slog.Logger)) {
	loggerMu.RLock()
	l := Logger
	loggerMu.RUnlock()
	fn(l)
}

func appendKeyIfAbsent(args []any, key, value string) []any {
	if value == "" {
		return args
	}
	for i := 0; i < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == key {
			return args
		}
	}
	return append(args, key, value)
}

func toSlogLevel(level Level) slog.Level {
	switch strings.ToLower(string(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
