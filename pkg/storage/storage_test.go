package storage

import (
	"log/slog"
	"os"
	"testing"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestUnwrap(t *testing.T) {
	t.Run("UnwrapsDatabaseWrapper", func(t *testing.T) {
		inner := &FirestoreProvider{
			projectID: "test-proj",
			database:  "test-db",
		}
		wrapped := &databaseWrapper{Database: inner}

		got := Unwrap(wrapped)
		assert.Same(t, inner, got)
	})

	t.Run("ReturnsUnwrappedDatabaseDirectly", func(t *testing.T) {
		inner := &FirestoreProvider{
			projectID: "direct-proj",
			database:  "direct-db",
		}

		assert.Same(t, inner, Unwrap(inner))
		assert.Nil(t, Unwrap(nil))
	})
}

func TestConfigured(t *testing.T) {
	t.Run("FirestoreProvider", func(t *testing.T) {
		t.Setenv("FIRESTORE_EMULATOR_HOST", "")
		lflag.Reset()
		defer lflag.Reset()

		db := Configured()
		require.NotNil(t, db)
		assert.Nil(t, Unwrap(db))

		lflag.Parse(lflag.SourceStub{
			"storage-provider":     "firestore",
			"firestore-project-id": "configured-project",
			"firestore-database":   "configured-db",
			"firestore-emulator":   "127.0.0.1:8087",
		})
		defer func() {
			assert.NoError(t, db.Close())
		}()

		assert.Equal(t, "127.0.0.1:8087", os.Getenv("FIRESTORE_EMULATOR_HOST"))
		fs, ok := Unwrap(db).(*FirestoreProvider)
		require.True(t, ok)
		assert.Equal(t, "configured-project", fs.projectID)
		assert.Equal(t, "configured-db", fs.database)
		assert.NotNil(t, fs.FirestoreClient())
	})

	t.Run("UnknownProviderPanics", func(t *testing.T) {
		lflag.Reset()
		defer lflag.Reset()

		db := Configured()
		require.NotNil(t, db)

		assert.PanicsWithValue(t, "unknown storage provider: invalid-provider", func() {
			lflag.Parse(lflag.SourceStub{
				"storage-provider": "invalid-provider",
			})
		})
	})
}
