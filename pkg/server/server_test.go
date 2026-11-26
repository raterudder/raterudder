package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
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
	mockS.On("Ping", mock.Anything).Return(nil).Maybe()

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
		t.Run("StorageNotConfigured", func(t *testing.T) {
			srv := &Server{}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
			assert.Contains(t, w.Body.String(), "database health check failed")
		})

		t.Run("PingSuccess", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(nil).Once()

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

		t.Run("PingFailure", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(assert.AnError).Once()

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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestConfigured(t *testing.T) {
	if os.Getenv("TEST_CONFIGURED_UNSUPPORTED_OIDC") == "1" {
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			os.Exit(2)
			return nil, errors.New("unexpected network call")
		})
		lflag.Reset()
		_ = Configured(utility.NewMap(nil), ess.NewMap(), &mockStorage{})
		lflag.Parse(lflag.SourceStub{
			"credentials-encryption-key": "12345678901234567890123456789012",
			"session-encryption-key":     "12345678901234567890123456789012",
			"oidc-audiences":             `{"github":"bad-audience"}`,
		})
		return
	}

	t.Run("CompletesWithoutOutboundOIDCCalls", func(t *testing.T) {
		var outboundCalls atomic.Int32
		origTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			outboundCalls.Add(1)
			return nil, errors.New("unexpected outbound HTTP request during Configured")
		})
		t.Cleanup(func() {
			http.DefaultTransport = origTransport
			lflag.Reset()
		})

		lflag.Reset()
		srv := Configured(utility.NewMap(nil), ess.NewMap(), &mockStorage{})
		lflag.Parse(lflag.SourceStub{
			"credentials-encryption-key": "12345678901234567890123456789012",
			"session-encryption-key":     "12345678901234567890123456789012",
			"oidc-audiences":             `{"google":"web-audience","apple":"apple-audience"}`,
			"update-specific-audience":   "update-audience",
		})

		assert.Equal(t, int32(0), outboundCalls.Load(), "Configured must not perform outbound OIDC HTTP calls")
		assert.Equal(t, map[string]string{
			"google": "web-audience",
			"apple":  "apple-audience",
		}, srv.oidcAudiences)
		assert.Equal(t, "update-audience", srv.updateSpecificAudience)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
		w := httptest.NewRecorder()
		srv.setupHandler().ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		var status authStatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
		assert.True(t, status.AuthRequired)
		assert.Equal(t, map[string]string{
			"google": "web-audience",
			"apple":  "apple-audience",
		}, status.ClientIDs)
		assert.Equal(t, int32(0), outboundCalls.Load(), "GET /api/auth/status must not trigger OIDC discovery")
	})

	t.Run("RejectsUnsupportedOIDCClient", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConfigured$")
		cmd.Env = append(os.Environ(), "TEST_CONFIGURED_UNSUPPORTED_OIDC=1")
		err := cmd.Run()
		var exitErr *exec.ExitError
		if assert.ErrorAs(t, err, &exitErr) {
			assert.Equal(t, 1, exitErr.ExitCode())
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("LoadsFeatureConfigFirstAndServesHealthzWithPing", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)
		token := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-123")

		mockS := &mockStorage{}
		featsStarted := make(chan struct{})
		featsRelease := make(chan struct{})
		var observedTimeout time.Duration
		var addrDuringFeatsLoad string

		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		cfg := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}

		var srv *Server
		mockS.On("GetFeatureConfig", mock.Anything, "production").Run(func(args mock.Arguments) {
			reqCtx := args.Get(0).(context.Context)
			if deadline, ok := reqCtx.Deadline(); ok {
				observedTimeout = time.Until(deadline)
			}
			addrDuringFeatsLoad = srv.boundAddr()
			close(featsStarted)
			<-featsRelease
		}).Return(cfg, now, nil).Once()

		mockS.On("Ping", mock.Anything).Return(nil).Once()
		mockS.On("Ping", mock.Anything).Return(errors.New("db down")).Once()

		mockS.On("GetUser", mock.Anything, "google:user-123").Return(types.User{
			ID:    "google:user-123",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "google:user-123"}},
		}, nil).Once()
		mockS.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			DryRun:        true,
			MinBatterySOC: 20,
			Release:       "production",
		}, types.CurrentSettingsVersion, now, nil).Once()

		srv = &Server{
			utilities:            utility.NewMap(mockS),
			ess:                  ess.NewMap(),
			storage:              mockS,
			controller:           controller.NewController(),
			listenAddr:           "127.0.0.1:0",
			release:              "production",
			sessionEncryptionKey: "12345678901234567890123456789012",
			encryptionKey:        "12345678901234567890123456789012",
			oidcAudiences:        map[string]string{"google": "test-audience"},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			generalRateLimit:   rate.Inf,
			generalBurst:       30,
			sensitiveRateLimit: rate.Inf,
			sensitiveBurst:     5,
			nowFunc:            func() time.Time { return now },
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runErr := make(chan error, 1)
		go func() {
			runErr <- srv.Run(ctx)
		}()

		select {
		case <-featsStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for initial GetFeatureConfig")
		}

		// Listener must not be bound yet while initial GetFeatureConfig is still running
		assert.Empty(t, addrDuringFeatsLoad)
		assert.Empty(t, srv.boundAddr())
		assert.Greater(t, observedTimeout, 3*time.Second)
		assert.LessOrEqual(t, observedTimeout, featsFetchTimeout+500*time.Millisecond)

		close(featsRelease)

		require.Eventually(t, func() bool {
			return srv.boundAddr() != ""
		}, 2*time.Second, 5*time.Millisecond)

		client := &http.Client{Timeout: 2 * time.Second}

		// /healthz runs storage.Ping unconditionally: 200 on nil error, 503 on error
		resp, err := client.Get("http://" + srv.boundAddr() + "/healthz")
		require.NoError(t, err)
		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "ok", string(respBody))

		respFail, err := client.Get("http://" + srv.boundAddr() + "/healthz")
		require.NoError(t, err)
		respFailBody, err := io.ReadAll(respFail.Body)
		_ = respFail.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusServiceUnavailable, respFail.StatusCode)
		assert.Contains(t, string(respFailBody), "database health check failed")

		// Feature flags are already loaded when the first request arrives
		req, err := http.NewRequest(http.MethodGet, "http://"+srv.boundAddr()+"/api/settings?siteID=site-1", nil)
		require.NoError(t, err)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})
		settingsResp, err := client.Do(req)
		require.NoError(t, err)
		var body SettingsRes
		decodeErr := json.NewDecoder(settingsResp.Body).Decode(&body)
		_, _ = io.Copy(io.Discard, settingsResp.Body)
		_ = settingsResp.Body.Close()
		require.NoError(t, decodeErr)
		require.Equal(t, http.StatusOK, settingsResp.StatusCode)
		if assert.NotNil(t, body.Features) {
			assert.True(t, body.Features["serverTestAlpha"])
			assert.True(t, body.Features["serverTestBeta"])
		}

		cancel()
		require.NoError(t, <-runErr)
		mockS.AssertExpectations(t)
	})

	t.Run("RetriesFailedInitialFeatureConfigInBackgroundAfterListen", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)
		token := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-123")

		mockS := &mockStorage{}
		retryStarted := make(chan struct{})
		retryRelease := make(chan struct{})
		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

		cfg := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}

		// 1st attempt (synchronous before Listen): fails immediately
		mockS.On("GetFeatureConfig", mock.Anything, "production").
			Return(feats.Config{}, time.Time{}, errors.New("firestore unavailable")).Once()

		// 2nd attempt (1st background retry after Listen): blocks until we check fallback defaults, then also fails
		mockS.On("GetFeatureConfig", mock.Anything, "production").Run(func(args mock.Arguments) {
			close(retryStarted)
			<-retryRelease
		}).Return(feats.Config{}, time.Time{}, errors.New("transient error")).Once()

		// 3rd attempt (2nd background retry after Listen): succeeds and stops retrying
		mockS.On("GetFeatureConfig", mock.Anything, "production").
			Return(cfg, now, nil).Once()

		mockS.On("Ping", mock.Anything).Return(nil).Once()
		mockS.On("GetUser", mock.Anything, "google:user-123").Return(types.User{
			ID:    "google:user-123",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
		}, nil).Twice()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "google:user-123"}},
		}, nil).Twice()
		mockS.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			DryRun:        true,
			MinBatterySOC: 20,
			Release:       "production",
		}, types.CurrentSettingsVersion, now, nil).Twice()

		srv := &Server{
			utilities:            utility.NewMap(mockS),
			ess:                  ess.NewMap(),
			storage:              mockS,
			controller:           controller.NewController(),
			listenAddr:           "127.0.0.1:0",
			release:              "production",
			sessionEncryptionKey: "12345678901234567890123456789012",
			encryptionKey:        "12345678901234567890123456789012",
			oidcAudiences:        map[string]string{"google": "test-audience"},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			featsRetryBackoff:  15 * time.Millisecond,
			generalRateLimit:   rate.Inf,
			generalBurst:       30,
			sensitiveRateLimit: rate.Inf,
			sensitiveBurst:     5,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runErr := make(chan error, 1)
		go func() {
			runErr <- srv.Run(ctx)
		}()

		hasFeats := func() bool {
			srv.featsMu.RLock()
			defer srv.featsMu.RUnlock()
			return srv.featsEvaluator != nil
		}

		require.Eventually(t, func() bool {
			return srv.boundAddr() != ""
		}, 2*time.Second, 5*time.Millisecond)
		assert.False(t, hasFeats())

		client := &http.Client{Timeout: 2 * time.Second}
		healthResp, err := client.Get("http://" + srv.boundAddr() + "/healthz")
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, healthResp.Body)
		_ = healthResp.Body.Close()
		assert.Equal(t, http.StatusOK, healthResp.StatusCode)

		// Wait for 1st background retry to start, then release after checking fallback defaults
		select {
		case <-retryStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for background GetFeatureConfig retry")
		}

		close(retryRelease)

		// While GetFeatureConfig has not yet succeeded (within retry backoff after 2nd failure), requests use registered defaults
		req1, err := http.NewRequest(http.MethodGet, "http://"+srv.boundAddr()+"/api/settings?siteID=site-1", nil)
		require.NoError(t, err)
		req1.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})
		resp1, err := client.Do(req1)
		require.NoError(t, err)
		var body1 SettingsRes
		decodeErr1 := json.NewDecoder(resp1.Body).Decode(&body1)
		_, _ = io.Copy(io.Discard, resp1.Body)
		_ = resp1.Body.Close()
		require.NoError(t, decodeErr1)
		require.Equal(t, http.StatusOK, resp1.StatusCode)
		if assert.NotNil(t, body1.Features) {
			assert.False(t, body1.Features["serverTestAlpha"])
			assert.True(t, body1.Features["serverTestBeta"])
		}

		// 3rd attempt in background succeeds and loads evaluator
		require.Eventually(t, hasFeats, 2*time.Second, 5*time.Millisecond)

		req2, err := http.NewRequest(http.MethodGet, "http://"+srv.boundAddr()+"/api/settings?siteID=site-1", nil)
		require.NoError(t, err)
		req2.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})
		resp2, err := client.Do(req2)
		require.NoError(t, err)
		var body2 SettingsRes
		decodeErr2 := json.NewDecoder(resp2.Body).Decode(&body2)
		_, _ = io.Copy(io.Discard, resp2.Body)
		_ = resp2.Body.Close()
		require.NoError(t, decodeErr2)
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		if assert.NotNil(t, body2.Features) {
			assert.True(t, body2.Features["serverTestAlpha"])
			assert.True(t, body2.Features["serverTestBeta"])
		}

		cancel()
		require.NoError(t, <-runErr)
		mockS.AssertExpectations(t)
	})

	t.Run("WarmsOIDCInParallelWithFeatureConfigWithoutBlockingStartup", func(t *testing.T) {
		googleSrv, googlePriv := setupOIDCTest(t)
		defer googleSrv.Close()

		appleSrv, applePriv := setupOIDCTest(t)
		defer appleSrv.Close()

		var appleFail atomic.Bool
		appleFail.Store(true)
		var googleDiscoveryCalls, appleDiscoveryCalls atomic.Int32
		appleFirstAttemptStarted := make(chan struct{})
		appleFirstAttemptRelease := make(chan struct{})
		warmOIDCDone := make(chan struct{})
		var appleOnce sync.Once

		flakyAppleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callNum := appleDiscoveryCalls.Add(1)
			if callNum == 1 {
				appleOnce.Do(func() { close(appleFirstAttemptStarted) })
				select {
				case <-appleFirstAttemptRelease:
				case <-r.Context().Done():
					return
				}
			}
			if appleFail.Load() {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			appleSrv.Config.Handler.ServeHTTP(w, r)
		}))
		defer flakyAppleSrv.Close()

		webToken := generateTestTokenWithAudience(t, googleSrv.URL, googlePriv, "user@example.com", "user-sub", "web-audience")
		updateToken := generateTestTokenWithAudience(t, googleSrv.URL, googlePriv, "updater@example.com", "updater-sub", "update-audience")
		appleToken := generateTestTokenWithAudience(t, appleSrv.URL, applePriv, "apple@example.com", "apple-sub", "apple-audience")

		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			switch issuer {
			case "https://accounts.google.com":
				googleDiscoveryCalls.Add(1)
				return oidc.NewProvider(ctx, googleSrv.URL)
			case "https://appleid.apple.com":
				insecureCtx := oidc.InsecureIssuerURLContext(ctx, appleSrv.URL)
				return oidc.NewProvider(insecureCtx, flakyAppleSrv.URL)
			default:
				return nil, assert.AnError
			}
		}

		mockS := &mockStorage{}
		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		featsStarted := make(chan struct{})
		featsRelease := make(chan struct{})
		mockS.On("GetFeatureConfig", mock.Anything, "production").Run(func(args mock.Arguments) {
			close(featsStarted)
			<-featsRelease
		}).Return(feats.Config{}, now, nil).Once()
		mockS.On("Ping", mock.Anything).Return(nil).Once()

		srv := &Server{
			utilities:            utility.NewMap(mockS),
			ess:                  ess.NewMap(),
			storage:              mockS,
			controller:           controller.NewController(),
			listenAddr:           "127.0.0.1:0",
			release:              "production",
			updateSpecificEmail:  "updater@example.com",
			sessionEncryptionKey: "12345678901234567890123456789012",
			encryptionKey:        "12345678901234567890123456789012",
			generalRateLimit:     rate.Inf,
			generalBurst:         30,
			sensitiveRateLimit:   rate.Inf,
			sensitiveBurst:       5,
			onWarmOIDCDone:       func() { close(warmOIDCDone) },
		}
		require.NoError(t, srv.configureOIDC(
			context.Background(),
			map[string]string{
				"google": "web-audience",
				"apple":  "apple-audience",
			},
			"",
			"update-audience",
			newProvider,
		))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runErr := make(chan error, 1)
		go func() {
			runErr <- srv.Run(ctx)
		}()

		// OIDC warmup starts in parallel while initial GetFeatureConfig is still in progress
		select {
		case <-featsStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for initial GetFeatureConfig to start")
		}
		select {
		case <-appleFirstAttemptStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for parallel Apple OIDC warmup to start")
		}

		// Google web and update verifiers warm independently via singleflight while both features and Apple are blocked
		require.Eventually(t, func() bool {
			srv.oidcMu.RLock()
			defer srv.oidcMu.RUnlock()
			return srv.oidcVerifiers["google"] != nil && srv.updateOIDCVerifier != nil
		}, 2*time.Second, 5*time.Millisecond)
		assert.Equal(t, int32(1), googleDiscoveryCalls.Load())

		// Release feature config -> server binds listener and serves /healthz while Apple OIDC warmup is still blocked
		close(featsRelease)
		require.Eventually(t, func() bool {
			return srv.boundAddr() != ""
		}, 2*time.Second, 5*time.Millisecond)

		client := &http.Client{Timeout: 2 * time.Second}
		healthResp, err := client.Get("http://" + srv.boundAddr() + "/healthz")
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, healthResp.Body)
		_ = healthResp.Body.Close()
		assert.Equal(t, http.StatusOK, healthResp.StatusCode)

		// Unblock Apple warmup attempt -> fails with 502 and continues without retrying in background
		close(appleFirstAttemptRelease)
		select {
		case <-warmOIDCDone:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for OIDC warmup completion")
		}

		srv.oidcMu.RLock()
		assert.Nil(t, srv.oidcProviders["https://appleid.apple.com"])
		assert.Nil(t, srv.oidcVerifiers["apple"])
		srv.oidcMu.RUnlock()
		assert.Equal(t, int32(1), appleDiscoveryCalls.Load())

		// Google verifiers are already warm and validate tokens without new discovery calls
		email, sub, _, err := srv.authenticateToken(context.Background(), webToken, "google")
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", email)
		assert.Equal(t, "google:user-sub", sub)

		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		_, err = updateVerifier(context.Background(), updateToken)
		require.NoError(t, err)
		assert.Equal(t, int32(1), googleDiscoveryCalls.Load())

		// When Apple recovers, a subsequent request lazily discovers Apple and caches it
		appleFail.Store(false)
		email, sub, _, err = srv.authenticateToken(context.Background(), appleToken, "apple")
		require.NoError(t, err)
		assert.Equal(t, "apple@example.com", email)
		assert.Equal(t, "apple:apple-sub", sub)
		assert.Greater(t, appleDiscoveryCalls.Load(), int32(1))

		appleCallsAfterRecovery := appleDiscoveryCalls.Load()
		email, sub, _, err = srv.authenticateToken(context.Background(), appleToken, "apple")
		require.NoError(t, err)
		assert.Equal(t, "apple@example.com", email)
		assert.Equal(t, "apple:apple-sub", sub)
		assert.Equal(t, appleCallsAfterRecovery, appleDiscoveryCalls.Load())

		cancel()
		require.NoError(t, <-runErr)
		mockS.AssertExpectations(t)
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
		assert.Equal(t, 0, googleCalls, "configureOIDC must not initialize providers eagerly")
		assert.Equal(t, map[string]string{"google": "web-audience"}, srv.oidcAudiences)
		assert.Empty(t, srv.oidcVerifiers)

		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		if assert.NotNil(t, updateVerifier) {
			_, err := updateVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
			_, err = updateVerifier(context.Background(), webToken)
			assert.Error(t, err)
		}

		_, _, _, err = srv.authenticateToken(context.Background(), webToken, "google")
		assert.NoError(t, err)
		assert.Equal(t, 1, googleCalls, "google provider should be lazily initialized once and reused")
		assert.Len(t, srv.oidcVerifiers, 1)
		assert.NotContains(t, srv.oidcVerifiers, "google_update_specific")
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
		assert.Equal(t, 0, googleCalls)
		assert.Equal(t, map[string]string{"google": "web-audience"}, srv.oidcAudiences)

		_, _, _, err = srv.authenticateToken(context.Background(), webToken, "google")
		assert.NoError(t, err)
		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		if assert.NotNil(t, updateVerifier) {
			_, err := updateVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
			_, err = updateVerifier(context.Background(), webToken)
			assert.Error(t, err)
		}
		assert.Equal(t, 1, googleCalls)
		assert.Len(t, srv.oidcVerifiers, 1)
	})

	t.Run("UpdateSpecificAudienceOnly", func(t *testing.T) {
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
			"",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Equal(t, 0, googleCalls)
		assert.Empty(t, srv.oidcAudiences)
		assert.Empty(t, srv.oidcVerifiers)

		_, _, _, err = srv.authenticateToken(context.Background(), updateToken, "")
		assert.Error(t, err)
		assert.Equal(t, 0, googleCalls, "authenticateToken with no user audiences must not discover Google OIDC")

		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		if assert.NotNil(t, updateVerifier) {
			_, err := updateVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
		}
		assert.Equal(t, 1, googleCalls)
	})

	t.Run("AppleOIDCAudienceWithUpdateSpecificAudience", func(t *testing.T) {
		var googleCalls, appleCalls int
		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			switch issuer {
			case "https://accounts.google.com":
				googleCalls++
				return oidc.NewProvider(ctx, googleSrv.URL)
			case "https://appleid.apple.com":
				appleCalls++
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
		assert.Equal(t, 0, googleCalls)
		assert.Equal(t, 0, appleCalls)
		assert.Equal(t, map[string]string{"apple": "apple-audience"}, srv.oidcAudiences)

		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		if assert.NotNil(t, updateVerifier) {
			_, err := updateVerifier(context.Background(), updateToken)
			assert.NoError(t, err)
		}
		assert.Equal(t, 1, googleCalls)
		assert.Equal(t, 0, appleCalls, "update token verification must not initialize Apple OIDC")

		_, _, _, err = srv.authenticateToken(context.Background(), appleToken, "apple")
		assert.NoError(t, err)
		assert.Equal(t, 1, appleCalls)
		assert.Len(t, srv.oidcVerifiers, 1)
		_, _, _, err = srv.authenticateToken(context.Background(), updateToken, "")
		assert.Error(t, err)
	})

	t.Run("Apple502LeavesGoogleWorkingAndRecoversWhenAppleRecovers", func(t *testing.T) {
		var appleFail atomic.Bool
		appleFail.Store(true)
		var googleDiscoveryCalls, appleDiscoveryCalls atomic.Int32

		flakyAppleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appleDiscoveryCalls.Add(1)
			if appleFail.Load() {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			appleSrv.Config.Handler.ServeHTTP(w, r)
		}))
		defer flakyAppleSrv.Close()

		flakyAppleToken := generateTestTokenWithAudience(t, appleSrv.URL, applePriv, "apple@example.com", "apple-sub", "apple-audience")

		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			switch issuer {
			case "https://accounts.google.com":
				googleDiscoveryCalls.Add(1)
				return oidc.NewProvider(ctx, googleSrv.URL)
			case "https://appleid.apple.com":
				// Use InsecureIssuerURLContext so flakyAppleSrv URL can serve appleSrv's issuer claims on recovery
				insecureCtx := oidc.InsecureIssuerURLContext(ctx, appleSrv.URL)
				return oidc.NewProvider(insecureCtx, flakyAppleSrv.URL)
			default:
				return nil, assert.AnError
			}
		}

		srv := &Server{
			updateSpecificEmail: "updater@example.com",
		}
		err := srv.configureOIDC(
			context.Background(),
			map[string]string{
				"google": "web-audience",
				"apple":  "apple-audience",
			},
			"",
			"update-audience",
			newProvider,
		)
		require.NoError(t, err)
		assert.Equal(t, int32(0), googleDiscoveryCalls.Load())
		assert.Equal(t, int32(0), appleDiscoveryCalls.Load())

		// While Apple returns HTTP 502, Apple login fails
		_, _, _, err = srv.authenticateToken(context.Background(), flakyAppleToken, "apple")
		assert.ErrorContains(t, err, "502 Bad Gateway")
		assert.Equal(t, int32(1), appleDiscoveryCalls.Load())
		assert.Equal(t, int32(0), googleDiscoveryCalls.Load())

		// Google verification still works (both explicit client and empty client fallback)
		email, sub, _, err := srv.authenticateToken(context.Background(), webToken, "google")
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", email)
		assert.Equal(t, "google:user-sub", sub)
		assert.Equal(t, int32(1), googleDiscoveryCalls.Load())

		email, sub, _, err = srv.authenticateToken(context.Background(), webToken, "")
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", email)
		assert.Equal(t, "google:user-sub", sub)

		// Cloud Scheduler POST /api/updateSites also succeeds despite Apple outage
		schedReq := httptest.NewRequest(http.MethodPost, "/api/updateSites", nil)
		schedReq.Header.Set("Authorization", "Bearer "+updateToken)
		schedRec := httptest.NewRecorder()
		srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, srv.getUpdateSpecificAuth(r))
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(schedRec, schedReq)
		assert.Equal(t, http.StatusOK, schedRec.Code)
		assert.Equal(t, int32(1), googleDiscoveryCalls.Load(), "scheduler auth reuses cached Google provider")

		// Apple recovers (HTTP 200) -> Apple verification succeeds and caches provider
		appleFail.Store(false)
		callsBeforeRecovery := appleDiscoveryCalls.Load()
		email, sub, _, err = srv.authenticateToken(context.Background(), flakyAppleToken, "apple")
		require.NoError(t, err)
		assert.Equal(t, "apple@example.com", email)
		assert.Equal(t, "apple:apple-sub", sub)
		callsAfterFirstSuccess := appleDiscoveryCalls.Load()
		assert.Greater(t, callsAfterFirstSuccess, callsBeforeRecovery)

		// Subsequent Apple verification uses cached provider/JWKS without re-running discovery
		email, sub, _, err = srv.authenticateToken(context.Background(), flakyAppleToken, "apple")
		require.NoError(t, err)
		assert.Equal(t, "apple@example.com", email)
		assert.Equal(t, "apple:apple-sub", sub)
		assert.Equal(t, callsAfterFirstSuccess, appleDiscoveryCalls.Load())
	})

	t.Run("SingleflightDeduplicatesConcurrentDiscoveryWithFiveSecondTimeout", func(t *testing.T) {
		var discoveryCalls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		var observedTimeout time.Duration

		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			discoveryCalls.Add(1)
			if deadline, ok := ctx.Deadline(); ok {
				observedTimeout = time.Until(deadline)
			}
			once.Do(func() { close(started) })
			<-release
			return oidc.NewProvider(ctx, googleSrv.URL)
		}

		srv := &Server{}
		require.NoError(t, srv.configureOIDC(
			context.Background(),
			map[string]string{"google": "web-audience"},
			"",
			"update-audience",
			newProvider,
		))

		const callers = 6
		var wg sync.WaitGroup
		errs := make([]error, callers)
		for i := range callers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if idx%2 == 0 {
					_, _, _, errs[idx] = srv.authenticateToken(context.Background(), webToken, "google")
				} else {
					v, ok, err := srv.getUpdateOIDCVerifier(context.Background())
					if err != nil {
						errs[idx] = err
						return
					}
					if !ok || v == nil {
						errs[idx] = errors.New("missing update verifier")
						return
					}
					_, errs[idx] = v(context.Background(), updateToken)
				}
			}(i)
		}

		<-started
		close(release)
		wg.Wait()

		for _, err := range errs {
			assert.NoError(t, err)
		}
		assert.Equal(t, int32(1), discoveryCalls.Load(), "concurrent callers must share a single OIDC discovery flight")
		assert.Greater(t, observedTimeout, 3*time.Second)
		assert.LessOrEqual(t, observedTimeout, oidcDiscoveryTimeout+500*time.Millisecond)
	})

	t.Run("WarmOIDCWarmsHealthyProvidersOnceAndContinuesOnFailure", func(t *testing.T) {
		var googleCalls, appleCalls atomic.Int32
		var appleFail atomic.Bool
		appleFail.Store(true)

		flakyAppleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appleCalls.Add(1)
			if appleFail.Load() {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			appleSrv.Config.Handler.ServeHTTP(w, r)
		}))
		defer flakyAppleSrv.Close()

		newProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
			switch issuer {
			case "https://accounts.google.com":
				googleCalls.Add(1)
				return oidc.NewProvider(ctx, googleSrv.URL)
			case "https://appleid.apple.com":
				insecureCtx := oidc.InsecureIssuerURLContext(ctx, appleSrv.URL)
				return oidc.NewProvider(insecureCtx, flakyAppleSrv.URL)
			default:
				return nil, assert.AnError
			}
		}

		srv := &Server{}
		require.NoError(t, srv.configureOIDC(
			context.Background(),
			map[string]string{
				"google": "web-audience",
				"apple":  "apple-audience",
			},
			"",
			"update-audience",
			newProvider,
		))

		srv.warmOIDC(context.Background())

		assert.Equal(t, int32(1), googleCalls.Load())
		assert.Equal(t, int32(1), appleCalls.Load())

		_, _, _, err := srv.authenticateToken(context.Background(), webToken, "google")
		require.NoError(t, err)
		updateVerifier, hasUpdate, err := srv.getUpdateOIDCVerifier(context.Background())
		require.NoError(t, err)
		require.True(t, hasUpdate)
		_, err = updateVerifier(context.Background(), updateToken)
		require.NoError(t, err)
		assert.Equal(t, int32(1), googleCalls.Load())

		appleFail.Store(false)
		_, _, _, err = srv.authenticateToken(context.Background(), appleToken, "apple")
		require.NoError(t, err)
		assert.Greater(t, appleCalls.Load(), int32(1))
	})

	t.Run("UnsupportedClientReturnsError", func(t *testing.T) {
		var called bool
		srv := &Server{}
		err := srv.configureOIDC(
			context.Background(),
			map[string]string{"google_update_specific": "update-audience"},
			"",
			"",
			func(ctx context.Context, issuer string) (*oidc.Provider, error) {
				called = true
				return oidc.NewProvider(ctx, googleSrv.URL)
			},
		)
		assert.ErrorContains(t, err, "unsupported oidc audience client")
		assert.False(t, called)
	})
}

func TestCrossOriginProtectionMiddleware(t *testing.T) {
	oidcSrv, priv := setupOIDCTest(t)
	defer oidcSrv.Close()
	provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
	require.NoError(t, err)
	updateToken := generateTestTokenWithAudience(t, oidcSrv.URL, priv, "updater@example.com", "updater-sub", "update-audience")

	newTestServer := func(mockS *mockStorage) *Server {
		return &Server{
			utilities:            utility.NewMap(mockS),
			ess:                  ess.NewMap(),
			storage:              mockS,
			controller:           controller.NewController(),
			sessionEncryptionKey: "12345678901234567890123456789012",
			encryptionKey:        "12345678901234567890123456789012",
			updateSpecificEmail:  "updater@example.com",
			oidcAudiences:        map[string]string{"google": "web-audience"},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "web-audience"}).Verify,
			},
			updateOIDCVerifier: provider.Verifier(&oidc.Config{ClientID: "update-audience"}).Verify,
			generalRateLimit:   rate.Inf,
			generalBurst:       30,
			sensitiveRateLimit: rate.Inf,
			sensitiveBurst:     30,
		}
	}

	newRecordedRequest := func(method, target string, body io.Reader) (*http.Request, *bytes.Buffer) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
		req := httptest.NewRequest(method, target, body)
		req = req.WithContext(log.With(req.Context(), logger))
		return req, &logBuf
	}

	decodeLogLines := func(t *testing.T, buf *bytes.Buffer) []map[string]any {
		t.Helper()
		raw := strings.TrimSpace(buf.String())
		if raw == "" {
			return nil
		}
		lines := strings.Split(raw, "\n")
		records := make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			var rec map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &rec))
			records = append(records, rec)
		}
		return records
	}

	t.Run("CrossSiteSecFetchSiteLogsErrorWithoutBlocking", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		records := decodeLogLines(t, logBuf)
		if assert.Len(t, records, 1) {
			assert.Equal(t, "cross-origin request would be blocked", records[0]["msg"])
			assert.Equal(t, http.MethodPost, records[0]["method"])
			assert.Equal(t, "/api/auth/logout", records[0]["path"])
			assert.Equal(t, "cross-site", records[0]["secFetchSite"])
			assert.Equal(t, "https://attacker.example", records[0]["origin"])
			assert.Equal(t, "raterudder.example.com", records[0]["host"])
			assert.Equal(t, "text/plain", records[0]["contentType"])
			assert.NotEmpty(t, records[0]["error"])
		}
		mockS.AssertExpectations(t)
	})

	t.Run("MismatchedOriginWithoutSecFetchSiteLogsErrorWithoutBlocking", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/auth/logout", nil)
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		records := decodeLogLines(t, logBuf)
		if assert.Len(t, records, 1) {
			assert.Equal(t, "cross-origin request would be blocked", records[0]["msg"])
			assert.Equal(t, http.MethodPost, records[0]["method"])
			assert.Equal(t, "/api/auth/logout", records[0]["path"])
			assert.Equal(t, "", records[0]["secFetchSite"])
			assert.Equal(t, "https://attacker.example", records[0]["origin"])
			assert.Equal(t, "raterudder.example.com", records[0]["host"])
			assert.Equal(t, "application/x-www-form-urlencoded", records[0]["contentType"])
			assert.NotEmpty(t, records[0]["error"])
		}
		mockS.AssertExpectations(t)
	})

	t.Run("SameOriginSecFetchSiteDoesNotLogError", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "https://raterudder.example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeLogLines(t, logBuf))
		mockS.AssertExpectations(t)
	})

	t.Run("SecFetchSiteNoneDoesNotLogError", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "none")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeLogLines(t, logBuf))
		mockS.AssertExpectations(t)
	})

	t.Run("MatchingOriginWithoutSecFetchSiteDoesNotLogError", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/auth/logout", nil)
		req.Header.Set("Origin", "https://raterudder.example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeLogLines(t, logBuf))
		mockS.AssertExpectations(t)
	})

	t.Run("SafeGetRequestDoesNotLogErrorEvenCrossSite", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodGet, "https://raterudder.example.com/api/auth/status", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "https://attacker.example")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeLogLines(t, logBuf))
		mockS.AssertExpectations(t)
	})

	t.Run("HeaderlessNonBrowserPostDoesNotLogError", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("ListSitesSettings", mock.Anything, "", mock.Anything).Return(
			map[string]types.Settings{},
			map[string]int{},
			map[string]time.Time{},
			nil,
		).Once()
		srv := newTestServer(mockS)
		handler := srv.setupHandler()

		req, logBuf := newRecordedRequest(http.MethodPost, "https://raterudder.example.com/api/updateSites", nil)
		req.Header.Set("Authorization", "Bearer "+updateToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeLogLines(t, logBuf))
		mockS.AssertExpectations(t)
	})
}
