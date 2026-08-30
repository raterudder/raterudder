package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSessionSecret(t *testing.T) {
	t.Run("Generates valid unique 32-char hex secret", func(t *testing.T) {
		s1, err := generateSessionSecret()
		require.NoError(t, err)
		if assert.Len(t, s1, 32) {
			s2, err := generateSessionSecret()
			require.NoError(t, err)
			assert.Len(t, s2, 32)
			assert.NotEqual(t, s1, s2)
		}
	})
}

func TestSessionToken(t *testing.T) {
	testKey := "12345678901234567890123456789012"
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	s := &Server{
		sessionEncryptionKey: testKey,
		nowFunc:              func() time.Time { return now },
	}

	t.Run("Valid token roundtrip", func(t *testing.T) {
		token, err := s.createSessionToken("google:12345", "user@example.com", "my-secret-123", 7*24*time.Hour)
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		session, err := s.verifySessionToken(token)
		require.NoError(t, err)
		if assert.NotNil(t, session) {
			assert.Equal(t, "google:12345", session.UserID)
			assert.Equal(t, "user@example.com", session.Email)
			assert.Equal(t, "my-secret-123", session.SessionSecret)
			assert.Equal(t, now.Add(7*24*time.Hour).Unix(), session.ExpiresAt)
		}
	})

	t.Run("Expired token rejected", func(t *testing.T) {
		token, err := s.createSessionToken("google:12345", "user@example.com", "my-secret-123", 1*time.Hour)
		require.NoError(t, err)

		// Advance time by 2 hours
		expiredServer := &Server{
			sessionEncryptionKey: testKey,
			nowFunc:              func() time.Time { return now.Add(2 * time.Hour) },
		}

		session, err := expiredServer.verifySessionToken(token)
		assert.ErrorContains(t, err, "session token expired")
		assert.Nil(t, session)
	})

	t.Run("Tampered ciphertext fails decryption", func(t *testing.T) {
		token, err := s.createSessionToken("google:12345", "user@example.com", "my-secret-123", 7*24*time.Hour)
		require.NoError(t, err)

		// Tamper with characters in the middle of the base64 string
		tampered := token
		if len(token) > 10 {
			r := []rune(token)
			if r[5] == 'A' {
				r[5] = 'B'
			} else {
				r[5] = 'A'
			}
			tampered = string(r)
		}

		session, err := s.verifySessionToken(tampered)
		assert.Error(t, err)
		assert.Nil(t, session)
	})

	t.Run("Wrong key fails decryption", func(t *testing.T) {
		token, err := s.createSessionToken("google:12345", "user@example.com", "my-secret-123", 7*24*time.Hour)
		require.NoError(t, err)

		otherServer := &Server{
			sessionEncryptionKey: strings.Repeat("x", 32),
			nowFunc:              func() time.Time { return now },
		}

		session, err := otherServer.verifySessionToken(token)
		assert.Error(t, err)
		assert.Nil(t, session)
	})

	t.Run("Invalid key length returns error", func(t *testing.T) {
		invalidServer := &Server{
			sessionEncryptionKey: "short-key",
			nowFunc:              func() time.Time { return now },
		}

		token, err := invalidServer.createSessionToken("google:12345", "user@example.com", "secret", 1*time.Hour)
		assert.ErrorContains(t, err, "invalid session encryption key")
		assert.Empty(t, token)

		session, err := invalidServer.verifySessionToken("some-token")
		assert.ErrorContains(t, err, "invalid session encryption key")
		assert.Nil(t, session)
	})

	t.Run("Malformed base64 token returns error", func(t *testing.T) {
		session, err := s.verifySessionToken("!!!not-base64!!!")
		assert.Error(t, err)
		assert.Nil(t, session)
	})
}
