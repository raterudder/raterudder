package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/log"
)

type CSPViolationReportBody struct {
	BlockedURL         string `json:"blockedURL"`
	ColumnNumber       int    `json:"columnNumber"`
	Disposition        string `json:"disposition"`
	DocumentURL        string `json:"documentURL"`
	EffectiveDirective string `json:"effectiveDirective"`
	LineNumber         int    `json:"lineNumber"`
	OriginalPolicy     string `json:"originalPolicy"`
	Referrer           string `json:"referrer"`
	Sample             string `json:"sample"`
	SourceFile         string `json:"sourceFile"`
	StatusCode         int    `json:"statusCode"`
}

type InterventionReportBody struct {
	ID           string `json:"id"`
	Message      string `json:"message"`
	SourceFile   string `json:"sourceFile"`
	LineNumber   int    `json:"lineNumber"`
	ColumnNumber int    `json:"columnNumber"`
}

type BrowserReport struct {
	Age       int             `json:"age"`
	Body      json.RawMessage `json:"body"`
	Type      string          `json:"type"`
	URL       string          `json:"url"`
	UserAgent string          `json:"user_agent"`
}

func (s *Server) handleReportBrowser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var reports []BrowserReport
	if err := json.NewDecoder(r.Body).Decode(&reports); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to decode browser report body", slog.Any("error", err))
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	for _, report := range reports {
		if report.Type == "csp-violation" {
			var body CSPViolationReportBody
			if err := json.Unmarshal(report.Body, &body); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to decode csp violation report body", slog.Any("error", err))
				continue
			}
			log.Ctx(ctx).ErrorContext(ctx, "CSP Violation",
				slog.String("type", report.Type),
				slog.String("url", report.URL),
				slog.String("user_agent", report.UserAgent),
				slog.String("blockedURL", body.BlockedURL),
				slog.Int("columnNumber", body.ColumnNumber),
				slog.String("disposition", body.Disposition),
				slog.String("documentURL", body.DocumentURL),
				slog.String("effectiveDirective", body.EffectiveDirective),
				slog.Int("lineNumber", body.LineNumber),
				slog.String("originalPolicy", body.OriginalPolicy),
				slog.String("referrer", body.Referrer),
				slog.String("sample", body.Sample),
				slog.String("sourceFile", body.SourceFile),
				slog.Int("statusCode", body.StatusCode),
			)
		} else if report.Type == "intervention" {
			var body InterventionReportBody
			if err := json.Unmarshal(report.Body, &body); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to decode intervention report body", slog.Any("error", err))
				continue
			}
			log.Ctx(ctx).ErrorContext(ctx, "Browser Intervention",
				slog.String("type", report.Type),
				slog.String("url", report.URL),
				slog.String("user_agent", report.UserAgent),
				slog.String("id", body.ID),
				slog.String("message", body.Message),
				slog.String("sourceFile", body.SourceFile),
				slog.Int("lineNumber", body.LineNumber),
				slog.Int("columnNumber", body.ColumnNumber),
			)
		} else {
			log.Ctx(ctx).WarnContext(ctx, "unknown browser report type",
				slog.String("type", report.Type),
				slog.String("url", report.URL),
				slog.String("user_agent", report.UserAgent),
			)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
