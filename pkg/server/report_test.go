package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportBrowser(t *testing.T) {
	t.Run("Handle CSP Violation", func(t *testing.T) {
		payload := `[{
			"age": 10,
			"body": {
				"blockedURL": "https://example.com/blocked",
				"columnNumber": 12,
				"disposition": "enforce",
				"documentURL": "https://example.com/doc",
				"effectiveDirective": "script-src",
				"lineNumber": 34,
				"originalPolicy": "default-src 'self'",
				"referrer": "https://example.com/ref",
				"sample": "",
				"sourceFile": "https://example.com/source",
				"statusCode": 200
			},
			"type": "csp-violation",
			"url": "https://example.com/doc",
			"user_agent": "Mozilla/5.0"
		}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		// Assert successful processing of the report (returns 204 No Content)
		require.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Handle Intervention", func(t *testing.T) {
		payload := `[{
			"age": 10,
			"body": {
				"id": "HeavyAdIntervention",
				"message": "Ad was heavy",
				"sourceFile": "https://example.com/ad.js",
				"lineNumber": 10,
				"columnNumber": 5
			},
			"type": "intervention",
			"url": "https://example.com/doc",
			"user_agent": "Mozilla/5.0"
		}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		// Assert successful processing of the report (returns 204 No Content)
		require.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Handle Invalid JSON", func(t *testing.T) {
		payload := `invalid json`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		// Assert that bad request is returned on invalid JSON
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Handle Ignore Unknown Type", func(t *testing.T) {
		payload := `[{
			"age": 10,
			"body": {},
			"type": "unknown-report-type",
			"url": "https://example.com/doc",
			"user_agent": "Mozilla/5.0"
		}]`

		req := httptest.NewRequest(http.MethodPost, "/api/report/browser", bytes.NewBufferString(payload))
		w := httptest.NewRecorder()

		srv := &Server{}
		srv.handleReportBrowser(w, req)

		// Assert successful processing of the report (returns 204 No Content)
		require.Equal(t, http.StatusNoContent, w.Code)
	})
}
