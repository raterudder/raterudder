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
		scriptSrc := "'self' 'unsafe-inline'"
		styleSrc := "'self' 'unsafe-inline' https://fonts.googleapis.com"
		fontSrc := "'self' data: https://fonts.gstatic.com"
		imgSrc := "'self' data:"
		connectSrc := "'self'"
		frameSrc := "'self'"

		if _, ok := s.oidcAudiences["google"]; ok {
			scriptSrc += " https://accounts.google.com/gsi/client"
			styleSrc += " https://accounts.google.com/gsi/style"
			imgSrc += " https://accounts.google.com/gsi/ https://ssl.gstatic.com/accounts/ui/"
			connectSrc += " https://accounts.google.com/gsi/"
			frameSrc += " https://accounts.google.com/gsi/"
		}

		if _, ok := s.oidcAudiences["apple"]; ok {
			scriptSrc += " https://appleid.cdn-apple.com"
			connectSrc += " https://appleid.apple.com"
			frameSrc += " https://appleid.apple.com"
		}

		csp := "default-src 'self'; " +
			"script-src " + scriptSrc + "; " +
			"style-src " + styleSrc + "; " +
			"font-src " + fontSrc + "; " +
			"img-src " + imgSrc + "; " +
			"connect-src " + connectSrc + "; " +
			"frame-src " + frameSrc + "; " +
			"worker-src 'self' blob:; " +
			"report-to browser-reports"
		w.Header().Set("Content-Security-Policy", csp)

		next.ServeHTTP(w, r)
	})
}
