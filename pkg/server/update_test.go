package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/raterudder/raterudder/pkg/weather"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleUpdate(t *testing.T) {
	// Scenario: High price -> Should Discharge
	mockU := &mockUtility{}
	// Replaced mock.Anything with explicit matching for the expected UtilityProvider from settings
	mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool { return s.UtilityProvider == "test" })).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

	mockS := &mockStorage{}
	mockS.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
		{
			Energy: []types.DailyEnergyStats{
				{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
			},
		},
	}, nil)
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          true,
		MinBatterySOC:   5.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil)
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	// Add GetEnergyHistory expectation
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	// We might need strict matching for SetModes later, but for now:
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
	}

	req := httptest.NewRequest("GET", "/api/update", nil)
	req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
	w := httptest.NewRecorder()

	srv.handleUpdate(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var res updateResult
	err := json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	if assert.NotNil(t, res.Action) {
		assert.False(t, res.Action.SystemTimestamp.IsZero())
		assert.Equal(t, 5.0, res.Action.ReserveSOC)
	}

	t.Run("Handle Update - Auth", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{DryRun: true, UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		// InsertAction might not be called if validation fails, so we can't strict expect it or we use .Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Mock GetUser for notadmin check
		mockS.On("GetUser", mock.Anything, "notadmin@example.com").Return(types.User{}, fmt.Errorf("user not found"))

		// Helper to create server with auth config
		newAuthServer := func(audience, email string, adminEmails []string, srvURL string) *Server {
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			provider, err := oidc.NewProvider(context.Background(), srvURL)
			require.NoError(t, err)

			return &Server{
				utilities:           mockUMap,
				ess:                 mockP,
				storage:             mockS,
				controller:          controller.NewController(),
				updateSpecificEmail: email,
				adminEmails:         adminEmails,
				oidcVerifiers: map[string]tokenVerifier{
					"google": provider.Verifier(&oidc.Config{ClientID: audience}).Verify,
				},
			}
		}

		t.Run("Missing Authorization Header - Specific Email", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Invalid Authorization Header Format", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Basic user:pass")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		})

		t.Run("Invalid Token", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Bearer bad-token")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Valid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			bodyBytes, err := json.Marshal(map[string]string{"siteID": types.SiteIDNone})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/update", bytes.NewBuffer(bodyBytes))
			validToken := generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Valid Token, Specific Email Wrong", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "right@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "wrong@example.com", "wrong")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Valid Token, Correct Specific Email", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "right@example.com", nil, oidcSrv.URL)
			bodyBytes, err := json.Marshal(map[string]string{"siteID": types.SiteIDNone})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/update", bytes.NewBuffer(bodyBytes))
			validToken := generateTestToken(t, oidcSrv.URL, priv, "right@example.com", "right")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Invalid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("test-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			// Token for a different user
			validToken := generateTestToken(t, oidcSrv.URL, priv, "notadmin@example.com", "notadmin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("No Auth Configured - Blocked", func(t *testing.T) {
			// In the new model, we always have oidcVerifiers if we use newAuthServer
			// so we just test with a bad token or missing header (already tested)
			// But for completeness, we can create a server with empty verifiers
			srv := &Server{
				storage:   mockS,
				utilities: utility.NewMap(mockS),
				ess:       ess.NewMap(),
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Direct Handler Without Auth Blocked Before Site Lookup", func(t *testing.T) {
			strictStorage := &mockStorage{}
			srvWithEmail := &Server{
				storage:             strictStorage,
				utilities:           utility.NewMap(strictStorage),
				ess:                 ess.NewMap(),
				updateSpecificEmail: "scheduler@example.com",
			}
			// Omit siteIDContextKey so s.getSiteID would panic if reached
			req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
			w := httptest.NewRecorder()
			srvWithEmail.handleUpdate(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)

			srvWithoutEmail := &Server{
				storage:   strictStorage,
				utilities: utility.NewMap(strictStorage),
				ess:       ess.NewMap(),
			}
			req2 := httptest.NewRequest(http.MethodPost, "/api/update", nil)
			w2 := httptest.NewRecorder()
			srvWithoutEmail.handleUpdate(w2, req2)
			assert.Equal(t, http.StatusUnauthorized, w2.Result().StatusCode)

			reqReadOnlyAdmin := httptest.NewRequest(http.MethodPost, "/api/update", nil)
			reqReadOnlyAdmin = reqReadOnlyAdmin.WithContext(context.WithValue(reqReadOnlyAdmin.Context(), userContextKey, types.User{
				ID:    "google:admin-sub",
				Email: "admin@example.com",
				Admin: false,
			}))
			w3 := httptest.NewRecorder()
			srvWithoutEmail.handleUpdate(w3, reqReadOnlyAdmin)
			assert.Equal(t, http.StatusForbidden, w3.Result().StatusCode)
			strictStorage.AssertExpectations(t)
		})

		t.Run("Cookie Fallback Blocked When Update Specific Email Set", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
			require.NoError(t, err)

			const siteID = "site1"
			var setModesCalls atomic.Int32
			var insertActionCalls atomic.Int32

			strictStorage := &mockStorage{}
			strictStorage.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				insertActionCalls.Add(1)
			}).Return(nil).Maybe()

			strictESS := &mockESS{}
			strictESS.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				setModesCalls.Add(1)
			}).Return(true, nil).Maybe()

			essMap := ess.NewMap()
			essMap.SetSystem(siteID, strictESS)
			uMap := utility.NewMap(strictStorage)
			uMap.SetProvider(siteID, mockU)

			srv := &Server{
				utilities:            uMap,
				ess:                  essMap,
				storage:              strictStorage,
				controller:           controller.NewController(),
				sessionEncryptionKey: "12345678901234567890123456789012",
				sessionDuration:      7 * 24 * time.Hour,
				updateSpecificEmail:  "updater@example.com",
				updateOIDCVerifier:   provider.Verifier(&oidc.Config{ClientID: "update-audience"}).Verify,
				adminEmails:          []string{"admin@example.com"},
				oidcAudiences: map[string]string{
					"google": "test-audience",
				},
				oidcVerifiers: map[string]tokenVerifier{
					"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
				},
			}

			adminSessionToken, err := srv.createSessionToken("google:admin-sub", "admin@example.com", "admin-secret", time.Hour)
			require.NoError(t, err)
			ownerSessionToken, err := srv.createSessionToken("google:owner-sub", "owner@example.com", "owner-secret", time.Hour)
			require.NoError(t, err)

			adminCookie := &http.Cookie{Name: sessionTokenCookie, Value: adminSessionToken}
			ownerCookie := &http.Cookie{Name: sessionTokenCookie, Value: ownerSessionToken}

			muxHandler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			doJSON := func(method, path string, body any, cookie *http.Cookie, bearer string) (int, http.Header, string) {
				var buf bytes.Buffer
				if body != nil {
					require.NoError(t, json.NewEncoder(&buf).Encode(body))
				}
				req := httptest.NewRequest(method, path, &buf)
				if cookie != nil {
					req.AddCookie(cookie)
				}
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				w := httptest.NewRecorder()
				muxHandler.ServeHTTP(w, req)
				return w.Code, w.Header(), w.Body.String()
			}

			adminUpdateCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, adminCookie, "")
			ownerUpdateCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, ownerCookie, "")
			assert.Equal(t, http.StatusUnauthorized, adminUpdateCode)
			assert.Equal(t, http.StatusUnauthorized, ownerUpdateCode)
			assert.Zero(t, setModesCalls.Load())
			assert.Zero(t, insertActionCalls.Load())

			adminLegacyCookie := &http.Cookie{Name: authTokenCookie, Value: generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin-sub")}
			ownerLegacyCookie := &http.Cookie{Name: authTokenCookie, Value: generateTestToken(t, oidcSrv.URL, priv, "owner@example.com", "owner-sub")}
			adminLegacyCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, adminLegacyCookie, "")
			ownerLegacyCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, ownerLegacyCookie, "")
			assert.Equal(t, http.StatusUnauthorized, adminLegacyCode)
			assert.Equal(t, http.StatusUnauthorized, ownerLegacyCode)
			assert.Zero(t, setModesCalls.Load())
			assert.Zero(t, insertActionCalls.Load())

			strictStorage.AssertNotCalled(t, "GetUser", mock.Anything, mock.Anything)
			strictStorage.AssertNotCalled(t, "GetSite", mock.Anything, mock.Anything)
			strictStorage.AssertNotCalled(t, "GetSettings", mock.Anything, mock.Anything)
		})

		t.Run("Site Owners Allowed And Non-Owners Blocked When Update Specific Email Unset", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
			require.NoError(t, err)

			const siteID = "site1"
			var setModesCalls atomic.Int32
			var insertActionCalls atomic.Int32

			ownerStorage := &mockStorage{}
			ownerStorage.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
			ownerStorage.On("GetLatestAction", mock.Anything, siteID).Return((*types.Action)(nil), nil).Maybe()
			ownerStorage.On("GetHistorySummaries", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{
					Energy: []types.DailyEnergyStats{
						{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
					},
				},
			}, nil).Maybe()
			ownerStorage.On("GetSettings", mock.Anything, siteID).Return(types.Settings{
				MinBatterySOC:   5.0,
				UtilityProvider: "test",
			}, types.CurrentSettingsVersion, time.Time{}, nil).Maybe()
			ownerStorage.On("GetLatestEnergyHistoryTime", mock.Anything, siteID).Return(time.Time{}, 0, nil).Maybe()
			ownerStorage.On("GetLatestPriceHistoryTime", mock.Anything, siteID).Return(time.Time{}, 0, nil).Maybe()
			ownerStorage.On("GetEnergyHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
			ownerStorage.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
			ownerStorage.On("UpsertEnergyHistories", mock.Anything, siteID, mock.Anything, mock.Anything).Return(nil).Maybe()
			ownerStorage.On("UpsertPrices", mock.Anything, siteID, mock.Anything, mock.Anything).Return(nil).Maybe()
			ownerStorage.On("InsertAction", mock.Anything, siteID, mock.Anything).Run(func(args mock.Arguments) {
				insertActionCalls.Add(1)
			}).Return(nil).Maybe()

			site1Doc := types.Site{
				ID:          siteID,
				Permissions: []types.SitePermissions{{UserID: "google:owner-sub"}},
			}
			ownerStorage.On("GetUser", mock.Anything, "google:admin-sub").Return(types.User{
				ID:            "google:admin-sub",
				Email:         "admin@example.com",
				SessionSecret: "admin-secret",
			}, nil)
			ownerStorage.On("GetUser", mock.Anything, "google:other-sub").Return(types.User{
				ID:            "google:other-sub",
				Email:         "other@example.com",
				SessionSecret: "other-secret",
			}, nil)
			ownerStorage.On("GetUser", mock.Anything, "google:owner-sub").Return(types.User{
				ID:            "google:owner-sub",
				Email:         "owner@example.com",
				SessionSecret: "owner-secret",
				Sites:         []types.UserSite{{ID: siteID}},
			}, nil)
			ownerStorage.On("GetSite", mock.Anything, siteID).Return(site1Doc, nil)

			ownerESS := &mockESS{}
			ownerESS.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
			ownerESS.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Maybe()
			ownerESS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
			ownerESS.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil).Maybe()
			ownerESS.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				setModesCalls.Add(1)
			}).Return(true, nil).Maybe()

			essMap := ess.NewMap()
			essMap.SetSystem(siteID, ownerESS)
			uMap := utility.NewMap(ownerStorage)
			uMap.SetProvider(siteID, mockU)

			srv := &Server{
				utilities:            uMap,
				ess:                  essMap,
				storage:              ownerStorage,
				controller:           controller.NewController(),
				sessionEncryptionKey: "12345678901234567890123456789012",
				sessionDuration:      7 * 24 * time.Hour,
				updateSpecificEmail:  "", // Optional: unset so site owners can trigger their own updates
				adminEmails:          []string{"admin@example.com"},
				oidcAudiences: map[string]string{
					"google": "test-audience",
				},
				oidcVerifiers: map[string]tokenVerifier{
					"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
				},
			}

			adminSessionToken, err := srv.createSessionToken("google:admin-sub", "admin@example.com", "admin-secret", time.Hour)
			require.NoError(t, err)
			otherSessionToken, err := srv.createSessionToken("google:other-sub", "other@example.com", "other-secret", time.Hour)
			require.NoError(t, err)
			ownerSessionToken, err := srv.createSessionToken("google:owner-sub", "owner@example.com", "owner-secret", time.Hour)
			require.NoError(t, err)

			adminCookie := &http.Cookie{Name: sessionTokenCookie, Value: adminSessionToken}
			otherCookie := &http.Cookie{Name: sessionTokenCookie, Value: otherSessionToken}
			ownerCookie := &http.Cookie{Name: sessionTokenCookie, Value: ownerSessionToken}
			ownerLegacyCookie := &http.Cookie{Name: authTokenCookie, Value: generateTestToken(t, oidcSrv.URL, priv, "owner@example.com", "owner-sub")}

			muxHandler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			doJSON := func(method, path string, body any, cookie *http.Cookie, bearer string) (int, http.Header, string) {
				var buf bytes.Buffer
				if body != nil {
					require.NoError(t, json.NewEncoder(&buf).Encode(body))
				}
				req := httptest.NewRequest(method, path, &buf)
				if cookie != nil {
					req.AddCookie(cookie)
				}
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				w := httptest.NewRecorder()
				muxHandler.ServeHTTP(w, req)
				return w.Code, w.Header(), w.Body.String()
			}

			// Read-only multi-site admin without direct site permission must be rejected with 403
			adminCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, adminCookie, "")
			assert.Equal(t, http.StatusForbidden, adminCode)
			// Unrelated user without site permission must be rejected with 403
			otherCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, otherCookie, "")
			assert.Equal(t, http.StatusForbidden, otherCode)
			// Bearer header must not silently fall back to owner cookie when updateSpecificEmail is unset
			bearerFallbackCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, ownerCookie, generateTestToken(t, oidcSrv.URL, priv, "owner@example.com", "owner-sub"))
			assert.Equal(t, http.StatusUnauthorized, bearerFallbackCode)
			assert.Zero(t, setModesCalls.Load())
			assert.Zero(t, insertActionCalls.Load())
			ownerStorage.AssertNotCalled(t, "GetSettings", mock.Anything, mock.Anything)

			// Site owner can trigger their own update via session_token or legacy auth_token cookie
			ownerCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, ownerCookie, "")
			assert.Equal(t, http.StatusOK, ownerCode)
			assert.Equal(t, int32(1), setModesCalls.Load())
			assert.Equal(t, int32(1), insertActionCalls.Load())

			ownerLegacyCode, _, _ := doJSON(http.MethodPost, "/api/update", map[string]string{"siteID": siteID}, ownerLegacyCookie, "")
			assert.Equal(t, http.StatusOK, ownerLegacyCode)
			assert.Equal(t, int32(2), setModesCalls.Load())
			assert.Equal(t, int32(2), insertActionCalls.Load())
		})
	})

	t.Run("Paused Updates", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			Pause:           true,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		// GetStatus should be called even when paused
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 75}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		// Expect a paused action to be inserted with paused=true and self-consumption modes
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Paused && a.Description == "Automation is paused" && a.BatteryMode == types.BatteryModeLoad && a.SolarMode == types.SolarModeAny
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		// Status should still be "paused"
		assert.Equal(t, "paused", resp["status"])
		// An action should be returned with the paused flag
		require.NotNil(t, resp["action"], "a paused action should be returned")
		actionMap, ok := resp["action"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, actionMap["paused"], "action should have paused=true")
		assert.Equal(t, float64(types.BatteryModeLoad), actionMap["batteryMode"], "action should have batteryMode=BatteryModeLoad")
		assert.Equal(t, float64(types.SolarModeAny), actionMap["solarMode"], "action should have solarMode=SolarModeAny")

		// GetStatus and GetCurrentPrice should have been called even when paused
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		// SetModes should NOT be called when paused
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Paused Updates Generates Plan", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		now := time.Now().Truncate(time.Hour)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			Pause:           true,
			UtilityProvider: "test",
			MinBatterySOC:   20.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         75,
			BatteryCapacityKWH: 13.5,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}, nil)
		var futurePrices []types.Price
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.15,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Paused && a.Description == "Automation is paused" && a.BatteryMode == types.BatteryModeLoad &&
				a.SolarMode == types.SolarModeAny && a.Plan != nil && len(a.Plan.Periods) > 0
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]any
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "paused", resp["status"])
		require.NotNil(t, resp["action"])
		actionMap, ok := resp["action"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, actionMap["paused"])
		assert.NotNil(t, actionMap["plan"], "plan should be generated and attached even if paused")

		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Emergency Mode", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{EmergencyMode: true}, nil)
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{}).Return(nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "In emergency mode" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "emergency mode", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertExpectations(t)
	})

	t.Run("Action - Alarms Present", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			Alarms: []types.SystemAlarm{{Name: "Test Alarm"}},
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "1 alarms present" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "alarms present", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Grid Unavailable", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{GridUnavailable: true}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "Grid is unavailable" && a.Reason == types.ActionReasonGridUnavailable && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "grid unavailable", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - VPP Event Active", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			VPPActive: true,
			VPPKW:     3.5,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description and reason
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "VPP event active" && a.Reason == types.ActionReasonVPPActive && !a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "vpp event", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - VPP Event Active Generates Plan", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		now := time.Now().Truncate(time.Hour)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			MinBatterySOC:   20.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			VPPActive:          true,
			VPPKW:              3.5,
			BatterySOC:         50,
			BatteryCapacityKWH: 13.5,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}, nil)
		var futurePrices []types.Price
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.15,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "VPP event active" && a.Reason == types.ActionReasonVPPActive && !a.Fault &&
				a.Plan != nil && len(a.Plan.Periods) > 0
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "vpp event", resp["status"])

		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Grid Unavailable Skips Plan", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		now := time.Now().Truncate(time.Hour)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			MinBatterySOC:   20.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable:    true,
			BatterySOC:         50,
			BatteryCapacityKWH: 13.5,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "Grid is unavailable" && a.Reason == types.ActionReasonGridUnavailable && a.Fault && a.Plan == nil
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "grid unavailable", resp["status"])
	})

	t.Run("Handle Update - Backfill Logic", func(t *testing.T) {
		t.Run("Version Mismatch - Backfill Triggered", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
			mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{
					Energy: []types.DailyEnergyStats{
						{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
					},
				},
			}, nil)
			// Set up old version
			lastTime := time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil) // Version 0 < CurrentVersion
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			// Expect backfill from 5 days ago, not lastTime
			// We can verify this by checking the start time in GetEnergyHistory call to ESS
			// But for now, let's just ensure it calls GetEnergyHistory
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			var startTimes []time.Time
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				startTimes = append(startTimes, args.Get(1).(time.Time))
			}).Return([]types.DailyEnergyStats{}, nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)

			// Verify that at least one call started from ~5 days ago (midnight)
			require.NotEmpty(t, startTimes, "GetEnergyHistory should have been called")
			earliest := startTimes[0]
			for _, t := range startTimes {
				if t.Before(earliest) {
					earliest = t
				}
			}
			now := time.Now()
			fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
			expected := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
			assert.Equal(t, expected, earliest, "Backfill should start from midnight 14 days ago")
		})

		t.Run("Version Match - No Backfill", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
			mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{
					Energy: []types.DailyEnergyStats{
						{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
					},
				},
			}, nil)
			// Set up current version
			// Make lastTime recent enough that it would normally just resume from there
			lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentEnergyStatsVersion, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentPriceHistoryVersion, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			mockES.On("GetEnergyHistory", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
				// Should be equal to lastTime (or close to it due to truncation logic)
				return start.Equal(lastTime)
			}), mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	})

	t.Run("Weather Query Future Range", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		timeLoc := "America/Chicago"

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			Location: &types.SiteLocation{
				Latitude:    41.8781,
				Longitude:   -87.6298,
				TimeZone:    timeLoc,
				PostalCode:  "60601",
				CountryCode: "US",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// Mock the weather update call
		mockW := &mockWeather{}
		mockW.On("Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Verify GetWeather is called with an end time in the future
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(end time.Time) bool {
			// end time should be in the future (roughly now + 2 days)
			return end.After(time.Now().Add(24 * time.Hour))
		})).Return([]types.Weather{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			weather:    mockW,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		mockS.AssertExpectations(t)
	})

	t.Run("Staging Release Calls Plan And Performs Plan Action", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		now := time.Now().Truncate(time.Hour)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{
			DollarsPerKWH: 0.15,
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
		}, nil)

		var futurePrices []types.Price
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.15,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		var insertedAction *types.Action
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			MinBatterySOC:   20.0,
			Release:         "production",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			act := args.Get(2).(types.Action)
			insertedAction = &act
		}).Return(nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "staging", // Staging release environment
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, insertedAction, "Action must be inserted into storage")
		assert.NotNil(t, insertedAction.Plan, "Action.Plan must be populated by Controller.Plan")
		assert.NotEmpty(t, insertedAction.Plan.Periods)
		assert.Equal(t, "none", insertedAction.SimulationParams.DetectedShift)
	})

	t.Run("Production Release Calls Plan", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		now := time.Now().Truncate(time.Hour)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{
			DollarsPerKWH: 0.15,
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
		}, nil)

		var futurePrices []types.Price
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.15,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		var insertedAction *types.Action
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider:     "test",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
			ManageTOUSchedules:  true,
			Release:             "production",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			act := args.Get(2).(types.Action)
			insertedAction = &act
		}).Return(nil)

		var setBatteryMode types.BatteryMode
		var setOpts types.ModesOptions
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         100,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			setBatteryMode = args.Get(1).(types.BatteryMode)
			setOpts = args.Get(3).(types.ModesOptions)
		}).Return(true, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production", // Production release environment
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, insertedAction, "Action must be inserted into storage")
		assert.NotNil(t, insertedAction.Plan, "On production, Action.Plan must be populated by Controller.Plan")
		assert.NotEmpty(t, insertedAction.Plan.Periods)
		assert.Equal(t, types.BatteryModeLoad, insertedAction.BatteryMode, "Plan's BatteryMode must be used")
		assert.Equal(t, types.BatteryModeLoad, setBatteryMode, "SetModes must receive Plan's BatteryMode")
		assert.NotNil(t, setOpts.Schedule, "When ManageTOUSchedules=true, TOUSchedule must be built")
	})

	t.Run("Combined History Fetch Failure Returns Error", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(([]types.HistorySummary)(nil), fmt.Errorf("storage error")).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(([]types.DailyEnergyStats)(nil), fmt.Errorf("storage error")).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Maybe()
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC: 50.0,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
	})

	t.Run("GetLatestAction Returns Action Without Filtering", func(t *testing.T) {
		mockS := &mockStorage{}
		now := time.Now()
		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return now },
		}

		// 1. Fault action should still be returned
		faultAct := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Fault:       true,
			Timestamp:   now.Add(-10 * time.Minute),
		}
		mockS.On("GetLatestAction", mock.Anything, "fault-site").Return(faultAct, nil).Once()
		assert.Equal(t, faultAct, srv.getLatestAction(context.Background(), "fault-site"))

		// 2. Paused action should still be returned
		pausedAct := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Paused:      true,
			Timestamp:   now.Add(-10 * time.Minute),
		}
		mockS.On("GetLatestAction", mock.Anything, "paused-site").Return(pausedAct, nil).Once()
		assert.Equal(t, pausedAct, srv.getLatestAction(context.Background(), "paused-site"))

		// 3. Action older than 90m should still be returned
		staleAct := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Timestamp:   now.Add(-95 * time.Minute),
		}
		mockS.On("GetLatestAction", mock.Anything, "stale-site").Return(staleAct, nil).Once()
		assert.Equal(t, staleAct, srv.getLatestAction(context.Background(), "stale-site"))

		// 4. Valid action with timezone should adjust timestamps
		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		validAct := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Timestamp:   now.UTC(),
			SystemStatus: types.SystemStatus{
				TimeLocation: "America/Chicago",
			},
		}
		mockS.On("GetLatestAction", mock.Anything, "valid-site").Return(validAct, nil).Once()
		act := srv.getLatestAction(context.Background(), "valid-site")
		require.NotNil(t, act)
		assert.Equal(t, loc.String(), act.Timestamp.Location().String())
	})

	t.Run("Does Not Skip Recently Updated Site", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, types.SiteIDNone).Return(&types.Action{
			Timestamp:   now.Add(-1 * time.Minute),
			BatteryMode: types.BatteryModeLoad,
		}, nil).Once()
		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil).Once()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetPriceHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, types.SiteIDNone, mock.Anything).Return(nil).Once()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil).Once()
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil).Once()
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil).Once()
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Once()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var res updateResult
		err := json.NewDecoder(w.Body).Decode(&res)
		require.NoError(t, err)
		assert.Equal(t, "success", res.Status)
		assert.NotNil(t, res.Action)
		mockS.AssertExpectations(t)
	})

	t.Run("WeatherOnlySummaryBackfillsLateEnergyWithoutRepeatingBackfill", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
		today := truncateDay(now)
		yesterday := today.AddDate(0, 0, -1)
		historyStart := today.AddDate(0, 0, -forecastHistoryDays)
		monthStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		lateEnergy := []types.DailyEnergyStats{
			{
				TSDayStart: yesterday,
				Hourly: []types.EnergyStats{
					{TSHourStart: yesterday.Add(23 * time.Hour), HomeKWH: 1.5},
				},
			},
		}

		// 1. Late ESS energy arrives within the 35-day lookback window: merged into 2026-07 without
		// triggering backfillHistorySummaries or re-querying already-archived weather.
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, types.SiteIDNone).Return((*types.Action)(nil), nil).Once()
		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Weather: []types.Weather{
					{TSDayStart: yesterday},
				},
			},
		}, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, historyStart, yesterday.AddDate(0, 0, 1)).Return(lateEnergy, nil).Once()
		mockS.On("GetActionHistory", mock.Anything, types.SiteIDNone, yesterday.Add(23*time.Hour), yesterday.Add(24*time.Hour)).Return([]types.Action{}, nil).Once()
		mockS.On("UpdateHistorySummary", mock.Anything, types.SiteIDNone, "2026-07", mock.MatchedBy(func(hs types.HistorySummary) bool {
			return len(hs.Energy) == 1 && hs.Energy[0].TSDayStart.Equal(yesterday) && len(hs.Weather) == 0
		})).Return(types.HistorySummary{
			TSMonthStart: monthStart,
			Energy:       lateEnergy,
			Weather:      []types.Weather{{TSDayStart: yesterday}},
		}, nil).Once()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, today, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetPriceHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, types.SiteIDNone, mock.Anything).Return(nil).Once()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil).Once()
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil).Once()
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil).Once()
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Once()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockS.AssertNotCalled(t, "GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)

		// 2. Weather-only month with no energy data yet: performs zero UpdateHistorySummary writes
		// and does not repeat backfillHistorySummaries.
		mockNoOp := &mockStorage{}
		mockNoOp.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockNoOp.On("GetLatestAction", mock.Anything, types.SiteIDNone).Return((*types.Action)(nil), nil).Once()
		mockNoOp.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Weather: []types.Weather{
					{TSDayStart: yesterday},
				},
			},
		}, nil).Once()
		mockNoOp.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, historyStart, yesterday.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockNoOp.On("GetLatestEnergyHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockNoOp.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockNoOp.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, historyStart, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockNoOp.On("GetPriceHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockNoOp.On("InsertAction", mock.Anything, types.SiteIDNone, mock.Anything).Return(nil).Once()

		mockESNoOp := &mockESS{}
		mockESNoOp.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockESNoOp.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockESNoOp.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockESNoOp.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil).Once()
		mockESNoOp.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		mockPNoOp := ess.NewMap()
		mockPNoOp.SetSystem(types.SiteIDNone, mockESNoOp)

		mockUNoOp := &mockUtility{}
		mockUNoOp.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockUNoOp.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil).Once()
		mockUNoOp.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil).Once()
		mockUNoOp.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Once()
		mockUNoOp.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMapNoOp := utility.NewMap(mockNoOp)
		mockUMapNoOp.SetProvider(types.SiteIDNone, mockUNoOp)

		srvNoOp := &Server{
			utilities:  mockUMapNoOp,
			ess:        mockPNoOp,
			storage:    mockNoOp,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		reqNoOp := httptest.NewRequest("GET", "/api/update", nil)
		reqNoOp = reqNoOp.WithContext(context.WithValue(reqNoOp.Context(), siteIDContextKey, types.SiteIDNone))
		wNoOp := httptest.NewRecorder()
		srvNoOp.handleUpdate(wNoOp, reqNoOp)

		assert.Equal(t, http.StatusOK, wNoOp.Code)
		mockNoOp.AssertNotCalled(t, "UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockNoOp.AssertExpectations(t)
	})

	t.Run("SetESSModes Rate Limited In PerformSiteUpdate Emits Warn And Zero Error Logs", func(t *testing.T) {
		now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		yesterday := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, "site1").Return((*types.Action)(nil), nil).Once()
		mockS.On("GetHistorySummaries", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{{TSDayStart: yesterday}},
			},
		}, nil).Once()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, "site1", truncateDay(now), truncateDay(now).AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, "site1", mock.MatchedBy(func(a types.Action) bool {
			return a.Failed && strings.Contains(a.Error, errESSRateLimited.Error())
		})).Return(nil).Once()

		mockES := &mockESS{MockName: "tesla"}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil).Once()
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(false, fmt.Errorf("%w, try again in 30m0s", errESSRateLimited)).Once()

		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil).Once()
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil).Once()
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Once()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			nowFunc:    func() time.Time { return now },
		}

		var errBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&errBuf, &slog.HandlerOptions{Level: slog.LevelError})))

		action, _, err := srv.performSiteUpdate(ctx, "site1", settingsWithVersion{
			Settings: types.Settings{
				ESS:             "tesla",
				MinBatterySOC:   5.0,
				UtilityProvider: "test",
			},
			version: types.CurrentSettingsVersion,
		}, types.Credentials{}, false)
		require.NoError(t, err)
		if assert.NotNil(t, action) {
			assert.True(t, action.Failed)
		}
		assert.Empty(t, errBuf.String(), "performSiteUpdate must emit zero ERROR logs when setESSModes returns errESSRateLimited")
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("PerformSiteUpdate Reserves 30s Control Budget After Slow Authenticate", func(t *testing.T) {
		t.Run("Tight Remaining Budget Caps Parallel Optional Syncs And Completes SetESSModes", func(t *testing.T) {
			now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
			yesterday := truncateDay(now).AddDate(0, 0, -1)

			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, "site-budget-tight").Return((*types.Action)(nil), nil).Once()
			mockS.On("GetHistorySummaries", mock.Anything, "site-budget-tight", mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{Energy: []types.DailyEnergyStats{{TSDayStart: yesterday}}},
			}, nil).Once()
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-budget-tight").Return(time.Time{}, 0, nil).Once()
			mockS.On("UpsertEnergyHistories", mock.Anything, "site-budget-tight", mock.Anything, types.CurrentEnergyStatsVersion).Return(nil).Once()
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-budget-tight").Return(time.Time{}, 0, nil).Once()
			mockS.On("GetLatestWeatherTime", mock.Anything, "site-budget-tight").Return(time.Time{}, time.Time{}, 0, nil).Once()
			mockS.On("GetEnergyHistory", mock.Anything, "site-budget-tight", truncateDay(now), truncateDay(now).AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
			mockS.On("GetWeather", mock.Anything, "site-budget-tight", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
			mockS.On("GetPriceHistory", mock.Anything, "site-budget-tight", mock.Anything, mock.Anything).Return([]types.Price{
				{DollarsPerKWH: 0.15, TSStart: now.Add(-time.Hour), TSEnd: now},
			}, nil).Maybe()
			mockS.On("InsertAction", mock.Anything, "site-budget-tight", mock.MatchedBy(func(a types.Action) bool {
				return !a.Failed
			})).Return(nil).Once()

			var activeSyncs atomic.Int32
			var maxConcurrentSyncs atomic.Int32
			recordSyncConcurrency := func(callCtx context.Context) {
				cur := activeSyncs.Add(1)
				for {
					prev := maxConcurrentSyncs.Load()
					if cur <= prev || maxConcurrentSyncs.CompareAndSwap(prev, cur) {
						break
					}
				}
				defer activeSyncs.Add(-1)
				<-callCtx.Done()
			}

			var setModesRemaining time.Duration
			mockES := &mockESS{MockName: "tesla"}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				// Simulate Authenticate consuming ~59.5s of a 90s siteCtx by sleeping until ~450ms above the 30s reserve.
				callCtx := args.Get(0).(context.Context)
				if dl, ok := callCtx.Deadline(); ok {
					waitDur := time.Until(dl.Add(-30*time.Second - 450*time.Millisecond))
					if waitDur > 0 {
						time.Sleep(waitDur)
					}
				}
			}).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{
				{TSDayStart: truncateDay(now).AddDate(0, 0, -14)},
			}, nil).Once()
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				recordSyncConcurrency(args.Get(0).(context.Context))
			}).Return([]types.DailyEnergyStats{}, context.DeadlineExceeded).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
				BatterySOC:            75,
				BatteryCapacityKWH:    13.5,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
				Timestamp:             now,
			}, nil).Once()
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				callCtx := args.Get(0).(context.Context)
				if dl, ok := callCtx.Deadline(); ok {
					setModesRemaining = time.Until(dl)
				}
			}).Return(true, nil).Once()

			mockP := ess.NewMap()
			mockP.SetSystem("site-budget-tight", mockES)

			var futurePrices []types.Price
			for h := 1; h <= 24; h++ {
				futurePrices = append(futurePrices, types.Price{
					DollarsPerKWH: 0.05,
					TSStart:       now.Add(time.Duration(h) * time.Hour),
					TSEnd:         now.Add(time.Duration(h+1) * time.Hour),
				})
			}
			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				recordSyncConcurrency(args.Get(0).(context.Context))
			}).Return([]types.Price{}, context.DeadlineExceeded).Once()
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.25, TSStart: now, TSEnd: now.Add(time.Hour)}, nil).Once()
			mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil).Once()
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider("site-budget-tight", mockU)

			mockW := &mockWeather{}
			mockW.On("Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				recordSyncConcurrency(args.Get(0).(context.Context))
			}).Return([]types.Weather{}, context.DeadlineExceeded).Once()

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				weather:    mockW,
				controller: controller.NewController(),
				nowFunc:    func() time.Time { return now },
			}

			var warnBuf bytes.Buffer
			warnLogger := slog.New(slog.NewJSONHandler(&warnBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
			baseCtx := log.With(context.Background(), warnLogger)
			// Simulate siteCtx with ~30.6s remaining so Authenticate leaves ~450ms for syncCtx and 30s for control.
			siteCtx, cancel := context.WithTimeout(baseCtx, 30*time.Second+600*time.Millisecond)
			defer cancel()

			action, status, err := srv.performSiteUpdate(siteCtx, "site-budget-tight", settingsWithVersion{
				Settings: types.Settings{
					ESS:             "tesla",
					MinBatterySOC:   10.0,
					UtilityProvider: "test",
					Location:        &types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: "UTC"},
				},
				version: types.CurrentSettingsVersion,
			}, types.Credentials{}, false)
			require.NoError(t, err)
			assert.Empty(t, status)
			if assert.NotNil(t, action) {
				assert.False(t, action.Failed, "setESSModes must succeed without context deadline exceeded")
			}
			assert.Greater(t, setModesRemaining, 28*time.Second, "at least ~30s control budget must remain for setESSModes")
			assert.Equal(t, int32(3), maxConcurrentSyncs.Load(), "energy, price, and weather optional syncs must execute in parallel")

			logLines := strings.Split(strings.TrimSpace(warnBuf.String()), "\n")
			if assert.Len(t, logLines, 3, "parallel optional sync timeouts must emit 3 WARNs and 0 ERROR logs") {
				for _, line := range logLines {
					assert.Contains(t, line, `"level":"WARN"`)
					assert.NotContains(t, line, `"level":"ERROR"`)
				}
			}
			assert.Contains(t, warnBuf.String(), "failed to sync energy history")
			assert.Contains(t, warnBuf.String(), "failed to update price history")
			assert.Contains(t, warnBuf.String(), "failed to update weather history")
			mockS.AssertExpectations(t)
			mockES.AssertExpectations(t)
			mockU.AssertExpectations(t)
			mockW.AssertExpectations(t)
		})

		t.Run("Insufficient Remaining Budget Skips Optional Syncs And Completes SetESSModes", func(t *testing.T) {
			now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
			yesterday := truncateDay(now).AddDate(0, 0, -1)

			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, "site-budget-skip").Return((*types.Action)(nil), nil).Once()
			mockS.On("GetHistorySummaries", mock.Anything, "site-budget-skip", mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{Energy: []types.DailyEnergyStats{{TSDayStart: yesterday}}},
			}, nil).Once()
			mockS.On("GetEnergyHistory", mock.Anything, "site-budget-skip", truncateDay(now), truncateDay(now).AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
			mockS.On("GetWeather", mock.Anything, "site-budget-skip", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
			mockS.On("GetPriceHistory", mock.Anything, "site-budget-skip", mock.Anything, mock.Anything).Return([]types.Price{
				{DollarsPerKWH: 0.15, TSStart: now.Add(-time.Hour), TSEnd: now},
			}, nil).Maybe()
			mockS.On("InsertAction", mock.Anything, "site-budget-skip", mock.MatchedBy(func(a types.Action) bool {
				return !a.Failed
			})).Return(nil).Once()

			mockES := &mockESS{MockName: "tesla"}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
				BatterySOC:            80,
				BatteryCapacityKWH:    13.5,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
				Timestamp:             now,
			}, nil).Once()
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil).Once()

			mockP := ess.NewMap()
			mockP.SetSystem("site-budget-skip", mockES)

			var futurePrices []types.Price
			for h := 1; h <= 24; h++ {
				futurePrices = append(futurePrices, types.Price{
					DollarsPerKWH: 0.05,
					TSStart:       now.Add(time.Duration(h) * time.Hour),
					TSEnd:         now.Add(time.Duration(h+1) * time.Hour),
				})
			}
			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.25, TSStart: now, TSEnd: now.Add(time.Hour)}, nil).Once()
			mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil).Once()
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider("site-budget-skip", mockU)
			mockW := &mockWeather{}

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				weather:    mockW,
				controller: controller.NewController(),
				nowFunc:    func() time.Time { return now },
			}

			var warnBuf bytes.Buffer
			baseCtx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&warnBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
			// Simulate 65s of the 90s siteCtx already consumed by Authenticate retries -> 25s remaining (< 30s reserve).
			siteCtx, cancel := context.WithTimeout(baseCtx, 25*time.Second)
			defer cancel()

			action, _, err := srv.performSiteUpdate(siteCtx, "site-budget-skip", settingsWithVersion{
				Settings: types.Settings{
					ESS:             "tesla",
					MinBatterySOC:   10.0,
					UtilityProvider: "test",
					Location:        &types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: "UTC"},
				},
				version: types.CurrentSettingsVersion,
			}, types.Credentials{}, false)
			require.NoError(t, err)
			if assert.NotNil(t, action) {
				assert.False(t, action.Failed)
			}

			mockES.AssertNotCalled(t, "GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything)
			mockU.AssertNotCalled(t, "GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything)
			mockW.AssertNotCalled(t, "Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

			logLines := strings.Split(strings.TrimSpace(warnBuf.String()), "\n")
			if assert.Len(t, logLines, 1, "must emit 1 WARN and 0 ERROR logs when skipping optional syncs") {
				assert.Contains(t, logLines[0], `"level":"WARN"`)
				assert.Contains(t, logLines[0], "skipping optional history syncs")
			}
			mockS.AssertExpectations(t)
			mockES.AssertExpectations(t)
		})
	})

	t.Run("PerformSiteUpdate Upstream 502 503 And Decode Failures Emit Warn And Zero Error Logs", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Hour)
		yesterday := truncateDay(now).AddDate(0, 0, -1)

		var franklinHistStatus int
		var franklinHistBody string
		var enphaseHistStatus int
		var enphaseHistBody string
		var weatherStatus int
		var weatherBody string

		origTransport := http.DefaultTransport
		t.Cleanup(func() {
			http.DefaultTransport = origTransport
		})

		franklinHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/hes-gateway/terminal/getDeviceInfoV2":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"zoneInfo":        "UTC",
						"totalCap":        15.0,
						"peHwVersionList": []int{20},
					},
				})
			case "/hes-gateway/terminal/getDeviceCompositeInfo":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":     70.0,
							"mode":    20.0,
							"t_amb":   25.0,
							"time":    now.Format("2006-01-02 15:04:05"),
							"version": 1,
						},
					},
				})
			case "/hes-gateway/terminal/tou/getGatewayTouListV2":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"currendId": 20.0,
						"list":      []map[string]any{{"id": 20.0, "workMode": 2, "soc": 20.0, "editSocFlag": true}},
					},
				})
			case "/hes-gateway/terminal/tou/getPowerControlSetting":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
			case "/hes-gateway/common/getPowerCapConfigList":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000},
					},
				})
			case "/api-energy/power/getFhpPowerByDay":
				w.WriteHeader(franklinHistStatus)
				w.Write([]byte(franklinHistBody))
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		})

		enphaseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app-api/123/data.json":
				json.NewEncoder(w).Encode(map[string]any{
					"app":   map[string]any{"timezone": "UTC", "user_id": 555},
					"state": map[string]any{"site_id": 123, "battery_config": map[string]any{"usage": "self-consumption", "battery_backup_percentage": 20}},
				})
			case "/pv/systems/123/today":
				w.Write([]byte(`{"start_date":"2026-07-20","battery_details":{"aggregate_soc":70},"stats":[]}`))
			case "/pv/settings/123/battery_status.json":
				w.Write([]byte(`{"max_capacity":10,"available_power":7.68}`))
			case "/app-api/123/grid_control_check.json":
				w.Write([]byte(`{"gridOutageCheck":false}`))
			case "/app-api/123/get_pel_details":
				w.Write([]byte(`{"pel_type":"Not Configured","export_limit":null}`))
			case "/pv/systems/123/daily_energy":
				w.WriteHeader(enphaseHistStatus)
				w.Write([]byte(enphaseHistBody))
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		})

		weatherTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(weatherStatus)
			w.Write([]byte(weatherBody))
		}))
		defer weatherTS.Close()

		http.DefaultTransport = syncTestRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host {
			case "energy.franklinwh.com":
				rec := httptest.NewRecorder()
				franklinHandler.ServeHTTP(rec, r)
				return rec.Result(), nil
			case "enlighten.enphaseenergy.com":
				rec := httptest.NewRecorder()
				enphaseHandler.ServeHTTP(rec, r)
				return rec.Result(), nil
			default:
				return origTransport.RoundTrip(r)
			}
		})

		forecastURL, err := url.Parse(weatherTS.URL)
		require.NoError(t, err)
		weatherSvc := &weather.OpenMeteo{
			ForecastURL: forecastURL,
			HTTPClient:  weatherTS.Client(),
		}

		runSingleSyncFailureCheck := func(essName string, creds types.Credentials, failWeather bool, expectedWarnSubstring string) {
			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, "site-log-check").Return((*types.Action)(nil), nil).Once()
			mockS.On("GetHistorySummaries", mock.Anything, "site-log-check", mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{Energy: []types.DailyEnergyStats{{TSDayStart: yesterday}}},
			}, nil).Once()
			if failWeather {
				// Skip energy sync by returning current hour
				mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-log-check").Return(now, types.CurrentEnergyStatsVersion, nil).Once()
				mockS.On("GetLatestWeatherTime", mock.Anything, "site-log-check").Return(time.Time{}, time.Time{}, 0, nil).Once()
			} else {
				mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-log-check").Return(yesterday, types.CurrentEnergyStatsVersion, nil).Once()
			}
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-log-check").Return(now.Add(12*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
			mockS.On("GetEnergyHistory", mock.Anything, "site-log-check", truncateDay(now), truncateDay(now).AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
			mockS.On("GetWeather", mock.Anything, "site-log-check", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
			mockS.On("GetPriceHistory", mock.Anything, "site-log-check", mock.Anything, mock.Anything).Return([]types.Price{
				{DollarsPerKWH: 0.15, TSStart: now.Add(-time.Hour), TSEnd: now},
			}, nil).Maybe()
			mockS.On("InsertAction", mock.Anything, "site-log-check", mock.Anything).Return(nil).Maybe()

			var futurePrices []types.Price
			for h := 1; h <= 24; h++ {
				futurePrices = append(futurePrices, types.Price{
					DollarsPerKWH: 0.15,
					TSStart:       now.Add(time.Duration(h) * time.Hour),
					TSEnd:         now.Add(time.Duration(h+1) * time.Hour),
				})
			}
			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now, TSEnd: now.Add(time.Hour)}, nil).Once()
			mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil).Once()
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider("site-log-check", mockU)

			srv := &Server{
				utilities:         mockUMap,
				ess:               ess.NewMap(),
				storage:           mockS,
				weather:           weatherSvc,
				controller:        controller.NewController(),
				nowFunc:           func() time.Time { return now },
				weatherRetryDelay: time.Millisecond,
			}

			var logBuf bytes.Buffer
			ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))

			settings := types.Settings{
				ESS:             essName,
				Pause:           true,
				DryRun:          true,
				MinBatterySOC:   20.0,
				UtilityProvider: "test",
			}
			if failWeather {
				settings.Location = &types.SiteLocation{Latitude: 34.07, Longitude: -118.40, CountryCode: "US", TimeZone: "UTC"}
			}

			_, _, runErr := srv.performSiteUpdate(ctx, "site-log-check", settingsWithVersion{
				Settings: settings,
				version:  types.CurrentSettingsVersion,
			}, creds, false)
			require.NoError(t, runErr)

			lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
			if assert.NotEmpty(t, lines, "expected WARN logs and 0 ERROR logs") {
				for _, line := range lines {
					assert.Contains(t, line, `"level":"WARN"`)
					assert.NotContains(t, line, `"level":"ERROR"`)
				}
				assert.Contains(t, logBuf.String(), expectedWarnSubstring)
			}
		}

		franklinCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:    "u@example.com",
				MD5Password: "md5",
				GatewayID:   "gw-1",
				Token:       "valid-tok",
			},
		}
		enphaseCreds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "u@example.com",
				SessionID:    "sess-1",
				ManagerToken: "mgr-1",
				SystemID:     123,
				UserID:       555,
			},
		}

		// Franklin.GetEnergyHistory: 502, 503, JSON decode failure
		franklinHistStatus, franklinHistBody = http.StatusBadGateway, "<html>502</html>"
		runSingleSyncFailureCheck("franklin", franklinCreds, false, "failed to sync energy history")
		franklinHistStatus, franklinHistBody = http.StatusServiceUnavailable, "503 unavailable"
		runSingleSyncFailureCheck("franklin", franklinCreds, false, "failed to sync energy history")
		franklinHistStatus, franklinHistBody = http.StatusOK, `{"code":200,"success":true,"result":{bad`
		runSingleSyncFailureCheck("franklin", franklinCreds, false, "failed to sync energy history")

		// Enphase.GetEnergyHistory: 502, 503, JSON decode failure
		enphaseHistStatus, enphaseHistBody = http.StatusBadGateway, "<html>502</html>"
		runSingleSyncFailureCheck("enphase", enphaseCreds, false, "failed to sync energy history")
		enphaseHistStatus, enphaseHistBody = http.StatusServiceUnavailable, "503 unavailable"
		runSingleSyncFailureCheck("enphase", enphaseCreds, false, "failed to sync energy history")
		enphaseHistStatus, enphaseHistBody = http.StatusOK, `{bad-json`
		runSingleSyncFailureCheck("enphase", enphaseCreds, false, "failed to sync energy history")

		// OpenMeteo.Forecast: 502, 503, JSON decode failure
		weatherStatus, weatherBody = http.StatusBadGateway, "<html>502</html>"
		runSingleSyncFailureCheck("franklin", franklinCreds, true, "failed to update weather history")
		weatherStatus, weatherBody = http.StatusServiceUnavailable, `{"error":true,"reason":"overloaded"}`
		runSingleSyncFailureCheck("franklin", franklinCreds, true, "failed to update weather history")
		weatherStatus, weatherBody = http.StatusOK, `{"hourly":{bad`
		runSingleSyncFailureCheck("franklin", franklinCreds, true, "failed to update weather history")
	})
}

func TestHandleUpdateSites(t *testing.T) {
	mockU := &mockUtility{}
	mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

	mockS := &mockStorage{}
	mockS.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
		{
			Energy: []types.DailyEnergyStats{
				{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
			},
		},
	}, nil)
	mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
		"site1": {ESS: "mock", UtilityProvider: "test", Release: "production"},
		"site3": {ESS: "mock", UtilityProvider: "test", Release: "production"},
	}, map[string]int{
		"site1": types.CurrentSettingsVersion,
		"site3": types.CurrentSettingsVersion,
	}, map[string]time.Time{}, nil)
	mockS.On("ListSitesSettings", mock.Anything, "staging", mock.Anything).Return(map[string]types.Settings{
		"site2": {ESS: "mock", UtilityProvider: "test", Release: "staging"},
	}, map[string]int{
		"site2": types.CurrentSettingsVersion,
	}, map[string]time.Time{}, nil)

	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Now().Add(-1*time.Hour), types.CurrentPriceHistoryVersion, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem("site1", mockES)
	mockP.SetSystem("site2", mockES)
	mockP.SetSystem("site3", mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider("site1", mockU)
	mockUMap.SetProvider("site2", mockU)
	mockUMap.SetProvider("site3", mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
		release:    "production",
	}

	req := httptest.NewRequest("POST", "/api/updateSites", nil)
	w := httptest.NewRecorder()

	srv.handleUpdateSites(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var results map[string]string
	err := json.NewDecoder(w.Body).Decode(&results)
	require.NoError(t, err)

	assert.Equal(t, "success", results["site1"])
	assert.NotContains(t, results, "site2")
	assert.Equal(t, "success", results["site3"])

	// Verify caching: GetCurrentPrice should be called twice (once for site1 and once for site3)
	mockU.AssertNumberOfCalls(t, "GetCurrentPrice", 2)

	t.Run("Staging Release", func(t *testing.T) {
		srv.release = "staging"
		// Reset mocks if necessary, but here we just want to verify site2 is picked up
		mockP.SetSystem("site2", mockES)

		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)

		assert.NotContains(t, results, "site1")
		assert.Equal(t, "success", results["site2"])
		assert.NotContains(t, results, "site3")
	})

	t.Run("No ESS Configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-no-ess": {
				Release: "production",
				ESS:     "", // Empty ESS
			},
		}, map[string]int{
			"site-no-ess": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: no ESS configured", results["site-no-ess"])
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		updateEmail := "updater@example.com"
		token := generateTestTokenWithAudience(t, oidcSrv.URL, priv, updateEmail, "updater", "update-audience")
		webAudSchedEmailToken := generateTestTokenWithAudience(t, oidcSrv.URL, priv, updateEmail, "updater", "test-audience")

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site1": {
				Release:         "production",
				ESS:             "mock",
				UtilityProvider: "test",
			},
		}, map[string]int{
			"site1": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)

		srv := &Server{
			storage:             mockS,
			utilities:           mockUMap,
			ess:                 mockP,
			controller:          controller.NewController(),
			release:             "production",
			updateSpecificEmail: updateEmail,
			updateOIDCVerifier:  provider.Verifier(&oidc.Config{ClientID: "update-audience"}).Verify,
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
		}

		t.Run("Valid Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("Mismatch Token", func(t *testing.T) {
			badToken := generateTestTokenWithAudience(t, oidcSrv.URL, priv, "wrong@example.com", "wrong", "update-audience")
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+badToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Web Audience Token Rejected", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+webAudSchedEmailToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Unverified Email Token Rejected", func(t *testing.T) {
			unverifiedClaims := fmt.Sprintf(`{
				"iss": "%s",
				"aud": "update-audience",
				"sub": "updater",
				"email": "%s",
				"email_verified": false,
				"exp": %d
			}`, oidcSrv.URL, updateEmail, time.Now().Add(time.Hour).Unix())
			unverifiedToken := oidctest.SignIDToken(priv, "my-key-id", "RS256", unverifiedClaims)

			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+unverifiedToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Missing Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Update Specific Email Required Even With Cookies", func(t *testing.T) {
			savedEmail := srv.updateSpecificEmail
			savedSessionKey := srv.sessionEncryptionKey
			srv.sessionEncryptionKey = "12345678901234567890123456789012"
			defer func() {
				srv.updateSpecificEmail = savedEmail
				srv.sessionEncryptionKey = savedSessionKey
			}()

			sessionToken, err := srv.createSessionToken("google:owner-sub", "owner@example.com", "owner-secret", time.Hour)
			require.NoError(t, err)
			ownerCookie := &http.Cookie{Name: sessionTokenCookie, Value: sessionToken}
			legacyCookie := &http.Cookie{Name: authTokenCookie, Value: generateTestToken(t, oidcSrv.URL, priv, "owner@example.com", "owner-sub")}

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))

			// With updateSpecificEmail set: cookies must not fallback
			reqCookieSet := httptest.NewRequest("POST", "/api/updateSites", nil)
			reqCookieSet.AddCookie(ownerCookie)
			wCookieSet := httptest.NewRecorder()
			handler.ServeHTTP(wCookieSet, reqCookieSet)
			assert.Equal(t, http.StatusUnauthorized, wCookieSet.Code)

			// With updateSpecificEmail unset: /api/updateSites must still reject cookies and unauthenticated calls
			srv.updateSpecificEmail = ""
			reqOwnerUnset := httptest.NewRequest("POST", "/api/updateSites", nil)
			reqOwnerUnset.AddCookie(ownerCookie)
			wOwnerUnset := httptest.NewRecorder()
			handler.ServeHTTP(wOwnerUnset, reqOwnerUnset)
			assert.Equal(t, http.StatusUnauthorized, wOwnerUnset.Code)

			reqLegacyUnset := httptest.NewRequest("POST", "/api/updateSites", nil)
			reqLegacyUnset.AddCookie(legacyCookie)
			wLegacyUnset := httptest.NewRecorder()
			handler.ServeHTTP(wLegacyUnset, reqLegacyUnset)
			assert.Equal(t, http.StatusUnauthorized, wLegacyUnset.Code)

			reqNoAuthUnset := httptest.NewRequest("POST", "/api/updateSites", nil)
			wNoAuthUnset := httptest.NewRecorder()
			handler.ServeHTTP(wNoAuthUnset, reqNoAuthUnset)
			assert.Equal(t, http.StatusUnauthorized, wNoAuthUnset.Code)

			mockS.AssertNotCalled(t, "GetUser", mock.Anything, mock.Anything)
			mockS.AssertNotCalled(t, "GetSite", mock.Anything, mock.Anything)
		})
	})

	t.Run("ESS Rate Limited", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-rate-limited": {
				Release: "production",
				ESS:     "mock",
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         time.Now(),
				},
			},
		}, map[string]int{
			"site-rate-limited": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: ESS rate limited", results["site-rate-limited"])
	})

	t.Run("ESS Write Rate Limited", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-write-rate-limited": {
				Release: "production",
				ESS:     "mock",
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 2,
					LastAttempt:            time.Now(),
				},
				UtilityProvider: "test",
			},
		}, map[string]int{
			"site-write-rate-limited": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-write-rate-limited").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-write-rate-limited").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site-write-rate-limited", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, "site-write-rate-limited", mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		// Simulate an OAuth token refresh (updated=true) to verify it does NOT wipe ConsecutiveSetFailures
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "refreshed"},
		}, true, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockS.On("SetSettings", mock.Anything, "site-write-rate-limited", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 2
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem("site-write-rate-limited", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site-write-rate-limited", mockU)

		srv := &Server{
			storage:       mockS,
			utilities:     mockUMap,
			ess:           mockP,
			release:       "production",
			bypassAuth:    true,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: ESS rate limited", results["site-write-rate-limited"])
		mockS.AssertExpectations(t)
	})

	t.Run("Cron Partitioning", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()

		var capturedGroups []int
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Run(func(args mock.Arguments) {
			capturedGroups = args.Get(2).([]int)
		}).Return(map[string]types.Settings{}, map[string]int{}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
			nowFunc:    func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
		}

		t.Run("Valid kind=tou idx=0 total=2 sets groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?kind=tou&idx=0&total=2", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if assert.Len(t, capturedGroups, 6) {
				expected, err := getCronGroups(srv.now(), "tou", 0, 2)
				require.NoError(t, err)
				assert.Equal(t, expected, capturedGroups)
			}
		})

		t.Run("Valid kind=tou idx=1 total=2 sets groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?kind=tou&idx=1&total=2", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if assert.Len(t, capturedGroups, 6) {
				expected, err := getCronGroups(srv.now(), "tou", 1, 2)
				require.NoError(t, err)
				assert.Equal(t, expected, capturedGroups)
			}
		})

		t.Run("Valid kind=comed idx=0 total=1 sets groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?kind=comed&idx=0&total=1", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, []int{13, 14, 15, 16}, capturedGroups)
		})

		t.Run("Valid kind=all sets all 16 groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?kind=all", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, capturedGroups, 16)
			for i := 1; i <= 16; i++ {
				assert.Contains(t, capturedGroups, i)
			}
		})

		t.Run("Legacy cron=1 and cron=2 set TOU groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?cron=1", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, capturedGroups, 6)

			req2 := httptest.NewRequest("POST", "/api/updateSites?cron=2", nil)
			w2 := httptest.NewRecorder()
			srv.handleUpdateSites(w2, req2)

			assert.Equal(t, http.StatusOK, w2.Code)
			assert.Len(t, capturedGroups, 6)
		})

		t.Run("Invalid parameters return bad request", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?kind=invalid&idx=0&total=1", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var resp map[string]string
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], "invalid kind")
		})
	})

	t.Run("Performance 100 Sites Parallel 3", func(t *testing.T) {
		const numSites = 100
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

		futurePrices := make([]types.Price, 24)
		for i := 0; i < 24; i++ {
			tStep := now.Add(time.Duration(i) * time.Hour)
			rate := 0.08
			if i >= 14 && i <= 19 {
				rate = 0.45
			}
			futurePrices[i] = types.Price{
				TSStart:              tStep,
				TSEnd:                tStep.Add(time.Hour),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.04,
			}
		}

		settingsMap := make(map[string]types.Settings, numSites)
		versionsMap := make(map[string]int, numSites)
		updatedTimesMap := make(map[string]time.Time, numSites)
		mockUMap := utility.NewMap(&mockStorage{})
		mockP := ess.NewMap()

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(futurePrices[0], nil)
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50.0,
			HomeKW:             1.5,
			SolarKW:            2.0,
		}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

		for i := 0; i < numSites; i++ {
			siteID := fmt.Sprintf("site-perf-%03d", i+1)
			settingsMap[siteID] = types.Settings{
				Release:             "production",
				ESS:                 "mock",
				UtilityProvider:     "test",
				MinBatterySOC:       20.0,
				GridChargeBatteries: true,
			}
			versionsMap[siteID] = types.CurrentSettingsVersion
			updatedTimesMap[siteID] = now
			mockUMap.SetProvider(siteID, mockU)
			mockP.SetSystem(siteID, mockES)
		}

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(settingsMap, versionsMap, updatedTimesMap, nil)
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()

		// Run GC twice before the benchmark: the first cycle collects unreachable heap objects from
		// prior tests and moves sync.Pool entries to the victim cache, and the second cycle clears
		// the victim cache so baseline allocations are isolated.
		runtime.GC()
		runtime.GC()

		start := time.Now()
		srv.handleUpdateSites(w, req)
		elapsed := time.Since(start)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var results map[string]string
		err := json.NewDecoder(resp.Body).Decode(&results)
		require.NoError(t, err)
		assert.Len(t, results, numSites)
		for siteID, status := range results {
			assert.Equal(t, "success", status, "site %s should have succeeded", siteID)
		}

		// Ensure that running 100 sites with 3 parallel workers finishes well within Cloud Run's 90s deadline
		assert.Less(t, elapsed, 30*time.Second, "100 sites with concurrency 3 took too long: %v", elapsed)

		// Verify memory usage stays well below Cloud Run's 128 MB container limit
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		assert.Less(t, mem.HeapAlloc, uint64(64*1024*1024), "HeapAlloc exceeded 64 MB: %d bytes", mem.HeapAlloc)
	})

	t.Run("Panic Isolation", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-panic":   {ESS: "mock", UtilityProvider: "test", Release: "production"},
			"site-healthy": {ESS: "mock", UtilityProvider: "test", Release: "production"},
		}, map[string]int{
			"site-panic":   types.CurrentSettingsVersion,
			"site-healthy": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		mockS.On("GetLatestAction", mock.Anything, "site-panic").Run(func(args mock.Arguments) {
			panic("unexpected nil pointer in site-panic")
		}).Return((*types.Action)(nil), nil)
		mockS.On("GetLatestAction", mock.Anything, "site-healthy").Return((*types.Action)(nil), nil).Once()
		mockS.On("GetHistorySummaries", mock.Anything, "site-healthy", mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-healthy").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-healthy").Return(now.Add(-1*time.Hour), types.CurrentPriceHistoryVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site-healthy", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, "site-healthy", mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, "site-healthy", mock.Anything).Return(nil).Once()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem("site-panic", mockES)
		mockP.SetSystem("site-healthy", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site-panic", mockU)
		mockUMap.SetProvider("site-healthy", mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)

		assert.Equal(t, "failed: panic: unexpected nil pointer in site-panic", results["site-panic"])
		assert.Equal(t, "success", results["site-healthy"])
		mockS.AssertExpectations(t)
	})

	t.Run("Skips Recently Updated Site And Reuses LatestAction", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-recent": {ESS: "mock", UtilityProvider: "test", Release: "production"},
			"site-older":  {ESS: "mock", UtilityProvider: "test", Release: "production"},
		}, map[string]int{
			"site-recent": types.CurrentSettingsVersion,
			"site-older":  types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		// site-recent was updated 2 minutes ago (< 5 minutes) -> should be skipped immediately
		mockS.On("GetLatestAction", mock.Anything, "site-recent").Return(&types.Action{
			Timestamp:   now.Add(-2 * time.Minute),
			BatteryMode: types.BatteryModeLoad,
		}, nil).Once()

		// site-older was updated 6 minutes ago (>= 5 minutes) -> should run and only call GetLatestAction once
		mockS.On("GetLatestAction", mock.Anything, "site-older").Return(&types.Action{
			Timestamp:   now.Add(-6 * time.Minute),
			BatteryMode: types.BatteryModeLoad,
		}, nil).Once()

		mockS.On("GetHistorySummaries", mock.Anything, "site-older", mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(now).AddDate(0, 0, -1)},
				},
			},
		}, nil).Once()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-older").Return(time.Time{}, 0, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-older").Return(now.Add(-1*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, "site-older", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetPriceHistory", mock.Anything, "site-older", mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("InsertAction", mock.Anything, "site-older", mock.Anything).Return(nil).Once()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80, Timestamp: now}, nil).Once()
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem("site-recent", mockES)
		mockP.SetSystem("site-older", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: now}, nil).Once()
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: now.Add(time.Hour)}}, nil).Once()
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Once()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site-recent", mockU)
		mockUMap.SetProvider("site-older", mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)

		assert.Equal(t, "skipped: recently updated", results["site-recent"])
		assert.Equal(t, "success", results["site-older"])
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
		mockU.AssertExpectations(t)
	})

	t.Run("Permanent auth failure in handleUpdateSites emits WARN and zero ERROR logs", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
		mockS.On("ListSitesSettings", mock.Anything, "", mock.Anything).Return(map[string]types.Settings{
			"site1": {
				ESS:             "franklin",
				UtilityProvider: "test",
				UpdateGroup:     1,
			},
		}, map[string]int{
			"site1": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)
		mockS.On("GetLatestAction", mock.Anything, "site1").Return((*types.Action)(nil), nil)
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

		mockES := &mockESS{MockName: "franklin"}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("wrong password: %w", ess.ErrUnauthorized))
		essMap := ess.NewMap()
		essMap.SetSystem("site1", mockES)

		srv := &Server{
			storage:    mockS,
			ess:        essMap,
			utilities:  utility.NewMap(mockS),
			bypassAuth: true,
		}

		var errBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&errBuf, &slog.HandlerOptions{Level: slog.LevelError})))
		req := httptest.NewRequest("POST", "/api/updateSites", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdateSites(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.Empty(t, errBuf.String(), "handleUpdateSites must not emit ERROR logs on permanent ESS auth rejection")
	})

	t.Run("Emits site_update_heartbeat structured log on success", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("ListSitesSettings", mock.Anything, "", mock.Anything).Return(
			map[string]types.Settings{},
			map[string]int{},
			map[string]time.Time{},
			nil,
		)

		srv := &Server{
			storage:    mockS,
			ess:        ess.NewMap(),
			utilities:  utility.NewMap(mockS),
			bypassAuth: true,
		}

		var logBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
		req := httptest.NewRequest("POST", "/api/updateSites", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdateSites(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var found bool
		for _, line := range strings.Split(strings.TrimSpace(logBuf.String()), "\n") {
			if line == "" {
				continue
			}
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &entry))
			if entry["event"] == "site_update_heartbeat" {
				found = true
				assert.Equal(t, "site update heartbeat", entry["msg"])
				assert.Equal(t, float64(0), entry["siteCount"])
			}
		}
		assert.True(t, found, "expected structured JSON log with event=site_update_heartbeat")
		mockS.AssertExpectations(t)
	})

	t.Run("Does not emit site_update_heartbeat on ListSitesSettings error", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("ListSitesSettings", mock.Anything, "", mock.Anything).Return(
			(map[string]types.Settings)(nil),
			(map[string]int)(nil),
			(map[string]time.Time)(nil),
			fmt.Errorf("firestore unavailable"),
		)

		srv := &Server{
			storage:    mockS,
			ess:        ess.NewMap(),
			utilities:  utility.NewMap(mockS),
			bypassAuth: true,
		}

		var logBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
		req := httptest.NewRequest("POST", "/api/updateSites", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdateSites(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		assert.NotContains(t, logBuf.String(), "site_update_heartbeat")
		mockS.AssertExpectations(t)
	})
}

// Helpers for Recording Mocks
type RecordingMockESS struct {
	mockESS
	status        types.SystemStatus
	setModes      bool
	setBatMode    types.BatteryMode
	setSolMode    types.SolarMode
	setOpts       types.ModesOptions
	GetStatusFunc func(ctx context.Context) (types.SystemStatus, error)
	SetModesFunc  func(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) error
}

func (m *RecordingMockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx)
	}
	return m.status, nil
}

func (m *RecordingMockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) error {
	if m.SetModesFunc != nil {
		return m.SetModesFunc(ctx, bat, sol, opts)
	}
	m.setModes = true
	m.setBatMode = bat
	m.setSolMode = sol
	m.setOpts = opts
	return nil
}

type RecordingMockStorage struct {
	mockStorage
	insertedAction   *types.Action
	InsertActionFunc func(ctx context.Context, action types.Action) error
}

func (m *RecordingMockStorage) InsertAction(ctx context.Context, action types.Action) error {
	if m.InsertActionFunc != nil {
		return m.InsertActionFunc(ctx, action)
	}
	m.insertedAction = &action
	return nil
}

func TestUpdateSitePrices(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockU := &mockUtility{}
		// Expect GetConfirmedPrices to be called for ~8 days
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{
			{DollarsPerKWH: 0.1, TSStart: time.Now()},
		}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertPrices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Verify that it started from 8 days ago
		now := time.Now()
		eightDaysAgo := now.AddDate(0, 0, -8)
		expectedStart := time.Date(eightDaysAgo.Year(), eightDaysAgo.Month(), eightDaysAgo.Day(), 0, 0, 0, 0, eightDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)

		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Incremental Update", func(t *testing.T) {
		mockU := &mockUtility{}
		lastTime := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Hour)

		// Expect GetConfirmedPrices to start from lastTime
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		expectedEnd := time.Now().Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("No Future Update", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is now (or very close), we might still get a call for the current partial hour/day
		lastTime := time.Now().Truncate(time.Hour)

		// Allow calls for past/present/future within the day
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return !start.After(time.Now())
		}), mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Ensure strictly no calls with start time in the future
		mockU.AssertNotCalled(t, "GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return start.After(time.Now())
		}), mock.Anything)
	})

	t.Run("Version Mismatch Backfill", func(t *testing.T) {
		mockU := &mockUtility{}
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentPriceHistoryVersion - 1

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Should have triggered backfill from 8 days ago, not 1 hour ago
		now := time.Now()
		eightDaysAgo := now.AddDate(0, 0, -8)
		expectedStart := time.Date(eightDaysAgo.Year(), eightDaysAgo.Month(), eightDaysAgo.Day(), 0, 0, 0, 0, eightDaysAgo.Location())

		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Refresh Now", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is in the future, normally we wouldn't fetch.
		// But with refreshNow=true, we should fetch starting from now.
		lastTime := time.Now().Add(5 * time.Hour).Truncate(time.Hour)

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, true)
		require.NoError(t, err)

		// Should have started from now, not lastTime
		assert.WithinDuration(t, time.Now(), startTime, time.Second)
		assert.WithinDuration(t, time.Now().Add(12*time.Hour), endTime, time.Second)
	})
}

func TestUpdateEnergyHistory(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		// Expect 3 chronological 5-day windows across the 14-day backfill
		var windowStarts []time.Time
		var windowEnds []time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			windowStarts = append(windowStarts, args.Get(1).(time.Time))
			windowEnds = append(windowEnds, args.Get(2).(time.Time))
		}).Return([]types.DailyEnergyStats{{}}, nil).Times(3)

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
		expectedStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
		if assert.Len(t, windowStarts, 3) && assert.Len(t, windowEnds, 3) {
			assert.True(t, expectedStart.Equal(windowStarts[0]), "Expected first window start %v, got %v", expectedStart, windowStarts[0])
			assert.True(t, expectedStart.AddDate(0, 0, 5).Add(-time.Hour).Equal(windowEnds[0]))
			assert.True(t, expectedStart.AddDate(0, 0, 5).Equal(windowStarts[1]))
			assert.True(t, expectedStart.AddDate(0, 0, 10).Add(-time.Hour).Equal(windowEnds[1]))
			assert.True(t, expectedStart.AddDate(0, 0, 10).Equal(windowStarts[2]))
			assert.WithinDuration(t, now.Truncate(time.Hour), windowEnds[2], time.Second)
		}
		mockS.AssertNumberOfCalls(t, "UpsertEnergyHistories", 3)
	})

	t.Run("Incremental Update - Recent History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		assert.WithinDuration(t, time.Now().Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Incremental Update - Last Hour", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		lastTime := time.Now().Add(-1 * time.Hour).Truncate(time.Hour)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		assert.WithinDuration(t, time.Now().Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Version Mismatch - Partial Backfill", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentEnergyStatsVersion - 1
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var windowStarts []time.Time
		var windowEnds []time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			windowStarts = append(windowStarts, args.Get(1).(time.Time))
			windowEnds = append(windowEnds, args.Get(2).(time.Time))
		}).Return([]types.DailyEnergyStats{{}}, nil).Times(3)

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
		expectedStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
		if assert.Len(t, windowStarts, 3) && assert.Len(t, windowEnds, 3) {
			assert.True(t, expectedStart.Equal(windowStarts[0]), "Expected first window start %v, got %v", expectedStart, windowStarts[0])
			assert.True(t, expectedStart.AddDate(0, 0, 5).Add(-time.Hour).Equal(windowEnds[0]))
			assert.True(t, expectedStart.AddDate(0, 0, 5).Equal(windowStarts[1]))
			assert.True(t, expectedStart.AddDate(0, 0, 10).Add(-time.Hour).Equal(windowEnds[1]))
			assert.True(t, expectedStart.AddDate(0, 0, 10).Equal(windowStarts[2]))
			assert.WithinDuration(t, now.Truncate(time.Hour), windowEnds[2], time.Second)
		}
		mockS.AssertNumberOfCalls(t, "UpsertEnergyHistories", 3)
	})

	t.Run("Future Time - No Update", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		lastTime := time.Now().Add(1 * time.Hour) // Future
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)

		mockES := &mockESS{}
		// Should NOT call GetEnergyHistory

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		mockES.AssertNotCalled(t, "GetEnergyHistory")
	})

	t.Run("Future Price Fallback", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockES := &mockESS{}
		mockU := &mockUtility{}

		now := time.Now().Truncate(time.Hour)
		pastPrice := types.Price{
			TSStart:       now.Add(-12 * time.Hour),
			TSEnd:         now.Add(-11 * time.Hour),
			DollarsPerKWH: 0.25,
		}

		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{pastPrice}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil) // Trigger fallback
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)
		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
		}

		_, _, err := srv.performSiteUpdate(context.Background(), "site1", settingsWithVersion{Settings: types.Settings{UtilityProvider: "test"}}, types.Credentials{}, false)
		assert.NoError(t, err)

		mockS.AssertCalled(t, "GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything)
	})

	t.Run("Windowed Backfill Retains Partial Progress And Resumes From Latest Stored Hour", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 10, 30, 0, 0, time.UTC)

		storedByDay := make(map[string]types.DailyEnergyStats)
		latestStoredTime := func() (time.Time, int) {
			if len(storedByDay) == 0 {
				return time.Time{}, 0
			}
			var latestDay time.Time
			var latest types.DailyEnergyStats
			for _, d := range storedByDay {
				if latestDay.IsZero() || d.TSDayStart.After(latestDay) {
					latestDay = d.TSDayStart
					latest = d
				}
			}
			if len(latest.Hourly) > 0 {
				maxH := latest.Hourly[0].TSHourStart
				for _, h := range latest.Hourly[1:] {
					if h.TSHourStart.After(maxH) {
						maxH = h.TSHourStart
					}
				}
				return maxH, types.CurrentEnergyStatsVersion
			}
			return latest.TSDayStart, types.CurrentEnergyStatsVersion
		}

		buildDailyRange := func(start, end time.Time) []types.DailyEnergyStats {
			var out []types.DailyEnergyStats
			startDay := truncateDay(start)
			endDay := truncateDay(end)
			for d := startDay; !d.After(endDay); d = d.AddDate(0, 0, 1) {
				lastH := 23
				if d.Equal(endDay) {
					lastH = end.Hour()
				}
				var hourly []types.EnergyStats
				for h := 0; h <= lastH; h++ {
					hourly = append(hourly, types.EnergyStats{
						TSHourStart: d.Add(time.Duration(h) * time.Hour),
						SolarKWH:    1.0,
					})
				}
				out = append(out, types.DailyEnergyStats{
					TSDayStart:   d,
					TimeLocation: "UTC",
					Hourly:       hourly,
				})
			}
			return out
		}

		mockS := &mockStorage{}
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-resume").Return(time.Time{}, 0, nil).Once()
		mockS.On("UpsertEnergyHistories", mock.Anything, "site-resume", mock.Anything, types.CurrentEnergyStatsVersion).Run(func(args mock.Arguments) {
			batch := args.Get(2).([]types.DailyEnergyStats)
			for _, d := range batch {
				storedByDay[d.TSDayStart.Format("2006-01-02")] = d
			}
		}).Return(nil)

		// Cycle 1: Window 1 succeeds and saves 5 days (2026-07-06..2026-07-10); Window 2 times out; Window 3 does not run.
		win1Start := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
		win1End := time.Date(2026, 7, 10, 23, 0, 0, 0, time.UTC)
		mockES1 := &mockESS{}
		var cycle1Windows [][2]time.Time
		mockES1.On("GetEnergyHistory", mock.Anything, win1Start, win1End).Run(func(args mock.Arguments) {
			cycle1Windows = append(cycle1Windows, [2]time.Time{args.Get(1).(time.Time), args.Get(2).(time.Time)})
		}).Return(buildDailyRange(win1Start, win1End), nil).Once()
		mockES1.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			cycle1Windows = append(cycle1Windows, [2]time.Time{args.Get(1).(time.Time), args.Get(2).(time.Time)})
		}).Return([]types.DailyEnergyStats{}, context.DeadlineExceeded).Once()

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return now },
		}

		err := srv.updateEnergyHistory(context.Background(), "site-resume", mockES1)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Len(t, cycle1Windows, 2)
		assert.Len(t, storedByDay, 5, "first completed 5-day window must be retained in storage")
		cursorAfterCycle1, verAfterCycle1 := latestStoredTime()
		assert.Equal(t, time.Date(2026, 7, 10, 23, 0, 0, 0, time.UTC), cursorAfterCycle1)
		assert.Equal(t, types.CurrentEnergyStatsVersion, verAfterCycle1)

		// Cycle 2: Resumes from cursorAfterCycle1 (2026-07-10 23:00) and completes remaining windows through 2026-07-20 10:00.
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-resume").Return(cursorAfterCycle1, verAfterCycle1, nil).Once()
		c2w1Start := time.Date(2026, 7, 10, 23, 0, 0, 0, time.UTC)
		c2w1End := time.Date(2026, 7, 14, 23, 0, 0, 0, time.UTC)
		c2w2Start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
		c2w2End := time.Date(2026, 7, 19, 23, 0, 0, 0, time.UTC)
		c2w3Start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
		c2w3End := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

		mockES2 := &mockESS{}
		var cycle2Windows [][2]time.Time
		mockES2.On("GetEnergyHistory", mock.Anything, c2w1Start, c2w1End).Run(func(args mock.Arguments) {
			cycle2Windows = append(cycle2Windows, [2]time.Time{args.Get(1).(time.Time), args.Get(2).(time.Time)})
		}).Return(buildDailyRange(c2w1Start, c2w1End), nil).Once()
		mockES2.On("GetEnergyHistory", mock.Anything, c2w2Start, c2w2End).Run(func(args mock.Arguments) {
			cycle2Windows = append(cycle2Windows, [2]time.Time{args.Get(1).(time.Time), args.Get(2).(time.Time)})
		}).Return(buildDailyRange(c2w2Start, c2w2End), nil).Once()
		mockES2.On("GetEnergyHistory", mock.Anything, c2w3Start, c2w3End).Run(func(args mock.Arguments) {
			cycle2Windows = append(cycle2Windows, [2]time.Time{args.Get(1).(time.Time), args.Get(2).(time.Time)})
		}).Return(buildDailyRange(c2w3Start, c2w3End), nil).Once()

		err = srv.updateEnergyHistory(context.Background(), "site-resume", mockES2)
		require.NoError(t, err)
		if assert.Len(t, cycle2Windows, 3) {
			assert.Equal(t, time.Date(2026, 7, 10, 23, 0, 0, 0, time.UTC), cycle2Windows[0][0])
			assert.Equal(t, time.Date(2026, 7, 14, 23, 0, 0, 0, time.UTC), cycle2Windows[0][1])
			assert.Equal(t, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), cycle2Windows[1][0])
			assert.Equal(t, time.Date(2026, 7, 19, 23, 0, 0, 0, time.UTC), cycle2Windows[1][1])
			assert.Equal(t, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), cycle2Windows[2][0])
			assert.Equal(t, time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC), cycle2Windows[2][1])
		}
		assert.Len(t, storedByDay, 15, "all 15 calendar days (2026-07-06..2026-07-20) must be stored across the two cycles")
	})
}

type syncTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f syncTestRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestUpdateWeatherHistory(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	sl := types.SiteLocation{
		PostalCode:  "90210",
		CountryCode: "US",
		Latitude:    34.09,
		Longitude:   -118.4,
		TimeZone:    loc.String(),
	}

	// Fixed test time: 2026-06-05 10:00:00 UTC (3:00 AM PDT)
	testTime := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)

	t.Run("Update Missing History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Cold start: expected fetch range is 14 days ago to end of tomorrow
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fourteenDaysAgoMidnight := todayMidnight.AddDate(0, 0, -14)
		endOfTomorrow := todayMidnight.AddDate(0, 0, 2)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fourteenDaysAgoMidnight)
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(endOfTomorrow)
		})).Return([]types.Weather{{TSDayStart: now}}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Backfill on Version Mismatch", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// Old version history exists
		now := testTime.In(loc)
		lastTime := now.AddDate(0, 0, -1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, 1, nil) // Version 1 < 3
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Should still start from 14 days ago due to version mismatch
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fourteenDaysAgoMidnight := todayMidnight.AddDate(0, 0, -14)
		endOfTomorrow := todayMidnight.AddDate(0, 0, 2)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fourteenDaysAgoMidnight)
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(endOfTomorrow)
		})).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
	})

	t.Run("No Update If Already Recent", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// fetchEnd = tomorrowMidnight + 1 day = todayMidnight + 2 days
		fetchEnd := todayMidnight.AddDate(0, 0, 2)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(fetchEnd, testTime, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Resume From Last Weather Time", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// Recent history exists — should always re-fetch from today midnight
		// regardless of lastWeatherTime, so today's hourly updates are included.
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// last time is mid-today, still current version
		lastTime := todayMidnight.Add(6 * time.Hour)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Sync should always start from today midnight
		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(todayMidnight)
		}), mock.Anything).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Should Not Fetch Anything When Up To Date", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// lastWeatherTime is exactly tomorrow midnight (fully up to date for today)
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, testTime, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}
		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Scheduled Slots Refresh - normal hour (Today & Tomorrow)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// 8:30am local time is hour 8 slot.
		normalTime := time.Date(2026, 6, 5, 8, 30, 0, 0, loc)
		now := normalTime
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated before the 8:00 AM local slot.
		lastUpdate := time.Date(2026, 6, 5, 7, 59, 0, 0, loc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Expect fetch starting from today to end of tomorrow (today and tomorrow).
		mockW.On("Forecast", mock.Anything, sl, todayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: todayMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return normalTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - 10:00pm Local (Today & Tomorrow)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		utcLoc := time.UTC
		utcSl := sl
		utcSl.TimeZone = utcLoc.String()

		// 22:15 local time is 10:00pm local slot.
		tenPMTime := time.Date(2026, 6, 5, 22, 15, 0, 0, utcLoc)
		now := tenPMTime
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utcLoc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated before 10:00 PM local slot.
		lastUpdate := time.Date(2026, 6, 5, 21, 59, 0, 0, utcLoc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)

		// Expect fetch starting from today through tomorrow ([todayMidnight, todayMidnight+2d)).
		mockW.On("Forecast", mock.Anything, utcSl, todayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: todayMidnight},
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return tenPMTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", utcSl)
		assert.NoError(t, err)
		mockS.AssertNotCalled(t, "GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - 11:00pm CST (6/4) updates 6/4 and 6/5", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		cstLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		cstSl := sl
		cstSl.TimeZone = cstLoc.String()

		// 11:00pm CST June 4th
		elevenPMTime := time.Date(2026, 6, 4, 23, 0, 0, 0, cstLoc)

		todayMidnight := time.Date(2026, 6, 4, 0, 0, 0, 0, cstLoc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1) // June 5th

		// Last updated before 10:00 PM CST June 4 slot (e.g. 9:59 PM CST June 4)
		lastUpdate := time.Date(2026, 6, 4, 21, 59, 0, 0, cstLoc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)

		// Evening slot refreshes both today (June 4) and tomorrow (June 5) without reading daytime GTI
		mockW.On("Forecast", mock.Anything, cstSl, todayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: todayMidnight},
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return elevenPMTime },
		}

		err = srv.updateWeatherHistory(context.Background(), "test-site", cstSl)
		assert.NoError(t, err)
		mockS.AssertNotCalled(t, "GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - 00:00 Slot Backfills Yesterday", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// 00:15 local time is the 00:00 slot.
		midnightSlotTime := time.Date(2026, 6, 5, 0, 15, 0, 0, loc)
		todayMidnight := time.Date(2026, 6, 5, 0, 0, 0, 0, loc)
		yesterdayMidnight := todayMidnight.AddDate(0, 0, -1)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated during yesterday's 22:00 slot.
		lastUpdate := time.Date(2026, 6, 4, 22, 5, 0, 0, loc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(todayMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", todayMidnight, todayMidnight.Add(24*time.Hour)).Return([]types.Weather{}, nil).Once()

		// Expect fetch starting from yesterday through tomorrow ([todayMidnight-1d, todayMidnight+2d)).
		mockW.On("Forecast", mock.Anything, sl, yesterdayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: yesterdayMidnight},
			{TSDayStart: todayMidnight},
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return midnightSlotTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - Missed 00:00 Slot Recovers At 02:00", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// 02:10 local time is the 02:00 slot after the 00:00 slot failed.
		twoAMTime := time.Date(2026, 6, 5, 2, 10, 0, 0, loc)
		todayMidnight := time.Date(2026, 6, 5, 0, 0, 0, 0, loc)
		yesterdayMidnight := todayMidnight.AddDate(0, 0, -1)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated before midnight (22:05 yesterday) because 00:00 failed.
		lastUpdate := time.Date(2026, 6, 4, 22, 5, 0, 0, loc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(todayMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", todayMidnight, todayMidnight.Add(24*time.Hour)).Return([]types.Weather{}, nil).Once()

		// Non-zero lastWeatherUpdated before todayMidnight triggers yesterday backfill at 02:00.
		mockW.On("Forecast", mock.Anything, sl, yesterdayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: yesterdayMidnight},
			{TSDayStart: todayMidnight},
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return twoAMTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Forecast Transient Error Succeeds On Single Retry", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		now := time.Date(2026, 6, 5, 10, 0, 0, 0, loc)
		todayMidnight := time.Date(2026, 6, 5, 0, 0, 0, 0, loc)
		startDay := todayMidnight.AddDate(0, 0, -14)
		endDay := todayMidnight.AddDate(0, 0, 2)

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockW.On("Forecast", mock.Anything, sl, startDay, endDay).Return([]types.Weather{}, fmt.Errorf("weather api returned status 502")).Once()
		mockW.On("Forecast", mock.Anything, sl, startDay, endDay).Return([]types.Weather{
			{TSDayStart: todayMidnight},
		}, nil).Once()
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.MatchedBy(func(w []types.Weather) bool {
			return len(w) == 1 && w[0].TSDayStart.Equal(todayMidnight)
		}), types.CurrentWeatherVersion).Return(nil).Once()

		srv := &Server{
			storage:           mockS,
			weather:           mockW,
			nowFunc:           func() time.Time { return now },
			weatherRetryDelay: time.Millisecond,
		}

		var warnBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&warnBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		err := srv.updateWeatherHistory(ctx, "test-site", sl)
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(warnBuf.String()), "\n")
		if assert.Len(t, lines, 1) {
			assert.Contains(t, lines[0], `"level":"WARN"`)
			assert.Contains(t, lines[0], "weather forecast failed, retrying")
		}
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Forecast Fails Twice Returns Error After Single Retry", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		now := time.Date(2026, 6, 5, 10, 0, 0, 0, loc)
		todayMidnight := time.Date(2026, 6, 5, 0, 0, 0, 0, loc)
		startDay := todayMidnight.AddDate(0, 0, -14)
		endDay := todayMidnight.AddDate(0, 0, 2)

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockW.On("Forecast", mock.Anything, sl, startDay, endDay).Return([]types.Weather{}, fmt.Errorf("weather api returned status 503")).Times(2)

		srv := &Server{
			storage:           mockS,
			weather:           mockW,
			nowFunc:           func() time.Time { return now },
			weatherRetryDelay: time.Millisecond,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		require.ErrorContains(t, err, "weather api returned status 503")
		mockS.AssertNotCalled(t, "UpsertWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Forecast Retry Aborts Promptly When Context Canceled During Cooldown", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		now := time.Date(2026, 6, 5, 10, 0, 0, 0, loc)
		todayMidnight := time.Date(2026, 6, 5, 0, 0, 0, 0, loc)
		startDay := todayMidnight.AddDate(0, 0, -14)
		endDay := todayMidnight.AddDate(0, 0, 2)

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockW.On("Forecast", mock.Anything, sl, startDay, endDay).Return([]types.Weather{}, fmt.Errorf("weather api returned status 502")).Once()

		srv := &Server{
			storage:           mockS,
			weather:           mockW,
			nowFunc:           func() time.Time { return now },
			weatherRetryDelay: 5 * time.Second,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := srv.updateWeatherHistory(ctx, "test-site", sl)
		elapsed := time.Since(start)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, elapsed, 500*time.Millisecond)
		mockW.AssertExpectations(t)
	})
}

func TestSetESSModes(t *testing.T) {
	t.Run("Success clears ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// Expect SetModes to succeed with changed=true
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeAny, mock.Anything).Return(true, nil)

		// Expect settings to be saved with ConsecutiveSetFailures reset to 0
		mockS.On("SetSettings", mock.Anything, "test-site", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 0
		}), 1, mock.Anything).Return(time.Time{}, nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 3,
					LastAttempt:            time.Now().Add(-time.Hour),
				},
			},
			version: 1,
		}

		changed, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.NoError(t, err)
		assert.True(t, changed)
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("No-op SetModes (changed=false) does not clear ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// When hardware is already in the target state, SetModes sends no write API calls and returns changed=false, nil
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, mock.Anything).Return(false, nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 3,
					LastAttempt:            time.Now().Add(-time.Hour),
				},
			},
			version: 1,
		}

		changed, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.NoError(t, err)
		assert.False(t, changed)
		mockS.AssertNotCalled(t, "SetSettings")
		mockES.AssertExpectations(t)
	})

	t.Run("Unauthorized error increments ConsecutiveSetFailures 0 to 1 then jumps to 40", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// 1. First permanent unauthorized error increments 0 -> 1
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, mock.Anything).Return(false, ess.ErrUnauthorized).Twice()
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 1
		}), 1, mock.Anything).Return(time.Time{}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 0,
				},
			},
			version: 1,
		}

		_, err := srv.setESSModes(context.Background(), "site1", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, s0)
		assert.ErrorIs(t, err, ess.ErrUnauthorized)

		// 2. Second permanent unauthorized error when ConsecutiveSetFailures >= 1 jumps to 40
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 40
		}), 1, mock.Anything).Return(time.Time{}, nil).Once()

		s1 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		_, err = srv.setESSModes(context.Background(), "site1", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, s1)
		assert.ErrorIs(t, err, ess.ErrUnauthorized)

		// 3. Explicit lockout error jumps directly from 0 to 40
		lockedErr := fmt.Errorf("locked: %w: %w", ess.ErrAccountLocked, ess.ErrUnauthorized)
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, mock.Anything).Return(false, lockedErr).Once()
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 40
		}), 1, mock.Anything).Return(time.Time{}, nil).Once()

		_, err = srv.setESSModes(context.Background(), "site1", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, s0)
		assert.ErrorIs(t, err, ess.ErrAccountLocked)

		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("Other error does not increment ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// Expect SetModes to return some other error
		otherErr := fmt.Errorf("network timeout")
		mockES.On("SetModes", mock.Anything, types.BatteryModeStandby, types.SolarModeAny, mock.Anything).Return(false, otherErr)

		// SetSettings should NOT be called since it is not an unauthorized error
		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		_, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeStandby, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.ErrorIs(t, err, otherErr)
		mockS.AssertNotCalled(t, "SetSettings")
		mockES.AssertExpectations(t)
	})

	t.Run("Context WaitGroup populated in handleUpdate", func(t *testing.T) {
		var wgFound bool
		mockES := &mockESS{}
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			if common.CtxWaitGroup(ctx) != nil {
				wgFound = true
			}
		}).Return(true, nil)

		settings := settingsWithVersion{
			Settings: types.Settings{ESS: "tesla"},
			version:  1,
		}

		srv := &Server{}
		modesChanged, err := srv.setESSModes(common.CtxWithWaitGroup(context.Background(), &sync.WaitGroup{}), "test-site", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, settings)
		require.NoError(t, err)
		assert.True(t, modesChanged)
		assert.True(t, wgFound)
	})

	t.Run("SolarMode is forwarded to ESS SetModes and defaults to SolarModeAny", func(t *testing.T) {
		mockES := &mockESS{}
		// Test explicit solarMode forwarding (e.g. SolarModeNoExport)
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeNoExport, mock.Anything).Return(true, nil).Once()
		// Test SolarModeNoChange defaulting to SolarModeAny
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeAny, mock.Anything).Return(true, nil).Once()

		srv := &Server{}
		settings := settingsWithVersion{Settings: types.Settings{ESS: "tesla"}, version: 1}

		changed1, err1 := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeLoad, types.SolarModeNoExport, types.ModesOptions{}, settings)
		require.NoError(t, err1)
		assert.True(t, changed1)

		changed2, err2 := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{}, settings)
		require.NoError(t, err2)
		assert.True(t, changed2)
		mockES.AssertExpectations(t)
	})
}

func TestGetCronGroups(t *testing.T) {
	t.Run("Empty kind param returns all 16 groups", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		groups, err := getCronGroups(now, "", 0, 0)
		require.NoError(t, err)
		assert.Len(t, groups, 16)
		for i := 1; i <= 16; i++ {
			assert.Contains(t, groups, i)
		}
	})

	t.Run("TOU partitions 12 groups into 2 parts of 6", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		g0, err := getCronGroups(now, "tou", 0, 2)
		require.NoError(t, err)
		g1, err := getCronGroups(now, "tou", 1, 2)
		require.NoError(t, err)

		assert.Len(t, g0, 6)
		assert.Len(t, g1, 6)

		all := make(map[int]bool)
		for _, g := range g0 {
			all[g] = true
		}
		for _, g := range g1 {
			all[g] = true
		}

		assert.Len(t, all, 12)
		for i := 1; i <= 12; i++ {
			assert.True(t, all[i], "missing TOU group %d", i)
		}
	})

	t.Run("ComEd returns all 4 groups (13..16)", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		groups, err := getCronGroups(now, "comed", 0, 1)
		require.NoError(t, err)
		assert.Equal(t, []int{13, 14, 15, 16}, groups)
	})

	t.Run("kind=all returns all 16 groups", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		groups, err := getCronGroups(now, "all", 0, 1)
		require.NoError(t, err)
		assert.Len(t, groups, 16)
		for i := 1; i <= 16; i++ {
			assert.Contains(t, groups, i)
		}
	})

	t.Run("TOU groups change when hour changes", func(t *testing.T) {
		t1 := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)

		g0_t1, err1 := getCronGroups(t1, "tou", 0, 2)
		require.NoError(t, err1)
		g0_t2, err2 := getCronGroups(t2, "tou", 0, 2)
		require.NoError(t, err2)

		assert.NotEqual(t, g0_t1, g0_t2)
	})

	t.Run("Invalid parameters return error", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		_, err := getCronGroups(now, "invalid", 0, 1)
		assert.Error(t, err)

		_, err = getCronGroups(now, "tou", 0, 5)
		assert.ErrorContains(t, err, "cannot evenly partition")

		_, err = getCronGroups(now, "tou", 2, 2)
		assert.ErrorContains(t, err, "out of bounds")
	})
}

func TestMergeUtilityVPPEvents(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	t.Run("basic merge within 24 hours", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(-24 * time.Hour),
						End:   now.Add(48 * time.Hour),
						Hours: []types.UtilityHourPeriod{
							{HourStart: 14, HourEnd: 17},
						},
					},
					ReserveSOC: 20.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, "Mandatory Utility VPP Event", status.VPPEvents[0].Description)
			assert.True(t, status.VPPEvents[0].TSStart.Equal(now.Add(2*time.Hour)))
			assert.True(t, status.VPPEvents[0].TSEnd.Equal(now.Add(5*time.Hour)))
			assert.Equal(t, 20.0, status.VPPEvents[0].VPPSoc)
			assert.True(t, status.VPPEvents[0].Mandatory)
		}
	})

	t.Run("ignore overlapping with ESS VPP events", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
			VPPEvents: []types.VPPEvent{
				{
					Description: "ESS VPP Event",
					TSStart:     now.Add(2 * time.Hour),
					TSEnd:       now.Add(4 * time.Hour),
					VPPSoc:      15.0,
				},
			},
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(3 * time.Hour),
						End:   now.Add(5 * time.Hour),
					},
					ReserveSOC: 40.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, "ESS VPP Event", status.VPPEvents[0].Description)
			assert.Equal(t, 15.0, status.VPPEvents[0].VPPSoc)
			assert.True(t, status.VPPEvents[0].Mandatory)
		}
	})

	t.Run("ignore overlapping with storm warnings", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
			Storms: []types.Storm{
				{
					Description: "Stormy weather",
					TSStart:     now.Add(2 * time.Hour),
					TSEnd:       now.Add(5 * time.Hour),
				},
			},
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(3 * time.Hour),
						End:   now.Add(4 * time.Hour),
					},
					ReserveSOC: 40.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		assert.Empty(t, status.VPPEvents)
	})

	t.Run("filter out events starting after 24 hours", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(25 * time.Hour),
						End:   now.Add(27 * time.Hour),
					},
					ReserveSOC: 50.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		assert.Empty(t, status.VPPEvents)
	})

	t.Run("default price for optional VPP events", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
			VPPEvents: []types.VPPEvent{
				{
					Description: "Optional ESS VPP Event",
					TSStart:     now.Add(2 * time.Hour),
					TSEnd:       now.Add(4 * time.Hour),
					VPPSoc:      15.0,
					Mandatory:   false,
				},
				{
					Description:   "Optional ESS VPP Event With Custom Price",
					TSStart:       now.Add(6 * time.Hour),
					TSEnd:         now.Add(8 * time.Hour),
					VPPSoc:        20.0,
					Mandatory:     false,
					DollarsPerKWH: 3.50,
				},
			},
		}

		vppInfo := types.UtilityVPPInfo{}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		if assert.Len(t, status.VPPEvents, 2) {
			assert.Equal(t, 2.0, status.VPPEvents[0].DollarsPerKWH)
			assert.Equal(t, 3.50, status.VPPEvents[1].DollarsPerKWH)
		}
	})
}

func TestInsertAction(t *testing.T) {
	t.Run("InsertAction With Canceled Context", func(t *testing.T) {
		mockS := &mockStorage{}
		var capturedErr error
		var hasDeadline bool
		var capturedDeadline time.Time

		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			capturedErr = ctx.Err()
			d, ok := ctx.Deadline()
			hasDeadline = ok
			capturedDeadline = d
		}).Return(nil)

		srv := &Server{storage: mockS}
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		before := time.Now()
		err := srv.insertAction(canceledCtx, "site1", types.Action{Description: "test action"})
		require.NoError(t, err)

		assert.NoError(t, capturedErr)
		if assert.True(t, hasDeadline) {
			remaining := capturedDeadline.Sub(before)
			assert.GreaterOrEqual(t, remaining, 25*time.Second)
			assert.Less(t, remaining, 35*time.Second)
		}
	})
}

func TestGetFuturePrices(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	fixedNow := time.Date(2026, 10, 6, 10, 30, 0, 0, loc) // Tuesday 10:30 AM
	todayStart := time.Date(2026, 10, 6, 0, 0, 0, 0, loc)
	tomorrowStart := time.Date(2026, 10, 7, 0, 0, 0, 0, loc)
	tomorrowEnd := time.Date(2026, 10, 8, 0, 0, 0, 0, loc)

	t.Run("HasAllTomorrow_NoEstimationNeeded", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// 48 hours of future prices covering today and tomorrow
		var futurePrices []types.Price
		for hr := 11; hr < 48; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			futurePrices = append(futurePrices, types.Price{
				Provider:      "comed",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.15,
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)
		assert.Equal(t, len(futurePrices), len(prices))
		mockS.AssertNotCalled(t, "GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("MissingTomorrow_EstimatesFrom72hHistory", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// Today has prices from 11:00 to 24:00 (13 hours). Tomorrow is completely missing.
		var todayPrices []types.Price
		for hr := 11; hr < 24; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			todayPrices = append(todayPrices, types.Price{
				Provider:             "comed",
				TSStart:              st,
				TSEnd:                st.Add(time.Hour),
				DollarsPerKWH:        0.20,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(todayPrices, nil)

		// 72 hours of history (3 days)
		// For hour 14:
		// Day -3 (Oct 3): 0.10, grid 0.02
		// Day -2 (Oct 4): 0.12, grid 0.04
		// Day -1 (Oct 5): 0.14, grid 0.06
		// Expected average for hour 14: 0.12, grid 0.04
		var histPrices []types.Price
		for day := 1; day <= 3; day++ {
			dStart := todayStart.AddDate(0, 0, -day)
			for h := 0; h < 24; h++ {
				rate := 0.10 + float64(h)*0.005 + float64(day)*0.01
				grid := 0.03
				if h == 14 {
					if day == 1 {
						rate = 0.14
						grid = 0.06
					} else if day == 2 {
						rate = 0.12
						grid = 0.04
					} else {
						rate = 0.10
						grid = 0.02
					}
				}
				st := dStart.Add(time.Duration(h) * time.Hour)
				histPrices = append(histPrices, types.Price{
					Provider:             "comed",
					TSStart:              st,
					TSEnd:                st.Add(time.Hour),
					DollarsPerKWH:        rate,
					GridUseDollarsPerKWH: grid,
				})
			}
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		// 13 today hours + 24 tomorrow hours = 37 total prices
		assert.Len(t, prices, 37)

		// Verify today's prices remain unchanged
		assert.Equal(t, 0.20, prices[0].DollarsPerKWH)
		assert.Equal(t, todayStart.Add(11*time.Hour), prices[0].TSStart)

		// Verify tomorrow's 24 hours are all present
		tomorrowPricesMap := make(map[int]types.Price)
		for _, p := range prices {
			if !p.TSStart.Before(tomorrowStart) && p.TSStart.Before(tomorrowEnd) {
				tomorrowPricesMap[p.TSStart.Hour()] = p
			}
		}
		assert.Len(t, tomorrowPricesMap, 24)

		// Verify hour 14 median template: closest price to median([0.10, 0.12, 0.14, 0.20] = 0.13) is 0.12, grid 0.04
		h14Price, ok := tomorrowPricesMap[14]
		require.True(t, ok)
		assert.InDelta(t, 0.12, h14Price.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.04, h14Price.GridUseDollarsPerKWH, 0.0001)

		mockS.AssertExpectations(t)
	})

	t.Run("PartiallyMissingTomorrow_EstimatesOnlyMissingHours", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// Today 11:00-24:00, Tomorrow 00:00-12:00 (12 hours of tomorrow). 12:00-24:00 missing.
		var partialPrices []types.Price
		for hr := 11; hr < 24; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			partialPrices = append(partialPrices, types.Price{
				Provider:      "ameren",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.18,
			})
		}
		for hr := 0; hr < 12; hr++ {
			st := tomorrowStart.Add(time.Duration(hr) * time.Hour)
			partialPrices = append(partialPrices, types.Price{
				Provider:      "ameren",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.22,
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(partialPrices, nil)

		histPrices := []types.Price{
			{
				Provider:      "ameren",
				TSStart:       fixedNow.Add(-24 * time.Hour),
				TSEnd:         fixedNow.Add(-23 * time.Hour),
				DollarsPerKWH: 0.30,
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		// 13 today + 12 posted tomorrow + 12 estimated tomorrow = 37 total
		assert.Len(t, prices, 37)

		// Verify 00:00-12:00 of tomorrow retained 0.22 posted price
		for _, p := range prices {
			if !p.TSStart.Before(tomorrowStart) && p.TSStart.Before(tomorrowStart.Add(12*time.Hour)) {
				assert.Equal(t, 0.22, p.DollarsPerKWH)
			}
			if !p.TSStart.Before(tomorrowStart.Add(12*time.Hour)) && p.TSStart.Before(tomorrowEnd) {
				assert.InDelta(t, 0.18, p.DollarsPerKWH, 0.0001)
			}
		}
	})

	t.Run("NoFuturePrices_EstimatesTodayAndTomorrow", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		histPrices := []types.Price{
			{
				Provider:      "comed",
				TSStart:       fixedNow.Truncate(time.Hour).Add(-24 * time.Hour),
				TSEnd:         fixedNow.Truncate(time.Hour).Add(-23 * time.Hour),
				DollarsPerKWH: 0.15,
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		// From fixedNow.Truncate(time.Hour) (10:00) through tomorrowEnd (tomorrow 24:00):
		// Today 10:00-24:00 = 14 hours + Tomorrow 24 hours = 38 hours
		assert.Len(t, prices, 38)
		assert.Equal(t, fixedNow.Truncate(time.Hour), prices[0].TSStart)
		assert.Equal(t, tomorrowEnd, prices[len(prices)-1].TSEnd)
	})

	t.Run("NoFuturePrices_NoHistory_ReturnsError", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		assert.Nil(t, prices)
		assert.ErrorContains(t, err, "insufficient future pricing data")
	})

	t.Run("OutlierSpike_DiscardedByMedian", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		var todayPrices []types.Price
		for hr := 11; hr < 24; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			todayPrices = append(todayPrices, types.Price{
				Provider:      "comed",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.20,
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(todayPrices, nil)

		// 3 days of history:
		// Hour 14: Day -3 = $0.35, Day -2 = $0.35, Day -1 = $1.00 (spike)
		var histPrices []types.Price
		for day := 1; day <= 3; day++ {
			dStart := todayStart.AddDate(0, 0, -day)
			for h := 0; h < 24; h++ {
				rate := 0.20
				if h == 14 {
					if day == 1 {
						rate = 1.00 // spike
					} else {
						rate = 0.35
					}
				}
				st := dStart.Add(time.Duration(h) * time.Hour)
				histPrices = append(histPrices, types.Price{
					Provider:      "comed",
					TSStart:       st,
					TSEnd:         st.Add(time.Hour),
					DollarsPerKWH: rate,
				})
			}
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		var tomorrowH14Price *types.Price
		for _, p := range prices {
			if p.TSStart.Equal(tomorrowStart.Add(14 * time.Hour)) {
				tomorrowH14Price = &p
				break
			}
		}
		require.NotNil(t, tomorrowH14Price)
		// Median of [0.20, 0.35, 0.35, 1.00] is 0.35 (whereas arithmetic mean would be 0.475)
		assert.InDelta(t, 0.35, tomorrowH14Price.DollarsPerKWH, 0.0001)
	})

	t.Run("HalfHourIntervals_WithStaticTOUImportAndDynamicExportCredit", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// Simulate Octopus Agile Outgoing + Intelligent Octopus Go before 16:00 UK time:
		// Published future prices cover 30-minute intervals from 10:30 today until 23:00 today.
		var todayPrices []types.Price
		for slot := 21; slot < 46; slot++ { // 10:30 (slot 21) to 23:00 (slot 46)
			st := todayStart.Add(time.Duration(slot) * 30 * time.Minute)
			todayPrices = append(todayPrices, types.Price{
				Provider:                      "octopus",
				TSStart:                       st,
				TSEnd:                         st.Add(30 * time.Minute),
				DollarsPerKWH:                 0.2802,
				GenerationCreditDollarsPerKWH: 0.15,
				SeparateGenerationCredit:      true,
				PeriodName:                    "peak",
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(todayPrices, nil)

		// 3 days of 30-minute historical prices:
		// Off-peak (0.07): 00:00-05:30 (slots 0..10) and 23:30-24:00 (slot 47)
		// Peak (0.2802): 05:30-23:30 (slots 11..46)
		// Slot 36 (18:00-18:30): export credits across 3 days are [0.40 (day 1), 0.25 (day 2), 0.25 (day 3)], today future is 0.15 -> sorted [0.15, 0.25, 0.25, 0.40], median is 0.25
		// Slot 37 (18:30-19:00): export credits across 3 days are [0.08 (day 1), 0.18 (day 2), 0.18 (day 3)], today future is 0.15 -> sorted [0.08, 0.15, 0.18, 0.18], closest to median (0.165) with two 0.18s vs 0.15: let's use [0.18 (day 1), 0.18 (day 2), 0.30 (day 3)] -> sorted [0.15, 0.18, 0.18, 0.30], median is 0.18
		// Slot 46 (23:00-23:30, not in todayPrices): export credits across 3 days are [0.35 (day 1), 0.14 (day 2), 0.05 (day 3)] -> median is 0.14
		var histPrices []types.Price
		for day := 1; day <= 3; day++ {
			dStart := todayStart.AddDate(0, 0, -day)
			for slot := 0; slot < 48; slot++ {
				st := dStart.Add(time.Duration(slot) * 30 * time.Minute)
				importRate := 0.2802
				periodName := "peak"
				if slot < 11 || slot == 47 {
					importRate = 0.07
					periodName = "off-peak"
				}
				exportRate := 0.10
				switch slot {
				case 36: // 18:00-18:30
					switch day {
					case 1:
						exportRate = 0.40
					case 2:
						exportRate = 0.25
					case 3:
						exportRate = 0.25
					}
				case 37: // 18:30-19:00
					switch day {
					case 1:
						exportRate = 0.18
					case 2:
						exportRate = 0.18
					case 3:
						exportRate = 0.30
					}
				case 46: // 23:00-23:30
					switch day {
					case 1:
						exportRate = 0.35
					case 2:
						exportRate = 0.14
					case 3:
						exportRate = 0.05
					}
				}
				histPrices = append(histPrices, types.Price{
					Provider:                      "octopus",
					TSStart:                       st,
					TSEnd:                         st.Add(30 * time.Minute),
					DollarsPerKWH:                 importRate,
					GenerationCreditDollarsPerKWH: exportRate,
					SeparateGenerationCredit:      true,
					PeriodName:                    periodName,
				})
			}
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		// Today 10:30-23:00 (25 known 30m slots) + Today 23:00-24:00 (2 estimated 30m slots) + Tomorrow 00:00-24:00 (48 estimated 30m slots) = 75 slots
		if assert.Len(t, prices, 75) {
			priceByStart := make(map[time.Time]types.Price, len(prices))
			for _, p := range prices {
				assert.Equal(t, 30*time.Minute, p.TSEnd.Sub(p.TSStart))
				priceByStart[p.TSStart] = p
			}

			// Verify 05:00-05:30 vs 05:30-06:00 tomorrow preserves the half-hour TOU boundary
			p0500, ok := priceByStart[tomorrowStart.Add(5*time.Hour)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.07, p0500.DollarsPerKWH, 0.0001)
				assert.Equal(t, "off-peak", p0500.PeriodName)
			}
			p0530, ok := priceByStart[tomorrowStart.Add(5*time.Hour+30*time.Minute)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.2802, p0530.DollarsPerKWH, 0.0001)
				assert.Equal(t, "peak", p0530.PeriodName)
			}

			// Verify 23:00-23:30 vs 23:30-24:00 today (missing tonight slots after 23:00) preserves the half-hour boundary and median export credit
			pTonight2300, ok := priceByStart[todayStart.Add(23*time.Hour)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.2802, pTonight2300.DollarsPerKWH, 0.0001)
				assert.InDelta(t, 0.14, pTonight2300.GenerationCreditDollarsPerKWH, 0.0001)
				assert.True(t, pTonight2300.SeparateGenerationCredit)
			}
			pTonight2330, ok := priceByStart[todayStart.Add(23*time.Hour+30*time.Minute)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.07, pTonight2330.DollarsPerKWH, 0.0001)
				assert.Equal(t, "off-peak", pTonight2330.PeriodName)
			}

			// Verify 18:00-18:30 vs 18:30-19:00 tomorrow have distinct 30m median export credits
			p1800, ok := priceByStart[tomorrowStart.Add(18*time.Hour)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.2802, p1800.DollarsPerKWH, 0.0001)
				assert.InDelta(t, 0.25, p1800.GenerationCreditDollarsPerKWH, 0.0001)
			}
			p1830, ok := priceByStart[tomorrowStart.Add(18*time.Hour+30*time.Minute)]
			if assert.True(t, ok) {
				assert.InDelta(t, 0.2802, p1830.DollarsPerKWH, 0.0001)
				assert.InDelta(t, 0.18, p1830.GenerationCreditDollarsPerKWH, 0.0001)
			}
		}
	})

	t.Run("SeparateGenerationCredit_IndependentDynamicImportAndExport", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		// 3 days of history at hour 12 where import and export medians occur on different days:
		// Day -1: import = 0.10, export = 0.30
		// Day -2: import = 0.20 (median import), export = 0.10
		// Day -3: import = 0.30, export = 0.20 (median export)
		var histPrices []types.Price
		for day := 1; day <= 3; day++ {
			st := todayStart.AddDate(0, 0, -day).Add(12 * time.Hour)
			var imp, exp float64
			switch day {
			case 1:
				imp, exp = 0.10, 0.30
			case 2:
				imp, exp = 0.20, 0.10
			case 3:
				imp, exp = 0.30, 0.20
			}
			histPrices = append(histPrices, types.Price{
				Provider:                      "octopus",
				TSStart:                       st,
				TSEnd:                         st.Add(time.Hour),
				DollarsPerKWH:                 imp,
				GenerationCreditDollarsPerKWH: exp,
				SeparateGenerationCredit:      true,
			})
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		var tomorrowH12 *types.Price
		for _, p := range prices {
			if p.TSStart.Equal(tomorrowStart.Add(12 * time.Hour)) {
				tomorrowH12 = &p
				break
			}
		}
		if assert.NotNil(t, tomorrowH12) {
			assert.InDelta(t, 0.20, tomorrowH12.DollarsPerKWH, 0.0001)
			assert.InDelta(t, 0.20, tomorrowH12.GenerationCreditDollarsPerKWH, 0.0001)
			assert.True(t, tomorrowH12.SeparateGenerationCredit)
		}
	})

	t.Run("ArbitrarySubHourIntervals_PreservedFromOverlappingHistory", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// Today has prices from 11:00 to 24:00 (1-hour blocks), but tomorrow is missing.
		// Historical data has a 15-minute split in the 08:00 hour:
		// 08:00-08:15 ($0.10), 08:15-08:45 ($0.45), 08:45-09:00 ($0.12), and 1-hour blocks elsewhere ($0.15).
		var todayPrices []types.Price
		for hr := 11; hr < 24; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			todayPrices = append(todayPrices, types.Price{
				Provider:      "tou",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.15,
			})
		}
		mockU.On("GetFuturePrices", mock.Anything).Return(todayPrices, nil)

		var histPrices []types.Price
		for day := 1; day <= 3; day++ {
			dStart := todayStart.AddDate(0, 0, -day)
			for h := 0; h < 24; h++ {
				st := dStart.Add(time.Duration(h) * time.Hour)
				if h == 8 {
					histPrices = append(histPrices,
						types.Price{Provider: "tou", TSStart: st, TSEnd: st.Add(15 * time.Minute), DollarsPerKWH: 0.10},
						types.Price{Provider: "tou", TSStart: st.Add(15 * time.Minute), TSEnd: st.Add(45 * time.Minute), DollarsPerKWH: 0.45},
						types.Price{Provider: "tou", TSStart: st.Add(45 * time.Minute), TSEnd: st.Add(time.Hour), DollarsPerKWH: 0.12},
					)
				} else {
					histPrices = append(histPrices, types.Price{
						Provider:      "tou",
						TSStart:       st,
						TSEnd:         st.Add(time.Hour),
						DollarsPerKWH: 0.15,
					})
				}
			}
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		// 13 today + 26 tomorrow (23 1-hour blocks + 3 sub-hour blocks in hour 8) = 39 total
		if assert.Len(t, prices, 39) {
			priceByStart := make(map[time.Time]types.Price, len(prices))
			for _, p := range prices {
				priceByStart[p.TSStart] = p
			}

			t0800 := tomorrowStart.Add(8 * time.Hour)
			if p, ok := priceByStart[t0800]; assert.True(t, ok) {
				assert.Equal(t, t0800.Add(15*time.Minute), p.TSEnd)
				assert.InDelta(t, 0.10, p.DollarsPerKWH, 0.0001)
			}
			t0815 := t0800.Add(15 * time.Minute)
			if p, ok := priceByStart[t0815]; assert.True(t, ok) {
				assert.Equal(t, t0800.Add(45*time.Minute), p.TSEnd)
				assert.InDelta(t, 0.45, p.DollarsPerKWH, 0.0001)
			}
			t0845 := t0800.Add(45 * time.Minute)
			if p, ok := priceByStart[t0845]; assert.True(t, ok) {
				assert.Equal(t, t0800.Add(time.Hour), p.TSEnd)
				assert.InDelta(t, 0.12, p.DollarsPerKWH, 0.0001)
			}
		}
	})

	t.Run("InitialGapAndMidnightSpanningAndNextCandidateClamping", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}

		// Future prices start at 14:00 today (leaving an initial gap 11:00-14:00 today) and stop at 23:30 today.
		var todayPrices []types.Price
		for hr := 14; hr < 23; hr++ {
			st := todayStart.Add(time.Duration(hr) * time.Hour)
			todayPrices = append(todayPrices, types.Price{
				Provider:      "tou",
				TSStart:       st,
				TSEnd:         st.Add(time.Hour),
				DollarsPerKWH: 0.20,
			})
		}
		todayPrices = append(todayPrices, types.Price{
			Provider:      "tou",
			TSStart:       todayStart.Add(23 * time.Hour),
			TSEnd:         todayStart.Add(23*time.Hour + 30*time.Minute),
			DollarsPerKWH: 0.20,
		})
		mockU.On("GetFuturePrices", mock.Anything).Return(todayPrices, nil)

		// History has:
		// - A midnight-spanning interval 23:30–00:30 ($0.08)
		// - A sub-hour candidate 07:30–08:00 ($0.30) with no history for 07:00–07:30 (so 07:00 clamps to 07:30)
		dStart := todayStart.AddDate(0, 0, -1)
		histPrices := []types.Price{
			{
				Provider:      "tou",
				TSStart:       dStart.Add(-30 * time.Minute), // 23:30 two nights ago -> 00:30 yesterday
				TSEnd:         dStart.Add(30 * time.Minute),
				DollarsPerKWH: 0.08,
			},
			{
				Provider:      "tou",
				TSStart:       dStart.Add(7*time.Hour + 30*time.Minute),
				TSEnd:         dStart.Add(8 * time.Hour),
				DollarsPerKWH: 0.30,
			},
		}

		histStart := fixedNow.Add(-168 * time.Hour)
		mockS.On("GetPriceHistory", mock.Anything, "site1", histStart, fixedNow).Return(histPrices, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return fixedNow },
		}

		prices, err := srv.getFuturePrices(context.Background(), "site1", mockU)
		require.NoError(t, err)

		priceByStart := make(map[time.Time]types.Price, len(prices))
		for _, p := range prices {
			priceByStart[p.TSStart] = p
		}

		// 1. Initial gap 11:00-14:00 today is filled starting at 11:00 today
		assert.Equal(t, todayStart.Add(11*time.Hour), prices[0].TSStart)

		// 2. Midnight-spanning 23:30 today -> 00:30 tomorrow is matched with $0.08
		if p2330, ok := priceByStart[todayStart.Add(23*time.Hour+30*time.Minute)]; assert.True(t, ok) {
			assert.Equal(t, tomorrowStart.Add(30*time.Minute), p2330.TSEnd)
			assert.InDelta(t, 0.08, p2330.DollarsPerKWH, 0.0001)
		}

		// 3. Tomorrow 06:30–07:30 clamps at 07:30 so 07:30–08:00 ($0.30) is preserved
		if p0730, ok := priceByStart[tomorrowStart.Add(7*time.Hour+30*time.Minute)]; assert.True(t, ok) {
			assert.Equal(t, tomorrowStart.Add(8*time.Hour), p0730.TSEnd)
			assert.InDelta(t, 0.30, p0730.DollarsPerKWH, 0.0001)
		}
	})
}

func TestSyncHistorySummaryWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 10, 20, 8, 0, 0, 0, loc)
	yesterday := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)

	t.Run("EnrichesHourlyEnergyStatsFromActions", func(t *testing.T) {
		mockS := &mockStorage{}

		h8 := yesterday.Add(8 * time.Hour)
		h12 := yesterday.Add(12 * time.Hour)
		h17 := yesterday.Add(17 * time.Hour)
		h23 := yesterday.Add(23 * time.Hour)

		dailyEnergy := []types.DailyEnergyStats{
			{
				TSDayStart:   yesterday,
				TimeLocation: loc.String(),
				Hourly: []types.EnergyStats{
					{TSHourStart: h8, TimeLocation: loc.String(), HomeKWH: 1.5},
					{TSHourStart: h12, TimeLocation: loc.String(), SolarKWH: 4.0},
					{TSHourStart: h17, TimeLocation: loc.String(), BatteryUsedKWH: 3.0},
					{TSHourStart: h23, TimeLocation: loc.String(), HomeKWH: 0.8},
				},
			},
		}

		alarm1 := types.SystemAlarm{Code: "E100", Name: "Inverter Fault", Description: "Fault details"}
		actions := []types.Action{
			{
				Timestamp: h8.Add(15 * time.Minute),
				SystemStatus: types.SystemStatus{
					BatteryHeating: true,
				},
			},
			{
				Timestamp: h8.Add(35 * time.Minute),
				SystemStatus: types.SystemStatus{
					BatteryHeating: false,
				},
			},
			{
				Timestamp: h12.Add(10 * time.Minute),
				SolarMode: types.SolarModeNoExport,
				Reason:    types.ActionReasonPreventSolarCurtailment,
			},
			{
				Timestamp: h17.Add(5 * time.Minute),
				Reason:    types.ActionReasonVPPActive,
				SystemStatus: types.SystemStatus{
					VPPActive: true,
				},
			},
			{
				Timestamp: h23.Add(10 * time.Minute),
				Reason:    types.ActionReasonGridUnavailable,
				SystemStatus: types.SystemStatus{
					GridUnavailable: true,
					EmergencyMode:   true,
					Alarms:          []types.SystemAlarm{alarm1},
				},
			},
			{
				Timestamp: h23.Add(25 * time.Minute),
				SystemStatus: types.SystemStatus{
					Alarms: []types.SystemAlarm{alarm1}, // duplicate alarm in same hour should be deduplicated
				},
			},
		}

		mockS.On("GetEnergyHistory", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(dailyEnergy, nil).Once()
		mockS.On("GetWeather", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return([]types.Weather{}, nil).Once()
		mockS.On("GetActionHistory", mock.Anything, "site1", h8, h23.Add(time.Hour)).Return(actions, nil).Once()

		var capturedSummary types.HistorySummary
		mockS.On("UpdateHistorySummary", mock.Anything, "site1", "2026-10", mock.Anything).Run(func(args mock.Arguments) {
			capturedSummary = args.Get(3).(types.HistorySummary)
		}).Return(types.HistorySummary{}, nil).Once()

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return now },
		}

		_, err := srv.syncHistorySummaryWindow(context.Background(), "site1", yesterday, true, yesterday, true, now, false, nil)
		require.NoError(t, err)
		mockS.AssertExpectations(t)

		if assert.Len(t, capturedSummary.Energy, 1) {
			hourly := capturedSummary.Energy[0].Hourly
			if assert.Len(t, hourly, 4) {
				// 08:00 hour: HadBatteryHeating = true
				assert.True(t, hourly[0].HadBatteryHeating)
				assert.False(t, hourly[0].HadVPPActive)
				assert.False(t, hourly[0].HadSolarNoExport)
				assert.Empty(t, hourly[0].Alarms)

				// 12:00 hour: HadSolarNoExport = true
				assert.False(t, hourly[1].HadBatteryHeating)
				assert.True(t, hourly[1].HadSolarNoExport)

				// 17:00 hour: HadVPPActive = true
				assert.True(t, hourly[2].HadVPPActive)
				assert.False(t, hourly[2].HadGridUnavailable)

				// 23:00 hour: HadGridUnavailable, HadEmergencyMode, and deduplicated Alarms
				assert.True(t, hourly[3].HadGridUnavailable)
				assert.True(t, hourly[3].HadEmergencyMode)
				if assert.Len(t, hourly[3].Alarms, 1) {
					assert.Equal(t, "E100", hourly[3].Alarms[0].Code)
				}
			}
		}
	})

	t.Run("DefersPreMidnightWeatherUntilPostMidnightOrSixHourThreshold", func(t *testing.T) {
		h23 := yesterday.Add(23 * time.Hour)
		dailyEnergy := []types.DailyEnergyStats{
			{
				TSDayStart:   yesterday,
				TimeLocation: loc.String(),
				Hourly: []types.EnergyStats{
					{TSHourStart: h23, TimeLocation: loc.String(), HomeKWH: 1.2},
				},
			},
		}

		staleWeather := []types.Weather{
			{
				TSDayStart:   yesterday,
				TimeLocation: loc.String(),
				TSUpdated:    yesterday.Add(22*time.Hour + 5*time.Minute),
			},
		}

		// 1. Before 6-hour threshold with pre-midnight weather: archival is deferred.
		earlyNow := time.Date(2026, 10, 20, 1, 0, 0, 0, loc)
		mockDefer := &mockStorage{}
		mockDefer.On("GetEnergyHistory", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(dailyEnergy, nil).Once()
		mockDefer.On("GetWeather", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(staleWeather, nil).Once()

		srvDefer := &Server{
			storage: mockDefer,
			nowFunc: func() time.Time { return earlyNow },
		}
		updated, err := srvDefer.syncHistorySummaryWindow(context.Background(), "site1", yesterday, true, yesterday, true, earlyNow, false, nil)
		require.NoError(t, err)
		assert.Empty(t, updated)
		mockDefer.AssertNotCalled(t, "GetActionHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockDefer.AssertNotCalled(t, "UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockDefer.AssertExpectations(t)

		// 2. Before 6-hour threshold once post-midnight weather arrives: archives immediately.
		freshWeather := []types.Weather{
			{
				TSDayStart:   yesterday,
				TimeLocation: loc.String(),
				TSUpdated:    time.Date(2026, 10, 20, 0, 5, 0, 0, loc),
			},
		}
		mockFresh := &mockStorage{}
		mockFresh.On("GetEnergyHistory", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(dailyEnergy, nil).Once()
		mockFresh.On("GetWeather", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(freshWeather, nil).Once()
		mockFresh.On("GetActionHistory", mock.Anything, "site1", h23, h23.Add(time.Hour)).Return([]types.Action{}, nil).Once()

		var archivedFresh types.HistorySummary
		mockFresh.On("UpdateHistorySummary", mock.Anything, "site1", "2026-10", mock.Anything).Run(func(args mock.Arguments) {
			archivedFresh = args.Get(3).(types.HistorySummary)
		}).Return(types.HistorySummary{}, nil).Once()

		srvFresh := &Server{
			storage: mockFresh,
			nowFunc: func() time.Time { return earlyNow },
		}
		updated, err = srvFresh.syncHistorySummaryWindow(context.Background(), "site1", yesterday, true, yesterday, true, earlyNow, false, nil)
		require.NoError(t, err)
		assert.Len(t, updated, 1)
		if assert.Len(t, archivedFresh.Weather, 1) {
			assert.Equal(t, freshWeather[0].TSUpdated, archivedFresh.Weather[0].TSUpdated)
		}
		mockFresh.AssertExpectations(t)

		// 3. At 6-hour threshold with pre-midnight weather: falls back to archiving.
		sixHourNow := yesterday.Add(30 * time.Hour)
		mockFallback := &mockStorage{}
		mockFallback.On("GetEnergyHistory", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(dailyEnergy, nil).Once()
		mockFallback.On("GetWeather", mock.Anything, "site1", yesterday, yesterday.AddDate(0, 0, 1)).Return(staleWeather, nil).Once()
		mockFallback.On("GetActionHistory", mock.Anything, "site1", h23, h23.Add(time.Hour)).Return([]types.Action{}, nil).Once()

		var archivedFallback types.HistorySummary
		mockFallback.On("UpdateHistorySummary", mock.Anything, "site1", "2026-10", mock.Anything).Run(func(args mock.Arguments) {
			archivedFallback = args.Get(3).(types.HistorySummary)
		}).Return(types.HistorySummary{}, nil).Once()

		srvFallback := &Server{
			storage: mockFallback,
			nowFunc: func() time.Time { return sixHourNow },
		}
		updated, err = srvFallback.syncHistorySummaryWindow(context.Background(), "site1", yesterday, true, yesterday, true, sixHourNow, false, nil)
		require.NoError(t, err)
		assert.Len(t, updated, 1)
		if assert.Len(t, archivedFallback.Weather, 1) {
			assert.Equal(t, staleWeather[0].TSUpdated, archivedFallback.Weather[0].TSUpdated)
		}
		mockFallback.AssertExpectations(t)
	})

	t.Run("DefersPreMidnightWeatherAcrossFallBackDST", func(t *testing.T) {
		// 2026-11-01 in America/Chicago is a 25-hour Fall-Back DST day (-05:00 CDT -> -06:00 CST).
		dstDay := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
		dstDayEnd := time.Date(2026, 11, 2, 0, 0, 0, 0, loc) // 25 hours after dstDay
		h23 := time.Date(2026, 11, 1, 23, 0, 0, 0, loc)

		dailyEnergy := []types.DailyEnergyStats{
			{
				TSDayStart:   dstDay,
				TimeLocation: loc.String(),
				Hourly: []types.EnergyStats{
					{TSHourStart: h23, TimeLocation: loc.String(), HomeKWH: 1.0},
				},
			},
		}

		// Simulate JSON-unmarshaled TSDayStart with a fixed -05:00 offset and TSUpdated at 23:15 CST
		// (24h15m after start of day, yet still 45 minutes before local midnight dstDayEnd).
		fixedCDT := time.FixedZone("", -5*3600)
		dstWeather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 11, 1, 0, 0, 0, 0, fixedCDT),
				TimeLocation: loc.String(),
				TSUpdated:    dstDayEnd.Add(-45 * time.Minute),
			},
		}

		earlyNow := dstDayEnd.Add(30 * time.Minute)
		mockS := &mockStorage{}
		mockS.On("GetEnergyHistory", mock.Anything, "site1", dstDay, dstDayEnd).Return(dailyEnergy, nil).Once()
		mockS.On("GetWeather", mock.Anything, "site1", dstDay, dstDayEnd).Return(dstWeather, nil).Once()

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return earlyNow },
		}

		updated, err := srv.syncHistorySummaryWindow(context.Background(), "site1", dstDay, true, dstDay, true, earlyNow, false, nil)
		require.NoError(t, err)
		assert.Empty(t, updated)
		mockS.AssertNotCalled(t, "UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipsEmptyMonthlySummariesAndAlreadyArchivedDays", func(t *testing.T) {
		day1 := time.Date(2026, 10, 10, 0, 0, 0, 0, loc)
		day2 := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)
		now := time.Date(2026, 10, 12, 8, 0, 0, 0, loc)

		// 1. Empty month skips UpdateHistorySummary
		mockEmpty := &mockStorage{}
		mockEmpty.On("GetEnergyHistory", mock.Anything, "site1", day1, truncateDay(now)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockEmpty.On("GetWeather", mock.Anything, "site1", day1, truncateDay(now)).Return([]types.Weather{}, nil).Once()
		srvEmpty := &Server{storage: mockEmpty, nowFunc: func() time.Time { return now }}

		updated, err := srvEmpty.syncHistorySummaryWindow(context.Background(), "site1", day1, true, day1, true, now, true, nil)
		require.NoError(t, err)
		assert.Empty(t, updated)
		mockEmpty.AssertNotCalled(t, "UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockEmpty.AssertExpectations(t)

		// 2. Already-archived weather-only days skip redundant UpdateHistorySummary writes
		existing := []types.HistorySummary{
			{
				Weather: []types.Weather{
					{TSDayStart: day1, TimeLocation: loc.String(), TSUpdated: day1.Add(30 * time.Hour)},
					{TSDayStart: day2, TimeLocation: loc.String(), TSUpdated: day2.Add(30 * time.Hour)},
				},
			},
		}
		mockArchived := &mockStorage{}
		mockArchived.On("GetEnergyHistory", mock.Anything, "site1", day1, truncateDay(now)).Return([]types.DailyEnergyStats{}, nil).Once()
		srvArchived := &Server{storage: mockArchived, nowFunc: func() time.Time { return now }}

		updated, err = srvArchived.syncHistorySummaryWindow(context.Background(), "site1", day1, true, day2.AddDate(0, 0, 1), true, now, false, existing)
		require.NoError(t, err)
		assert.Empty(t, updated)
		mockArchived.AssertNotCalled(t, "GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockArchived.AssertNotCalled(t, "UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockArchived.AssertExpectations(t)
	})
}

func TestGetCombinedHistory(t *testing.T) {
	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	nyLoc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("CrossTimezoneWeatherSummaryAdvancesToTodayStart", func(t *testing.T) {
		mockS := &mockStorage{}
		now := time.Date(2026, 11, 4, 12, 0, 0, 0, chicagoLoc)
		todayStart := truncateDay(now) // 2026-11-04T00:00:00-06:00
		historyStart := todayStart.AddDate(0, 0, -35)

		// Energy is in America/Chicago (-06:00), Weather is in America/New_York (-05:00)
		yesterdayEnergyStart := time.Date(2026, 11, 3, 0, 0, 0, 0, chicagoLoc)
		yesterdayWeatherStart := time.Date(2026, 11, 3, 0, 0, 0, 0, nyLoc)

		summaries := []types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart:   yesterdayEnergyStart,
						TimeLocation: chicagoLoc.String(),
						Hourly:       []types.EnergyStats{{TSHourStart: yesterdayEnergyStart, HomeKWH: 1.0}},
					},
				},
				Weather: []types.Weather{
					{
						TSDayStart:    yesterdayWeatherStart,
						TimeLocation:  nyLoc.String(),
						ForecastHours: []types.HourlyWeather{{TSHourStart: yesterdayWeatherStart, TemperatureC: 10}},
					},
				},
			},
		}

		// Both energyFetchStart and weatherFetchStart must advance to todayStart (2026-11-04T00:00:00-06:00),
		// not re-fetch yesterday (2026-11-03).
		mockS.On("GetEnergyHistory", mock.Anything, "site1", todayStart, todayStart.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetWeather", mock.Anything, "site1", todayStart, todayStart.AddDate(0, 0, 2)).Return([]types.Weather{}, nil).Once()

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return now },
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				Location: &types.SiteLocation{
					TimeZone: nyLoc.String(),
				},
			},
		}

		energy, weather, err := srv.getCombinedHistory(context.Background(), "site1", settings, historyStart, now, summaries)
		require.NoError(t, err)
		assert.Len(t, energy, 1)
		assert.Len(t, weather, 1)
		mockS.AssertExpectations(t)
	})

	t.Run("CallerPassingTomorrowEndUsesRealYesterdayForSummaryCheck", func(t *testing.T) {
		mockS := &mockStorage{}
		realNow := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
		realToday := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
		realYesterday := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

		// handleHistoryEnergy passes targetDate + 1 day (2026-11-05T00:00:00Z) as `now`
		queryEnd := realToday.AddDate(0, 0, 1)
		historyStart := realToday.AddDate(0, 0, -36)

		summaries := []types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart: realYesterday,
						Hourly:     []types.EnergyStats{{TSHourStart: realYesterday, HomeKWH: 2.0}},
					},
				},
				Weather: []types.Weather{
					{
						TSDayStart:    realYesterday,
						ForecastHours: []types.HourlyWeather{{TSHourStart: realYesterday, TemperatureC: 12}},
					},
				},
			},
		}

		// Fetch starts at realToday (2026-11-04) up to queryEnd + 1 day / 2 days
		mockS.On("GetEnergyHistory", mock.Anything, "site1", realToday, queryEnd.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("GetWeather", mock.Anything, "site1", realToday, queryEnd.AddDate(0, 0, 2)).Return([]types.Weather{}, nil).Once()

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return realNow },
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				Location: &types.SiteLocation{
					TimeZone: "UTC",
				},
			},
		}

		energy, weather, err := srv.getCombinedHistory(context.Background(), "site1", settings, historyStart, queryEnd, summaries)
		require.NoError(t, err)
		assert.Len(t, energy, 1)
		assert.Len(t, weather, 1)
		mockS.AssertExpectations(t)
	})

	t.Run("SixHourPostMidnightDeferralAndBehindWarningGuards", func(t *testing.T) {
		today := time.Date(2026, 7, 20, 0, 0, 0, 0, chicagoLoc)
		yesterday := today.AddDate(0, 0, -1)
		twoDaysAgo := today.AddDate(0, 0, -2)
		historyStart := today.AddDate(0, 0, -35)

		summaries := []types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart:   twoDaysAgo,
						TimeLocation: chicagoLoc.String(),
						Hourly:       []types.EnergyStats{{TSHourStart: twoDaysAgo, HomeKWH: 1.0}},
					},
				},
				Weather: []types.Weather{
					{
						TSDayStart:    twoDaysAgo,
						TimeLocation:  chicagoLoc.String(),
						ForecastHours: []types.HourlyWeather{{TSHourStart: twoDaysAgo, TemperatureC: 20}},
					},
				},
			},
		}

		locSettings := settingsWithVersion{
			Settings: types.Settings{
				Location: &types.SiteLocation{
					TimeZone:  chicagoLoc.String(),
					Latitude:  41.8781,
					Longitude: -87.6298,
				},
			},
		}

		// 1. At 03:00 local (< 6h after local midnight): yesterday is fetched & merged, zero behind warnings emitted.
		earlyNow := today.Add(3 * time.Hour)
		mockEarly := &mockStorage{}
		mockEarly.On("GetEnergyHistory", mock.Anything, "site1", yesterday, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   yesterday,
				TimeLocation: chicagoLoc.String(),
				Hourly:       []types.EnergyStats{{TSHourStart: yesterday.Add(12 * time.Hour), HomeKWH: 2.5}},
			},
		}, nil).Once()
		mockEarly.On("GetWeather", mock.Anything, "site1", yesterday, today.AddDate(0, 0, 2)).Return([]types.Weather{
			{
				TSDayStart:    yesterday,
				TimeLocation:  chicagoLoc.String(),
				ForecastHours: []types.HourlyWeather{{TSHourStart: yesterday.Add(12 * time.Hour), TemperatureC: 25}},
			},
		}, nil).Once()

		var warnBuf bytes.Buffer
		warnLogger := slog.New(slog.NewJSONHandler(&warnBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		ctxEarly := log.With(context.Background(), warnLogger)

		srvEarly := &Server{storage: mockEarly, nowFunc: func() time.Time { return earlyNow }}
		energy, weatherRes, err := srvEarly.getCombinedHistory(ctxEarly, "site1", locSettings, historyStart, earlyNow, summaries)
		require.NoError(t, err)
		assert.Len(t, energy, 2, "yesterday unsummarized energy must still be merged during 00:00-06:00 window")
		assert.Len(t, weatherRes, 2, "yesterday unsummarized weather must still be merged during 00:00-06:00 window")
		assert.Empty(t, warnBuf.String(), "must emit zero behind warnings between 00:00 and 06:00 local")
		mockEarly.AssertExpectations(t)

		// 2. At 07:00 local (>= 6h after local midnight) with unsummarized yesterday records: emits behind warning.
		lateNow := today.Add(7 * time.Hour)
		mockLate := &mockStorage{}
		mockLate.On("GetEnergyHistory", mock.Anything, "site1", yesterday, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   yesterday,
				TimeLocation: chicagoLoc.String(),
				Hourly:       []types.EnergyStats{{TSHourStart: yesterday.Add(12 * time.Hour), HomeKWH: 2.5}},
			},
		}, nil).Once()
		mockLate.On("GetWeather", mock.Anything, "site1", yesterday, today.AddDate(0, 0, 2)).Return([]types.Weather{}, nil).Once()

		warnBuf.Reset()
		srvLate := &Server{storage: mockLate, nowFunc: func() time.Time { return lateNow }}
		_, _, err = srvLate.getCombinedHistory(log.With(context.Background(), warnLogger), "site1", locSettings, historyStart, lateNow, summaries)
		require.NoError(t, err)
		assert.Contains(t, warnBuf.String(), "energy history summary is behind")
		assert.NotContains(t, warnBuf.String(), "weather history summary is behind", "no unsummarized weather <= yesterday means zero weather behind warning")
		mockLate.AssertExpectations(t)

		// 3. ESS outage (no unsummarized energy <= yesterday, only today's data) and nil Location: zero warnings and zero GetWeather calls.
		nilLocSettings := settingsWithVersion{
			Settings: types.Settings{Location: nil},
		}
		mockOutage := &mockStorage{}
		mockOutage.On("GetEnergyHistory", mock.Anything, "site1", yesterday, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   today,
				TimeLocation: chicagoLoc.String(),
				Hourly:       []types.EnergyStats{{TSHourStart: today.Add(time.Hour), HomeKWH: 1.0}},
			},
		}, nil).Once()

		warnBuf.Reset()
		srvOutage := &Server{storage: mockOutage, nowFunc: func() time.Time { return lateNow }}
		energyOutage, weatherOutage, err := srvOutage.getCombinedHistory(log.With(context.Background(), warnLogger), "site1", nilLocSettings, historyStart, lateNow, summaries)
		require.NoError(t, err)
		assert.Len(t, energyOutage, 2)
		assert.Len(t, weatherOutage, 1)
		assert.Empty(t, warnBuf.String(), "must emit zero behind warnings during ESS outage or with nil settings.Location")
		mockOutage.AssertNotCalled(t, "GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockOutage.AssertExpectations(t)
	})

	t.Run("TrimsSummaryAndUnsummarizedDaysBeforeHistoryStart", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 12, 0, 0, 0, chicagoLoc)
		today := truncateDay(now)
		yesterday := today.AddDate(0, 0, -1)
		historyStart := today.AddDate(0, 0, -5)
		tooOld := historyStart.AddDate(0, 0, -3)

		summaries := []types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: tooOld, TimeLocation: chicagoLoc.String(), Hourly: []types.EnergyStats{{TSHourStart: tooOld, HomeKWH: 9}}},
					{TSDayStart: yesterday, TimeLocation: chicagoLoc.String(), Hourly: []types.EnergyStats{{TSHourStart: yesterday, HomeKWH: 2}}},
				},
				Weather: []types.Weather{
					{TSDayStart: tooOld, TimeLocation: chicagoLoc.String(), ForecastHours: []types.HourlyWeather{{TSHourStart: tooOld, TemperatureC: 10}}},
					{TSDayStart: yesterday, TimeLocation: chicagoLoc.String(), ForecastHours: []types.HourlyWeather{{TSHourStart: yesterday, TemperatureC: 22}}},
				},
			},
		}

		mockS := &mockStorage{}
		mockS.On("GetEnergyHistory", mock.Anything, "site1", today, today.AddDate(0, 0, 1)).Return([]types.DailyEnergyStats{
			{TSDayStart: tooOld, TimeLocation: chicagoLoc.String(), Hourly: []types.EnergyStats{{TSHourStart: tooOld, HomeKWH: 9}}},
			{TSDayStart: today, TimeLocation: chicagoLoc.String(), Hourly: []types.EnergyStats{{TSHourStart: today, HomeKWH: 3}}},
		}, nil).Once()
		mockS.On("GetWeather", mock.Anything, "site1", today, today.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: tooOld, TimeLocation: chicagoLoc.String(), ForecastHours: []types.HourlyWeather{{TSHourStart: tooOld, TemperatureC: 10}}},
		}, nil).Once()

		srv := &Server{storage: mockS, nowFunc: func() time.Time { return now }}
		locSettings := settingsWithVersion{
			Settings: types.Settings{
				Location: &types.SiteLocation{TimeZone: chicagoLoc.String(), Latitude: 41.8, Longitude: -87.6},
			},
		}

		energy, weatherRes, err := srv.getCombinedHistory(context.Background(), "site1", locSettings, historyStart, now, summaries)
		require.NoError(t, err)
		if assert.Len(t, energy, 2) {
			assert.Equal(t, "2026-07-19", energy[0].TSDayStart.In(chicagoLoc).Format("2006-01-02"))
			assert.Equal(t, "2026-07-20", energy[1].TSDayStart.In(chicagoLoc).Format("2006-01-02"))
		}
		if assert.Len(t, weatherRes, 1) {
			assert.Equal(t, "2026-07-19", weatherRes[0].TSDayStart.In(chicagoLoc).Format("2006-01-02"))
		}
		mockS.AssertExpectations(t)
	})
}

func TestGetSummaryLatestDates(t *testing.T) {
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	d4 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	t.Run("EmptySummaries", func(t *testing.T) {
		e, w := getSummaryLatestDates(nil, time.UTC)
		assert.True(t, e.IsZero())
		assert.True(t, w.IsZero())

		e, w = getSummaryLatestDates([]types.HistorySummary{}, time.UTC)
		assert.True(t, e.IsZero())
		assert.True(t, w.IsZero())

		e, w = getSummaryLatestDates([]types.HistorySummary{{}}, time.UTC)
		assert.True(t, e.IsZero())
		assert.True(t, w.IsZero())
	})

	t.Run("EnergyOnlySummaries", func(t *testing.T) {
		summaries := []types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: d2},
					{TSDayStart: d1},
				},
			},
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: d3},
				},
			},
		}
		e, w := getSummaryLatestDates(summaries, time.UTC)
		assert.Equal(t, d3, e)
		assert.True(t, w.IsZero())
	})

	t.Run("WeatherOnlySummaries", func(t *testing.T) {
		summaries := []types.HistorySummary{
			{
				Weather: []types.Weather{
					{TSDayStart: d1},
					{TSDayStart: d3},
				},
			},
			{
				Weather: []types.Weather{
					{TSDayStart: d2},
				},
			},
		}
		e, w := getSummaryLatestDates(summaries, time.UTC)
		assert.True(t, e.IsZero())
		assert.Equal(t, d3, w)
	})

	t.Run("MixedEnergyAndWeatherTrackedSeparately", func(t *testing.T) {
		weatherNewer := []types.HistorySummary{
			{
				Energy:  []types.DailyEnergyStats{{TSDayStart: d2}},
				Weather: []types.Weather{{TSDayStart: d4}},
			},
		}
		e, w := getSummaryLatestDates(weatherNewer, time.UTC)
		assert.Equal(t, d2, e)
		assert.Equal(t, d4, w)

		energyNewer := []types.HistorySummary{
			{
				Energy:  []types.DailyEnergyStats{{TSDayStart: d4}},
				Weather: []types.Weather{{TSDayStart: d3}},
			},
		}
		e, w = getSummaryLatestDates(energyNewer, time.UTC)
		assert.Equal(t, d4, e)
		assert.Equal(t, d3, w)
	})

	t.Run("LocalizesDatesToSiteTimezone", func(t *testing.T) {
		sydLoc, err := time.LoadLocation("Australia/Sydney")
		require.NoError(t, err)
		sydDay := time.Date(2026, 6, 10, 0, 0, 0, 0, sydLoc)
		// Stored as UTC instant 2026-06-09T14:00:00Z after JSON round-trip
		summaries := []types.HistorySummary{
			{
				Energy:  []types.DailyEnergyStats{{TSDayStart: sydDay.UTC(), TimeLocation: sydLoc.String()}},
				Weather: []types.Weather{{TSDayStart: sydDay.UTC(), TimeLocation: sydLoc.String()}},
			},
		}
		e, w := getSummaryLatestDates(summaries, sydLoc)
		assert.Equal(t, sydDay, e)
		assert.Equal(t, sydDay, w)
	})
}
