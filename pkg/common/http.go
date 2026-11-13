package common

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
)

//go:embed VERSION
var version string

var ssrfAllowedIPv6Prefix = netip.MustParsePrefix("2000::/3")

var blockedSSRFPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// IsBlockedSSRFDialIP reports whether ip is loopback, private, link-local,
// non-global-unicast, or within a reserved/transition range unsafe for outbound
// fetches to untrusted hosts.
func IsBlockedSSRFDialIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	if ip.Is6() && !ssrfAllowedIPv6Prefix.Contains(ip) {
		return true
	}
	for _, prefix := range blockedSSRFPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// Version returns the embedded RateRudder version string.
func Version() string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "v0.0.0"
	}
	return v
}

func userAgent() string {
	return "RateRudder/" + Version()
}

type defaultTransport struct {
	transport http.RoundTripper
	userAgent string
}

// RoundTrip sets the default User-Agent header and caps response bodies at 16MB.
func (t *defaultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original request's headers
	// which might be shared or reused
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)

	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Automatically wrap the response body with a 16MB LimitReader to prevent DoS memory exhaustion
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(resp.Body, 16*1024*1024),
		Closer: resp.Body,
	}

	return resp, nil
}

// HTTPClient returns a default http client with a default user-agent set
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &defaultTransport{
			transport: http.DefaultTransport,
			userAgent: userAgent(),
		},
		Timeout: timeout,
	}
}

// SSRFSafeHTTPClient returns an HTTP client that uses the standard RateRudder User-Agent,
// disables proxy environment variables, keep-alives, and HTTP redirects, and validates all
// resolved IP addresses at dial time against IsBlockedSSRFDialIP to prevent SSRF and DNS rebinding.
func SSRFSafeHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				log.Ctx(ctx).WarnContext(
					ctx,
					"ssrf-safe http client dns lookup failed",
					slog.String("host", host),
					slog.Any("error", err),
				)
				return nil, err
			}
			if len(ips) == 0 {
				log.Ctx(ctx).WarnContext(
					ctx,
					"ssrf-safe http client dns lookup returned no addresses",
					slog.String("host", host),
				)
				return nil, fmt.Errorf("no IP addresses found for %s", host)
			}
			for _, ip := range ips {
				if IsBlockedSSRFDialIP(ip) {
					log.Ctx(ctx).WarnContext(
						ctx,
						"ssrf-safe http client blocked address",
						slog.String("host", host),
						slog.String("ip", ip.String()),
					)
					return nil, fmt.Errorf("blocked IP address resolved for %s", host)
				}
			}
			dialer := &net.Dialer{Timeout: timeout}
			var lastErr error
			for _, ip := range ips {
				targetAddr := net.JoinHostPort(ip.Unmap().String(), port)
				conn, dialErr := dialer.DialContext(ctx, network, targetAddr)
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &defaultTransport{
			transport: transport,
			userAgent: userAgent(),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			var fromURL string
			if len(via) > 0 && via[len(via)-1].URL != nil {
				fromURL = via[len(via)-1].URL.Redacted()
			}
			log.Ctx(req.Context()).WarnContext(
				req.Context(),
				"ssrf-safe http client blocked redirect",
				slog.String("fromURL", fromURL),
				slog.String("redirectHost", req.URL.Host),
			)
			return errors.New("redirects are not allowed")
		},
	}
}
