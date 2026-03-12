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
	"testing"

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
				json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "partner-mock-access",
					"expires_in":   3600,
				})
			case "/api/1/partner_accounts":
				assert.Equal(t, "Bearer partner-mock-access", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{"registered": true},
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
					json.NewEncoder(w).Encode(map[string]interface{}{
						"access_token":  "mock-access",
						"refresh_token": "mock-refresh",
						"expires_in":    3600,
					})
				}
			case "/api/1/products":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": []map[string]interface{}{
						{"energy_site_id": 1234, "resource_type": "battery"},
					},
				})
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
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
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 55.4,
						"total_pack_energy":  26000.0,
						"grid_status":        "Active",
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
		assert.Equal(t, 26.0, status.BatteryCapacityKWH)
		assert.False(t, status.ElevatedMinBatterySOC)
		assert.True(t, status.BatteryAboveMinSOC)
		assert.True(t, status.EmergencyMode)
	})

	t.Run("GetStatus Elevated SOC", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"backup_reserve_percent": 25.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 55.4,
						"total_pack_energy":  26000.0,
						"grid_status":        "Active",
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
		assert.Equal(t, 26.0, status.BatteryCapacityKWH)
		assert.True(t, status.ElevatedMinBatterySOC)
		assert.True(t, status.BatteryAboveMinSOC)
		assert.False(t, status.EmergencyMode)
	})

	t.Run("GetStatus Below SOC", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"backup_reserve_percent": 25.0,
						"nameplate_energy":       27000.0,
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"solar_power":        1200.0,
						"battery_power":      -500.0,
						"grid_power":         700.0,
						"load_power":         1400.0,
						"percentage_charged": 21.0,
						"total_pack_energy":  26000.0,
						"grid_status":        "Active",
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
		assert.Equal(t, 26.0, status.BatteryCapacityKWH)
		assert.True(t, status.ElevatedMinBatterySOC)
		assert.False(t, status.BatteryAboveMinSOC)
		assert.False(t, status.EmergencyMode)
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
}
