package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	oauthCodeDuration      = 60 * time.Second
	maxOAuthRedirectURIs   = 5
	maxCIMDRedirectURIs    = 32
	maxRedirectURILength   = 512
	maxCIMDClientIDLength  = 512
	maxCIMDResponseBytes   = 16 * 1024
	cimdFetchTimeout       = 3 * time.Second
	cimdCacheTTL           = 5 * time.Minute
	maxOAuthStateLength    = 512
	maxOAuthTokenBodyBytes = 16 * 1024
	minCodeVerifierLength  = 43
	maxCodeVerifierLength  = 128
	maxCodeChallengeLength = 128
)

// oauthClientData holds the stateless registered client metadata encrypted inside client_id (rrc_...).
// Uses 1-character JSON keys to keep client_id strings compact.
type oauthClientData struct {
	Name         string   `json:"n"`
	RedirectURIs []string `json:"r"`
	CreatedAt    int64    `json:"c"`
}

// oauthCodeData holds the payload encrypted inside a short-lived authorization code (rro_...).
// Uses 1-character JSON keys to keep authorization code strings compact.
type oauthCodeData struct {
	UserID    string `json:"u"`
	SiteID    string `json:"i"`
	AppID     string `json:"a"`
	Secret    string `json:"s"`
	ExpiresAt int64  `json:"e"`
}

type resolvedOAuthClient struct {
	Name         string
	RedirectURIs []string
	ClientDomain string
	Verified     bool
}

type cimdCacheEntry struct {
	client    resolvedOAuthClient
	expiresAt time.Time
}

var defaultCIMDHTTPClient = common.SSRFSafeHTTPClient(cimdFetchTimeout)

func truncateLogString(val string, maxLen int) string {
	if len(val) <= maxLen {
		return val
	}
	return val[:maxLen] + "..."
}

func (s *Server) getCIMDHTTPClient() *http.Client {
	if s.cimdHTTPClient != nil {
		return s.cimdHTTPClient
	}
	return defaultCIMDHTTPClient
}

func validateCIMDClientID(rawClientID string) (*url.URL, error) {
	if rawClientID == "" {
		return nil, errors.New("client_id is required")
	}
	if len(rawClientID) > maxCIMDClientIDLength {
		return nil, fmt.Errorf("client_id must be %d characters or fewer", maxCIMDClientIDLength)
	}
	if strings.TrimSpace(rawClientID) != rawClientID {
		return nil, errors.New("client_id cannot contain leading or trailing whitespace")
	}
	if strings.Contains(rawClientID, `\`) {
		return nil, errors.New("client_id cannot contain backslashes")
	}
	for _, r := range rawClientID {
		if unicode.IsControl(r) {
			return nil, errors.New("client_id cannot contain control characters")
		}
	}
	if !strings.HasPrefix(rawClientID, "https://") {
		return nil, errors.New("client_id metadata URL must use https scheme")
	}

	u, err := url.Parse(rawClientID)
	if err != nil {
		return nil, fmt.Errorf("invalid client_id URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("client_id metadata URL must use https scheme")
	}
	if u.User != nil {
		return nil, errors.New("client_id metadata URL must not contain userinfo")
	}
	if u.Port() != "" || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("client_id metadata URL must not specify a port")
	}
	if u.Fragment != "" || strings.Contains(rawClientID, "#") {
		return nil, errors.New("client_id metadata URL must not contain a fragment")
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return nil, errors.New("client_id metadata URL must include a valid host")
	}
	if _, err := netip.ParseAddr(hostname); err == nil {
		return nil, errors.New("client_id metadata URL host must not be an IP address")
	}
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || !strings.Contains(hostname, ".") {
		return nil, errors.New("client_id metadata URL host must be a public domain")
	}

	if u.Path == "" || u.Path == "/" {
		return nil, errors.New("client_id metadata URL must include a non-root path")
	}
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		if seg == "." || seg == ".." {
			return nil, errors.New("client_id metadata URL must not contain dot path segments")
		}
		unescaped, err := url.PathUnescape(seg)
		if err != nil || unescaped == "." || unescaped == ".." || strings.Contains(unescaped, "/") || strings.Contains(unescaped, `\`) {
			return nil, errors.New("client_id metadata URL must not contain dot path segments")
		}
	}
	return u, nil
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
	if u.Fragment != "" || strings.Contains(trimmed, "#") {
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

func sha256Hex(val string) string {
	sum := sha256.Sum256([]byte(val))
	return hex.EncodeToString(sum[:])
}

func computePendingCodeHash(secret, clientID string) string {
	return sha256Hex(secret + ":" + clientID)
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
		ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
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
		ClientIDMetadataDocumentSupported: true,
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

	ctx := r.Context()
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "oauth register rejected: invalid json", slog.Any("error", err))
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON request body")
		return
	}

	name, err := validateAppName(req.ClientName)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "oauth register rejected: invalid client_name", slog.String("clientName", truncateLogString(req.ClientName, 128)), slog.Any("error", err))
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}

	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxOAuthRedirectURIs {
		log.Ctx(ctx).WarnContext(ctx, "oauth register rejected: invalid redirect_uris count", slog.String("clientName", name), slog.Int("redirectURICount", len(req.RedirectURIs)))
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("between 1 and %d redirect_uris are required", maxOAuthRedirectURIs))
		return
	}

	validatedURIs := make([]string, 0, len(req.RedirectURIs))
	for _, rawURI := range req.RedirectURIs {
		validURI, err := validateRedirectURI(rawURI)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "oauth register rejected: invalid redirect_uri", slog.String("clientName", name), slog.String("redirectURI", truncateLogString(rawURI, 256)), slog.Any("error", err))
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
		validatedURIs = append(validatedURIs, validURI)
	}

	now := s.now().Unix()
	clientID, err := s.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
		Name:         name,
		RedirectURIs: validatedURIs,
		CreatedAt:    now,
	})
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt oauth client_id", slog.Any("error", err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "registered oauth dynamic client", slog.String("clientName", name), slog.Int("redirectURICount", len(validatedURIs)))

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

func cloneResolvedOAuthClient(c resolvedOAuthClient) *resolvedOAuthClient {
	return &resolvedOAuthClient{
		Name:         c.Name,
		RedirectURIs: slices.Clone(c.RedirectURIs),
		ClientDomain: c.ClientDomain,
		Verified:     c.Verified,
	}
}

func (s *Server) getCachedCIMDClient(clientID string, now time.Time) (*resolvedOAuthClient, bool) {
	val, ok := s.cimdCache.Load(clientID)
	if !ok {
		return nil, false
	}
	entry := val.(cimdCacheEntry)
	if now.Before(entry.expiresAt) {
		return cloneResolvedOAuthClient(entry.client), true
	}
	s.cimdCache.Delete(clientID)
	return nil, false
}

func (s *Server) storeCachedCIMDClient(clientID string, resolved resolvedOAuthClient, now time.Time) {
	s.cimdCache.Store(clientID, cimdCacheEntry{
		client: resolvedOAuthClient{
			Name:         resolved.Name,
			RedirectURIs: slices.Clone(resolved.RedirectURIs),
			ClientDomain: resolved.ClientDomain,
			Verified:     resolved.Verified,
		},
		expiresAt: now.Add(cimdCacheTTL),
	})
}

func (s *Server) cleanupExpiredCIMDCache() {
	now := s.now()
	s.cimdCache.Range(func(key, value any) bool {
		entry := value.(cimdCacheEntry)
		if !now.Before(entry.expiresAt) {
			s.cimdCache.Delete(key)
		}
		return true
	})
}

func (s *Server) fetchCIMDClient(ctx context.Context, clientID string) (*resolvedOAuthClient, error) {
	parsedURL, err := validateCIMDClientID(clientID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "oauth cimd client_id rejected", slog.String("clientID", truncateLogString(clientID, 256)), slog.Any("error", err))
		return nil, err
	}

	if cached, ok := s.getCachedCIMDClient(clientID, s.now()); ok {
		return cached, nil
	}

	val, err, _ := s.cimdFetchGroup.Do(clientID, func() (any, error) {
		if cached, ok := s.getCachedCIMDClient(clientID, s.now()); ok {
			return *cached, nil
		}

		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cimdFetchTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, clientID, nil)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "oauth cimd request build failed", slog.String("clientID", clientID), slog.Any("error", err))
			return nil, errors.New("invalid client_id metadata URL")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := s.getCIMDHTTPClient().Do(req)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch oauth client metadata document", slog.String("clientID", clientID), slog.Any("error", err))
			return nil, errors.New("failed to fetch client metadata document")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document returned non-200 status", slog.String("clientID", clientID), slog.Int("statusCode", resp.StatusCode))
			return nil, errors.New("failed to fetch client metadata document")
		}

		rawContentType := resp.Header.Get("Content-Type")
		mediaType, _, mediaErr := mime.ParseMediaType(rawContentType)
		if mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document rejected: invalid content-type", slog.String("clientID", clientID), slog.String("contentType", truncateLogString(rawContentType, 128)))
			return nil, errors.New("client metadata document must have application/json Content-Type")
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxCIMDResponseBytes+1))
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document read failed", slog.String("clientID", clientID), slog.Any("error", err))
			return nil, errors.New("failed to read client metadata document")
		}
		if len(body) > maxCIMDResponseBytes {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document rejected: exceeds maximum size", slog.String("clientID", clientID), slog.Int("maxBytes", maxCIMDResponseBytes))
			return nil, errors.New("client metadata document exceeds maximum size")
		}

		var doc struct {
			ClientID                string   `json:"client_id"`
			ClientName              string   `json:"client_name"`
			RedirectURIs            []string `json:"redirect_uris"`
			GrantTypes              []string `json:"grant_types"`
			ResponseTypes           []string `json:"response_types"`
			TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
			ClientSecret            string   `json:"client_secret"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document rejected: invalid json", slog.String("clientID", clientID), slog.Any("error", err))
			return nil, errors.New("invalid JSON in client metadata document")
		}

		rejectDoc := func(reason string, retErr error) (any, error) {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document rejected", slog.String("clientID", clientID), slog.String("reason", reason))
			return nil, retErr
		}

		if doc.ClientID != clientID {
			return rejectDoc("client_id mismatch", errors.New("client_id in metadata document does not match URL"))
		}
		if doc.ClientSecret != "" {
			return rejectDoc("client_secret present", errors.New("client_secret is not allowed in client metadata document"))
		}
		if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
			return rejectDoc("unsupported token_endpoint_auth_method", errors.New("token_endpoint_auth_method must be none"))
		}
		if len(doc.GrantTypes) > 0 && !slices.Contains(doc.GrantTypes, "authorization_code") {
			return rejectDoc("missing authorization_code grant_type", errors.New("grant_types must include authorization_code"))
		}
		if len(doc.ResponseTypes) > 0 && !slices.Contains(doc.ResponseTypes, "code") {
			return rejectDoc("missing code response_type", errors.New("response_types must include code"))
		}
		if len(doc.RedirectURIs) == 0 || len(doc.RedirectURIs) > maxCIMDRedirectURIs {
			return rejectDoc("invalid redirect_uris count", fmt.Errorf("between 1 and %d redirect_uris are required in client metadata document", maxCIMDRedirectURIs))
		}

		validatedURIs := make([]string, 0, len(doc.RedirectURIs))
		for _, rawURI := range doc.RedirectURIs {
			if validURI, uriErr := validateRedirectURI(rawURI); uriErr == nil {
				validatedURIs = append(validatedURIs, validURI)
			} else {
				log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document skipped invalid redirect_uri", slog.String("clientID", clientID), slog.String("redirectURI", truncateLogString(rawURI, 256)), slog.Any("error", uriErr))
			}
		}
		if len(validatedURIs) == 0 {
			return rejectDoc("no valid redirect_uris", errors.New("client metadata document contains no valid redirect_uris"))
		}

		domain := strings.ToLower(parsedURL.Hostname())
		rawName := strings.TrimSpace(doc.ClientName)
		if rawName == "" {
			rawName = domain
		}
		name, err := validateAppName(rawName)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "oauth client metadata document rejected: invalid client_name", slog.String("clientID", clientID), slog.String("clientName", truncateLogString(rawName, 128)), slog.Any("error", err))
			return nil, fmt.Errorf("invalid client_name in metadata document: %w", err)
		}

		resolved := resolvedOAuthClient{
			Name:         name,
			RedirectURIs: validatedURIs,
			ClientDomain: domain,
			Verified:     true,
		}
		s.storeCachedCIMDClient(clientID, resolved, s.now())
		log.Ctx(ctx).InfoContext(ctx, "fetched oauth client metadata document", slog.String("clientID", clientID), slog.String("clientName", name), slog.String("clientDomain", domain), slog.Int("redirectURICount", len(validatedURIs)))
		return resolved, nil
	})
	if err != nil {
		return nil, err
	}
	resolved := val.(resolvedOAuthClient)
	return cloneResolvedOAuthClient(resolved), nil
}

func (s *Server) resolveOAuthClient(ctx context.Context, clientID string) (*resolvedOAuthClient, error) {
	if strings.HasPrefix(clientID, oauthClientIDPrefix) {
		var clientData oauthClientData
		if err := s.decryptAppPayload(oauthClientIDPrefix, clientID, &clientData); err != nil {
			return nil, errors.New("invalid client_id")
		}
		if len(clientData.RedirectURIs) == 0 || clientData.Name == "" {
			return nil, errors.New("invalid client_id metadata")
		}
		return &resolvedOAuthClient{
			Name:         clientData.Name,
			RedirectURIs: clientData.RedirectURIs,
			Verified:     false,
		}, nil
	}
	if strings.HasPrefix(clientID, "https://") {
		return s.fetchCIMDClient(ctx, clientID)
	}
	return nil, errors.New("invalid client_id")
}

func normalizedOrigin(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

func (s *Server) validateOAuthAuthorizeParams(ctx context.Context, clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod string) (*resolvedOAuthClient, error) {
	if clientID == "" {
		return nil, errors.New("client_id is required")
	}
	if redirectURI == "" {
		return nil, errors.New("redirect_uri is required")
	}
	if _, err := validateRedirectURI(redirectURI); err != nil {
		return nil, err
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

	client, err := s.resolveOAuthClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(client.RedirectURIs, redirectURI) {
		return nil, errors.New("redirect_uri does not match registered client")
	}
	if client.Verified && client.ClientDomain != "" {
		expectedOrigin := "https://" + client.ClientDomain
		if normalizedOrigin(redirectURI) != expectedOrigin {
			log.Ctx(ctx).WarnContext(
				ctx,
				"oauth cimd redirect_uri origin differs from client domain",
				slog.String("clientID", clientID),
				slog.String("clientDomain", client.ClientDomain),
				slog.String("redirectURI", redirectURI),
			)
		}
	}
	return client, nil
}

func (s *Server) handleOAuthClientInfo(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	ctx := r.Context()
	user := s.getUser(r)
	if !s.bypassAuth && (user.ID == "" || (!s.singleSite && len(user.Sites) == 0)) {
		log.Ctx(ctx).WarnContext(ctx, "oauth client info rejected: user lacks active site", slog.String("userID", user.ID))
		writeJSONError(w, "a RateRudder account with an active site is required to authorize apps", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	client, err := s.validateOAuthAuthorizeParams(ctx, clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod)
	if err != nil {
		log.Ctx(ctx).WarnContext(
			ctx,
			"oauth client info rejected",
			slog.String("userID", user.ID),
			slog.String("clientID", truncateLogString(clientID, 256)),
			slog.String("redirectURI", truncateLogString(redirectURI, 256)),
			slog.String("responseType", truncateLogString(responseType, 64)),
			slog.String("codeChallengeMethod", truncateLogString(codeChallengeMethod, 64)),
			slog.Any("error", err),
		)
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err = json.NewEncoder(w).Encode(struct {
		ClientName   string `json:"clientName"`
		RedirectURI  string `json:"redirectURI"`
		ClientDomain string `json:"clientDomain,omitempty"`
		Verified     bool   `json:"verified"`
	}{
		ClientName:   client.Name,
		RedirectURI:  redirectURI,
		ClientDomain: client.ClientDomain,
		Verified:     client.Verified,
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
	if !s.hasDirectSitePermission(r) {
		log.Ctx(ctx).WarnContext(ctx, "oauth approve rejected: user lacks direct site permission", slog.String("userID", user.ID), slog.String("siteID", siteID))
		writeJSONError(w, "site access denied", http.StatusForbidden)
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
		log.Ctx(ctx).WarnContext(ctx, "oauth approve rejected: invalid request body", slog.String("userID", user.ID), slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.State) > maxOAuthStateLength {
		log.Ctx(ctx).WarnContext(ctx, "oauth approve rejected: state too long", slog.String("userID", user.ID), slog.Int("stateLen", len(req.State)))
		writeJSONError(w, fmt.Sprintf("state must be %d characters or fewer", maxOAuthStateLength), http.StatusBadRequest)
		return
	}
	for _, r := range req.State {
		if unicode.IsControl(r) {
			log.Ctx(ctx).WarnContext(ctx, "oauth approve rejected: state contains control character", slog.String("userID", user.ID))
			writeJSONError(w, "state cannot contain control characters", http.StatusBadRequest)
			return
		}
	}

	responseType := req.ResponseType
	if responseType == "" {
		responseType = "code"
	}
	client, err := s.validateOAuthAuthorizeParams(ctx, req.ClientID, req.RedirectURI, responseType, req.CodeChallenge, req.CodeChallengeMethod)
	if err != nil {
		log.Ctx(ctx).WarnContext(
			ctx,
			"oauth approve rejected",
			slog.String("userID", user.ID),
			slog.String("siteID", siteID),
			slog.String("clientID", truncateLogString(req.ClientID, 256)),
			slog.String("redirectURI", truncateLogString(req.RedirectURI, 256)),
			slog.Any("error", err),
		)
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, remainingCount := s.cleanExpiredPendingApps(ctx, user, siteID)
	if remainingCount >= maxUserApps {
		log.Ctx(ctx).WarnContext(ctx, "oauth approve rejected: maximum number of apps reached", slog.String("userID", user.ID), slog.String("siteID", siteID))
		writeJSONError(w, "maximum number of apps reached", http.StatusBadRequest)
		return
	}

	appID, err := generateAppID()
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate oauth app ID", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rawSecret, err := generateAppSecret()
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate oauth code secret", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := s.now().UTC()
	expiresAt := now.Add(oauthCodeDuration)
	pendingApp := types.UserApp{
		ID:               appID,
		Name:             client.Name,
		ClientDomain:     client.ClientDomain,
		Type:             types.AppTypeOAuth,
		SiteID:           siteID,
		TSCreated:        now,
		PendingCodeHash:  computePendingCodeHash(rawSecret, req.ClientID),
		PendingExpiresAt: expiresAt,
		CodeChallenge:    req.CodeChallenge,
		RedirectURI:      req.RedirectURI,
	}

	code, err := s.encryptAppPayload(oauthCodePrefix, oauthCodeData{
		UserID:    user.ID,
		SiteID:    siteID,
		AppID:     appID,
		Secret:    rawSecret,
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

	log.Ctx(ctx).InfoContext(ctx, "approved oauth authorization code", slog.String("appID", appID), slog.String("clientName", client.Name), slog.Bool("verified", client.Verified))

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
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "application/json") {
		var req struct {
			GrantType    string `json:"grant_type"`
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
			ClientID     string `json:"client_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxOAuthTokenBodyBytes)).Decode(&req); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		grantType = req.GrantType
		code = req.Code
		codeVerifier = req.CodeVerifier
		redirectURI = req.RedirectURI
		clientID = req.ClientID
	} else {
		if contentType != "" && !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unsupported Content-Type")
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxOAuthTokenBodyBytes+1))
		if err != nil || len(bodyBytes) > maxOAuthTokenBodyBytes {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "failed to read form body")
			return
		}
		vals, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form body")
			return
		}
		for _, param := range []string{"grant_type", "code", "code_verifier", "redirect_uri", "client_id"} {
			if len(vals[param]) > 1 {
				writeOAuthError(w, http.StatusBadRequest, "invalid_request", "duplicate OAuth parameter")
				return
			}
		}
		grantType = vals.Get("grant_type")
		code = vals.Get("code")
		codeVerifier = vals.Get("code_verifier")
		redirectURI = vals.Get("redirect_uri")
		clientID = vals.Get("client_id")
	}

	// Decrypt the authorization code early so that if any validation or token exchange
	// step fails while the code is still within its validity window, we can remove an
	// unredeemed pending app from the user's apps list.
	var codeData oauthCodeData
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
	// cleanupEarly removes an unredeemed pending app when an early parameter validation
	// error occurs, so a failed exchange doesn't leave a dead pending entry behind. We
	// must fetch the user and verify PendingCodeHash != "" first so that sending a
	// malformed request (e.g. grant_type=invalid) with an already-redeemed code within
	// its 60-second lifetime cannot delete an active connected app without going through
	// the full replay check below.
	cleanupEarly := func() {
		if !hasValidCodePayload || !codeNotExpired {
			return
		}
		u, getErr := s.storage.GetUser(ctx, codeData.UserID)
		if getErr != nil {
			return
		}
		for i := range u.Apps {
			if u.Apps[i].ID == codeData.AppID && u.Apps[i].SiteID == codeData.SiteID {
				if u.Apps[i].PendingCodeHash != "" {
					removeApp()
				}
				return
			}
		}
	}

	if grantType != "authorization_code" {
		cleanupEarly()
		log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: unsupported grant_type", slog.String("grantType", truncateLogString(grantType, 64)), slog.String("clientID", truncateLogString(clientID, 256)))
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	if code == "" || codeVerifier == "" || redirectURI == "" || clientID == "" {
		cleanupEarly()
		log.Ctx(ctx).WarnContext(
			ctx,
			"oauth token rejected: missing required parameters",
			slog.String("clientID", truncateLogString(clientID, 256)),
			slog.Bool("hasCode", code != ""),
			slog.Bool("hasCodeVerifier", codeVerifier != ""),
			slog.Bool("hasRedirectURI", redirectURI != ""),
		)
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, code, code_verifier, and redirect_uri are required")
		return
	}
	if !hasValidCodePayload {
		log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: invalid authorization code payload", slog.String("clientID", truncateLogString(clientID, 256)))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid authorization code")
		return
	}
	if !isValidCodeVerifier(codeVerifier) {
		cleanupEarly()
		log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: invalid code_verifier", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.String("clientID", truncateLogString(clientID, 256)))
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid code_verifier")
		return
	}

	// Validate client_id and redirect_uri locally without any outbound HTTP fetch:
	// - DCR (rrc_...): decrypt stateless client payload and verify redirect_uri membership.
	// - CIMD (https://...): validate CIMD URL syntax and redirect_uri syntax; cryptographic
	//   binding to the exact approved client_id and redirect_uri is enforced via PendingCodeHash
	//   and UserApp.RedirectURI below.
	var expectedClientName string
	hasExpectedClientName := false
	switch {
	case strings.HasPrefix(clientID, oauthClientIDPrefix):
		var clientData oauthClientData
		if err := s.decryptAppPayload(oauthClientIDPrefix, clientID, &clientData); err != nil {
			cleanupEarly()
			log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: failed to decrypt dcr client_id", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID))
			writeOAuthError(w, http.StatusBadRequest, "invalid_client", "invalid client_id")
			return
		}
		if !slices.Contains(clientData.RedirectURIs, redirectURI) {
			cleanupEarly()
			log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: redirect_uri does not match dcr client_id", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.String("redirectURI", truncateLogString(redirectURI, 256)))
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match client_id")
			return
		}
		expectedClientName = clientData.Name
		hasExpectedClientName = true
	case strings.HasPrefix(clientID, "https://"):
		if _, err := validateCIMDClientID(clientID); err != nil {
			cleanupEarly()
			log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: invalid cimd client_id", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.String("clientID", truncateLogString(clientID, 256)), slog.Any("error", err))
			writeOAuthError(w, http.StatusBadRequest, "invalid_client", "invalid client_id")
			return
		}
		if _, err := validateRedirectURI(redirectURI); err != nil {
			cleanupEarly()
			log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: invalid redirect_uri", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.String("clientID", clientID), slog.String("redirectURI", truncateLogString(redirectURI, 256)), slog.Any("error", err))
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid redirect_uri")
			return
		}
	default:
		cleanupEarly()
		log.Ctx(ctx).WarnContext(ctx, "oauth token rejected: unrecognized client_id format", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID), slog.String("clientID", truncateLogString(clientID, 256)))
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "invalid client_id")
		return
	}

	codeHash := computePendingCodeHash(codeData.Secret, clientID)
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
	nameMatch := !hasExpectedClientName || target.Name == expectedClientName
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
	if !codeNotExpired || target.PendingExpiresAt.IsZero() || !now.Before(target.PendingExpiresAt) ||
		!nameMatch ||
		(codeMatch&challengeMatch&redirectMatch) != 1 {
		if codeNotExpired {
			removeApp()
		}
		log.Ctx(ctx).WarnContext(ctx, "oauth code validation failed", slog.String("userID", codeData.UserID), slog.String("appID", codeData.AppID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	accessToken, tokenHash, err := s.createAppToken(codeData.UserID, target.SiteID, target.ID)
	if err != nil {
		removeApp()
		log.Ctx(ctx).ErrorContext(ctx, "failed to create oauth access token", slog.String("userID", codeData.UserID), slog.Any("error", err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}

	redeemedApp := *target
	redeemedApp.TokenHash = tokenHash
	redeemedApp.PendingCodeHash = ""
	redeemedApp.PendingExpiresAt = time.Time{}
	redeemedApp.CodeChallenge = ""
	redeemedApp.RedirectURI = ""

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
