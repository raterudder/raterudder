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

var gcpIPRanges = []struct {
	ipnet *net.IPNet
}{
	{mustParseCIDR("35.191.0.0/16")},
	{mustParseCIDR("130.211.0.0/22")},
	{mustParseCIDR("2600:2d00:1:1::/64")},
	{mustParseCIDR("2600:2d00:1:b029::/64")},
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return ipnet
}

func isGCPIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, r := range gcpIPRanges {
		if r.ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

var cloudRunIPRanges = []struct {
	ipnet *net.IPNet
}{
	{mustParseCIDR("169.254.0.0/16")},
}

func isCloudRunIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, r := range cloudRunIPRanges {
		if r.ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

var cloudflareIPRanges = []struct {
	ipnet *net.IPNet
}{
	// IPv4
	{mustParseCIDR("173.245.48.0/20")},
	{mustParseCIDR("103.21.244.0/22")},
	{mustParseCIDR("103.22.200.0/22")},
	{mustParseCIDR("103.31.4.0/22")},
	{mustParseCIDR("141.101.64.0/18")},
	{mustParseCIDR("108.162.192.0/18")},
	{mustParseCIDR("190.93.240.0/20")},
	{mustParseCIDR("188.114.96.0/20")},
	{mustParseCIDR("197.234.240.0/22")},
	{mustParseCIDR("198.41.128.0/17")},
	{mustParseCIDR("162.158.0.0/15")},
	{mustParseCIDR("104.16.0.0/13")},
	{mustParseCIDR("104.24.0.0/14")},
	{mustParseCIDR("172.64.0.0/13")},
	{mustParseCIDR("131.0.72.0/22")},
	// IPv6
	{mustParseCIDR("2400:cb00::/32")},
	{mustParseCIDR("2606:4700::/32")},
	{mustParseCIDR("2803:f800::/32")},
	{mustParseCIDR("2405:b500::/32")},
	{mustParseCIDR("2405:8100::/32")},
	{mustParseCIDR("2a06:98c0::/29")},
	{mustParseCIDR("2c0f:f248::/32")},
}

func isCloudflareIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, r := range cloudflareIPRanges {
		if r.ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

func getClientIP(r *http.Request) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	// we trust RemoteAddr
	trustedRemoteIP := remoteIP

	// Check X-Forwarded-For and find the last public IP
	// We parse this in reverse to prevent spoofing of IPs.
	// We assume this is not behind an external load balancer because if it was
	// then the last public IP would be the load balancer's IP itself.
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")

		var behindCloudRun bool
		// If the request comes from a Google Cloud Platform (GCP) load balancer,
		// RemoteAddr will be a GCP GFE (Google Front End) IP address.
		// Google Cloud HTTP(S) Load Balancing appends the client IP and the
		// load balancer's forwarding rule IP to the X-Forwarded-For header.
		// Thus, we must strip the last IP (which is the load balancer's forwarding rule IP)
		// to correctly identify the client IP.
		// Details: https://docs.cloud.google.com/load-balancing/docs/https#x-forwarded-for_header
		behindGCPLB := isGCPIP(remoteIP)
		if behindGCPLB {
			if len(ips) > 0 {
				ips = ips[:len(ips)-1]
			}
		} else {
			behindCloudRun = isCloudRunIP(remoteIP)
		}

		found := false
		for i := len(ips) - 1; i >= 0; i-- {
			ipStr := strings.TrimSpace(ips[i])
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				remoteIP = ipStr
				if behindGCPLB || behindCloudRun {
					// we trust the last IP since it was set by Google/Cloud Run
					trustedRemoteIP = ipStr
				}
				found = true
				break
			}
		}
		if !found && len(ips) > 0 {
			remoteIP = strings.TrimSpace(ips[len(ips)-1])
		}
	}

	// Only trust the CF-Connecting-IP header if the direct connecting IP (trustedRemoteIP)
	// is identified as a Cloudflare proxy IP.
	if isCloudflareIP(trustedRemoteIP) {
		cfIP := r.Header.Get("CF-Connecting-IP")
		if cfIP != "" {
			return strings.TrimSpace(cfIP)
		}
	}

	return remoteIP
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
			r.URL.Path == "/api/tesla/register" ||
			r.URL.Path == "/api/ess/stage"

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
