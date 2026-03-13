package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	// A simple mock handler to wrap with our middleware
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	t.Run("Default Headers (No Audiences)", func(t *testing.T) {
		srv := &Server{
			oidcAudiences: map[string]string{},
		}

		handler := srv.securityHeadersMiddleware(mockHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		resp := w.Result()

		// Assert base security headers
		assert.Equal(t, "max-age=63072000; includeSubDomains", resp.Header.Get("Strict-Transport-Security"))
		assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
		assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))
		assert.Equal(t, "1; mode=block", resp.Header.Get("X-XSS-Protection"))
		assert.Equal(t, "browser-reports=\"/api/report/browser\"", resp.Header.Get("Reporting-Endpoints"))

		csp := resp.Header.Get("Content-Security-Policy")
		assert.Contains(t, csp, "default-src 'self'")
		assert.Contains(t, csp, "worker-src 'self' blob:")
		// Google/Apple endpoints should be excluded by default
		assert.NotContains(t, csp, "accounts.google.com")
		assert.NotContains(t, csp, "appleid.apple.com")
		assert.NotContains(t, csp, "appleid.cdn-apple.com")
	})

	t.Run("With Google Audience", func(t *testing.T) {
		srv := &Server{
			oidcAudiences: map[string]string{
				"google": "test-client-id.apps.googleusercontent.com",
			},
		}

		handler := srv.securityHeadersMiddleware(mockHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		resp := w.Result()
		csp := resp.Header.Get("Content-Security-Policy")

		assert.Contains(t, csp, "script-src 'self' 'unsafe-inline' https://accounts.google.com/gsi/client")
		assert.Contains(t, csp, "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://accounts.google.com/gsi/style")
		assert.Contains(t, csp, "img-src 'self' data: https://accounts.google.com/gsi/ https://ssl.gstatic.com/accounts/ui/")
		assert.Contains(t, csp, "connect-src 'self' https://accounts.google.com/gsi/")
		assert.Contains(t, csp, "frame-src 'self' https://accounts.google.com/gsi/")

		// Apple should not be present
		assert.NotContains(t, csp, "appleid")
	})

	t.Run("With Apple Audience", func(t *testing.T) {
		srv := &Server{
			oidcAudiences: map[string]string{
				"apple": "com.raterudder.auth",
			},
		}

		handler := srv.securityHeadersMiddleware(mockHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		resp := w.Result()
		csp := resp.Header.Get("Content-Security-Policy")

		assert.Contains(t, csp, "script-src 'self' 'unsafe-inline' https://appleid.cdn-apple.com")
		assert.Contains(t, csp, "connect-src 'self' https://appleid.apple.com")
		assert.Contains(t, csp, "frame-src 'self' https://appleid.apple.com")

		// Google should not be present
		assert.NotContains(t, csp, "accounts.google.com")
	})

	t.Run("With Both Audiences", func(t *testing.T) {
		srv := &Server{
			oidcAudiences: map[string]string{
				"google": "google-client",
				"apple":  "apple-client",
			},
		}

		handler := srv.securityHeadersMiddleware(mockHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		resp := w.Result()
		csp := resp.Header.Get("Content-Security-Policy")

		// Both should be correctly configured together
		assert.Contains(t, csp, "https://accounts.google.com/gsi/client", "script-src should contain Google domains")
		assert.Contains(t, csp, "https://appleid.cdn-apple.com", "script-src should contain Apple domains")

		// The exact order might be specific, so we just check for the presence of both within the directives
		assert.Contains(t, csp, "connect-src 'self'")
		assert.Contains(t, csp, "https://accounts.google.com/gsi/")
		assert.Contains(t, csp, "https://appleid.apple.com")

		assert.Contains(t, csp, "frame-src 'self'")
		assert.Contains(t, csp, "https://accounts.google.com/gsi/")
		assert.Contains(t, csp, "https://appleid.apple.com")
	})
}
