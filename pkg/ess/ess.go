package ess

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

var ErrCredentialsMissing = errors.New("credentials missing")
var ErrNeedsNextStage = errors.New("authentication needs next stage")

// System defines the interface for interacting with an Energy Storage System (like FranklinWH).
type System interface {
	// Name returns the name of the system.
	Name() string

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
	GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error)
}

// Map manages multiple ESS systems.
type Map struct {
	mu        sync.Mutex
	systems   map[string]System
	baseTesla *baseTesla
}

// NewMap creates a new ESS Map.
func NewMap() *Map {
	return &Map{
		systems: make(map[string]System),
	}
}

// Configured sets up the ESS system provider Map
func Configured() *Map {
	m := NewMap()
	m.baseTesla = configuredBaseTesla()
	return m
}

// ListSystems returns the available ESS systems and their required credentials.
func (m *Map) ListSystems(ctx context.Context) []types.ESSProviderInfo {
	sys := []types.ESSProviderInfo{
		franklinInfo(),
		enphaseInfo(),
		mockInfo(),
	}
	if m.baseTesla != nil && m.baseTesla.enabled() {
		sys = append(sys, m.baseTesla.info(ctx))
	}
	return sys
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
		if settings.ESS == "" || sys.Name() == settings.ESS {
			if err := sys.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
			return sys, nil
		}
		log.Ctx(ctx).InfoContext(ctx, "site changed ess system", slog.String("expected", settings.ESS), slog.String("actual", sys.Name()))
	}

	var sys System
	switch settings.ESS {
	case "franklin":
		sys = newFranklin()
	case "enphase":
		sys = newEnphase()
	case "mock":
		sys = newMock(siteID)
	case "tesla":
		if m.baseTesla == nil {
			return nil, errors.New("tesla client is not configured")
		}
		sys = newTesla(m.baseTesla)
	default:
		return nil, errors.New("unknown ESS system: " + settings.ESS)
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

// RegisterTesla generates a partner authentication token and calls the Tesla register endpoint.
func (m *Map) RegisterTesla(ctx context.Context, domain string) error {
	if m.baseTesla == nil {
		return errors.New("tesla not configured")
	}
	return m.baseTesla.RegisterTesla(ctx, domain)
}
