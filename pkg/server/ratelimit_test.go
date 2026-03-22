package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestRateLimitMiddleware(t *testing.T) {
	s := &Server{}

	// Create a dummy handler to wrap
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap the dummy handler with the middleware
	handler := s.rateLimitMiddleware(dummyHandler)

	t.Run("General Rate Limit - Allowed", func(t *testing.T) {
		// Reset limiters for clean state
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)
		clientLimiterMux.Unlock()

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// Should be allowed for the burst limit
		for i := 0; i < generalBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i)
		}
	})

	t.Run("General Rate Limit - Exceeded", func(t *testing.T) {
		// Reset limiters for clean state
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)
		// Artificially lower the burst for testing
		generalBurst = 2
		generalRateLimit = rate.Every(time.Minute / 2) // 2 per minute
		clientLimiterMux.Unlock()

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.2:12345"

		// Use up the burst
		for i := 0; i < generalBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Next request should be blocked
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("Sensitive Endpoint Limit - Exceeded", func(t *testing.T) {
		// Reset limiters for clean state
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)
		// Artificially lower the burst for testing
		sensitiveBurst = 1
		sensitiveRateLimit = rate.Every(time.Minute / 1) // 1 per minute
		clientLimiterMux.Unlock()

		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// First request allowed
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request blocked (sensitive burst is 1)
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})

	t.Run("IP Separation", func(t *testing.T) {
		// Reset limiters for clean state
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)
		sensitiveBurst = 1
		sensitiveRateLimit = rate.Every(time.Minute / 1)
		clientLimiterMux.Unlock()

		req1 := httptest.NewRequest("POST", "/api/auth/login", nil)
		req1.RemoteAddr = "192.168.1.4:12345"

		req2 := httptest.NewRequest("POST", "/api/auth/login", nil)
		req2.RemoteAddr = "192.168.1.5:12345" // Different IP

		// First IP allowed
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// First IP blocked (limit reached)
		w1_blocked := httptest.NewRecorder()
		handler.ServeHTTP(w1_blocked, req1)
		assert.Equal(t, http.StatusTooManyRequests, w1_blocked.Code)

		// Second IP allowed (independent limit)
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("X-Forwarded-For Header Handling", func(t *testing.T) {
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)
		sensitiveBurst = 1
		sensitiveRateLimit = rate.Every(time.Minute / 1)
		clientLimiterMux.Unlock()

		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		// Set a chain of IPs in X-Forwarded-For
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1, 10.0.0.1")
		req.RemoteAddr = "127.0.0.1:8080" // Proxied connection

		// Should extract 203.0.113.1 and allow
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify the limiter was created for the correct IP
		clientLimiterMux.Lock()
		_, exists := clientLimiters["203.0.113.1"]
		clientLimiterMux.Unlock()
		assert.True(t, exists, "Limiter should be created for the client IP from X-Forwarded-For")
	})

	t.Run("Cleanup Stale Limiters", func(t *testing.T) {
		clientLimiterMux.Lock()
		clientLimiters = make(map[string]*clientRateLimiter)

		// Create a stale limiter (last seen 15 minutes ago)
		staleLimiter := &clientRateLimiter{
			general:   rate.NewLimiter(rate.Inf, 1),
			sensitive: rate.NewLimiter(rate.Inf, 1),
			lastSeen:  time.Now().Add(-15 * time.Minute),
		}
		clientLimiters["1.1.1.1"] = staleLimiter

		// Create a recent limiter (last seen 1 minute ago)
		recentLimiter := &clientRateLimiter{
			general:   rate.NewLimiter(rate.Inf, 1),
			sensitive: rate.NewLimiter(rate.Inf, 1),
			lastSeen:  time.Now().Add(-1 * time.Minute),
		}
		clientLimiters["2.2.2.2"] = recentLimiter
		clientLimiterMux.Unlock()

		// Run cleanup
		cleanupStaleLimiters()

		clientLimiterMux.Lock()
		_, staleExists := clientLimiters["1.1.1.1"]
		_, recentExists := clientLimiters["2.2.2.2"]
		clientLimiterMux.Unlock()

		assert.False(t, staleExists, "Stale limiter should have been removed")
		assert.True(t, recentExists, "Recent limiter should have been kept")
	})
}
