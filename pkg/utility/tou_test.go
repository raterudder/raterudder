package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOUUtility(t *testing.T) {
	u := &genericTOU{}
	err := u.ApplySettings(context.Background(), types.Settings{
		UtilityProvider: "tou",
		UtilityRate:     "example",
	})
	require.NoError(t, err)

	// Test GetCurrentPrice
	p, err := u.GetCurrentPrice(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tou", p.Provider)

	// Test GetFuturePrices
	future, err := u.GetFuturePrices(context.Background())
	require.NoError(t, err)
	assert.Len(t, future, 48)

	// Test GetConfirmedPrices
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	confirmed, err := u.GetConfirmedPrices(context.Background(), start, end)
	require.NoError(t, err)
	assert.Len(t, confirmed, 24)

	// Verify price changes over a day
	for _, cp := range confirmed {
		// New York location should be set
		h := cp.TSStart.In(u.location).Hour()
		if h >= 0 && h < 6 {
			assert.Equal(t, 0.01, cp.DollarsPerKWH)
		} else if h >= 6 && h < 12 {
			assert.Equal(t, 0.02, cp.DollarsPerKWH)
		} else {
			assert.Equal(t, 0.10, cp.DollarsPerKWH)
		}
	}
}
