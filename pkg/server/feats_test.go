package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	ffServerTestAlpha = feats.Bool("serverTestAlpha", false)
	ffServerTestBeta  = feats.Bool("serverTestBeta", true)
)

func TestServerFeats(t *testing.T) {
	t.Run("CacheTTLAndSkipRebuildWhenUpdatedAtUnchanged", func(t *testing.T) {
		mockS := &mockStorage{}
		// Start mid-interval (12:03:20) to verify first refresh aligns to the 12:05:00 5-minute boundary
		now := time.Date(2026, 11, 3, 12, 3, 20, 0, time.UTC)
		srv := &Server{
			storage: mockS,
			release: "production",
			nowFunc: func() time.Time { return now },
		}

		t1 := now.Add(-10 * time.Minute)
		cfg1 := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}

		// 1st call: cold cache -> fetches from storage and builds evaluator
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(cfg1, t1, nil).Once()

		ctx1 := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctx1))
		assert.True(t, ffServerTestBeta.Enabled(ctx1))
		eval1 := srv.featsEvaluator
		require.NotNil(t, eval1)

		// Before 12:05:00 boundary (1m39s later at 12:04:59): uses cached evaluator without calling storage
		now = now.Add(1*time.Minute + 39*time.Second)
		ctx2 := srv.initFeats(context.Background(), "site-2")
		assert.False(t, ffServerTestAlpha.Enabled(ctx2))
		assert.Same(t, eval1, srv.featsEvaluator)

		// At 12:05:00 boundary (1s later): refreshes early to sync to 5-minute cadence; updatedAt is still t1 -> skips rebuilding evaluator
		now = now.Add(1 * time.Second)
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(cfg1, t1, nil).Once()

		ctx3 := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctx3))
		assert.Same(t, eval1, srv.featsEvaluator)

		// 4m59s later (12:09:59): still within the synced 5-minute window, uses cached evaluator
		now = now.Add(4*time.Minute + 59*time.Second)
		ctx3Cached := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctx3Cached))
		assert.Same(t, eval1, srv.featsEvaluator)

		// At next 5-minute boundary (12:10:00): updatedAt advances to t2 -> rebuilds evaluator
		now = now.Add(1 * time.Second)
		t2 := now.Add(-1 * time.Minute)
		cfg2 := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-2"}, Value: true},
				},
				"serverTestBeta": {
					{SitePercent: 100, Value: false},
				},
			},
		}
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(cfg2, t2, nil).Once()

		ctx4 := srv.initFeats(context.Background(), "site-2")
		assert.True(t, ffServerTestAlpha.Enabled(ctx4))
		assert.False(t, ffServerTestBeta.Enabled(ctx4))
		assert.NotSame(t, eval1, srv.featsEvaluator)

		mockS.AssertExpectations(t)
	})

	t.Run("ErrorFallbackColdAndWarm", func(t *testing.T) {
		mockS := &mockStorage{}
		now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage: mockS,
			release: "staging",
			nowFunc: func() time.Time { return now },
		}

		// Cold start error -> falls back to defaults and does not fail
		mockS.On("GetFeatureConfig", mock.Anything, "staging").Return(feats.Config{}, time.Time{}, errors.New("firestore unavailable")).Once()

		ctxCold := srv.initFeats(context.Background(), "site-1")
		assert.False(t, ffServerTestAlpha.Enabled(ctxCold))
		assert.True(t, ffServerTestBeta.Enabled(ctxCold))

		// 30 seconds later -> still within 1-minute error retry backoff, does not call storage
		now = now.Add(30 * time.Second)
		ctxRetryWait := srv.initFeats(context.Background(), "site-1")
		assert.False(t, ffServerTestAlpha.Enabled(ctxRetryWait))

		// At 1 minute after failure -> retries storage and succeeds
		now = now.Add(30 * time.Second)
		t1 := now
		cfg := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}
		mockS.On("GetFeatureConfig", mock.Anything, "staging").Return(cfg, t1, nil).Once()

		ctxWarm := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctxWarm))

		// 5 minutes later -> storage error, retains previous valid evaluator and schedules retry in 1 minute
		now = now.Add(5 * time.Minute)
		mockS.On("GetFeatureConfig", mock.Anything, "staging").Return(feats.Config{}, time.Time{}, errors.New("transient error")).Once()

		ctxAfterErr := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctxAfterErr))

		// 1 minute later -> retries storage again
		now = now.Add(1 * time.Minute)
		mockS.On("GetFeatureConfig", mock.Anything, "staging").Return(cfg, t1, nil).Once()

		ctxAfterRetry := srv.initFeats(context.Background(), "site-1")
		assert.True(t, ffServerTestAlpha.Enabled(ctxAfterRetry))

		mockS.AssertExpectations(t)
	})

	t.Run("AuthMiddlewareAndGetSettingsReturnsFeatures", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		token := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-123")
		mockS := &mockStorage{}
		now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

		srv := &Server{
			storage:              mockS,
			release:              "production",
			sessionEncryptionKey: "12345678901234567890123456789012",
			encryptionKey:        "12345678901234567890123456789012",
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			nowFunc: func() time.Time { return now },
		}

		mockS.On("GetUser", mock.Anything, "google:user-123").Return(types.User{
			ID:    "google:user-123",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
		}, nil)
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID: "site-1",
			Permissions: []types.SitePermissions{
				{UserID: "google:user-123"},
			},
		}, nil)
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}, now, nil).Once()
		mockS.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			DryRun:        true,
			MinBatterySOC: 20,
			Release:       "production",
		}, types.CurrentSettingsVersion, now, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/settings?siteID=site-1", nil)
		req.AddCookie(&http.Cookie{
			Name:  authTokenCookie,
			Value: token,
		})
		w := httptest.NewRecorder()

		handler := srv.authMiddleware(http.HandlerFunc(srv.handleGetSettings))
		handler.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp SettingsRes
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		if assert.NotNil(t, resp.Features) {
			assert.True(t, resp.Features["serverTestAlpha"])
			assert.True(t, resp.Features["serverTestBeta"])
		}
		mockS.AssertExpectations(t)
	})
}
