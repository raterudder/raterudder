package common

import (
	_ "embed"
	"io"
	"net/http"
	"strings"
	"time"
)

//go:embed VERSION
var version string

type defaultTransport struct {
	transport http.RoundTripper
	userAgent string
}

// RoundTrip implements the
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
	v := strings.TrimSpace(version)
	userAgent := "RateRudder/" + v

	return &http.Client{
		Transport: &defaultTransport{
			transport: http.DefaultTransport,
			userAgent: userAgent,
		},
		Timeout: timeout,
	}
}
