package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"golang.org/x/time/rate"
)

type clientRateLimiter struct {
	general   *rate.Limiter
	sensitive *rate.Limiter
	lastSeen  atomic.Int64
}

func (s *Server) startCleanupStaleLimiters() {
	// Start background goroutine to clean up stale limiters
	go func() {
		for {
			time.Sleep(time.Minute)
			s.cleanupStaleLimiters()
		}
	}()
}

func (s *Server) cleanupStaleLimiters() {
	threshold := time.Now().Add(-10 * time.Minute).UnixNano()
	s.clientLimiters.Range(func(key, value any) bool {
		limiter := value.(*clientRateLimiter)
		if limiter.lastSeen.Load() < threshold {
			s.clientLimiters.Delete(key)
		}
		return true // Continue iteration
	})
}

func (s *Server) getClientLimiter(ip string) *clientRateLimiter {
	// Optimistic load
	if val, ok := s.clientLimiters.Load(ip); ok {
		limiter := val.(*clientRateLimiter)
		limiter.lastSeen.Store(time.Now().UnixNano())
		return limiter
	}

	// Create a new limiter if not found
	newLimiter := &clientRateLimiter{
		general:   rate.NewLimiter(s.generalRateLimit, s.generalBurst),
		sensitive: rate.NewLimiter(s.sensitiveRateLimit, s.sensitiveBurst),
	}
	newLimiter.lastSeen.Store(time.Now().UnixNano())

	// Try to store, but handle the case where another goroutine beat us to it
	actual, loaded := s.clientLimiters.LoadOrStore(ip, newLimiter)
	if loaded {
		limiter := actual.(*clientRateLimiter)
		limiter.lastSeen.Store(time.Now().UnixNano())
		return limiter
	}

	return newLimiter
}

func getClientIP(r *http.Request) string {
	// 1. Check X-Forwarded-For and find the last public IP
	// We parse this in reverse to prevent spoofing of IPs.
	// We assume this is not behind an external load balancer because if it was
	// then the last public IP would be the load balancer's IP itself.
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		for i := len(ips) - 1; i >= 0; i-- {
			ipStr := strings.TrimSpace(ips[i])
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				return ipStr
			}
		}
		// If no public IP found but header exists, return the last one (it might be all private)
		if len(ips) > 0 {
			return strings.TrimSpace(ips[len(ips)-1])
		}
	}

	// 3. Fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ip := getClientIP(r)
		limiter := s.getClientLimiter(ip)

		// Check if it's a sensitive endpoint
		isSensitive := r.URL.Path == "/api/auth/login" ||
			r.URL.Path == "/api/auth/logout" ||
			r.URL.Path == "/api/join" ||
			r.URL.Path == "/api/update" ||
			r.URL.Path == "/api/updateSites" ||
			r.URL.Path == "/api/tesla/register"

		// Apply sensitive rate limit first if applicable
		if isSensitive {
			if !limiter.sensitive.Allow() {
				log.Ctx(ctx).WarnContext(ctx, "sensitive rate limit exceeded", slog.String("ip", ip), slog.String("path", r.URL.Path))
				writeJSONError(w, "too many requests", http.StatusTooManyRequests)
				return
			}
		}

		// Apply general rate limit
		if !limiter.general.Allow() {
			log.Ctx(ctx).WarnContext(ctx, "general rate limit exceeded", slog.String("ip", ip), slog.String("path", r.URL.Path))
			writeJSONError(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
