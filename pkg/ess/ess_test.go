package ess

import (
	"context"
	"log/slog"
	"testing"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStorage = storagemock.MockDatabase

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestListSystems(t *testing.T) {
	m := NewMap()
	systems := m.ListSystems(context.Background())

	require.NotEmpty(t, systems, "expected at least one system")

	ids := make(map[string]bool)
	for _, s := range systems {
		assert.NotEmpty(t, s.ID, "system must have an ID")
		assert.NotEmpty(t, s.Name, "system must have a name")
		assert.False(t, ids[s.ID], "system IDs must be unique, duplicate: %s", s.ID)
		ids[s.ID] = true

		for _, opt := range s.Credentials {
			assert.NotEmpty(t, opt.Field, "credential must have a Field")
			assert.NotEmpty(t, opt.Name, "credential must have a name")
			assert.True(t, opt.Type == types.ESSCredentialFieldTypeSelect || opt.Type == types.ESSCredentialFieldTypeString || opt.Type == types.ESSCredentialFieldTypePassword,
				"credential type must be 'select' or 'string' or 'password', got %q", opt.Type)

			if opt.Type == types.ESSCredentialFieldTypeSelect {
				assert.NotEmpty(t, opt.Choices, "select option %q must have choices", opt.Field)
				for _, c := range opt.Choices {
					assert.NotEmpty(t, c.Value, "choice must have a Value in option %q", opt.Field)
					assert.NotEmpty(t, c.Name, "choice must have a name in option %q", opt.Field)
				}
				assert.NotNil(t, opt.Default, "select option %q must have a default", opt.Field)
			}
		}
	}
}
