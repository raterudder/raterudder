package ess

import (
	"context"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// System defines the interface for interacting with an Energy Storage System (like FranklinWH).
type System interface {
	// GetStatus returns the current status of the system.
	GetStatus(ctx context.Context) (types.SystemStatus, error)

	// SetModes sets the operating modes of the system.
	SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error

	// ApplySettings updates the system using the provided global settings.
	ApplySettings(ctx context.Context, settings types.Settings) error

	// Authenticate validates the credentials that were applied and returns updated
	// credentials along with a bool indicating if the credentials were updated.
	// Avoid updating any caches/state until the sent credentials are valid/successful.
	// This should be called AFTER ApplySettings.
	Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error)

	// GetEnergyHistory returns the energy history for the specified period.
	GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error)
}

// Configured sets up the ESS system provider Map
func Configured() *Map {
	return NewMap()
}

// Map manages multiple ESS systems.
type Map struct {
	mu      sync.Mutex
	systems map[string]System
}

// NewMap creates a new ESS Map.
func NewMap() *Map {
	return &Map{
		systems: make(map[string]System),
	}
}

// ListSystems returns the available ESS systems and their required credentials.
func (m *Map) ListSystems() []types.ESSProviderInfo {
	return []types.ESSProviderInfo{
		franklinInfo(),
		mockInfo(),
	}
}

// Site returns the system for the given siteID.
// If the siteID is new, it creates a new system instance.
func (m *Map) Site(ctx context.Context, siteID string, settings types.Settings) (System, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if siteID == "" {
		siteID = types.SiteIDNone
	}

	if sys, ok := m.systems[siteID]; ok {
		if err := sys.ApplySettings(ctx, settings); err != nil {
			return nil, err
		}
		return sys, nil
	}

	var sys System
	switch settings.ESS {
	case "franklin":
		sys = newFranklin()
	case "mock":
		sys = newMock(siteID)
	default:
		// Default to franklin for backwards compatibility if not specified
		// or if an unknown system is provided.
		sys = newFranklin()
	}

	if err := sys.ApplySettings(ctx, settings); err != nil {
		return nil, err
	}
	m.systems[siteID] = sys
	return sys, nil
}

// SetSystem sets the system for a specific site. This is primarily used for testing.
func (m *Map) SetSystem(siteID string, sys System) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systems[siteID] = sys
}
