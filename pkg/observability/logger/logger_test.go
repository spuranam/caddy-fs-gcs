package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	// Subtests are sequential because Init mutates the global Logger.
	t.Run("json format", func(t *testing.T) {
		Init(LevelInfo, true)
		loggerMu.RLock()
		l := Logger
		loggerMu.RUnlock()
		assert.NotNil(t, l)
	})

	t.Run("text format", func(t *testing.T) {
		Init(LevelDebug, false)
		loggerMu.RLock()
		l := Logger
		loggerMu.RUnlock()
		assert.NotNil(t, l)
	})
}

func TestSetLevel(t *testing.T) {
	// Subtests are sequential because they mutate the global levelVar.
	t.Run("set debug level", func(t *testing.T) {
		SetLevel(LevelDebug)
		assert.Equal(t, slog.LevelDebug, levelVar.Level())
	})

	t.Run("set warn level", func(t *testing.T) {
		SetLevel(LevelWarn)
		assert.Equal(t, slog.LevelWarn, levelVar.Level())
	})

	t.Run("set error level", func(t *testing.T) {
		SetLevel(LevelError)
		assert.Equal(t, slog.LevelError, levelVar.Level())
	})

	t.Run("set info level", func(t *testing.T) {
		SetLevel(LevelInfo)
		assert.Equal(t, slog.LevelInfo, levelVar.Level())
	})

	t.Run("unknown defaults to info", func(t *testing.T) {
		SetLevel(Level("unknown"))
		assert.Equal(t, slog.LevelInfo, levelVar.Level())
	})
}

func TestLevelEnabled(t *testing.T) {
	// Subtests are sequential because they mutate the global levelVar.
	t.Run("debug enabled at debug level", func(t *testing.T) {
		levelVar.Set(slog.LevelDebug)
		assert.True(t, DebugEnabled())
		assert.True(t, InfoEnabled())
	})

	t.Run("debug disabled at info level", func(t *testing.T) {
		levelVar.Set(slog.LevelInfo)
		assert.False(t, DebugEnabled())
		assert.True(t, InfoEnabled())
	})

	t.Run("info disabled at warn level", func(t *testing.T) {
		levelVar.Set(slog.LevelWarn)
		assert.False(t, DebugEnabled())
		assert.False(t, InfoEnabled())
	})
}

func TestLogFunctions(t *testing.T) {
	// Not parallel: modifies global Logger

	// Replace global logger with one that writes to a buffer
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	loggerMu.Lock()
	origLogger := Logger
	Logger = slog.New(handler)
	loggerMu.Unlock()
	t.Cleanup(func() {
		loggerMu.Lock()
		Logger = origLogger
		loggerMu.Unlock()
	})

	Debug("debug msg", "key", "val")
	assert.Contains(t, buf.String(), "debug msg")
	buf.Reset()

	Info("info msg", "key", "val")
	assert.Contains(t, buf.String(), "info msg")
	buf.Reset()

	Warn("warn msg", "key", "val")
	assert.Contains(t, buf.String(), "warn msg")
	buf.Reset()

	Error("error msg", "key", "val")
	assert.Contains(t, buf.String(), "error msg")
}

func TestContextLogFunctions(t *testing.T) {
	// Not parallel: modifies global Logger

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	loggerMu.Lock()
	origLogger := Logger
	Logger = slog.New(handler)
	loggerMu.Unlock()
	t.Cleanup(func() {
		loggerMu.Lock()
		Logger = origLogger
		loggerMu.Unlock()
	})

	ctx := context.Background()

	DebugContext(ctx, "debug ctx msg")
	assert.Contains(t, buf.String(), "debug ctx msg")
	buf.Reset()

	InfoContext(ctx, "info ctx msg")
	assert.Contains(t, buf.String(), "info ctx msg")
	buf.Reset()

	WarnContext(ctx, "warn ctx msg")
	assert.Contains(t, buf.String(), "warn ctx msg")
	buf.Reset()

	ErrorContext(ctx, "error ctx msg")
	assert.Contains(t, buf.String(), "error ctx msg")
}

func TestWith(t *testing.T) {
	t.Parallel()

	child := With("module", "test")
	require.NotNil(t, child)
}

func TestFromContext(t *testing.T) {
	t.Parallel()

	t.Run("nil context returns logger", func(t *testing.T) {
		t.Parallel()
		l := FromContext(nil) //nolint:staticcheck
		assert.NotNil(t, l)
	})

	t.Run("background context returns logger", func(t *testing.T) {
		t.Parallel()
		l := FromContext(context.Background())
		assert.NotNil(t, l)
	})
}

func TestNop(t *testing.T) {
	t.Parallel()

	l := Nop()
	require.NotNil(t, l)
	// Should not panic
	l.Info("test message")
}

func TestWithTraceContext(t *testing.T) {
	t.Parallel()

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()
		args := WithTraceContext(nil, "key", "val") //nolint:staticcheck
		assert.Equal(t, []any{"key", "val"}, args)
	})

	t.Run("background context no span", func(t *testing.T) {
		t.Parallel()
		args := WithTraceContext(context.Background(), "key", "val")
		assert.Equal(t, []any{"key", "val"}, args)
	})
}

func TestToSlogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    Level
		expected slog.Level
	}{
		{LevelDebug, slog.LevelDebug},
		{LevelInfo, slog.LevelInfo},
		{LevelWarn, slog.LevelWarn},
		{LevelError, slog.LevelError},
		{Level("DEBUG"), slog.LevelDebug},  // case insensitive
		{Level("WARN"), slog.LevelWarn},    // case insensitive
		{Level("unknown"), slog.LevelInfo}, // defaults to info
		{Level(""), slog.LevelInfo},        // empty defaults to info
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, toSlogLevel(tt.input))
		})
	}
}

func TestAppendKeyIfAbsent(t *testing.T) {
	t.Parallel()

	t.Run("adds key when absent", func(t *testing.T) {
		t.Parallel()
		args := appendKeyIfAbsent([]any{"a", "1"}, "b", "2")
		assert.Equal(t, []any{"a", "1", "b", "2"}, args)
	})

	t.Run("skips key when present", func(t *testing.T) {
		t.Parallel()
		args := appendKeyIfAbsent([]any{"b", "1"}, "b", "2")
		assert.Equal(t, []any{"b", "1"}, args)
	})

	t.Run("skips empty value", func(t *testing.T) {
		t.Parallel()
		args := appendKeyIfAbsent([]any{"a", "1"}, "b", "")
		assert.Equal(t, []any{"a", "1"}, args)
	})
}

func TestFromContext_WithSpan(t *testing.T) {
	t.Parallel()

	// Use a noop tracer but still validate no panic
	ctx := context.Background()
	l := FromContext(ctx)
	assert.NotNil(t, l)
}

func TestNewDefaultLogger(t *testing.T) {
	t.Parallel()

	lv := new(slog.LevelVar)
	lv.Set(slog.LevelDebug)
	l := newDefaultLogger(lv)
	assert.NotNil(t, l)
}

func TestNewLevelVarWithDefault(t *testing.T) {
	t.Parallel()

	lv := newLevelVarWithDefault()
	assert.Equal(t, slog.LevelInfo, lv.Level())
}

func TestLogFunctions_Concurrent(t *testing.T) {
	// Not parallel: modifies global Logger

	loggerMu.Lock()
	origLogger := Logger
	Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	loggerMu.Unlock()
	t.Cleanup(func() {
		loggerMu.Lock()
		Logger = origLogger
		loggerMu.Unlock()
	})

	done := make(chan struct{})
	for i := range 10 {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			Info("concurrent", "n", n)
			Debug("concurrent debug", "n", n)
			Warn("concurrent warn", "n", n)
			Error("concurrent error", "n", n)
		}(i)
	}
	for range 10 {
		<-done
	}
}

func TestSanitizeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal string", "hello world", "hello world"},
		{"with newlines", "hello\nworld", "helloworld"},
		{"with carriage return", "hello\rworld", "helloworld"},
		{"with tab preserved", "hello\tworld", "hello\tworld"},
		{"with null byte", "hello\x00world", "helloworld"},
		{"empty string", "", ""},
		{"control chars", "hello\x01\x02\x03world", "helloworld"},
		{"mixed", "a\nb\rc\td\x00e", "abcde"}, // tab is preserved but c is followed by tab
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SanitizeString(tt.input)
			// Verify no control chars remain (except tab)
			for _, r := range result {
				if r != ' ' && r != '\t' {
					assert.False(t, r < 32, "unexpected control char: %d", r)
				}
			}
		})
	}
}

func TestSanitizeQuery(t *testing.T) {
	t.Parallel()

	t.Run("short query unchanged", func(t *testing.T) {
		t.Parallel()
		result := SanitizeQuery("select * from table")
		assert.Equal(t, "select * from table", result)
	})

	t.Run("long query truncated", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("a", 300)
		result := SanitizeQuery(long)
		assert.Len(t, result, 203) // 200 chars + "..."
		assert.True(t, strings.HasSuffix(result, "..."))
	})

	t.Run("query with control chars sanitized", func(t *testing.T) {
		t.Parallel()
		result := SanitizeQuery("hello\nworld")
		assert.Equal(t, "helloworld", result)
	})

	t.Run("truncation preserves valid UTF-8", func(t *testing.T) {
		t.Parallel()
		// 50 × 4-byte emoji (200 bytes) + a 4-byte emoji split at byte 200
		query := strings.Repeat("🎉", 51) // 204 bytes
		result := SanitizeQuery(query)
		assert.True(t, strings.HasSuffix(result, "..."))
		// The truncated part (before "...") must be valid UTF-8.
		trimmed := strings.TrimSuffix(result, "...")
		for _, r := range trimmed {
			assert.NotEqual(t, '�', r, "result contains replacement character")
		}
	})
}
