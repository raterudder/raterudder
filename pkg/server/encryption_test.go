package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"io"
	"testing"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encryptLegacyV1Credentials(t *testing.T, key string, creds types.Credentials) []byte {
	t.Helper()
	jsonBytes, err := json.Marshal(creds)
	require.NoError(t, err)

	block, err := aes.NewCipher([]byte(key))
	require.NoError(t, err)

	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)

	return gcm.Seal(nonce, nonce, jsonBytes, nil)
}

func TestCredentials(t *testing.T) {
	// 32-byte key for AES-256
	testKey := "01234567890123456789012345678901"

	t.Run("Encrypt and Decrypt", func(t *testing.T) {
		srv := &Server{
			encryptionKey: testKey,
		}

		originalCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:    "test@example.com",
				MD5Password: "password123",
			},
		}

		// Encrypt
		encrypted, err := srv.encryptCredentials(t.Context(), "site-1", originalCreds)
		require.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		assert.True(t, len(encrypted) > len(credsEncryptionPrefixV2)+hkdfSaltSize)
		assert.Equal(t, credsEncryptionPrefixV2, string(encrypted[:len(credsEncryptionPrefixV2)]))

		// Decrypt
		decrypted, needsUpgrade, err := srv.decryptCredentials(t.Context(), "site-1", encrypted)
		require.NoError(t, err)
		assert.False(t, needsUpgrade)
		assert.Equal(t, originalCreds, decrypted)
	})

	t.Run("Decryption with Wrong SiteID Fails", func(t *testing.T) {
		srv := &Server{
			encryptionKey: testKey,
		}

		originalCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:    "test@example.com",
				MD5Password: "password123",
			},
		}

		encrypted, err := srv.encryptCredentials(t.Context(), "site-1", originalCreds)
		require.NoError(t, err)

		_, _, err = srv.decryptCredentials(t.Context(), "site-2", encrypted)
		assert.ErrorContains(t, err, "failed to decrypt credentials")
	})

	t.Run("Decrypt Legacy V1 Signals Upgrade", func(t *testing.T) {
		srv := &Server{
			encryptionKey: testKey,
		}

		originalCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:    "legacy@example.com",
				MD5Password: "legacy123",
			},
		}

		legacyEncrypted := encryptLegacyV1Credentials(t, testKey, originalCreds)

		decrypted, needsUpgrade, err := srv.decryptCredentials(t.Context(), "site-1", legacyEncrypted)
		require.NoError(t, err)
		assert.True(t, needsUpgrade)
		assert.Equal(t, originalCreds, decrypted)

		// Also verify a legacy v1 ciphertext whose random 12-byte nonce happens to start with "v2:"
		jsonBytes, err := json.Marshal(originalCreds)
		require.NoError(t, err)
		block, err := aes.NewCipher([]byte(testKey))
		require.NoError(t, err)
		gcm, err := cipher.NewGCM(block)
		require.NoError(t, err)
		collidingNonce := []byte("v2:345678901") // 12 bytes starting with "v2:"
		collidingLegacy := gcm.Seal(collidingNonce, collidingNonce, jsonBytes, nil)

		decryptedCollision, needsUpgradeCollision, err := srv.decryptCredentials(t.Context(), "site-1", collidingLegacy)
		require.NoError(t, err)
		assert.True(t, needsUpgradeCollision)
		assert.Equal(t, originalCreds, decryptedCollision)
	})

	t.Run("Decryption with Wrong Key Fails", func(t *testing.T) {
		srv1 := &Server{encryptionKey: testKey}
		srv2 := &Server{encryptionKey: "12345678901234567890123456789012"} // Different key

		originalCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "test@example.com"},
		}

		encrypted, err := srv1.encryptCredentials(t.Context(), "site-1", originalCreds)
		require.NoError(t, err)

		_, _, err = srv2.decryptCredentials(t.Context(), "site-1", encrypted)
		assert.Error(t, err)
	})

	t.Run("Missing Key or SiteID Fails", func(t *testing.T) {
		srv := &Server{encryptionKey: ""}

		creds := types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "test@example.com"},
		}

		_, err := srv.encryptCredentials(t.Context(), "site-1", creds)
		assert.ErrorContains(t, err, "no encryption key configured")

		// Let's try decrypting random data
		_, _, err = srv.decryptCredentials(t.Context(), "site-1", []byte("some-random-data"))
		assert.ErrorContains(t, err, "no encryption key configured")

		srvWithKey := &Server{encryptionKey: testKey}
		_, err = srvWithKey.encryptCredentials(t.Context(), "", creds)
		assert.ErrorContains(t, err, "missing siteID")

		_, _, err = srvWithKey.decryptCredentials(t.Context(), "", []byte("v2:0123456789012345"))
		assert.ErrorContains(t, err, "missing siteID")
	})

	t.Run("Malformed Ciphertext", func(t *testing.T) {
		srv := &Server{encryptionKey: testKey}

		// Too short (legacy v1)
		_, _, err := srv.decryptCredentials(t.Context(), "site-1", []byte("short"))
		assert.ErrorContains(t, err, "malformed encrypted credentials")

		// Too short (v2 salt)
		_, _, err = srv.decryptCredentials(t.Context(), "site-1", []byte("v2:short"))
		assert.ErrorContains(t, err, "malformed encrypted credentials")

		// Too short (v2 nonce)
		_, _, err = srv.decryptCredentials(t.Context(), "site-1", []byte("v2:0123456789012345short"))
		assert.ErrorContains(t, err, "malformed encrypted credentials")

		// Random junk
		junk := make([]byte, 50)
		_, _, err = srv.decryptCredentials(t.Context(), "site-1", junk)
		assert.Error(t, err)
	})

	t.Run("Encrypt Empty Credentials", func(t *testing.T) {
		srv := &Server{encryptionKey: testKey}

		creds := types.Credentials{}
		encrypted, err := srv.encryptCredentials(t.Context(), "site-1", creds)
		require.NoError(t, err)

		decrypted, needsUpgrade, err := srv.decryptCredentials(t.Context(), "site-1", encrypted)
		require.NoError(t, err)
		assert.False(t, needsUpgrade)
		assert.Equal(t, creds, decrypted)
	})

}
