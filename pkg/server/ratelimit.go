package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"golang.org/x/time/rate"
)

type clientRateLimiter struct {
	general   *rate.Limiter
	sensitive *rate.Limiter
	lastSeen  time.Time
}

var (
	// Rate limits
	generalRateLimit   = rate.Every(time.Minute / 100) // 100 per minute
	generalBurst       = 100
	sensitiveRateLimit = rate.Every(time.Minute / 5) // 5 per minute
	sensitiveBurst     = 5

	// Store for IP rate limiters
	clientLimiters   = make(map[string]*clientRateLimiter)
	clientLimiterMux sync.Mutex
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
	clientLimiterMux.Lock()
	defer clientLimiterMux.Unlock()

	threshold := time.Now().Add(-10 * time.Minute)
	for ip, limiter := range clientLimiters {
		if limiter.lastSeen.Before(threshold) {
			delete(clientLimiters, ip)
		}
	}
}

func getClientLimiter(ip string) *clientRateLimiter {
	clientLimiterMux.Lock()
	defer clientLimiterMux.Unlock()

	limiter, exists := clientLimiters[ip]
	if !exists {
		limiter = &clientRateLimiter{
			general:   rate.NewLimiter(generalRateLimit, generalBurst),
			sensitive: rate.NewLimiter(sensitiveRateLimit, sensitiveBurst),
		}
		clientLimiters[ip] = limiter
	}
	limiter.lastSeen = time.Now()
	return limiter
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract IP address from request
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			var err error
			ip, _, err = net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// Fallback if RemoteAddr doesn't have a port
				ip = r.RemoteAddr
			}
		} else {
			// X-Forwarded-For can contain multiple IPs, the first one is the client
			ip = strings.Split(ip, ",")[0]
			ip = strings.TrimSpace(ip)
		}

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
