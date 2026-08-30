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
	"net/url"
	"sort"
	"sync"
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
		t.Run("Default fallback when no serial number provided", func(t *testing.T) {
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

		t.Run("Match site via serial number", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth2/v3/token":
					json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "mock-access",
						"refresh_token": "mock-refresh",
						"expires_in":    3600,
					})
				case "/api/1/products":
					json.NewEncoder(w).Encode(map[string]any{
						"response": []map[string]any{
							{"energy_site_id": 1234, "device_type": "energy", "resource_type": "battery", "gateway_id": "Part123_SerialXYZ"},
							{"energy_site_id": 5678, "device_type": "energy", "resource_type": "battery", "gateway_id": "Part456_SerialABC"},
						},
					})
				case "/api/1/energy_sites/5678/site_info":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"backup_reserve_percent": 20.0,
							"components": map[string]any{
								"gateways": []map[string]any{
									{"serial_number": "SerialABC"},
								},
							},
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
				Tesla: &types.TeslaCredentials{
					AuthCode:     "NA_test-code",
					SerialNumber: "serialabc", // test case-insensitive match
				},
			}
			updatedCreds, changed, err := sys.Authenticate(ctx, creds)
			require.NoError(t, err)
			if assert.True(t, changed) {
				assert.EqualValues(t, 5678, updatedCreds.Tesla.EnergySiteID)
			}
		})

		t.Run("Fail when serial number doesn't match", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth2/v3/token":
					json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "mock-access",
						"refresh_token": "mock-refresh",
						"expires_in":    3600,
					})
				case "/api/1/products":
					json.NewEncoder(w).Encode(map[string]any{
						"response": []map[string]any{
							{"energy_site_id": 1234, "device_type": "energy", "resource_type": "battery", "gateway_id": "Part123_SerialXYZ"},
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

			creds := types.Credentials{
				Tesla: &types.TeslaCredentials{
					AuthCode:     "NA_test-code",
					SerialNumber: "non-existent-serial",
				},
			}
			_, _, err = sys.Authenticate(ctx, creds)
			assert.Error(t, err)
			assert.ErrorContains(t, err, "no energy site found matching serial number")
		})
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
						"solar_power":          1200.0,
						"battery_power":        -500.0,
						"grid_power":           700.0,
						"load_power":           1400.0,
						"percentage_charged":   55.4,
						"grid_status":          "Active",
						"island_status":        "on_grid",
						"storm_mode_active":    true,
						"grid_services_active": true,
						"grid_services_power":  5200.0,
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
		assert.True(t, status.VPPActive)
		assert.Equal(t, 5.2, status.VPPKW)
		assert.NotEmpty(t, status.TimeLocation, "TimeLocation should be populated")
	})

	t.Run("GetStatus Max Backup EmergencyMode", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"battery_count":          1,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				// Battery grid charging at 3.05 kW (> 2.0 kW per battery)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"solar_power":        2230.0,
						"battery_power":      -3050.0,
						"grid_power":         4744.0,
						"load_power":         3924.0,
						"percentage_charged": 88.5,
						"grid_status":        "Active",
						"island_status":      "on_grid",
						"storm_mode_active":  false,
					},
				})
			default:
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
		assert.True(t, status.EmergencyMode, "EmergencyMode should be true when Max Backup grid charging is active")
	})

	t.Run("GetStatus Gateways Fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       0.0, // Trigger gateway fallback
						"components": map[string]any{
							"gateways": []map[string]any{
								{
									"device_id":              "9626696f-3ba0-46b1-bb9c-22f3d2082017",
									"din":                    "1707000-11-J--TG124059002L4J",
									"serial_number":          "TG124059002L4J",
									"part_number":            "1707000-11-J",
									"part_type":              4,
									"part_name":              "Powerwall 3",
									"is_active":              true,
									"site_id":                "887f6ddd-6085-4203-bad0-e54d4fd325f2",
									"firmware_version":       "26.10.3-1-p acae60d8",
									"updated_datetime":       "2026-05-05T05:51:52.152Z",
									"nameplate_power_watts":  11520.0,
									"nameplate_energy_watts": 13500.0,
								},
							},
						},
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
		assert.Equal(t, 13.5, status.BatteryCapacityKWH)
		assert.Equal(t, 3.3, status.MaxBatteryChargeKW)
		assert.Equal(t, 11.52, status.MaxBatteryDischargeKW)
	})

	t.Run("GetStatus Ignore Unknown Batteries Fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       0.0,
						"components": map[string]any{
							"batteries": []map[string]any{
								{
									"is_active":                     true,
									"device_id":                     "36ff8a16-3735-4244-be90-197a3d4ea3c3",
									"serial_number":                 "TG1242490038TF",
									"part_name":                     "Unknown",
									"din":                           "1807000-20-B--TG1242490038TF",
									"nameplate_energy":              0.0,
									"part_number":                   "1807000-20-B",
									"nameplate_max_discharge_power": 0.0,
									"nameplate_max_charge_power":    0.0,
								},
							},
							"gateways": []map[string]any{
								{
									"device_id":              "9626696f-3ba0-46b1-bb9c-22f3d2082017",
									"din":                    "1707000-11-J--TG124059002L4J",
									"serial_number":          "TG124059002L4J",
									"part_number":            "1707000-11-J",
									"nameplate_power_watts":  11520.0,
									"nameplate_energy_watts": 13500.0,
								},
							},
						},
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 55.4,
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
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

		status, err := sys.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1.2, status.SolarKW)
		assert.Equal(t, -0.5, status.BatteryKW)
		assert.Equal(t, 0.7, status.GridKW)
		assert.Equal(t, 1.4, status.HomeKW)
		assert.Equal(t, 55.4, status.BatterySOC)
		assert.Equal(t, 13.5, status.BatteryCapacityKWH)
		assert.Equal(t, 3.3, status.MaxBatteryChargeKW)
		assert.Equal(t, 11.52, status.MaxBatteryDischargeKW)
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
		if assert.Len(t, info.OAuthURLs, 1) {
			defaultURL := info.OAuthURLs["default"]
			parsed, err := url.Parse(defaultURL)
			require.NoError(t, err)
			assert.Equal(t, "true", parsed.Query().Get("require_requested_scopes"))
		}
		require.Nil(t, info.OAuthKey)
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		dateEnergyCalls := make(map[string]int)
		dateSOECalls := make(map[string]int)
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
				startDate := r.URL.Query().Get("start_date")
				switch kind {
				case "energy":
					dateEnergyCalls[startDate]++
					switch startDate {
					case "2026-03-12T00:00:00-05:00":
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
										"grid_services_energy_exported":         500.0,
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
										"grid_services_energy_exported":         1000.0,
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
									// bad data
									{
										"timestamp":                             "2026-03-13T12:00:00-05:00",
										"solar_energy_exported":                 9999.0,
										"battery_energy_exported":               9999.0,
										"battery_energy_imported_from_grid":     0.0,
										"battery_energy_imported_from_solar":    9999.0,
										"grid_energy_imported":                  9999.0,
										"grid_energy_exported_from_solar":       9999.0,
										"grid_energy_exported_from_battery":     0.0,
										"consumer_energy_imported_from_grid":    9999.0,
										"consumer_energy_imported_from_solar":   9999.0,
										"consumer_energy_imported_from_battery": 9999.0,
										"total_home_usage":                      9999.0,
										"total_solar_generation":                9999.0,
										"total_battery_charge":                  9999.0,
										"total_grid_energy_exported":            9999.0,
									},
								},
							},
						})
					case "2026-03-13T00:00:00-05:00":
						json.NewEncoder(w).Encode(map[string]any{
							"response": map[string]any{
								"serial_number": "abc123",
								"period":        "day",
								"time_series": []map[string]any{
									{
										"timestamp":                             "2026-03-13T00:00:00-05:00",
										"solar_energy_exported":                 4000.0,
										"battery_energy_exported":               3000.0,
										"battery_energy_imported_from_grid":     500.0,
										"battery_energy_imported_from_solar":    500.0,
										"grid_energy_imported":                  1000.0,
										"grid_energy_exported_from_solar":       400.0,
										"grid_energy_exported_from_battery":     100.0,
										"consumer_energy_imported_from_grid":    1050.0,
										"consumer_energy_imported_from_solar":   600.0,
										"consumer_energy_imported_from_battery": 400.0,
										"total_home_usage":                      5000.0,
										"total_solar_generation":                6000.0,
										"total_battery_charge":                  9000.0,
										"total_grid_energy_exported":            8000.0,
									},
								},
							},
						})
					default:
						t.Logf("Unexpected date: %s", startDate)
						w.WriteHeader(http.StatusBadRequest)
					}
				case "soe":
					dateSOECalls[startDate]++
					switch startDate {
					case "2026-03-12T00:00:00-05:00":
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
									{"timestamp": "2026-03-12T23:15:00-05:00", "soe": 68.0},
									// bad data
									{"timestamp": "2026-03-13T12:15:00-05:00", "soe": 20.0},
								},
							},
						})
					default:
						t.Logf("Unexpected date: %s", startDate)
						w.WriteHeader(http.StatusBadRequest)
					}
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
		if assert.Len(t, stats, 1) { // 1 day
			assert.Equal(t, "America/Chicago", stats[0].TimeLocation)
			hourly := stats[0].Hourly
			sort.Slice(hourly, func(i, j int) bool {
				return hourly[i].TSHourStart.Before(hourly[j].TSHourStart)
			})
			// Hour 10: aggregated from two entries (2000+3000=5000 Wh solar, etc)
			s := hourly[0]
			assert.Equal(t, "America/Chicago", s.TimeLocation)
			assert.Equal(t, time.Date(2026, 3, 12, 10, 0, 0, 0, loc), s.TSHourStart)
			assert.Equal(t, 5.0, s.SolarKWH)
			assert.Equal(t, 1.0, s.BatteryChargedKWH)
			assert.Equal(t, 2.0, s.BatteryUsedKWH)
			assert.Equal(t, 3.0, s.GridImportKWH)
			assert.Equal(t, 0.5, s.GridExportKWH)
			assert.Equal(t, 1.5, s.VPPExportKWH)
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
			s2 := hourly[1]
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

			// Hour 23
			s3 := hourly[2]
			assert.Equal(t, time.Date(2026, 3, 12, 23, 0, 0, 0, loc), s3.TSHourStart)
			assert.Equal(t, 4.0, s3.SolarKWH)
			assert.Equal(t, 1.0, s3.BatteryChargedKWH)
			assert.Equal(t, 3.0, s3.BatteryUsedKWH)
			assert.Equal(t, 1.0, s3.GridImportKWH)
			assert.Equal(t, 0.5, s3.GridExportKWH)
			assert.Equal(t, 2.05, s3.HomeKWH)
			assert.Equal(t, 68.0, s3.MinBatterySOC)
			assert.Equal(t, 68.0, s3.MaxBatterySOC)
		}
		assert.Equal(t, 1, dateEnergyCalls["2026-03-12T00:00:00-05:00"])
		assert.Equal(t, 1, dateEnergyCalls["2026-03-13T00:00:00-05:00"])
		assert.Equal(t, 1, dateSOECalls["2026-03-12T00:00:00-05:00"])
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

			_, err := sys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{})
			require.NoError(t, err)
			assert.False(t, *mode)
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("No change requested", func(t *testing.T) {
			sys, ts, mode, backup, grid, _ := setupTesla(t, "autonomous", 20.0, true, "pv_only", 50.0, false, types.Settings{ESS: "tesla"})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeNoChange, types.SolarModeNoChange, types.ModesOptions{})
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

			_, err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
			require.NoError(t, err)
			assert.True(t, *mode)
			assert.True(t, *backup)
			assert.True(t, *grid)

			// Check one of the payloads to ensure correctness
			assert.False(t, (*lastReq)["disallow_charge_from_grid_with_solar_installed"].(bool))
			assert.Equal(t, "battery_ok", (*lastReq)["customer_preferred_export_rule"])
		})

		t.Run("Standby below min SOC", func(t *testing.T) {
			sys, ts, _, backup, grid, _ := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 15.0, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			// Target is floor(15) = 15, which is < 20, so target should be 20.
			// Since initial SOC is already 20, backup update should NOT be called.
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("Standby avoids small SOC updates", func(t *testing.T) {
			sys, ts, _, backup, grid, _ := setupTesla(t, "self_consumption", 55.0, false, "pv_only", 55.6, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			// Target is math.Floor(55.6) = 55.0. Since old SOC is 55.0, the diff is <= 1.0.
			// We avoid updating backup reserve percent, and grid import export is not updated.
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("Standby above min SOC", func(t *testing.T) {
			sys, ts, _, backup, _, lastReq := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 55.6, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			assert.True(t, *backup)
			assert.Equal(t, 55.0, (*lastReq)["backup_reserve_percent"])
		})

		t.Run("Storm mode active grid charge change", func(t *testing.T) {
			sys, ts, mode, backup, grid, lastReq := setupTesla(t, "self_consumption", 20.0, true, "pv_only", 55.0, true, types.Settings{
				ESS:                 "tesla",
				GridChargeBatteries: true,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
			require.NoError(t, err)
			assert.False(t, *mode)
			assert.False(t, *backup)
			assert.True(t, *grid)
			assert.False(t, (*lastReq)["disallow_charge_from_grid_with_solar_installed"].(bool))
		})

		t.Run("Storm mode active no change needed", func(t *testing.T) {
			sys, ts, mode, backup, grid, _ := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 55.0, true, types.Settings{
				ESS:                 "tesla",
				GridChargeBatteries: true,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
			require.NoError(t, err)
			assert.False(t, *mode)
			assert.False(t, *backup)
			assert.False(t, *grid)
		})

		t.Run("Standby fallback to backup mode when SOC between 81 and 99", func(t *testing.T) {
			sys, ts, mode, backup, grid, lastReq := setupTesla(t, "self_consumption", 20.0, false, "pv_only", 85.6, false, types.Settings{
				ESS:           "tesla",
				MinBatterySOC: 20.0,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			assert.True(t, *mode)
			assert.True(t, *backup)
			assert.True(t, *grid)
			assert.Equal(t, 100.0, (*lastReq)["backup_reserve_percent"])
		})

		t.Run("ChargeToSOC bulk backup mode when delta > 10", func(t *testing.T) {
			sys, ts, mode, backup, _, lastReq := setupTesla(t, "self_consumption", 20.0, true, "pv_only", 50.0, false, types.Settings{
				ESS:                 "tesla",
				GridChargeBatteries: true,
				GridExportSolar:     true,
				GridExportBatteries: true,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 85})
			require.NoError(t, err)
			assert.True(t, *mode)
			assert.True(t, *backup)
			assert.Equal(t, 100.0, (*lastReq)["backup_reserve_percent"])
		})

		t.Run("ChargeToSOC tail self_consumption mode when delta <= 10", func(t *testing.T) {
			sys, ts, _, backup, _, lastReq := setupTesla(t, "self_consumption", 20.0, true, "pv_only", 80.0, false, types.Settings{
				ESS:                 "tesla",
				GridChargeBatteries: true,
				GridExportSolar:     true,
				GridExportBatteries: true,
			})
			defer ts.Close()

			_, err := sys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 85})
			require.NoError(t, err)
			assert.True(t, *backup)
			assert.Equal(t, 100.0, (*lastReq)["backup_reserve_percent"])
		})

		t.Run("Unexpected grid charging forces reserve update in BatteryModeLoad", func(t *testing.T) {
			backupCalled := false
			lastReq := make(map[string]any)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/1/energy_sites/1234/site_info":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"backup_reserve_percent": 20.0,
							"default_real_mode":      "self_consumption",
							"components": map[string]any{
								"customer_preferred_export_rule":                 "pv_only",
								"disallow_charge_from_grid_with_solar_installed": true,
							},
						},
					})
				case "/api/1/energy_sites/1234/live_status":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"percentage_charged": 50.0,
							"battery_power":      -1670.0,
							"solar_power":        170.0,
							"grid_power":         6000.0,
						},
					})
				case "/api/1/energy_sites/1234/operation":
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				case "/api/1/energy_sites/1234/backup":
					backupCalled = true
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				default:
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

			_, err = teslaSys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			assert.True(t, backupCalled)
			assert.Equal(t, 20.0, lastReq["backup_reserve_percent"])
		})

		t.Run("Unexpected non-discharging forces reserve update in BatteryModeLoad", func(t *testing.T) {
			backupCalled := false
			lastReq := make(map[string]any)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/1/energy_sites/1234/site_info":
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"backup_reserve_percent": 20.0,
							"default_real_mode":      "self_consumption",
							"components": map[string]any{
								"customer_preferred_export_rule":                 "pv_only",
								"disallow_charge_from_grid_with_solar_installed": true,
							},
						},
					})
				case "/api/1/energy_sites/1234/live_status":
					// Battery is idle (0W), load is 3000W from grid, SOC is 50% (> 20% reserve)
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"percentage_charged": 50.0,
							"battery_power":      0.0,
							"solar_power":        0.0,
							"grid_power":         3000.0,
							"load_power":         3000.0,
						},
					})
				case "/api/1/energy_sites/1234/operation":
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				case "/api/1/energy_sites/1234/backup":
					backupCalled = true
					json.NewDecoder(r.Body).Decode(&lastReq)
					json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"code": 200}})
				default:
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

			_, err = teslaSys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
			require.NoError(t, err)
			assert.True(t, backupCalled)
			assert.Equal(t, 20.0, lastReq["backup_reserve_percent"])
		})
	})

	t.Run("doGETRequest Decode Failures", func(t *testing.T) {
		t.Run("Envelope unmarshal failure", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`"envelope-error"`))
			}))
			defer ts.Close()

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
			require.NoError(t, err)

			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			var res teslaSiteInfoResponse
			err = teslaSys.doGETRequest(ctx, "api/1/energy_sites/1234/site_info", nil, &res)
			require.Error(t, err)
			assert.ErrorContains(t, err, "failed to decode tesla envelope")
		})

		t.Run("Target unmarshal failure (empty/string response)", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"response": ""}`))
			}))
			defer ts.Close()

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
			require.NoError(t, err)

			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			var res teslaSiteInfoResponse
			err = teslaSys.doGETRequest(ctx, "api/1/energy_sites/1234/site_info", nil, &res)
			require.Error(t, err)
			assert.ErrorContains(t, err, "failed to decode tesla response")
		})
	})

	t.Run("GetStatus 424 Retry Success", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				calls++
				if calls < 3 {
					w.WriteHeader(424)
					w.Write([]byte(`{"error": "failed_dependency", "error_description": "device offline"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
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
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:           "tesla",
			MinBatterySOC: 20.0,
		})
		if assert.NoError(t, err) {
			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL
			teslaSys.retryDelay1 = 1 * time.Millisecond
			teslaSys.retryDelay2 = 1 * time.Millisecond

			status, err := sys.GetStatus(ctx)
			if assert.NoError(t, err) {
				assert.Equal(t, 55.4, status.BatterySOC)
				assert.Equal(t, 3, calls)
			}
		}
	})

	t.Run("GetStatus 424 Retry Failure", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				calls++
				w.WriteHeader(424)
				w.Write([]byte(`{"error": "failed_dependency", "error_description": "device offline"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:           "tesla",
			MinBatterySOC: 20.0,
		})
		if assert.NoError(t, err) {
			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL
			teslaSys.retryDelay1 = 1 * time.Millisecond
			teslaSys.retryDelay2 = 1 * time.Millisecond

			_, err := sys.GetStatus(ctx)
			assert.Error(t, err)
			assert.Equal(t, 3, calls)
		}
	})

	t.Run("getSiteInfo 500 Retry Success", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/1/energy_sites/1234/site_info" {
				calls++
				if calls < 3 {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error": "internal_error", "error_description": "server error"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
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
		teslaSys.retryDelay1 = 1 * time.Millisecond
		teslaSys.retryDelay2 = 1 * time.Millisecond

		res, err := teslaSys.getSiteInfo(ctx)
		require.NoError(t, err)
		assert.Equal(t, 20.0, res.BackupReservePercent)
		assert.Equal(t, 3, calls)
	})

	t.Run("getSiteInfo 500 Retry Failure", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/1/energy_sites/1234/site_info" {
				calls++
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error": "internal_error", "error_description": "server error"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
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
		teslaSys.retryDelay1 = 1 * time.Millisecond
		teslaSys.retryDelay2 = 1 * time.Millisecond

		_, err = teslaSys.getSiteInfo(ctx)
		require.Error(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("SetModes Delayed Verification Success", func(t *testing.T) {
		backupPosts := 0
		reservePercent := 20.0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": reservePercent,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 50.0,
						"solar_power":        0.0,
						"battery_power":      0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				var payload map[string]float64
				if json.NewDecoder(r.Body).Decode(&payload) == nil {
					if val, ok := payload["backup_reserve_percent"]; ok {
						reservePercent = val
					}
				}
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"code": 200,
					},
				})
			default:
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
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{MinimumSOC: 10})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 1, backupPosts)
	})

	t.Run("SetModes Delayed Verification Retry", func(t *testing.T) {
		backupPosts := 0
		siteInfoCalls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				siteInfoCalls++
				reserve := 20.0
				// On verification call (call #2), return mismatched reserve to force retry
				if siteInfoCalls > 1 {
					reserve = 50.0
				}
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": reserve,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				liveCalls := siteInfoCalls
				soc := 50.0
				if liveCalls > 1 {
					soc = 50.1
				}
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": soc,
						"solar_power":        0.0,
						"battery_power":      0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"code": 200,
					},
				})
			default:
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
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{MinimumSOC: 10})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 2, backupPosts, "expected initial backup post plus retry backup post")
	})

	t.Run("SetModes Delayed Verification Fully Charged Battery", func(t *testing.T) {
		backupPosts := 0
		reservePercent := 50.0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": reservePercent,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 100.0,
						"solar_power":        0.0,
						"battery_power":      0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				var payload map[string]float64
				if json.NewDecoder(r.Body).Decode(&payload) == nil {
					if val, ok := payload["backup_reserve_percent"]; ok {
						reservePercent = val
					}
				}
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:                 "tesla",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 100})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 1, backupPosts, "expected only 1 backup post since battery is 100% full")
	})

	t.Run("SetModes Delayed Verification Standby Discharging Below Reserve Retries", func(t *testing.T) {
		backupPosts := 0
		liveStatusCalls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				liveStatusCalls++
				soc := 55.4
				if liveStatusCalls > 1 {
					soc = 54.0
				}
				// SOC is 54.0% (<= expectedReserve 55.0%), but battery is still discharging at 500W
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": soc,
						"solar_power":        0.0,
						"battery_power":      500.0,
						"grid_power":         1000.0,
						"load_power":         1500.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		// SetModes standby at live SOC 55.4% sets expectedReserve to 55.0%
		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 2, backupPosts, "expected initial backup post plus retry backup post when battery continues discharging at/below reserve in standby")
	})

	t.Run("getLiveStatusWithCache Caching", func(t *testing.T) {
		liveStatusCalls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/1/energy_sites/1234/live_status" {
				liveStatusCalls++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 75.0,
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-token"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL
		teslaSys.retryDelay1 = 1 * time.Millisecond
		teslaSys.retryDelay2 = 1 * time.Millisecond

		// 1. Initial fetch
		ls1, err := teslaSys.getLiveStatusWithCache(ctx, false)
		require.NoError(t, err)
		assert.Equal(t, 75.0, ls1.PercentageCharged)
		assert.Equal(t, 1, liveStatusCalls)

		// 2. Cached fetch (refresh = false)
		ls2, err := teslaSys.getLiveStatusWithCache(ctx, false)
		require.NoError(t, err)
		assert.Equal(t, 75.0, ls2.PercentageCharged)
		assert.Equal(t, 1, liveStatusCalls, "should use cached liveStatus without HTTP request")

		// 3. Forced refresh (refresh = true)
		ls3, err := teslaSys.getLiveStatusWithCache(ctx, true)
		require.NoError(t, err)
		assert.Equal(t, 75.0, ls3.PercentageCharged)
		assert.Equal(t, 2, liveStatusCalls, "should force HTTP fetch when refresh is true")
	})

	t.Run("SetModes Delayed Verification ChargeAny Under 80% Retries", func(t *testing.T) {
		backupPosts := 0
		liveStatusCalls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				liveStatusCalls++
				soc := 50.0
				if liveStatusCalls > 1 {
					soc = 50.1
				}
				// SOC is 50.1% (<80%), but battery_power is 0.0 (>= -1000.0, i.e. not charging > 1kW)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": soc,
						"solar_power":        0.0,
						"battery_power":      0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			case "/api/1/energy_sites/1234/operation":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		m := teslaMap(ts)
		sys, err := m.Site(ctx, "test-site", types.Settings{
			ESS:                 "tesla",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		})
		require.NoError(t, err)

		teslaSys := sys.(*Tesla)
		teslaSys.token = "mock-access"
		teslaSys.energySiteID = 1234
		teslaSys.baseURL = ts.URL
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 100})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 2, backupPosts, "expected initial backup post plus retry backup post when battery is <80% and not charging at >1kW")
	})

	t.Run("SetModes Delayed Verification Live Status Unchanged Bails Out", func(t *testing.T) {
		backupPosts := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				// Return identical static live status numbers every time
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 50.0,
						"solar_power":        0.0,
						"battery_power":      0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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
		teslaSys.verifyDelay = 1 * time.Millisecond

		var wg sync.WaitGroup
		testCtx := common.CtxWithWaitGroup(ctx, &wg)

		// Expected reserve will be set to 50.0%, but actual site_info will stay at 20.0%
		changed, err := teslaSys.SetModes(testCtx, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{MinimumSOC: 10})
		require.NoError(t, err)
		assert.True(t, changed)

		wg.Wait()
		assert.Equal(t, 1, backupPosts, "expected initial backup post only; should bail out after live_status fails to update after 2 attempts")
	})

	t.Run("SetModes Suppresses Reserve Override When Max Backup Active (>2kW per battery)", func(t *testing.T) {
		backupPosts := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
						"battery_count":          1,
						"components": map[string]any{
							"customer_preferred_export_rule": "never",
						},
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				// Battery is grid charging at 3.05 kW (-3050 W > 2.0 kW/battery threshold)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 88.5,
						"battery_power":      -3050.0,
						"solar_power":        2230.0,
						"grid_power":         4744.0,
						"load_power":         3924.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			case "/api/1/energy_sites/1234/operation", "/api/1/energy_sites/1234/grid_import_export":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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

		changed, err := teslaSys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{MinimumSOC: 20})
		require.NoError(t, err)
		assert.False(t, changed, "should return false and suppress backup POST when Max Backup (>2kW/battery) is active")
		assert.Equal(t, 0, backupPosts, "should not send POST /backup when Max Backup is active")
	})

	t.Run("SetModes Triggers Reserve Override For Unexpected Low-Rate Grid Charging (<=2kW per battery)", func(t *testing.T) {
		backupPosts := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
						"battery_count":          1,
						"components": map[string]any{
							"customer_preferred_export_rule": "never",
						},
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				// Battery is grid charging at 1.67 kW (-1670 W <= 2.0 kW/battery threshold)
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 88.5,
						"battery_power":      -1670.0,
						"solar_power":        0.0,
						"grid_power":         2000.0,
						"load_power":         330.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				backupPosts++
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			case "/api/1/energy_sites/1234/operation", "/api/1/energy_sites/1234/grid_import_export":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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

		changed, err := teslaSys.SetModes(ctx, types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{MinimumSOC: 20})
		require.NoError(t, err)
		assert.True(t, changed, "should return true and trigger backup POST for low-rate unexpected grid charging (<=2kW/battery)")
		assert.Equal(t, 1, backupPosts, "should send POST /backup for low-rate unexpected grid charging")
	})

	t.Run("SetModes ChargeAny Rounds Reserve 85+ Up To 100 In Self Consumption", func(t *testing.T) {
		postedReserve := 0.0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
						"battery_count":          1,
						"components": map[string]any{
							"customer_preferred_export_rule": "never",
						},
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 80.0,
						"battery_power":      0.0,
						"solar_power":        0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				var req map[string]float64
				json.NewDecoder(r.Body).Decode(&req)
				postedReserve = req["backup_reserve_percent"]
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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

		changed, err := teslaSys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{ChargeToSOC: 88})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 100.0, postedReserve, "reserve 88% in self_consumption for ChargeAny should round up to 100%")
	})

	t.Run("SetModes ChargeAny Rounds Reserve 81-84 Down To 80 In Self Consumption", func(t *testing.T) {
		postedReserve := 0.0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"backup_reserve_percent": 20.0,
						"default_real_mode":      "self_consumption",
						"nameplate_energy":       13500.0,
						"battery_count":          1,
						"components": map[string]any{
							"customer_preferred_export_rule": "never",
						},
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 80.0,
						"battery_power":      0.0,
						"solar_power":        0.0,
						"grid_power":         0.0,
						"load_power":         0.0,
					},
				})
			case "/api/1/energy_sites/1234/backup":
				var req map[string]float64
				json.NewDecoder(r.Body).Decode(&req)
				postedReserve = req["backup_reserve_percent"]
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{"code": 200},
				})
			default:
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

		changed, err := teslaSys.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{ChargeToSOC: 83})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 80.0, postedReserve, "reserve 83% in self_consumption for ChargeAny should round down to 80%")
	})

	t.Run("GridSettings", func(t *testing.T) {
		t.Run("EditSettingFlagsEnabled", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/1/energy_sites/1234/site_info" {
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"components": map[string]any{
								"edit_setting_grid_charging":        true,
								"edit_setting_permission_to_export": true,
								"edit_setting_energy_exports":       true,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer ts.Close()

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
			require.NoError(t, err)

			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			gs, err := teslaSys.GridSettings(ctx)
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.True(t, gs.GridExportBatteries)
		})

		t.Run("EditSettingFlagsSolarOnly", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/1/energy_sites/1234/site_info" {
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"components": map[string]any{
								"edit_setting_grid_charging":                     false,
								"edit_setting_permission_to_export":              true,
								"edit_setting_energy_exports":                    false,
								"disallow_charge_from_grid_with_solar_installed": true,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer ts.Close()

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
			require.NoError(t, err)

			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			gs, err := teslaSys.GridSettings(ctx)
			require.NoError(t, err)
			assert.False(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.False(t, gs.GridExportBatteries)
		})

		t.Run("FallbackWhenEditSettingFlagsAbsent", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/1/energy_sites/1234/site_info" {
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"components": map[string]any{
								"solar":                          true,
								"customer_preferred_export_rule": "battery_ok",
								"disallow_charge_from_grid_with_solar_installed": false,
							},
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer ts.Close()

			m := teslaMap(ts)
			sys, err := m.Site(ctx, "test-site", types.Settings{ESS: "tesla"})
			require.NoError(t, err)

			teslaSys := sys.(*Tesla)
			teslaSys.token = "mock-access"
			teslaSys.energySiteID = 1234
			teslaSys.baseURL = ts.URL

			gs, err := teslaSys.GridSettings(ctx)
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.True(t, gs.GridExportBatteries)
		})
	})
}
