package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestWebHandler(t *testing.T) {
	// Setup basics for server
	mockU := &mockUtility{}
	mockS := &mockStorage{}
	mockE := &mockESS{}
	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockE)

	mockUMap := utility.NewMap(nil)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          true,
		MinBatterySOC:   5.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil).Maybe()
	mockS.On("Ping", mock.Anything).Return(nil)

	// Create a map-based filesystem for testing
	testFS := fstest.MapFS{
		"index.html":     {Data: []byte("<html>index</html>")},
		"assets/main.js": {Data: []byte("console.log('hello');")},
		"logo_192.png":   {Data: []byte("pngdata")},
	}

	t.Run("Serve Existing Web File", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		// Add a non-asset file to test map
		testFS["favicon.ico"] = &fstest.MapFile{Data: []byte("icon")}

		req := httptest.NewRequest("GET", "/favicon.ico", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body := w.Body.String()
		assert.Equal(t, "icon", body)
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Index on Root", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Matching HTML File for Public Subpage", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		testFS["utilities.html"] = &fstest.MapFile{Data: []byte("<html>utilities seo</html>")}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/utilities", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>utilities seo</html>", w.Body.String())
	})

	t.Run("Serve Index on Unknown Route", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/some/random/route", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Index on Unknown Route with Trailing Slash", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/wp-admin/css/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
	})

	t.Run("Proxy to Dev Server", func(t *testing.T) {
		// Start a mock dev server
		devServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dev server response"))
		}))
		defer devServer.Close()

		t.Setenv("VITE_DEV_SERVER_URL", devServer.URL)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			bypassAuth: true,
			singleSite: true,
			controller: controller.NewController(),
			devProxy:   devServer.URL, // Point to our mock dev server
		}

		// This uses setupHandler which reads srv.devProxy
		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "dev server response", w.Body.String())
	})

	t.Run("Serve Not Found on .well-known", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/.well-known/some-file", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Serve Existing Asset File", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/assets/main.js", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		if assert.Equal(t, http.StatusOK, resp.StatusCode) {
			assert.Equal(t, "console.log('hello');", w.Body.String())
		}
	})

	t.Run("Serve Not Found on Missing Asset", func(t *testing.T) {
		srv := &Server{
			utilities:        mockUMap,
			ess:              mockP,
			storage:          mockS,
			listenAddr:       ":8080",
			controller:       controller.NewController(),
			webCacheDuration: 5 * time.Minute,
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/assets/stale-chunk-9f8a7b.js", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.NotContains(t, w.Body.String(), "<html>index</html>")
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Assetlinks on .well-known", func(t *testing.T) {
		fs := fstest.MapFS{
			".well-known/assetlinks.json": {Data: []byte("[]\n")},
		}
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(fs))
		mux.Handle("/", srv.webHandler(fs, fileServer))

		req := httptest.NewRequest("GET", "/.well-known/assetlinks.json", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		if assert.Equal(t, http.StatusOK, resp.StatusCode) {
			assert.Equal(t, "[]\n", w.Body.String())
		}
	})

	t.Run("Server Header", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			serverName: "test-revision-123",
		}

		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, "test-revision-123", resp.Header.Get("Server"))
	})

	t.Run("web Cache Duration Header", func(t *testing.T) {
		srv := &Server{
			utilities:             mockUMap,
			ess:                   mockP,
			storage:               mockS,
			listenAddr:            ":8080",
			controller:            controller.NewController(),
			webCacheDuration:      5 * time.Minute,
			webImageCacheDuration: 30 * 24 * time.Hour,
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		t.Run("Regular File", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
		})

		t.Run("Image File", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/logo_192.png", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "public, max-age=2592000, stale-while-revalidate, stale-if-error", w.Header().Get("Cache-Control"))
		})
	})

	t.Run("Healthz Endpoint", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(nil)

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "ok", w.Body.String())
			mockS.AssertExpectations(t)
		})

		t.Run("Failure", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(assert.AnError)

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
			assert.Contains(t, w.Body.String(), "database health check failed")
			mockS.AssertExpectations(t)
		})
	})
}
func TestIsMultiSiteAdmin(t *testing.T) {
	server := &Server{
		adminEmails: []string{"admin@example.com"},
	}

	assert.True(t, server.isMultiSiteAdmin(types.User{Email: "admin@example.com"}))
	assert.False(t, server.isMultiSiteAdmin(types.User{Email: "user@example.com"}))
}

func TestConfigureOIDC(t *testing.T) {
	googleSrv, googlePriv := setupOIDCTest(t)
	defer googleSrv.Close()

	appleSrv, applePriv := setupOIDCTest(t)
	defer appleSrv.Close()

	webToken := generateTestTokenWithAudience(t, googleSrv.URL, googlePriv, "user@example.com", "user-sub", "web-audience")
	updateToken := generateTestTokenWithAudience(t, googleSrv.URL, googlePriv, "updater@example.com", "updater-sub", "update-audience")
	appleToken := generateTestTokenWithAudience(t, appleSrv.URL, applePriv, "apple@example.com", "apple-sub", "apple-audience")

	t.Run("OIDCAudiencesWithUpdateSpecificAudienceReusesGoogleProvider", func(t *testing.T) {
		var googleCalls int
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			if issuer == "https://accounts.google.com" {
				googleCalls++
				return oidc.NewProvider(ctx, googleSrv.URL)
			}
			return nil, assert.AnError
		}

		srv := &Server{}
		err := srv.configureOIDC(
			context.Background(),
			map[string]string{"google": "web-audience"},
			"",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Equal(t, 1, googleCalls, "google provider should be initialized once and reused")
		assert.Equal(t, map[string]string{"google": "web-audience"}, srv.oidcAudiences)
		assert.Len(t, srv.oidcVerifiers, 1)
		assert.NotContains(t, srv.oidcVerifiers, "google_update_specific")
		if assert.NotNil(t, srv.updateOIDCVerifier) {
			_, err := srv.updateOIDCVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
			_, err = srv.updateOIDCVerifier(context.Background(), webToken)
			assert.Error(t, err)
		}
		_, _, _, err = srv.authenticateToken(context.Background(), webToken, "google")
		assert.NoError(t, err)
		_, _, _, err = srv.authenticateToken(context.Background(), updateToken, "")
		assert.Error(t, err)
	})

	t.Run("LegacyOIDCAudienceWithUpdateSpecificAudience", func(t *testing.T) {
		var googleCalls int
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			if issuer == "https://accounts.google.com" {
				googleCalls++
				return oidc.NewProvider(ctx, googleSrv.URL)
			}
			return nil, assert.AnError
		}

		srv := &Server{}
		err := srv.configureOIDC(
			context.Background(),
			nil,
			"web-audience",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Equal(t, 1, googleCalls)
		assert.Equal(t, map[string]string{"google": "web-audience"}, srv.oidcAudiences)
		assert.Len(t, srv.oidcVerifiers, 1)
		if assert.NotNil(t, srv.updateOIDCVerifier) {
			_, err := srv.updateOIDCVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
			_, err = srv.updateOIDCVerifier(context.Background(), webToken)
			assert.Error(t, err)
		}
	})

	t.Run("UpdateSpecificAudienceOnlySingleSite", func(t *testing.T) {
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			if issuer == "https://accounts.google.com" {
				return oidc.NewProvider(ctx, googleSrv.URL)
			}
			return nil, assert.AnError
		}

		srv := &Server{singleSite: true}
		err := srv.configureOIDC(
			context.Background(),
			nil,
			"",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Empty(t, srv.oidcAudiences)
		assert.Empty(t, srv.oidcVerifiers)
		if assert.NotNil(t, srv.updateOIDCVerifier) {
			_, err := srv.updateOIDCVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
		}
		_, _, _, err = srv.authenticateToken(context.Background(), updateToken, "")
		assert.Error(t, err)
	})

	t.Run("AppleOIDCAudienceWithUpdateSpecificAudience", func(t *testing.T) {
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			switch issuer {
			case "https://accounts.google.com":
				return oidc.NewProvider(ctx, googleSrv.URL)
			case "https://appleid.apple.com":
				return oidc.NewProvider(ctx, appleSrv.URL)
			default:
				return nil, assert.AnError
			}
		}

		srv := &Server{}
		err := srv.configureOIDC(
			context.Background(),
			map[string]string{"apple": "apple-audience"},
			"",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"apple": "apple-audience"}, srv.oidcAudiences)
		assert.Len(t, srv.oidcVerifiers, 1)
		if assert.NotNil(t, srv.updateOIDCVerifier) {
			_, err := srv.updateOIDCVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
		}
		_, _, _, err = srv.authenticateToken(context.Background(), appleToken, "apple")
		assert.NoError(t, err)
		_, _, _, err = srv.authenticateToken(context.Background(), updateToken, "")
		assert.Error(t, err)
	})

	t.Run("UnsupportedClientReturnsError", func(t *testing.T) {
		srv := &Server{}
		err := srv.configureOIDC(
			context.Background(),
			map[string]string{"google_update_specific": "update-audience"},
			"",
			"",
			func(ctx context.Context, issuer string) (*oidc.Provider, error) {
				return oidc.NewProvider(ctx, googleSrv.URL)
			},
		)
		assert.ErrorContains(t, err, "unsupported oidc audience client")
	})
}
