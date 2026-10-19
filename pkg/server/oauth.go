package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	oauthCodeDuration      = 60 * time.Second
	maxOAuthRedirectURIs   = 5
	maxRedirectURILength   = 512
	maxOAuthStateLength    = 512
	minCodeVerifierLength  = 43
	maxCodeVerifierLength  = 128
	maxCodeChallengeLength = 128
)

// OAuthClientData holds the stateless registered client metadata encrypted inside client_id (rrc_...).
type OAuthClientData struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirectURIs"`
	CreatedAt    int64    `json:"createdAt"`
}

// OAuthCodeData holds the payload encrypted inside a short-lived authorization code (rro_...).
type OAuthCodeData struct {
	UserID    string `json:"userID"`
	SiteID    string `json:"siteID"`
	AppID     string `json:"appID"`
	Secret    string `json:"secret"`
	ClientID  string `json:"clientID"`
	ExpiresAt int64  `json:"expiresAt"`
}

func requestBaseURL(r *http.Request) string {
	host := r.Host
	if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n\"'") {
		host = "localhost"
	}
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
		scheme = proto
	} else if r.TLS == nil && (strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") || strings.HasPrefix(host, "[::1]")) {
		scheme = "http"
	}
	return scheme + "://" + host
}

func validateRedirectURI(rawURI string) (string, error) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return "", errors.New("redirect_uri cannot be empty")
	}
	if len(trimmed) > maxRedirectURILength {
		return "", fmt.Errorf("redirect_uri must be %d characters or fewer", maxRedirectURILength)
	}
	if strings.Contains(trimmed, "*") || strings.Contains(trimmed, `\`) {
		return "", errors.New("redirect_uri cannot contain wildcards or backslashes")
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", errors.New("redirect_uri cannot contain control characters")
		}
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid redirect_uri: %w", err)
	}
	if u.Fragment != "" {
		return "", errors.New("redirect_uri must not contain a fragment")
	}
	if u.User != nil {
		return "", errors.New("redirect_uri must not contain userinfo")
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", errors.New("redirect_uri must include a valid host")
	}

	switch u.Scheme {
	case "https":
		return trimmed, nil
	case "http":
		if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
			return trimmed, nil
		}
		return "", errors.New("http redirect_uri is only allowed for localhost/loopback")
	default:
		return "", errors.New("redirect_uri scheme must be https (or http for localhost)")
	}
}

func isValidCodeVerifier(verifier string) bool {
	if len(verifier) < minCodeVerifierLength || len(verifier) > maxCodeVerifierLength {
		return false
	}
	for i := 0; i < len(verifier); i++ {
		c := verifier[i]
		isAlphaNum := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' && c != '.' && c != '_' && c != '~' {
			return false
		}
	}
	return true
}

func generateOAuthSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate random oauth secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func sha256Hex(val string) string {
	sum := sha256.Sum256([]byte(val))
	return hex.EncodeToString(sum[:])
}

func computePKCES256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeOAuthError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description,omitempty"`
	}{
		Error:            errCode,
		ErrorDescription: description,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleOAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	baseURL := requestBaseURL(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err := json.NewEncoder(w).Encode(struct {
		Issuer                            string   `json:"issuer"`
		AuthorizationEndpoint             string   `json:"authorization_endpoint"`
		TokenEndpoint                     string   `json:"token_endpoint"`
		RegistrationEndpoint              string   `json:"registration_endpoint"`
		ResponseTypesSupported            []string `json:"response_types_supported"`
		GrantTypesSupported               []string `json:"grant_types_supported"`
		TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
		CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	}{
		Issuer:                 baseURL,
		AuthorizationEndpoint:  baseURL + "/oauth/authorize",
		TokenEndpoint:          baseURL + "/oauth/token",
		RegistrationEndpoint:   baseURL + "/oauth/register",
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code"},
		// Public OAuth 2.1 / MCP clients authenticate token exchanges via PKCE (S256)
		// rather than a client_secret, keeping client registration stateless.
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	baseURL := requestBaseURL(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err := json.NewEncoder(w).Encode(struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}{
		Resource:             baseURL + "/api/mcp",
		AuthorizationServers: []string{baseURL},
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON request body")
		return
	}

	name, err := validateAppName(req.ClientName)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}

	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxOAuthRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("between 1 and %d redirect_uris are required", maxOAuthRedirectURIs))
		return
	}

	validatedURIs := make([]string, 0, len(req.RedirectURIs))
	for _, rawURI := range req.RedirectURIs {
		validURI, err := validateRedirectURI(rawURI)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
		validatedURIs = append(validatedURIs, validURI)
	}

	now := s.now().Unix()
	clientID, err := s.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
		Name:         name,
		RedirectURIs: validatedURIs,
		CreatedAt:    now,
	})
	if err != nil {
		log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to encrypt oauth client_id", slog.Any("error", err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(struct {
		ClientID                string   `json:"client_id"`
		ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}{
		ClientID:         clientID,
		ClientIDIssuedAt: now,
		ClientName:       name,
		RedirectURIs:     validatedURIs,
		GrantTypes:       []string{"authorization_code"},
		ResponseTypes:    []string{"code"},
		// We use "none" because registered clients are public OAuth 2.1 clients
		// that use PKCE (S256) instead of a client_secret, so we don't need to
		// issue or store client secrets.
		TokenEndpointAuthMethod: "none",
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) validateOAuthAuthorizeParams(clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod string) (*OAuthClientData, error) {
	if clientID == "" {
		return nil, errors.New("client_id is required")
	}
	var clientData OAuthClientData
	if err := s.decryptAppPayload(oauthClientIDPrefix, clientID, &clientData); err != nil {
		return nil, errors.New("invalid client_id")
	}
	if len(clientData.RedirectURIs) == 0 || clientData.Name == "" {
		return nil, errors.New("invalid client_id metadata")
	}
	if redirectURI == "" {
		return nil, errors.New("redirect_uri is required")
	}
	if _, err := validateRedirectURI(redirectURI); err != nil {
		return nil, err
	}
	if !slices.Contains(clientData.RedirectURIs, redirectURI) {
		return nil, errors.New("redirect_uri does not match registered client")
	}
	if responseType != "code" {
		return nil, errors.New("response_type must be code")
	}
	if codeChallenge == "" || len(codeChallenge) > maxCodeChallengeLength {
		return nil, errors.New("valid code_challenge is required")
	}
	decodedChallenge, err := base64.RawURLEncoding.DecodeString(codeChallenge)
	if err != nil || len(decodedChallenge) != sha256.Size {
		return nil, errors.New("valid code_challenge is required")
	}
	if codeChallengeMethod != "S256" {
		return nil, errors.New("code_challenge_method must be S256")
	}
	return &clientData, nil
}

func (s *Server) handleOAuthClientInfo(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	clientData, err := s.validateOAuthAuthorizeParams(clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err = json.NewEncoder(w).Encode(struct {
		ClientName  string `json:"clientName"`
		RedirectURI string `json:"redirectURI"`
	}{
		ClientName:  clientData.Name,
		RedirectURI: redirectURI,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleOAuthApprove(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	ctx := r.Context()
	user := s.getUser(r)
	siteID := s.getSiteID(r)
	if user.ID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ClientID            string `json:"clientID"`
		RedirectURI         string `json:"redirectURI"`
		ResponseType        string `json:"responseType"`
		CodeChallenge       string `json:"codeChallenge"`
		CodeChallengeMethod string `json:"codeChallengeMethod"`
		State               string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.State) > maxOAuthStateLength {
		writeJSONError(w, fmt.Sprintf("state must be %d characters or fewer", maxOAuthStateLength), http.StatusBadRequest)
		return
	}
	for _, r := range req.State {
		if unicode.IsControl(r) {
			writeJSONError(w, "state cannot contain control characters", http.StatusBadRequest)
			return
		}
	}

	responseType := req.ResponseType
	if responseType == "" {
		responseType = "code"
	}
	clientData, err := s.validateOAuthAuthorizeParams(req.ClientID, req.RedirectURI, responseType, req.CodeChallenge, req.CodeChallengeMethod)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, remainingCount := s.cleanExpiredPendingApps(ctx, user, siteID)
	if remainingCount >= maxUserApps {
		writeJSONError(w, "maximum number of apps reached", http.StatusBadRequest)
		return
	}

	appID, err := generateAppID()
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate oauth app ID", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rawSecret, err := generateOAuthSecret()
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate oauth code secret", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := s.now().UTC()
	expiresAt := now.Add(oauthCodeDuration)
	pendingApp := types.UserApp{
		ID:               appID,
		Name:             clientData.Name,
		Type:             types.AppTypeOAuth,
		SiteID:           siteID,
		TSCreated:        now,
		PendingCodeHash:  sha256Hex(rawSecret),
		PendingExpiresAt: expiresAt,
		CodeChallenge:    req.CodeChallenge,
		RedirectURI:      req.RedirectURI,
	}

	code, err := s.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
		UserID:    user.ID,
		SiteID:    siteID,
		AppID:     appID,
		Secret:    rawSecret,
		ClientID:  req.ClientID,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt oauth code", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	targetURL, err := url.Parse(req.RedirectURI)
	if err != nil {
		writeJSONError(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := targetURL.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	targetURL.RawQuery = q.Encode()

	if err := s.storage.AddUserApp(ctx, user.ID, pendingApp); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to store pending oauth app", slog.Any("error", err))
		writeJSONError(w, "failed to authorize app", http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "approved oauth authorization code", slog.String("appID", appID), slog.String("clientName", clientData.Name))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err = json.NewEncoder(w).Encode(struct {
		RedirectURL string `json:"redirectURL"`
	}{
		RedirectURL: targetURL.String(),
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeOAuthError(w, http.StatusNotImplemented, "server_error", "apps not enabled")
		return
	}

	ctx := r.Context()
	now := s.now().UTC()
	var grantType, code, codeVerifier, redirectURI, clientID string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var req struct {
			GrantType    string `json:"grant_type"`
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
			ClientID     string `json:"client_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		grantType = req.GrantType
		code = req.Code
		codeVerifier = req.CodeVerifier
		redirectURI = req.RedirectURI
		clientID = req.ClientID
	} else {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form body")
			return
		}
		grantType = r.FormValue("grant_type")
		code = r.FormValue("code")
		codeVerifier = r.FormValue("code_verifier")
		redirectURI = r.FormValue("redirect_uri")
		clientID = r.FormValue("client_id")
	}

	// Decrypt the authorization code early so that if any validation or token exchange
	// step fails while the code is still within its validity window, we can remove the
	// pending app from the user's apps list.
	var codeData OAuthCodeData
	hasValidCodePayload := false
	codeNotExpired := false
	if code != "" {
		if err := s.decryptAppPayload(oauthCodePrefix, code, &codeData); err == nil &&
			codeData.UserID != "" && codeData.SiteID != "" && codeData.AppID != "" && codeData.Secret != "" {
			hasValidCodePayload = true
			codeNotExpired = codeData.ExpiresAt > 0 && now.Unix() < codeData.ExpiresAt
		}
	}
	removeApp := func() {
		if !hasValidCodePayload {
			return
		}
		if rmErr := s.storage.RemoveUserApp(ctx, codeData.UserID, codeData.SiteID, codeData.AppID); rmErr != nil && !errors.Is(rmErr, storage.ErrAppNotFound) {
			log.Ctx(ctx).ErrorContext(ctx, "failed to clean up oauth app after token error", slog.String("userID", codeData.UserID), slog.String("siteID", codeData.SiteID), slog.String("appID", codeData.AppID), slog.Any("error", rmErr))
		}
	}
	cleanupEarly := func() {
		if codeNotExpired {
			removeApp()
		}
	}

	if grantType != "authorization_code" {
		cleanupEarly()
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	if code == "" || codeVerifier == "" || redirectURI == "" || clientID == "" {
		cleanupEarly()
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, code, code_verifier, and redirect_uri are required")
		return
	}
	if !hasValidCodePayload {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid authorization code")
		return
	}
	if !isValidCodeVerifier(codeVerifier) {
		cleanupEarly()
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid code_verifier")
		return
	}

	var clientData OAuthClientData
	if err := s.decryptAppPayload(oauthClientIDPrefix, clientID, &clientData); err != nil {
		cleanupEarly()
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "invalid client_id")
		return
	}
	if !slices.Contains(clientData.RedirectURIs, redirectURI) {
		cleanupEarly()
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match client_id")
		return
	}

	codeHash := sha256Hex(codeData.Secret)
	computedChallenge := computePKCES256Challenge(codeVerifier)

	user, err := s.storage.GetUser(ctx, codeData.UserID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "oauth code redemption failed to get user", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.Any("error", err))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	var target *types.UserApp
	for i := range user.Apps {
		if user.Apps[i].ID == codeData.AppID && user.Apps[i].SiteID == codeData.SiteID {
			target = &user.Apps[i]
			break
		}
	}
	if target == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	// If PendingCodeHash is already empty, this authorization code was replayed.
	// Only revoke the active app if the replay occurred within the code's validity
	// window so an expired historical code from browser history cannot be used as an
	// unauthenticated kill-switch against a long-running active app.
	if target.PendingCodeHash == "" {
		if codeNotExpired {
			removeApp()
			log.Ctx(ctx).WarnContext(ctx, "revoked oauth app due to authorization code replay", slog.String("userID", codeData.UserID), slog.String("appID", target.ID))
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	codeMatch := subtle.ConstantTimeCompare([]byte(target.PendingCodeHash), []byte(codeHash))
	challengeMatch := subtle.ConstantTimeCompare([]byte(target.CodeChallenge), []byte(computedChallenge))
	redirectMatch := subtle.ConstantTimeCompare([]byte(target.RedirectURI), []byte(redirectURI))
	clientMatch := subtle.ConstantTimeCompare([]byte(codeData.ClientID), []byte(clientID))
	if !codeNotExpired || target.PendingExpiresAt.IsZero() || !now.Before(target.PendingExpiresAt) ||
		target.Name != clientData.Name ||
		(codeMatch&challengeMatch&redirectMatch&clientMatch) != 1 {
		removeApp()
		log.Ctx(ctx).WarnContext(ctx, "oauth code validation failed", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	redeemedApp := *target
	redeemedApp.PendingCodeHash = ""
	redeemedApp.PendingExpiresAt = time.Time{}
	redeemedApp.CodeChallenge = ""
	redeemedApp.RedirectURI = ""

	accessToken, err := s.createAppToken(codeData.UserID, redeemedApp.SiteID, redeemedApp.ID, types.AppTypeOAuth)
	if err != nil {
		removeApp()
		log.Ctx(ctx).ErrorContext(ctx, "failed to create oauth access token", slog.String("userID", codeData.UserID), slog.Any("error", err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}

	expectedCodeHash := target.PendingCodeHash
	err = s.storage.ReplaceUserApp(ctx, codeData.UserID, redeemedApp, storage.ReplaceUserAppOptions{
		ExpectedPendingCodeHash: &expectedCodeHash,
	})
	if err != nil {
		// Remove the app whether ReplaceUserApp failed due to a concurrent replay (ErrAppConflict)
		// or a storage error so a broken/unredeemed app is not left behind on the user.
		removeApp()
		log.Ctx(ctx).WarnContext(ctx, "oauth code redemption failed", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.Any("error", err))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "redeemed oauth authorization code", slog.String("userID", codeData.UserID), slog.String("siteID", redeemedApp.SiteID), slog.String("appID", redeemedApp.ID))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err = json.NewEncoder(w).Encode(struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}
