package ess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnphase(t *testing.T) {
	t.Run("Authenticate successful login", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login/login.json" {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))

				err := r.ParseForm()
				assert.NoError(t, err)
				assert.Equal(t, "test@example.com", r.Form.Get("user[email]"))
				assert.Equal(t, "password123", r.Form.Get("user[password]"))

				res := enphaseLoginResponse{
					Message:      "success",
					SessionID:    "session123",
					ManagerToken: "token123",
					SystemID:     123456789,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			if r.URL.Path == "/app-api/123456789/data.json" {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "session123", r.Header.Get("e-auth-token"))
				assert.Contains(t, r.Header.Get("Cookie"), "_enlighten_4_session=session123")
				assert.Contains(t, r.Header.Get("Cookie"), "enlighten_manager_token_production=token123")

				res := enphaseDataResult{
					App: enphaseApp{
						UserID: 987654,
					},
					State: enphaseState{
						SiteID: 123,
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username: "test@example.com",
				Password: "password123",
			},
		}

		updatedCreds, changed, err := e.Authenticate(context.Background(), creds)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "session123", updatedCreds.Enphase.SessionID)
		assert.Equal(t, "token123", updatedCreds.Enphase.ManagerToken)
		assert.Equal(t, 123456789, updatedCreds.Enphase.SystemID)
		assert.Equal(t, 987654, updatedCreds.Enphase.UserID)
	})

	t.Run("Authenticate restored credentials", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123456789/data.json" {
				res := enphaseDataResult{
					State: enphaseState{
						SiteID: 123,
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "test@example.com",
				Password:     "password123",
				SessionID:    "session123",
				ManagerToken: "token123",
				SystemID:     123456789,
			},
		}

		updatedCreds, changed, err := e.Authenticate(context.Background(), creds)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, "session123", updatedCreds.Enphase.SessionID)
	})

	t.Run("Authenticate retry login on 401", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path == "/app-api/123456789/data.json" {
				if calls == 1 {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte("unauthorized"))
					return
				}
				res := enphaseDataResult{State: enphaseState{SiteID: 123}}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}
			if r.URL.Path == "/login/login.json" {
				res := enphaseLoginResponse{
					Message:      "success",
					SessionID:    "new_session",
					ManagerToken: "new_token",
					SystemID:     123456789,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "test@example.com",
				Password:     "password123",
				SessionID:    "old_session",
				ManagerToken: "old_token",
				SystemID:     123456789,
			},
		}

		updatedCreds, changed, err := e.Authenticate(context.Background(), creds)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "new_session", updatedCreds.Enphase.SessionID)
		assert.Equal(t, 3, calls) // 1st data.json (401), 2nd login.json, 3rd data.json (200)
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		today := time.Now().UTC()
		todayStr := today.Format("2006-01-02")
		yesterday := today.AddDate(0, 0, -1)
		yesterdayStr := yesterday.Format("2006-01-02")

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				json.NewEncoder(w).Encode(map[string]any{
					"app": map[string]any{
						"timezone": "UTC",
					},
				})
				return
			}
			if r.URL.Path == "/pv/systems/123/today" {
				date := r.URL.Query().Get("date")
				if date == todayStr {
					json.NewEncoder(w).Encode(map[string]any{
						"start_date": todayStr,
						"stats": []map[string]any{
							{
								"production":      []float64{0, 0, 0, 800},
								"consumption":     []float64{0, 0, 0, 0},
								"solar_home":      []float64{0, 0, 0, 400},
								"solar_grid":      []float64{0, 0, 0, 400},
								"soc":             []float64{30, 35, 40, 45},
								"start_time":      today.Truncate(24 * time.Hour).Unix(),
								"interval_length": 900,
							},
						},
					})
					return
				}
			}
			if r.URL.Path == "/pv/systems/123/daily_energy" {
				startDate := r.URL.Query().Get("start_date")
				endDate := r.URL.Query().Get("end_date")

				var stats []map[string]any
				if startDate == "2026-04-13" && endDate == "2026-04-14" {
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 1000},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 500},
						"solar_grid":      []float64{0, 0, 0, 500},
						"soc":             []float64{10, 15, 20, 25},
						"start_time":      1776038400, // 2026-04-13T00:00:00Z
						"interval_length": 900,
					})
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 1200},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 600},
						"solar_grid":      []float64{0, 0, 0, 600},
						"soc":             []float64{20, 25, 30, 35},
						"start_time":      1776124800, // 2026-04-14T00:00:00Z
						"interval_length": 900,
					})
				} else if startDate == "2026-04-13" && endDate == "2026-04-15" {
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 1000},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 500},
						"solar_grid":      []float64{0, 0, 0, 500},
						"soc":             []float64{10, 15, 20, 25},
						"start_time":      1776038400, // 2026-04-13T00:00:00Z
						"interval_length": 900,
					})
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 1200},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 600},
						"solar_grid":      []float64{0, 0, 0, 600},
						"soc":             []float64{20, 25, 30, 35},
						"start_time":      1776124800, // 2026-04-14T00:00:00Z
						"interval_length": 900,
					})
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 1400},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 700},
						"solar_grid":      []float64{0, 0, 0, 700},
						"soc":             []float64{30, 35, 40, 45},
						"start_time":      1776211200, // 2026-04-15T00:00:00Z
						"interval_length": 900,
					})
				} else if startDate == yesterdayStr && endDate == yesterdayStr {
					stats = append(stats, map[string]any{
						"production":      []float64{0, 0, 0, 600},
						"consumption":     []float64{0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 300},
						"solar_grid":      []float64{0, 0, 0, 300},
						"soc":             []float64{5, 10, 15, 20},
						"start_time":      yesterday.Truncate(24 * time.Hour).Unix(),
						"interval_length": 900,
					})
				}

				json.NewEncoder(w).Encode(map[string]any{
					"start_date": startDate,
					"end_date":   endDate,
					"stats":      stats,
				})
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(ts.URL)
		e.systemID = 123
		e.sessionID = "session123"

		t.Run("HistoricalOnly_SingleDay", func(t *testing.T) {
			start := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
			end := time.Date(2026, 4, 13, 23, 59, 59, 0, time.UTC)

			stats, err := e.GetEnergyHistory(context.Background(), start, end)
			require.NoError(t, err)

			if assert.Len(t, stats, 1) {
				assert.Equal(t, start, stats[0].TSDayStart)
				if assert.Len(t, stats[0].Hourly, 1) {
					hourStat := stats[0].Hourly[0]
					assert.Equal(t, start, hourStat.TSHourStart)
					assert.Equal(t, 1.0, hourStat.SolarKWH)
					assert.Equal(t, 0.5, hourStat.SolarToHomeKWH)
					assert.Equal(t, 0.5, hourStat.SolarToGridKWH)
					assert.Equal(t, 0.5, hourStat.HomeKWH)
					assert.Equal(t, 10.0, hourStat.MinBatterySOC)
					assert.Equal(t, 25.0, hourStat.MaxBatterySOC)
				}
			}
		})

		t.Run("HistoricalOnly_MultipleDays", func(t *testing.T) {
			start := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
			end := time.Date(2026, 4, 14, 23, 59, 59, 0, time.UTC)

			stats, err := e.GetEnergyHistory(context.Background(), start, end)
			require.NoError(t, err)

			if assert.Len(t, stats, 2) {
				assert.Equal(t, time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC), stats[0].TSDayStart)
				assert.Equal(t, time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC), stats[1].TSDayStart)

				if assert.Len(t, stats[0].Hourly, 1) {
					assert.Equal(t, 1.0, stats[0].Hourly[0].SolarKWH)
					assert.Equal(t, 10.0, stats[0].Hourly[0].MinBatterySOC)
					assert.Equal(t, 25.0, stats[0].Hourly[0].MaxBatterySOC)
				}
				if assert.Len(t, stats[1].Hourly, 1) {
					assert.Equal(t, 1.2, stats[1].Hourly[0].SolarKWH)
					assert.Equal(t, 20.0, stats[1].Hourly[0].MinBatterySOC)
					assert.Equal(t, 35.0, stats[1].Hourly[0].MaxBatterySOC)
				}
			}
		})

		t.Run("TodayOnly", func(t *testing.T) {
			start := today.Truncate(24 * time.Hour)
			end := start.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

			stats, err := e.GetEnergyHistory(context.Background(), start, end)
			require.NoError(t, err)

			if assert.Len(t, stats, 1) {
				assert.Equal(t, start, stats[0].TSDayStart)
				if assert.Len(t, stats[0].Hourly, 1) {
					assert.Equal(t, 0.8, stats[0].Hourly[0].SolarKWH)
					assert.Equal(t, 30.0, stats[0].Hourly[0].MinBatterySOC)
					assert.Equal(t, 45.0, stats[0].Hourly[0].MaxBatterySOC)
				}
			}
		})

		t.Run("HistoricalAndToday", func(t *testing.T) {
			start := yesterday.Truncate(24 * time.Hour)
			end := today.Truncate(24 * time.Hour).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

			stats, err := e.GetEnergyHistory(context.Background(), start, end)
			require.NoError(t, err)

			if assert.Len(t, stats, 2) {
				assert.Equal(t, yesterday.Truncate(24*time.Hour), stats[0].TSDayStart)
				assert.Equal(t, today.Truncate(24*time.Hour), stats[1].TSDayStart)

				if assert.Len(t, stats[0].Hourly, 1) {
					assert.Equal(t, 0.6, stats[0].Hourly[0].SolarKWH)
					assert.Equal(t, 5.0, stats[0].Hourly[0].MinBatterySOC)
					assert.Equal(t, 20.0, stats[0].Hourly[0].MaxBatterySOC)
				}
				if assert.Len(t, stats[1].Hourly, 1) {
					assert.Equal(t, 0.8, stats[1].Hourly[0].SolarKWH)
					assert.Equal(t, 30.0, stats[1].Hourly[0].MinBatterySOC)
					assert.Equal(t, 45.0, stats[1].Hourly[0].MaxBatterySOC)
				}
			}
		})
	})

	t.Run("Authenticate trigger OTP", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/generate_login_otp.json" {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))

				err := r.ParseForm()
				assert.NoError(t, err)
				assert.Equal(t, "dGVzdEBleGFtcGxlLmNvbQ==", r.Form.Get("email"))
				assert.Equal(t, "en", r.Form.Get("locale"))
				assert.Equal(t, "ENHO", r.Form.Get("source"))

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"success":true,"isBlocked":false}`))
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username: "test@example.com",
			},
		}

		_, changed, err := e.Authenticate(context.Background(), creds)
		require.ErrorIs(t, err, ErrNeedsNextStage)
		assert.False(t, changed)
	})

	t.Run("Authenticate validate OTP code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/validate_login_otp.json" {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))

				err := r.ParseForm()
				assert.NoError(t, err)
				assert.Equal(t, "dGVzdEBleGFtcGxlLmNvbQ==", r.Form.Get("email"))
				assert.Equal(t, "MTIzNDU2", r.Form.Get("otp"))
				assert.Equal(t, "true", r.Form.Get("xhrFields[withCredentials]"))

				res := enphaseLoginResponse{
					Message:      "success",
					SessionID:    "sessionotp123",
					ManagerToken: "tokenotp123",
					SystemID:     987654321,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			if r.URL.Path == "/app-api/987654321/data.json" {
				res := enphaseDataResult{State: enphaseState{SiteID: 123}}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username: "test@example.com",
				Code:     "123456",
			},
		}

		updatedCreds, changed, err := e.Authenticate(context.Background(), creds)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "sessionotp123", updatedCreds.Enphase.SessionID)
		assert.Equal(t, "tokenotp123", updatedCreds.Enphase.ManagerToken)
		assert.Equal(t, 987654321, updatedCreds.Enphase.SystemID)
		assert.Empty(t, updatedCreds.Enphase.Code)
	})

	t.Run("GetStatus", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				res := enphaseDataResult{
					App: enphaseApp{
						Timezone: "America/Chicago",
					},
					State: enphaseState{
						SiteID: 123,
						BatteryInfo: enphaseBatteryInfo{
							NumberOfBatteries: 2,
							TotalCapacityWH:   6720,
						},
						HasBatteries:    true,
						BatteryGridMode: "NoImportOrExport",
						IsEncharge5P:    false,
						Devices: []enphaseDevice{
							{Name: "IQ Battery 3", SerialNumber: "1", Connected: true, Status: "normal"},
							{Name: "IQ Battery 3", SerialNumber: "2", Connected: false, Status: "error"},
						},
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: 30,
							DrEventActive:           true,
							SevereWeatherWatch:      "disabled",
							ShowSevereWeatherAlert:  true,
							StormAlertMessage: &enphaseStormAlertMessage{
								AlertName: "Blizzard Warning",
							},
							EnvStorageSettings: map[string]enphaseEnvStorageSettings{
								"envoy1": {SOC: 75, Mode: "self-consumption"},
							},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			if r.URL.Path == "/pv/systems/123/today" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"start_date": "2026-06-06",
					"battery_details": {
						"aggregate_soc": 75
					},
					"stats": [{
						"production": [1000],
						"consumption": [800],
						"solar_home": [400],
						"solar_battery": [400],
						"solar_grid": [200],
						"battery_home": [100],
						"battery_grid": [0],
						"grid_battery": [0],
						"grid_home": [300],
						"start_time": 1776038400,
						"interval_length": 900
					}]
				}`))
				return
			}

			if r.URL.Path == "/pv/settings/123/battery_status.json" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"max_capacity": 6.72,
					"available_power": 2.56
				}`))
				return
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.settings = types.Settings{
			MinBatterySOC: 20,
		}

		status, err := e.GetStatus(context.Background())
		require.NoError(t, err)

		assert.Equal(t, 75.0, status.BatterySOC)
		assert.Equal(t, 6.72, status.BatteryCapacityKWH)
		assert.Equal(t, 2.56, status.MaxBatteryDischargeKW)
		assert.Equal(t, 2.56, status.MaxBatteryChargeKW)

		assert.InDelta(t, 4.0, status.SolarKW, 0.0001)
		assert.InDelta(t, -1.2, status.BatteryKW, 0.0001)
		assert.InDelta(t, 0.4, status.GridKW, 0.0001)
		assert.InDelta(t, 3.2, status.HomeKW, 0.0001)

		assert.True(t, status.ElevatedMinBatterySOC)
		assert.True(t, status.BatteryAboveMinSOC)
		assert.False(t, status.EmergencyMode)
		assert.True(t, status.VPPActive)

		assert.Len(t, status.Storms, 1)
		assert.Equal(t, "Severe weather alert active", status.Storms[0].Description)

		assert.Len(t, status.Alarms, 1)
		assert.Equal(t, "IQ Battery 3", status.Alarms[0].Name)
		assert.Contains(t, status.Alarms[0].Description, "offline or in status")
	})

	t.Run("SetModes", func(t *testing.T) {
		var lastPayload *enphaseBatterySchedulesPayload
		var postCalled bool
		var settingsChargeFromGrid bool
		var settingsScheduleEnabled bool
		var useRequestedConfig bool
		var settingsProfile string = "self-consumption"
		var requestedProfile string = "self-consumption"
		var putCalled bool
		var lastSettingsPut *struct {
			ChargeFromGrid                bool   `json:"chargeFromGrid"`
			ChargeFromGridScheduleEnabled bool   `json:"chargeFromGridScheduleEnabled"`
			AcceptedItcDisclaimer         string `json:"acceptedItcDisclaimer"`
			ChargeBeginTime               int    `json:"chargeBeginTime"`
			ChargeEndTime                 int    `json:"chargeEndTime"`
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				res := enphaseDataResult{
					State: enphaseState{
						SiteID:          123,
						BatteryGridMode: "NoImportOrExport",
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: 30,
							Usage:                   "self-consumption",
							ChargeFromGrid:          false,
							EnvStorageSettings: map[string]enphaseEnvStorageSettings{
								"envoy1": {SOC: 80},
							},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}

			if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" {
				assert.Equal(t, "123456", r.URL.Query().Get("userId"))
				if r.Method == "GET" {
					w.Header().Set("X-Csrf-Token", "settings_csrf_123")
					w.WriteHeader(http.StatusOK)
					chargeVal := "false"
					if settingsChargeFromGrid {
						chargeVal = "true"
					}
					schedVal := "false"
					if settingsScheduleEnabled {
						schedVal = "true"
					}
					requestedJson := ""
					if useRequestedConfig {
						requestedJson = `, "requestedConfig": {` +
							`"chargeFromGrid": true,` +
							`"chargeFromGridScheduleEnabled": false,` +
							`"profile": "` + requestedProfile + `",` +
							`"batteryBackupPercentage": 20` +
							`}`
					}
					w.Write([]byte(`{"data": {` +
						`"chargeFromGrid": ` + chargeVal + `,` +
						`"chargeFromGridScheduleEnabled": ` + schedVal + `,` +
						`"profile": "` + settingsProfile + `",` +
						`"batteryBackupPercentage": 30,` +
						`"acceptedItcDisclaimer": "2026-04-04T03:00:41.108Z",` +
						`"chargeBeginTime": 60,` +
						`"chargeEndTime": 240` +
						requestedJson +
						`}}`))
					return
				}
				if r.Method == "PUT" {
					assert.Equal(t, "settings_csrf_123", r.Header.Get("X-Xsrf-Token"))
					var body struct {
						ChargeFromGrid                bool   `json:"chargeFromGrid"`
						ChargeFromGridScheduleEnabled bool   `json:"chargeFromGridScheduleEnabled"`
						AcceptedItcDisclaimer         string `json:"acceptedItcDisclaimer"`
						ChargeBeginTime               int    `json:"chargeBeginTime"`
						ChargeEndTime                 int    `json:"chargeEndTime"`
					}
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)
					lastSettingsPut = &body
					putCalled = true
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"message":"success"}`))
					return
				}
			}

			if r.URL.Path == "/service/batteryConfig/api/v1/profile/123" {
				if r.Method == "GET" {
					assert.Equal(t, "123456", r.URL.Query().Get("userId"))
					assert.Equal(t, "enho", r.URL.Query().Get("source"))
					assert.Equal(t, "en", r.URL.Query().Get("locale"))
					w.Header().Set("X-Csrf-Token", "profile_csrf_123")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"data": {}}`))
					return
				}
				if r.Method == "PUT" {
					assert.Empty(t, r.URL.Query().Get("userId"))
					assert.Equal(t, "profile_csrf_123", r.Header.Get("X-Xsrf-Token"))
					var body struct {
						Profile                 string `json:"profile"`
						BatteryBackupPercentage int    `json:"batteryBackupPercentage"`
					}
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)
					lastPayload = &enphaseBatterySchedulesPayload{
						Usage:                   body.Profile,
						BatteryBackupPercentage: body.BatteryBackupPercentage,
					}
					postCalled = true
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"message":"success"}`))
					return
				}
			}

			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.userID = 123456
		e.settings = types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: false,
		}

		err := e.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err)
		if assert.True(t, postCalled) {
			assert.Equal(t, 80, lastPayload.BatteryBackupPercentage)
			assert.Equal(t, "self-consumption", lastPayload.Usage)
		}
		assert.False(t, putCalled)

		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil

		err = e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		if assert.True(t, postCalled) {
			assert.Equal(t, 100, lastPayload.BatteryBackupPercentage)
		}
		if assert.True(t, putCalled) {
			assert.True(t, lastSettingsPut.ChargeFromGrid)
			assert.False(t, lastSettingsPut.ChargeFromGridScheduleEnabled)
		}

		// Test disabling schedules
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = false
		settingsScheduleEnabled = true
		e.settings.GridChargeBatteries = false

		err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		if assert.True(t, postCalled) {
			assert.Equal(t, 20, lastPayload.BatteryBackupPercentage)
		}
		if assert.True(t, putCalled) {
			assert.False(t, lastSettingsPut.ChargeFromGrid)
			assert.False(t, lastSettingsPut.ChargeFromGridScheduleEnabled)
		}

		// Test requestedConfig checks
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = false
		settingsScheduleEnabled = false
		useRequestedConfig = true
		e.settings.GridChargeBatteries = true

		err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.False(t, postCalled)
		assert.False(t, putCalled)

		// Test backup mode guard (current profile is backup_only)
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = false
		settingsScheduleEnabled = false
		useRequestedConfig = false
		settingsProfile = "backup_only"

		err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.ErrorContains(t, err, "device is in backup mode")
		assert.False(t, postCalled)
		assert.False(t, putCalled)

		// Test backup mode guard (pending profile is backup_only)
		settingsProfile = "self-consumption"
		requestedProfile = "backup_only"
		useRequestedConfig = true

		err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.ErrorContains(t, err, "device is in backup mode")
		assert.False(t, postCalled)
		assert.False(t, putCalled)
	})

	t.Run("SetModes storm mode guard", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				res := enphaseDataResult{
					State: enphaseState{
						SiteID: 123,
						BatteryConfig: enphaseBatteryConfig{
							SevereWeatherWatch: "active",
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123

		err := e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
		require.ErrorContains(t, err, "device is in storm mode")
	})

	t.Run("SetModes respects ChargeToSOC", func(t *testing.T) {
		var postCalled bool
		var lastPayload *enphaseBatterySchedulesPayload
		var putCalled bool
		var lastSettingsPut *enphaseBatterySettingsPayload

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				res := enphaseDataResult{
					State: enphaseState{
						SiteID: 123,
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: 30,
							Usage:                   "self-consumption",
							ChargeFromGrid:          false,
							EnvStorageSettings: map[string]enphaseEnvStorageSettings{
								"envoy1": {SOC: 80},
							},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(res)
				return
			}
			if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" {
				if r.Method == "GET" {
					w.Header().Set("X-Csrf-Token", "settings_csrf_123")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"data": {
						"chargeFromGrid": false,
						"chargeFromGridScheduleEnabled": false,
						"profile": "self-consumption",
						"batteryBackupPercentage": 30
					}}`))
					return
				}
				if r.Method == "PUT" {
					assert.Equal(t, "settings_csrf_123", r.Header.Get("X-Xsrf-Token"))
					var body enphaseBatterySettingsPayload
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)
					lastSettingsPut = &body
					putCalled = true
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"message":"success"}`))
					return
				}
			}
			if r.URL.Path == "/service/batteryConfig/api/v1/profile/123" {
				if r.Method == "GET" {
					w.Header().Set("X-Csrf-Token", "profile_csrf_123")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"data": {}}`))
					return
				}
				if r.Method == "PUT" {
					assert.Equal(t, "profile_csrf_123", r.Header.Get("X-Xsrf-Token"))
					var body struct {
						Profile                 string `json:"profile"`
						BatteryBackupPercentage int    `json:"batteryBackupPercentage"`
					}
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)
					lastPayload = &enphaseBatterySchedulesPayload{
						Usage:                   body.Profile,
						BatteryBackupPercentage: body.BatteryBackupPercentage,
					}
					postCalled = true
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"message":"success"}`))
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.settings = types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		err := e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 85})
		require.NoError(t, err)
		if assert.True(t, postCalled) {
			assert.Equal(t, 85, lastPayload.BatteryBackupPercentage)
			assert.Equal(t, "self-consumption", lastPayload.Usage)
		}
		assert.True(t, putCalled)
		assert.True(t, lastSettingsPut.ChargeFromGrid)
	})

	t.Run("getToday future intervals truncated and null SOC handled and cached", func(t *testing.T) {
		calls := 0
		now := time.Now()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/pv/systems/123/today" {
				calls++
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(fmt.Sprintf(`{
					"start_date": "2026-07-01",
					"stats": [{
						"production": [1000, 2000, 3000],
						"consumption": [800, 900, 1000],
						"solar_home": [400, 500, 600],
						"solar_battery": [400, 500, 600],
						"solar_grid": [200, 300, 400],
						"battery_home": [100, 200, 300],
						"battery_grid": [0, 0, 0],
						"grid_battery": [0, 0, 0],
						"grid_home": [300, 400, 500],
						"soc": [75, null, 80],
						"start_time": %d,
						"interval_length": 900
					}]
				}`, now.Unix()-1000)))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123

		res, err := e.getToday(context.Background(), now)
		require.NoError(t, err)
		assert.Equal(t, 1, calls)

		if assert.Len(t, res.Stats, 1) {
			stat := res.Stats[0]
			assert.Len(t, stat.Production, 2)
			assert.Len(t, stat.Consumption, 2)
			if assert.Len(t, stat.SOC, 2) {
				assert.NotNil(t, stat.SOC[0])
				assert.Equal(t, 75.0, *stat.SOC[0])
				assert.Nil(t, stat.SOC[1])
			}
		}

		res2, err := e.getToday(context.Background(), now)
		require.NoError(t, err)
		assert.Equal(t, 1, calls)

		assert.Equal(t, res.StartDate, res2.StartDate)

		e.todayCacheExpiry = time.Time{}
		res3, err := e.getToday(context.Background(), now)
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
		_ = res3
	})
}
