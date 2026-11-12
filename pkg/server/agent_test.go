package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleAgentSummary(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	// Fixed "now": 2026-06-15 14:30 CDT
	fixedNow := time.Date(2026, 6, 15, 14, 30, 0, 0, loc)
	todayStart := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)

	t.Run("JSONOutputWithBearerAppToken", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := &Server{
			storage:           mockStore,
			appsEncryptionKey: testAppsEncryptionKey,
			nowFunc:           func() time.Time { return fixedNow.UTC() },
		}

		rawToken, tokenHash, err := srv.createAppToken("user-1", "site-1", "app-1")
		require.NoError(t, err)

		mockStore.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
			Apps: []types.UserApp{
				{
					ID:        "app-1",
					Name:      "Home Assistant",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TokenHash: tokenHash,
				},
			},
		}, nil).Once()
		mockStore.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()

		mockStore.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS:             "tesla",
			UtilityProvider: "comed",
			MinBatterySOC:   10,
			Location: &types.SiteLocation{
				TimeZone: "America/Chicago",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		actionTS := time.Date(2026, 6, 15, 14, 20, 0, 0, loc)
		p1Start := time.Date(2026, 6, 15, 14, 0, 0, 0, loc)
		p1End := time.Date(2026, 6, 15, 15, 0, 0, 0, loc)
		p2Start := time.Date(2026, 6, 15, 15, 0, 0, 0, loc)
		p2End := time.Date(2026, 6, 15, 16, 0, 0, 0, loc)
		pTomorrowStart := time.Date(2026, 6, 16, 12, 0, 0, 0, loc)
		pTomorrowEnd := time.Date(2026, 6, 16, 13, 0, 0, 0, loc)
		pBeyond24hStart := time.Date(2026, 6, 16, 16, 0, 0, 0, loc)
		pBeyond24hEnd := time.Date(2026, 6, 16, 17, 0, 0, 0, loc)

		mockStore.On("GetLatestAction", mock.Anything, "site-1").Return(&types.Action{
			Timestamp:       actionTS,
			SystemTimestamp: actionTS,
			BatteryMode:     types.BatteryModeLoad,
			SolarMode:       types.SolarModeAny,
			Reason:          types.ActionReasonSufficientBattery,
			Description:     "Powering home from battery",
			ReserveSOC:      20,
			SystemStatus: types.SystemStatus{
				Timestamp:          actionTS,
				BatterySOC:         78.44,
				BatteryCapacityKWH: 13.5,
				BatteryKW:          1.25,
				SolarKW:            4.5,
				HomeKW:             2.1,
				GridKW:             -3.65,
				TimeLocation:       "America/Chicago",
			},
			Plan: &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       p1Start,
						TSEnd:         p1End,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeLoad,
						SolarMode:     types.SolarModeAny,
						Reason:        types.ActionReasonSufficientBattery,
						StartSOC:      75.0,
						EndSOC:        80.0,
						ReserveSOC:    20.0,
						SolarKWH:      4.0,
						LoadKWH:       2.0,
						GridExportKWH: 1.5,
						ImportDollars: 0.085,
						ExportDollars: 0.045,
					},
					{
						TSStart:       p2Start,
						TSEnd:         p2End,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeExport,
						SolarMode:     types.SolarModeExport,
						Reason:        types.ActionReasonDischargeAtPeak,
						StartSOC:      80.0,
						EndSOC:        50.0,
						ReserveSOC:    20.0,
						SolarKWH:      3.5,
						LoadKWH:       2.2,
						GridExportKWH: 5.3,
						ImportDollars: 0.18,
						ExportDollars: 0.14,
					},
					{
						TSStart:       pTomorrowStart,
						TSEnd:         pTomorrowEnd,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeChargeAny,
						SolarMode:     types.SolarModeAny,
						StartSOC:      30.0,
						EndSOC:        60.0,
						SolarKWH:      6.25,
						LoadKWH:       1.5,
						ImportDollars: 0.03,
						ExportDollars: 0.01,
					},
					{
						TSStart:       pBeyond24hStart,
						TSEnd:         pBeyond24hEnd,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeLoad,
						SolarMode:     types.SolarModeAny,
						StartSOC:      60.0,
						EndSOC:        55.0,
						SolarKWH:      5.0,
						LoadKWH:       1.8,
						ImportDollars: 0.09,
						ExportDollars: 0.05,
					},
				},
			},
		}, nil).Once()

		// Actual solar history through 14:00 today (hStart 13:00 -> hEnd 14:00)
		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", todayStart, fixedNow).Return([]types.DailyEnergyStats{
			{
				TSDayStart: todayStart,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: time.Date(2026, 6, 15, 12, 0, 0, 0, loc),
						SolarKWH:    5.2,
					},
					{
						TSHourStart: time.Date(2026, 6, 15, 13, 0, 0, 0, loc),
						SolarKWH:    4.8,
					},
				},
			},
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
		assert.Equal(t, "noindex, nofollow, noarchive, nosnippet", w.Header().Get("X-Robots-Tag"))
		assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
		assert.Equal(t, "Accept, Authorization, Cookie", w.Header().Get("Vary"))

		var res AgentSummaryRes
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))

		assert.Equal(t, "America/Chicago", res.TimeZone)
		assert.Equal(t, 78.4, res.Status.BatterySOC)
		assert.Equal(t, 13.5, res.Status.BatteryCapacityKWH)
		assert.Equal(t, 1.25, res.Status.BatteryKW)
		assert.Equal(t, 4.5, res.Status.SolarKW)
		assert.Equal(t, 2.1, res.Status.HomeKW)
		assert.Equal(t, -3.65, res.Status.GridKW)
		assert.Equal(t, types.BatteryModeLoad, res.Status.BatteryMode)
		assert.Equal(t, "Power Home", res.Status.BatteryModeLabel)
		assert.Equal(t, types.SolarModeAny, res.Status.SolarMode)
		assert.Equal(t, "Power Home & Export Surplus", res.Status.SolarModeLabel)
		assert.Equal(t, "Powering home from battery", res.Status.Description)
		assert.Equal(t, 20.0, res.Status.ReserveSOC)
		assert.Equal(t, 0.085, res.Status.ImportDollarsPerKWH)
		assert.Equal(t, 0.045, res.Status.ExportDollarsPerKWH)

		// Today actual: 5.2 + 4.8 = 10.0 kWh
		// Today remaining: p1 (14:00-15:00, now=14:30 -> 4.0 kWh since latestActualEnd=14:00 <= p1Start) + p2 (3.5 kWh) = 7.5 kWh
		// Tomorrow forecast: pTomorrow (6.25) + pBeyond24h (5.0) = 11.25 kWh
		assert.Equal(t, 10.0, res.SolarForecast.TodayActualSolarKWH)
		assert.Equal(t, 7.5, res.SolarForecast.TodayRemainingForecastSolarKWH)
		assert.Equal(t, 17.5, res.SolarForecast.TodayTotalEstimatedSolarKWH)
		assert.Equal(t, 11.25, res.SolarForecast.TomorrowForecastSolarKWH)

		// Plan only includes periods overlapping [14:30, +24h] -> p1, p2, pTomorrow (not pBeyond24h at 16:00 tomorrow)
		if assert.Len(t, res.Plan, 3) {
			assert.Equal(t, "Power Home", res.Plan[0].BatteryModeLabel)
			assert.Equal(t, "Export to Grid", res.Plan[1].BatteryModeLabel)
			assert.Equal(t, "Charge from Grid/Solar", res.Plan[2].BatteryModeLabel)
		}

		mockStore.AssertExpectations(t)
	})

	t.Run("TextOutputWithQueryAppToken", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := &Server{
			storage:           mockStore,
			appsEncryptionKey: testAppsEncryptionKey,
			nowFunc:           func() time.Time { return fixedNow.UTC() },
		}

		rawToken, tokenHash, err := srv.createAppToken("user-1", "site-1", "app-1")
		require.NoError(t, err)

		mockStore.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
			Apps: []types.UserApp{
				{
					ID:        "app-1",
					Name:      "Claude Web Fetch",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TokenHash: tokenHash,
				},
			},
		}, nil).Once()
		mockStore.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()

		mockStore.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS:             "franklinwh",
			UtilityProvider: "comed",
			Pause:           true,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		actionTS := time.Date(2026, 6, 15, 14, 20, 0, 0, loc)
		p1Start := time.Date(2026, 6, 15, 14, 0, 0, 0, loc)
		p1End := time.Date(2026, 6, 15, 15, 0, 0, 0, loc)

		mockStore.On("GetLatestAction", mock.Anything, "site-1").Return(&types.Action{
			Timestamp:       actionTS,
			SystemTimestamp: actionTS,
			BatteryMode:     types.BatteryModeStandby,
			SolarMode:       types.SolarModeNoExport,
			Description:     "Paused by user",
			ReserveSOC:      15,
			Paused:          true,
			SystemStatus: types.SystemStatus{
				Timestamp:          actionTS,
				BatterySOC:         92.0,
				BatteryCapacityKWH: 15.0,
				BatteryKW:          0,
				SolarKW:            1.5,
				HomeKW:             1.5,
				GridKW:             0,
				TimeLocation:       "America/Chicago",
				GridUnavailable:    true,
				EmergencyMode:      true,
				VPPActive:          true,
			},
			Plan: &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       p1Start,
						TSEnd:         p1End,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeStandby,
						SolarMode:     types.SolarModeNoExport,
						StartSOC:      92.0,
						EndSOC:        92.0,
						SolarKWH:      1.5,
						LoadKWH:       1.5,
						ImportDollars: 0.05,
						ExportDollars: -0.01,
					},
				},
			},
		}, nil).Once()

		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", todayStart, fixedNow).Return(([]types.DailyEnergyStats)(nil), nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary?token="+url.QueryEscape(rawToken)+"&format=text", nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))

		body := w.Body.String()
		assert.Contains(t, body, "# RateRudder Energy Summary")
		assert.Contains(t, body, "## Current Status")
		assert.Contains(t, body, "- **Battery SOC:** 92.0% (Capacity: 15.0 kWh, Reserve: 15.0%)")
		assert.Contains(t, body, "- **Battery Mode:** Standby")
		assert.Contains(t, body, "- **Solar Mode:** Curtailed (No Grid Export)")
		assert.Contains(t, body, "- **Automation Status:** Paused")
		assert.Contains(t, body, "- **Grid Status:** Grid Unavailable (Outage)")
		assert.Contains(t, body, "- **Emergency Mode:** Active")
		assert.Contains(t, body, "- **VPP Event:** Active")
		assert.Contains(t, body, "## Solar Forecast")
		assert.Contains(t, body, "## Next 24-Hour Plan")
		assert.Contains(t, body, "| 2026-06-15 14:00 - 15:00 | 92.0% -> 92.0% | 1.50 | 1.50 | $0.0500 | $-0.0100 | Standby |")

		mockStore.AssertExpectations(t)
	})

	t.Run("HTMLOutputDefaultWhenFormatOmitted", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := &Server{
			storage:           mockStore,
			appsEncryptionKey: testAppsEncryptionKey,
			nowFunc:           func() time.Time { return fixedNow.UTC() },
		}

		rawToken, tokenHash, err := srv.createAppToken("user-1", "site-1", "app-1")
		require.NoError(t, err)

		mockStore.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
			Apps: []types.UserApp{
				{
					ID:        "app-1",
					Name:      "Gemini",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TokenHash: tokenHash,
				},
			},
		}, nil).Once()
		mockStore.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()

		mockStore.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS:             "franklinwh",
			UtilityProvider: "comed",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		actionTS := time.Date(2026, 6, 15, 14, 20, 0, 0, loc)
		p1Start := time.Date(2026, 6, 15, 14, 0, 0, 0, loc)
		p1End := time.Date(2026, 6, 15, 15, 0, 0, 0, loc)

		mockStore.On("GetLatestAction", mock.Anything, "site-1").Return(&types.Action{
			Timestamp:       actionTS,
			SystemTimestamp: actionTS,
			BatteryMode:     types.BatteryModeStandby,
			SolarMode:       types.SolarModeAny,
			Description:     "Holding battery <reserve>",
			ReserveSOC:      20,
			SystemStatus: types.SystemStatus{
				Timestamp:          actionTS,
				BatterySOC:         85.0,
				BatteryCapacityKWH: 15.0,
				BatteryKW:          0,
				SolarKW:            3.2,
				HomeKW:             1.2,
				GridKW:             -2.0,
				TimeLocation:       "America/Chicago",
			},
			Plan: &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       p1Start,
						TSEnd:         p1End,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeStandby,
						SolarMode:     types.SolarModeAny,
						StartSOC:      85.0,
						EndSOC:        85.0,
						SolarKWH:      3.2,
						LoadKWH:       1.2,
						ImportDollars: 0.06,
						ExportDollars: 0.03,
					},
				},
			},
		}, nil).Once()

		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", todayStart, fixedNow).Return(([]types.DailyEnergyStats)(nil), nil).Once()

		// Omit format parameter (simulating the default Settings Summary URL fetched by Gemini)
		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary?token="+url.QueryEscape(rawToken), nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
		assert.Equal(t, "noindex, nofollow, noarchive, nosnippet", w.Header().Get("X-Robots-Tag"))
		assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
		assert.Equal(t, "Accept, Authorization, Cookie", w.Header().Get("Vary"))

		body := w.Body.String()
		assert.Contains(t, body, "<!DOCTYPE html>")
		assert.Contains(t, body, "<meta name=\"robots\" content=\"noindex, nofollow\">")
		assert.Contains(t, body, "<h1>RateRudder Energy Summary</h1>")
		assert.Contains(t, body, "<h2>Current Status</h2>")
		assert.Contains(t, body, "<li><strong>Battery SOC:</strong> 85.0% (Capacity: 15.0 kWh, Reserve: 20.0%)</li>")
		assert.Contains(t, body, "<li><strong>Decision:</strong> Holding battery &lt;reserve&gt;</li>")
		assert.Contains(t, body, "<h2>Solar Forecast</h2>")
		assert.Contains(t, body, "<h2>Next 24-Hour Plan</h2>")
		assert.Contains(t, body, "<tr><td>2026-06-15 14:00 - 15:00</td><td>85.0% -&gt; 85.0%</td><td>3.20</td><td>1.20</td><td>$0.0600</td><td>$0.0300</td><td>Standby</td></tr>")

		mockStore.AssertExpectations(t)
	})

	t.Run("NoESSConfiguredReturnsBadRequest", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := &Server{
			storage:           mockStore,
			appsEncryptionKey: testAppsEncryptionKey,
			bypassAuth:        true,
			singleSite:        true,
			nowFunc:           func() time.Time { return fixedNow.UTC() },
		}

		mockStore.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStore.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "no ESS configured")
		mockStore.AssertExpectations(t)
	})

	t.Run("NoLatestActionReturnsNotFound", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := &Server{
			storage:           mockStore,
			appsEncryptionKey: testAppsEncryptionKey,
			bypassAuth:        true,
			singleSite:        true,
			nowFunc:           func() time.Time { return fixedNow.UTC() },
		}

		mockStore.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStore.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "tesla",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockStore.On("GetLatestAction", mock.Anything, types.SiteIDNone).Return((*types.Action)(nil), nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "no status available")
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidFormatQueryParamReturnsBadRequest", func(t *testing.T) {
		srv := &Server{
			appsEncryptionKey: testAppsEncryptionKey,
			bypassAuth:        true,
			singleSite:        true,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary?format=xml", nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid format: must be json, text, html, or auto")
	})

	t.Run("AppsNotEnabledReturnsNotImplemented", func(t *testing.T) {
		srv := &Server{
			appsEncryptionKey: "",
			bypassAuth:        true,
			singleSite:        true,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		w := httptest.NewRecorder()

		srv.authMiddleware(http.HandlerFunc(srv.handleAgentSummary)).ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotImplemented, w.Code)
		assert.Contains(t, w.Body.String(), "apps not enabled")
	})

	t.Run("AcceptHeaderSelectsEarliestMatchingFormat", func(t *testing.T) {
		reqTextFirst := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		reqTextFirst.Header.Set("Accept", "text/markdown, text/plain, text/html;q=0.9, */*;q=0.8")
		fmtText, err := resolveAgentSummaryFormat(reqTextFirst)
		require.NoError(t, err)
		assert.Equal(t, "text", fmtText)

		reqJSONFirst := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		reqJSONFirst.Header.Set("Accept", "application/json, text/html;q=0.8")
		fmtJSON, err := resolveAgentSummaryFormat(reqJSONFirst)
		require.NoError(t, err)
		assert.Equal(t, "json", fmtJSON)

		reqHTMLFirst := httptest.NewRequest(http.MethodGet, "/api/agent/summary", nil)
		reqHTMLFirst.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
		fmtHTML, err := resolveAgentSummaryFormat(reqHTMLFirst)
		require.NoError(t, err)
		assert.Equal(t, "html", fmtHTML)
	})
}
