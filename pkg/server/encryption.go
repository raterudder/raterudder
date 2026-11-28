package server

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	credsEncryptionPrefixV2 = "v2:"
	hkdfSaltSize            = 16
	hkdfInfoPrefix          = "raterudder-credentials:"
	essStageHKDFInfoPrefix  = "raterudder-ess-stage:"
	essStageTokenTTL        = 30 * time.Minute
)

var errESSStageTokenExpired = errors.New("ess stage token expired")

type essStageTokenPayload struct {
	ESS         string            `json:"ess"`
	Credentials types.Credentials `json:"credentials"`
	ExpiresAt   time.Time         `json:"expiresAt"`
}

func (s *Server) decryptCredentials(ctx context.Context, siteID string, encrypted []byte) (types.Credentials, bool, error) {
	if len(encrypted) == 0 {
		return types.Credentials{}, false, nil
	}

	if s.encryptionKey == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot decrypt credentials: no encryption key configured")
		return types.Credentials{}, false, errors.New("cannot decrypt credentials: no encryption key configured")
	}

	masterKey := []byte(s.encryptionKey)
	if len(masterKey) != 32 {
		log.Ctx(ctx).ErrorContext(ctx, "invalid encryption key length (must be 32 bytes)", slog.Int("length", len(masterKey)))
		return types.Credentials{}, false, errors.New("invalid encryption key length (must be 32 bytes)")
	}

	if bytes.HasPrefix(encrypted, []byte(credsEncryptionPrefixV2)) {
		if siteID == "" {
			log.Ctx(ctx).ErrorContext(ctx, "cannot decrypt credentials: missing siteID")
			return types.Credentials{}, false, errors.New("cannot decrypt credentials: missing siteID")
		}
		creds, v2Err := decryptWithKeyAndAAD(masterKey, siteID, encrypted)
		if v2Err == nil {
			return creds, false, nil
		}
		// A legacy v1 ciphertext has a 12-byte random nonce and at least a 16-byte GCM tag (28+ bytes).
		// With probability 2^-24, a random v1 nonce could start with "v2:". Fall back to v1 before failing.
		if len(encrypted) >= 28 {
			if v1Creds, v1Err := decryptV1(masterKey, encrypted); v1Err == nil {
				return v1Creds, true, nil
			}
		}
		log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", v2Err))
		return types.Credentials{}, false, v2Err
	}

	creds, err := decryptV1(masterKey, encrypted)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
		return types.Credentials{}, false, err
	}

	return creds, true, nil
}

func decryptWithKeyAndAAD(masterKey []byte, siteID string, encrypted []byte) (types.Credentials, error) {
	raw := encrypted[len(credsEncryptionPrefixV2):]
	if len(raw) < hkdfSaltSize {
		return types.Credentials{}, errors.New("malformed encrypted credentials")
	}
	salt := raw[:hkdfSaltSize]
	payload := raw[hkdfSaltSize:]

	key, err := hkdf.Key(sha256.New, masterKey, salt, hkdfInfoPrefix+siteID, 32)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to derive encryption key: %w", err)
	}

	return openAESGCM(key, payload, []byte(siteID))
}

func decryptV1(masterKey []byte, encrypted []byte) (types.Credentials, error) {
	return openAESGCM(masterKey, encrypted, nil)
}

func openAESGCM(key []byte, payload []byte, aad []byte) (types.Credentials, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to create gcm: %w", err)
	}

	if len(payload) < gcm.NonceSize() {
		return types.Credentials{}, errors.New("malformed encrypted credentials")
	}

	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to decrypt credentials: %w", err)
	}

	var creds types.Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return types.Credentials{}, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	return creds, nil
}

func (s *Server) encryptCredentials(ctx context.Context, siteID string, creds types.Credentials) ([]byte, error) {
	if s.encryptionKey == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot encrypt credentials: no encryption key configured")
		return nil, errors.New("cannot encrypt credentials: no encryption key configured")
	}

	if siteID == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot encrypt credentials: missing siteID")
		return nil, errors.New("cannot encrypt credentials: missing siteID")
	}

	jsonBytes, err := json.Marshal(creds)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to marshal credentials", slog.Any("error", err))
		return nil, fmt.Errorf("failed to marshal credentials: %w", err)
	}

	masterKey := []byte(s.encryptionKey)
	if len(masterKey) != 32 {
		log.Ctx(ctx).ErrorContext(ctx, "invalid encryption key length (must be 32 bytes)", slog.Int("length", len(masterKey)))
		return nil, errors.New("invalid encryption key length (must be 32 bytes)")
	}

	salt := make([]byte, hkdfSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate salt", slog.Any("error", err))
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	derivedKey, err := hkdf.Key(sha256.New, masterKey, salt, hkdfInfoPrefix+siteID, 32)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to derive encryption key", slog.Any("error", err))
		return nil, fmt.Errorf("failed to derive encryption key: %w", err)
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to create cipher", slog.Any("error", err))
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to create gcm", slog.Any("error", err))
		return nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to generate nonce", slog.Any("error", err))
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, jsonBytes, []byte(siteID))
	out := make([]byte, 0, len(credsEncryptionPrefixV2)+len(salt)+len(ciphertext))
	out = append(out, credsEncryptionPrefixV2...)
	out = append(out, salt...)
	out = append(out, ciphertext...)
	return out, nil
}

func (s *Server) encryptStageCredentials(ctx context.Context, siteID, essName string, creds types.Credentials, expiresAt time.Time) (string, error) {
	if s.encryptionKey == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot encrypt stage credentials: no encryption key configured")
		return "", errors.New("cannot encrypt stage credentials: no encryption key configured")
	}
	if siteID == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot encrypt stage credentials: missing siteID")
		return "", errors.New("cannot encrypt stage credentials: missing siteID")
	}
	if essName == "" {
		return "", errors.New("cannot encrypt stage credentials: missing ess")
	}
	if expiresAt.IsZero() {
		return "", errors.New("cannot encrypt stage credentials: missing expiration")
	}

	masterKey := []byte(s.encryptionKey)
	if len(masterKey) != 32 {
		log.Ctx(ctx).ErrorContext(ctx, "invalid encryption key length (must be 32 bytes)", slog.Int("length", len(masterKey)))
		return "", errors.New("invalid encryption key length (must be 32 bytes)")
	}

	payloadBytes, err := json.Marshal(essStageTokenPayload{
		ESS:         essName,
		Credentials: creds,
		ExpiresAt:   expiresAt.UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal stage credentials: %w", err)
	}

	salt := make([]byte, hkdfSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	domain := essStageHKDFInfoPrefix + siteID
	derivedKey, err := hkdf.Key(sha256.New, masterKey, salt, domain, 32)
	if err != nil {
		return "", fmt.Errorf("failed to derive stage encryption key: %w", err)
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, payloadBytes, []byte(domain))
	out := make([]byte, 0, len(credsEncryptionPrefixV2)+len(salt)+len(ciphertext))
	out = append(out, credsEncryptionPrefixV2...)
	out = append(out, salt...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (s *Server) decryptStageCredentials(ctx context.Context, siteID, expectedESS, token string, now time.Time) (types.Credentials, error) {
	if token == "" {
		return types.Credentials{}, errors.New("missing ess stage token")
	}
	if s.encryptionKey == "" {
		log.Ctx(ctx).ErrorContext(ctx, "cannot decrypt stage credentials: no encryption key configured")
		return types.Credentials{}, errors.New("cannot decrypt stage credentials: no encryption key configured")
	}
	if siteID == "" {
		return types.Credentials{}, errors.New("cannot decrypt stage credentials: missing siteID")
	}
	if expectedESS == "" {
		return types.Credentials{}, errors.New("cannot decrypt stage credentials: missing ess")
	}

	masterKey := []byte(s.encryptionKey)
	if len(masterKey) != 32 {
		log.Ctx(ctx).ErrorContext(ctx, "invalid encryption key length (must be 32 bytes)", slog.Int("length", len(masterKey)))
		return types.Credentials{}, errors.New("invalid encryption key length (must be 32 bytes)")
	}

	encrypted, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("malformed ess stage token: %w", err)
	}
	if !bytes.HasPrefix(encrypted, []byte(credsEncryptionPrefixV2)) {
		return types.Credentials{}, errors.New("malformed ess stage token")
	}

	raw := encrypted[len(credsEncryptionPrefixV2):]
	if len(raw) < hkdfSaltSize {
		return types.Credentials{}, errors.New("malformed ess stage token")
	}
	salt := raw[:hkdfSaltSize]
	sealed := raw[hkdfSaltSize:]

	domain := essStageHKDFInfoPrefix + siteID
	derivedKey, err := hkdf.Key(sha256.New, masterKey, salt, domain, 32)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to derive stage encryption key: %w", err)
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to create gcm: %w", err)
	}

	if len(sealed) < gcm.NonceSize() {
		return types.Credentials{}, errors.New("malformed ess stage token")
	}

	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(domain))
	if err != nil {
		return types.Credentials{}, fmt.Errorf("failed to decrypt ess stage token: %w", err)
	}

	var payload essStageTokenPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return types.Credentials{}, fmt.Errorf("failed to unmarshal ess stage token: %w", err)
	}

	if payload.ESS != expectedESS {
		return types.Credentials{}, errors.New("ess stage token provider mismatch")
	}
	if payload.ExpiresAt.IsZero() || !now.Before(payload.ExpiresAt) {
		return types.Credentials{}, errESSStageTokenExpired
	}

	return payload.Credentials, nil
}
