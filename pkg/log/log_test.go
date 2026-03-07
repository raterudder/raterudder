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
