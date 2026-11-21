package server

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
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

type openRecorderFS struct {
	fsys   fs.FS
	onOpen func(name string)
}

func (r openRecorderFS) Open(name string) (fs.File, error) {
	if r.onOpen != nil {
		r.onOpen(name)
	}
	return r.fsys.Open(name)
}

type errorFS struct {
	err error
}

func (e errorFS) Open(name string) (fs.File, error) {
	return nil, e.err
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

	t.Run("SubFS Normalized and Invalid Paths", func(t *testing.T) {
		embeddedFS := fstest.MapFS{
			"dist/index.html":                  {Data: []byte("<html>index</html>")},
			"dist/assets/main.js":              {Data: []byte("console.log('hello');")},
			"dist/.well-known/assetlinks.json": {Data: []byte("[]\n")},
			"dist/docs/index.html":             {Data: []byte("<html>docs</html>")},
		}
		subFS, err := fs.Sub(embeddedFS, "dist")
		require.NoError(t, err)

		srv := &Server{
			utilities:        mockUMap,
			ess:              mockP,
			storage:          mockS,
			listenAddr:       ":8080",
			controller:       controller.NewController(),
			webCacheDuration: 5 * time.Minute,
		}

		newSubMux := func() *http.ServeMux {
			mux := http.NewServeMux()
			fileServer := http.FileServer(http.FS(subFS))
			mux.Handle("/", srv.webHandler(subFS, fileServer))
			return mux
		}

		newRecordedMux := func(fsys fs.FS) (*http.ServeMux, *[]string) {
			var opened []string
			wrapped := openRecorderFS{
				fsys: fsys,
				onOpen: func(name string) {
					opened = append(opened, name)
				},
			}
			mux := http.NewServeMux()
			fileServer := http.FileServer(http.FS(subFS))
			mux.Handle("/", srv.webHandler(wrapped, fileServer))
			return mux, &opened
		}

		newRequestWithLogger := func(target string) (*http.Request, *bytes.Buffer) {
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req = req.WithContext(log.With(req.Context(), logger))
			return req, &logBuf
		}

		t.Run("Encoded Leading Slash Unknown File Falls Back to Index", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2F.env.prod")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "<html>index</html>", w.Body.String())
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded Root Slash Serves Index Without Opening Slash", func(t *testing.T) {
			mux, opened := newRecordedMux(subFS)
			req, logBuf := newRequestWithLogger("/%2F")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "<html>index</html>", w.Body.String())
			assert.Empty(t, *opened)
			assert.Empty(t, logBuf.String())
		})

		t.Run("DotDot Root Serves Index Without Opening Slash", func(t *testing.T) {
			mux, opened := newRecordedMux(subFS)
			req, logBuf := newRequestWithLogger("/%2e%2e")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "<html>index</html>", w.Body.String())
			assert.Empty(t, *opened)
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded DotDot Unknown File Falls Back to Index", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2e%2e/.env")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "<html>index</html>", w.Body.String())
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded Leading Slash WellKnown Missing Returns Not Found", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2F.well-known/missing")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.NotContains(t, w.Body.String(), "<html>index</html>")
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded Leading Slash WellKnown Directory Returns Not Found", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2F.well-known/")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded Leading Slash Existing WellKnown File Serves Content", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2F.well-known/assetlinks.json")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			if assert.Equal(t, http.StatusOK, resp.StatusCode) {
				assert.Equal(t, "[]\n", w.Body.String())
			}
			assert.Empty(t, logBuf.String())
		})

		t.Run("Encoded Leading Slash Assets Missing Returns Not Found", func(t *testing.T) {
			mux := newSubMux()
			req, logBuf := newRequestWithLogger("/%2Fassets/missing.js")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.NotContains(t, w.Body.String(), "<html>index</html>")
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, logBuf.String())
		})

		t.Run("Invalid UTF8 Path Returns Not Found Without Calling FileServer", func(t *testing.T) {
			fileServerCalled := false
			mux := http.NewServeMux()
			mux.Handle("/", srv.webHandler(subFS, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fileServerCalled = true
			})))

			req, logBuf := newRequestWithLogger("/%ff")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.False(t, fileServerCalled)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, logBuf.String())
		})

		t.Run("Filesystem ErrInvalid Returns Not Found Without Error Log", func(t *testing.T) {
			errInvalidFS := errorFS{err: &fs.PathError{Op: "open", Path: "bad", Err: fs.ErrInvalid}}
			mux, _ := newRecordedMux(errInvalidFS)
			req, logBuf := newRequestWithLogger("/bad")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, logBuf.String())
		})

		t.Run("Unexpected Filesystem Error Logs Error and Returns 500", func(t *testing.T) {
			boomFS := errorFS{err: errors.New("disk read failure")}
			mux, _ := newRecordedMux(boomFS)
			req, logBuf := newRequestWithLogger("/boom")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Contains(t, logBuf.String(), "failed to open file")
		})

		t.Run("Preserves Trailing Slash For Directory Redirects", func(t *testing.T) {
			mux := newSubMux()

			reqNoSlash, _ := newRequestWithLogger("/%2Fdocs")
			wNoSlash := httptest.NewRecorder()
			mux.ServeHTTP(wNoSlash, reqNoSlash)
			assert.Equal(t, http.StatusMovedPermanently, wNoSlash.Result().StatusCode)
			assert.Equal(t, "docs/", wNoSlash.Header().Get("Location"))

			reqWithSlash, _ := newRequestWithLogger("/%2Fdocs/")
			wWithSlash := httptest.NewRecorder()
			mux.ServeHTTP(wWithSlash, reqWithSlash)
			assert.Equal(t, http.StatusOK, wWithSlash.Result().StatusCode)
			assert.Equal(t, "<html>docs</html>", wWithSlash.Body.String())
		})
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

	t.Run("UpdateSpecificAudienceOnly", func(t *testing.T) {
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			if issuer == "https://accounts.google.com" {
				return oidc.NewProvider(ctx, googleSrv.URL)
			}
			return nil, assert.AnError
		}

		srv := &Server{}
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
