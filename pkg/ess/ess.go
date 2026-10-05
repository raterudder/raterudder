package ess

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

var ErrCredentialsMissing = errors.New("credentials missing")
var ErrNeedsNextStage = errors.New("authentication needs next stage")

// ErrUnauthorized is returned when the request is unauthorized or has missing scopes.
var ErrUnauthorized = errors.New("unauthorized")

// System defines the interface for interacting with an Energy Storage System (like FranklinWH).
type System interface {
	// Name returns the name of the system.
	Name() string

	// GetStatus returns the current status of the system.
	GetStatus(ctx context.Context) (types.SystemStatus, error)

	// SetModes sets the operating modes of the system.
	SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error)

	// ApplySettings updates the system using the provided global settings.
	ApplySettings(ctx context.Context, settings types.Settings) error

	// Authenticate validates the credentials that were applied and returns updated
	// credentials along with a bool indicating if the credentials were updated.
	// Avoid updating any caches/state until the sent credentials are valid/successful.
	// This should be called AFTER ApplySettings.
	Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error)

	// GetEnergyHistory returns the energy history for the specified period.
	GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error)

	// GridSettings returns the grid-related configuration reported by the ESS.
	GridSettings(ctx context.Context) (types.GridSettings, error)
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

// calculateDurationWeightedRates computes the duration-weighted average buy and sell rates
// for a slice of TOU periods, rounded to 5 decimal places ($0.00001 / 0.001¢ precision).
// This preserves real sub-cent pricing accuracy from dynamic utility tariffs (e.g. ComEd RTP)
// while eliminating IEEE 754 floating-point division noise, and ensures that buyRate >= sellRate.
func calculateDurationWeightedRates(periods []types.TOUPeriod) (buyRate float64, sellRate float64) {
	if len(periods) == 0 {
		return 0, 0
	}
	var totalDur int
	var buySum, sellSum float64
	for _, p := range periods {
		dur := p.DurationMinutes()
		if dur <= 0 {
			dur = 60
		}
		totalDur += dur
		buySum += p.ImportDollars * float64(dur)
		sellSum += p.ExportDollars * float64(dur)
	}
	if totalDur == 0 {
		return 0, 0
	}
	bRate := math.Round((buySum/float64(totalDur))*100000) / 100000
	sRate := math.Round((sellSum/float64(totalDur))*100000) / 100000
	if sRate > bRate {
		bRate = sRate
	}
	return bRate, sRate
}

// touTier represents a normalized time-of-use rate tier.
type touTier string

const (
	touTierOnPeak       touTier = "ON_PEAK"
	touTierPartialPeak  touTier = "PARTIAL_PEAK" // Shoulder
	touTierOffPeak      touTier = "OFF_PEAK"     // Valley
	touTierSuperOffPeak touTier = "SUPER_OFF_PEAK"
)

const (
	// multiTierSpreadThresholdDollars defines the minimum rate spread ($/kWh) among non-peak periods
	// before activating 3 non-peak tiers (SUPER_OFF_PEAK, OFF_PEAK, PARTIAL_PEAK).
	// If the spread is <= $0.05, at most 2 non-peak tiers are used to prevent micro-segmentation.
	multiTierSpreadThresholdDollars = 0.05
)

// classifyTOUScheduleTiers partitions schedule periods into up to 4 standard TOU tiers:
// ON_PEAK, PARTIAL_PEAK (Shoulder), OFF_PEAK (Valley), and SUPER_OFF_PEAK.
// It returns:
// 1. A slice of TOUTier mapped 1:1 with the input periods.
// 2. A map of TOUTier to the slice of periods belonging to that tier.
func classifyTOUScheduleTiers(periods []types.TOUPeriod) ([]touTier, map[touTier][]types.TOUPeriod) {
	tiers := make([]touTier, len(periods))
	tierPeriods := make(map[touTier][]types.TOUPeriod)

	var nonPeakIndices []int
	var nonPeakRates []float64

	for i, p := range periods {
		if p.Peak || p.BatteryMode == types.BatteryModeExport || p.SolarMode == types.SolarModeExport {
			tiers[i] = touTierOnPeak
			tierPeriods[touTierOnPeak] = append(tierPeriods[touTierOnPeak], p)
		} else {
			nonPeakIndices = append(nonPeakIndices, i)
			nonPeakRates = append(nonPeakRates, p.ImportDollars)
		}
	}

	if len(nonPeakIndices) > 0 {
		sortedRates := make([]float64, len(nonPeakRates))
		copy(sortedRates, nonPeakRates)
		sort.Float64s(sortedRates)

		// Group rates within types.TOUCoalesceRateThresholdDollars ($0.015) into clusters
		var clusters [][]float64
		for _, r := range sortedRates {
			if len(clusters) == 0 {
				clusters = append(clusters, []float64{r})
			} else {
				lastCluster := clusters[len(clusters)-1]
				if r-lastCluster[0] <= types.TOUCoalesceRateThresholdDollars {
					clusters[len(clusters)-1] = append(clusters[len(clusters)-1], r)
				} else {
					clusters = append(clusters, []float64{r})
				}
			}
		}

		k := len(clusters)
		spread := sortedRates[len(sortedRates)-1] - sortedRates[0]

		getNonPeakTier := func(rate float64) touTier {
			if k <= 1 || spread <= types.TOUCoalesceRateThresholdDollars {
				return touTierOffPeak
			}
			if k == 2 || spread <= multiTierSpreadThresholdDollars {
				// 2 non-peak tiers: SUPER_OFF_PEAK and OFF_PEAK
				midCluster := len(clusters) / 2
				maxSuperOffPeakRate := clusters[midCluster-1][len(clusters[midCluster-1])-1]
				if rate <= maxSuperOffPeakRate {
					return touTierSuperOffPeak
				}
				return touTierOffPeak
			}
			// 3 non-peak tiers: SUPER_OFF_PEAK, OFF_PEAK, and PARTIAL_PEAK
			numLow := max(1, len(clusters)/3)
			numHigh := max(1, len(clusters)/3)
			maxSuperOffPeakRate := clusters[numLow-1][len(clusters[numLow-1])-1]
			minPartialPeakRate := clusters[len(clusters)-numHigh][0]
			if rate <= maxSuperOffPeakRate {
				return touTierSuperOffPeak
			}
			if rate >= minPartialPeakRate {
				return touTierPartialPeak
			}
			return touTierOffPeak
		}

		for _, idx := range nonPeakIndices {
			p := periods[idx]
			tier := getNonPeakTier(p.ImportDollars)
			tiers[idx] = tier
			tierPeriods[tier] = append(tierPeriods[tier], p)
		}
	}

	return tiers, tierPeriods
}
