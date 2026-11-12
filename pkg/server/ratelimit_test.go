package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRateLimitMiddleware(t *testing.T) {
	// Create a dummy handler to wrap
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("General Rate Limit - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// Should be allowed for the burst limit
		for i := 0; i < s.generalBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i)
		}
	})

	t.Run("General Rate Limit - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 2), // 2 per minute
			generalBurst:       2,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.2:12345"

		// Use up the burst
		for i := 0; i < s.generalBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Next request should be blocked
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("Sensitive Endpoint Limit - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit auth/logout - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit tesla/register - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/tesla/register", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit ess/stage - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/ess/stage", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit updateSites - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

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

	t.Run("Sensitive Endpoint Limit auth/logout - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
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

	t.Run("Sensitive Endpoint Limit tesla/register - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/tesla/register", nil)
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

	t.Run("Sensitive Endpoint Limit ess/stage - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/ess/stage", nil)
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

	t.Run("Sensitive Endpoint Limit updateSites - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
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

	t.Run("Sensitive Endpoint Limit logout - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit logout - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
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

	t.Run("Sensitive Endpoint Limit tesla register - Allowed", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/tesla/register", nil)
		req.RemoteAddr = "192.168.1.3:12345"

		// Should be allowed up to the sensitive burst limit
		for i := 0; i < s.sensitiveBurst; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Sensitive request %d should be allowed", i)
		}
	})

	t.Run("Sensitive Endpoint Limit tesla register - Exceeded", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/tesla/register", nil)
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

	t.Run("General Endpoint - Only Uses General Limit", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.99:12345"

		// General endpoints should not consume sensitive limit, so sending more
		// requests than the sensitive burst limit should be allowed.
		for i := 0; i < s.sensitiveBurst+2; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i)
		}
	})

	t.Run("IP Separation", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1), // 1 per minute
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

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

	t.Run("Cleanup Stale Limiters", func(t *testing.T) {
		s := &Server{}

		// Create a stale limiter (last seen 15 minutes ago)
		staleLimiter := &clientRateLimiter{
			general:   rate.NewLimiter(rate.Inf, 1),
			sensitive: rate.NewLimiter(rate.Inf, 1),
		}
		staleLimiter.lastSeen.Store(time.Now().Add(-15 * time.Minute).UnixNano())
		s.clientLimiters.Store("1.1.1.1", staleLimiter)

		// Create a recent limiter (last seen 1 minute ago)
		recentLimiter := &clientRateLimiter{
			general:   rate.NewLimiter(rate.Inf, 1),
			sensitive: rate.NewLimiter(rate.Inf, 1),
		}
		recentLimiter.lastSeen.Store(time.Now().Add(-1 * time.Minute).UnixNano())
		s.clientLimiters.Store("2.2.2.2", recentLimiter)

		// Run cleanup
		s.cleanupStaleLimiters()

		_, staleExists := s.clientLimiters.Load("1.1.1.1")
		_, recentExists := s.clientLimiters.Load("2.2.2.2")

		assert.False(t, staleExists, "Stale limiter should have been removed")
		assert.True(t, recentExists, "Recent limiter should have been kept")
	})

	t.Run("Concurrency test for LoadOrStore", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		var wg sync.WaitGroup
		concurrency := 100

		// Have 100 goroutines try to get the limiter for the same IP concurrently
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = s.getClientLimiter("10.0.0.99")
			}()
		}

		wg.Wait()
		// If LoadOrStore is working correctly, there's no panic and we have 1 limiter.
		val, exists := s.clientLimiters.Load("10.0.0.99")
		assert.True(t, exists)
		assert.NotNil(t, val)
	})

	t.Run("App Token Per-User Rate Limit - Bearer and Query Token", func(t *testing.T) {
		s := &Server{
			appsEncryptionKey:  testAppsEncryptionKey,
			generalRateLimit:   rate.Every(time.Minute / 1),
			generalBurst:       1,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		tokenUser1, _, err := s.createAppToken("user-1", "site-1", "app-1")
		require.NoError(t, err)
		tokenUser2, _, err := s.createAppToken("user-2", "site-2", "app-2")
		require.NoError(t, err)

		// User 1 from shared egress IP 198.51.100.50 succeeds on first request
		reqUser1A := httptest.NewRequest("GET", "/api/agent/summary", nil)
		reqUser1A.RemoteAddr = "198.51.100.50:12345"
		reqUser1A.Header.Set("Authorization", "Bearer "+tokenUser1)
		wUser1A := httptest.NewRecorder()
		handler.ServeHTTP(wUser1A, reqUser1A)
		assert.Equal(t, http.StatusOK, wUser1A.Code)

		// User 1 from a DIFFERENT egress IP 198.51.100.51 via ?token= on /api/agent/summary is throttled (same user bucket)
		reqUser1B := httptest.NewRequest("GET", "/api/agent/summary?token="+url.QueryEscape(tokenUser1), nil)
		reqUser1B.RemoteAddr = "198.51.100.51:12345"
		wUser1B := httptest.NewRecorder()
		handler.ServeHTTP(wUser1B, reqUser1B)
		assert.Equal(t, http.StatusTooManyRequests, wUser1B.Code)

		// User 2 from the SAME shared egress IP 198.51.100.50 is allowed (independent user bucket)
		reqUser2 := httptest.NewRequest("GET", "/api/agent/summary?token="+url.QueryEscape(tokenUser2), nil)
		reqUser2.RemoteAddr = "198.51.100.50:12345"
		wUser2 := httptest.NewRecorder()
		handler.ServeHTTP(wUser2, reqUser2)
		assert.Equal(t, http.StatusOK, wUser2.Code)

		// Invalid app token from 198.51.100.50 falls back to IP limiter (first IP request succeeds, second is blocked)
		reqInvalid1 := httptest.NewRequest("GET", "/api/agent/summary", nil)
		reqInvalid1.RemoteAddr = "198.51.100.50:12345"
		reqInvalid1.Header.Set("Authorization", "Bearer rr_invalid")
		wInvalid1 := httptest.NewRecorder()
		handler.ServeHTTP(wInvalid1, reqInvalid1)
		assert.Equal(t, http.StatusOK, wInvalid1.Code)

		reqInvalid2 := httptest.NewRequest("GET", "/api/agent/summary", nil)
		reqInvalid2.RemoteAddr = "198.51.100.50:12345"
		reqInvalid2.Header.Set("Authorization", "Bearer rr_invalid")
		wInvalid2 := httptest.NewRecorder()
		handler.ServeHTTP(wInvalid2, reqInvalid2)
		assert.Equal(t, http.StatusTooManyRequests, wInvalid2.Code)
	})

	t.Run("Sensitive Endpoint Limit OAuth and Apps Minting Endpoints", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 1),
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		for idx, path := range []string{"/api/apps/new", "/api/oauth/client", "/api/oauth/approve", "/oauth/register", "/oauth/token"} {
			ip := fmt.Sprintf("198.51.100.%d:12345", 60+idx)

			req1 := httptest.NewRequest("POST", path, nil)
			req1.RemoteAddr = ip
			w1 := httptest.NewRecorder()
			handler.ServeHTTP(w1, req1)
			assert.Equal(t, http.StatusOK, w1.Code, "first request to %s should succeed", path)

			req2 := httptest.NewRequest("POST", path, nil)
			req2.RemoteAddr = ip
			w2 := httptest.NewRecorder()
			handler.ServeHTTP(w2, req2)
			assert.Equal(t, http.StatusTooManyRequests, w2.Code, "second request to %s should hit sensitive limit", path)
		}
	})

	t.Run("Rejects Multiple X-Forwarded-For Headers", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req := httptest.NewRequest("GET", "/api/settings", nil)
		req.Header.Add("X-Forwarded-For", "203.0.113.1")
		req.Header.Add("X-Forwarded-For", "198.51.100.1")
		req.RemoteAddr = "127.0.0.1:8080"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("IPv6 Same /64 Prefix Shares Limiter", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 1),
			generalBurst:       1,
			sensitiveRateLimit: rate.Every(time.Minute / 1),
			sensitiveBurst:     1,
		}
		handler := s.rateLimitMiddleware(dummyHandler)

		req1 := httptest.NewRequest("GET", "/api/settings", nil)
		req1.RemoteAddr = "[2606:4700:4700:1234::1]:12345"
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		req2 := httptest.NewRequest("GET", "/api/settings", nil)
		req2.RemoteAddr = "[2606:4700:4700:1234:abcd:ef01:2345:6789]:12345"
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})
}

func TestGetClientIP(t *testing.T) {
	t.Run("CF-Connecting-IP takes precedence when RemoteAddr is a Cloudflare IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("CF-Connecting-IP", "203.0.113.5")
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		req.RemoteAddr = "103.21.244.5:8080" // Cloudflare IP

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.5", ip)
	})

	t.Run("CF-Connecting-IP ignored when RemoteAddr is not a Cloudflare IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("CF-Connecting-IP", "203.0.113.5")
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		req.RemoteAddr = "127.0.0.1:8080" // Not Cloudflare IP

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("CF-Connecting-IP takes precedence when behind GCP LB and trustedRemoteIP is a Cloudflare IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// 130.211.0.1 is a load balancer IP; 103.21.244.5 is Cloudflare.
		req.Header.Set("X-Forwarded-For", "103.21.244.5, 130.211.0.1")
		req.Header.Set("CF-Connecting-IP", "203.0.113.5")
		req.RemoteAddr = "35.191.0.10:12345" // GFE IP

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.5", ip)
	})

	t.Run("CF-Connecting-IP ignored when behind GCP LB and trustedRemoteIP is not a Cloudflare IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// 130.211.0.1 is a load balancer IP; 198.51.100.10 is not Cloudflare.
		req.Header.Set("X-Forwarded-For", "198.51.100.10, 130.211.0.1")
		req.Header.Set("CF-Connecting-IP", "203.0.113.5")
		req.RemoteAddr = "35.191.0.10:12345" // GFE IP

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("X-Forwarded-For single public IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		req.RemoteAddr = "127.0.0.1:8080"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("X-Forwarded-For multiple IPs returns last public (prevents spoofing)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.10, 198.51.100.10")
		req.RemoteAddr = "127.0.0.1:8080"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("X-Forwarded-For all private IPs returns last", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
		req.RemoteAddr = "127.0.0.1:8080"

		ip := getClientIP(req)
		assert.Equal(t, "192.168.1.1", ip)
	})

	t.Run("X-Forwarded-For empty fallback to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "")
		req.RemoteAddr = "198.51.100.10:8080"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("No headers fallback to RemoteAddr with port", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.10:12345"

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.10", ip)
	})

	t.Run("No headers fallback to RemoteAddr without port", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.10"

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.10", ip)
	})

	t.Run("Invalid IP in X-Forwarded-For skipped", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "invalid-ip, 198.51.100.10")
		req.RemoteAddr = "127.0.0.1:8080"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("RemoteAddr is GCP IP - IPv4 - strips last IP from X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// 55.55.55.55 is a load balancer's public IP; 203.0.113.5 is the real client.
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 55.55.55.55")
		req.RemoteAddr = "35.191.0.10:12345"

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.5", ip)
	})

	t.Run("RemoteAddr is GCP IP - IPv6 - strips last IP from X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// 2600:feed::1 is a load balancer's public IP; 203.0.113.5 is the real client.
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 2600:feed::1")
		req.RemoteAddr = "[2600:2d00:1:1::5]:12345"

		ip := getClientIP(req)
		assert.Equal(t, "203.0.113.5", ip)
	})

	t.Run("RemoteAddr is GCP IP - only one IP in X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "35.191.0.1")
		req.RemoteAddr = "130.211.0.10"

		ip := getClientIP(req)
		assert.Equal(t, "130.211.0.10", ip)
	})

	t.Run("RemoteAddr is Cloud Run IP - trusts last value of X-Forwarded-For (does not strip)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.10")
		req.RemoteAddr = "169.254.8.1:12345"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("RemoteAddr is Cloud Run IP - trusts last value of X-Forwarded-For and resolves Cloudflare proxy", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 103.21.244.5") // 103.21.244.5 is Cloudflare IP
		req.Header.Set("CF-Connecting-IP", "198.51.100.10")
		req.RemoteAddr = "169.254.8.1:12345"

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})

	t.Run("Invalid CF-Connecting-IP ignored when RemoteAddr is a Cloudflare IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("CF-Connecting-IP", "not-a-valid-ip")
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		req.RemoteAddr = "103.21.244.5:8080" // Cloudflare IP

		ip := getClientIP(req)
		assert.Equal(t, "198.51.100.10", ip)
	})
}

func TestCleanupStaleLimiters(t *testing.T) {
	t.Run("EvictsStaleClientLimiters", func(t *testing.T) {
		s := &Server{
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}

		staleLimiter := s.getClientLimiter("198.51.100.1")
		staleLimiter.lastSeen.Store(time.Now().Add(-15 * time.Minute).UnixNano())
		_ = s.getClientLimiter("198.51.100.2")

		s.cleanupStaleLimiters()

		_, staleOK := s.clientLimiters.Load("198.51.100.1")
		assert.False(t, staleOK)
		_, freshOK := s.clientLimiters.Load("198.51.100.2")
		assert.True(t, freshOK)
	})
}
