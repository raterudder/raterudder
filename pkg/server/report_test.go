package server

import (
	"bytes"
	"context"
	"encoding/json"
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
		assert.Contains(t, logOutput, `"level":"WARN"`)
		assert.NotContains(t, logOutput, `"level":"ERROR"`)
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
		assert.Contains(t, logOutput, `"level":"WARN"`)
		assert.NotContains(t, logOutput, `"level":"ERROR"`)
		assert.Contains(t, logOutput, "Browser Intervention")
		assert.Contains(t, logOutput, "HeavyAdIntervention")
		assert.Contains(t, logOutput, "Ad was heavy")
	})

	t.Run("Handle Notification Error", func(t *testing.T) {
		payload := "[{\"age\": 5, \"body\": {\"message\": \"Failed to load notification icon\", \"name\": \"TypeError\", \"phase\": \"primary_show_notification\", \"title\": \"RateRudder Alert\", \"tag\": \"price-spike\", \"stack\": \"TypeError: Failed to load icon...\", \"data\": {\"url\": \"/dashboard\"}}, \"type\": \"x-notification-error\", \"url\": \"https://example.com/sw.js\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		assert.Contains(t, logOutput, `"level":"ERROR"`)
		assert.Contains(t, logOutput, "Service Worker Notification Error")
		assert.Contains(t, logOutput, "Failed to load notification icon")
		assert.Contains(t, logOutput, "primary_show_notification")
		assert.Contains(t, logOutput, "RateRudder Alert")
		assert.Contains(t, logOutput, "price-spike")
		assert.Contains(t, logOutput, "TypeError")
	})

	t.Run("Handle Invalid Notification Error Body", func(t *testing.T) {
		payload := "[{\"age\": 5, \"body\": \"invalid body string\", \"type\": \"x-notification-error\", \"url\": \"https://example.com/sw.js\", \"user_agent\": \"Mozilla/5.0\"}]"

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, buf.String(), "failed to decode notification error report body")
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

	t.Run("Handle Too Many Reports", func(t *testing.T) {
		// Generate an array of 15 reports
		var reports []map[string]any
		for i := 0; i < 15; i++ {
			reports = append(reports, map[string]any{
				"type": "unknown-report-type",
				"url":  "https://example.com/doc",
			})
		}
		payload, err := json.Marshal(reports)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBuffer(payload))
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "too many browser reports in single request")

		logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
		// The logger adds an extra empty string from the last newline
		if len(logLines) > 0 && logLines[len(logLines)-1] == "" {
			logLines = logLines[:len(logLines)-1]
		}
		// 1 log for the warning + 10 logs for the reports
		require.Len(t, logLines, 11)
	})

	t.Run("Handle Notification Error Relative URL And Host Port", func(t *testing.T) {
		payload := `[{"age": 0, "body": {"message": "Registration showNotification rejected", "name": "AbortError", "phase": "fallback_show_notification"}, "type": "x-notification-error", "url": "/sw.js", "user_agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com:8443"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		assert.Contains(t, logOutput, `"level":"ERROR"`)
		assert.Contains(t, logOutput, "Service Worker Notification Error")
		assert.Contains(t, logOutput, "Registration showNotification rejected")
	})

	t.Run("Handle First-Party CSP Violation With Host Port", func(t *testing.T) {
		payload := `[{"age": 2, "body": {"blockedURL": "https://unapproved-cdn.example.org/app.js", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": "https://raterudder.com/assets/index.js", "statusCode": 200}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com:443"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logOutput := buf.String()
		assert.Contains(t, logOutput, `"level":"WARN"`)
		assert.NotContains(t, logOutput, `"level":"ERROR"`)
		assert.Contains(t, logOutput, "CSP Violation")
		assert.Contains(t, logOutput, "https://unapproved-cdn.example.org/app.js")
	})

	t.Run("Ignore HeadlessChrome In Report UserAgent", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "https://raterudder.com/blocked", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "connect-src"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/131.0.0.0 Safari/537.36"},
			{"age": 1, "body": {"id": "HeavyAdIntervention", "message": "Intervention"}, "type": "intervention", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0 HeadlessChrome/131.0.0.0"},
			{"age": 1, "body": {"message": "Notification failed"}, "type": "x-notification-error", "url": "/sw.js", "user_agent": "Mozilla/5.0 HeadlessChrome/131.0.0.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, buf.String())
	})

	t.Run("Ignore Automated Request UserAgent Header", func(t *testing.T) {
		payload := `[{"age": 1, "body": {"blockedURL": "https://raterudder.com/blocked", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": ""}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/131.0.0.0 Safari/537.36")
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, buf.String())
	})

	t.Run("Ignore Other Automated UserAgents", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "https://raterudder.com/blocked", "documentURL": "https://raterudder.com/", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://raterudder.com/", "user_agent": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
			{"age": 1, "body": {"blockedURL": "https://raterudder.com/blocked", "documentURL": "https://raterudder.com/", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://raterudder.com/", "user_agent": "Mozilla/5.0 Playwright/1.48"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, buf.String())
	})

	t.Run("Ignore Cross-Origin URLs And DocumentURLs", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "https://other.example.com/script.js", "documentURL": "https://other.example.com/page", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://other.example.com/page", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "https://other.example.com/script.js", "documentURL": "https://translate.googleusercontent.com/translate_c", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "inline", "documentURL": "about:blank", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"id": "Intervention", "message": "Cross-origin intervention"}, "type": "intervention", "url": "https://other.example.com/page", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"message": "Spoofed notification error"}, "type": "x-notification-error", "url": "https://other.example.com/sw.js", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"message": "Protocol-relative URL"}, "type": "x-notification-error", "url": "//other.example.com/sw.js", "user_agent": "Mozilla/5.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, buf.String())
	})

	t.Run("Ignore Browser Extension Sources And BlockedURLs", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "wasm-eval", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": "chrome-extension"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "inline", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "style-src", "sourceFile": "chrome-extension"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "eval", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": "chrome-extension://nkbihfbeogaeaoehlefnkodbefgpgknn/inpage.js"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "chrome-extension", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": ""}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "moz-extension://1234-5678/inject.js", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": ""}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "inline", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "script-src", "sourceFile": "safari-web-extension://ABCD/content.js"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"id": "ExtensionIntervention", "message": "Extension intervention", "sourceFile": "chrome-extension"}, "type": "intervention", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, buf.String())
	})

	t.Run("Ignore Third-Party Media-Src Blocks But Log First-Party Media-Src", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "https://ssl.gstatic.com/dictionary/static/sounds/beep.mp3", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "media-src", "sourceFile": ""}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "https", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "media-src", "sourceFile": ""}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "https://raterudder.com/audio/alert.mp3", "documentURL": "https://raterudder.com/dashboard", "effectiveDirective": "media-src", "sourceFile": "https://raterudder.com/assets/index.js"}, "type": "csp-violation", "url": "https://raterudder.com/dashboard", "user_agent": "Mozilla/5.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		req.Host = "raterudder.com"
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logLines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if assert.Len(t, logLines, 1) {
			assert.Contains(t, logLines[0], `"level":"WARN"`)
			assert.Contains(t, logLines[0], "CSP Violation")
			assert.Contains(t, logLines[0], "https://raterudder.com/audio/alert.mp3")
		}
	})

	t.Run("Handle Edge Cases Blob Worker IPv6 Host And Empty URLs", func(t *testing.T) {
		payload := `[
			{"age": 1, "body": {"blockedURL": "", "documentURL": "", "effectiveDirective": "media-src"}, "type": "csp-violation", "url": "", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"message": "Empty URL notification"}, "type": "x-notification-error", "url": "", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "https://evil.example.org/x.js", "documentURL": "http://[::1]:8080/dashboard", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "blob:http://[::1]:8080/worker-id", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "data:audio/wav;base64,AAAA", "documentURL": "http://[::1]:8080/dashboard", "effectiveDirective": "media-src"}, "type": "csp-violation", "url": "", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "blob", "documentURL": "http://[::1]:8080/dashboard", "effectiveDirective": "media-src"}, "type": "csp-violation", "url": "http://[::1]:8080/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "", "documentURL": "http://[::1]:8080/dashboard", "effectiveDirective": "media-src"}, "type": "csp-violation", "url": "http://[::1]:8080/dashboard", "user_agent": "Mozilla/5.0"},
			{"age": 1, "body": {"blockedURL": "inline", "documentURL": "://bad-url", "effectiveDirective": "script-src"}, "type": "csp-violation", "url": "http://[::1]:8080/dashboard", "user_agent": "Mozilla/5.0"}
		]`

		req := httptest.NewRequest(http.MethodPost, "http://[::1]:8080/api/report/browser", bytes.NewBufferString(payload))
		req.Host = ""
		ctx, buf := setupTestLogger(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		logLines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		require.Len(t, logLines, 4)
		for _, line := range logLines {
			assert.Contains(t, line, `"level":"WARN"`)
		}
	})
}
