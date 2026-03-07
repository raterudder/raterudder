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
	"github.com/stretchr/testify/require"
)

// stringBuffer implements io.Writer and stores written data.
type stringBuffer struct {
	b bytes.Buffer
}

func (sb *stringBuffer) Write(p []byte) (n int, err error) {
	return sb.b.Write(p)
}

func (sb *stringBuffer) String() string {
	return sb.b.String()
}

func setupTestLogger(ctx context.Context) (context.Context, *stringBuffer) {
	buf := &stringBuffer{}
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

		require.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		require.Contains(t, logOutput, "CSP Violation")
		require.Contains(t, logOutput, "https://example.com/blocked")
		require.Contains(t, logOutput, "script-src")
	})

	t.Run("Handle Intervention", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {\"id\": \"HeavyAdIntervention\", \"message\": \"Ad was heavy\", \"sourceFile\": \"https://example.com/ad.js\", \"lineNumber\": 10, \"columnNumber\": 5}, \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		require.Contains(t, logOutput, "Browser Intervention")
		require.Contains(t, logOutput, "HeavyAdIntervention")
		require.Contains(t, logOutput, "Ad was heavy")
	})

	t.Run("Handle Invalid JSON", func(t *testing.T) {
		payload := "invalid json"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, buf.String(), "failed to decode browser report body")
	})

	t.Run("Handle Ignore Unknown Type", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {}, \"type\": \"unknown-report-type\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Contains(t, buf.String(), "unknown browser report type")
	})

	t.Run("Handle Invalid CSP Violation Body", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": \"invalid csp body format\", \"type\": \"csp-violation\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Contains(t, buf.String(), "failed to decode csp violation report body")
	})

	t.Run("Handle Invalid Intervention Body", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": \"invalid intervention body format\", \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Contains(t, buf.String(), "failed to decode intervention report body")
	})

	t.Run("Handle Multiple Reports", func(t *testing.T) {
		payload := "[{\"age\": 10, \"body\": {\"blockedURL\": \"https://example.com/blocked\", \"columnNumber\": 12, \"disposition\": \"enforce\", \"documentURL\": \"https://example.com/doc\", \"effectiveDirective\": \"script-src\", \"lineNumber\": 34, \"originalPolicy\": \"default-src 'self'\", \"referrer\": \"https://example.com/ref\", \"sample\": \"\", \"sourceFile\": \"https://example.com/source\", \"statusCode\": 200}, \"type\": \"csp-violation\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}, {\"age\": 10, \"body\": {\"id\": \"HeavyAdIntervention\", \"message\": \"Ad was heavy\", \"sourceFile\": \"https://example.com/ad.js\", \"lineNumber\": 10, \"columnNumber\": 5}, \"type\": \"intervention\", \"url\": \"https://example.com/doc\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)

		logOutput := buf.String()
		logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
		// The logger adds an extra empty string from the last newline
		if len(logLines) > 0 && logLines[len(logLines)-1] == "" {
			logLines = logLines[:len(logLines)-1]
		}
		require.Len(t, logLines, 2, "Expected exactly two log entries")

		require.Contains(t, logLines[0], "CSP Violation")
		require.Contains(t, logLines[0], "https://example.com/blocked")

		require.Contains(t, logLines[1], "Browser Intervention")
		require.Contains(t, logLines[1], "HeavyAdIntervention")
	})

	t.Run("Handle Empty Report Array", func(t *testing.T) {
		payload := "[]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)

		// Ensure nothing is logged when there are no reports
		require.Empty(t, buf.String())
	})
}
