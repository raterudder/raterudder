package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestLogger(ctx context.Context) (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	return log.With(ctx, logger), buf
}

func TestReportBrowser(t *testing.T) {
	t.Run("Handle CSP Violation", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {\"blockedURL\": \"https://example.com/blocked\", \"columnNumber\": 12, \"disposition\": \"enforce\", \"documentURL\": \"https://example.com/doc\", \"effectiveDirective\": \"script-src\", \"lineNumber\": 34, \"originalPolicy\": \"default-src 'self'\", \"referrer\": \"https://example.com/ref\", \"sample\": \"\", \"sourceFile\": \"https://example.com/source\", \"statusCode\": 200}, \"type\": \"csp-violation\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		assert.Contains(t, logOutput, "CSP Violation")
		assert.Contains(t, logOutput, "https://example.com/blocked")
		assert.Contains(t, logOutput, "script-src")
	})

	t.Run("Handle Intervention", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {\"id\": \"HeavyAdIntervention\", \"message\": \"Ad was heavy\", \"sourceFile\": \"https://example.com/ad.js\", \"lineNumber\": 10, \"columnNumber\": 5}, \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		assert.Contains(t, logOutput, "Browser Intervention")
		assert.Contains(t, logOutput, "HeavyAdIntervention")
		assert.Contains(t, logOutput, "Ad was heavy")
	})

	t.Run("Handle Invalid JSON", func(t *testing.T) {
		payload := "invalid json"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, buf.String(), "failed to decode browser report body")
	})

	t.Run("Handle Ignore Unknown Type", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {}, \"type\": \"unknown-report-type\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, buf.String(), "unknown browser report type")
	})

	t.Run("Handle Invalid CSP Violation Body", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": \"invalid csp body format\", \"type\": \"csp-violation\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, buf.String(), "failed to decode csp violation report body")
	})

	t.Run("Handle Invalid Intervention Body", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": \"invalid intervention body format\", \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, buf.String(), "failed to decode intervention report body")
	})

	t.Run("Handle Multiple Reports", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {\"blockedURL\": \"https://example.com/blocked\", \"columnNumber\": 12, \"disposition\": \"enforce\", \"documentURL\": \"https://example.com/doc\", \"effectiveDirective\": \"script-src\", \"lineNumber\": 34, \"originalPolicy\": \"default-src 'self'\", \"referrer\": \"https://example.com/ref\", \"sample\": \"\", \"sourceFile\": \"https://example.com/source\", \"statusCode\": 200}, \"type\": \"csp-violation\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}, {\"age\": 10, \"body\": {\"id\": \"HeavyAdIntervention\", \"message\": \"Ad was heavy\", \"sourceFile\": \"https://example.com/ad.js\", \"lineNumber\": 10, \"columnNumber\": 5}, \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		logOutput := buf.String()
		logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
		// The logger adds an extra empty string from the last newline
		if len(logLines) > 0 && logLines[len(logLines)-1] == "" {
			logLines = logLines[:len(logLines)-1]
		}
		require.Len(t, logLines, 2, "Expected exactly two log entries")

		assert.Contains(t, logLines[0], "CSP Violation")
		assert.Contains(t, logLines[0], "https://example.com/blocked")

		assert.Contains(t, logLines[1], "Browser Intervention")
		assert.Contains(t, logLines[1], "HeavyAdIntervention")
	})

	t.Run("Handle Empty Report Array", func(t *testing.T) {
		payload := "[]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		// Ensure nothing is logged when there are no reports
		assert.Empty(t, buf.String())
	})

	t.Run("Handle Mixed Batch (Valid, Invalid, Unknown)", func(t *testing.T) {
		// This payload contains an unknown type, an invalid formatted CSP report,
		// a valid intervention, and a valid CSP report. This asserts that the loop
		// doesn't return early on errors and processes the full batch.
		payload := `[
			{"age": 10, "body": {}, "type": "unknown-report-type", "url": "https://example.com/doc", "user_agent": "Mozilla/5.0"},
			{"age": 10, "body": "invalid csp body format", "type": "csp-violation", "url": "https://example.com/doc", "user_agent": "Mozilla/5.0"},
			{"age": 10, "body": {"id": "HeavyAdIntervention", "message": "Ad was heavy", "sourceFile": "https://example.com/ad.js", "lineNumber": 10, "columnNumber": 5}, "type": "intervention", "url": "https://example.com/doc", "user_agent": "Mozilla/5.0"},
			{"age": 10, "body": {"blockedURL": "https://example.com/blocked", "columnNumber": 12, "disposition": "enforce", "documentURL": "https://example.com/doc", "effectiveDirective": "script-src", "lineNumber": 34, "originalPolicy": "default-src 'self'", "referrer": "https://example.com/ref", "sample": "", "sourceFile": "https://example.com/source", "statusCode": 200}, "type": "csp-violation", "url": "https://example.com/doc", "user_agent": "Mozilla/5.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		logOutput := buf.String()
		logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
		// The logger adds an extra empty string from the last newline
		if len(logLines) > 0 && logLines[len(logLines)-1] == "" {
			logLines = logLines[:len(logLines)-1]
		}
		require.Len(t, logLines, 4, "Expected exactly four log entries (1 unknown, 1 invalid csp, 1 valid intervention, 1 valid csp)")

		// Assertions verify the handler processed everything in order
		assert.Contains(t, logLines[0], "unknown browser report type")

		assert.Contains(t, logLines[1], "failed to decode csp violation report body")

		assert.Contains(t, logLines[2], "Browser Intervention")
		assert.Contains(t, logLines[2], "HeavyAdIntervention")

		assert.Contains(t, logLines[3], "CSP Violation")
		assert.Contains(t, logLines[3], "https://example.com/blocked")
	})
}
