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
					"last_report_date": today.Truncate(24*time.Hour).Unix() + 3600,
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
						"production":      []float64{0, 0, 0, 1000, 400},
						"consumption":     []float64{0, 0, 0, 0, 0},
						"solar_home":      []float64{0, 0, 0, 500, 400},
						"solar_grid":      []float64{0, 0, 0, 500, 0},
						"soc":             []float64{10, 15, 20, 25, 28},
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
				if assert.Len(t, stats[0].Hourly, 2) {
					hourStat0 := stats[0].Hourly[0]
					assert.Equal(t, start, hourStat0.TSHourStart)
					assert.Equal(t, 1.0, hourStat0.SolarKWH)
					assert.Equal(t, 0.5, hourStat0.SolarToHomeKWH)
					assert.Equal(t, 0.5, hourStat0.SolarToGridKWH)
					assert.Equal(t, 0.5, hourStat0.HomeKWH)
					// Includes 01:00:00 boundary SOC (28.0) as the end of Hour 0
					assert.Equal(t, 10.0, hourStat0.MinBatterySOC)
					assert.Equal(t, 28.0, hourStat0.MaxBatterySOC)

					hourStat1 := stats[0].Hourly[1]
					assert.Equal(t, start.Add(time.Hour), hourStat1.TSHourStart)
					assert.Equal(t, 28.0, hourStat1.MinBatterySOC)
					assert.Equal(t, 28.0, hourStat1.MaxBatterySOC)
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

			if r.URL.Path == "/app-api/123/grid_control_check.json" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"gridOutageCheck": false}`))
				return
			}

			if r.URL.Path == "/app-api/123/get_pel_details" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"pel_type": "Not Configured", "export_limit": null}`))
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
		assert.False(t, status.GridUnavailable)
		assert.True(t, status.VPPActive)

		assert.Len(t, status.Storms, 1)
		assert.Equal(t, "Severe weather alert active", status.Storms[0].Description)

		assert.Len(t, status.Alarms, 1)
		assert.Equal(t, "IQ Battery 3", status.Alarms[0].Name)
		assert.Contains(t, status.Alarms[0].Description, "offline or in status")
	})

	t.Run("GetStatus partial interval power calculation", func(t *testing.T) {
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
						},
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: 30,
						},
					},
					LastReportDate: 1776038430, // 30 seconds after start_time
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
						"production": [10],
						"consumption": [8],
						"solar_home": [4],
						"solar_battery": [4],
						"solar_grid": [2],
						"battery_home": [1],
						"battery_grid": [0],
						"grid_battery": [0],
						"grid_home": [3],
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

			if r.URL.Path == "/app-api/123/grid_control_check.json" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"gridOutageCheck": false}`))
				return
			}

			if r.URL.Path == "/app-api/123/get_pel_details" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"pel_type": "Not Configured", "export_limit": null}`))
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

		// Calculations:
		// latestIntervalDuration = 1776038430 - 1776038400 = 30 seconds.
		// whToKW(val) = (val / 1000.0) / (30 / 3600.0) = (val / 1000.0) * 120 = val * 0.12.
		// prod = 10 -> SolarKW = prod * 0.12 = 1.2 kW.
		// batteryKW = (1 + 0 - 0 - 4) * 0.12 = -0.36 kW.
		// gridKW = (3 + 0 - 2 - 0) * 0.12 = 0.12 kW.
		// homeKW = (4 + 1 + 3) * 0.12 = 0.96 kW.
		assert.InDelta(t, 1.2, status.SolarKW, 0.0001)
		assert.InDelta(t, -0.36, status.BatteryKW, 0.0001)
		assert.InDelta(t, 0.12, status.GridKW, 0.0001)
		assert.InDelta(t, 0.96, status.HomeKW, 0.0001)
	})

	t.Run("Skips corrupt negative counter rollover intervals in parseDailyStats and GetStatus", func(t *testing.T) {
		soc20 := 20.0
		soc21 := 21.0
		dayStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		hour8Start := dayStart.Add(8 * time.Hour)

		// 4 intervals in 08:00 hour: first 3 are valid, 4th has a counter rollover (-15,160 Wh battery_home, +74,900 Wh grid_battery, +73,860 Wh battery_grid)
		rawStat := enphaseTodayStats{
			StartTime:      hour8Start.Unix(),
			IntervalLength: 900,
			Production:     []float64{500, 800, 940, 0},
			Consumption:    []float64{500, 500, 590, -15160},
			SolarHome:      []float64{300, 400, 420, 0},
			SolarBattery:   []float64{200, 400, 520, 0},
			SolarGrid:      []float64{0, 0, 0, 0},
			GridHome:       []float64{200, 100, 170, 0},
			GridBattery:    []float64{0, 0, 0, 74900},
			BatteryHome:    []float64{0, 0, 0, -15160},
			BatteryGrid:    []float64{0, 0, 0, 73860},
			SOC:            []*float64{&soc20, &soc20, &soc21, &soc21},
		}

		parsed := parseDailyStats(context.Background(), rawStat, dayStart, time.UTC)
		if assert.Len(t, parsed.Hourly, 1) {
			h := parsed.Hourly[0]
			assert.InDelta(t, 2.24, h.SolarKWH, 0.0001)
			assert.InDelta(t, 1.12, h.SolarToHomeKWH, 0.0001)
			assert.InDelta(t, 1.12, h.SolarToBatteryKWH, 0.0001)
			assert.InDelta(t, 1.59, h.HomeKWH, 0.0001)
			assert.InDelta(t, 0.47, h.GridImportKWH, 0.0001)
			assert.InDelta(t, 0.0, h.GridExportKWH, 0.0001)
			assert.InDelta(t, 1.12, h.BatteryChargedKWH, 0.0001)
			assert.InDelta(t, 0.0, h.BatteryUsedKWH, 0.0001)
			assert.InDelta(t, 0.0, h.BatteryToHomeKWH, 0.0001)
			assert.Equal(t, 20.0, h.MinBatterySOC)
			assert.Equal(t, 21.0, h.MaxBatterySOC)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app-api/123/data.json":
				res := enphaseDataResult{
					App:            enphaseApp{Timezone: "UTC"},
					LastReportDate: hour8Start.Add(time.Hour).Unix(),
				}
				json.NewEncoder(w).Encode(res)
			case "/pv/systems/123/today":
				res := enphaseTodayResponse{
					StartDate: "2026-10-01",
					Stats:     []enphaseTodayStats{rawStat},
					BatteryDetails: &struct {
						AggregateSOC float64 `json:"aggregate_soc"`
					}{AggregateSOC: 21},
				}
				json.NewEncoder(w).Encode(res)
			case "/pv/settings/123/battery_status.json":
				w.Write([]byte(`{"max_capacity": 10.0, "available_power": 3.84}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		status, err := e.GetStatus(context.Background())
		require.NoError(t, err)
		// Should step back from index 3 (corrupt) to index 2 (valid: prod=940Wh -> 3.76kW, solarHome=420Wh+gridHome=170Wh=590Wh -> 2.36kW)
		assert.InDelta(t, 3.76, status.SolarKW, 0.0001)
		assert.InDelta(t, 2.36, status.HomeKW, 0.0001)
		assert.InDelta(t, -2.08, status.BatteryKW, 0.0001)
		assert.InDelta(t, 0.68, status.GridKW, 0.0001)

		// Positive spike with flat SOC (e.g. Oct 6 13:45 reboot glitch: 5,271 Wh battery_home while SOC changes 99% -> 98%)
		soc97 := 97.0
		soc98 := 98.0
		soc99 := 99.0
		soc100 := 100.0
		positiveSpikeStat := enphaseTodayStats{
			StartTime:      dayStart.Add(13 * time.Hour).Unix(),
			IntervalLength: 900,
			Production:     []float64{2088, 2001, 1899, 1831},
			Consumption:    []float64{503, 456, 792, 5873},
			SolarHome:      []float64{502, 453, 208, 602},
			SolarBattery:   []float64{29, 368, 291, 29},
			SolarGrid:      []float64{1558, 1180, 1200, 1201},
			GridHome:       []float64{0, 0, 0, 0},
			GridBattery:    []float64{0, 0, 0, 0},
			BatteryHome:    []float64{1, 3, 584, 5271},
			BatteryGrid:    []float64{0, 0, 0, 0},
			SOC:            []*float64{&soc98, &soc97, &soc99, &soc98, &soc100},
		}
		assert.False(t, hasCorruptEnphaseInterval(context.Background(), positiveSpikeStat, 0, time.UTC))
		assert.False(t, hasCorruptEnphaseInterval(context.Background(), positiveSpikeStat, 1, time.UTC))
		// Index 2 has production=1899 vs solarSum=1699 (diff=200 > 50)
		assert.True(t, hasCorruptEnphaseInterval(context.Background(), positiveSpikeStat, 2, time.UTC))
		// Index 3 has netBatWH=-5242 with socDelta=-1 (99 -> 98)
		assert.True(t, hasCorruptEnphaseInterval(context.Background(), positiveSpikeStat, 3, time.UTC))
	})

	t.Run("SetModes", func(t *testing.T) {
		var lastPayload *enphaseBatteryProfilePayload
		var postCalled bool
		var settingsChargeFromGrid bool
		var settingsScheduleEnabled bool
		var useRequestedConfig bool
		var settingsProfile string = "self-consumption"
		var requestedProfile string = "self-consumption"
		var settingsMessage string = "success"
		var profileMessage string = "success"
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

			if r.URL.Path == "/pv/systems/123/today" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"start_date": "2026-06-06",
					"battery_details": {
						"aggregate_soc": 80
					},
					"stats": []
				}`))
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
					json.NewEncoder(w).Encode(map[string]string{"message": settingsMessage})
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
					assert.Equal(t, "123456", r.URL.Query().Get("userId"))
					assert.Equal(t, "profile_csrf_123", r.Header.Get("X-Xsrf-Token"))
					var body struct {
						Profile                 string `json:"profile"`
						BatteryBackupPercentage int    `json:"batteryBackupPercentage"`
					}
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)
					lastPayload = &enphaseBatteryProfilePayload{
						Profile:                 body.Profile,
						BatteryBackupPercentage: body.BatteryBackupPercentage,
					}
					postCalled = true
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]string{"message": profileMessage})
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

		changed, err := e.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		if assert.True(t, postCalled) {
			assert.Equal(t, 80, lastPayload.BatteryBackupPercentage)
			assert.Equal(t, "self-consumption", lastPayload.Profile)
		}
		if assert.True(t, putCalled) {
			assert.True(t, lastSettingsPut.ChargeFromGrid)
		}

		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil

		changed, err = e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		if assert.True(t, postCalled) {
			assert.Equal(t, 100, lastPayload.BatteryBackupPercentage)
		}

		// Test disabling schedules
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = false
		settingsScheduleEnabled = true
		e.settings.GridChargeBatteries = false

		changed, err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
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

		changed, err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, postCalled)
		assert.False(t, putCalled)

		// Test backup mode grid charge update (current profile is backup_only)
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = false
		settingsScheduleEnabled = false
		useRequestedConfig = false
		settingsProfile = "backup_only"
		e.settings.GridChargeBatteries = true

		changed, err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.False(t, postCalled)
		if assert.True(t, putCalled) {
			assert.True(t, lastSettingsPut.ChargeFromGrid)
		}

		// Test backup mode grid charge update (pending profile is backup_only)
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		settingsChargeFromGrid = true
		settingsScheduleEnabled = false
		settingsProfile = "self-consumption"
		requestedProfile = "backup_only"
		useRequestedConfig = true
		e.settings.GridChargeBatteries = false

		changed, err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.False(t, postCalled)
		if assert.True(t, putCalled) {
			assert.False(t, lastSettingsPut.ChargeFromGrid)
		}

		// Test Changes Already Activated! response
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		profileMessage = "Changes Already Activated!"
		settingsMessage = "Changes Already Activated!"
		settingsChargeFromGrid = false
		settingsScheduleEnabled = false
		settingsProfile = "self-consumption"
		useRequestedConfig = false
		e.settings.GridChargeBatteries = true

		changed, err = e.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)

		// Test profile update when currentProfile is cost_savings even if reserve SOC already matches
		postCalled = false
		lastPayload = nil
		putCalled = false
		lastSettingsPut = nil
		profileMessage = "success"
		settingsMessage = "success"
		settingsChargeFromGrid = true
		settingsScheduleEnabled = false
		settingsProfile = "cost_savings"
		useRequestedConfig = false
		e.settings.MinBatterySOC = 30
		e.settings.GridChargeBatteries = true

		changed, err = e.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.False(t, putCalled)
		if assert.True(t, postCalled) {
			assert.Equal(t, 30, lastPayload.BatteryBackupPercentage)
			assert.Equal(t, "self-consumption", lastPayload.Profile)
		}
	})

	t.Run("SetModes storm mode grid charge", func(t *testing.T) {
		putCalled := false
		var lastPayload *struct {
			ChargeFromGrid                bool `json:"chargeFromGrid"`
			ChargeFromGridScheduleEnabled bool `json:"chargeFromGridScheduleEnabled"`
		}

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
			if r.URL.Path == "/pv/systems/123/today" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"start_date": "2026-06-06",
					"battery_details": {
						"aggregate_soc": 80
					},
					"stats": []
				}`))
				return
			}
			if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "GET" {
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
			if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "PUT" {
				assert.Equal(t, "settings_csrf_123", r.Header.Get("X-Xsrf-Token"))
				var body struct {
					ChargeFromGrid                bool `json:"chargeFromGrid"`
					ChargeFromGridScheduleEnabled bool `json:"chargeFromGridScheduleEnabled"`
				}
				err := json.NewDecoder(r.Body).Decode(&body)
				assert.NoError(t, err)
				lastPayload = &body
				putCalled = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"message":"success"}`))
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
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		changed, err := e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		if assert.True(t, putCalled) {
			assert.True(t, lastPayload.ChargeFromGrid)
		}
	})

	t.Run("SetModes respects ChargeToSOC", func(t *testing.T) {
		var postCalled bool
		var lastPayload *enphaseBatteryProfilePayload
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

			if r.URL.Path == "/pv/systems/123/today" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"start_date": "2026-06-06",
					"battery_details": {
						"aggregate_soc": 80
					},
					"stats": []
				}`))
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
					lastPayload = &enphaseBatteryProfilePayload{
						Profile:                 body.Profile,
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

		changed, err := e.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 85})
		require.NoError(t, err)
		assert.True(t, changed)
		if assert.True(t, postCalled) {
			assert.Equal(t, 85, lastPayload.BatteryBackupPercentage)
			assert.Equal(t, "self-consumption", lastPayload.Profile)
		}
		assert.True(t, putCalled)
		if assert.NotNil(t, lastSettingsPut.ChargeFromGrid) {
			assert.True(t, *lastSettingsPut.ChargeFromGrid)
		}
	})

	t.Run("getToday future intervals truncated and null SOC handled and cached", func(t *testing.T) {
		calls := 0
		now := time.Now()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/pv/systems/123/today" {
				calls++
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{
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
				}`, now.Unix()-1000)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123

		res, err := e.getToday(context.Background(), now, true)
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

		res2, err := e.getToday(context.Background(), now, true)
		require.NoError(t, err)
		assert.Equal(t, 1, calls)

		assert.Equal(t, res.StartDate, res2.StartDate)

		e.todayCacheExpiry = time.Time{}
		res3, err := e.getToday(context.Background(), now, true)
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
		_ = res3
	})

	t.Run("GridSettings", func(t *testing.T) {
		t.Run("ImportAndExport", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app-api/123/data.json" {
					json.NewEncoder(w).Encode(map[string]any{
						"state": map[string]any{
							"batteryGridMode": "ImportAndExport",
							"batteryConfig": map[string]any{
								"charge_from_grid": true,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			e := newEnphase()
			e.baseURL, _ = url.Parse(server.URL)
			e.systemID = 123

			gs, err := e.GridSettings(context.Background())
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.True(t, gs.GridExportBatteries)
		})

		t.Run("ImportOnly", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app-api/123/data.json" {
					json.NewEncoder(w).Encode(map[string]any{
						"state": map[string]any{
							"batteryGridMode": "ImportOnly",
							"batteryConfig": map[string]any{
								"charge_from_grid": true,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			e := newEnphase()
			e.baseURL, _ = url.Parse(server.URL)
			e.systemID = 123

			gs, err := e.GridSettings(context.Background())
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.False(t, gs.GridExportBatteries)
		})

		t.Run("NoImportOrExport", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app-api/123/data.json" {
					json.NewEncoder(w).Encode(map[string]any{
						"state": map[string]any{
							"batteryGridMode": "NoImportOrExport",
							"batteryConfig": map[string]any{
								"charge_from_grid": false,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			e := newEnphase()
			e.baseURL, _ = url.Parse(server.URL)
			e.systemID = 123

			gs, err := e.GridSettings(context.Background())
			require.NoError(t, err)
			assert.False(t, gs.GridChargeBatteries)
			assert.False(t, gs.GridExportSolar)
			assert.False(t, gs.GridExportBatteries)
		})

		t.Run("NullGridModeWithDTGControl", func(t *testing.T) {
			dtgLocked := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app-api/123/data.json" {
					json.NewEncoder(w).Encode(map[string]any{
						"state": map[string]any{
							"batteryGridMode": nil,
							"batteryConfig": map[string]any{
								"charge_from_grid": false,
							},
						},
					})
					return
				}
				if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" {
					json.NewEncoder(w).Encode(map[string]any{
						"data": map[string]any{
							"chargeFromGrid": true,
							"dtgControl": map[string]any{
								"show":    true,
								"enabled": false,
								"locked":  dtgLocked,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			e := newEnphase()
			e.baseURL, _ = url.Parse(server.URL)
			e.systemID = 123

			gs, err := e.GridSettings(context.Background())
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.True(t, gs.GridExportBatteries)

			e.dataExpiry = time.Time{}
			dtgLocked = true
			gsLocked, err := e.GridSettings(context.Background())
			require.NoError(t, err)
			assert.False(t, gsLocked.GridExportBatteries)
		})
	})

	t.Run("GetStatus ManagedTOUMode", func(t *testing.T) {
		usage := "cost_savings"
		dtgEnabled := false
		gridOutage := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				json.NewEncoder(w).Encode(enphaseDataResult{
					App: enphaseApp{Timezone: "UTC"},
					State: enphaseState{
						SiteID: 123,
						BatteryConfig: enphaseBatteryConfig{
							Usage:                   usage,
							BatteryBackupPercentage: 20,
						},
					},
				})
				return
			}
			if r.URL.Path == "/pv/systems/123/today" {
				w.Write([]byte(`{"start_date":"2026-06-06","battery_details":{"aggregate_soc":50},"stats":[]}`))
				return
			}
			if r.URL.Path == "/pv/settings/123/battery_status.json" {
				w.Write([]byte(`{"max_capacity":10,"available_power":5}`))
				return
			}
			if r.URL.Path == "/app-api/123/grid_control_check.json" {
				json.NewEncoder(w).Encode(map[string]any{
					"gridOutageCheck":           gridOutage,
					"userInitiatedGridToggle":   false,
					"disableGridControl":        false,
					"sunlightBackupSystemCheck": false,
				})
				return
			}
			if r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" {
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"profile": usage,
						"dtgControl": map[string]any{
							"show":    true,
							"enabled": dtgEnabled,
						},
					},
				})
				return
			}
			if r.URL.Path == "/app-api/123/get_pel_details" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"pel_type": "Not Configured", "export_limit": null}`))
				return
			}
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.settings = types.Settings{MinBatterySOC: 20}
		ctx := ffEnphaseExportSchedules.With(context.Background(), true)

		t.Run("cost_savings marks ManagedTOUMode true", func(t *testing.T) {
			usage = "cost_savings"
			dtgEnabled = false
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.True(t, st.ManagedTOUMode)
		})

		t.Run("self-consumption with dtgControl enabled marks ManagedTOUMode true", func(t *testing.T) {
			usage = "self-consumption"
			dtgEnabled = true
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.True(t, st.ManagedTOUMode)
		})

		t.Run("self-consumption without dtgControl marks ManagedTOUMode false", func(t *testing.T) {
			usage = "self-consumption"
			dtgEnabled = false
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.False(t, st.ManagedTOUMode)
		})

		t.Run("ai_optimisation never marks ManagedTOUMode true", func(t *testing.T) {
			usage = "ai_optimisation"
			dtgEnabled = false
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.False(t, st.ManagedTOUMode)
		})

		t.Run("backup_only marks EmergencyMode true and ManagedTOUMode false", func(t *testing.T) {
			usage = "backup_only"
			dtgEnabled = true
			gridOutage = false
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.True(t, st.EmergencyMode)
			assert.False(t, st.ManagedTOUMode)
			assert.False(t, st.GridUnavailable)
		})

		t.Run("grid_control_check gridOutageCheck marks GridUnavailable true", func(t *testing.T) {
			usage = "self-consumption"
			dtgEnabled = false
			gridOutage = true
			e.dataExpiry = time.Time{}
			e.todayCacheExpiry = time.Time{}
			st, err := e.GetStatus(ctx)
			require.NoError(t, err)
			assert.True(t, st.GridUnavailable)
		})
	})

	t.Run("GetStatus PowerExportLimit", func(t *testing.T) {
		var pelStatus int = http.StatusOK
		var pelBody string = `{"pel_type": "Not Configured", "export_limit": null}`
		var pelCalls int
		var lastAuthHeader string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app-api/123/data.json":
				json.NewEncoder(w).Encode(enphaseDataResult{
					App: enphaseApp{Timezone: "UTC"},
					State: enphaseState{
						SiteID: 123,
						BatteryConfig: enphaseBatteryConfig{
							Usage:                   "self-consumption",
							BatteryBackupPercentage: 20,
						},
					},
				})
			case "/pv/systems/123/today":
				w.Write([]byte(`{"start_date":"2026-06-06","battery_details":{"aggregate_soc":50},"stats":[]}`))
			case "/pv/settings/123/battery_status.json":
				w.Write([]byte(`{"max_capacity":10,"available_power":7.68}`))
			case "/app-api/123/grid_control_check.json":
				w.Write([]byte(`{"gridOutageCheck":false}`))
			case "/app-api/123/get_pel_details":
				pelCalls++
				lastAuthHeader = r.Header.Get("Authorization")
				w.WriteHeader(pelStatus)
				w.Write([]byte(pelBody))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.sessionID = "sess_abc"
		e.managerToken = "mgr_jwt_xyz"
		e.syncCookies()

		t.Run("Not configured returns 0 and caches for 24h", func(t *testing.T) {
			pelCalls = 0
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "Not Configured", "export_limit": null}`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 0.0, st.MaxGridExportKW)
			assert.Equal(t, 0.0, st.MaxGridImportKW)
			assert.Equal(t, "mgr_jwt_xyz", lastAuthHeader)
			assert.Equal(t, 1, pelCalls)

			// Second GetStatus within 24h must reuse cached PEL response without hitting endpoint
			st2, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 0.0, st2.MaxGridExportKW)
			assert.Equal(t, 1, pelCalls)
		})

		t.Run("Single gateway static export_value in kW", func(t *testing.T) {
			pelCalls = 0
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "Static", "export_limit": {"serial_number": "GW1234", "export_value": "11.0 kW"}}`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, 11.0, st.MaxGridExportKW, 1e-6)
			assert.Equal(t, 1, pelCalls)
		})

		t.Run("Scalar export_limit in Watts", func(t *testing.T) {
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "Static", "export_limit": "5,000 W"}`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, 5.0, st.MaxGridExportKW, 1e-6)
		})

		t.Run("Multi-gateway static array sums gateway export limits", func(t *testing.T) {
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "Static", "export_limit": [{"serial_number": "GW1", "export_value": "3.84 kW"}, {"serial_number": "GW2", "export_value": "3.84 kW"}]}`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, 7.68, st.MaxGridExportKW, 1e-6)
		})

		t.Run("24-Hour Schedule is ignored and returns 0", func(t *testing.T) {
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "24-Hour Schedule", "export_limit": {"serial_number": "GW1", "schedule": [{"time_range": "08:00 - 16:00", "pel": "4.5 kW"}]}}`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 0.0, st.MaxGridExportKW)
		})

		t.Run("Unitless numbers and unsupported units return 0", func(t *testing.T) {
			ctx := context.Background()
			assert.Equal(t, 0.0, resolveEnphasePELExportKW(ctx, enphasePELDetailsResponse{
				PELType:     "Static",
				ExportLimit: json.RawMessage(`5000`),
			}))
			assert.Equal(t, 0.0, resolveEnphasePELExportKW(ctx, enphasePELDetailsResponse{
				PELType:     "Static",
				ExportLimit: json.RawMessage(`{"serial_number": "GW1", "export_value": "5000"}`),
			}))
			assert.Equal(t, 0.0, resolveEnphasePELExportKW(ctx, enphasePELDetailsResponse{
				PELType:     "Static",
				ExportLimit: json.RawMessage(`{"serial_number": "GW1", "export_value": "20 A"}`),
			}))
		})

		t.Run("HTTP failure gracefully falls back to 0 without caching error", func(t *testing.T) {
			pelCalls = 0
			pelStatus = http.StatusInternalServerError
			pelBody = `internal error`
			e.pelExpiry = time.Time{}

			st, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 0.0, st.MaxGridExportKW)
			assert.Equal(t, 1, pelCalls)

			// Next poll retries since failure was not cached
			pelStatus = http.StatusOK
			pelBody = `{"pel_type": "Static", "export_limit": "7.6 kW"}`
			st2, err := e.GetStatus(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, 7.6, st2.MaxGridExportKW, 1e-6)
			assert.Equal(t, 2, pelCalls)
		})
	})

	t.Run("SetModes BatteryModeExport DTG Schedule", func(t *testing.T) {
		now := time.Now().UTC()
		startH := now.Hour()
		endH := startH + 2
		if endH > 24 {
			endH = 24
		}
		expectedStart := fmt.Sprintf("%02d:00", startH)
		expectedEnd := fmt.Sprintf("%02d:00", endH)
		if endH == 24 {
			expectedEnd = "23:59"
		}

		var createdSchedule enphaseBatteryScheduleDetail
		var activeSchedule *enphaseBatteryScheduleDetail
		var isValidCalls int
		var schedGetCalls int
		var expectedIsValidCSRF = "csrf_sched"
		var settingsPuts []enphaseBatterySettingsPayload
		currentDTGEnabled := false
		currentProfile := "self-consumption"
		currentReserve := 20
		currentCFG := true

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/app-api/123/data.json":
				json.NewEncoder(w).Encode(enphaseDataResult{
					App: enphaseApp{Timezone: "UTC", UserID: 555},
					State: enphaseState{
						SiteID:          123,
						BatteryGridMode: "ImportAndExport",
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: currentReserve,
							Usage:                   currentProfile,
							ChargeFromGrid:          currentCFG,
						},
					},
				})
			case r.URL.Path == "/pv/systems/123/today":
				w.Write([]byte(`{"start_date":"2026-06-06","battery_details":{"aggregate_soc":80},"stats":[]}`))
			case r.URL.Path == "/app-api/123/tariff.json":
				json.NewEncoder(w).Encode(enphaseTariffJSONResponse{
					SiteID: 123,
					Purchase: &enphaseTariffSection{
						TypeID: "tou",
						Seasons: []enphaseTariffSeason{{
							Days: []enphaseTariffDaySchedule{{
								Days: []int{1, 2, 3, 4, 5, 6, 7},
								Periods: []enphaseTariffPeriod{
									{StartTime: startH * 60, EndTime: endH * 60, Rate: "0.35", Type: "peak"},
									{StartTime: "", EndTime: "", Rate: "0.10", Type: "off-peak"},
								},
							}},
						}},
					},
					Buyback: &enphaseTariffSection{
						TypeID: "tou",
						Seasons: []enphaseTariffSeason{{
							Days: []enphaseTariffDaySchedule{{
								Days: []int{1, 2, 3, 4, 5, 6, 7},
								Periods: []enphaseTariffPeriod{
									{StartTime: startH * 60, EndTime: endH * 60, Rate: "0.35", Type: "peak"},
									{StartTime: "", EndTime: "", Rate: "0.10", Type: "off-peak"},
								},
							}},
						}},
					},
				})
			case r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_bs")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"chargeFromGrid":             currentCFG,
						"profile":                    currentProfile,
						"batteryBackupPercentage":    currentReserve,
						"batteryBackupPercentageMin": 5,
						"veryLowSoc":                 5,
						"dtgControl": map[string]any{
							"show":              true,
							"enabled":           currentDTGEnabled,
							"locked":            false,
							"scheduleSupported": true,
						},
					},
				})
			case r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "PUT":
				assert.Equal(t, "csrf_bs", r.Header.Get("X-Xsrf-Token"))
				assert.Equal(t, "555", r.Header.Get("Username"))
				var p enphaseBatterySettingsPayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				settingsPuts = append(settingsPuts, p)
				if p.ChargeFromGrid != nil {
					currentCFG = *p.ChargeFromGrid
				}
				if p.DTGControl != nil {
					currentDTGEnabled = p.DTGControl.Enabled
				}
				w.Write([]byte(`{"message":"success"}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/battery/sites/123/schedules" && r.Method == "GET":
				schedGetCalls++
				w.Header().Set("X-Csrf-Token", "csrf_sched")
				var details []enphaseBatteryScheduleDetail
				if activeSchedule != nil {
					details = append(details, *activeSchedule)
				}
				json.NewEncoder(w).Encode(enphaseBatterySchedulesResponse{
					Type: "BATTERY_SCHEDULES_CONFIG",
					DTG: enphaseBatteryScheduleGroup{
						ScheduleStatus: "active",
						Count:          len(details),
						Details:        details,
					},
				})
			case r.URL.Path == "/service/batteryConfig/api/v1/battery/sites/123/schedules" && r.Method == "POST":
				assert.Equal(t, "csrf_sched", r.Header.Get("X-Xsrf-Token"))
				assert.Equal(t, "555", r.Header.Get("Username"))
				require.NoError(t, json.NewDecoder(r.Body).Decode(&createdSchedule))
				createdSchedule.ScheduleID = "dtg-uuid-1"
				createdSchedule.IsEnabled = true
				activeSchedule = &createdSchedule
				w.Header().Set("X-Csrf-Token", "csrf_sched_post")
				expectedIsValidCSRF = "csrf_sched_post"
				w.Write([]byte(`{"scheduleId":"dtg-uuid-1"}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/battery/sites/123/schedules/dtg-uuid-1" && r.Method == "PUT":
				assert.NotEmpty(t, r.Header.Get("X-Xsrf-Token"))
				require.NoError(t, json.NewDecoder(r.Body).Decode(&createdSchedule))
				createdSchedule.ScheduleID = "dtg-uuid-1"
				createdSchedule.IsEnabled = true
				activeSchedule = &createdSchedule
				w.Header().Set("X-Csrf-Token", "csrf_sched_put")
				expectedIsValidCSRF = "csrf_sched_put"
				json.NewEncoder(w).Encode(createdSchedule)
			case r.URL.Path == "/service/batteryConfig/api/v1/battery/sites/123/schedules/isValid" && r.Method == "POST":
				assert.Equal(t, expectedIsValidCSRF, r.Header.Get("X-Xsrf-Token"))
				isValidCalls++
				w.Header().Set("X-Csrf-Token", expectedIsValidCSRF)
				w.Write([]byte(`{"isValid":true}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_prof")
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"profile": currentProfile}})
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "PUT":
				var p enphaseBatteryProfilePayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				currentProfile = p.Profile
				currentReserve = p.BatteryBackupPercentage
				w.Write([]byte(`{"message":"success"}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.userID = 555
		e.settings = types.Settings{
			MinBatterySOC:       15,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
		}
		ctx := ffEnphaseExportSchedules.With(context.Background(), true)

		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{
					StartHour:     startH,
					StartMinute:   0,
					EndHour:       endH,
					EndMinute:     0,
					ImportDollars: 0.35,
					ExportDollars: 0.35,
					BatteryMode:   types.BatteryModeExport,
					SolarMode:     types.SolarModeAny,
					Peak:          true,
				},
			},
		}

		changed, err := e.SetModes(ctx, types.BatteryModeExport, types.SolarModeAny, types.ModesOptions{
			MinimumSOC: 10,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, expectedStart, createdSchedule.StartTime)
		assert.Equal(t, expectedEnd, createdSchedule.EndTime)
		assert.Equal(t, 10, createdSchedule.Limit)
		assert.Equal(t, "DTG", createdSchedule.ScheduleType)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7}, createdSchedule.Days)
		assert.Equal(t, 1, isValidCalls)
		assert.Equal(t, 1, schedGetCalls)
		if assert.Len(t, settingsPuts, 1) {
			if assert.NotNil(t, settingsPuts[0].ChargeFromGrid) {
				assert.False(t, *settingsPuts[0].ChargeFromGrid)
			}
			if assert.NotNil(t, settingsPuts[0].DTGControl) {
				assert.True(t, settingsPuts[0].DTGControl.Enabled)
			}
		}

		// Idempotent second call should make no schedule or settings mutations
		changed, err = e.SetModes(ctx, types.BatteryModeExport, types.SolarModeAny, types.ModesOptions{
			MinimumSOC: 10,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Len(t, settingsPuts, 1)
		assert.Equal(t, 2, schedGetCalls)

		// Updating existing schedule should reuse PUT response X-Csrf-Token for isValid without extra GET /schedules
		changed, err = e.SetModes(ctx, types.BatteryModeExport, types.SolarModeAny, types.ModesOptions{
			MinimumSOC: 25,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 25, createdSchedule.Limit)
		assert.Equal(t, 2, isValidCalls)
		assert.Equal(t, 3, schedGetCalls)
		assert.Len(t, settingsPuts, 2)

		// Transitioning back to BatteryModeLoad disables DTG control without re-sending CFG fields
		changed, err = e.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{
			MinimumSOC: 15,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		if assert.Len(t, settingsPuts, 3) {
			assert.Nil(t, settingsPuts[2].ChargeFromGrid)
			if assert.NotNil(t, settingsPuts[2].DTGControl) {
				assert.False(t, settingsPuts[2].DTGControl.Enabled)
			}
		}
	})

	t.Run("SetModes SolarModeExport CostSavings and Real-Rate Tariff Sync", func(t *testing.T) {
		now := time.Now().UTC()
		curH := now.Hour()

		// Build a 24h multi-tier schedule covering super-off-peak, off-peak, partial-peak, and peak (including curH)
		var periods []types.TOUPeriod
		for h := 0; h < 24; h++ {
			imp := 0.12
			exp := 0.04
			solMode := types.SolarModeAny
			isPeak := false
			switch {
			case h == curH:
				imp = 0.45
				exp = 0.15
				solMode = types.SolarModeExport
				isPeak = true
			case h >= 0 && h < 6 && h != curH:
				imp = 0.06
				exp = 0.02
			case h >= 17 && h < 21 && h != curH:
				imp = 0.25
				exp = 0.05
			}
			periods = append(periods, types.TOUPeriod{
				StartHour:     h,
				EndHour:       h + 1,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     solMode,
				Peak:          isPeak,
			})
		}
		sched := &types.TOUSchedule{Periods: periods}

		var tariffPosts int
		var postedJourney map[string]any
		var profilePuts []enphaseBatteryProfilePayload
		var callOrder []string
		isBatteryChangePending := false
		tariffSynced := false
		currentProfile := "self-consumption"
		currentReserve := 20
		currentCFG := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/app-api/123/data.json":
				json.NewEncoder(w).Encode(enphaseDataResult{
					App: enphaseApp{Timezone: "UTC", UserID: 777},
					State: enphaseState{
						SiteID:          123,
						BatteryGridMode: "ImportOnly",
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: currentReserve,
							Usage:                   currentProfile,
							ChargeFromGrid:          currentCFG,
						},
					},
				})
			case r.URL.Path == "/pv/systems/123/today":
				w.Write([]byte(`{"start_date":"2026-06-06","battery_details":{"aggregate_soc":64.7},"stats":[]}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_bs")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"chargeFromGrid":             currentCFG,
						"profile":                    currentProfile,
						"batteryBackupPercentage":    currentReserve,
						"batteryBackupPercentageMin": 5,
						"veryLowSoc":                 5,
						"isBatteryChangePending":     isBatteryChangePending,
					},
				})
			case r.URL.Path == "/app-api/123/tariff.json":
				if !tariffSynced {
					json.NewEncoder(w).Encode(enphaseTariffJSONResponse{
						SiteID: 123,
						Purchase: &enphaseTariffSection{
							TypeID: "flat",
							Seasons: []enphaseTariffSeason{{
								Days: []enphaseTariffDaySchedule{{
									Days:    []int{1, 2, 3, 4, 5, 6, 7},
									Periods: []enphaseTariffPeriod{{StartTime: "", EndTime: "", Rate: "0.10", Type: "off-peak"}},
								}},
							}},
						},
					})
					return
				}
				json.NewEncoder(w).Encode(enphaseTariffJSONResponse{
					SiteID: 123,
					Purchase: &enphaseTariffSection{
						TypeID: "tou",
						Source: "manual",
						Seasons: []enphaseTariffSeason{{
							Days: []enphaseTariffDaySchedule{{
								Days: []int{1, 2, 3, 4, 5, 6, 7},
								Periods: []enphaseTariffPeriod{
									{StartTime: curH * 60, EndTime: (curH + 1) * 60, Rate: "0.45", Type: "peak"},
									{StartTime: "", EndTime: "", Rate: "0.12", Type: "off-peak"},
								},
							}},
						}},
					},
					Buyback: &enphaseTariffSection{
						TypeID: "tou",
						Seasons: []enphaseTariffSeason{{
							Days: []enphaseTariffDaySchedule{{
								Days: []int{1, 2, 3, 4, 5, 6, 7},
								Periods: []enphaseTariffPeriod{
									{StartTime: curH * 60, EndTime: (curH + 1) * 60, Rate: "0.15", Type: "peak"},
									{StartTime: "", EndTime: "", Rate: "0.03", Type: "off-peak"},
								},
							}},
						}},
					},
				})
			case r.URL.Path == "/service/tariff/tariff-ms/systems/123/tariff-journey" && r.Method == "GET":
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"journeyId": "journey-standard",
						"defaultValues": map[string]any{
							"tariffMode": "autofill",
							"country":    "US",
						},
						"screens": []any{
							map[string]any{
								"id":         "tariff-type-selection",
								"components": []any{map[string]any{"id": "tariffMode", "defaultValue": "autofill"}},
							},
							map[string]any{
								"id":         "season-rate-entry",
								"components": []any{map[string]any{"id": "energy-rate-entry_default_rate_1"}},
							},
							map[string]any{
								"id":         "export-type-selection",
								"components": []any{map[string]any{"id": "exportType", "defaultValue": "net_metering"}},
							},
							map[string]any{
								"id":         "export-season-entry",
								"components": []any{map[string]any{"id": "export-energy-rate-entry_default_rate_1"}},
							},
						},
					},
				})
			case r.URL.Path == "/service/tariff/tariff-ms/systems/123/tariff-journey" && r.Method == "POST":
				assert.Equal(t, "777", r.Header.Get("Username"))
				callOrder = append(callOrder, "tariff")
				tariffPosts++
				require.NoError(t, json.NewDecoder(r.Body).Decode(&postedJourney))
				tariffSynced = true
				w.Write([]byte(`{"type":"tariff-journey","data":{"message":"Tariff saved successfully"}}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_prof")
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"profile": currentProfile}})
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "PUT":
				callOrder = append(callOrder, "profile")
				var p enphaseBatteryProfilePayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				profilePuts = append(profilePuts, p)
				currentProfile = p.Profile
				currentReserve = p.BatteryBackupPercentage
				w.Write([]byte(`{"message":"success"}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.userID = 777
		e.settings = types.Settings{
			MinBatterySOC:       5,
			GridExportSolar:     true,
			GridChargeBatteries: true,
			ManageTOUSchedules:  true,
		}
		ctx := ffEnphaseExportSchedules.With(context.Background(), true)

		// 1. Defer tariff update when battery change is pending, and do NOT enter cost_savings without peak at curH
		isBatteryChangePending = true
		changed, err := e.SetModes(ctx, types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			MinimumSOC: 5,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed) // reserve SOC updated 20 -> 5 (dynamic min 5%!)
		assert.Equal(t, 0, tariffPosts)
		if assert.Len(t, profilePuts, 1) {
			assert.Equal(t, "self-consumption", profilePuts[0].Profile)
			assert.Equal(t, 5, profilePuts[0].BatteryBackupPercentage)
		}

		// 2. Now allow tariff update -> syncs real multi-tier import & TOU export rates BEFORE switching to cost_savings
		isBatteryChangePending = false
		callOrder = nil
		changed, err = e.SetModes(ctx, types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			MinimumSOC: 5,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 1, tariffPosts)
		assert.Equal(t, []string{"tariff", "profile"}, callOrder)

		if assert.NotNil(t, postedJourney) {
			dv, ok := postedJourney["defaultValues"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "tou", dv["tariffType"])
			assert.Equal(t, "tou", dv["exportType"])
			ratesMap := dv["rates"].(map[string]any)
			rateEntry := ratesMap["default"].([]any)[0].(map[string]any)
			assert.Equal(t, "0.45", rateEntry["peakRate"])
			assert.Equal(t, "0.06", rateEntry["offPeak"])
			assert.NotEmpty(t, rateEntry["midPeakPeriods"])
		}
		if assert.Len(t, profilePuts, 2) {
			assert.Equal(t, "cost_savings", profilePuts[1].Profile)
			assert.Equal(t, 64, profilePuts[1].BatteryBackupPercentage) // floor(64.7) for BatteryModeStandby
		}

		// 3. Subsequent call with matching tariff skips /tariff-journey POST
		changed, err = e.SetModes(ctx, types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			MinimumSOC: 5,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, 1, tariffPosts)

		// 4. ManageTOUSchedules: false skips tariff sync, cost_savings, and DTG schedules
		e.settings.ManageTOUSchedules = false
		tariffSynced = false
		changed, err = e.SetModes(ctx, types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			MinimumSOC: 5,
			Schedule:   sched,
		})
		require.NoError(t, err)
		assert.True(t, changed) // reverts cost_savings -> self-consumption and reserve 64 -> 5
		assert.Equal(t, 1, tariffPosts)
		if assert.Len(t, profilePuts, 3) {
			assert.Equal(t, "self-consumption", profilePuts[2].Profile)
			assert.Equal(t, 5, profilePuts[2].BatteryBackupPercentage)
		}
	})

	t.Run("Partial RequestedConfig and Seasonal Tariff Journey Synthesis", func(t *testing.T) {
		now := time.Now().UTC()
		curH := now.Hour()

		// Verify parseEnphaseTariffTOUSchedule handles differing buy/sell peak boundaries cleanly
		parsed := parseEnphaseTariffTOUSchedule(enphaseTariffJSONResponse{
			Purchase: &enphaseTariffSection{
				TypeID: "tou",
				Seasons: []enphaseTariffSeason{{
					Days: []enphaseTariffDaySchedule{{
						Days:    []int{1, 2, 3, 4, 5, 6, 7},
						Periods: []enphaseTariffPeriod{{StartTime: "", EndTime: "", Rate: "0.10", Type: "off-peak"}},
					}},
				}},
			},
			Buyback: &enphaseTariffSection{
				TypeID: "tou",
				Seasons: []enphaseTariffSeason{{
					Days: []enphaseTariffDaySchedule{{
						Days: []int{1, 2, 3, 4, 5, 6, 7},
						Periods: []enphaseTariffPeriod{
							{StartTime: 840, EndTime: 1020, Rate: "0.25", Type: "peak"},
							{StartTime: "", EndTime: "", Rate: "0.05", Type: "off-peak"},
						},
					}},
				}},
			},
		}, now)
		require.NotNil(t, parsed)
		assert.Len(t, parsed.Periods, 3)
		assert.True(t, parsed.Periods[1].Peak)
		assert.Equal(t, 0.25, parsed.Periods[1].ExportDollars)

		// Verify applyEnphaseTariffPlanToJourney replaces seasonal components cleanly and enforces 3DP + net_metering stripping
		journey := map[string]any{
			"defaultValues": map[string]any{
				"hasSeasonalRates": "yes",
				"installDate":      "2026-01-01",
			},
			"screens": []any{
				map[string]any{
					"id": "season-rate-entry",
					"components": []any{
						map[string]any{"id": "energy-rate-entry_summer_rate_1", "type": "SEASON_RATE_REPEATER"},
						map[string]any{"id": "energy-rate-entry_winter_rate_1", "type": "SEASON_RATE_REPEATER"},
						map[string]any{"id": "demandChargeDetails", "type": "SECTION"},
					},
				},
				map[string]any{
					"id": "export-season-entry",
					"components": []any{
						map[string]any{"id": "export-energy-rate-entry_summer_rate_1", "type": "SEASON_RATE_REPEATER"},
					},
				},
			},
		}
		netMeterSched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: curH, EndHour: curH + 1, ImportDollars: 0.30, ExportDollars: 0.30, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: (curH + 1) % 24, EndHour: (curH+1)%24 + 1, ImportDollars: 0.0, ExportDollars: 0.0, BatteryMode: types.BatteryModeLoad},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), netMeterSched, now)
		require.NotNil(t, plan)
		assert.Equal(t, "0.001", formatEnphaseRate(0))
		assert.Equal(t, "0.125", formatEnphaseRate(0.1254))
		assert.Equal(t, "net_metering", plan.ExportType)

		applyEnphaseTariffPlanToJourney(journey, plan, now)
		screens := journey["screens"].([]any)
		seasonComps := screens[0].(map[string]any)["components"].([]any)
		assert.Len(t, seasonComps, 2)
		assert.Equal(t, "energy-rate-entry_default_rate_1", seasonComps[0].(map[string]any)["id"])
		assert.Equal(t, "demandChargeDetails", seasonComps[1].(map[string]any)["id"])
		exportComps := screens[1].(map[string]any)["components"].([]any)
		assert.Empty(t, exportComps)

		// Verify SetModes with partial requestedConfig (only dtgControl) and ScheduleSupported=false
		var settingsPut bool
		var profPut *enphaseBatteryProfilePayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/app-api/123/data.json":
				json.NewEncoder(w).Encode(enphaseDataResult{
					App: enphaseApp{Timezone: "UTC", UserID: 888},
					State: enphaseState{
						SiteID: 123,
						BatteryConfig: enphaseBatteryConfig{
							BatteryBackupPercentage: 20,
							Usage:                   "cost_savings",
						},
					},
				})
			case r.URL.Path == "/pv/systems/123/today":
				w.Write([]byte(`{"start_date":"2026-06-06","battery_details":{"aggregate_soc":70},"stats":[]}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_bs")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"chargeFromGrid":          false,
						"profile":                 "cost_savings",
						"batteryBackupPercentage": 20,
						"requestedConfig": map[string]any{
							"dtgControl": map[string]any{"enabled": false},
						},
						"dtgControl": map[string]any{
							"show":              true,
							"enabled":           false,
							"locked":            false,
							"scheduleSupported": false,
						},
					},
				})
			case r.URL.Path == "/service/batteryConfig/api/v1/batterySettings/123" && r.Method == "PUT":
				settingsPut = true
				w.Write([]byte(`{"message":"success"}`))
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "GET":
				w.Header().Set("X-Csrf-Token", "csrf_prof")
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"profile": "cost_savings"}})
			case r.URL.Path == "/service/batteryConfig/api/v1/profile/123" && r.Method == "PUT":
				var p enphaseBatteryProfilePayload
				require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				profPut = &p
				w.Write([]byte(`{"message":"success"}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123
		e.settings = types.Settings{
			MinBatterySOC:      20,
			ManageTOUSchedules: true,
		}
		ctx := ffEnphaseExportSchedules.With(context.Background(), true)
		changed, err := e.SetModes(ctx, types.BatteryModeExport, types.SolarModeAny, types.ModesOptions{MinimumSOC: 20})
		require.NoError(t, err)
		assert.True(t, changed)
		// Partial requestedConfig must still detect profile transition cost_savings -> self-consumption!
		if assert.NotNil(t, profPut) {
			assert.Equal(t, "self-consumption", profPut.Profile)
			assert.Equal(t, 20, profPut.BatteryBackupPercentage)
		}
		// ScheduleSupported=false warns and skips DTG schedule configuration instead of guessing settings fields
		assert.False(t, settingsPut)
	})

	t.Run("Authenticate OTP Redemption Returns Updated Credentials When Validation Fails", func(t *testing.T) {
		failData := true
		var otpCalls int

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app-api/validate_login_otp.json":
				otpCalls++
				json.NewEncoder(w).Encode(enphaseLoginResponse{
					Message:      "success",
					SessionID:    "otp_session_saved",
					ManagerToken: "otp_manager_saved",
					SystemID:     444555,
				})
			case "/app-api/444555/data.json":
				if failData {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				json.NewEncoder(w).Encode(enphaseDataResult{
					App:   enphaseApp{UserID: 777},
					State: enphaseState{SiteID: 444555},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		origCreds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username: "user@example.com",
				Code:     "999888",
			},
		}

		savedCreds, changed, err := e.Authenticate(context.Background(), origCreds)
		require.ErrorContains(t, err, "credential validation failed")
		assert.True(t, changed, "redeemed single-use OTP session must report changed=true on validation failure")
		assert.Empty(t, savedCreds.Enphase.Code)
		assert.Equal(t, "otp_session_saved", savedCreds.Enphase.SessionID)
		assert.Equal(t, "otp_manager_saved", savedCreds.Enphase.ManagerToken)
		assert.Equal(t, 444555, savedCreds.Enphase.SystemID)
		assert.Equal(t, "999888", origCreds.Enphase.Code, "caller input struct must not be mutated")

		// Retry after data.json recovers succeeds without needing another single-use OTP code
		failData = false
		finalCreds, changed2, err := e.Authenticate(context.Background(), savedCreds)
		require.NoError(t, err)
		assert.True(t, changed2)
		assert.Equal(t, 1, otpCalls)
		assert.Equal(t, 777, finalCreds.Enphase.UserID)
	})

	t.Run("Authenticate 401 Re-Login Returns Updated Session When Post-Retry Validation Fails", func(t *testing.T) {
		dataCalls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app-api/444555/data.json":
				dataCalls++
				if dataCalls == 1 {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			case "/login/login.json":
				json.NewEncoder(w).Encode(enphaseLoginResponse{
					Message:      "success",
					SessionID:    "relogin_session",
					ManagerToken: "relogin_token",
					SystemID:     444555,
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)

		creds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "user@example.com",
				Password:     "secret",
				SessionID:    "expired_session",
				ManagerToken: "expired_token",
				SystemID:     444555,
			},
		}

		updatedCreds, changed, err := e.Authenticate(context.Background(), creds)
		require.ErrorContains(t, err, "credential validation failed after retry")
		assert.True(t, changed)
		assert.Equal(t, "relogin_session", updatedCreds.Enphase.SessionID)
		assert.Equal(t, "relogin_token", updatedCreds.Enphase.ManagerToken)
	})

	t.Run("getDataWithCache And getCSRFToken Caching", func(t *testing.T) {
		var dataHits int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app-api/123/data.json" {
				dataHits++
				json.NewEncoder(w).Encode(enphaseDataResult{
					State: enphaseState{SiteID: dataHits},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		e := newEnphase()
		e.baseURL, _ = url.Parse(server.URL)
		e.systemID = 123

		d1, err := e.getDataWithCache(context.Background(), false)
		require.NoError(t, err)
		assert.Equal(t, 1, d1.State.SiteID)

		d2, err := e.getDataWithCache(context.Background(), false)
		require.NoError(t, err)
		assert.Equal(t, 1, d2.State.SiteID)
		assert.Equal(t, 1, dataHits)

		d3, err := e.getDataWithCache(context.Background(), true)
		require.NoError(t, err)
		assert.Equal(t, 2, d3.State.SiteID)
		assert.Equal(t, 2, dataHits)

		// CSRF token cached within 60s TTL and re-fetched once expired
		fetchCalls := 0
		path := "/service/batteryConfig/api/v1/schedules/123"
		key := enphaseCSRFKey(path)
		tok, err := e.getCSRFToken(context.Background(), path, func() error {
			fetchCalls++
			e.lastCSRFToken[key] = enphaseCSRFToken{Token: "csrf-1", CreatedAt: time.Now()}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "csrf-1", tok)
		assert.Equal(t, 1, fetchCalls)

		tok2, err := e.getCSRFToken(context.Background(), path+"/dtg", func() error {
			fetchCalls++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "csrf-1", tok2)
		assert.Equal(t, 1, fetchCalls, "subpath under /schedules must share cached CSRF key")

		e.lastCSRFToken[key] = enphaseCSRFToken{Token: "csrf-1", CreatedAt: time.Now().Add(-2 * time.Minute)}
		tok3, err := e.getCSRFToken(context.Background(), path, func() error {
			fetchCalls++
			e.lastCSRFToken[key] = enphaseCSRFToken{Token: "csrf-2", CreatedAt: time.Now()}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "csrf-2", tok3)
		assert.Equal(t, 2, fetchCalls)
	})
}

func TestBuildEnphaseTariffPlan(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 15, 0, 0, time.UTC)

	t.Run("Nil empty and non-peak schedules return nil", func(t *testing.T) {
		assert.Nil(t, buildEnphaseTariffPlan(context.Background(), nil, now))
		assert.Nil(t, buildEnphaseTariffPlan(context.Background(), &types.TOUSchedule{}, now))
		assert.Nil(t, buildEnphaseTariffPlan(context.Background(), &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 24, ImportDollars: 0.12, ExportDollars: 0.12, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny},
			},
		}, now))
	})

	t.Run("Two-tier net metering TOU plan", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 16, ImportDollars: 0.12, ExportDollars: 0.12, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny},
				{StartHour: 16, EndHour: 21, ImportDollars: 0.45, ExportDollars: 0.45, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, EndHour: 24, ImportDollars: 0.12, ExportDollars: 0.12, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.InDelta(t, 0.45, plan.ImportPeakDollars, 1e-9)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 960, EndMin: 1260}}, plan.ImportPeakRanges)
		assert.Empty(t, plan.ImportMidPeakGroups)
		assert.InDelta(t, 0.12, plan.ImportOffPeakDollars, 1e-9)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 0, EndMin: 960}, {StartMin: 1260, EndMin: 1440}}, plan.ImportOffPeakRanges)
		assert.Equal(t, "net_metering", plan.ExportType)
	})

	t.Run("Three-tier asymmetric TOU plan", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 6, ImportDollars: 0.08, ExportDollars: 0.02, BatteryMode: types.BatteryModeChargeAny},
				{StartHour: 6, EndHour: 16, ImportDollars: 0.22, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},
				{StartHour: 16, EndHour: 21, ImportDollars: 0.50, ExportDollars: 0.18, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, EndHour: 24, ImportDollars: 0.22, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.InDelta(t, 0.50, plan.ImportPeakDollars, 1e-9)
		assert.InDelta(t, 0.08, plan.ImportOffPeakDollars, 1e-9)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 0, EndMin: 360}}, plan.ImportOffPeakRanges)
		if assert.Len(t, plan.ImportMidPeakGroups, 1) {
			assert.InDelta(t, 0.22, plan.ImportMidPeakGroups[0].RateDollars, 1e-9)
			assert.Equal(t, []enphaseMinuteRange{{StartMin: 360, EndMin: 960}, {StartMin: 1260, EndMin: 1440}}, plan.ImportMidPeakGroups[0].Ranges)
		}
		assert.Equal(t, "tou", plan.ExportType)
		assert.InDelta(t, 0.18, plan.ExportPeakDollars, 1e-9)
		// Weighted export rate (6*0.02 + 13*0.04)/19 rounded to 5 decimal places by calculateDurationWeightedRates
		assert.InDelta(t, 0.03368, plan.ExportOffPeakDollars, 1e-9)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 960, EndMin: 1260}}, plan.ExportPeakRanges)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 0, EndMin: 960}, {StartMin: 1260, EndMin: 1440}}, plan.ExportOffPeakRanges)
	})

	t.Run("Four-tier asymmetric TOU plan", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 6, ImportDollars: 0.06, ExportDollars: 0.02, BatteryMode: types.BatteryModeChargeAny},
				{StartHour: 6, EndHour: 15, ImportDollars: 0.15, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},
				{StartHour: 15, EndHour: 17, ImportDollars: 0.28, ExportDollars: 0.06, BatteryMode: types.BatteryModeLoad},
				{StartHour: 17, EndHour: 21, ImportDollars: 0.55, ExportDollars: 0.20, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, EndHour: 24, ImportDollars: 0.15, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.InDelta(t, 0.55, plan.ImportPeakDollars, 1e-9)
		assert.InDelta(t, 0.06, plan.ImportOffPeakDollars, 1e-9)
		if assert.Len(t, plan.ImportMidPeakGroups, 2) {
			assert.InDelta(t, 0.15, plan.ImportMidPeakGroups[0].RateDollars, 1e-9)
			assert.InDelta(t, 0.28, plan.ImportMidPeakGroups[1].RateDollars, 1e-9)
		}
		assert.Equal(t, "tou", plan.ExportType)
		assert.InDelta(t, 0.20, plan.ExportPeakDollars, 1e-9)
	})

	t.Run("All-peak schedule synthesizes lower off-peak fallback", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 24, ImportDollars: 0.30, ExportDollars: 0.10, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.InDelta(t, 0.30, plan.ImportPeakDollars, 1e-9)
		assert.InDelta(t, 0.29, plan.ImportOffPeakDollars, 1e-9)
		assert.Equal(t, []enphaseMinuteRange{{StartMin: 0, EndMin: 1440}}, plan.ImportPeakRanges)
		assert.Empty(t, plan.ImportOffPeakRanges)
		assert.Equal(t, "tou", plan.ExportType)
		assert.InDelta(t, 0.09, plan.ExportOffPeakDollars, 1e-9)
		assert.InDelta(t, 0.10, plan.ExportPeakDollars, 1e-9)
	})

	t.Run("Exceeding max peak periods keeps active and longest windows", func(t *testing.T) {
		// now is 14:15 (minute 855): the 14:00-14:30 peak is short (30m) but currently active,
		// while 09:00-09:30 is short (30m) and inactive so it must be demoted.
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 1, ImportDollars: 0.08, ExportDollars: 0.02, BatteryMode: types.BatteryModeChargeAny},
				{StartHour: 1, EndHour: 2, EndMinute: 30, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},                   // 90m
				{StartHour: 3, EndHour: 5, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},                                  // 120m
				{StartHour: 6, EndHour: 7, EndMinute: 15, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},                   // 75m
				{StartHour: 7, EndHour: 9, ImportDollars: 0.16, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},                                            // off-peak tier 2
				{StartHour: 9, StartMinute: 0, EndHour: 9, EndMinute: 30, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},   // 30m inactive -> demoted
				{StartHour: 10, EndHour: 12, ImportDollars: 0.16, ExportDollars: 0.04, BatteryMode: types.BatteryModeLoad},                                          // off-peak tier 2
				{StartHour: 12, EndHour: 13, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},                                // 60m inactive -> demoted
				{StartHour: 14, StartMinute: 0, EndHour: 14, EndMinute: 30, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true}, // 30m active at 14:15 -> kept
				{StartHour: 16, EndHour: 17, EndMinute: 45, ImportDollars: 0.50, ExportDollars: 0.20, SolarMode: types.SolarModeExport, Peak: true},                 // 105m
				{StartHour: 18, EndHour: 24, ImportDollars: 0.08, ExportDollars: 0.02, BatteryMode: types.BatteryModeLoad},
			},
		}

		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.Equal(t, []enphaseMinuteRange{
			{StartMin: 60, EndMin: 150},   // 01:00-02:30 (90m)
			{StartMin: 180, EndMin: 300},  // 03:00-05:00 (120m)
			{StartMin: 360, EndMin: 435},  // 06:00-07:15 (75m)
			{StartMin: 840, EndMin: 870},  // 14:00-14:30 (30m active)
			{StartMin: 960, EndMin: 1065}, // 16:00-17:45 (105m)
		}, plan.ImportPeakRanges)
		// Demoted peak windows (09:00-09:30 and 12:00-13:00) coalesce with adjacent mid-peak ranges while staying < ImportPeakDollars
		if assert.Len(t, plan.ImportMidPeakGroups, 1) {
			assert.Equal(t, []enphaseMinuteRange{
				{StartMin: 435, EndMin: 570}, // 07:15-09:00 + demoted 09:00-09:30
				{StartMin: 600, EndMin: 780}, // 10:00-12:00 + demoted 12:00-13:00
			}, plan.ImportMidPeakGroups[0].Ranges)
			assert.Less(t, plan.ImportMidPeakGroups[0].RateDollars, plan.ImportPeakDollars)
		}
	})

	t.Run("Short peaks expand to 15m clamp at midnight and merge overlaps", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 8, ImportDollars: 0.10, ExportDollars: 0.10, BatteryMode: types.BatteryModeLoad},
				{StartHour: 8, StartMinute: 0, EndHour: 8, EndMinute: 10, ImportDollars: 0.35, ExportDollars: 0.35, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 8, StartMinute: 5, EndHour: 8, EndMinute: 20, ImportDollars: 0.35, ExportDollars: 0.35, BatteryMode: types.BatteryModeExport, Peak: true},
				{StartHour: 8, StartMinute: 20, EndHour: 23, EndMinute: 55, ImportDollars: 0.10, ExportDollars: 0.10, BatteryMode: types.BatteryModeLoad},
				{StartHour: 23, StartMinute: 55, EndHour: 24, EndMinute: 0, ImportDollars: 0.35, ExportDollars: 0.35, SolarMode: types.SolarModeExport, Peak: true},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.Equal(t, []enphaseMinuteRange{
			{StartMin: 480, EndMin: 500},   // 08:00-08:15 merged with 08:05-08:20
			{StartMin: 1425, EndMin: 1440}, // 23:55-24:00 expanded backward to 15m
		}, plan.ImportPeakRanges)
		assert.Equal(t, []enphaseMinuteRange{
			{StartMin: 0, EndMin: 480},
			{StartMin: 500, EndMin: 1425},
		}, plan.ImportOffPeakRanges)
	})

	t.Run("Negative prices clamp to 0.001 and export peak enforces strictly higher rate", func(t *testing.T) {
		negSched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 15, ImportDollars: -0.02, ExportDollars: -0.03, BatteryMode: types.BatteryModeChargeAny},
				{StartHour: 15, EndHour: 18, ImportDollars: 0.08, ExportDollars: 0.03, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 18, EndHour: 24, ImportDollars: -0.01, ExportDollars: -0.02, BatteryMode: types.BatteryModeLoad},
			},
		}
		negPlan := buildEnphaseTariffPlan(context.Background(), negSched, now)
		require.NotNil(t, negPlan)
		assert.InDelta(t, 0.001, negPlan.ImportOffPeakDollars, 1e-9)
		assert.InDelta(t, 0.08, negPlan.ImportPeakDollars, 1e-9)
		assert.Equal(t, "tou", negPlan.ExportType)
		assert.InDelta(t, 0.001, negPlan.ExportOffPeakDollars, 1e-9)
		assert.InDelta(t, 0.03, negPlan.ExportPeakDollars, 1e-9)

		invertedExportSched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 15, ImportDollars: 0.02, ExportDollars: 0.05, BatteryMode: types.BatteryModeChargeAny},
				{StartHour: 15, EndHour: 18, ImportDollars: 0.08, ExportDollars: 0.03, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 18, EndHour: 24, ImportDollars: 0.02, ExportDollars: 0.05, BatteryMode: types.BatteryModeLoad},
			},
		}
		invPlan := buildEnphaseTariffPlan(context.Background(), invertedExportSched, now)
		require.NotNil(t, invPlan)
		assert.Equal(t, "tou", invPlan.ExportType)
		assert.InDelta(t, 0.05, invPlan.ExportOffPeakDollars, 1e-9)
		assert.InDelta(t, 0.051, invPlan.ExportPeakDollars, 1e-9)
	})

	t.Run("Rates within net metering tolerance treat plan as net_metering", func(t *testing.T) {
		sched := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, EndHour: 17, ImportDollars: 0.10, ExportDollars: 0.104, BatteryMode: types.BatteryModeLoad},
				{StartHour: 17, EndHour: 21, ImportDollars: 0.30, ExportDollars: 0.297, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, EndHour: 24, ImportDollars: 0.10, ExportDollars: 0.10, BatteryMode: types.BatteryModeLoad},
			},
		}
		plan := buildEnphaseTariffPlan(context.Background(), sched, now)
		require.NotNil(t, plan)
		assert.Equal(t, "net_metering", plan.ExportType)
	})
}
