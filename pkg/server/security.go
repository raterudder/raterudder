package server

import (
	"net/http"
)

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strict-Transport-Security: max-age=2 years
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")

		// Prevent MIME-sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// XSS protection
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Reporting endpoints
		w.Header().Set("Reporting-Endpoints", "browser-reports=\"/api/report/browser\"")

		// Content Security Policy
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' https://accounts.google.com/gsi/client https://appleid.apple.com; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://accounts.google.com/gsi/style; " +
			"font-src 'self' data: https://fonts.gstatic.com; " +
			"img-src 'self' data: https://accounts.google.com/gsi/ https://ssl.gstatic.com/accounts/ui/; " +
			"connect-src 'self' https://accounts.google.com/gsi/ https://appleid.apple.com; " +
			"frame-src 'self' https://accounts.google.com/gsi/ https://appleid.apple.com; " +
			"report-to browser-reports"
		w.Header().Set("Content-Security-Policy", csp)

		next.ServeHTTP(w, r)
	})
}
