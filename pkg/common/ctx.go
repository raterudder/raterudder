package common

import (
	"context"
	"net/http"
)

type contextKey int

var httpHost contextKey = 1

// CtxFromRequest returns a context with information populated from the request
func CtxFromRequest(r *http.Request) context.Context {
	return context.WithValue(r.Context(), httpHost, r.Host)
}

// CtxHost returns the host from the context
func CtxHost(ctx context.Context) string {
	return ctx.Value(httpHost).(string)
}

// CtxFromRequestMiddleware returns a middleware that populates the context with
// information from the request
func CtxFromRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(CtxFromRequest(r)))
	})
}
