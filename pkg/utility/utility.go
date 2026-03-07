package utility

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// UtilityPrices defines the interface for a utility prices provider.
type UtilityPrices interface {
	// GetCurrentPrice returns the current price of electricity.
	GetCurrentPrice(ctx context.Context) (types.Price, error)

	// GetFuturePrices returns a list of future prices.
	GetFuturePrices(ctx context.Context) ([]types.Price, error)

	// GetConfirmedPrices returns confirmed prices for a specific time range.
	// This should be used for syncing historical data.
	GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error)
}

// Utility defines the interface for a utility provider.
type Utility interface {
	UtilityPrices

	// ApplySettings updates the system using the provided global settings.
	ApplySettings(ctx context.Context, settings types.Settings) error
}

// Configured sets up the utility providers and returns a Map.
func Configured() *Map {
	m := NewMap()
	// Initialize supported providers
	m.baseComEdHourly = configuredComEdHourly()
	m.baseAmerenSmart = configuredAmerenSmart()
	return m
}

// Map manages utility providers.
type Map struct {
	mu              sync.Mutex
	baseComEdHourly *BaseComEdHourly
	baseAmerenSmart *BaseAmerenSmart
	utilities       map[string]Utility
}

// NewMap creates a new Utility Map.
func NewMap() *Map {
	return &Map{
		utilities: make(map[string]Utility),
	}
}

// Site returns the utility provider for the given site based on settings.
func (m *Map) Site(ctx context.Context, siteID string, settings types.Settings) (Utility, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.utilities[settings.UtilityProvider]; ok {
		if err := p.ApplySettings(ctx, settings); err != nil {
			return nil, err
		}
		return p, nil
	}

	switch settings.UtilityProvider {
	case "comed":
		if m.baseComEdHourly == nil {
			return nil, fmt.Errorf("comed provider not configured")
		}
		// For now we only support BESH
		if settings.UtilityRate != "comed_besh" {
			return nil, fmt.Errorf("unsupported comed rate: %s", settings.UtilityRate)
		}
		u := &SiteFees{
			base:   m.baseComEdHourly,
			siteID: siteID,
		}
		if err := u.ApplySettings(ctx, settings); err != nil {
			return nil, err
		}
		m.utilities[settings.UtilityProvider] = u
		return u, nil
	case "ameren":
		if m.baseAmerenSmart == nil {
			return nil, fmt.Errorf("ameren provider not configured")
		}
		if settings.UtilityRate != "ameren_psp" {
			return nil, fmt.Errorf("unsupported ameren rate: %s", settings.UtilityRate)
		}
		u := &SiteFees{
			base:   m.baseAmerenSmart,
			siteID: siteID,
		}
		if err := u.ApplySettings(ctx, settings); err != nil {
			return nil, err
		}
		m.utilities[settings.UtilityProvider] = u
		return u, nil
	default:
		return nil, fmt.Errorf("unknown utility provider: %s", settings.UtilityProvider)
	}
}

// SetProvider sets a mock provider for testing.
func (m *Map) SetProvider(name string, provider Utility) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.utilities[name] = provider
}

// ListUtilities returns metadata for all supported utility providers.
func (m *Map) ListUtilities() []types.UtilityProviderInfo {
	return []types.UtilityProviderInfo{
		comEdUtilityInfo(),
		amerenUtilityInfo(),
	}
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
