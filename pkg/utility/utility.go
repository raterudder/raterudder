package utility

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
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

	// Name returns the utility rate name.
	Name() string

	// ApplySettings updates the system using the provided global settings.
	ApplySettings(ctx context.Context, settings types.Settings) error

	// GetVPPInfo returns the VPP info for the utility provider.
	GetVPPInfo(ctx context.Context) (types.UtilityVPPInfo, error)

	// GetPeriods returns the periods for the utility provider. This might return
	// an empty list if the utility doesn't have any pre-defined periods.
	GetPeriods(ctx context.Context) ([]types.TimePeriod, error)
}

// Configured sets up the utility providers and returns a Map.
func Configured(db storage.Database) *Map {
	m := NewMap(db)
	// Initialize supported providers
	m.baseComEdHourly = configuredComEdHourly(db)
	m.baseAmerenSmart = configuredAmerenSmart(db)
	return m
}

// Map manages utility providers.
type Map struct {
	mu              sync.Mutex
	db              storage.Database
	baseComEdHourly *baseComEdHourly
	baseAmerenSmart *baseAmerenSmart
	utilities       map[string]Utility
}

// NewMap creates a new Utility Map.
func NewMap(db storage.Database) *Map {
	return &Map{
		db:        db,
		utilities: make(map[string]Utility),
	}
}

// Site returns the utility provider for the given site based on settings.
func (m *Map) Site(ctx context.Context, siteID string, settings types.Settings) (Utility, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.utilities[siteID]; ok {
		if settings.UtilityRate == "" || p.Name() == settings.UtilityRate {
			if err := p.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
			return p, nil
		}
		log.Ctx(ctx).Warn("site changed utility rate", slog.String("expected", settings.UtilityRate), slog.String("actual", p.Name()))
	}

	var u Utility
	switch settings.UtilityProvider {
	case "comed":
		if settings.UtilityRate == "comed_bes" || settings.UtilityRate == "comed_best" {
			u = &genericTOU{
				siteID: siteID,
			}
			if err := u.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
		} else {
			if m.baseComEdHourly == nil {
				return nil, fmt.Errorf("comed provider not configured")
			}
			u = &SiteFees{
				base:   m.baseComEdHourly,
				siteID: siteID,
			}
			if err := u.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
		}
	case "ameren":
		if settings.UtilityRate == "ameren_bgs" {
			u = &genericTOU{
				siteID: siteID,
			}
			if err := u.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
		} else {
			if m.baseAmerenSmart == nil {
				return nil, fmt.Errorf("ameren provider not configured")
			}
			u = &SiteFees{
				base:   m.baseAmerenSmart,
				siteID: siteID,
			}
			if err := u.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
		}
	default:
		if _, ok := touUtilitiesMap[settings.UtilityProvider]; ok {
			u = &genericTOU{
				siteID: siteID,
			}
			if err := u.ApplySettings(ctx, settings); err != nil {
				return nil, err
			}
			m.utilities[siteID] = u
			return u, nil
		}
		return nil, fmt.Errorf("unknown utility provider: %s", settings.UtilityProvider)
	}
	m.utilities[siteID] = u
	return u, nil
}

// SetProvider sets a mock provider for testing.
func (m *Map) SetProvider(siteID string, provider Utility) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.utilities[siteID] = provider
}

var (
	touUtilities    = touUtilityInfo()
	touUtilitiesMap = func() map[string]types.UtilityProviderInfo {
		out := make(map[string]types.UtilityProviderInfo)
		for _, u := range touUtilities {
			out[u.ID] = u
		}
		return out
	}()
	allUtilities = append([]types.UtilityProviderInfo{
		comEdUtilityInfo(),
		amerenUtilityInfo(),
	}, touUtilities...)
	utilityRateFeesMap = func() map[string]func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
		out := make(map[string]func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error))
		for _, u := range allUtilities {
			for _, r := range u.Rates {
				// might be nil but that's okay we will check it later
				out[r.ID] = r.GetFees
			}
		}
		return out
	}()
	utilityRateVPPMap = func() map[string]func(types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
		out := make(map[string]func(types.UtilityRateOptions) (types.UtilityVPPInfo, error))
		for _, u := range allUtilities {
			for _, r := range u.Rates {
				if r.GetVPP != nil {
					out[r.ID] = r.GetVPP
				}
			}
		}
		return out
	}()
)

func getUtilityRateFees(rate string, options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	if rate == "gp_tou_oa_14" {
		rate = "gp_tou_oa"
	} else if rate == "gp_tou_rd_11" {
		rate = "gp_tou_rd"
	} else if rate == "gp_tou_reo_18" {
		rate = "gp_tou_reo"
	}

	fees, ok := utilityRateFeesMap[rate]
	if !ok {
		return nil, fmt.Errorf("unknown utility rate: %s", rate)
	}
	// this just means no fees
	if fees == nil {
		return nil, nil
	}
	return fees(options)
}

func getUtilityVPPInfo(rate string, options types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
	if rate == "gp_tou_oa_14" {
		rate = "gp_tou_oa"
	} else if rate == "gp_tou_rd_11" {
		rate = "gp_tou_rd"
	} else if rate == "gp_tou_reo_18" {
		rate = "gp_tou_reo"
	}

	getVPP, ok := utilityRateVPPMap[rate]
	if !ok {
		// not every rate supports VPP so we ignore when one isn't found
		return types.UtilityVPPInfo{}, nil
	}
	if getVPP == nil {
		return types.UtilityVPPInfo{}, nil
	}
	return getVPP(options)
}

// ListUtilities returns metadata for all supported utility providers.
func (m *Map) ListUtilities() []types.UtilityProviderInfo {
	return allUtilities
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
