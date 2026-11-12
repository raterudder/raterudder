package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

var (
	errNoESSConfigured   = errors.New("no ESS configured")
	errNoStatusAvailable = errors.New("no status available")
)

// AgentSummaryStatus represents the current site status in the agent summary response.
type AgentSummaryStatus struct {
	Timestamp           time.Time          `json:"timestamp"`
	BatterySOC          float64            `json:"batterySOC"`
	BatteryCapacityKWH  float64            `json:"batteryCapacityKWH"`
	BatteryKW           float64            `json:"batteryKW"`
	SolarKW             float64            `json:"solarKW"`
	HomeKW              float64            `json:"homeKW"`
	GridKW              float64            `json:"gridKW"`
	BatteryMode         types.BatteryMode  `json:"batteryMode"`
	BatteryModeLabel    string             `json:"batteryModeLabel"`
	SolarMode           types.SolarMode    `json:"solarMode"`
	SolarModeLabel      string             `json:"solarModeLabel"`
	Reason              types.ActionReason `json:"reason,omitempty"`
	Description         string             `json:"description,omitempty"`
	ReserveSOC          float64            `json:"reserveSOC"`
	Paused              bool               `json:"paused,omitempty"`
	GridUnavailable     bool               `json:"gridUnavailable,omitempty"`
	EmergencyMode       bool               `json:"emergencyMode,omitempty"`
	VPPActive           bool               `json:"vppActive,omitempty"`
	ImportDollarsPerKWH float64            `json:"importDollarsPerKWH"`
	ExportDollarsPerKWH float64            `json:"exportDollarsPerKWH"`
}

// AgentSolarForecast represents today's and tomorrow's solar generation totals in kWh.
type AgentSolarForecast struct {
	TodayActualSolarKWH            float64 `json:"todayActualSolarKWH"`
	TodayRemainingForecastSolarKWH float64 `json:"todayRemainingForecastSolarKWH"`
	TodayTotalEstimatedSolarKWH    float64 `json:"todayTotalEstimatedSolarKWH"`
	TomorrowForecastSolarKWH       float64 `json:"tomorrowForecastSolarKWH"`
}

// agentPlanPeriod represents a single scheduled period in the next 24 hours.
type agentPlanPeriod struct {
	TSStart             time.Time          `json:"tsStart"`
	TSEnd               time.Time          `json:"tsEnd"`
	BatteryMode         types.BatteryMode  `json:"batteryMode"`
	BatteryModeLabel    string             `json:"batteryModeLabel"`
	SolarMode           types.SolarMode    `json:"solarMode"`
	SolarModeLabel      string             `json:"solarModeLabel"`
	Reason              types.ActionReason `json:"reason,omitempty"`
	StartSOC            float64            `json:"startSOC"`
	EndSOC              float64            `json:"endSOC"`
	ReserveSOC          float64            `json:"reserveSOC,omitempty"`
	SolarKWH            float64            `json:"solarKWH"`
	HomeLoadKWH         float64            `json:"homeLoadKWH"`
	GridImportKWH       float64            `json:"gridImportKWH,omitempty"`
	GridExportKWH       float64            `json:"gridExportKWH,omitempty"`
	ImportDollarsPerKWH float64            `json:"importDollarsPerKWH"`
	ExportDollarsPerKWH float64            `json:"exportDollarsPerKWH"`
}

// AgentSummaryRes represents the complete response for GET /api/agent/summary and MCP get_energy_summary.
type AgentSummaryRes struct {
	SiteTime      time.Time          `json:"siteTime"`
	TimeZone      string             `json:"timeZone"`
	UpdatedAt     time.Time          `json:"updatedAt"`
	Status        AgentSummaryStatus `json:"status"`
	SolarForecast AgentSolarForecast `json:"solarForecast"`
	Plan          []agentPlanPeriod  `json:"plan"`
}

func batteryModeLabel(mode types.BatteryMode) string {
	switch mode {
	case types.BatteryModeChargeAny:
		return "Charge from Grid/Solar"
	case types.BatteryModeStandby:
		return "Standby"
	case types.BatteryModeLoad:
		return "Power Home"
	case types.BatteryModeExport:
		return "Export to Grid"
	case types.BatteryModeNoChange:
		return "No Change"
	default:
		return fmt.Sprintf("Mode(%d)", mode)
	}
}

func solarModeLabel(mode types.SolarMode) string {
	switch mode {
	case types.SolarModeAny:
		return "Power Home & Export Surplus"
	case types.SolarModeExport:
		return "Export to Grid"
	case types.SolarModeNoExport:
		return "Curtailed (No Grid Export)"
	case types.SolarModeNoChange:
		return "No Change"
	default:
		return fmt.Sprintf("Mode(%d)", mode)
	}
}

func roundSummaryFloat(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}

func (s *Server) buildAgentSummary(ctx context.Context, siteID string) (AgentSummaryRes, error) {
	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings for agent summary", slog.Any("error", err))
		return AgentSummaryRes{}, fmt.Errorf("failed to get settings: %w", err)
	}
	if settings.ESS == "" {
		return AgentSummaryRes{}, errNoESSConfigured
	}

	latestAction, err := s.storage.GetLatestAction(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get latest action for agent summary", slog.Any("error", err))
		return AgentSummaryRes{}, fmt.Errorf("failed to get latest action: %w", err)
	}
	if latestAction == nil {
		return AgentSummaryRes{}, errNoStatusAvailable
	}

	var loc *time.Location
	if latestAction.SystemStatus.TimeLocation != "" {
		if l, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
			loc = l
		}
	}
	if loc == nil && settings.Location != nil && settings.Location.TimeZone != "" {
		if l, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			loc = l
		}
	}
	if loc == nil && !latestAction.Timestamp.IsZero() && latestAction.Timestamp.Location() != nil {
		loc = latestAction.Timestamp.Location()
	}
	if loc == nil {
		loc = time.UTC
	}

	latestAction.Timestamp = latestAction.Timestamp.In(loc)
	latestAction.SystemTimestamp = latestAction.SystemTimestamp.In(loc)
	latestAction.SystemStatus.Timestamp = latestAction.SystemStatus.Timestamp.In(loc)

	now := s.now().In(loc)
	todayStart := truncateDay(now)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	tomorrowEnd := todayStart.AddDate(0, 0, 2)

	var todayActualSolarKWH float64
	var latestActualEnd time.Time
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, todayStart, now)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get energy history for agent summary", slog.Any("error", err))
	} else {
		for _, day := range energyHistory {
			for _, h := range day.Hourly {
				hStart := h.TSHourStart.In(loc)
				if !hStart.Before(todayStart) && hStart.Before(tomorrowStart) {
					if h.SolarKWH > 0 {
						todayActualSolarKWH += h.SolarKWH
					}
					hEnd := hStart.Add(time.Hour)
					if hEnd.After(latestActualEnd) {
						latestActualEnd = hEnd
					}
				}
			}
		}
	}

	var todayRemainingForecastSolarKWH float64
	var tomorrowForecastSolarKWH float64
	var importDollarsPerKWH float64
	var exportDollarsPerKWH float64
	var priceSetFromPlan bool

	planPeriods := make([]agentPlanPeriod, 0)
	if latestAction.Plan != nil && len(latestAction.Plan.Periods) > 0 {
		periods := latestAction.Plan.Periods

		for _, p := range periods {
			pStart := p.TSStart.In(loc)
			pEnd := p.TSEnd.In(loc)

			if !priceSetFromPlan && !now.Before(pStart) && now.Before(pEnd) {
				importDollarsPerKWH = p.ImportDollars
				exportDollarsPerKWH = p.ExportDollars
				priceSetFromPlan = true
			}

			// Remaining today solar forecast
			if pEnd.After(now) && pStart.Before(tomorrowStart) {
				if pStart.Before(now) {
					// Energy history is synced in whole-hour buckets (typically up to the start of the
					// current hour, e.g. 14:00), while the first plan period starts at the last poll time
					// (e.g. 14:20-15:00). Only prorate the in-progress plan period if recorded actuals
					// (latestActualEnd) overlap past pStart so we don't double-count solar already in
					// todayActualSolarKWH or drop the unrecorded gap between the hour start and now.
					if !latestActualEnd.IsZero() && latestActualEnd.After(pStart) && p.DurationHours > 0 {
						remStart := now
						if latestActualEnd.After(remStart) {
							remStart = latestActualEnd
						}
						if pEnd.After(remStart) {
							frac := pEnd.Sub(remStart).Hours() / p.DurationHours
							todayRemainingForecastSolarKWH += p.SolarKWH * math.Min(1, math.Max(0, frac))
						}
					} else {
						todayRemainingForecastSolarKWH += p.SolarKWH
					}
				} else {
					todayRemainingForecastSolarKWH += p.SolarKWH
				}
			}

			// Tomorrow solar forecast
			if !pStart.Before(tomorrowStart) && pStart.Before(tomorrowEnd) {
				tomorrowForecastSolarKWH += p.SolarKWH
			}
		}

		// fallback to the first period
		if !priceSetFromPlan {
			importDollarsPerKWH = periods[0].ImportDollars
			exportDollarsPerKWH = periods[0].ExportDollars
			priceSetFromPlan = true
		}

		windowStart := now
		if periods[len(periods)-1].TSEnd.Before(now) && !latestAction.Timestamp.IsZero() {
			windowStart = latestAction.Timestamp
		}
		windowEnd := windowStart.Add(24 * time.Hour)

		for _, p := range periods {
			pStart := p.TSStart.In(loc)
			pEnd := p.TSEnd.In(loc)
			if pEnd.After(windowStart) && pStart.Before(windowEnd) {
				planPeriods = append(planPeriods, agentPlanPeriod{
					TSStart:             pStart,
					TSEnd:               pEnd,
					BatteryMode:         p.BatteryMode,
					BatteryModeLabel:    batteryModeLabel(p.BatteryMode),
					SolarMode:           p.SolarMode,
					SolarModeLabel:      solarModeLabel(p.SolarMode),
					Reason:              p.Reason,
					StartSOC:            roundSummaryFloat(p.StartSOC, 1),
					EndSOC:              roundSummaryFloat(p.EndSOC, 1),
					ReserveSOC:          roundSummaryFloat(p.ReserveSOC, 1),
					SolarKWH:            roundSummaryFloat(p.SolarKWH, 3),
					HomeLoadKWH:         roundSummaryFloat(p.LoadKWH, 3),
					GridImportKWH:       roundSummaryFloat(p.GridImportKWH, 3),
					GridExportKWH:       roundSummaryFloat(p.GridExportKWH, 3),
					ImportDollarsPerKWH: roundSummaryFloat(p.ImportDollars, 4),
					ExportDollarsPerKWH: roundSummaryFloat(p.ExportDollars, 4),
				})
			}
		}
	}

	// otherwise get it from the action itself
	if !priceSetFromPlan && latestAction.CurrentPrice != nil {
		importDollarsPerKWH = latestAction.CurrentPrice.ImportRateDollars()
		exportDollarsPerKWH = latestAction.CurrentPrice.ExportRateDollars(settings.Settings, nil)
	}

	todayActualRounded := roundSummaryFloat(todayActualSolarKWH, 2)
	todayRemainingRounded := roundSummaryFloat(todayRemainingForecastSolarKWH, 2)
	todayTotalRounded := roundSummaryFloat(todayActualSolarKWH+todayRemainingForecastSolarKWH, 2)
	tomorrowForecastRounded := roundSummaryFloat(tomorrowForecastSolarKWH, 2)

	statusTS := latestAction.SystemStatus.Timestamp
	if statusTS.IsZero() {
		statusTS = latestAction.Timestamp
	}

	return AgentSummaryRes{
		SiteTime:  now,
		TimeZone:  loc.String(),
		UpdatedAt: latestAction.Timestamp,
		Status: AgentSummaryStatus{
			Timestamp:           statusTS,
			BatterySOC:          roundSummaryFloat(latestAction.SystemStatus.BatterySOC, 1),
			BatteryCapacityKWH:  roundSummaryFloat(latestAction.SystemStatus.BatteryCapacityKWH, 2),
			BatteryKW:           roundSummaryFloat(latestAction.SystemStatus.BatteryKW, 2),
			SolarKW:             roundSummaryFloat(latestAction.SystemStatus.SolarKW, 2),
			HomeKW:              roundSummaryFloat(latestAction.SystemStatus.HomeKW, 2),
			GridKW:              roundSummaryFloat(latestAction.SystemStatus.GridKW, 2),
			BatteryMode:         latestAction.BatteryMode,
			BatteryModeLabel:    batteryModeLabel(latestAction.BatteryMode),
			SolarMode:           latestAction.SolarMode,
			SolarModeLabel:      solarModeLabel(latestAction.SolarMode),
			Reason:              latestAction.Reason,
			Description:         latestAction.Description,
			ReserveSOC:          roundSummaryFloat(latestAction.ReserveSOC, 1),
			Paused:              latestAction.Paused || settings.Pause,
			GridUnavailable:     latestAction.SystemStatus.GridUnavailable,
			EmergencyMode:       latestAction.SystemStatus.EmergencyMode,
			VPPActive:           latestAction.SystemStatus.VPPActive,
			ImportDollarsPerKWH: roundSummaryFloat(importDollarsPerKWH, 4),
			ExportDollarsPerKWH: roundSummaryFloat(exportDollarsPerKWH, 4),
		},
		SolarForecast: AgentSolarForecast{
			TodayActualSolarKWH:            todayActualRounded,
			TodayRemainingForecastSolarKWH: todayRemainingRounded,
			TodayTotalEstimatedSolarKWH:    todayTotalRounded,
			TomorrowForecastSolarKWH:       tomorrowForecastRounded,
		},
		Plan: planPeriods,
	}, nil
}

func formatAgentSummaryText(summary AgentSummaryRes) string {
	var b strings.Builder
	b.WriteString("# RateRudder Energy Summary\n\n")
	fmt.Fprintf(&b, "- **Site Time:** %s (%s)\n", summary.SiteTime.Format("2006-01-02 15:04 MST"), summary.TimeZone)
	fmt.Fprintf(&b, "- **Last Updated:** %s\n\n", summary.UpdatedAt.Format("2006-01-02 15:04 MST"))

	st := summary.Status
	b.WriteString("## Current Status\n\n")
	fmt.Fprintf(&b, "- **Battery SOC:** %.1f%% (Capacity: %.1f kWh, Reserve: %.1f%%)\n", st.BatterySOC, st.BatteryCapacityKWH, st.ReserveSOC)
	fmt.Fprintf(&b, "- **Power Flows:** Battery %.2f kW, Solar %.2f kW, Home Load %.2f kW, Grid %.2f kW\n", st.BatteryKW, st.SolarKW, st.HomeKW, st.GridKW)
	fmt.Fprintf(&b, "- **Battery Mode:** %s\n", st.BatteryModeLabel)
	fmt.Fprintf(&b, "- **Solar Mode:** %s\n", st.SolarModeLabel)
	if st.Description != "" {
		fmt.Fprintf(&b, "- **Decision:** %s\n", st.Description)
	}
	fmt.Fprintf(&b, "- **Current Prices:** Import $%.4f/kWh, Export $%.4f/kWh\n", st.ImportDollarsPerKWH, st.ExportDollarsPerKWH)
	if st.Paused {
		b.WriteString("- **Automation Status:** Paused\n")
	}
	if st.GridUnavailable {
		b.WriteString("- **Grid Status:** Grid Unavailable (Outage)\n")
	}
	if st.EmergencyMode {
		b.WriteString("- **Emergency Mode:** Active\n")
	}
	if st.VPPActive {
		b.WriteString("- **VPP Event:** Active\n")
	}

	sf := summary.SolarForecast
	b.WriteString("\n## Solar Forecast\n\n")
	fmt.Fprintf(&b, "- **Today Actual So Far:** %.2f kWh\n", sf.TodayActualSolarKWH)
	fmt.Fprintf(&b, "- **Today Remaining Forecast:** %.2f kWh\n", sf.TodayRemainingForecastSolarKWH)
	fmt.Fprintf(&b, "- **Today Total Estimated:** %.2f kWh\n", sf.TodayTotalEstimatedSolarKWH)
	fmt.Fprintf(&b, "- **Tomorrow Forecast:** %.2f kWh\n", sf.TomorrowForecastSolarKWH)

	b.WriteString("\n## Next 24-Hour Plan\n\n")
	if len(summary.Plan) == 0 {
		b.WriteString("No upcoming schedule periods available.\n")
	} else {
		b.WriteString("| Time | Battery SOC | Solar (kWh) | Home Load (kWh) | Import ($/kWh) | Export ($/kWh) | Battery Mode |\n")
		b.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")
		for _, p := range summary.Plan {
			fmt.Fprintf(
				&b,
				"| %s - %s | %.1f%% -> %.1f%% | %.2f | %.2f | $%.4f | $%.4f | %s |\n",
				p.TSStart.Format("2006-01-02 15:04"),
				p.TSEnd.Format("15:04"),
				p.StartSOC,
				p.EndSOC,
				p.SolarKWH,
				p.HomeLoadKWH,
				p.ImportDollarsPerKWH,
				p.ExportDollarsPerKWH,
				p.BatteryModeLabel,
			)
		}
	}

	return b.String()
}

func formatAgentSummaryHTML(summary AgentSummaryRes) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"robots\" content=\"noindex, nofollow\">\n<title>RateRudder Energy Summary</title>\n</head>\n<body>\n<main>\n")
	b.WriteString("<h1>RateRudder Energy Summary</h1>\n<ul>\n")
	fmt.Fprintf(&b, "<li><strong>Site Time:</strong> %s (%s)</li>\n", html.EscapeString(summary.SiteTime.Format("2006-01-02 15:04 MST")), html.EscapeString(summary.TimeZone))
	fmt.Fprintf(&b, "<li><strong>Last Updated:</strong> %s</li>\n", html.EscapeString(summary.UpdatedAt.Format("2006-01-02 15:04 MST")))
	b.WriteString("</ul>\n")

	st := summary.Status
	b.WriteString("<h2>Current Status</h2>\n<ul>\n")
	fmt.Fprintf(&b, "<li><strong>Battery SOC:</strong> %.1f%% (Capacity: %.1f kWh, Reserve: %.1f%%)</li>\n", st.BatterySOC, st.BatteryCapacityKWH, st.ReserveSOC)
	fmt.Fprintf(&b, "<li><strong>Power Flows:</strong> Battery %.2f kW, Solar %.2f kW, Home Load %.2f kW, Grid %.2f kW</li>\n", st.BatteryKW, st.SolarKW, st.HomeKW, st.GridKW)
	fmt.Fprintf(&b, "<li><strong>Battery Mode:</strong> %s</li>\n", html.EscapeString(st.BatteryModeLabel))
	fmt.Fprintf(&b, "<li><strong>Solar Mode:</strong> %s</li>\n", html.EscapeString(st.SolarModeLabel))
	if st.Description != "" {
		fmt.Fprintf(&b, "<li><strong>Decision:</strong> %s</li>\n", html.EscapeString(st.Description))
	}
	fmt.Fprintf(&b, "<li><strong>Current Prices:</strong> Import $%.4f/kWh, Export $%.4f/kWh</li>\n", st.ImportDollarsPerKWH, st.ExportDollarsPerKWH)
	if st.Paused {
		b.WriteString("<li><strong>Automation Status:</strong> Paused</li>\n")
	}
	if st.GridUnavailable {
		b.WriteString("<li><strong>Grid Status:</strong> Grid Unavailable (Outage)</li>\n")
	}
	if st.EmergencyMode {
		b.WriteString("<li><strong>Emergency Mode:</strong> Active</li>\n")
	}
	if st.VPPActive {
		b.WriteString("<li><strong>VPP Event:</strong> Active</li>\n")
	}
	b.WriteString("</ul>\n")

	sf := summary.SolarForecast
	b.WriteString("<h2>Solar Forecast</h2>\n<ul>\n")
	fmt.Fprintf(&b, "<li><strong>Today Actual So Far:</strong> %.2f kWh</li>\n", sf.TodayActualSolarKWH)
	fmt.Fprintf(&b, "<li><strong>Today Remaining Forecast:</strong> %.2f kWh</li>\n", sf.TodayRemainingForecastSolarKWH)
	fmt.Fprintf(&b, "<li><strong>Today Total Estimated:</strong> %.2f kWh</li>\n", sf.TodayTotalEstimatedSolarKWH)
	fmt.Fprintf(&b, "<li><strong>Tomorrow Forecast:</strong> %.2f kWh</li>\n", sf.TomorrowForecastSolarKWH)
	b.WriteString("</ul>\n")

	b.WriteString("<h2>Next 24-Hour Plan</h2>\n")
	if len(summary.Plan) == 0 {
		b.WriteString("<p>No upcoming schedule periods available.</p>\n")
	} else {
		b.WriteString("<table>\n<thead>\n<tr><th>Time</th><th>Battery SOC</th><th>Solar (kWh)</th><th>Home Load (kWh)</th><th>Import ($/kWh)</th><th>Export ($/kWh)</th><th>Battery Mode</th></tr>\n</thead>\n<tbody>\n")
		for _, p := range summary.Plan {
			fmt.Fprintf(
				&b,
				"<tr><td>%s - %s</td><td>%.1f%% -&gt; %.1f%%</td><td>%.2f</td><td>%.2f</td><td>$%.4f</td><td>$%.4f</td><td>%s</td></tr>\n",
				html.EscapeString(p.TSStart.Format("2006-01-02 15:04")),
				html.EscapeString(p.TSEnd.Format("15:04")),
				p.StartSOC,
				p.EndSOC,
				p.SolarKWH,
				p.HomeLoadKWH,
				p.ImportDollarsPerKWH,
				p.ExportDollarsPerKWH,
				html.EscapeString(p.BatteryModeLabel),
			)
		}
		b.WriteString("</tbody>\n</table>\n")
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

func resolveAgentSummaryFormat(r *http.Request) (string, error) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "json", "text", "html":
		return format, nil
	case "", "auto":
		accept := strings.ToLower(r.Header.Get("Accept"))
		bestFormat := "html"
		bestIdx := -1
		for _, candidate := range []struct {
			mime   string
			format string
		}{
			{"text/html", "html"},
			{"application/json", "json"},
			{"text/plain", "text"},
			{"text/markdown", "text"},
		} {
			if idx := strings.Index(accept, candidate.mime); idx >= 0 && (bestIdx == -1 || idx < bestIdx) {
				bestIdx = idx
				bestFormat = candidate.format
			}
		}
		// Default to HTML when format is omitted or auto and no specific type matches so
		// web-fetch agents (including bots that send Accept: */* or no Accept header) receive parseable HTML.
		return bestFormat, nil
	default:
		return "", errors.New("invalid format: must be json, text, html, or auto")
	}
}

func (s *Server) handleAgentSummary(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	format, err := resolveAgentSummaryFormat(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	siteID := s.getSiteID(r)

	summary, err := s.buildAgentSummary(ctx, siteID)
	if err != nil {
		if errors.Is(err, errNoESSConfigured) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, errNoStatusAvailable) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONError(w, "failed to build energy summary", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Add("Vary", "Accept, Authorization, Cookie")
	switch format {
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(formatAgentSummaryHTML(summary))); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(formatAgentSummaryText(summary))); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(summary); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
