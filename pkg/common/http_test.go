package common

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClient(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify User-Agent header
		userAgent := r.Header.Get("User-Agent")
		assert.Equal(t, "RateRudder/"+strings.TrimSpace(version), userAgent, "User-Agent should match expected format")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Test client creation
	timeout := 5 * time.Second
	client := HTTPClient(timeout)

	// Verify client settings
	assert.Equal(t, timeout, client.Timeout, "Timeout should be set correctly")
	assert.NotNil(t, client.Transport, "Transport should not be nil")

	// Test actual request
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIsBlockedSSRFDialIP(t *testing.T) {
	t.Run("AllowsPublicIPv4AndIPv6", func(t *testing.T) {
		assert.False(t, IsBlockedSSRFDialIP(netip.MustParseAddr("8.8.8.8")))
		assert.False(t, IsBlockedSSRFDialIP(netip.MustParseAddr("2606:4700:4700::1111")))
		assert.False(t, IsBlockedSSRFDialIP(netip.MustParseAddr("2001:4860:4860::8888")))
	})

	t.Run("BlocksLoopbackAndPrivate", func(t *testing.T) {
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("127.0.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::ffff:127.0.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("10.0.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("172.16.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("192.168.1.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("fd00::1")))
	})

	t.Run("BlocksMetadataAndLinkLocal", func(t *testing.T) {
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("169.254.169.254")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::ffff:169.254.169.254")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("fe80::1")))
	})

	t.Run("BlocksCGNATAndNAT64AndReserved", func(t *testing.T) {
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("100.64.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("0.0.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("192.88.99.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("255.255.255.255")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("64:ff9b::a9fe:a9fe")))
	})

	t.Run("BlocksIPv6TransitionAndNon2000Ranges", func(t *testing.T) {
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::127.0.0.1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::169.254.169.254")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("::ffff:0:169.254.169.254")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("2001::1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("2001:db8::1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("2002:7f00:1::1")))
		assert.True(t, IsBlockedSSRFDialIP(netip.MustParseAddr("fec0::1")))
	})
}

func TestSSRFSafeHTTPClient(t *testing.T) {
	t.Run("BlocksLoopbackDial", func(t *testing.T) {
		client := SSRFSafeHTTPClient(2 * time.Second)
		assert.Equal(t, 2*time.Second, client.Timeout)

		req, err := http.NewRequest("GET", "https://127.0.0.1:443/client.json", nil)
		require.NoError(t, err)

		_, err = client.Do(req)
		assert.ErrorContains(t, err, "blocked IP address")
	})

	t.Run("SetsUserAgentAndBlocksRedirects", func(t *testing.T) {
		var seenUserAgent string
		client := SSRFSafeHTTPClient(2 * time.Second)
		// Wrap a mock RoundTripper inside defaultTransport to verify User-Agent and CheckRedirect
		dt, ok := client.Transport.(*defaultTransport)
		require.True(t, ok)
		dt.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seenUserAgent = req.Header.Get("User-Agent")
			h := make(http.Header)
			h.Set("Location", "https://example.org/next")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     h,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		})

		req, err := http.NewRequest("GET", "https://example.com/client.json", nil)
		require.NoError(t, err)
		_, err = client.Do(req)
		assert.ErrorContains(t, err, "redirects are not allowed")
		assert.Equal(t, "RateRudder/"+strings.TrimSpace(version), seenUserAgent)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
