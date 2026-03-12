package log

import (
	"context"
	"log/slog"
	"os"
)

var (
	defaultLogLevel slog.LevelVar
	defaultLogger   = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource:   true,
		Level:       &defaultLogLevel,
		ReplaceAttr: defaultReplaceAttr,
	}))
)

func init() {
	defaultLogLevel.Set(slog.LevelInfo)
}

type contextKey int

var loggerKey contextKey = 1

// Ctx returns the logger from the context. If no logger is found, it returns the default logger.
func Ctx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return defaultLogger
}

// With returns a new context with the given logger.
func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func SetDefaultLogLevel(level slog.Level) {
	defaultLogLevel.Set(level)
}

// defaultReplaceAttr replaces attributes to match what GCP expects
// https://cloud.google.com/logging/docs/structured-logging
func defaultReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		lvl, ok := a.Value.Any().(slog.Level)
		if ok {
			a.Key = "severity"
			switch lvl {
			case slog.LevelDebug:
				a.Value = slog.StringValue("DEBUG")
			case slog.LevelInfo:
				a.Value = slog.StringValue("INFO")
			case slog.LevelWarn:
				a.Value = slog.StringValue("WARNING")
			case slog.LevelError:
				a.Value = slog.StringValue("ERROR")
			}
		}
	case slog.SourceKey:
		source, ok := a.Value.Any().(*slog.Source)
		if ok && source != nil {
			a.Key = "logging.googleapis.com/sourceLocation"
		}
	case slog.MessageKey:
		// rename to message if its already a string
		if a.Value.Kind() == slog.KindString {
			a.Key = "message"
			return a
		}
	}
	return a
}
