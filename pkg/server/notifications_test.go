package server

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func generateTestVAPIDKeys(t *testing.T) (string, string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)

	privB64 := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubB64 := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())

	return privB64, pubB64
}

func TestParseVAPIDPrivateKey(t *testing.T) {
	t.Run("ValidKey", func(t *testing.T) {
		privB64, pubB64 := generateTestVAPIDKeys(t)
		priv, pub, err := parseVAPIDPrivateKey(privB64)
		require.NoError(t, err)
		assert.NotNil(t, priv)
		assert.Equal(t, pubB64, pub)
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		_, _, err := parseVAPIDPrivateKey("not-valid-base64-!@#$")
		assert.Error(t, err)
	})

	t.Run("InvalidLength", func(t *testing.T) {
		shortKey := base64.RawURLEncoding.EncodeToString([]byte("too-short"))
		_, _, err := parseVAPIDPrivateKey(shortKey)
		assert.ErrorContains(t, err, "invalid private key length")
	})
}

func TestGenerateMorningSummary(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	status := types.SystemStatus{
		BatterySOC:         74.0,
		BatteryCapacityKWH: 13.6,
		Timestamp:          time.Date(2026, 9, 4, 7, 0, 0, 0, loc),
	}

	hitCapacityAt := time.Date(2026, 9, 4, 13, 15, 0, 0, loc) // 1:15 PM
	peakSolarKWH := 40.0

	t.Run("MetricsHeavyWithCapacityETA", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "metrics_heavy", status, 38.4, 33.4, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, title, "10.1 kWh")
		assert.Contains(t, title, "38.4 kWh Solar")
		assert.Contains(t, body, "+15% vs yesterday")
		assert.Contains(t, body, "Full charge expected by 1:15 PM")
	})

	t.Run("MetricsHeavyProjectedPeakHigher", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 88.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "-60% vs yesterday")
		assert.Contains(t, body, "Battery projected to peak at ~88% today.")
	})

	t.Run("MetricsHeavyProjectedPeakNotCharging", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 74.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "-60% vs yesterday")
		assert.Contains(t, body, "Battery not projected to charge today (currently 74%).")
	})

	t.Run("MetricsHeavyProjectedPeakMarginalNotCharging", func(t *testing.T) {
		// Battery is at 74%, projected peak is 76% (+2% < 5% min delta). Should be treated as not charging.
		title, body := generateMorningSummary(t.Context(), "metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 76.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "Battery not projected to charge today (currently 74%).")
	})

	t.Run("HomePlannerGreatSolar", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "home_planner", status, 35.0, 30.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "☀️ Great Solar Day Ahead", title)
		assert.Contains(t, body, "Full battery expected by 1:15 PM")
	})

	t.Run("HomePlannerClearSkiesAhead", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "home_planner", status, 35.0, 30.0, peakSolarKWH, time.Time{}, 88.0, loc)
		assert.Equal(t, "☀️ Clear Skies Ahead", title)
		assert.Contains(t, body, "Strong solar today (+17% vs yesterday) will help cover daytime home usage.")
	})

	t.Run("HomePlannerModerateSolar", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "home_planner", status, 24.0, 30.0, peakSolarKWH, time.Time{}, 80.0, loc)
		assert.Equal(t, "⛅ Moderate Solar Outlook", title)
		assert.Contains(t, body, "Moderate solar expected today (-20% vs yesterday)")
	})

	t.Run("HomePlannerLowSolar", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "home_planner", status, 8.0, 30.0, peakSolarKWH, time.Time{}, 74.0, loc)
		assert.Equal(t, "☁️ Low Solar Outlook", title)
		assert.Contains(t, body, "Solar will be limited today (-73% vs yesterday). Consider avoiding heavy loads.")
	})

	t.Run("ExecutiveSummary", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "executive", status, 38.0, 38.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "☀️ 38.0 kWh Solar Expected • 🔋 74% SOC", title)
		assert.Contains(t, body, "Great solar today; battery will fully top off by 1:15 PM")
	})

	t.Run("AutonomousPilot", func(t *testing.T) {
		title, body := generateMorningSummary(t.Context(), "pilot", status, 38.0, 30.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "🤖 RateRudder: Morning Outlook", title)
		assert.Contains(t, body, "Forecast shows 38.0 kWh solar refilling battery by 1:15 PM")
		assert.Contains(t, body, "Optimizing daytime self-consumption")
	})
}

func TestGenerateEveningSummary(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	status := types.SystemStatus{
		BatterySOC:         85.0,
		BatteryCapacityKWH: 13.6,
		Timestamp:          time.Date(2026, 9, 4, 20, 0, 0, 0, loc),
	}

	hitDeficitAt := time.Date(2026, 9, 5, 1, 15, 0, 0, loc) // 1:15 AM

	t.Run("HomePlannerNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "home_planner", status, 42.0, 18.0, 20.0, 0.0, 20.0, time.Time{}, time.Time{}, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Projected to power home through the night until tomorrow's solar")
		assert.Contains(t, body, "85%")
	})

	t.Run("HomePlannerWithDeficitETA", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "home_planner", status, 42.0, 18.0, 20.0, 0.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Projected to supply home until ~1:15 AM.")
		assert.Contains(t, body, "85%")
	})

	t.Run("HomePlannerWithScheduledCharge", func(t *testing.T) {
		scheduledChargeAt := time.Date(2026, 9, 5, 2, 0, 0, 0, loc)
		title, body := generateEveningSummary(t.Context(), "home_planner", status, 42.0, 18.0, 20.0, 0.0, 20.0, time.Time{}, scheduledChargeAt, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Scheduled to charge from the grid at ~2:00 AM.")
	})

	t.Run("HomePlannerLowReserve", func(t *testing.T) {
		lowStatus := status
		lowStatus.BatterySOC = 18.0
		title, body := generateEveningSummary(t.Context(), "home_planner", lowStatus, 12.0, 25.0, 0.0, 10.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Reserve is low; home will switch to grid power shortly")
		assert.Contains(t, body, "18%")
	})

	t.Run("ExecutiveSummaryWithExport", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "executive", status, 42.1, 18.0, 15.5, 0.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🌙 42.1 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "15.5 kWh exported to the grid")
	})

	t.Run("ExecutiveSummaryFullyCovering", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "executive", status, 25.0, 20.0, 0.0, 0.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🌙 25.0 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "fully covering home needs")
	})

	t.Run("ExecutiveSummaryPartiallyCovering", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "executive", status, 10.0, 20.0, 0.0, 10.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🌙 10.0 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "covered 50% of home use")
	})

	t.Run("AutonomousPilotWithDeficit", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "pilot", status, 42.0, 18.0, 15.0, 0.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "will supply home until ~1:15 AM.")
	})

	t.Run("AutonomousPilotWithScheduledCharge", func(t *testing.T) {
		scheduledChargeAt := time.Date(2026, 9, 5, 2, 0, 0, 0, loc)
		title, body := generateEveningSummary(t.Context(), "pilot", status, 42.0, 18.0, 15.0, 0.0, 20.0, time.Time{}, scheduledChargeAt, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "Scheduled to charge from the grid at ~2:00 AM.")
	})

	t.Run("AutonomousPilotNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "pilot", status, 42.0, 18.0, 15.0, 0.0, 20.0, time.Time{}, time.Time{}, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "projected to power home through sunrise")
	})

	t.Run("MetricsHeavyWithDeficit", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "metrics_heavy", status, 42.0, 18.0, 15.0, 0.0, 20.0, hitDeficitAt, time.Time{}, loc)
		assert.Contains(t, title, "42.0 kWh Solar")
		assert.Contains(t, title, "85% SOC")
		assert.Contains(t, body, "42.0 kWh solar")
		assert.Contains(t, body, "18.0 kWh home")
		assert.Contains(t, body, "15.0 kWh exported")
		assert.Contains(t, body, "powers home until ~1:15 AM")
	})

	t.Run("MetricsHeavyWithScheduledCharge", func(t *testing.T) {
		scheduledChargeAt := time.Date(2026, 9, 5, 2, 0, 0, 0, loc)
		_, body := generateEveningSummary(t.Context(), "metrics_heavy", status, 42.0, 18.0, 15.0, 0.0, 20.0, time.Time{}, scheduledChargeAt, loc)
		assert.Contains(t, body, "scheduled to charge from grid at ~2:00 AM")
	})

	t.Run("MetricsHeavyNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary(t.Context(), "metrics_heavy", status, 42.0, 18.0, 0.0, 5.0, 20.0, time.Time{}, time.Time{}, loc)
		assert.Contains(t, title, "42.0 kWh Solar")
		assert.Contains(t, title, "85% SOC")
		assert.Contains(t, body, "5.0 kWh imported")
		assert.Contains(t, body, "powers home through sunrise")
	})

	t.Run("MetricsHeavyLowReserve", func(t *testing.T) {
		lowStatus := status
		lowStatus.BatterySOC = 12.0
		title, body := generateEveningSummary(t.Context(), "metrics_heavy", lowStatus, 47.0, 51.7, 7.1, 0.0, 25.0, time.Time{}, time.Time{}, loc)
		assert.Contains(t, title, "47.0 kWh Solar")
		assert.Contains(t, title, "12% SOC")
		assert.Contains(t, body, "reserve is low; home will switch to grid power shortly")
	})

	t.Run("PilotLowReserve", func(t *testing.T) {
		lowStatus := status
		lowStatus.BatterySOC = 12.0
		title, body := generateEveningSummary(t.Context(), "pilot", lowStatus, 47.0, 51.7, 7.1, 0.0, 25.0, time.Time{}, time.Time{}, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "Reserve is low; home will switch to grid power shortly")
	})

	t.Run("HomePlannerLowReserveNoDeficit", func(t *testing.T) {
		lowStatus := status
		lowStatus.BatterySOC = 12.0
		title, body := generateEveningSummary(t.Context(), "home_planner", lowStatus, 47.0, 51.7, 7.1, 0.0, 25.0, time.Time{}, time.Time{}, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Reserve is low; home will switch to grid power shortly")
	})
}

func TestEncryptWebPushPayload(t *testing.T) {
	t.Run("ValidEncryption", func(t *testing.T) {
		// Generate client keypair & auth
		clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
		require.NoError(t, err)
		clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())

		authBytes := make([]byte, 16)
		_, err = rand.Read(authBytes)
		require.NoError(t, err)
		clientAuthB64 := base64.RawURLEncoding.EncodeToString(authBytes)

		plaintext := []byte(`{"title":"Test","body":"Hello World"}`)
		ciphertext, encoding, err := encryptWebPushPayload(plaintext, clientPubB64, clientAuthB64)
		require.NoError(t, err)
		assert.Equal(t, "aes128gcm", encoding)
		assert.NotEmpty(t, ciphertext)
		// RFC 8188 header length: 16 (salt) + 4 (rs) + 1 (idlen) + 65 (key) = 86 bytes minimum
		assert.Greater(t, len(ciphertext), 86)
	})

	t.Run("InvalidSubscriberKey", func(t *testing.T) {
		_, _, err := encryptWebPushPayload([]byte("test"), "invalid-key", "invalid-auth")
		assert.Error(t, err)
	})
}

func TestWebPushTopic(t *testing.T) {
	t.Run("EmptyTag", func(t *testing.T) {
		assert.Equal(t, "", webPushTopic(""))
	})

	t.Run("ShortCompliantTag", func(t *testing.T) {
		tag := "raterudder-home"
		assert.Equal(t, "raterudder-home", webPushTopic(tag))
	})

	t.Run("SpecialCharactersStripped", func(t *testing.T) {
		tag := "raterudder@site#1!"
		assert.Equal(t, "rateruddersite1", webPushTopic(tag))
	})

	t.Run("LongTagHashedToAtMost32Chars", func(t *testing.T) {
		longTag := "raterudder-very-long-site-id-12345-price-spike-alert"
		topic := webPushTopic(longTag)
		assert.NotEmpty(t, topic)
		assert.LessOrEqual(t, len(topic), 32)
		// Deterministic
		assert.Equal(t, topic, webPushTopic(longTag))
		// Base64URL character compliance check
		for _, r := range topic {
			valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
			assert.True(t, valid, "Topic contains invalid character: %c", r)
		}
	})
}

func createTestEndpointsServer(t *testing.T, mockS *storagemock.MockDatabase) (*Server, http.Handler, string, string) {
	t.Helper()
	privB64, pubB64 := generateTestVAPIDKeys(t)
	privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
	require.NoError(t, err)

	srv := &Server{
		storage:            mockS,
		vapidKey:           privKey,
		vapidPublicKey:     pubKey,
		vapidSubject:       "mailto:support@raterudder.com",
		bypassAuth:         true,
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 10),
		sensitiveBurst:     10,
		nowFunc: func() time.Time {
			return time.Date(2026, 9, 4, 7, 10, 0, 0, time.UTC)
		},
	}
	return srv, srv.setupHandler(), privB64, pubB64
}

func TestHandleGetVAPIDPublicKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, pubB64 := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/vapidPublicKey", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
		expectedBytes, err := decodeBase64Key(pubB64)
		require.NoError(t, err)
		assert.Equal(t, expectedBytes, rec.Body.Bytes())
		assert.Equal(t, 65, len(rec.Body.Bytes()))
	})

	t.Run("Disabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage:            mockS,
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 10),
			sensitiveBurst:     10,
		}
		disabledHandler := disabledSrv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/vapidPublicKey", nil)
		rec := httptest.NewRecorder()
		disabledHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestHandleSubscribe(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)
		mockS.On("AddUserPushSubscription", mock.Anything, "fake", mock.Anything).Return(nil).Once()

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint:  "https://fcm.googleapis.com/fcm/send/test-sub-id",
				Keys:      types.PushSubscriptionKeys{P256DH: "test-p256dh", Auth: "test-auth"},
				UserAgent: "TestBrowser",
			},
			SendTest: false,
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Disabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage:            mockS,
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 10),
			sensitiveBurst:     10,
		}
		disabledHandler := disabledSrv.setupHandler()

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader([]byte("{}")))
		rec := httptest.NewRecorder()
		disabledHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader([]byte("invalid json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint: "",
			},
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("AddUserPushSubscription", mock.Anything, "fake", mock.Anything).Return(errors.New("db error")).Once()

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint:  "https://fcm.googleapis.com/fcm/send/test-sub-id",
				Keys:      types.PushSubscriptionKeys{P256DH: "test-p256dh", Auth: "test-auth"},
				UserAgent: "TestBrowser",
			},
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleUnsubscribe(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("RemoveUserPushSubscription", mock.Anything, "fake", "https://fcm.googleapis.com/fcm/send/test-sub-id").Return(nil).Once()

		unsubReq := unsubscribeRequest{
			Endpoint: "https://fcm.googleapis.com/fcm/send/test-sub-id",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingEndpoint", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		unsubReq := unsubscribeRequest{
			Endpoint: "",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("RemoveUserPushSubscription", mock.Anything, "fake", "https://fcm.googleapis.com/fcm/send/test-sub-id").Return(errors.New("db error")).Once()

		unsubReq := unsubscribeRequest{
			Endpoint: "https://fcm.googleapis.com/fcm/send/test-sub-id",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleReplaceSubscription(t *testing.T) {
	t.Run("ReplacementSuccess", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		prevEndpoint := "https://updates.push.services.mozilla.com/wpush/v2/old-endpoint"
		prevAuth := "test-prev-auth"

		mockS.On("ReplaceUserPushSubscription", mock.Anything, prevEndpoint, prevAuth, mock.MatchedBy(func(sub *types.PushSubscription) bool {
			return sub != nil && sub.Endpoint == "https://updates.push.services.mozilla.com/wpush/v2/new-endpoint" && sub.Keys.Auth == "test-new-auth"
		})).Return(nil).Once()

		reqBody := replaceSubscriptionRequest{
			PrevEndpoint: prevEndpoint,
			PrevAuth:     prevAuth,
			Subscription: &types.PushSubscription{
				Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/new-endpoint",
				Keys: types.PushSubscriptionKeys{
					P256DH: "test-new-p256dh",
					Auth:   "test-new-auth",
				},
				UserAgent: "TestBrowser",
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp["replaced"])
	})

	t.Run("RevocationSuccess", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		prevEndpoint := "https://updates.push.services.mozilla.com/wpush/v2/old-endpoint"
		prevAuth := "test-prev-auth"

		mockS.On("ReplaceUserPushSubscription", mock.Anything, prevEndpoint, prevAuth, (*types.PushSubscription)(nil)).Return(nil).Once()

		reqBody := replaceSubscriptionRequest{
			PrevEndpoint: prevEndpoint,
			PrevAuth:     prevAuth,
			Subscription: nil,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.False(t, resp["replaced"])
	})

	t.Run("NotificationsDisabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage:            mockS,
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 10),
			sensitiveBurst:     10,
		}
		disabledHandler := disabledSrv.setupHandler()

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader([]byte("{}")))
		rec := httptest.NewRecorder()
		disabledHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader([]byte("not-json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		reqBody := replaceSubscriptionRequest{
			PrevEndpoint: "",
			PrevAuth:     "",
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingKeysOnReplacement", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		reqBody := replaceSubscriptionRequest{
			PrevEndpoint: "https://updates.push.services.mozilla.com/wpush/v2/old-endpoint",
			PrevAuth:     "test-prev-auth",
			Subscription: &types.PushSubscription{
				Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/new-endpoint",
				// Keys missing
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("StorageForbidden", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		prevEndpoint := "https://updates.push.services.mozilla.com/wpush/v2/old-endpoint"
		prevAuth := "invalid-auth"

		mockS.On("ReplaceUserPushSubscription", mock.Anything, prevEndpoint, prevAuth, (*types.PushSubscription)(nil)).Return(errors.New("invalid subscription authentication secret")).Once()

		reqBody := replaceSubscriptionRequest{
			PrevEndpoint: prevEndpoint,
			PrevAuth:     prevAuth,
			Subscription: nil,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/replace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestHandleUpdateNotificationSettings(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader([]byte("bad json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingSiteID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		updateReq := updateNotificationSettingsRequest{
			SiteID: "",
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidMorningHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    25, // Invalid hour
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingMorningFlavorWhenEnabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "", // Missing flavor
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidEveningHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				EveningSummaryEnabled: true,
				EveningSummaryHour:    24, // Invalid evening hour
				EveningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingEveningFlavorWhenEnabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "", // Missing evening flavor when enabled
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("DisabledWithEmptyFlavors", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.MatchedBy(func(s types.UserNotificationSettings) bool {
			return s.MorningSummaryFlavor == defaultSummaryFlavor && s.EveningSummaryFlavor == defaultSummaryFlavor
		})).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: false,
				MorningSummaryHour:    0,
				MorningSummaryFlavor:  "",
				EveningSummaryEnabled: false,
				EveningSummaryHour:    0,
				EveningSummaryFlavor:  "",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidPriceSpikeSensitivity", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				PriceSpikeAlert:       "ultra_extreme", // Invalid sensitivity
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidSolarSensitivity", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled:     true,
				MorningSummaryHour:        8,
				MorningSummaryFlavor:      "home_planner",
				SolarUnderproductionAlert: "invalid_level", // Invalid sensitivity
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("WithAllAnomalyAlerts", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled:     true,
				MorningSummaryHour:        7,
				MorningSummaryFlavor:      "executive",
				EveningSummaryEnabled:     true,
				EveningSummaryHour:        20,
				EveningSummaryFlavor:      "pilot",
				GridOutageAlert:           true,
				PriceSpikeAlert:           "high",
				SolarUnderproductionAlert: "low",
				VPPDispatchAlert:          true,
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(errors.New("db error")).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("ValidQuietPeriods", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.MatchedBy(func(s types.UserNotificationSettings) bool {
			return len(s.QuietPeriods) == 1 && s.QuietPeriods[0].Hours[0].HourStart == 22 && s.QuietPeriods[0].Hours[0].HourEnd == 7
		})).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("InvalidQuietPeriodsEqualHours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 8}},
					},
				},
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "quiet period start and end hours must be between 0 and 23, and start cannot equal end")
	})

	t.Run("InvalidQuietPeriodsOutOfRangeHours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 25, HourEnd: 7}},
					},
				},
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "quiet period start and end hours must be between 0 and 23, and start cannot equal end")
	})
}

func TestHandleGetNotificationSubscriptions(t *testing.T) {
	t.Run("SuccessWithSubscriptions", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv, _, _, _ := createTestEndpointsServer(t, mockS)
		privB64, _ := generateTestVAPIDKeys(t)
		privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
		require.NoError(t, err)
		srv.vapidKey = privKey
		srv.vapidPublicKey = pubKey

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/subscriptions", nil)
		ctx := context.WithValue(req.Context(), userContextKey, types.User{
			ID: "user-123",
			Subscriptions: []types.PushSubscription{
				{ID: "sub-1", Endpoint: "https://example.com/sub-1"},
			},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		srv.handleGetNotificationSubscriptions(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp notificationSubscriptionsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp.NotificationsEnabled)
		if assert.Len(t, resp.Subscriptions, 1) {
			assert.Equal(t, "sub-1", resp.Subscriptions[0].ID)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv, _, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/subscriptions", nil)
		ctx := context.WithValue(req.Context(), userContextKey, types.User{})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		srv.handleGetNotificationSubscriptions(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestHandleNotificationClick(t *testing.T) {
	t.Run("StructuredID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		month := "2026-09"
		id := "2026-09_site-123_a1b2c3d4e5f67890"
		mockS.On("RecordNotificationClick", mock.Anything, siteID, month, id, mock.Anything).Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/click?id=%s", id), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp["recorded"])
	})

	t.Run("SiteWithUnderscores", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "my_home_site_01"
		month := "2026-09"
		id := "2026-09_my_home_site_01_a1b2c3d4e5f67890"
		mockS.On("RecordNotificationClick", mock.Anything, siteID, month, id, mock.Anything).Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/click?id=%s", id), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp["recorded"])
	})

	t.Run("InvalidID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/click?id=short_id", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingParams", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/click", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func createTestPushUser(t *testing.T, userID, endpoint string) types.User {
	t.Helper()
	clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())
	clientAuthB64 := base64.RawURLEncoding.EncodeToString([]byte("1234567812345678"))

	return types.User{
		ID:    userID,
		Email: userID,
		Subscriptions: []types.PushSubscription{
			{
				ID:       "sub-" + userID,
				Endpoint: endpoint,
				Keys:     types.PushSubscriptionKeys{P256DH: clientPubB64, Auth: clientAuthB64},
			},
		},
	}
}

func createTestNotificationServer(t *testing.T, mockS *storagemock.MockDatabase, nowTime time.Time) *Server {
	t.Helper()
	privB64, _ := generateTestVAPIDKeys(t)
	privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
	require.NoError(t, err)

	return &Server{
		storage:         mockS,
		controller:      controller.NewController(),
		vapidKey:        privKey,
		vapidPublicKey:  pubKey,
		vapidSubject:    "mailto:support@raterudder.com",
		gridOutageDelay: 10 * time.Millisecond,
		nowFunc: func() time.Time {
			return nowTime
		},
	}
}

type mockUserStore struct {
	users map[string]types.User
	mu    sync.Mutex
	calls map[string]int
}

func newMockUserStore(users ...types.User) *mockUserStore {
	m := &mockUserStore{
		users: make(map[string]types.User),
		calls: make(map[string]int),
	}
	for _, u := range users {
		m.users[u.ID] = u
	}
	return m
}

func (m *mockUserStore) fetcher() userFetcher {
	return func(ctx context.Context, userID string) (types.User, error) {
		m.mu.Lock()
		m.calls[userID]++
		m.mu.Unlock()
		if u, ok := m.users[userID]; ok {
			return u, nil
		}
		return types.User{}, fmt.Errorf("user not found: %s", userID)
	}
}

func (m *mockUserStore) callCount(userID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[userID]
}

func (m *mockUserStore) totalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, c := range m.calls {
		total += c
	}
	return total
}

func mockUserGetter(users ...types.User) userFetcher {
	return newMockUserStore(users...).fetcher()
}

func TestHandleMorningSummaryNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusMorning := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	mockEnergyHistory := []types.DailyEnergyStats{
		{
			TSDayStart: nowMorning.AddDate(0, 0, -3),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -3), SolarKWH: 25.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -2),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 28.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -1),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 24.0},
			},
		},
	}

	notifData := &dataForNotifications{
		status:        statusMorning,
		settings:      settings,
		energyHistory: mockEnergyHistory,
	}

	t.Run("DispatchWhenHourMatchesAndNotSentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary && l.Flavor == "metrics_heavy"
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("MetricsHeavyProjectedPeakDispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}

		peakStatus := statusMorning
		peakStatus.BatterySOC = 47.0

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowMorning,
					TSEnd:    nowMorning.Add(time.Hour),
					SolarKWH: 20.0,
					EndSOC:   75.0,
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Battery projected to peak at ~75% today.")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        peakStatus,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			plan:          mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("MetricsHeavyProjectedNotChargingDispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}

		decliningStatus := statusMorning
		decliningStatus.BatterySOC = 47.0

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowMorning,
					TSEnd:    nowMorning.Add(time.Hour),
					SolarKWH: 5.0,
					StartSOC: 47.0,
					EndSOC:   30.0,
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Battery not projected to charge today (currently 47%).")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        decliningStatus,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			plan:          mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("CapacityHitDetectionFromHitCapacityAt", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}

		hitTime := time.Date(2026, 9, 4, 13, 30, 0, 0, loc)
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowMorning,
					TSEnd:    hitTime,
					SolarKWH: 25.0,
					EndSOC:   100.0,
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Full charge expected by 1:30 PM")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        statusMorning,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			plan:          mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenLessThan3DaysHistory", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}

		shortHistory := []types.DailyEnergyStats{
			{
				TSDayStart: nowMorning.AddDate(0, 0, -2),
				Hourly: []types.EnergyStats{
					{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 20.0},
				},
			},
			{
				TSDayStart: nowMorning.AddDate(0, 0, -1),
				Hourly: []types.EnergyStats{
					{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 22.0},
				},
			},
		}

		shortData := &dataForNotifications{
			status:        statusMorning,
			settings:      settings,
			energyHistory: shortHistory,
		}

		userStore := newMockUserStore()
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, shortData, nowMorning, getNotifState, userStore.fetcher())
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenAlreadySentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-1",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeMorningSummary,
				Flavor:    "metrics_heavy",
				Success:   true,
			},
		}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenDifferentHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8, // configured for 8 AM, current is 7 AM
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestHandleEveningSummaryNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowEvening := time.Date(2026, 9, 4, 20, 15, 0, 0, loc)

	statusEvening := types.SystemStatus{
		Timestamp:          nowEvening,
		BatterySOC:         82.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            0.0,
		HomeKW:             1.5,
	}

	settings := types.Settings{}

	notifData := &dataForNotifications{
		status:   statusEvening,
		settings: settings,
	}

	t.Run("DispatchWhenHourMatchesAndNotSentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "executive",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeEveningSummary && l.Flavor == "executive"
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowEvening, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("HomePlannerUsesPlanDeficitETA", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "home_planner",
			},
		}
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:    nowEvening,
					TSEnd:      nowEvening.Add(5 * time.Hour), // 1:00 AM
					EndSOC:     15.0,                          // reaches below reserve (20%)
					ReserveSOC: 20.0,
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeEveningSummary &&
				l.Flavor == "home_planner" &&
				strings.Contains(l.Body, "Projected to supply home until ~1:15 AM.")
		})).Return(nil).Once()

		plannerData := &dataForNotifications{
			status:   statusEvening,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), siteID, notifications, plannerData, nowEvening, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenAlreadySentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "executive",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-2",
				TSCreated: nowEvening.Add(-1 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeEveningSummary,
				Flavor:    "executive",
				Success:   true,
			},
		}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowEvening, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenDifferentHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				EveningSummaryEnabled: true,
				EveningSummaryHour:    21, // configured for 9 PM, current is 8 PM
				EveningSummaryFlavor:  "executive",
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), siteID, notifications, notifData, nowEvening, getNotifState, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ImmediateDeficitAtReserve", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "metrics_heavy",
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowEvening,
					TSEnd:    nowEvening.Add(time.Hour),
					StartSOC: 12.0,
					EndSOC:   12.0,
				},
			},
		}

		lowStatus := statusEvening
		lowStatus.BatterySOC = 12.0
		lowStatus.BatteryCapacityKWH = 15.0

		// Settings with a scheduled 25% minimum SOC period covering the evening
		scheduledSettings := types.Settings{
			MinBatterySOC: 5.0, // base setting is 5%
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod: types.TimePeriod{
						Hours: []types.UtilityHourPeriod{{HourStart: 19, HourEnd: 23}},
					},
					MinBatterySOC: 25.0,
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeEveningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "reserve is low; home will switch to grid power shortly") &&
				!strings.Contains(l.Body, "powers home through sunrise")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:   lowStatus,
			settings: scheduledSettings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), siteID, notifications, data, nowEvening, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})
}

func TestHandleGridOutageNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusOutage := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         65.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    true,
		HomeKW:             1.5,
	}

	t.Run("VerifiedAfterDelay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		srv.gridOutageDelay = 10 * time.Millisecond
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridOutage
		})).Return(nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable:    true,
			BatterySOC:         65.0,
			BatteryCapacityKWH: 13.6,
			HomeKW:             1.5,
		}, nil)

		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(ctxWithWg, siteID, notifications, statusOutage, mockEss, nowMorning, getNotifState, mockUserGetter(user))
		wg.Wait()

		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedForTemporaryBlip", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		srv.gridOutageDelay = 10 * time.Millisecond
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		// ESS shows grid came back online before delay expired
		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable: false,
			BatterySOC:      65.0,
		}, nil)

		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(ctxWithWg, siteID, notifications, statusOutage, mockEss, nowMorning, getNotifState, mockUserGetter(user))
		wg.Wait()

		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedWhenAlreadyAlerted", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage-1",
				TSCreated: nowMorning.Add(-10 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   true,
				Muted:     false,
			},
		}, nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable: true,
		}, nil)

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusOutage, mockEss, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredNotification", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage",
				TSCreated: nowMorning.Add(-20 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   true,
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridRestored && !l.Muted
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusRestored, nil, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredSuppressedWhenNoPriorOutage", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
			},
		}
		// No prior outage log
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusRestored, nil, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("OutageMutedDuringQuietPeriod", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowQuiet := time.Date(2026, 9, 4, 2, 0, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowQuiet)
		srv.gridOutageDelay = 10 * time.Millisecond

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridOutage && l.Muted && !l.Success
		})).Return(nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable:    true,
			BatterySOC:         50.0,
			BatteryCapacityKWH: 13.6,
			HomeKW:             1.0,
		}, nil)

		userStore := newMockUserStore()
		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowQuiet)
		srv.handleGridOutageNotifications(ctxWithWg, siteID, notifications, statusOutage, mockEss, nowQuiet, getNotifState, userStore.fetcher())
		wg.Wait()

		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})

	t.Run("OutageAlertSentUponWakeup", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowWakeup := time.Date(2026, 9, 4, 7, 5, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowWakeup)
		srv.gridOutageDelay = 10 * time.Millisecond
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}

		// Prior log was outage but muted
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage-1",
				TSCreated: nowWakeup.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   false,
				Muted:     true,
			},
		}, nil).Once()
		// Now outside quiet period -> should dispatch delivered push!
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridOutage && !l.Muted && l.Success
		})).Return(nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable:    true,
			BatterySOC:         45.0,
			BatteryCapacityKWH: 13.6,
			HomeKW:             1.5,
		}, nil)

		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowWakeup)
		srv.handleGridOutageNotifications(ctxWithWg, siteID, notifications, statusOutage, mockEss, nowWakeup, getNotifState, mockUserGetter(user))
		wg.Wait()

		mockS.AssertExpectations(t)
	})

	t.Run("RestoredMutedDuringQuietPeriod", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowQuiet := time.Date(2026, 9, 4, 3, 0, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowQuiet)

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}
		// Prior log was outage
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage-2",
				TSCreated: nowQuiet.Add(-1 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   false,
				Muted:     true,
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridRestored && l.Muted && !l.Success
		})).Return(nil).Once()

		userStore := newMockUserStore()
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowQuiet)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusRestored, nil, nowQuiet, getNotifState, userStore.fetcher())
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredDeferredAlertSentUponWakeupWhenRecent", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowWakeup := time.Date(2026, 9, 4, 7, 5, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowWakeup)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}
		// Restored 20 minutes ago (within 1 hour)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-muted-restored",
				TSCreated: nowWakeup.Add(-20 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridRestored,
				Success:   false,
				Muted:     true,
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridRestored && !l.Muted && l.Success
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowWakeup)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusRestored, nil, nowWakeup, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredDeferredAlertStaleSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowWakeup := time.Date(2026, 9, 4, 7, 5, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowWakeup)

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				GridOutageAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}
		// Restored 3 hours ago (> 1 hour)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-muted-restored",
				TSCreated: nowWakeup.Add(-3 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridRestored,
				Success:   false,
				Muted:     true,
			},
		}, nil).Once()

		userStore := newMockUserStore()
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowWakeup)
		srv.handleGridOutageNotifications(context.Background(), siteID, notifications, statusRestored, nil, nowWakeup, getNotifState, userStore.fetcher())
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})
}

func TestHandlePriceSpikeNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	t.Run("Dispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -2), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -3), DollarsPerKWH: 0.11, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike && l.Flavor == "medium"
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.50, // Surge to $0.55/kWh
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedForNormalTOUMonday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		// Routine peak price at 8 AM was always $0.35/kWh over past 5 days
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -1), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -2), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -3), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.31, // $0.36 vs typical $0.35 -> difference only $0.01 < $0.15 minDelta
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SensitivityTiers", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		userHigh := createTestPushUser(t, "user-high@test.com", pushServer.URL+"/push/user-high")

		notifications := map[string]types.UserNotificationSettings{
			"user-low@test.com": {
				PriceSpikeAlert: "low",
			},
			"user-high@test.com": {
				PriceSpikeAlert: "high",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -2), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -3), DollarsPerKWH: 0.11, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike && l.UserID == "user-high@test.com" && l.Flavor == "high"
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.17, // Total $0.22/kWh. Delta = $0.22 - $0.17 = $0.05 (>= high's $0.03, but < low's $0.10)
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		userStore := newMockUserStore(userHigh)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, userStore.fetcher())
		assert.Equal(t, 0, userStore.callCount("user-low@test.com"))
		assert.Equal(t, 1, userStore.callCount("user-high@test.com"))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBelowAbsoluteFloor", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "high",
			},
		}

		// Rate increased from $0.05 to $0.17. Even though delta is big, $0.17 < $0.18 absolute floor.
		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.01, GridUseDollarsPerKWH: 0.04},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("Deduplication18Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent price spike alert 4 hours ago (well within 18h window)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike",
				TSCreated: nowMorning.Add(-4 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Success:   true,
			},
		}, nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("AllowedAfter18Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent price spike alert 20 hours ago (> 18h window)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike",
				TSCreated: nowMorning.Add(-20 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Success:   true,
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SolarCoversHomeDuringSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMorning.Add(1 * time.Hour),
					TSEnd:         nowMorning.Add(2 * time.Hour),
					SolarKWH:      5.0,
					LoadKWH:       2.0,
					GridImportKWH: 0.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Solar is projected to cover your home usage during the spike.")
		}
	})

	t.Run("SolarUnder05KWHFallsBackToBattery", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMorning.Add(1 * time.Hour),
					TSEnd:         nowMorning.Add(2 * time.Hour),
					SolarKWH:      0.3, // < 0.5 kWh threshold, so hasSolar should be false
					LoadKWH:       0.3,
					GridImportKWH: 0.0,
					EndSOC:        75.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.NotContains(t, recordedLog.Body, "Solar is projected to cover your home")
			assert.Contains(t, recordedLog.Body, "Battery is at 80% and projected to power your home")
		}
	})

	t.Run("NetLoadOver01KWHFallsBackToBattery", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMorning.Add(1 * time.Hour),
					TSEnd:         nowMorning.Add(2 * time.Hour),
					SolarKWH:      2.0, // >= 0.5 kWh
					LoadKWH:       2.15,
					GridImportKWH: 0.15, // > 0.05 kWh threshold, so solar does not cover all load
					EndSOC:        75.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.NotContains(t, recordedLog.Body, "Solar is projected to cover your home")
			assert.Contains(t, recordedLog.Body, "Battery is at 80% and projected to power your home")
		}
	})

	t.Run("NetLoadUnder01KWHWithMeaningfulSolarCoversHome", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMorning.Add(1 * time.Hour),
					TSEnd:         nowMorning.Add(2 * time.Hour),
					SolarKWH:      0.8,  // >= 0.5 kWh
					LoadKWH:       0.88, // load
					GridImportKWH: 0.0,  // <= 0.05 kWh tolerance
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Solar is projected to cover your home usage during the spike.")
		}
	})

	t.Run("BatteryPowersThroughSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowMorning.Add(1 * time.Hour),
					TSEnd:    nowMorning.Add(2 * time.Hour),
					SolarKWH: 0.0,
					LoadKWH:  2.0,
					EndSOC:   70.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         85.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 85% and projected to power your home through the spike.")
		}
	})

	t.Run("BatteryReachesReserveDuringSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		spikeStart := nowMorning.Add(1 * time.Hour)
		futurePrices := []types.Price{
			{
				TSStart:              spikeStart,
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:    spikeStart,
					TSEnd:      spikeStart.Add(35 * time.Minute),
					SolarKWH:   0.0,
					LoadKWH:    3.0,
					EndSOC:     18.0,
					ReserveSOC: 20.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         35.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 35% and will supply home until ~")
		}
	})

	t.Run("ThirtyMinuteSimulationSlotsCaptured", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		spikeStart := nowMorning.Add(1 * time.Hour) // 7:00 AM
		futurePrices := []types.Price{
			{
				TSStart:              spikeStart,
				TSEnd:                spikeStart.Add(1 * time.Hour), // 8:00 AM
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		// Two 30-minute plan periods spanning 7:00 AM - 7:30 AM and 7:30 AM - 8:00 AM
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       spikeStart,
					TSEnd:         spikeStart.Add(30 * time.Minute),
					DurationHours: 0.5,
					SolarKWH:      0.0,
					LoadKWH:       1.5,
					EndSOC:        30.0,
				},
				{
					TSStart:       spikeStart.Add(30 * time.Minute),
					TSEnd:         spikeStart.Add(60 * time.Minute),
					DurationHours: 0.5,
					SolarKWH:      0.0,
					LoadKWH:       1.5,
					EndSOC:        18.0, // reaches reserve during second slot
					ReserveSOC:    20.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         40.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 40% and will supply home until ~")
		}
	})

	t.Run("BatteryAlreadyAtReserve", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:    nowMorning.Add(1 * time.Hour),
					TSEnd:      nowMorning.Add(2 * time.Hour),
					EndSOC:     20.0,
					ReserveSOC: 20.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         20.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is currently at 20% reserve; your home will draw from the grid during the spike.")
		}
	})

	t.Run("RealTimeCurrentPriceSpikeWithBatteryOutcome", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -2), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -3), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// Real-time current price is spiking right now ($0.35/kWh)
		currentPrice := types.Price{
			TSStart:              nowMorning,
			TSEnd:                nowMorning.Add(1 * time.Hour),
			DollarsPerKWH:        0.30,
			GridUseDollarsPerKWH: 0.05,
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart: nowMorning,
					TSEnd:   nowMorning.Add(1 * time.Hour),
					EndSOC:  70.0,
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: currentPrice,
			futurePrices: []types.Price{},
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         85.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			plan: mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Equal(t, "🚨 Price Spike: $0.35/kWh", recordedLog.Title)
			assert.Contains(t, recordedLog.Body, "Price is $0.35/kWh now and anticipated to last until 8:10 AM.")
			assert.Contains(t, recordedLog.Body, "Battery is at 85% and projected to power your home through the spike.")
			if assert.NotNil(t, recordedLog.Metadata) {
				assert.Equal(t, "0.3500", recordedLog.Metadata["price"])
				assert.Equal(t, "0.3500", recordedLog.Metadata["peakPrice"])
				assert.Equal(t, "85.0", recordedLog.Metadata["currentSOC"])
			}
		}
	})

	t.Run("ForecastHigherPeakAndDuration", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		currentPrice := types.Price{
			TSStart:              nowMorning,
			TSEnd:                nowMorning.Add(1 * time.Hour),
			DollarsPerKWH:        0.25,
			GridUseDollarsPerKWH: 0.05,
		}
		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				TSEnd:                nowMorning.Add(2 * time.Hour),
				DollarsPerKWH:        0.45,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: currentPrice,
			futurePrices: futurePrices,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Equal(t, "🚨 Price Spike: $0.30/kWh (peaking at $0.50 at 8:10 AM)", recordedLog.Title)
			assert.Contains(t, recordedLog.Body, "Price is $0.30/kWh now and expected to rise to $0.50/kWh at 8:10 AM (lasting until 9:10 AM).")
			if assert.NotNil(t, recordedLog.Metadata) {
				assert.Equal(t, "0.3000", recordedLog.Metadata["price"])
				assert.Equal(t, "0.5000", recordedLog.Metadata["peakPrice"])
			}
		}
	})

	t.Run("SuppressedWithin1Hour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent 30 minutes ago (< 1 hour)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-recent",
				TSCreated: nowMorning.Add(-30 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ReAlertBetween1And6HoursOnSignificantChange", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		// Baseline historical prices: 19 at $0.15, 1 at $0.30 (95th percentile is $0.30)
		var hist []types.Price
		for i := 1; i <= 19; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		hist = append(hist, types.Price{
			TSStart:              nowMorning.AddDate(0, 0, -20),
			DollarsPerKWH:        0.25,
			GridUseDollarsPerKWH: 0.05,
		})
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		// Previous alert was sent 2 hours ago for $0.30/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// New price is $0.45/kWh (>= $0.30 * 1.20 = $0.36, and >= 95th percentile ($0.30))
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.40,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBetween1And6HoursIfNotSignificant", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert was sent 2 hours ago for $0.30/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()

		// New price is $0.32/kWh (< $0.30 * 1.20 = $0.36)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.27,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBetween1And6HoursIfPreviouslyWarned", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert already warned of peaking at $0.45/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.25/kWh (peaking at $0.45 at 7:10 AM)",
				Success:   true,
				Metadata: map[string]string{
					"price":     "0.2500",
					"peakPrice": "0.4500",
				},
			},
		}, nil).Once()

		// Price is now $0.45/kWh (not >= $0.45 * 1.20 = $0.54)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.40,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ReAlertAfter6HoursIfPriceDroppedBelow", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		// Historical prices include an hour where price dropped back to $0.15 between alerts
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(-4 * time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert was sent 8 hours ago
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-8h",
				TSCreated: nowMorning.Add(-8 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.35/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3500",
				},
			},
		}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// New spike of $0.35/kWh after price had dropped below
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedAfter6HoursIfPriceRemainedElevated", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}
		// Historical prices: baseline from previous days ($0.15), and all hours between -8h and now stayed elevated at $0.35/kWh
		var hist []types.Price
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{TSStart: nowMorning.AddDate(0, 0, -i), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05})
		}
		for h := 1; h < 8; h++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(h) * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		// Previous alert was sent 8 hours ago for $0.35/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-8h",
				TSCreated: nowMorning.Add(-8 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.35/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3500",
				},
			},
		}, nil).Once()

		// Price is still $0.35/kWh (not a >= 20% surge, and never dropped below)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("HighSensitivityAlertsOnTop10PercentWithoutTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "high",
			},
		}

		// 90 hours at $0.15, 10 hours at $0.30 (including same hour on previous days)
		var hist []types.Price
		for i := 1; i <= 90; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(i*2) * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("MediumSensitivitySuppressesIfNormalForTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}

		// Same history where this time of day is consistently $0.30
		var hist []types.Price
		for i := 1; i <= 90; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(i*2) * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()

		// $0.30 is in top 10% overall, but matches time-of-day baseline ($0.30), so Medium sensitivity suppresses it
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("MediumSensitivityAlertsIfAboveTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
			},
		}

		// History where this time of day is $0.20 ($0.15 + $0.05)
		var hist []types.Price
		for i := 1; i <= 20; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.15,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// $0.35 surges significantly above time-of-day baseline ($0.20) by >= $0.05
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("PriceSpikeMutedDuringQuietPeriod", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowQuiet := time.Date(2026, 9, 4, 1, 0, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowQuiet)
		userStore := newMockUserStore()

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowQuiet,
				TSEnd:                nowQuiet.Add(1 * time.Hour),
				DollarsPerKWH:        0.40,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowQuiet)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowQuiet, getNotifState, userStore.fetcher())
		mockS.AssertNotCalled(t, "GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("PriceSpikeAlertSentUponWakeup", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowWakeup := time.Date(2026, 9, 4, 7, 5, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowWakeup)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				PriceSpikeAlert: "medium",
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}

		var hist []types.Price
		for i := 1; i <= 20; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowWakeup.AddDate(0, 0, -i),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		// No delivered alert during quiet period
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		// Wakeup outside quiet period -> dispatches delivered alert!
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike && !l.Muted && l.Success
		})).Return(nil).Once()

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowWakeup,
				TSEnd:                nowWakeup.Add(1 * time.Hour),
				DollarsPerKWH:        0.45,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowWakeup)
		srv.handlePriceSpikeNotifications(context.Background(), siteID, notifications, data, nowWakeup, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})
}

func TestHandleSolarUnderproductionNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)
	nowMidday := time.Date(2026, 9, 4, 12, 10, 0, 0, loc)

	statusMid := types.SystemStatus{
		Timestamp:          nowMidday,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.0,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	t.Run("SignificantDrop", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeSolarUnderproduction && l.Flavor == "medium"
		})).Return(nil).Once()

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.5,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedDawnDusk", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning) // 7:10 AM (< 11 AM)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}

		statusMorning := statusMid
		statusMorning.Timestamp = nowMorning
		data := &dataForNotifications{
			status:   statusMorning,
			settings: settings,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedLowForecast", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}

		// Forecast only 1.5 kW (< 2.0 kW min forecast)
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      1.5,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedStormAlarms", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusStorm := statusMid
		statusStorm.Storms = []types.Storm{{Description: "Severe Thunderstorm"}}

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}
		data := &dataForNotifications{
			status:   statusStorm,
			settings: settings,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedActiveAlarms", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusAlarm := statusMid
		statusAlarm.Alarms = []types.SystemAlarm{{Code: "501", Description: "Grid sync lost"}}

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}
		data := &dataForNotifications{
			status:   statusAlarm,
			settings: settings,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedMinorVariance", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}

		// Forecast 4.0 kW, actual 3.5 kW (87.5% - well above 30% medium threshold)
		statusNormal := statusMid
		statusNormal.SolarKW = 3.5
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.0,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusNormal,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("DailyDeduplication", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}
		// Already sent earlier today
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-solar",
				TSCreated: time.Date(2026, 9, 4, 11, 0, 0, 0, loc).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeSolarUnderproduction,
				Success:   true,
			},
		}, nil).Once()

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.0,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SensitivityLevels", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		user := createTestPushUser(t, "user-low@test.com", pushServer.URL+"/push/user-low")
		userStore := newMockUserStore(user)

		statusSensitivity := statusMid
		// Forecast 6.0 kW, actual 2.5 kW. Deficit = 3.5 kW >= 2.5 kW.
		// "high" ratio is 0.15: 2.5 is NOT < 0.9 kW -> suppressed for high.
		// "low" ratio is 0.50: 2.5 < 3.0 kW -> triggers for low!
		statusSensitivity.SolarKW = 2.5

		notifications := map[string]types.UserNotificationSettings{
			"user-high@test.com": {
				SolarUnderproductionAlert: "high",
			},
			"user-low@test.com": {
				SolarUnderproductionAlert: "low",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeSolarUnderproduction && l.UserID == "user-low@test.com" && l.Flavor == "low"
		})).Return(nil).Once()

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      6.0,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusSensitivity,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, userStore.fetcher())
		assert.Equal(t, 1, userStore.callCount("user-low@test.com"))
		assert.Equal(t, 0, userStore.callCount("user-high@test.com"))
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedOvercastWeather", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusOvercast := statusMid
		statusOvercast.SolarKW = 0.5

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
			},
		}

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.0,
				},
			},
		}
		weatherHistory := []types.Weather{
			{
				TSDayStart: nowMidday.Truncate(24 * time.Hour),
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart:       nowMidday.Truncate(time.Hour),
						CloudCoverPercent: 85.0, // >= 60.0% overcast threshold
					},
				},
			},
		}
		data := &dataForNotifications{
			status:         statusOvercast,
			settings:       settings,
			weatherHistory: weatherHistory,
			plan:           mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("MutedDuringQuietPeriod", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		userStore := newMockUserStore()

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				SolarUnderproductionAlert: "medium",
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 15}},
					},
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeSolarUnderproduction && l.Muted && !l.Success
		})).Return(nil).Once()

		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       nowMidday.Truncate(time.Hour),
					TSEnd:         nowMidday.Truncate(time.Hour).Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.5,
				},
			},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			plan:     mockPlan,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), siteID, notifications, data, nowMidday, getNotifState, userStore.fetcher())
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})
}

func TestHandleVPPDispatchNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusVPP := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
		VPPActive:          true,
	}

	t.Run("UnplannedVPPDispatch", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				VPPDispatchAlert: true,
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeVPPDispatch
		})).Return(nil).Once()

		vppInfo := types.UtilityVPPInfo{}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), siteID, notifications, statusVPP, vppInfo, nowMorning, getNotifState, mockUserGetter(user))
		mockS.AssertExpectations(t)
	})

	t.Run("ScheduledVPPDispatchSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				VPPDispatchAlert: true,
			},
		}

		// Scheduled mandatory window covers nowMorning
		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: nowMorning.Add(-15 * time.Minute),
						End:   nowMorning.Add(45 * time.Minute),
					},
				},
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), siteID, notifications, statusVPP, vppInfo, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ScheduledVPPEventsSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		statusScheduled := statusVPP
		statusScheduled.VPPEvents = []types.VPPEvent{
			{
				Description: "California DSGS",
				TSStart:     nowMorning.Add(-30 * time.Minute),
				TSEnd:       nowMorning.Add(90 * time.Minute),
			},
		}

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				VPPDispatchAlert: true,
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), siteID, notifications, statusScheduled, types.UtilityVPPInfo{}, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("VPPDispatchDeduplication5Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				VPPDispatchAlert: true,
			},
		}
		// Sent 2 hours ago
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-vpp",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeVPPDispatch,
				Success:   true,
			},
		}, nil).Once()

		vppInfo := types.UtilityVPPInfo{}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), siteID, notifications, statusVPP, vppInfo, nowMorning, getNotifState, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("MutedDuringQuietPeriod", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowQuiet := time.Date(2026, 9, 4, 2, 0, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowQuiet)
		userStore := newMockUserStore()

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				VPPDispatchAlert: true,
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
					},
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeVPPDispatch && l.Muted && !l.Success
		})).Return(nil).Once()

		vppInfo := types.UtilityVPPInfo{}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowQuiet)
		srv.handleVPPDispatchNotifications(context.Background(), siteID, notifications, statusVPP, vppInfo, nowQuiet, getNotifState, userStore.fetcher())
		assert.Equal(t, 0, userStore.totalCalls())
		mockS.AssertExpectations(t)
	})
}

func TestHandleNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusMorning := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	mockEnergyHistory := []types.DailyEnergyStats{
		{
			TSDayStart: nowMorning.AddDate(0, 0, -3),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -3), SolarKWH: 25.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -2),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 28.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -1),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 24.0},
			},
		},
	}

	t.Run("DispatchesDueNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary && l.Flavor == "metrics_heavy"
		})).Return(nil).Once()

		notifSettings := settings
		notifSettings.PlanMode = true
		notifSettings.Notifications = notifications
		mockPlan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:  nowMorning,
					TSEnd:    nowMorning.Add(time.Hour),
					SolarKWH: 20.0,
					EndSOC:   75.0,
				},
			},
		}
		data := &dataForNotifications{
			settings:      notifSettings,
			status:        statusMorning,
			energyHistory: mockEnergyHistory,
			plan:          mockPlan,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertExpectations(t)
	})

	t.Run("PlanModeDisabledSuppressesForecastNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifSettings := settings
		notifSettings.PlanMode = false
		notifSettings.Notifications = map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}
		data := &dataForNotifications{
			settings:      notifSettings,
			status:        statusMorning,
			energyHistory: mockEnergyHistory,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("MissingPlanSuppressesForecastNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifSettings := settings
		notifSettings.PlanMode = true
		notifSettings.Notifications = map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
				MorningSummaryFlavor:  "metrics_heavy",
			},
		}
		data := &dataForNotifications{
			settings:      notifSettings,
			status:        statusMorning,
			energyHistory: mockEnergyHistory,
			plan:          nil,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("NotificationsDisabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return nowMorning },
		}

		data := &dataForNotifications{
			settings: settings,
			status:   statusMorning,
		}
		disabledSrv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetSite", mock.Anything, mock.Anything)
	})

	t.Run("NoSiteNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		data := &dataForNotifications{
			settings: settings,
			status:   statusMorning,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("LazyNotificationLookupZeroReadsWhenNoConditions", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		notifSettings := settings
		notifSettings.Notifications = map[string]types.UserNotificationSettings{
			"user1@test.com": {
				MorningSummaryEnabled:     false,
				EveningSummaryEnabled:     false,
				GridOutageAlert:           false,
				PriceSpikeAlert:           "",
				SolarUnderproductionAlert: "",
				VPPDispatchAlert:          false,
			},
		}

		data := &dataForNotifications{
			settings: notifSettings,
			status:   statusMorning,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("CachedUserFetcherPreventsDuplicateDBQueries", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		fetcher := srv.newCachedUserFetcher()

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u, err := fetcher(context.Background(), "user1@test.com")
				assert.NoError(t, err)
				assert.Equal(t, "user1@test.com", u.Email)
			}()
		}
		wg.Wait()

		mockS.AssertExpectations(t)
	})

	t.Run("RecommendedAlertModeResolution", func(t *testing.T) {
		// When AllAlertSensitivity is specified (e.g. "high"), all alerts are enabled at that sensitivity
		recHigh := types.UserNotificationSettings{
			AllAlertSensitivity: "high",
		}
		assert.True(t, recHigh.RealTimeAlertEnabled(false))
		assert.Equal(t, "high", recHigh.RealTimeAlertSensitivity(""))
		assert.Equal(t, "high", recHigh.RealTimeAlertSensitivity("low"))

		// When AllAlertSensitivity is "medium" (recommended default)
		recMed := types.UserNotificationSettings{
			AllAlertSensitivity: "medium",
		}
		assert.True(t, recMed.RealTimeAlertEnabled(false))
		assert.Equal(t, "medium", recMed.RealTimeAlertSensitivity(""))
		assert.Equal(t, "medium", recMed.RealTimeAlertSensitivity("low"))

		// When AllAlertSensitivity is "disabled" (all real-time alerts explicitly muted)
		recDisabled := types.UserNotificationSettings{
			AllAlertSensitivity: "disabled",
			GridOutageAlert:     true,
			PriceSpikeAlert:     "high",
		}
		assert.False(t, recDisabled.RealTimeAlertEnabled(true))
		assert.Equal(t, "", recDisabled.RealTimeAlertSensitivity("high"))
		assert.Equal(t, "", recDisabled.RealTimeAlertSensitivity(""))

		// When AllAlertSensitivity is empty (custom / individual settings)
		customCfg := types.UserNotificationSettings{
			AllAlertSensitivity:       "",
			GridOutageAlert:           true,
			VPPDispatchAlert:          false,
			PriceSpikeAlert:           "high",
			SolarUnderproductionAlert: "low",
			HighHomeLoadAlert:         "",
		}
		assert.True(t, customCfg.RealTimeAlertEnabled(customCfg.GridOutageAlert))
		assert.False(t, customCfg.RealTimeAlertEnabled(customCfg.VPPDispatchAlert))
		assert.Equal(t, "high", customCfg.RealTimeAlertSensitivity(customCfg.PriceSpikeAlert))
		assert.Equal(t, "low", customCfg.RealTimeAlertSensitivity(customCfg.SolarUnderproductionAlert))
		assert.Equal(t, "", customCfg.RealTimeAlertSensitivity(customCfg.HighHomeLoadAlert))

		// When legacy config has no AllAlertSensitivity set
		legacyCfg := types.UserNotificationSettings{
			GridOutageAlert:           false,
			PriceSpikeAlert:           "medium",
			SolarUnderproductionAlert: "",
		}
		assert.False(t, legacyCfg.RealTimeAlertEnabled(legacyCfg.GridOutageAlert))
		assert.Equal(t, "medium", legacyCfg.RealTimeAlertSensitivity(legacyCfg.PriceSpikeAlert))
		assert.Equal(t, "", legacyCfg.RealTimeAlertSensitivity(legacyCfg.SolarUnderproductionAlert))
	})
}

func TestNotificationIDHelpers(t *testing.T) {
	t.Run("GenerateAndParseWithUnderscoresInSiteID", func(t *testing.T) {
		siteID := "my_home_site_01"
		userID := "user@example.com"
		endpoint := "https://fcm.googleapis.com/fcm/send/abc123"
		ts := time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC)

		id := generateNotificationLogID(siteID, userID, endpoint, ts)
		assert.True(t, strings.HasPrefix(id, "2026-09_my_home_site_01_"))

		month, parsedSiteID, hash, err := parseNotificationLogID(id)
		require.NoError(t, err)
		assert.Equal(t, "2026-09", month)
		assert.Equal(t, siteID, parsedSiteID)
		assert.NotEmpty(t, hash)
	})

	t.Run("GenerateAndParseWithHyphensInSiteID", func(t *testing.T) {
		siteID := "test-site-123"
		userID := "user@example.com"
		endpoint := "https://fcm.googleapis.com/fcm/send/abc123"
		ts := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)

		id := generateNotificationLogID(siteID, userID, endpoint, ts)
		assert.True(t, strings.HasPrefix(id, "2026-12_test-site-123_"))

		month, parsedSiteID, hash, err := parseNotificationLogID(id)
		require.NoError(t, err)
		assert.Equal(t, "2026-12", month)
		assert.Equal(t, siteID, parsedSiteID)
		assert.NotEmpty(t, hash)
	})

	t.Run("ParseInvalidID_TooShort", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("short_id")
		assert.ErrorContains(t, err, "invalid notification ID structure")
	})

	t.Run("ParseInvalidID_WrongDelimiter", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("2026-09-site-1234567890abcdef")
		assert.ErrorContains(t, err, "invalid notification ID structure")
	})

	t.Run("ParseInvalidID_MissingSiteID", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("2026-09__1234567890abcdef12")
		assert.ErrorContains(t, err, "missing siteID or hash")
	})
}

func TestNotificationPlanHelper(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, loc)

	t.Run("TodaySolarForecastKWH", func(t *testing.T) {
		todayStart := time.Date(2026, 9, 4, 0, 0, 0, 0, loc)
		todayEnd := todayStart.AddDate(0, 0, 1)

		t.Run("FiltersPeriodsStrictlyWithinToday", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: todayStart.Add(-1 * time.Hour), TSEnd: todayStart, SolarKWH: 1.0}, // yesterday
					{TSStart: todayStart, TSEnd: todayStart.Add(time.Hour), SolarKWH: 0.5},      // at boundary
					{TSStart: todayStart.Add(8 * time.Hour), TSEnd: todayStart.Add(9 * time.Hour), SolarKWH: 2.5},
					{TSStart: todayStart.Add(12 * time.Hour), TSEnd: todayStart.Add(13 * time.Hour), SolarKWH: 5.0},
					{TSStart: todayEnd, TSEnd: todayEnd.Add(time.Hour), SolarKWH: 1.2},                    // at end boundary
					{TSStart: todayEnd.Add(time.Hour), TSEnd: todayEnd.Add(2 * time.Hour), SolarKWH: 3.0}, // tomorrow
				},
			}
			helper := newNotificationPlanHelper(plan)
			assert.InDelta(t, 8.0, helper.todaySolarForecastKWH(todayStart, todayEnd), 1e-4)
		})

		t.Run("NilOrEmptyPlanReturnsZero", func(t *testing.T) {
			assert.Equal(t, 0.0, newNotificationPlanHelper(nil).todaySolarForecastKWH(todayStart, todayEnd))
			assert.Equal(t, 0.0, newNotificationPlanHelper(&types.Plan{}).todaySolarForecastKWH(todayStart, todayEnd))
		})
	})

	t.Run("TodayPeakSOC", func(t *testing.T) {
		todayStart := time.Date(2026, 9, 4, 0, 0, 0, 0, loc)
		todayEnd := todayStart.AddDate(0, 0, 1)

		t.Run("ReturnsMaxOfStartOrEndSOC", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: todayStart.Add(8 * time.Hour), TSEnd: todayStart.Add(9 * time.Hour), StartSOC: 50.0, EndSOC: 65.0},
					{TSStart: todayStart.Add(12 * time.Hour), TSEnd: todayStart.Add(13 * time.Hour), StartSOC: 88.0, EndSOC: 72.0}, // StartSOC higher
				},
			}
			helper := newNotificationPlanHelper(plan)
			assert.Equal(t, 88.0, helper.todayPeakSOC(todayStart, todayEnd, 45.0))
		})

		t.Run("ExcludesPeriodsFromOtherDays", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: todayStart.Add(-2 * time.Hour), TSEnd: todayStart.Add(-1 * time.Hour), EndSOC: 99.0},
					{TSStart: todayStart.Add(10 * time.Hour), TSEnd: todayStart.Add(11 * time.Hour), EndSOC: 68.0},
					{TSStart: todayEnd.Add(2 * time.Hour), TSEnd: todayEnd.Add(3 * time.Hour), EndSOC: 100.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			assert.Equal(t, 68.0, helper.todayPeakSOC(todayStart, todayEnd, 45.0))
		})

		t.Run("LowerThanCurrentSOCDefaultsToCurrent", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: todayStart.Add(10 * time.Hour), TSEnd: todayStart.Add(11 * time.Hour), EndSOC: 55.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			assert.Equal(t, 75.0, helper.todayPeakSOC(todayStart, todayEnd, 75.0))
		})

		t.Run("NilOrEmptyPlanReturnsCurrentSOC", func(t *testing.T) {
			assert.Equal(t, 62.0, newNotificationPlanHelper(nil).todayPeakSOC(todayStart, todayEnd, 62.0))
			assert.Equal(t, 62.0, newNotificationPlanHelper(&types.Plan{}).todayPeakSOC(todayStart, todayEnd, 62.0))
		})
	})

	t.Run("BatteryCapacityETA", func(t *testing.T) {
		todayEnd := time.Date(2026, 9, 4, 23, 59, 59, 0, loc)
		etaTime := now.Add(2 * time.Hour)

		t.Run("ReachesExact99PercentReturnsETA", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: now, TSEnd: now.Add(time.Hour), EndSOC: 85.0},
					{TSStart: now.Add(time.Hour), TSEnd: etaTime, EndSOC: 99.0}, // exactly 99%
				},
			}
			helper := newNotificationPlanHelper(plan)
			eta, ok := helper.batteryCapacityETA(now, todayEnd)
			assert.True(t, ok)
			assert.Equal(t, etaTime, eta)
		})

		t.Run("PastPeriodsHittingFullChargeSkipped", func(t *testing.T) {
			futureETA := now.Add(3 * time.Hour)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: now.Add(-2 * time.Hour), TSEnd: now.Add(-1 * time.Hour), EndSOC: 100.0}, // past
					{TSStart: now, TSEnd: now.Add(time.Hour), EndSOC: 75.0},
					{TSStart: now.Add(time.Hour), TSEnd: futureETA, EndSOC: 100.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			eta, ok := helper.batteryCapacityETA(now, todayEnd)
			assert.True(t, ok)
			assert.Equal(t, futureETA, eta)
		})

		t.Run("Under99PercentReturnsFalse", func(t *testing.T) {
			lowPlan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: now, TSEnd: now.Add(time.Hour), EndSOC: 98.9},
				},
			}
			_, ok := newNotificationPlanHelper(lowPlan).batteryCapacityETA(now, todayEnd)
			assert.False(t, ok)
		})

		t.Run("PeriodsStartingAfterTodayEndSkipped", func(t *testing.T) {
			tomorrowPlan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: todayEnd.Add(time.Hour), TSEnd: todayEnd.Add(2 * time.Hour), EndSOC: 100.0},
				},
			}
			_, ok := newNotificationPlanHelper(tomorrowPlan).batteryCapacityETA(now, todayEnd)
			assert.False(t, ok)
		})

		t.Run("NilOrEmptyPlanReturnsFalse", func(t *testing.T) {
			_, ok := newNotificationPlanHelper(nil).batteryCapacityETA(now, todayEnd)
			assert.False(t, ok)
			_, ok = newNotificationPlanHelper(&types.Plan{}).batteryCapacityETA(now, todayEnd)
			assert.False(t, ok)
		})
	})

	t.Run("OvernightPlanOutcome", func(t *testing.T) {
		evening := time.Date(2026, 9, 4, 20, 0, 0, 0, loc)

		t.Run("BatteryAlreadyAtOrBelowReserve", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: evening.Add(time.Hour), EndSOC: 15.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 15.0, 20.0)
			assert.Equal(t, evening, outcome.reachesReserveAt)
			assert.False(t, outcome.lastsUntilSunrise)
		})

		t.Run("ScheduledChargeOvernight", func(t *testing.T) {
			chargeTime := evening.Add(4 * time.Hour)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: evening.Add(time.Hour), BatteryMode: types.BatteryModeStandby},
					{TSStart: chargeTime, TSEnd: chargeTime.Add(time.Hour), BatteryMode: types.BatteryModeChargeAny},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 50.0, 20.0)
			assert.Equal(t, chargeTime, outcome.scheduledChargeAt)
		})

		t.Run("ReachesReserveBeforeSunrise", func(t *testing.T) {
			reserveTime := evening.Add(5 * time.Hour)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: reserveTime, EndSOC: 20.0, ReserveSOC: 20.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 50.0, 20.0)
			assert.Equal(t, reserveTime, outcome.reachesReserveAt)
			assert.False(t, outcome.lastsUntilSunrise)
		})

		t.Run("PerPeriodReserveOverride", func(t *testing.T) {
			periodEnd := evening.Add(3 * time.Hour)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: periodEnd, EndSOC: 25.0, ReserveSOC: 30.0}, // reaches period-specific reserve (30%)
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 50.0, 20.0)
			assert.Equal(t, periodEnd, outcome.reachesReserveAt)
			assert.False(t, outcome.lastsUntilSunrise)
		})

		t.Run("LastsUntilSunriseWithTomorrowSolar", func(t *testing.T) {
			tomorrowSunrise := time.Date(2026, 9, 5, 6, 30, 0, 0, loc)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: evening.Add(4 * time.Hour), EndSOC: 65.0, ReserveSOC: 20.0, BatteryMode: types.BatteryModeStandby},
					{TSStart: evening.Add(4 * time.Hour), TSEnd: tomorrowSunrise, EndSOC: 45.0, ReserveSOC: 20.0, BatteryMode: types.BatteryModeStandby},
					{TSStart: tomorrowSunrise, TSEnd: tomorrowSunrise.Add(time.Hour), SolarKWH: 0.8, EndSOC: 50.0}, // tomorrow solar start
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 70.0, 20.0)
			assert.True(t, outcome.lastsUntilSunrise)
			assert.True(t, outcome.allStandby)
			assert.True(t, outcome.reachesReserveAt.IsZero())
		})

		t.Run("LastsUntilSunriseFallbackCutoffWhenNoTomorrowSolar", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: evening.Add(8 * time.Hour), EndSOC: 50.0, ReserveSOC: 20.0, BatteryMode: types.BatteryModeStandby},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 60.0, 20.0)
			assert.True(t, outcome.lastsUntilSunrise)
			assert.True(t, outcome.reachesReserveAt.IsZero())
		})

		t.Run("AllStandbyFalseWhenSelfConsuming", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening, TSEnd: evening.Add(4 * time.Hour), EndSOC: 60.0, BatteryMode: types.BatteryModeStandby},
					{TSStart: evening.Add(4 * time.Hour), TSEnd: evening.Add(8 * time.Hour), EndSOC: 45.0, BatteryMode: types.BatteryModeLoad},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 75.0, 20.0)
			assert.True(t, outcome.lastsUntilSunrise)
			assert.False(t, outcome.allStandby)
		})

		t.Run("NoOvernightPeriods", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: evening.Add(-5 * time.Hour), TSEnd: evening.Add(-4 * time.Hour), EndSOC: 40.0},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.overnightPlanOutcome(context.Background(), evening, loc, 60.0, 20.0)
			assert.False(t, outcome.lastsUntilSunrise)
			assert.True(t, outcome.reachesReserveAt.IsZero())
			assert.True(t, outcome.scheduledChargeAt.IsZero())
		})

		t.Run("NilOrEmptyPlanReturnsEmptyOutcome", func(t *testing.T) {
			nilOutcome := newNotificationPlanHelper(nil).overnightPlanOutcome(context.Background(), evening, loc, 60.0, 20.0)
			assert.False(t, nilOutcome.lastsUntilSunrise)
			emptyOutcome := newNotificationPlanHelper(&types.Plan{}).overnightPlanOutcome(context.Background(), evening, loc, 60.0, 20.0)
			assert.False(t, emptyOutcome.lastsUntilSunrise)
		})
	})

	t.Run("CurrentSolarForecastKW", func(t *testing.T) {
		plan := &types.Plan{
			Periods: []types.PlanPeriod{
				{
					TSStart:       now,
					TSEnd:         now.Add(time.Hour),
					DurationHours: 1.0,
					SolarKWH:      4.2,
				},
				{
					TSStart:       now.Add(time.Hour),
					TSEnd:         now.Add(90 * time.Minute),
					DurationHours: 0.5,
					SolarKWH:      2.5, // 2.5 kWh over 30 mins = 5.0 kW
				},
				{
					TSStart:       now.Add(90 * time.Minute),
					TSEnd:         now.Add(150 * time.Minute),
					DurationHours: 0.0, // fallback to SolarKWH
					SolarKWH:      3.5,
				},
			},
		}
		helper := newNotificationPlanHelper(plan)

		t.Run("MatchesIntervalMidpoint", func(t *testing.T) {
			kw, ok := helper.currentSolarForecastKW(now.Add(15 * time.Minute))
			assert.True(t, ok)
			assert.InDelta(t, 4.2, kw, 1e-4)
		})

		t.Run("MatchesIntervalStartBoundary", func(t *testing.T) {
			kw, ok := helper.currentSolarForecastKW(now)
			assert.True(t, ok)
			assert.InDelta(t, 4.2, kw, 1e-4)
		})

		t.Run("ExcludesIntervalEndBoundary", func(t *testing.T) {
			// At exactly now.Add(time.Hour), should match the second period (not first)
			kw, ok := helper.currentSolarForecastKW(now.Add(time.Hour))
			assert.True(t, ok)
			assert.InDelta(t, 5.0, kw, 1e-4)
		})

		t.Run("ScalesFractionalDurationHours", func(t *testing.T) {
			kw, ok := helper.currentSolarForecastKW(now.Add(75 * time.Minute))
			assert.True(t, ok)
			assert.InDelta(t, 5.0, kw, 1e-4)
		})

		t.Run("ZeroDurationHoursFallback", func(t *testing.T) {
			kw, ok := helper.currentSolarForecastKW(now.Add(100 * time.Minute))
			assert.True(t, ok)
			assert.InDelta(t, 3.5, kw, 1e-4)
		})

		t.Run("OutsidePlanReturnsFalse", func(t *testing.T) {
			_, ok := helper.currentSolarForecastKW(now.Add(4 * time.Hour))
			assert.False(t, ok)
		})

		t.Run("NilOrEmptyPlanReturnsFalse", func(t *testing.T) {
			_, ok := newNotificationPlanHelper(nil).currentSolarForecastKW(now)
			assert.False(t, ok)
			_, ok = newNotificationPlanHelper(&types.Plan{}).currentSolarForecastKW(now)
			assert.False(t, ok)
		})
	})

	t.Run("PriceSpikeGuidance", func(t *testing.T) {
		spikeStart := now.Add(time.Hour)
		spikeEnd := spikeStart.Add(2 * time.Hour)

		t.Run("SolarCoversAllLoad", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       spikeStart,
						TSEnd:         spikeEnd,
						SolarKWH:      4.0,
						LoadKWH:       2.5,
						GridImportKWH: 0.0,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.True(t, outcome.solarCovers)
			assert.False(t, outcome.isExporting)
		})

		t.Run("SolarDeficitWithGridImportFailsCoverage", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       spikeStart,
						TSEnd:         spikeEnd,
						SolarKWH:      2.0,
						LoadKWH:       3.0,
						GridImportKWH: 0.25, // > 0.05
						EndSOC:        70.0,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.False(t, outcome.solarCovers)
			assert.True(t, outcome.lastsEntireSpike) // falls back to battery endurance
		})

		t.Run("SolarBelowMinimumThresholdFailsCoverage", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       spikeStart,
						TSEnd:         spikeEnd,
						SolarKWH:      0.3, // < 0.5 kWh threshold
						LoadKWH:       0.2,
						GridImportKWH: 0.0,
						EndSOC:        75.0,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.False(t, outcome.solarCovers)
			assert.True(t, outcome.lastsEntireSpike)
		})

		t.Run("ExportArbitragePriority", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:     spikeStart,
						TSEnd:       spikeEnd,
						BatteryMode: types.BatteryModeExport,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.True(t, outcome.isExporting)
			assert.False(t, outcome.solarCovers)
		})

		t.Run("ReachesReserveInSecondPeriodOfSpike", func(t *testing.T) {
			midSpike := spikeStart.Add(time.Hour)
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:    spikeStart,
						TSEnd:      midSpike,
						EndSOC:     30.0,
						ReserveSOC: 20.0,
					},
					{
						TSStart:    midSpike,
						TSEnd:      spikeEnd,
						EndSOC:     18.0, // reaches reserve during 2nd period
						ReserveSOC: 20.0,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 45.0, 20.0)
			assert.Equal(t, spikeEnd, outcome.reachesReserveAt)
			assert.False(t, outcome.lastsEntireSpike)
		})

		t.Run("PerPeriodReserveOverride", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:    spikeStart,
						TSEnd:      spikeEnd,
						EndSOC:     25.0,
						ReserveSOC: 30.0, // overrides default 20%
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 45.0, 20.0)
			assert.Equal(t, spikeEnd, outcome.reachesReserveAt)
			assert.False(t, outcome.lastsEntireSpike)
		})

		t.Run("LastsEntireSpike", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:    spikeStart,
						TSEnd:      spikeEnd,
						EndSOC:     60.0,
						ReserveSOC: 20.0,
					},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.True(t, outcome.lastsEntireSpike)
			assert.True(t, outcome.reachesReserveAt.IsZero())
		})

		t.Run("NoSpikePeriods", func(t *testing.T) {
			plan := &types.Plan{
				Periods: []types.PlanPeriod{
					{TSStart: spikeStart.Add(-5 * time.Hour), TSEnd: spikeStart.Add(-4 * time.Hour)},
				},
			}
			helper := newNotificationPlanHelper(plan)
			outcome := helper.priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.False(t, outcome.solarCovers)
			assert.False(t, outcome.isExporting)
			assert.False(t, outcome.lastsEntireSpike)
			assert.True(t, outcome.reachesReserveAt.IsZero())
		})

		t.Run("NilOrEmptyPlanReturnsEmptyOutcome", func(t *testing.T) {
			nilOutcome := newNotificationPlanHelper(nil).priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.False(t, nilOutcome.lastsEntireSpike)
			emptyOutcome := newNotificationPlanHelper(&types.Plan{}).priceSpikeGuidance(context.Background(), spikeStart, spikeEnd, 80.0, 20.0)
			assert.False(t, emptyOutcome.lastsEntireSpike)
		})
	})
}

func TestNotificationTag(t *testing.T) {
	siteID := "site123"

	t.Run("MorningSummary", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeMorningSummary, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("EveningSummary", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeEveningSummary, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("GridOutage", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeGridOutage, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("GridRestored", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeGridRestored, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("SolarUnderproduction", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeSolarUnderproduction, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("PriceSpike", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypePriceSpike, siteID)
		assert.Equal(t, "raterudder-site123-price-spike", tag)
	})

	t.Run("VPPDispatch", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeVPPDispatch, siteID)
		assert.Equal(t, "raterudder-site123-vpp", tag)
	})

	t.Run("DefaultFallback", func(t *testing.T) {
		tag := notificationTag("unknown_type", siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})
}

func TestPushPayloadIcon(t *testing.T) {
	t.Run("WebAPKAppType", func(t *testing.T) {
		sub := types.PushSubscription{
			AppType:   types.PushSubscriptionAppTypeWebAPK,
			UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		}
		icon := pushPayloadIcon(sub)
		assert.Equal(t, "/transparent_192.png", icon)
	})

	t.Run("BrowserTab", func(t *testing.T) {
		sub := types.PushSubscription{
			AppType:   types.PushSubscriptionAppTypeBrowser,
			UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		}
		icon := pushPayloadIcon(sub)
		assert.Equal(t, "/logo_192.png", icon)
	})

	t.Run("EmptySubscription", func(t *testing.T) {
		icon := pushPayloadIcon(types.PushSubscription{})
		assert.Equal(t, "/logo_192.png", icon)
	})
}

func TestExtractHighestAlertedPrice(t *testing.T) {
	t.Run("ReadsPeakPriceFromMetadata", func(t *testing.T) {
		log := &types.NotificationLog{
			Title: "Alert",
			Body:  "Something happened",
			Metadata: map[string]string{
				"price":     "0.3000",
				"peakPrice": "0.5500",
			},
		}
		assert.Equal(t, 0.55, extractHighestAlertedPrice(log))
	})

	t.Run("ReadsPriceIfNoPeakPrice", func(t *testing.T) {
		log := &types.NotificationLog{
			Metadata: map[string]string{
				"price": "0.4200",
			},
		}
		assert.Equal(t, 0.42, extractHighestAlertedPrice(log))
	})

	t.Run("EmptyMetadataReturnsZero", func(t *testing.T) {
		log := &types.NotificationLog{
			Title: "🚨 Price Spike: $0.35/kWh (peaking at $0.60 at 8:00 AM)",
			Body:  "Price is $0.35/kWh now",
		}
		assert.Equal(t, 0.0, extractHighestAlertedPrice(log))
	})

	t.Run("NilLogReturnsZero", func(t *testing.T) {
		assert.Equal(t, 0.0, extractHighestAlertedPrice(nil))
	})
}

func TestComputeTimeOfDayRefPrice(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	targetTime := time.Date(2026, 9, 10, 19, 0, 0, 0, loc) // 7:00 PM

	t.Run("MatchesTargetHourAndBuffer", func(t *testing.T) {
		yesterday := targetTime.AddDate(0, 0, -1)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 18, 0, 0, 0, loc), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.05}, // 6 PM: $0.20
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.17, GridUseDollarsPerKWH: 0.05}, // 7 PM: $0.22
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 20, 0, 0, 0, loc), DollarsPerKWH: 0.19, GridUseDollarsPerKWH: 0.05}, // 8 PM: $0.24
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, loc), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.05}, // 12 PM: ignored
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.10)
		assert.InDelta(t, 0.22, ref, 0.001)
	})

	t.Run("WrapsAroundMidnight", func(t *testing.T) {
		midnightTarget := time.Date(2026, 9, 10, 0, 15, 0, 0, loc) // 12:15 AM (hour 0)
		yesterday := midnightTarget.AddDate(0, 0, -1)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 0, 0, 0, loc), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05}, // 11 PM: $0.15
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, loc), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},  // 12 AM: $0.17
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 1, 0, 0, 0, loc), DollarsPerKWH: 0.14, GridUseDollarsPerKWH: 0.05},  // 1 AM: $0.19
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 5, 0, 0, 0, loc), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.05},  // 5 AM: ignored
		}
		ref := computeTimeOfDayRefPrice(hist, midnightTarget, loc, 0.10)
		assert.InDelta(t, 0.17, ref, 0.001)
	})

	t.Run("ExcludesSameDayPrices", func(t *testing.T) {
		hist := []types.Price{
			{TSStart: time.Date(targetTime.Year(), targetTime.Month(), targetTime.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.40, GridUseDollarsPerKWH: 0.05},
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.15)
		assert.Equal(t, 0.15, ref)
	})

	t.Run("OutliersDoNotPoisonMedian", func(t *testing.T) {
		yesterday := targetTime.AddDate(0, 0, -1)
		twoDaysAgo := targetTime.AddDate(0, 0, -2)
		threeDaysAgo := targetTime.AddDate(0, 0, -3)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.05},          // $0.20
			{TSStart: time.Date(twoDaysAgo.Year(), twoDaysAgo.Month(), twoDaysAgo.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.16, GridUseDollarsPerKWH: 0.05},       // $0.21
			{TSStart: time.Date(threeDaysAgo.Year(), threeDaysAgo.Month(), threeDaysAgo.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.75, GridUseDollarsPerKWH: 0.05}, // $0.80 spike
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.10)
		assert.InDelta(t, 0.21, ref, 0.001)
	})

	t.Run("EmptyHistoryFallsBackToFallback", func(t *testing.T) {
		ref := computeTimeOfDayRefPrice(nil, targetTime, loc, 0.14)
		assert.Equal(t, 0.14, ref)
	})
}

func TestSiteRecentNotifications(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	user1 := "user1@example.com"
	user2 := "user2@example.com"

	logs := []types.NotificationLog{
		{
			ID:        "log-delivered",
			TSCreated: now.Add(-2 * time.Hour),
			UserID:    user1,
			Type:      types.NotificationTypePriceSpike,
			Success:   true,
			Muted:     false,
		},
		{
			ID:        "log-muted",
			TSCreated: now.Add(-1 * time.Hour),
			UserID:    user1,
			Type:      types.NotificationTypePriceSpike,
			Success:   false,
			Muted:     true,
		},
		{
			ID:        "log-failed",
			TSCreated: now.Add(-30 * time.Minute),
			UserID:    user1,
			Type:      types.NotificationTypePriceSpike,
			Success:   false,
			Muted:     false,
			Error:     "failed to send push",
		},
		{
			ID:        "log-stale-suppressed",
			TSCreated: now.Add(-15 * time.Minute),
			UserID:    user1,
			Type:      types.NotificationTypeGridRestored,
			Success:   false,
			Muted:     false,
			Error:     "",
		},
	}

	state := &siteRecentNotifications{logs: logs}

	t.Run("HasSentToday", func(t *testing.T) {
		dateStr := now.Format("2006-01-02")
		// user1 PriceSpike has delivered and muted logs today
		assert.True(t, state.hasSentToday(user1, types.NotificationTypePriceSpike, dateStr, time.UTC))
		// user2 has no logs
		assert.False(t, state.hasSentToday(user2, types.NotificationTypePriceSpike, dateStr, time.UTC))
		// different date
		assert.False(t, state.hasSentToday(user1, types.NotificationTypePriceSpike, "2026-09-03", time.UTC))
	})

	t.Run("HasSentWithin", func(t *testing.T) {
		// within 90 minutes -> log-muted (-1h) is within window
		assert.True(t, state.hasSentWithin(user1, types.NotificationTypePriceSpike, 90*time.Minute, now))
		// failed attempt (-30m) does not count as sent
		// within 45 minutes -> only log-failed is within window, should return false
		assert.False(t, state.hasSentWithin(user1, types.NotificationTypePriceSpike, 45*time.Minute, now))
	})

	t.Run("LastLogOnlyDelivered", func(t *testing.T) {
		// onlyDelivered: true should find log-delivered (-2h), ignoring muted (-1h) and failed (-30m)
		log, found := state.lastLog(user1, true, types.NotificationTypePriceSpike)
		assert.True(t, found)
		assert.Equal(t, "log-delivered", log.ID)
		assert.True(t, log.Success)
		assert.False(t, log.Muted)
	})

	t.Run("LastLogIncludingMuted", func(t *testing.T) {
		// onlyDelivered: false should find log-muted (-1h), ignoring failed with error (-30m)
		log, found := state.lastLog(user1, false, types.NotificationTypePriceSpike)
		assert.True(t, found)
		assert.Equal(t, "log-muted", log.ID)
		assert.False(t, log.Success)
		assert.True(t, log.Muted)
	})

	t.Run("LastLogAuditSuppressed", func(t *testing.T) {
		// internal audit suppression log (Success: false, Muted: false, Error: "") is found when onlyDelivered is false
		log, found := state.lastLog(user1, false, types.NotificationTypeGridRestored)
		assert.True(t, found)
		assert.Equal(t, "log-stale-suppressed", log.ID)
		assert.False(t, log.Success)
		assert.False(t, log.Muted)

		// but ignored when onlyDelivered is true
		_, foundDelivered := state.lastLog(user1, true, types.NotificationTypeGridRestored)
		assert.False(t, foundDelivered)
	})
}

func TestFormatHomeLoadPriceGuidance(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 9, 19, 14, 0, 0, 0, loc) // 2:00 PM

	t.Run("PeakAndDrop", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.38,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.38},
			{TSStart: time.Date(2026, 9, 19, 20, 0, 0, 0, loc), DollarsPerKWH: 0.12}, // 8 PM drop
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Equal(t, "Rates are currently at today's peak ($0.38/kWh). Consider waiting until 8 PM when rates drop to $0.12/kWh.", guidance)
	})

	t.Run("PeakOnly", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.38,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.38},
			{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.35}, // 3¢ drop < 5¢
			{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.34}, // 4¢ drop < 5¢
			// 10 hours later (beyond 8h drop window): price drops by >= 0.05, establishing max - min >= 0.05
			{TSStart: now.Add(10 * time.Hour), DollarsPerKWH: 0.33},
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Equal(t, "Rates are currently at today's peak ($0.38/kWh).", guidance)
	})

	t.Run("DropOnly", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.25,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.38},                   // higher peak later
			{TSStart: time.Date(2026, 9, 19, 20, 0, 0, 0, loc), DollarsPerKWH: 0.12}, // drop
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Equal(t, "Consider waiting until 8 PM when rates drop to $0.12/kWh.", guidance)
	})

	t.Run("CheapestRate", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.10,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.35},
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Empty(t, guidance)
	})

	t.Run("SmallDropIgnored", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.22,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.19}, // 3¢ drop < 5¢
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Empty(t, guidance)
	})

	t.Run("NonZeroMinuteFormatting", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.30,
		}
		future := []types.Price{
			{TSStart: time.Date(2026, 9, 19, 20, 30, 0, 0, loc), DollarsPerKWH: 0.12}, // 8:30 PM drop
		}
		guidance := formatHomeLoadPriceGuidance(current, future, loc)
		assert.Contains(t, guidance, "Consider waiting until 8:30 PM when rates drop to $0.12/kWh.")
	})

	t.Run("isCheapestRateOfDay", func(t *testing.T) {
		current := types.Price{
			TSStart:       now,
			DollarsPerKWH: 0.10,
		}
		future := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.35},
		}
		isCheapest, minPrice, maxPrice := isCheapestRateOfDay(current, future, loc)
		assert.True(t, isCheapest)
		assert.InDelta(t, 0.10, minPrice, 1e-4)
		assert.InDelta(t, 0.35, maxPrice, 1e-4)

		// Flat rate (spread < $0.02)
		flatFuture := []types.Price{
			{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.11},
		}
		isCheapestFlat, minPriceFlat, maxPriceFlat := isCheapestRateOfDay(current, flatFuture, loc)
		assert.False(t, isCheapestFlat)
		assert.InDelta(t, 0.10, minPriceFlat, 1e-4)
		assert.InDelta(t, 0.11, maxPriceFlat, 1e-4)
	})
}

func TestHandleHighHomeLoadNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-high-load"
	now := time.Date(2026, 9, 19, 7, 49, 0, 0, loc)

	// 5 days of baseline: morning (6-8 AM) is low (0.5 kW), evening (5-7 PM) has routine dinner cooking (5.5 kW)
	var baseHistory []types.DailyEnergyStats
	for d := 5; d >= 1; d-- {
		dayDate := now.AddDate(0, 0, -d)
		dayStart := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, loc)
		var hourly []types.EnergyStats
		for h := 0; h < 24; h++ {
			hTime := dayStart.Add(time.Duration(h) * time.Hour)
			load := 0.5
			if h >= 17 && h <= 19 {
				load = 5.5
			}
			hourly = append(hourly, types.EnergyStats{
				TSHourStart:   hTime,
				HomeKWH:       load,
				MaxBatterySOC: 80.0,
				MinBatterySOC: 70.0,
			})
		}
		baseHistory = append(baseHistory, types.DailyEnergyStats{
			TSDayStart: dayStart,
			Hourly:     hourly,
		})
	}

	t.Run("RoutineEveningCookingIgnored", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowEvening := time.Date(2026, 9, 19, 18, 0, 0, 0, loc)
		srv := createTestNotificationServer(t, mockS, nowEvening)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          nowEvening,
				HomeKW:             5.5,
				BatterySOC:         50.0,
				BatteryKW:          5.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, nowEvening, getNotifState, nil)
		mockS.AssertExpectations(t)
	})

	t.Run("AbnormalMorningSurgeAlertsWillRunOut", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "low",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// 39% SOC, 20% reserve -> 19% usable * 13.6 kWh = 2.584 kWh.
		// BatteryKW = 10.0 -> hoursRemaining = 0.2584 hr = ~16 min.
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Equal(t, "⚠️ High Home Load Detected", recordedLog.Title)
		assert.Contains(t, recordedLog.Body, "Large unusual home load detected (6.2 kW). Battery will run out in ~16 min (SOC 39%). Grid will be used.")
		assert.Equal(t, "6.20", recordedLog.Metadata["homeKW"])
		assert.Equal(t, "39.0", recordedLog.Metadata["batterySOC"])
		assert.Equal(t, "16", recordedLog.Metadata["minutesRemaining"])
	})

	t.Run("AtReserveAlertsHasRunOutWithoutDynamicKW", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Equal(t, "⚠️ High Home Load Detected", recordedLog.Title)
		assert.Equal(t, "Large unusual home load detected (6.2 kW) and battery is at reserve (25% SOC). Grid will be used.", recordedLog.Body)
		assert.NotContains(t, recordedLog.Body, "5.66")
		assert.NotContains(t, recordedLog.Body, "5.7 kW")
		assert.Equal(t, "5.66", recordedLog.Metadata["gridKW"])
	})

	t.Run("PriceGuidance_PeakWithUpcomingDrop", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowAfternoon := time.Date(2026, 9, 19, 14, 0, 0, 0, loc) // 2:00 PM -> 8 PM is 6 hours away
		srv := createTestNotificationServer(t, mockS, nowAfternoon)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          nowAfternoon,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
			currentPrice: types.Price{
				TSStart:       nowAfternoon,
				DollarsPerKWH: 0.38,
			},
			futurePrices: []types.Price{
				{TSStart: nowAfternoon.Add(1 * time.Hour), DollarsPerKWH: 0.38},
				{TSStart: time.Date(2026, 9, 19, 20, 0, 0, 0, loc), DollarsPerKWH: 0.12},
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowAfternoon)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, nowAfternoon, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "Rates are currently at today's peak ($0.38/kWh). Consider waiting until 8 PM when rates drop to $0.12/kWh.")
	})

	t.Run("PriceGuidance_CheapestRateOmitsPrice", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "high",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
			currentPrice: types.Price{
				TSStart:       now,
				DollarsPerKWH: 0.10,
			},
			futurePrices: []types.Price{
				{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.35},
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.NotContains(t, recordedLog.Body, "Rates are currently")
		assert.NotContains(t, recordedLog.Body, "Consider waiting")
		assert.NotContains(t, recordedLog.Body, "$0.10")
	})

	t.Run("PriceGuidance_DropLessThanFiveCentsIgnored", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
			currentPrice: types.Price{
				TSStart:       now,
				DollarsPerKWH: 0.22,
			},
			futurePrices: []types.Price{
				{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.19}, // 3¢ drop < 5¢
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.NotContains(t, recordedLog.Body, "Consider waiting")
	})

	t.Run("SolarCoverageSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				SolarKW:            5.4, // covers load within 1.0 kW tolerance (6.2 - 5.4 = 0.8 <= 1.0)
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)
		mockS.AssertExpectations(t)
	})

	t.Run("QuietPeriodSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
				QuietPeriods: []types.TimePeriod{
					{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 9}, // 7:49 AM falls within quiet period
						},
					},
				},
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad && l.Muted {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		mockS.AssertExpectations(t)
		assert.True(t, recordedLog.Muted)
		assert.Equal(t, "⚠️ High Home Load Detected", recordedLog.Title)
	})

	t.Run("CooldownEnforced", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		// Existing log delivered 1 hour ago
		existingLog := types.NotificationLog{
			ID:        "prev-log-id",
			TSCreated: now.Add(-1 * time.Hour),
			UserID:    "user1@test.com",
			Type:      types.NotificationTypeHighHomeLoad,
			Success:   true,
			Muted:     false,
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{existingLog}, nil).Once()

		// Scenario A: Still "will_run_out" -> suppressed by cooldown
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)
		mockS.AssertExpectations(t)

		// Scenario B: State transitioned to "at_reserve" -> escalation bypass removed, strictly suppressed by cooldown!
		mockS2 := &storagemock.MockDatabase{}
		srv2 := createTestNotificationServer(t, mockS2, now)

		mockS2.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{existingLog}, nil).Once()

		dataAtReserve := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState2 := srv2.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv2.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, dataAtReserve, now, getNotifState2, nil)

		mockS2.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS2.AssertExpectations(t)

		// Scenario C: Already sent alert 7 hours ago (> 6h cooldown) -> notification allowed!
		mockS3 := &storagemock.MockDatabase{}
		srv3 := createTestNotificationServer(t, mockS3, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")
		oldLog := existingLog
		oldLog.TSCreated = now.Add(-7 * time.Hour)
		mockS3.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{oldLog}, nil).Once()

		var recordedLog types.NotificationLog
		mockS3.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		getNotifState3 := srv3.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv3.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, dataAtReserve, now, getNotifState3, mockUserGetter(user))

		mockS3.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "and battery is at reserve (25% SOC). Grid will be used.")
	})

	t.Run("CheapestRateOfDay_SuppressedForLowAndMedium", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
			"user2@test.com": {
				HighHomeLoadAlert: "low",
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         24.9,
				BatteryKW:          0.0,
				GridKW:             5.66,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
			currentPrice: types.Price{
				TSStart:       now,
				DollarsPerKWH: 0.10, // Cheapest rate of the day ($0.10 vs future $0.35)
			},
			futurePrices: []types.Price{
				{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.35},
			},
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Neither GetUser nor AppendNotificationLog should be called because low and medium sensitivity
		// are suppressed during the day's cheapest rate period.
		mockS.AssertExpectations(t)
	})

	t.Run("EVChargingPeriodSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "high", // Even high sensitivity is suppressed during active EV charging periods
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
				EVChargingPeriods: []types.TimePeriod{
					{
						Name: "Nightly EV Charging",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 9}, // 7:49 AM falls within 7:00 - 9:00 AM
						},
					},
				},
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed by active EV charging period
		mockS.AssertExpectations(t)
	})

	t.Run("DayOne_NoPriorDaysIgnored", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "high",
			},
		}

		// Only today's hours in history, no prior days
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		dayOneHistory := []types.DailyEnergyStats{
			{
				TSDayStart: todayStart,
				Hourly: []types.EnergyStats{
					{TSHourStart: todayStart.Add(6 * time.Hour), HomeKWH: 1.0},
					{TSHourStart: todayStart.Add(7 * time.Hour), HomeKWH: 1.0},
				},
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: dayOneHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed because day 1 has no prior days to establish reliable baselines
		mockS.AssertExpectations(t)
	})

	t.Run("AtReserve_SkipsSustainedCheck", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// Prior hour in baseHistory at 6 AM was only 0.5 kW (not sustained), but battery is at reserve with GridKW >= 1.0 (1.2 kW)
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         25.0, // Exactly at reserve
				BatteryKW:          0.0,
				GridKW:             1.2, // Modest grid draw >= 1.0 kW
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 25.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "and battery is at reserve (25% SOC). Grid will be used.")
	})

	t.Run("WillRunOut_SuppressedWhenMoreThanOneHourRemaining", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		// Battery has usable energy and discharge rate results in > 1 hour remaining (1.7 hrs)
		// usableKWH = (45 - 20) * 13.6 / 100 = 3.4 kWh. BatteryKW = 2.0 kW -> hoursRemaining = 3.4 / 2.0 = 1.7 hrs
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         45.0,
				BatteryKW:          2.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed because hours remaining (1.7h) > 1.0 hour limit
		mockS.AssertExpectations(t)
	})

	// Create 5 days of history where battery was chronically at reserve (20% SOC)
	var chronicHistory []types.DailyEnergyStats
	for d := 5; d >= 1; d-- {
		dayDate := now.AddDate(0, 0, -d)
		dayStart := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, loc)
		var hourly []types.EnergyStats
		for h := 0; h < 24; h++ {
			hTime := dayStart.Add(time.Duration(h) * time.Hour)
			hourly = append(hourly, types.EnergyStats{
				TSHourStart:   hTime,
				HomeKWH:       0.5,
				MaxBatterySOC: 20.0,
				MinBatterySOC: 20.0,
			})
		}
		chronicHistory = append(chronicHistory, types.DailyEnergyStats{
			TSDayStart: dayStart,
			Hourly:     hourly,
		})
	}

	t.Run("ChronicReserve_SuppressedForAtReserve", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         20.0, // At reserve
				BatteryKW:          0.0,
				GridKW:             5.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: chronicHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed because site is chronically at reserve (reserve ratio 1.0 >= 0.35)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ChronicReserve_SuppressedForWillRunOut", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		// Battery is above reserve (39% SOC, 20% reserve -> usable 2.58 kWh >= 1.5 kWh floor)
		// Discharging at 10 kW -> will run out in ~15 min.
		// BUT the site is chronically at reserve (20% SOC) around this time of day across prior days.
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         39.0,
				BatteryKW:          10.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: chronicHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed because site is chronically at reserve around this time of day even though will_run_out is true
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ConservativeProfile_IncludesReserveBuffer", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// Base MinBatterySOC is 20%. OptimizationProfile is "conservative" -> adds 10% buffer -> effective reserve is 30%.
		// Battery is at 30% SOC with GridKW = 5.0 (at effective reserve).
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         30.0, // At effective reserve (20% + 10% buffer)
				BatteryKW:          0.0,
				GridKW:             5.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC:       20.0,
				OptimizationProfile: "conservative",
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "and battery is at reserve (30% SOC). Grid will be used.")
	})

	t.Run("UsableFloor_SuppressedWhenLessThan1Point5KWH", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		// Battery is above reserve (25.5% vs 20% reserve -> usable = 5.5% * 13.6 = 0.748 kWh < 1.5 kWh floor)
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         25.5,
				BatteryKW:          5.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, nil)

		// Suppressed because usable energy (0.748 kWh) < 1.5 kWh floor
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("VariableReserveSchedule_DynamicReserveRatio", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// 5 days of history:
		// Overnight (hours 0-6): battery was at 50% SOC (at night reserve of 50%)
		// Daytime (hours 6-24): battery was at 30% SOC (above day reserve of 20%)
		var variableHistory []types.DailyEnergyStats
		for d := 5; d >= 1; d-- {
			dayDate := now.AddDate(0, 0, -d)
			dayStart := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, loc)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				hTime := dayStart.Add(time.Duration(h) * time.Hour)
				soc := 30.0
				if h < 6 {
					soc = 50.0
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart:   hTime,
					HomeKWH:       0.5,
					MaxBatterySOC: soc,
					MinBatterySOC: soc,
				})
			}
			variableHistory = append(variableHistory, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		// Current time is 7:49 AM (daytime, reserve is 20%).
		// Battery is at 20% SOC with GridKW = 5.0 (at reserve).
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				HomeKW:             6.2,
				BatterySOC:         20.0, // At day reserve (20%)
				BatteryKW:          0.0,
				GridKW:             5.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}},
						MinBatterySOC: 50.0,
					},
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}}},
						MinBatterySOC: 20.0,
					},
				},
			},
			energyHistory: variableHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, now)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, now, getNotifState, mockUserGetter(user))

		// Chronic reserve ratio is 6/24 = 25% < 35%, so alert should be sent (not suppressed)
		mockS.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "and battery is at reserve (20% SOC). Grid will be used.")
	})

	t.Run("VariableReserveSchedule_WillRunOutUpcomingReserveJump", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		nowAfternoon := time.Date(2026, 9, 19, 15, 50, 0, 0, loc) // 3:50 PM
		srv := createTestNotificationServer(t, mockS, nowAfternoon)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		notifications := map[string]types.UserNotificationSettings{
			"user1@test.com": {
				HighHomeLoadAlert: "medium",
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypeHighHomeLoad {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// Reserve schedule: 0-16 is 20%, 16-24 is 50%.
		// At 3:50 PM, battery is at 35% SOC (well above current 20% reserve).
		// Discharging at 6.0 kW with capacity 13.6 kWh.
		// In 10 minutes (at 4:00 PM), reserve jumps to 50%.
		// At 4:00 PM, battery SOC drops from 35% by (6kW * 10/60h / 13.6kWh * 100) = 7.35% -> ~27.65% SOC.
		// Since 27.65% <= 50%, battery hits reserve at minute 10!
		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          nowAfternoon,
				HomeKW:             6.2,
				BatterySOC:         35.0,
				BatteryKW:          6.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}}},
						MinBatterySOC: 20.0,
					},
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 24}}},
						MinBatterySOC: 50.0,
					},
				},
			},
			energyHistory: baseHistory,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowAfternoon)
		srv.handleHighHomeLoadNotifications(context.Background(), siteID, notifications, data, nowAfternoon, getNotifState, mockUserGetter(user))

		mockS.AssertExpectations(t)
		assert.Contains(t, recordedLog.Body, "Battery will run out in ~10 min (SOC 35%). Grid will be used.")
		assert.Equal(t, "10", recordedLog.Metadata["minutesRemaining"])
	})

	t.Run("calculateTODReserveRatio", func(t *testing.T) {
		t.Run("InsufficientData", func(t *testing.T) {
			ratio, ok := calculateTODReserveRatio(nil, now, loc, 20.0)
			assert.False(t, ok)
			assert.Equal(t, 0.0, ratio)
		})

		t.Run("WindowFilteringAndRatio", func(t *testing.T) {
			// Construct 3 days of history
			// At targetHour (7), window is hours 6, 7, 8.
			// Day 1: hours 6, 7, 8 have SOC 20 (reserve). Other hours (e.g. 12) have SOC 80.
			// Day 2: hours 6, 7 have SOC 20 (reserve), hour 8 has SOC 60 (above reserve).
			// Day 3: hours 6, 7, 8 have SOC 60 (above reserve).
			// Total window samples: 3 * 3 = 9 hours.
			// Reserve hours: Day 1 (3) + Day 2 (2) + Day 3 (0) = 5 hours.
			// Expected ratio: 5 / 9 = ~0.555
			var testHistory []types.DailyEnergyStats
			for d := 3; d >= 1; d-- {
				dayDate := now.AddDate(0, 0, -d)
				dayStart := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, loc)
				var hourly []types.EnergyStats
				for h := 0; h < 24; h++ {
					hTime := dayStart.Add(time.Duration(h) * time.Hour)
					soc := 60.0
					if d == 3 && (h >= 6 && h <= 8) {
						soc = 20.0
					} else if d == 2 && (h == 6 || h == 7) {
						soc = 20.0
					}
					hourly = append(hourly, types.EnergyStats{
						TSHourStart:   hTime,
						MaxBatterySOC: soc,
						MinBatterySOC: soc,
					})
				}
				testHistory = append(testHistory, types.DailyEnergyStats{
					TSDayStart: dayStart,
					Hourly:     hourly,
				})
			}

			ratio, ok := calculateTODReserveRatio(testHistory, now, loc, 20.0)
			require.True(t, ok)
			assert.InDelta(t, 5.0/9.0, ratio, 1e-4)
		})
	})
}
