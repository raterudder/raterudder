package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/errgroup"
)

const (
	maxMCPBodyBytes       = 1 << 20
	mcpCacheTTLMs         = 3600000
	mcpDefaultRecentDays  = 7
	mcpServerInstructions = "RateRudder MCP server provides read-only solar, battery, electricity rate, forecast, savings, and decision history tools for a single home energy site (polled every 20 minutes). Use 'get_energy_summary' for the latest 20-minute telemetry snapshot, unsigned power flows, solar forecasts, and next 24h optimization plan. Use 'get_recent_savings' for 1-to-30-day energy totals and financial savings (passive solar+battery vs. RateRudder's incremental automation gain). Use 'get_daily_history' for hour-by-hour metered telemetry, electricity rates, and merged battery control decision windows on a specific date."
)

// mcpSupportedProtocolVersions lists the primary 2026-07-28 protocol plus the stateless
// 2025-11-25 backwards-compatibility shim.
var mcpSupportedProtocolVersions = []string{"2026-07-28", "2025-11-25"}

type mcpEmptyArgs struct{}

type mcpRecentSavingsArgs struct {
	Days *int `json:"days,omitempty" jsonschema:"Number of local calendar days to include through today (1 for today only, 7 for last 7 days, up to 30; default 7)."`
}

type mcpDailyHistoryArgs struct {
	Date string `json:"date,omitempty" jsonschema:"Local calendar date at the site in YYYY-MM-DD format (optional; defaults to today in the site's timezone)."`
}

// MCPDailySavings represents a single local calendar day inside MCPRecentSavingsRes.
type MCPDailySavings struct {
	Date                          string  `json:"date"`
	HomeUsedKWH                   float64 `json:"homeUsedKWH"`
	SolarGeneratedKWH             float64 `json:"solarGeneratedKWH"`
	GridImportedKWH               float64 `json:"gridImportedKWH"`
	GridExportedKWH               float64 `json:"gridExportedKWH"`
	BatteryDischargedKWH          float64 `json:"batteryDischargedKWH"`
	NetGridCostDollars            float64 `json:"netGridCostDollars"`
	TotalSavingsDollars           float64 `json:"totalSavingsDollars"`
	SolarAndBatterySavingsDollars float64 `json:"solarAndBatterySavingsDollars"`
	RateRudderSavingsDollars      float64 `json:"rateRudderSavingsDollars"`
	Estimated                     bool    `json:"estimated,omitempty"`
}

// MCPRecentSavingsRes holds the structured results of the get_recent_savings MCP tool.
type MCPRecentSavingsRes struct {
	Days                          int               `json:"days"`
	PeriodStart                   time.Time         `json:"periodStart"`
	PeriodEnd                     time.Time         `json:"periodEnd"`
	AsOf                          time.Time         `json:"asOf"`
	TimeZone                      string            `json:"timeZone"`
	HomeUsedKWH                   float64           `json:"homeUsedKWH"`
	SolarGeneratedKWH             float64           `json:"solarGeneratedKWH"`
	GridImportedKWH               float64           `json:"gridImportedKWH"`
	GridExportedKWH               float64           `json:"gridExportedKWH"`
	BatteryDischargedKWH          float64           `json:"batteryDischargedKWH"`
	TotalSavingsDollars           float64           `json:"totalSavingsDollars"`
	SolarAndBatterySavingsDollars float64           `json:"solarAndBatterySavingsDollars"`
	RateRudderSavingsDollars      float64           `json:"rateRudderSavingsDollars"`
	SolarSavingsDollars           float64           `json:"solarSavingsDollars"`
	BatterySavingsDollars         float64           `json:"batterySavingsDollars"`
	GridImportCostDollars         float64           `json:"gridImportCostDollars"`
	GridExportCreditDollars       float64           `json:"gridExportCreditDollars"`
	NetGridCostDollars            float64           `json:"netGridCostDollars"`
	GridOnlyBaselineCostDollars   float64           `json:"gridOnlyBaselineCostDollars"`
	StoredEnergyValueDollars      float64           `json:"storedEnergyValueDollars"`
	Estimated                     bool              `json:"estimated"`
	Daily                         []MCPDailySavings `json:"daily,omitempty"`
}

// MCPHourlyHistory represents one hour of actual energy flows, SOC bounds, and electricity rates.
type MCPHourlyHistory struct {
	TSHourStart          time.Time `json:"tsHourStart"`
	SolarKWH             float64   `json:"solarKWH"`
	HomeUsedKWH          float64   `json:"homeUsedKWH"`
	BatteryChargedKWH    float64   `json:"batteryChargedKWH"`
	BatteryDischargedKWH float64   `json:"batteryDischargedKWH"`
	GridImportedKWH      float64   `json:"gridImportedKWH"`
	GridExportedKWH      float64   `json:"gridExportedKWH"`
	MinBatterySOC        float64   `json:"minBatterySOC"`
	MaxBatterySOC        float64   `json:"maxBatterySOC"`
	ImportDollarsPerKWH  *float64  `json:"importDollarsPerKWH,omitempty"`
	ExportDollarsPerKWH  *float64  `json:"exportDollarsPerKWH,omitempty"`
	HadGridUnavailable   bool      `json:"hadGridUnavailable,omitempty"`
	HadEmergencyMode     bool      `json:"hadEmergencyMode,omitempty"`
	HadVPPActive         bool      `json:"hadVppActive,omitempty"`
	HadSolarNoExport     bool      `json:"hadSolarNoExport,omitempty"`
	AlarmsCount          int       `json:"alarmsCount,omitempty"`
}

// MCPDecisionSegment represents a contiguous time span where RateRudder maintained the same control state.
type MCPDecisionSegment struct {
	TSStart          time.Time          `json:"tsStart"`
	TSEnd            time.Time          `json:"tsEnd"`
	PollCount        int                `json:"pollCount"`
	BatteryMode      types.BatteryMode  `json:"batteryMode"`
	BatteryModeLabel string             `json:"batteryModeLabel"`
	SolarMode        types.SolarMode    `json:"solarMode"`
	SolarModeLabel   string             `json:"solarModeLabel"`
	Reason           types.ActionReason `json:"reason,omitempty"`
	Description      string             `json:"description,omitempty"`
	StartBatterySOC  float64            `json:"startBatterySOC"`
	EndBatterySOC    float64            `json:"endBatterySOC"`
	ReserveSOC       float64            `json:"reserveSOC,omitempty"`
	Paused           bool               `json:"paused,omitempty"`
	GridUnavailable  bool               `json:"gridUnavailable,omitempty"`
	EmergencyMode    bool               `json:"emergencyMode,omitempty"`
	VPPActive        bool               `json:"vppActive,omitempty"`
}

// MCPDailyHistoryRes holds the structured results of the get_daily_history MCP tool.
type MCPDailyHistoryRes struct {
	Date                      string               `json:"date"`
	TimeZone                  string               `json:"timeZone"`
	TotalSolarKWH             float64              `json:"totalSolarKWH"`
	TotalHomeUsedKWH          float64              `json:"totalHomeUsedKWH"`
	TotalBatteryChargedKWH    float64              `json:"totalBatteryChargedKWH"`
	TotalBatteryDischargedKWH float64              `json:"totalBatteryDischargedKWH"`
	TotalGridImportedKWH      float64              `json:"totalGridImportedKWH"`
	TotalGridExportedKWH      float64              `json:"totalGridExportedKWH"`
	HadGridUnavailable        bool                 `json:"hadGridUnavailable,omitempty"`
	HadEmergencyMode          bool                 `json:"hadEmergencyMode,omitempty"`
	HadVPPActive              bool                 `json:"hadVppActive,omitempty"`
	AlarmNames                []string             `json:"alarmNames,omitempty"`
	Hours                     []MCPHourlyHistory   `json:"hours"`
	Decisions                 []MCPDecisionSegment `json:"decisions"`
}

const maxMCPErrorLogBytes = 512

// mcpResponseWriter forces Cache-Control: no-store on MCP responses (overriding the SDK's
// "no-cache, no-transform" since tool results contain authenticated per-site data) and captures
// the HTTP status code and a bounded body prefix so handleMCP can warning-log SDK errors.
type mcpResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
	snippet     []byte
	overflowed  bool
}

func (w *mcpResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.statusCode = statusCode
		w.wroteHeader = true
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *mcpResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if rem := maxMCPErrorLogBytes - len(w.snippet); rem > 0 {
		if len(b) <= rem {
			w.snippet = append(w.snippet, b...)
		} else {
			w.snippet = append(w.snippet, b[:rem]...)
			w.overflowed = true
		}
	} else if len(b) > 0 {
		w.overflowed = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *mcpResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func mcpSiteID(ctx context.Context) string {
	siteID, _ := ctx.Value(siteIDContextKey).(string)
	return siteID
}

// handleMCP implements the Remote MCP Streamable HTTP endpoint (primary spec version 2026-07-28,
// with a stateless 2025-11-25 backwards-compatibility shim) by delegating to the go-sdk
// stateless JSON streamable HTTP handler.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if len(s.appsEncryptionKey) != 32 {
		log.Ctx(ctx).WarnContext(ctx, "mcp endpoint rejected: apps encryption key not configured")
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	siteID := mcpSiteID(ctx)
	if siteID == "" || siteID == SiteIDAll {
		log.Ctx(ctx).WarnContext(ctx, "mcp endpoint rejected: valid siteID required", slog.String("siteID", siteID))
		writeJSONError(w, "valid siteID required", http.StatusBadRequest)
		return
	}

	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	w.Header().Set("Referrer-Policy", "no-referrer")

	rw := &mcpResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	s.getMCPHTTPHandler().ServeHTTP(rw, r)

	if rw.statusCode >= http.StatusBadRequest {
		log.Ctx(ctx).WarnContext(ctx, "mcp request failed",
			slog.String("siteID", siteID),
			slog.Int("statusCode", rw.statusCode),
			slog.String("httpMethod", r.Method),
			slog.String("contentType", truncateLogString(r.Header.Get("Content-Type"), 128)),
			slog.String("accept", truncateLogString(r.Header.Get("Accept"), 128)),
			slog.String("mcpProtocolVersion", truncateLogString(r.Header.Get("MCP-Protocol-Version"), 64)),
			slog.String("mcpMethod", truncateLogString(r.Header.Get("Mcp-Method"), 64)),
			slog.String("mcpName", truncateLogString(r.Header.Get("Mcp-Name"), 64)),
			slog.String("response", strings.TrimSpace(string(rw.snippet))),
		)
	} else if rw.statusCode == http.StatusOK && !rw.overflowed && len(rw.snippet) > 0 {
		var rpcResp struct {
			Error *jsonrpc.Error `json:"error"`
		}
		if json.Unmarshal(rw.snippet, &rpcResp) == nil && rpcResp.Error != nil {
			log.Ctx(ctx).WarnContext(ctx, "mcp jsonrpc error",
				slog.String("siteID", siteID),
				slog.Int64("errorCode", rpcResp.Error.Code),
				slog.String("errorMessage", truncateLogString(rpcResp.Error.Message, 256)),
				slog.String("mcpProtocolVersion", truncateLogString(r.Header.Get("MCP-Protocol-Version"), 64)),
				slog.String("mcpMethod", truncateLogString(r.Header.Get("Mcp-Method"), 64)),
			)
		}
	}
}

// getMCPHTTPHandler builds the SDK server and stateless streamable HTTP handler once so tool
// registration and JSON schema reflection don't happen per request.
func (s *Server) getMCPHTTPHandler() http.Handler {
	s.mcpHandlerOnce.Do(func() {
		srv := s.newMCPServer()
		s.mcpHandler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
			Stateless:                    true,
			JSONResponse:                 true,
			DisableLocalhostProtection:   true,
			MaxRequestBodyBytes:          maxMCPBodyBytes,
			PropagateRequestCancellation: true,
		})
	})
	return s.mcpHandler
}

func (s *Server) newMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:       "raterudder",
		Title:      "RateRudder",
		Version:    common.Version(),
		WebsiteURL: "https://raterudder.com",
	}, &mcp.ServerOptions{
		Instructions:              mcpServerInstructions,
		Capabilities:              &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		SupportedProtocolVersions: mcpSupportedProtocolVersions,
		SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
			// server/discover and tools/list are the only cacheable results and are static per release.
			c.TTLMs = mcpCacheTTLMs
			c.CacheScope = "public"
		},
	})

	notDestructive, closedWorld := false, false
	readOnlyTool := func(name, title, description string, inputSchema any) *mcp.Tool {
		return &mcp.Tool{
			Name:        name,
			Title:       title,
			Description: description,
			InputSchema: inputSchema,
			Annotations: &mcp.ToolAnnotations{
				Title:           title,
				ReadOnlyHint:    true,
				DestructiveHint: &notDestructive,
				IdempotentHint:  true,
				OpenWorldHint:   &closedWorld,
			},
		}
	}

	mcp.AddTool(srv, readOnlyTool(
		"get_energy_summary",
		"Get Energy Summary",
		"Get the latest site snapshot (updated every 20 minutes at updatedAt): batterySOC % (usable energy sits above the reserveSOC % backup floor of batteryCapacityKWH), unsigned power flows in kW (batteryChargingKW, batteryDischargingKW, solarKW, homeKW, gridImportKW, gridExportKW), current battery/solar modes and reason, solar generation forecasts (today actual, remaining, estimated total, tomorrow), and the next 24-hour optimization plan. Real-time importDollarsPerKWH and exportDollarsPerKWH can be negative, which may trigger grid charging or solar curtailment (solarModeLabel: \"Curtailed (No Grid Export)\").",
		nil,
	), func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmptyArgs) (*mcp.CallToolResult, AgentSummaryRes, error) {
		res, err := s.callMCPEnergySummary(ctx, mcpSiteID(ctx))
		return nil, res, err
	})

	savingsSchema, err := jsonschema.For[mcpRecentSavingsArgs](nil)
	if err != nil {
		panic(fmt.Errorf("get_recent_savings input schema: %w", err))
	}
	minDays, maxDays := 1.0, 30.0
	savingsSchema.Properties["days"].Minimum = &minDays
	savingsSchema.Properties["days"].Maximum = &maxDays
	savingsSchema.Properties["days"].Default = json.RawMessage(fmt.Sprint(mcpDefaultRecentDays))
	mcp.AddTool(srv, readOnlyTool(
		"get_recent_savings",
		"Get Recent Savings",
		"Get energy totals and financial savings over the last N local calendar days (1 to 30, default 7), with a daily breakdown when days > 1 on plan-mode sites. Avoid double-counting savings: totalSavingsDollars (vs. gridOnlyBaselineCostDollars without solar/battery) equals passive solarAndBatterySavingsDollars plus incremental rateRudderSavingsDollars; solarSavingsDollars and batterySavingsDollars are an alternative component split. netGridCostDollars = gridImportCostDollars - gridExportCreditDollars (negative means a net bill credit); storedEnergyValueDollars tracks net change in stored battery value; estimated is true when final utility settlement rates are still pending.",
		savingsSchema,
	), func(ctx context.Context, _ *mcp.CallToolRequest, args mcpRecentSavingsArgs) (*mcp.CallToolResult, MCPRecentSavingsRes, error) {
		days := mcpDefaultRecentDays
		if args.Days != nil {
			days = *args.Days
		}
		res, err := s.callMCPRecentSavings(ctx, mcpSiteID(ctx), days)
		return nil, res, err
	})

	mcp.AddTool(srv, readOnlyTool(
		"get_daily_history",
		"Get Daily History",
		"Get hour-by-hour metered energy telemetry (solarKWH, homeUsedKWH, batteryChargedKWH, batteryDischargedKWH, gridImportedKWH, gridExportedKWH, min/max battery SOC, and time-weighted importDollarsPerKWH / exportDollarsPerKWH when available) plus decisions for a single local date (YYYY-MM-DD, defaults to today). Consecutive 20-minute control loop polls with the same mode and reason are merged into contiguous decision windows (pollCount, tsStart to tsEnd, startBatterySOC to endBatterySOC).",
		nil,
	), func(ctx context.Context, _ *mcp.CallToolRequest, args mcpDailyHistoryArgs) (*mcp.CallToolResult, MCPDailyHistoryRes, error) {
		res, err := s.callMCPDailyHistory(ctx, mcpSiteID(ctx), strings.TrimSpace(args.Date))
		return nil, res, err
	})

	return srv
}

func (s *Server) resolveSiteLocation(ctx context.Context, siteID string, settings settingsWithVersion) *time.Location {
	if settings.Location != nil && settings.Location.TimeZone != "" {
		if l, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			return l
		}
	}
	if latestAction, err := s.storage.GetLatestAction(ctx, siteID); err == nil && latestAction != nil {
		if latestAction.SystemStatus.TimeLocation != "" {
			if l, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
				return l
			}
		}
		if !latestAction.Timestamp.IsZero() && latestAction.Timestamp.Location() != nil && latestAction.Timestamp.Location() != time.UTC {
			return latestAction.Timestamp.Location()
		}
	}
	return time.UTC
}

// callMCPEnergySummary returns domain failures as plain errors so the SDK reports them as
// isError tool results.
func (s *Server) callMCPEnergySummary(ctx context.Context, siteID string) (AgentSummaryRes, error) {
	summary, err := s.buildAgentSummary(ctx, siteID)
	if err != nil {
		errMsg := "failed to build energy summary"
		if errors.Is(err, errNoESSConfigured) {
			errMsg = "no ESS configured for this site"
		} else if errors.Is(err, errNoStatusAvailable) {
			errMsg = "no status available yet for this site"
		}
		log.Ctx(ctx).WarnContext(ctx, "mcp get_energy_summary execution error", slog.String("siteID", siteID), slog.Any("error", err))
		return AgentSummaryRes{}, errors.New(errMsg)
	}
	return summary, nil
}

func (s *Server) callMCPRecentSavings(ctx context.Context, siteID string, days int) (MCPRecentSavingsRes, error) {
	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "mcp get_recent_savings failed to get settings", slog.String("siteID", siteID), slog.Any("error", err))
		return MCPRecentSavingsRes{}, errors.New("failed to load site settings")
	}

	loc := s.resolveSiteLocation(ctx, siteID, settings)
	now := s.now().In(loc)
	todayStart := truncateDay(now)
	end := todayStart.AddDate(0, 0, 1)
	start := todayStart.AddDate(0, 0, -(days - 1))

	var stats types.SavingsStats
	var dailyBreakdown []MCPDailySavings
	onlyPlanMode := ffOnlyPlanMode.Enabled(ctx)
	if onlyPlanMode {
		periods := make([]savingsPeriod, 0, 1+days)
		periods = append(periods, savingsPeriod{Start: start, End: end})
		if days > 1 {
			for cur := start; cur.Before(end); {
				next := cur.AddDate(0, 0, 1)
				if !next.After(cur) {
					next = cur.Add(24 * time.Hour)
				}
				periods = append(periods, savingsPeriod{Start: cur, End: next})
				cur = next
			}
		}
		periodStats, _, err := s.getSiteSavingsNewWithSettings(ctx, siteID, settings, end, end, periods)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "mcp get_recent_savings getSiteSavingsNew failed", slog.String("siteID", siteID), slog.Any("error", err))
			return MCPRecentSavingsRes{}, errors.New("failed to calculate recent savings")
		}
		if len(periodStats) > 0 {
			stats = periodStats[0]
		}
		if len(periodStats) > 1 {
			dailyBreakdown = make([]MCPDailySavings, 0, len(periodStats)-1)
			for _, ds := range periodStats[1:] {
				dailyBreakdown = append(dailyBreakdown, MCPDailySavings{
					Date:                          ds.Timestamp.In(loc).Format("2006-01-02"),
					HomeUsedKWH:                   roundSummaryFloat(ds.HomeUsed, 2),
					SolarGeneratedKWH:             roundSummaryFloat(ds.SolarGenerated, 2),
					GridImportedKWH:               roundSummaryFloat(ds.GridImported, 2),
					GridExportedKWH:               roundSummaryFloat(ds.GridExported, 2),
					BatteryDischargedKWH:          roundSummaryFloat(ds.BatteryUsed, 2),
					NetGridCostDollars:            roundSummaryFloat(ds.Cost-ds.Credit, 2),
					TotalSavingsDollars:           roundSummaryFloat(ds.TotalSavingsDollars, 2),
					SolarAndBatterySavingsDollars: roundSummaryFloat(ds.SolarAndBatterySavingsDollars, 2),
					RateRudderSavingsDollars:      roundSummaryFloat(ds.RateRudderSavingsDollars, 2),
					Estimated:                     ds.Estimated,
				})
			}
		}
	} else {
		stats, _, err = s.getSiteSavings(ctx, siteID, start, end)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "mcp get_recent_savings getSiteSavings failed", slog.String("siteID", siteID), slog.Any("error", err))
			return MCPRecentSavingsRes{}, errors.New("failed to calculate recent savings")
		}
		stats.BatterySavings = stats.AvoidedCost - stats.ChargingCost
	}

	totalSavingsDollars := stats.TotalSavingsDollars
	if !onlyPlanMode && totalSavingsDollars == 0 {
		totalSavingsDollars = stats.SolarSavings + stats.BatterySavings + stats.Credit
	}

	return MCPRecentSavingsRes{
		Days:                          days,
		PeriodStart:                   start,
		PeriodEnd:                     end,
		AsOf:                          now,
		TimeZone:                      loc.String(),
		HomeUsedKWH:                   roundSummaryFloat(stats.HomeUsed, 2),
		SolarGeneratedKWH:             roundSummaryFloat(stats.SolarGenerated, 2),
		GridImportedKWH:               roundSummaryFloat(stats.GridImported, 2),
		GridExportedKWH:               roundSummaryFloat(stats.GridExported, 2),
		BatteryDischargedKWH:          roundSummaryFloat(stats.BatteryUsed, 2),
		TotalSavingsDollars:           roundSummaryFloat(totalSavingsDollars, 2),
		SolarAndBatterySavingsDollars: roundSummaryFloat(stats.SolarAndBatterySavingsDollars, 2),
		RateRudderSavingsDollars:      roundSummaryFloat(stats.RateRudderSavingsDollars, 2),
		SolarSavingsDollars:           roundSummaryFloat(stats.SolarSavings, 2),
		BatterySavingsDollars:         roundSummaryFloat(stats.BatterySavings, 2),
		GridImportCostDollars:         roundSummaryFloat(stats.Cost, 2),
		GridExportCreditDollars:       roundSummaryFloat(stats.Credit, 2),
		NetGridCostDollars:            roundSummaryFloat(stats.Cost-stats.Credit, 2),
		GridOnlyBaselineCostDollars:   roundSummaryFloat(stats.GridOnlyCostDollars, 2),
		StoredEnergyValueDollars:      roundSummaryFloat(stats.StoredEnergyValueDollars, 2),
		Estimated:                     stats.Estimated,
		Daily:                         dailyBreakdown,
	}, nil
}

// callMCPDailyHistory returns a JSON-RPC -32602 Invalid params error for malformed or future
// dates and plain errors (isError tool results) for storage failures.
func (s *Server) callMCPDailyHistory(ctx context.Context, siteID string, dateStr string) (MCPDailyHistoryRes, error) {
	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "mcp get_daily_history failed to get settings", slog.String("siteID", siteID), slog.Any("error", err))
		return MCPDailyHistoryRes{}, errors.New("failed to load site settings")
	}

	loc := s.resolveSiteLocation(ctx, siteID, settings)
	nowLocal := s.now().In(loc)
	todayStart := truncateDay(nowLocal)

	dayStart := todayStart
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			return MCPDailyHistoryRes{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "invalid date format, expected YYYY-MM-DD"}
		}
		if parsed.After(todayStart) {
			return MCPDailyHistoryRes{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "date cannot be in the future; use get_energy_summary for future forecasts"}
		}
		dayStart = parsed
	} else {
		dateStr = dayStart.Format("2006-01-02")
	}
	dayEnd := dayStart.AddDate(0, 0, 1)

	var (
		energyDaily []types.DailyEnergyStats
		prices      []types.Price
		actions     []types.Action
	)

	priceLookbackStart := dayStart.Add(-24 * time.Hour)
	priceLookaheadEnd := dayEnd.Add(24 * time.Hour)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		energyDaily, err = s.getCombinedEnergyHistory(gctx, siteID, dayStart, dayEnd)
		return err
	})
	g.Go(func() error {
		var err error
		prices, err = s.storage.GetPriceHistory(gctx, siteID, priceLookbackStart, priceLookaheadEnd)
		return err
	})
	g.Go(func() error {
		var err error
		actions, err = s.storage.GetActionHistory(gctx, siteID, dayStart, dayEnd)
		return err
	})

	if err := g.Wait(); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "mcp get_daily_history failed to fetch history", slog.String("siteID", siteID), slog.String("date", dateStr), slog.Any("error", err))
		return MCPDailyHistoryRes{}, errors.New("failed to fetch daily history")
	}

	if settings.UtilityRateOptions.IsFlatNetMetering() && priceLookaheadEnd.After(s.now()) && s.utilities != nil {
		needFuture := true
		for i := range prices {
			if prices[i].Contains(priceLookaheadEnd.Add(-time.Hour)) {
				needFuture = false
				break
			}
		}
		if needFuture {
			if u, err := s.utilities.Site(ctx, siteID, settings.Settings); err == nil {
				if futurePrices, err := u.GetFuturePrices(ctx); err == nil {
					prices = append(prices, futurePrices...)
				}
			}
		}
	}

	var rawHours []types.EnergyStats
	for _, day := range energyDaily {
		for _, h := range day.Hourly {
			hLocal := h.TSHourStart.In(loc)
			if !hLocal.Before(dayStart) && hLocal.Before(dayEnd) {
				h.TSHourStart = hLocal
				rawHours = append(rawHours, h)
			}
		}
	}
	slices.SortFunc(rawHours, func(a, b types.EnergyStats) int {
		return a.TSHourStart.Compare(b.TSHourStart)
	})

	sort.Slice(prices, func(i, j int) bool {
		return prices[i].TSStart.Before(prices[j].TSStart)
	})

	var (
		totalSolarKWH             float64
		totalHomeUsedKWH          float64
		totalBatteryChargedKWH    float64
		totalBatteryDischargedKWH float64
		totalGridImportedKWH      float64
		totalGridExportedKWH      float64
		hadGridUnavailable        bool
		hadEmergencyMode          bool
		hadVPPActive              bool
	)
	alarmSet := make(map[string]struct{})
	var alarmNames []string

	hours := make([]MCPHourlyHistory, 0, len(rawHours))
	for _, h := range rawHours {
		totalSolarKWH += h.SolarKWH
		totalHomeUsedKWH += h.HomeKWH
		totalBatteryChargedKWH += h.BatteryChargedKWH
		totalBatteryDischargedKWH += h.BatteryUsedKWH
		totalGridImportedKWH += h.GridImportKWH
		totalGridExportedKWH += h.GridExportKWH
		if h.HadGridUnavailable {
			hadGridUnavailable = true
		}
		if h.HadEmergencyMode {
			hadEmergencyMode = true
		}
		if h.HadVPPActive {
			hadVPPActive = true
		}
		for _, al := range h.Alarms {
			label := strings.TrimSpace(al.Name)
			if label == "" {
				label = strings.TrimSpace(al.Code)
			}
			if label != "" {
				if _, exists := alarmSet[label]; !exists {
					alarmSet[label] = struct{}{}
					alarmNames = append(alarmNames, label)
				}
			}
		}

		// Compute time-weighted average import/export price across sub-hourly intervals within [hStart, hStart+1h).
		hEnd := h.TSHourStart.Add(time.Hour)
		var forwardPrices []types.Price
		if settings.UtilityRateOptions.IsFlatNetMetering() {
			fwdEnd := h.TSHourStart.Add(24 * time.Hour)
			for _, fp := range prices {
				if fp.TSEnd.After(h.TSHourStart) && fp.TSStart.Before(fwdEnd) {
					forwardPrices = append(forwardPrices, fp)
				}
			}
		}
		var weightedImp, weightedExp, totalWeight float64
		for _, p := range prices {
			ovStart := p.TSStart
			if h.TSHourStart.After(ovStart) {
				ovStart = h.TSHourStart
			}
			ovEnd := p.TSEnd
			if hEnd.Before(ovEnd) {
				ovEnd = hEnd
			}
			if ovEnd.After(ovStart) {
				wHours := ovEnd.Sub(ovStart).Hours()
				weightedImp += p.ImportRateDollars() * wHours
				weightedExp += p.ExportRateDollars(settings.Settings, forwardPrices) * wHours
				totalWeight += wHours
			}
		}

		hrEntry := MCPHourlyHistory{
			TSHourStart:          h.TSHourStart,
			SolarKWH:             roundSummaryFloat(h.SolarKWH, 2),
			HomeUsedKWH:          roundSummaryFloat(h.HomeKWH, 2),
			BatteryChargedKWH:    roundSummaryFloat(h.BatteryChargedKWH, 2),
			BatteryDischargedKWH: roundSummaryFloat(h.BatteryUsedKWH, 2),
			GridImportedKWH:      roundSummaryFloat(h.GridImportKWH, 2),
			GridExportedKWH:      roundSummaryFloat(h.GridExportKWH, 2),
			MinBatterySOC:        roundSummaryFloat(h.MinBatterySOC, 1),
			MaxBatterySOC:        roundSummaryFloat(h.MaxBatterySOC, 1),
			HadGridUnavailable:   h.HadGridUnavailable,
			HadEmergencyMode:     h.HadEmergencyMode,
			HadVPPActive:         h.HadVPPActive,
			HadSolarNoExport:     h.HadSolarNoExport,
			AlarmsCount:          len(h.Alarms),
		}
		if totalWeight > 0 {
			imp := roundSummaryFloat(weightedImp/totalWeight, 4)
			exp := roundSummaryFloat(weightedExp/totalWeight, 4)
			hrEntry.ImportDollarsPerKWH = &imp
			hrEntry.ExportDollarsPerKWH = &exp
		}
		hours = append(hours, hrEntry)
	}

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Timestamp.Before(actions[j].Timestamp)
	})

	const maxDecisionPollGap = 30 * time.Minute
	decisions := make([]MCPDecisionSegment, 0)
	var prevLastPollTS time.Time
	for _, a := range actions {
		tsLocal := a.Timestamp.In(loc)
		if tsLocal.Before(dayStart) || !tsLocal.Before(dayEnd) {
			continue
		}
		if a.SystemStatus.GridUnavailable {
			hadGridUnavailable = true
		}
		if a.SystemStatus.EmergencyMode {
			hadEmergencyMode = true
		}
		if a.SystemStatus.VPPActive {
			hadVPPActive = true
		}
		for _, al := range a.SystemStatus.Alarms {
			label := strings.TrimSpace(al.Name)
			if label == "" {
				label = strings.TrimSpace(al.Code)
			}
			if label != "" {
				if _, exists := alarmSet[label]; !exists {
					alarmSet[label] = struct{}{}
					alarmNames = append(alarmNames, label)
				}
			}
		}

		soc := roundSummaryFloat(a.SystemStatus.BatterySOC, 1)
		resSOC := roundSummaryFloat(resolveActionReserveSOC(a, settings.MinBatterySOC), 1)
		if len(decisions) > 0 {
			prev := &decisions[len(decisions)-1]
			gap := tsLocal.Sub(prevLastPollTS)
			if gap <= maxDecisionPollGap &&
				prev.BatteryMode == a.BatteryMode &&
				prev.SolarMode == a.SolarMode &&
				prev.Reason == a.Reason &&
				prev.ReserveSOC == resSOC &&
				prev.Paused == a.Paused &&
				prev.GridUnavailable == a.SystemStatus.GridUnavailable &&
				prev.EmergencyMode == a.SystemStatus.EmergencyMode &&
				prev.VPPActive == a.SystemStatus.VPPActive {
				prev.TSEnd = tsLocal.Add(20 * time.Minute)
				if prev.TSEnd.After(dayEnd) {
					prev.TSEnd = dayEnd
				}
				prev.EndBatterySOC = soc
				prev.PollCount++
				if prev.Description == "" && a.Description != "" {
					prev.Description = a.Description
				}
				prevLastPollTS = tsLocal
				continue
			}
			// Snap previous segment's end time to the exact timestamp of this state transition
			// only when polls were contiguous (do not bridge multi-hour telemetry/polling gaps).
			if tsLocal.After(prev.TSStart) && gap <= maxDecisionPollGap {
				prev.TSEnd = tsLocal
			}
		}

		segEnd := tsLocal.Add(20 * time.Minute)
		if segEnd.After(dayEnd) {
			segEnd = dayEnd
		}
		decisions = append(decisions, MCPDecisionSegment{
			TSStart:          tsLocal,
			TSEnd:            segEnd,
			PollCount:        1,
			BatteryMode:      a.BatteryMode,
			BatteryModeLabel: batteryModeLabel(a.BatteryMode),
			SolarMode:        a.SolarMode,
			SolarModeLabel:   solarModeLabel(a.SolarMode),
			Reason:           a.Reason,
			Description:      a.Description,
			StartBatterySOC:  soc,
			EndBatterySOC:    soc,
			ReserveSOC:       resSOC,
			Paused:           a.Paused,
			GridUnavailable:  a.SystemStatus.GridUnavailable,
			EmergencyMode:    a.SystemStatus.EmergencyMode,
			VPPActive:        a.SystemStatus.VPPActive,
		})
		prevLastPollTS = tsLocal
	}

	return MCPDailyHistoryRes{
		Date:                      dateStr,
		TimeZone:                  loc.String(),
		TotalSolarKWH:             roundSummaryFloat(totalSolarKWH, 2),
		TotalHomeUsedKWH:          roundSummaryFloat(totalHomeUsedKWH, 2),
		TotalBatteryChargedKWH:    roundSummaryFloat(totalBatteryChargedKWH, 2),
		TotalBatteryDischargedKWH: roundSummaryFloat(totalBatteryDischargedKWH, 2),
		TotalGridImportedKWH:      roundSummaryFloat(totalGridImportedKWH, 2),
		TotalGridExportedKWH:      roundSummaryFloat(totalGridExportedKWH, 2),
		HadGridUnavailable:        hadGridUnavailable,
		HadEmergencyMode:          hadEmergencyMode,
		HadVPPActive:              hadVPPActive,
		AlarmNames:                alarmNames,
		Hours:                     hours,
		Decisions:                 decisions,
	}, nil
}
