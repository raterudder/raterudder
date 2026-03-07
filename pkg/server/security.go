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

		next.ServeHTTP(w, r)
	})
}
