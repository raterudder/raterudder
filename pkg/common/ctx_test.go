package common

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCtxFromRequest(t *testing.T) {
	t.Run("sets host in context", func(t *testing.T) {
		host := "example.com"
		req := httptest.NewRequest("GET", "http://"+host, nil)

		ctx := CtxFromRequest(req)

		assert.Equal(t, host, CtxHost(ctx))
	})
}

func TestCtxHost(t *testing.T) {
	t.Run("panics when host is missing", func(t *testing.T) {
		assert.Panics(t, func() {
			CtxHost(context.Background())
		})
	})
}

func TestCtxFromRequestMiddleware(t *testing.T) {
	t.Run("injects host into handler context", func(t *testing.T) {
		host := "test-host.local"
		req := httptest.NewRequest("GET", "http://"+host, nil)
		rr := httptest.NewRecorder()

		var capturedHost string
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHost = CtxHost(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := CtxFromRequestMiddleware(nextHandler)
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, host, capturedHost)
	})
}
