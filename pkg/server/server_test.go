package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		t.Run("DefaultSkipsPing", func(t *testing.T) {
			mockS := &mockStorage{}

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "ok", w.Body.String())
			mockS.AssertNotCalled(t, "Ping", mock.Anything)
			mockS.AssertExpectations(t)
		})

		t.Run("PingSuccess", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(nil).Once()

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz?ping=true", nil)
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

			req := httptest.NewRequest("GET", "/healthz?ping=true", nil)
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
	t.Run("ServesHealthzWhileFeatureConfigBlocksAndFallsBackOnError", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)
		token := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-123")

		mockS := &mockStorage{}
		featsStarted := make(chan struct{})
		featsRelease := make(chan struct{})
		var once sync.Once
		mockS.On("GetFeatureConfig", mock.Anything, "production").Run(func(args mock.Arguments) {
			once.Do(func() { close(featsStarted) })
			reqCtx := args.Get(0).(context.Context)
			select {
			case <-featsRelease:
			case <-reqCtx.Done():
			}
		}).Return(feats.Config{}, time.Time{}, errors.New("firestore unavailable")).Once()

		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
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

		require.Eventually(t, func() bool {
			return srv.boundAddr() != ""
		}, 2*time.Second, 5*time.Millisecond)

		select {
		case <-featsStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for async GetFeatureConfig warm-up to start")
		}

		// While GetFeatureConfig is still blocked, /healthz must immediately respond 200 OK without calling storage.Ping
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://" + srv.boundAddr() + "/healthz")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		mockS.AssertNotCalled(t, "Ping", mock.Anything)

		// Start a concurrent authenticated settings request during cold warm-up, then unblock GetFeatureConfig with an error
		settingsDone := make(chan *http.Response, 1)
		settingsErr := make(chan error, 1)
		go func() {
			req, reqErr := http.NewRequest(http.MethodGet, "http://"+srv.boundAddr()+"/api/settings?siteID=site-1", nil)
			if reqErr != nil {
				settingsErr <- reqErr
				return
			}
			req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})
			r, doErr := client.Do(req)
			if doErr != nil {
				settingsErr <- doErr
				return
			}
			settingsDone <- r
		}()

		close(featsRelease)

		select {
		case err := <-settingsErr:
			require.NoError(t, err)
		case settingsResp := <-settingsDone:
			defer settingsResp.Body.Close()
			require.Equal(t, http.StatusOK, settingsResp.StatusCode)
			var body SettingsRes
			require.NoError(t, json.NewDecoder(settingsResp.Body).Decode(&body))
			if assert.NotNil(t, body.Features) {
				assert.False(t, body.Features["serverTestAlpha"])
				assert.True(t, body.Features["serverTestBeta"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for concurrent request during cold feature warm-up")
		}

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
