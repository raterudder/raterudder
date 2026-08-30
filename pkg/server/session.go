package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// SessionData holds the decrypted session payload stored inside the encrypted session cookie.
type SessionData struct {
	UserID        string `json:"userID"`
	SessionSecret string `json:"sessionSecret,omitempty"`
	ExpiresAt     int64  `json:"expiresAt"`
	Email         string `json:"email,omitempty"`
}

// generateSessionSecret creates a 16-byte cryptographically secure random hex string.
func generateSessionSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate random session secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// createSessionToken mints an XChaCha20-Poly1305 encrypted session token with a URL-safe base64 encoding.
func (s *Server) createSessionToken(userID, email, sessionSecret string, duration time.Duration) (string, error) {
	if len(s.sessionEncryptionKey) != 32 {
		return "", errors.New("cannot create session token: invalid session encryption key (must be 32 bytes)")
	}

	session := SessionData{
		UserID:        userID,
		SessionSecret: sessionSecret,
		ExpiresAt:     s.now().Add(duration).Unix(),
		Email:         email,
	}

	jsonBytes, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("failed to marshal session data: %w", err)
	}

	aead, err := chacha20poly1305.NewX([]byte(s.sessionEncryptionKey))
	if err != nil {
		return "", fmt.Errorf("failed to create session cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate session nonce: %w", err)
	}

	ciphertext := aead.Seal(nonce, nonce, jsonBytes, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// verifySessionToken decrypts and validates an XChaCha20-Poly1305 encrypted session token.
func (s *Server) verifySessionToken(tokenStr string) (*SessionData, error) {
	if len(s.sessionEncryptionKey) != 32 {
		return nil, errors.New("cannot verify session token: invalid session encryption key (must be 32 bytes)")
	}

	raw, err := base64.RawURLEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session token: %w", err)
	}

	aead, err := chacha20poly1305.NewX([]byte(s.sessionEncryptionKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create session cipher: %w", err)
	}

	if len(raw) < aead.NonceSize() {
		return nil, errors.New("malformed session token: too short")
	}

	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt session token: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	if s.now().Unix() > session.ExpiresAt {
		return nil, errors.New("session token expired")
	}

	return &session, nil
}

// setSessionCookie sets the session_token cookie on the response.
func (s *Server) setSessionCookie(w http.ResponseWriter, sessionToken string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionTokenCookie,
		Value:    sessionToken,
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
	})
}
