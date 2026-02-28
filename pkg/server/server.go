package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/raterudder/raterudder/web"
)

const (
	authTokenCookie = "auth_token"
	SiteIDAll       = "ALL"
)

type contextKey string

const (
	siteIDContextKey         contextKey = "siteID"
	allUserSitesContextKey   contextKey = "allUserSites"
	userContextKey           contextKey = "user"
	userToRegisterContextKey contextKey = "userToRegister"
)

// tokenVerifier is a function that validates a Google or Apple ID Token.
type tokenVerifier func(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)

// Server handles the HTTP API and control logic for the RateRudder system.
// It orchestrates interactions between the utility provider, ESS, and storage.
type Server struct {
	utilities  *utility.Map
	ess        *ess.Map
	storage    storage.Database
	controller *controller.Controller

	listenAddr string
	devProxy   string
	httpServer *http.Server

	updateSpecificEmail string
	adminEmails         []string
	oidcAudiences       map[string]string
	oidcVerifiers       map[string]tokenVerifier
	bypassAuth          bool
	singleSite          bool
	encryptionKey       string
	release             string
	serverName          string
	webCacheDuration    time.Duration
	showHidden          bool
}

// Configured initializes the Server with dependencies.
// It uses lflag to register command-line flags for configuration.
func Configured(u *utility.Map, e *ess.Map, s storage.Database) *Server {
	srv := &Server{
		utilities:  u,
		ess:        e,
		storage:    s,
		controller: controller.NewController(),
		serverName: "raterudder",
	}
	revision := os.Getenv("K_REVISION")
	if revision != "" {
		srv.serverName = revision
	}

	// get the port from PORT when running in cloud run
	port := os.Getenv("PORT")
	if port == "" {
		// otherwise default to 8080
		port = "8080"
	}

	listenAddr := lflag.String("http-listen", ":"+port, "HTTP server listen address")
	devProxy := lflag.String("dev-proxy", "", "Address of the dev server (e.g. http://localhost:5173)")
	updateSpecificEmail := lflag.String("update-specific-email", "", "email to validate for /api/update")
	adminEmails := lflag.String("admin-emails", "", "comma-delimited list of email addresses allowed to update settings via IAP")
	oidcAudience := lflag.String("oidc-audience", "", "token to use for id tokens audience to validate")
	oidcAudiences := map[string]string{}
	lflag.JSON(&oidcAudiences, "oidc-audiences", oidcAudiences, "JSON map of provider (google/apple) to audience/client ID")
	updateSpecificAudience := lflag.String("update-specific-audience", "", "Google-specific legacy audience to validate for /api/update")
	singleSite := lflag.Bool("single-site", false, "Enable single-site mode (disables siteID requirement)")
	showHidden := lflag.Bool("show-hidden", false, "Expose hidden providers in lists via the API")
	encryptionKey := lflag.RequiredString("credentials-encryption-key", "Key for encrypting credentials")
	release := lflag.String("release", "production", "Release environment (production or staging)")
	webCacheDuration := lflag.Duration("web-cache-duration", 0, "Duration to cache web files (e.g. 1h, 5m). 0 means no cache.")

	lflag.Do(func() {
		srv.listenAddr = *listenAddr
		srv.devProxy = *devProxy
		srv.updateSpecificEmail = *updateSpecificEmail
		if *adminEmails != "" {
			srv.adminEmails = strings.Split(*adminEmails, ",")
			for i, email := range srv.adminEmails {
				srv.adminEmails[i] = strings.TrimSpace(email)
			}
		}
		var googleProvider *oidc.Provider
		if len(oidcAudiences) > 0 {
			srv.oidcAudiences = make(map[string]string, len(oidcAudiences))
			srv.oidcVerifiers = make(map[string]tokenVerifier, len(oidcAudiences))
			for n, a := range oidcAudiences {
				switch n {
				case "google":
					provider, err := oidc.NewProvider(context.Background(), "https://accounts.google.com")
					if err != nil {
						log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
						os.Exit(1)
					}
					srv.oidcVerifiers[n] = provider.Verifier(&oidc.Config{ClientID: a}).Verify
					srv.oidcAudiences[n] = a
				case "apple":
					provider, err := oidc.NewProvider(context.Background(), "https://appleid.apple.com")
					if err != nil {
						log.Ctx(context.Background()).Error("failed to initialize Apple OIDC provider", slog.Any("error", err))
						os.Exit(1)
					}
					srv.oidcVerifiers[n] = provider.Verifier(&oidc.Config{ClientID: a}).Verify
					srv.oidcAudiences[n] = a
				default:
					log.Ctx(context.Background()).Error("unsupported oidc audience client", slog.String("client", n))
					os.Exit(1)
				}
			}
		} else if *oidcAudience != "" {
			var err error
			googleProvider, err = oidc.NewProvider(context.Background(), "https://accounts.google.com")
			if err != nil {
				log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
				os.Exit(1)
			}
			srv.oidcVerifiers = map[string]tokenVerifier{
				"google": googleProvider.Verifier(&oidc.Config{ClientID: *oidcAudience}).Verify,
			}
			srv.oidcAudiences = map[string]string{
				"google": *oidcAudience,
			}
		}
		if *updateSpecificAudience != "" {
			if srv.oidcVerifiers == nil {
				srv.oidcVerifiers = map[string]tokenVerifier{}
			}
			if googleProvider == nil {
				var err error
				googleProvider, err = oidc.NewProvider(context.Background(), "https://accounts.google.com")
				if err != nil {
					log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
					os.Exit(1)
				}
				srv.oidcVerifiers["google_update_specific"] = googleProvider.Verifier(&oidc.Config{ClientID: *updateSpecificAudience}).Verify
			}
		}
		srv.singleSite = *singleSite
		srv.showHidden = *showHidden
		srv.release = *release
		srv.webCacheDuration = *webCacheDuration

		if len(*encryptionKey) != 32 {
			log.Ctx(context.Background()).Error("credentials-encryption-key must be 32 characters")
			os.Exit(1)
		}
		srv.encryptionKey = *encryptionKey

		if srv.devProxy != "" && len(srv.oidcAudiences) == 0 && len(srv.adminEmails) == 0 {
			srv.bypassAuth = true
		}
	})

	return srv
}

func (s *Server) setupHandler() http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/update", s.handleUpdate)
	apiMux.HandleFunc("POST /api/updateSites", s.handleUpdateSites)
	apiMux.HandleFunc("GET /api/history/prices", s.handleHistoryPrices)
	apiMux.HandleFunc("GET /api/history/actions", s.handleHistoryActions)
	apiMux.HandleFunc("GET /api/history/savings", s.handleHistorySavings)
	apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
	apiMux.HandleFunc("POST /api/settings", s.handleUpdateSettings)
	apiMux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	apiMux.HandleFunc("POST /api/auth/login", s.handleLogin)
	apiMux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	apiMux.HandleFunc("GET /api/forecast", s.handleForecast)
	apiMux.HandleFunc("POST /api/join", s.handleJoin)
	apiMux.HandleFunc("GET /api/list/utilities", s.handleListUtilities)
	apiMux.HandleFunc("GET /api/list/ess", s.handleListESS)
	apiMux.HandleFunc("GET /api/list/sites", s.handleListSites)
	apiMux.HandleFunc("POST /api/feedback", s.handleSubmitFeedback)
	apiMux.HandleFunc("GET /api/list/feedback", s.handleListFeedback)

	mux := http.NewServeMux()
	mux.Handle("/api/", s.authMiddleware(apiMux))

	// serve the web frontend, either from the embedded filesystem or from the dev server
	if s.devProxy != "" {
		u, err := url.Parse(s.devProxy)
		if err != nil {
			panic(fmt.Errorf("invalid dev-proxy url (%s): %w", s.devProxy, err))
		}
		mux.Handle("/", httputil.NewSingleHostReverseProxy(u))
	} else {
		distFS, err := fs.Sub(web.DistFS, "dist")
		if err != nil {
			panic(fmt.Errorf("failed to get web dist fs: %w", err))
		}
		fileServer := http.FileServer(http.FS(distFS))
		mux.Handle("/", s.webHandler(distFS, fileServer))
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	return s.revisionMiddleware(gziphandler.GzipHandler(s.securityHeadersMiddleware(mux)))
}

func (s *Server) getSiteID(r *http.Request) string {
	if siteID, ok := r.Context().Value(siteIDContextKey).(string); ok {
		return siteID
	}
	// we want to have a stack trace when this happens
	panic("no siteID in context")
}

func (s *Server) getAllUserSites(r *http.Request) []types.UserSite {
	if sites, ok := r.Context().Value(allUserSitesContextKey).([]types.UserSite); ok {
		return sites
	}
	return nil
}

func (s *Server) getUser(r *http.Request) types.User {
	if user, ok := r.Context().Value(userContextKey).(types.User); ok {
		return user
	}
	return types.User{}
}

// Run starts the HTTP server and blocks until the context is canceled or an error occurs.
// It also handles graceful shutdown when the context is done.
func (s *Server) Run(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:         s.listenAddr,
		Handler:      s.setupHandler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// use a channel to capturing server errors
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		log.Ctx(ctx).InfoContext(ctx, "starting server", slog.String("addr", s.listenAddr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		// Context canceled, shut down gracefully
		log.Ctx(ctx).InfoContext(ctx, "shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}
		return nil
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	}
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: msg}); err != nil {
		slog.Warn("failed to write error response", slog.Any("error", err))
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) webHandler(dir fs.FS, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Default to serving index.html for unknown paths (SPA)
		if r.URL.Path != "/" {
			// Check if the file exists in the filesystem
			f, err := dir.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err == nil {
				f.Close()
			} else if errors.Is(err, fs.ErrNotExist) {
				// Don't fallback to index.html for .well-known
				if strings.HasPrefix(r.URL.Path, "/.well-known/") {
					// we don't write JSON here because we don't know what file type is expected
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				// If file doesn't exist, serve index.html
				r.URL.Path = "/"
			} else {
				log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to open file", "error", err)
				// we don't write JSON here because we don't know what file type is expected
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		}
		// cache SPA files if duration is set
		if s.webCacheDuration > 0 {
			w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(s.webCacheDuration.Seconds())))
		}

		h.ServeHTTP(w, r)
	}
}

func (s *Server) revisionMiddleware(next http.Handler) http.Handler {
	if s.serverName == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", s.serverName)
		next.ServeHTTP(w, r)
	})
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// isMultiSiteAdmin returns true if the user's email is in the adminEmails list.
func (s *Server) isMultiSiteAdmin(user types.User) bool {
	for _, adminEmail := range s.adminEmails {
		if user.Email == adminEmail {
			return true
		}
	}
	return false
}
