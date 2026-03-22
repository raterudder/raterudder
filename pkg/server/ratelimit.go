package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
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

var (
	// Rate limits
	generalRateLimit   = rate.Every(time.Minute / 30) // 30 per minute
	generalBurst       = 30
	sensitiveRateLimit = rate.Every(time.Minute / 5) // 5 per minute
	sensitiveBurst     = 5

	// Store for IP rate limiters
	clientLimiters sync.Map
)

func init() {
	// Start background goroutine to clean up stale limiters
	go func() {
		for {
			time.Sleep(time.Minute)
			cleanupStaleLimiters()
		}
	}()
}

func cleanupStaleLimiters() {
	threshold := time.Now().Add(-10 * time.Minute).UnixNano()
	clientLimiters.Range(func(key, value any) bool {
		limiter := value.(*clientRateLimiter)
		if limiter.lastSeen.Load() < threshold {
			clientLimiters.Delete(key)
		}
		return true // Continue iteration
	})
}

func getClientLimiter(ip string) *clientRateLimiter {
	// Optimistic load
	if val, ok := clientLimiters.Load(ip); ok {
		limiter := val.(*clientRateLimiter)
		limiter.lastSeen.Store(time.Now().UnixNano())
		return limiter
	}

	// Create a new limiter if not found
	newLimiter := &clientRateLimiter{
		general:   rate.NewLimiter(generalRateLimit, generalBurst),
		sensitive: rate.NewLimiter(sensitiveRateLimit, sensitiveBurst),
	}
	newLimiter.lastSeen.Store(time.Now().UnixNano())

	// Try to store, but handle the case where another goroutine beat us to it
	actual, loaded := clientLimiters.LoadOrStore(ip, newLimiter)
	if loaded {
		limiter := actual.(*clientRateLimiter)
		limiter.lastSeen.Store(time.Now().UnixNano())
		return limiter
	}

	return newLimiter
}

func getClientIP(r *http.Request) string {
	// 1. Check Cloudflare header first
	cfIP := r.Header.Get("CF-Connecting-IP")
	if cfIP != "" {
		return strings.TrimSpace(cfIP)
	}

	// 2. Check X-Forwarded-For and find the first public IP
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		for _, ipStr := range ips {
			ipStr = strings.TrimSpace(ipStr)
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				return ipStr
			}
		}
		// If no public IP found but header exists, return the first one (it might be all private)
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
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
		limiter := getClientLimiter(ip)

		// Check if it's a sensitive endpoint
		isSensitive := r.URL.Path == "/api/auth/login" ||
			r.URL.Path == "/api/join" ||
			r.URL.Path == "/api/update"

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
