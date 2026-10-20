package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	appTokenPrefix      = "rr_"
	oauthClientIDPrefix = "rrc_"
	oauthCodePrefix     = "rro_"

	maxUserApps      = 25
	maxAppNameLength = 64
)

// appTokenData holds the decrypted payload stored inside an encrypted app token (rr_...).
// Uses 1-character JSON keys to keep token strings compact.
type appTokenData struct {
	UserID string `json:"u"`
	SiteID string `json:"i"`
	AppID  string `json:"a"`
	Secret string `json:"s"`
}

func generateAppID() (string, error) {
	b := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate random app id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func generateAppSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate random app secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) encryptAppPayload(prefix string, payload any) (string, error) {
	if len(s.appsEncryptionKey) != 32 {
		return "", errors.New("apps encryption key not configured (must be 32 bytes)")
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal app payload: %w", err)
	}

	aead, err := chacha20poly1305.NewX([]byte(s.appsEncryptionKey))
	if err != nil {
		return "", fmt.Errorf("failed to create app cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate app nonce: %w", err)
	}

	// Bind prefix as AEAD additional data so tokens with different prefixes
	// (rr_, rrc_, rro_) are cryptographically domain-separated.
	ciphertext := aead.Seal(nonce, nonce, jsonBytes, []byte(prefix))
	return prefix + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (s *Server) decryptAppPayload(prefix, tokenStr string, out any) error {
	if len(s.appsEncryptionKey) != 32 {
		return errors.New("apps encryption key not configured (must be 32 bytes)")
	}
	if !strings.HasPrefix(tokenStr, prefix) {
		return errors.New("invalid token prefix")
	}

	encoded := strings.TrimPrefix(tokenStr, prefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("failed to decode token: %w", err)
	}

	aead, err := chacha20poly1305.NewX([]byte(s.appsEncryptionKey))
	if err != nil {
		return fmt.Errorf("failed to create app cipher: %w", err)
	}

	if len(raw) < aead.NonceSize() {
		return errors.New("malformed token: too short")
	}

	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(prefix))
	if err != nil {
		return fmt.Errorf("failed to decrypt token: %w", err)
	}

	if err := json.Unmarshal(plaintext, out); err != nil {
		return fmt.Errorf("failed to unmarshal token payload: %w", err)
	}
	return nil
}

func (s *Server) createAppToken(userID, siteID, appID string) (string, string, error) {
	if userID == "" || siteID == "" || appID == "" {
		return "", "", errors.New("userID, siteID, and appID are required")
	}
	secret, err := generateAppSecret()
	if err != nil {
		return "", "", err
	}
	token, err := s.encryptAppPayload(appTokenPrefix, appTokenData{
		UserID: userID,
		SiteID: siteID,
		AppID:  appID,
		Secret: secret,
	})
	if err != nil {
		return "", "", err
	}
	return token, sha256Hex(secret), nil
}

func (s *Server) verifyAppToken(tokenStr string) (*appTokenData, error) {
	var data appTokenData
	if err := s.decryptAppPayload(appTokenPrefix, tokenStr, &data); err != nil {
		return nil, err
	}
	if data.UserID == "" || data.SiteID == "" || data.AppID == "" || data.Secret == "" {
		return nil, errors.New("incomplete app token payload")
	}
	return &data, nil
}

func validateAppName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len([]rune(name)) > maxAppNameLength {
		return "", fmt.Errorf("name must be %d characters or fewer", maxAppNameLength)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain control characters")
		}
	}
	return name, nil
}

// cleanExpiredPendingApps opportunistically removes up to 3 expired pending OAuth apps
// over time so abandoned OAuth flows get cleaned up without needing a bulk-delete
// storage method or risking an unbounded number of DB calls in a single request.
// It returns the active apps for siteID and the total remaining (non-cleaned) app count.
func (s *Server) cleanExpiredPendingApps(ctx context.Context, user types.User, siteID string) ([]types.PublicUserApp, int) {
	const maxExpiredCleanup = 3
	now := s.now().UTC()
	cleaned := 0
	remainingTotal := 0
	activeApps := make([]types.PublicUserApp, 0)
	for _, app := range user.Apps {
		if app.PendingCodeHash != "" {
			if cleaned < maxExpiredCleanup && app.SiteID != "" && (app.PendingExpiresAt.IsZero() || !now.Before(app.PendingExpiresAt)) {
				cleaned++
				err := s.storage.RemoveUserApp(ctx, user.ID, app.SiteID, app.ID)
				if err != nil {
					if !errors.Is(err, storage.ErrAppNotFound) {
						log.Ctx(ctx).WarnContext(ctx, "failed to remove expired pending user app", slog.String("appID", app.ID), slog.Any("error", err))
						remainingTotal++
					}
				} else {
					log.Ctx(ctx).DebugContext(ctx, "removed stale pending app", slog.String("appID", app.ID))
				}
				continue
			}
			remainingTotal++
			continue
		}
		remainingTotal++
		if app.SiteID == siteID {
			activeApps = append(activeApps, app.Public())
		}
	}
	return activeApps, remainingTotal
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	if len(s.appsEncryptionKey) != 32 {
		writeJSONError(w, "apps not enabled", http.StatusNotImplemented)
		return
	}

	ctx := r.Context()
	user := s.getUser(r)
	siteID := s.getSiteID(r)

	activeApps, _ := s.cleanExpiredPendingApps(ctx, user, siteID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err := json.NewEncoder(w).Encode(struct {
		Apps []types.PublicUserApp `json:"apps"`
	}{
		Apps: activeApps,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleNewApp(w http.ResponseWriter, r *http.Request) {
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
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	name, err := validateAppName(req.Name)
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
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate app ID", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	token, tokenHash, err := s.createAppToken(user.ID, siteID, appID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to create app token", slog.Any("error", err))
		writeJSONError(w, "failed to create app token", http.StatusInternalServerError)
		return
	}

	newApp := types.UserApp{
		ID:        appID,
		Name:      name,
		Type:      types.AppTypeAPIKey,
		SiteID:    siteID,
		TSCreated: s.now().UTC(),
		TokenHash: tokenHash,
	}

	if err := s.storage.AddUserApp(ctx, user.ID, newApp); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save new user app", slog.Any("error", err))
		writeJSONError(w, "failed to create app", http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "stored new user app", slog.String("appID", newApp.ID), slog.String("appName", newApp.Name))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	err = json.NewEncoder(w).Encode(struct {
		App   types.PublicUserApp `json:"app"`
		Token string              `json:"token"`
	}{
		App:   newApp.Public(),
		Token: token,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleRevokeApp(w http.ResponseWriter, r *http.Request) {
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		writeJSONError(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.storage.RemoveUserApp(ctx, user.ID, siteID, req.ID); err != nil {
		if errors.Is(err, storage.ErrAppNotFound) {
			writeJSONError(w, "app not found", http.StatusNotFound)
			return
		}
		log.Ctx(ctx).ErrorContext(ctx, "failed to revoke user app", slog.String("appID", req.ID), slog.Any("error", err))
		writeJSONError(w, "failed to revoke app", http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "revoked user app", slog.String("appID", req.ID))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		panic(http.ErrAbortHandler)
	}
}
