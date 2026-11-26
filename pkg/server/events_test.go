package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/stretchr/testify/require"
)

func captureJSONLogs(ctx context.Context) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return log.With(ctx, logger), &buf
}

func findLoggedEvent(t *testing.T, raw string, eventName string) (map[string]any, bool) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["event"] == eventName {
			return entry, true
		}
	}
	return nil, false
}
