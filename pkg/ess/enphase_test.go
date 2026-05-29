package ess

import (
	"context"
	"encoding/json"
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
				if date == "2026-04-13" {
					json.NewEncoder(w).Encode(map[string]any{
						"start_date": "2026-04-13",
						"stats": []map[string]any{
							map[string]any{
								"production":      []int{0, 0, 0, 1000}, // 4th interval has 1000Wh
								"consumption":     []int{0, 0, 0, 0},
								"solar_home":      []int{0, 0, 0, 500},
								"solar_grid":      []int{0, 0, 0, 500},
								"start_time":      1776038400, // 2026-04-13T00:00:00Z
								"interval_length": 900,
							},
						},
					})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{}) // empty
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

		start := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 4, 13, 23, 59, 59, 0, time.UTC)

		stats, err := e.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err)

		require.Len(t, stats, 1)
		assert.Equal(t, start, stats[0].TSDayStart)

		require.Len(t, stats[0].Hourly, 1)
		hourStat := stats[0].Hourly[0]
		assert.Equal(t, start, hourStat.TSHourStart)

		assert.Equal(t, 1.0, hourStat.SolarKWH)
		assert.Equal(t, 0.5, hourStat.SolarToHomeKWH)
		assert.Equal(t, 0.5, hourStat.SolarToGridKWH)
		assert.Equal(t, 0.5, hourStat.HomeKWH)
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
}
