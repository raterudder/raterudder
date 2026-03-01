package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = log.With(ctx, log.Ctx(ctx).With(slog.String("reqPath", r.URL.Path)))

		allowNoLogin := r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/status" || r.URL.Path == "/api/join"
		ignoreUserNotFound := r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/join" || r.URL.Path == "/api/auth/status" || r.URL.Path == "/api/auth/logout"
		isUpdatePath := r.URL.Path == "/api/update" || r.URL.Path == "/api/updateSites"
		ignoreSiteID := r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/status" || r.URL.Path == "/api/auth/logout" || r.URL.Path == "/api/list/sites"

		// extract SiteID
		var siteID string
		if r.Method == http.MethodGet {
			siteID = r.URL.Query().Get("siteID")
		} else {
			// read body to find SiteID
			var bodyBytes []byte
			if r.Body != nil {
				var err error
				bodyBytes, err = io.ReadAll(r.Body)
				if err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to read request body", slog.Any("error", err))
					// since we failed to read, don't return JSON error
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				// restore body for next handler
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			// try to unmarshal just the SiteID
			if len(bodyBytes) > 0 {
				var justSiteID struct {
					SiteID string `json:"siteID"`
				}
				err := json.Unmarshal(bodyBytes, &justSiteID)
				if err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to unmarshal request body", slog.Any("error", err))
					// since we failed to read, don't return JSON error
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				siteID = justSiteID.SiteID
			}
		}

		var email string
		var userID string
		// userFound is true if the user is a real user found in the database
		var userFound bool
		// user might be a mock/fake user if this is bypassAuth or singleSite
		var user types.User
		var authViaUpdateSpecific bool
		// handle authentication
		if s.bypassAuth {
			user = types.User{
				ID:    "fake",
				Sites: []types.UserSite{{ID: types.SiteIDNone}},
				Admin: true,
			}
			ctx = context.WithValue(ctx, userContextKey, user)
		} else {
			var authSuccess bool

			// Check /api/update specific auth
			if isUpdatePath {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "" {
					if !strings.HasPrefix(authHeader, "Bearer ") {
						log.Ctx(ctx).ErrorContext(ctx, "invalid auth header", slog.String("header", authHeader))
						writeJSONError(w, "invalid auth header", http.StatusBadRequest)
						return
					}
					token := strings.TrimPrefix(authHeader, "Bearer ")
					specificClient := ""
					if _, ok := s.oidcAudiences["google_update_specific"]; ok {
						specificClient = "google_update_specific"
					}
					emailRet, subjectRet, _, err := s.authenticateToken(ctx, token, specificClient)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "update token validation failed", slog.Any("error", err))
					} else {
						email = emailRet
						userID = subjectRet
						if s.updateSpecificEmail != "" && subtle.ConstantTimeCompare([]byte(email), []byte(s.updateSpecificEmail)) == 1 {
							authSuccess = true
							authViaUpdateSpecific = true
						} else {
							log.Ctx(ctx).WarnContext(ctx, "update email mismatch", slog.String("got", email), slog.String("want", s.updateSpecificEmail))
							email = "" // invalid
						}
					}
				}
			}

			// normal user auth (cookie)
			if !authSuccess {
				// 1. Authenticate User
				authCookie, err := r.Cookie(authTokenCookie)
				if err != nil && !errors.Is(err, http.ErrNoCookie) {
					log.Ctx(ctx).ErrorContext(ctx, "failed to get auth cookie", slog.Any("error", err))
					writeJSONError(w, "missing auth cookie", http.StatusBadRequest)
					return
				}
				if authCookie != nil {
					emailRet, subjectRet, _, err := s.authenticateToken(ctx, authCookie.Value, "")
					if err != nil {
						log.Ctx(ctx).ErrorContext(ctx, "auth token validation failed", slog.Any("error", err))
						writeJSONError(w, "invalid auth token", http.StatusBadRequest)
						return
					}
					email = emailRet
					userID = subjectRet
					authSuccess = true
				} else if !allowNoLogin {
					log.Ctx(ctx).WarnContext(ctx, "no auth cookie found")
					writeJSONError(w, "missing auth cookie", http.StatusBadRequest)
					return
				}
			}

			if authViaUpdateSpecific && isUpdatePath {
				// allowed to update
			} else if authSuccess {
				// fetch user
				if s.singleSite {
					user = types.User{
						ID:    userID,
						Email: email,
						Sites: []types.UserSite{{ID: types.SiteIDNone}},
					}
				} else {
					var err error
					user, err = s.storage.GetUser(ctx, userID)
					if err != nil {
						if ignoreUserNotFound && errors.Is(err, storage.ErrUserNotFound) {
							log.Ctx(ctx).InfoContext(ctx, "user not found, will register on join", slog.String("userID", userID), slog.String("email", email))
							// Put a stub user in context so the join handler can create it
							ctx = context.WithValue(ctx, userToRegisterContextKey, types.User{
								ID:    userID,
								Email: email,
							})
						} else {
							log.Ctx(ctx).WarnContext(ctx, "user lookup failed", slog.String("userID", userID), slog.String("email", email), slog.Any("error", err))
							writeJSONError(w, "user lookup failed", http.StatusForbidden)
							return
						}
					} else {
						userFound = true
						// fill in default siteID if the user only has 1 site
						if siteID == "" && len(user.Sites) == 1 {
							siteID = user.Sites[0].ID
						}
					}
				}

				isAdmin := s.isMultiSiteAdmin(user)
				if isAdmin && s.singleSite {
					user.Admin = true
				}
				if !s.singleSite && siteID != "" && siteID != SiteIDAll && !authViaUpdateSpecific {
					site, err := s.storage.GetSite(ctx, siteID)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "site lookup failed", slog.String("siteID", siteID), slog.Any("error", err))
						writeJSONError(w, "site access denied", http.StatusForbidden)
						return
					}

					permFound := false
					for _, p := range site.Permissions {
						if p.UserID == user.ID {
							permFound = true
							user.Admin = true
							break
						}
					}
					if !permFound && !isAdmin {
						log.Ctx(ctx).WarnContext(ctx, "user does not have permission for site", slog.String("userID", userID), slog.String("email", email), slog.String("site", siteID))
						writeJSONError(w, "site access denied", http.StatusForbidden)
						return
					}
				}
				ctx = context.WithValue(ctx, userContextKey, user)
			} else if !allowNoLogin {
				log.Ctx(ctx).WarnContext(ctx, "unauthenticated request")
				s.clearCookie(w)
				writeJSONError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		if siteID == "" {
			if s.singleSite {
				siteID = types.SiteIDNone
			} else if !allowNoLogin && !isUpdatePath && !ignoreSiteID {
				log.Ctx(ctx).WarnContext(ctx, "siteID required", slog.String("userID", userID))
				writeJSONError(w, "siteID required", http.StatusBadRequest)
				return
			}
		}

		if userID != "" {
			ctx = log.With(ctx, log.Ctx(ctx).With(slog.String("authUserID", userID)))
		}
		if siteID != "" {
			ctx = log.With(ctx, log.Ctx(ctx).With(slog.String("authSiteID", siteID)))
		}

		log.Ctx(ctx).DebugContext(
			ctx,
			"authenticated request",
			slog.String("email", email),
			slog.Bool("userFound", userFound),
		)

		ctx = context.WithValue(ctx, allUserSitesContextKey, user.Sites)
		ctx = context.WithValue(ctx, siteIDContextKey, siteID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Parse Parse Form to get the token, expecting JSON body
	var req struct {
		Token  string `json:"token"`
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// since we failed to read, don't return JSON error
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	email, subject, expires, err := s.authenticateToken(r.Context(), req.Token, req.Client)
	if err != nil {
		log.Ctx(r.Context()).WarnContext(r.Context(), "failed to validate id token", slog.Any("error", err))
		writeJSONError(w, "invalid id token", http.StatusUnauthorized)
		return
	}

	if email == "" {
		log.Ctx(r.Context()).WarnContext(r.Context(), "invalid email in id token")
		writeJSONError(w, "invalid oidc claims", http.StatusUnauthorized)
		return
	}

	log.Ctx(r.Context()).InfoContext(r.Context(), "login token validated successfully", slog.String("email", email), slog.String("subject", subject))

	// Set the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookie,
		Value:    req.Token,
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookie,
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearCookie(w)
	w.WriteHeader(http.StatusOK)
}

type authStatusResponse struct {
	LoggedIn     bool              `json:"loggedIn"`
	Email        string            `json:"email"`
	AuthRequired bool              `json:"authRequired"`
	ClientIDs    map[string]string `json:"clientIDs"`
	Sites        []types.UserSite  `json:"sites"`
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	var loggedIn bool
	user := s.getUser(r)
	if user.ID != "" {
		loggedIn = true
	} else if userToRegister, ok := r.Context().Value(userToRegisterContextKey).(types.User); ok {
		user = userToRegister
		loggedIn = true
	}
	sites := s.getAllUserSites(r)

	err := json.NewEncoder(w).Encode(authStatusResponse{
		LoggedIn:     loggedIn,
		Email:        user.Email,
		AuthRequired: len(s.oidcAudiences) > 0,
		ClientIDs:    s.oidcAudiences,
		Sites:        sites,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) authenticateToken(ctx context.Context, token string, specificClient string) (string, string, time.Time, error) {
	var errs []error

	for providerName, verifier := range s.oidcVerifiers {
		if specificClient != "" && providerName != specificClient {
			continue
		}
		idToken, err := verifier(ctx, token)
		if err == nil {
			var claims struct {
				Email string `json:"email"`
			}
			err = idToken.Claims(&claims)
			if err == nil {
				return claims.Email, idToken.Subject, idToken.Expiry, nil
			}
		}
		errs = append(errs, fmt.Errorf("%s verifier failed: %v", providerName, err))
	}

	if len(errs) > 1 {
		return "", "", time.Time{}, errors.Join(errs...)
	}
	if len(errs) == 1 {
		return "", "", time.Time{}, errs[0]
	}
	return "", "", time.Time{}, errors.New("no valid audiences configured or token invalid")
}
