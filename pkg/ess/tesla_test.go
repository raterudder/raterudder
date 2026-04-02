package ess

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestECDSAKeyPEM(t *testing.T) string {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	derBytes, err := x509.MarshalECPrivateKey(privateKey)
	require.NoError(t, err)

	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: derBytes,
	}

	return string(pem.EncodeToMemory(pemBlock))
}

func TestTesla(t *testing.T) {
	keyPEM := generateTestECDSAKeyPEM(t)
	pubPEM, err := publicKeyPEMFromPrivate(keyPEM)
	require.NoError(t, err)

	teslaMap := func(ts *httptest.Server) *Map {
		m := NewMap()
		m.baseTesla = &baseTesla{
			clientID:     "test-client",
			clientSecret: "test-secret",
			keyPEM:       keyPEM,
			pubPEM:       pubPEM,
			baseURLs:     map[string]string{"NA": ts.URL + "/"},
			tokenURL:     ts.URL + "/oauth2/v3/token",
			authURL:      ts.URL + "/oauth2/v3/authorize",
			client:       http.DefaultClient,
		}
		return m
	}

	req := httptest.NewRequest("GET", "http://test.com", nil)
	ctx := common.CtxFromRequest(req)

	t.Run("RegisterTesla", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth2/v3/token":
				err := r.ParseForm()
				require.NoError(t, err)
				assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
				assert.Equal(t, "test-secret", r.FormValue("client_secret"))
				json.NewEncoder(w).Encode(map[string]any{
					"access_token": "partner-mock-access",
					"expires_in":   3600,
				})
			case "/api/1/partner_accounts":
				assert.Equal(t, "Bearer partner-mock-access", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"registered": true},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		err := teslaMap(ts).RegisterTesla(ctx, "test.com")
		require.NoError(t, err)
	})

	t.Run("Authenticate", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth2/v3/token":
				err := r.ParseForm()
				require.NoError(t, err)
				if assert.Equal(t, "NA_test-code", r.FormValue("code")) {
					json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "mock-access",
						"refresh_token": "mock-refresh",
						"expires_in":    3600,
					})
				}
			case "/api/1/products":
				json.NewEncoder(w).Encode(map[string]any{
					"response": []map[string]any{
						{"energy_site_id": 5678, "device_type": "energy", "resource_type": "wall_connector", "id": "wall-connector-5678"},
						{"energy_site_id": 1234, "device_type": "energy", "resource_type": "battery", "id": "battery-1234"},
					},
				})
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
					},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		creds := types.Credentials{
			Tesla: &types.TeslaCredentials{AuthCode: "NA_test-code"},
		}
		updatedCreds, changed, err := sys.Authenticate(ctx, creds)
		require.NoError(t, err)
		if assert.True(t, changed) {
			assert.Equal(t, "mock-access", updatedCreds.Tesla.AccessToken)
			assert.NotEmpty(t, updatedCreds.Tesla.Expiry)
			assert.Empty(t, updatedCreds.Tesla.AuthCode)
			assert.EqualValues(t, 1234, updatedCreds.Tesla.EnergySiteID)
		}
	})

	t.Run("GetStatus Basic", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 55.4,
						"grid_status":        "Active",
						"island_status":      "on_grid",
						"storm_mode_active":  true,
					},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:           "tesla",
			MinBatterySOC: 20.0,
		})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1.2, status.SolarKW)
		assert.Equal(t, -0.5, status.BatteryKW)
		assert.Equal(t, 0.7, status.GridKW)
		assert.Equal(t, 1.4, status.HomeKW)
		assert.Equal(t, 55.4, status.BatterySOC)
		assert.Equal(t, 27.0, status.BatteryCapacityKWH)
		assert.False(t, status.ElevatedMinBatterySOC)
		assert.True(t, status.BatteryAboveMinSOC)
		assert.True(t, status.EmergencyMode)
	})

	t.Run("GetStatus Grid Status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"grid_status": "Unavailable",
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)
		assert.True(t, status.GridUnavailable)
	})

	t.Run("GetStatus Elevated SOC", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 25.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 55.4,
						"grid_status":        "Active",
						"island_status":      "on_grid",
						"storm_mode_active":  false,
					},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:           "tesla",
			MinBatterySOC: 20.0,
		})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1.2, status.SolarKW)
		assert.Equal(t, -0.5, status.BatteryKW)
		assert.Equal(t, 0.7, status.GridKW)
		assert.Equal(t, 1.4, status.HomeKW)
		assert.Equal(t, 55.4, status.BatterySOC)
		assert.Equal(t, 27.0, status.BatteryCapacityKWH)
		assert.True(t, status.ElevatedMinBatterySOC)
		assert.True(t, status.BatteryAboveMinSOC)
		assert.False(t, status.EmergencyMode)
	})

	t.Run("GetStatus Below SOC", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 25.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 21.0,
						"grid_status":        "Active",
						"island_status":      "on_grid",
						"storm_mode_active":  false,
					},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:           "tesla",
			MinBatterySOC: 20.0,
		})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1.2, status.SolarKW)
		assert.Equal(t, -0.5, status.BatteryKW)
		assert.Equal(t, 0.7, status.GridKW)
		assert.Equal(t, 1.4, status.HomeKW)
		assert.Equal(t, 21.0, status.BatterySOC)
		assert.Equal(t, 27.0, status.BatteryCapacityKWH)
		assert.True(t, status.ElevatedMinBatterySOC)
		assert.False(t, status.BatteryAboveMinSOC)
		assert.False(t, status.EmergencyMode)
	})

	t.Run("GetStatus TimeZone", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"installation_time_zone": "America/New_York",
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)

		loc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		assert.Equal(t, loc.String(), status.Timestamp.Location().String())
	})

	t.Run("TeslaPublicKeyPEM_Valid", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		m := teslaMap(ts)
		pubPEM := m.TeslaPublicKeyPEM()
		assert.Contains(t, pubPEM, "BEGIN PUBLIC KEY")
		assert.Contains(t, pubPEM, "END PUBLIC KEY")
	})

	t.Run("TeslaPublicKeyPEM_Empty", func(t *testing.T) {
		mEmpty := NewMap()
		pubPEM := mEmpty.TeslaPublicKeyPEM()
		assert.Empty(t, pubPEM)
	})

	t.Run("PublicKeyPEMFromPrivate", func(t *testing.T) {
		// Valid ECDSA key
		pubPEM, err := publicKeyPEMFromPrivate(keyPEM)
		require.NoError(t, err)
		assert.Contains(t, pubPEM, "BEGIN PUBLIC KEY")
		assert.Contains(t, pubPEM, "END PUBLIC KEY")

		// Invalid key string
		_, err = publicKeyPEMFromPrivate("invalid-key")
		assert.ErrorContains(t, err, "failed to find PEM-encoded block")
	})

	t.Run("Info", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		m := teslaMap(ts)
		info := m.baseTesla.info(ctx)
		assert.Equal(t, "tesla", info.ID)
		assert.Len(t, info.OAuthURLs, 1)
		require.Nil(t, info.OAuthKey)
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"installation_time_zone": "America/Chicago",
					},
				})
			case "/api/1/energy_sites/1234/calendar_history":
				kind := r.URL.Query().Get("kind")
				assert.Equal(t, "day", r.URL.Query().Get("period"))
				assert.Equal(t, "America/Chicago", r.URL.Query().Get("time_zone"))
				switch kind {
				case "energy":
					// Return sub-hourly entries (two per hour) to test aggregation
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"serial_number": "abc123",
							"period":        "day",
							"time_series": []map[string]any{
								{
									"timestamp":                             "2026-03-12T10:15:00-05:00",
									"solar_energy_exported":                 2000.0,
									"battery_energy_exported":               500.0,
									"battery_energy_imported_from_grid":     250.0,
									"battery_energy_imported_from_solar":    500.0,
									"grid_energy_imported":                  1500.0,
									"grid_energy_exported_from_solar":       200.0,
									"grid_energy_exported_from_battery":     50.0,
									"consumer_energy_imported_from_grid":    1250.0,
									"consumer_energy_imported_from_solar":   1300.0,
									"consumer_energy_imported_from_battery": 450.0,
									"total_home_usage":                      3000.0,
									"total_solar_generation":                2000.0,
									"total_battery_charge":                  500.0,
									"total_grid_energy_exported":            250.0,
								},
								{
									"timestamp":                             "2026-03-12T11:00:00-05:00",
									"solar_energy_exported":                 3000.0,
									"battery_energy_exported":               1500.0,
									"battery_energy_imported_from_grid":     0.0,
									"battery_energy_imported_from_solar":    250.0,
									"grid_energy_imported":                  1500.0,
									"grid_energy_exported_from_solar":       200.0,
									"grid_energy_exported_from_battery":     50.0,
									"consumer_energy_imported_from_grid":    1500.0,
									"consumer_energy_imported_from_solar":   1400.0,
									"consumer_energy_imported_from_battery": 1350.0,
									"total_home_usage":                      4250.0,
									"total_solar_generation":                3000.0,
									"total_battery_charge":                  1500.0,
									"total_grid_energy_exported":            250.0,
								},
								{
									"timestamp":                             "2026-03-12T12:00:00-05:00",
									"solar_energy_exported":                 6000.0,
									"battery_energy_exported":               1000.0,
									"battery_energy_imported_from_grid":     0.0,
									"battery_energy_imported_from_solar":    2000.0,
									"grid_energy_imported":                  1000.0,
									"grid_energy_exported_from_solar":       1200.0,
									"grid_energy_exported_from_battery":     0.0,
									"consumer_energy_imported_from_grid":    1000.0,
									"consumer_energy_imported_from_solar":   2800.0,
									"consumer_energy_imported_from_battery": 1000.0,
									"total_home_usage":                      4800.0,
									"total_solar_generation":                6000.0,
									"total_battery_charge":                  2000.0,
									"total_grid_energy_exported":            1200.0,
								},
							},
						},
					})
				case "soe":
					// SOE at 15 min intervals shifted: 10:15=80, 10:30=75, 10:45=70, 11:00=72, 11:15=65, 12:00=68
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"serial_number": "abc123",
							"period":        "day",
							"time_series": []map[string]any{
								{"timestamp": "2026-03-12T10:15:00-05:00", "soe": 80.0},
								{"timestamp": "2026-03-12T10:30:00-05:00", "soe": 75.0},
								{"timestamp": "2026-03-12T10:45:00-05:00", "soe": 70.0},
								{"timestamp": "2026-03-12T11:00:00-05:00", "soe": 72.0},
								{"timestamp": "2026-03-12T11:15:00-05:00", "soe": 65.0},
								{"timestamp": "2026-03-12T12:00:00-05:00", "soe": 68.0},
							},
						},
					})
				default:
					t.Logf("Unexpected kind: %s", kind)
					w.WriteHeader(http.StatusBadRequest)
				}
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		start := time.Date(2026, 3, 12, 10, 0, 0, 0, loc)
		end := time.Date(2026, 3, 12, 12, 0, 0, 0, loc)

		stats, err := sys.GetEnergyHistory(ctx, start, end)
		require.NoError(t, err)
		if assert.Len(t, stats, 2) {
			sort.Slice(stats, func(i, j int) bool {
				return stats[i].TSHourStart.Before(stats[j].TSHourStart)
			})
			// Hour 10: aggregated from two entries (2000+3000=5000 Wh solar, etc)
			s := stats[0]
			assert.Equal(t, time.Date(2026, 3, 12, 10, 0, 0, 0, loc), s.TSHourStart)
			assert.Equal(t, 5.0, s.SolarKWH)
			assert.Equal(t, 1.0, s.BatteryChargedKWH)
			assert.Equal(t, 2.0, s.BatteryUsedKWH)
			assert.Equal(t, 3.0, s.GridImportKWH)
			assert.Equal(t, 0.5, s.GridExportKWH)
			assert.Equal(t, 7.25, s.HomeKWH)
			assert.Equal(t, 2.7, s.SolarToHomeKWH)
			assert.Equal(t, 0.75, s.SolarToBatteryKWH)
			assert.Equal(t, 0.4, s.SolarToGridKWH)
			assert.Equal(t, 1.8, s.BatteryToHomeKWH)
			assert.Equal(t, 0.1, s.BatteryToGridKWH)
			// SOC: min=70 (10:45), max=80 (10:15)
			assert.Equal(t, 70.0, s.MinBatterySOC)
			assert.Equal(t, 80.0, s.MaxBatterySOC)

			// Hour 11: single entry (from 12:00:00 timestamp)
			s2 := stats[1]
			assert.Equal(t, time.Date(2026, 3, 12, 11, 0, 0, 0, loc), s2.TSHourStart)
			assert.Equal(t, 6.0, s2.SolarKWH)
			assert.Equal(t, 2.0, s2.BatteryChargedKWH)
			assert.Equal(t, 1.0, s2.BatteryUsedKWH)
			assert.Equal(t, 1.0, s2.GridImportKWH)
			assert.Equal(t, 1.2, s2.GridExportKWH)
			assert.Equal(t, 4.8, s2.HomeKWH)
			// SOC: min=65 (11:15), max=72 (11:00)
			assert.Equal(t, 65.0, s2.MinBatterySOC)
			assert.Equal(t, 72.0, s2.MaxBatterySOC)
		}
	})

	t.Run("GetEnergyHistory Empty", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"installation_time_zone": "America/Chicago",
					},
				})
			case "/api/1/energy_sites/1234/calendar_history":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"serial_number": "abc123",
						"period":        "day",
						"time_series":   []map[string]any{},
					},
				})
			default:
				t.Logf("Unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		start := time.Date(2026, 3, 12, 0, 0, 0, 0, loc)
		end := time.Date(2026, 3, 13, 0, 0, 0, 0, loc)

		stats, err := sys.GetEnergyHistory(ctx, start, end)
		require.NoError(t, err)
		assert.Empty(t, stats)
	})

	t.Run("GetEnergyHistory Multi-day loop", func(t *testing.T) {
		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		var requests []map[string]string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"installation_time_zone": "America/Chicago",
					},
				})
			case "/api/1/energy_sites/1234/calendar_history":
				requests = append(requests, map[string]string{
					"kind":       r.URL.Query().Get("kind"),
					"start_date": r.URL.Query().Get("start_date"),
					"end_date":   r.URL.Query().Get("end_date"),
				})
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"serial_number": "abc123",
						"period":        "day",
						"time_series":   []map[string]any{},
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL

		// 3 days range
		start := time.Date(2026, 3, 10, 0, 0, 0, 0, loc)
		end := time.Date(2026, 3, 13, 0, 0, 0, 0, loc)

		_, err = sys.GetEnergyHistory(ctx, start, end)
		require.NoError(t, err)

		// Expect 3 days * 2 kinds (energy, soe) = 6 requests
		assert.Equal(t, 6, len(requests))

		for _, req := range requests {
			s, err := time.Parse(time.RFC3339, req["start_date"])
			require.NoError(t, err)
			e, err := time.Parse(time.RFC3339, req["end_date"])
			require.NoError(t, err)

			sInLoc := s.In(loc)
			eInLoc := e.In(loc)

			// Assert start and end are on the same day in local time
			assert.Equal(t, sInLoc.Year(), eInLoc.Year())
			assert.Equal(t, sInLoc.Month(), eInLoc.Month())
			assert.Equal(t, sInLoc.Day(), eInLoc.Day())
		}
	})

	t.Run("SetModes", func(t *testing.T) {
		setupTesla := func(t *testing.T, initialMode string, initialSOC float64, initialGrid bool, initialExport string, liveSOC float64, stormMode bool, settings types.Settings) (*Tesla, *httptest.Server, *bool, *bool, *bool, *map[string]any) {
			modeCalled := false
			backupCalled := false
			gridCalled := false
			lastReq := make(map[string]any)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/1/energy_sites/1234/site_info":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"backup_reserve_percent": initialSOC,
							"default_real_mode":      initialMode,
							"components": map[string]any{
								"customer_preferred_export_rule":                 initialExport,
								"disallow_charge_from_grid_with_solar_installed": initialGrid,
							},
						},
					})
				case "/api/1/energy_sites/1234/live_status":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"percentage_charged": liveSOC,
							"storm_mode_active":  stormMode,
						},
					})
				case "/api/1/energy_sites/1234/operation":
					modeCalled = true
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				case "/api/1/energy_sites/1234/backup":
					backupCalled = true
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				case "/api/1/energy_sites/1234/grid_import_export":
					gridCalled = true
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", settings)
			require.NoError(t, err)
			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			return teslaSys, ts, &modeCalled, &backupCalled, &gridCalled, &lastReq
		}

		t.Run("No changes needed", func(t *testing.T) {
			sys, ts, mode, backup, grid, _ := setupTesla(t, "self_consumption", 20.0, false, "battery_ok", 50.0, false, types.Settings{
				ESS:                 "tesla",
				MinBatterySOC:       20.0,
				GridChargeBatteries: true,
				GridExportSolar:     true,
				GridExportBatteries: true,
			})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny)
			require.NoError(t, err)
			assert.False(t, *mode)
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("No change requested", func(t *testing.T) {
			sys, ts, mode, backup, grid, _ := setupTesla(t, "autonomous", 20.0, true, "pv_only", 50.0, false, types.Settings{ESS: "tesla"})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeNoChange, types.SolarModeNoChange)
			require.NoError(t, err)
			assert.False(t, *mode)
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("ChargeAny", func(t *testing.T) {
			sys, ts, mode, backup, grid, lastReq := setupTesla(t, "autonomous", 20.0, true, "pv_only", 50.0, false, types.Settings{
				ESS:                 "tesla",
				GridChargeBatteries: true,
				GridExportSolar:     true,
				GridExportBatteries: true,
			})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny)
			require.NoError(t, err)
			assert.True(t, *mode)
			assert.True(t, *backup)
			assert.True(t, *grid)

			// Check one of the payloads to ensure correctness
			assert.False(t, (*lastReq)["disallow_charge_from_grid_with_solar_installed"].(bool))
			assert.Equal(t, "battery_ok", (*lastReq)["customer_preferred_export_rule"])
		})

		t.Run("ChargeSolar", func(t *testing.T) {
			sys, ts, _, backup, grid, lastReq := setupTesla(t, "self_consumption", 20.0, false, "battery_ok", 50.0, false, types.Settings{ESS: "tesla"})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeChargeSolar, types.SolarModeNoExport)
			require.NoError(t, err)
			assert.True(t, *backup)
			assert.True(t, *grid)
			assert.Equal(t, 100.0, (*lastReq)["backup_reserve_percent"])
			assert.Equal(t, "never", (*lastReq)["customer_preferred_export_rule"])
		})

		t.Run("Standby below min SOC", func(t *testing.T) {
			sys, ts, _, backup, grid, lastReq := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 15.0, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange)
			require.NoError(t, err)
			// Target is floor(15) = 15, which is < 20, so target should be 20.
			// Since initial SOC is already 20, backup update should NOT be called.
			assert.False(t, *backup)
			assert.True(t, *grid)
			assert.True(t, (*lastReq)["disallow_charge_from_grid_with_solar_installed"].(bool))
		})

		t.Run("Standby above min SOC", func(t *testing.T) {
			sys, ts, _, backup, grid, lastReq := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 55.6, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange)
			require.NoError(t, err)
			assert.True(t, *backup)
			assert.True(t, *grid)
			assert.Equal(t, 55.0, (*lastReq)["backup_reserve_percent"])
		})

		t.Run("Storm mode active", func(t *testing.T) {
			sys, ts, _, _, _, _ := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 55.0, true, types.Settings{ESS: "tesla"})
			defer ts.Close()

			err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "device is in storm mode")
		})
	})
}
