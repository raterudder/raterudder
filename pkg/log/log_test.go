package log

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextLogger(t *testing.T) {
	ctx := context.Background()

	// Test Ctx without a logger in the context
	l1 := Ctx(ctx)
	require.NotNil(t, l1, "Ctx returned nil instead of default logger")
	assert.Equal(t, defaultLogger, l1, "Ctx should return defaultLogger")

	// Create a new logger to test With
	customLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	require.NotEqual(t, defaultLogger, customLogger, "Failed to create a distinct custom logger for testing")

	// Test With and Ctx with a logger in the context
	ctxWithLogger := With(ctx, customLogger)
	l2 := Ctx(ctxWithLogger)
	require.NotNil(t, l2, "Ctx returned nil, expected custom logger")
	assert.Equal(t, customLogger, l2, "Ctx should return customLogger")
}

func TestSetDefaultLogLevel(t *testing.T) {
	t.Run("updates default log level", func(t *testing.T) {
		// Store the original level to restore it after the test
		originalLevel := defaultLogLevel.Level()
		defer SetDefaultLogLevel(originalLevel)

		// Change the log level
		newLevel := slog.LevelDebug
		SetDefaultLogLevel(newLevel)

		// Verify the log level was updated correctly
		assert.Equal(t, newLevel, defaultLogLevel.Level(), "Default log level should be updated to Debug")

		// Change it to another level to be sure
		anotherLevel := slog.LevelError
		SetDefaultLogLevel(anotherLevel)
		assert.Equal(t, anotherLevel, defaultLogLevel.Level(), "Default log level should be updated to Error")
	})
}

func TestDefaultReplaceAttr(t *testing.T) {
	t.Run("ignore groups", func(t *testing.T) {
		attr := slog.String(slog.LevelKey, "INFO")
		replaced := defaultReplaceAttr([]string{"group"}, attr)
		assert.Equal(t, attr, replaced)
	})

	t.Run("level debug", func(t *testing.T) {
		attr := slog.Any(slog.LevelKey, slog.LevelDebug)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "severity", replaced.Key)
		assert.Equal(t, slog.StringValue("DEBUG"), replaced.Value)
	})

	t.Run("level info", func(t *testing.T) {
		attr := slog.Any(slog.LevelKey, slog.LevelInfo)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "severity", replaced.Key)
		assert.Equal(t, slog.StringValue("INFO"), replaced.Value)
	})

	t.Run("level warn", func(t *testing.T) {
		attr := slog.Any(slog.LevelKey, slog.LevelWarn)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "severity", replaced.Key)
		assert.Equal(t, slog.StringValue("WARNING"), replaced.Value)
	})

	t.Run("level error", func(t *testing.T) {
		attr := slog.Any(slog.LevelKey, slog.LevelError)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "severity", replaced.Key)
		assert.Equal(t, slog.StringValue("ERROR"), replaced.Value)
	})

	t.Run("level not level type", func(t *testing.T) {
		attr := slog.Any(slog.LevelKey, "NOT_A_LEVEL")
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, attr, replaced)
	})

	t.Run("source key", func(t *testing.T) {
		src := &slog.Source{
			Function: "main",
			File:     "main.go",
			Line:     1,
		}
		attr := slog.Any(slog.SourceKey, src)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "logging.googleapis.com/sourceLocation", replaced.Key)
		assert.Equal(t, slog.AnyValue(src), replaced.Value)
	})

	t.Run("source key not source type", func(t *testing.T) {
		attr := slog.Any(slog.SourceKey, "not a source")
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, attr, replaced)
	})

	t.Run("message key", func(t *testing.T) {
		attr := slog.String(slog.MessageKey, "hello world")
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, "message", replaced.Key)
		assert.Equal(t, slog.StringValue("hello world"), replaced.Value)
	})

	t.Run("message key not string", func(t *testing.T) {
		attr := slog.Any(slog.MessageKey, 123)
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, attr, replaced)
	})

	t.Run("other key", func(t *testing.T) {
		attr := slog.String("other", "value")
		replaced := defaultReplaceAttr(nil, attr)
		assert.Equal(t, attr, replaced)
	})
}
