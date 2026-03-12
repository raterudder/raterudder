package utility

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// SiteComEd wraps BaseComEd to apply site-specific settings and fees.
type SiteFees struct {
	base    UtilityPrices
	mu      sync.Mutex
	siteID  string
	periods []types.UtilityAdditionalFeesPeriod
	name    string
}

// Name returns the name of the utility rate
func (s *SiteFees) Name() string {
	return s.name
}

// ApplySettings implements the Utility interface
func (s *SiteFees) ApplySettings(ctx context.Context, settings types.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// if they don't have any additional fees periods, we will need to find the
	// default for their utility provider
	if settings.AdditionalFeesPeriods == nil {
		switch settings.UtilityProvider {
		case "comed":
			if settings.UtilityRate != "comed_besh" {
				return fmt.Errorf("invalid utility rate for ComEd: %s", settings.UtilityRate)
			}
			fees, err := getComEdAdditionalFees(settings.UtilityRateOptions)
			if err != nil {
				return err
			}
			s.periods = fees
			s.name = settings.UtilityRate
		case "ameren":
			if settings.UtilityRate != "ameren_psp" {
				return fmt.Errorf("invalid utility rate for Ameren: %s", settings.UtilityRate)
			}
			fees, err := getAmerenAdditionalFees(settings.UtilityRateOptions)
			if err != nil {
				return err
			}
			s.periods = fees
			s.name = settings.UtilityRate
		default:
			return fmt.Errorf("invalid utility provider: %s", settings.UtilityProvider)
		}
	} else {
		s.periods = settings.AdditionalFeesPeriods
	}

	return nil
}

func (s *SiteFees) applyFees(p types.Price) (types.Price, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, period := range s.periods {
		// Calculate time-of-day in minutes for easier comparison if needed, or just use hour
		// Check date range
		if !period.Start.IsZero() && p.TSStart.Before(period.Start) {
			continue
		}
		// period.End is exclusive: skip if the price starts at or after End
		if !period.End.IsZero() && !p.TSStart.Before(period.End) {
			continue
		}

		// Check hour range (inclusive start, exclusive end)
		// Assuming p.TSStart is the start of the hour
		h := p.TSStart.In(ctLocation).Hour()
		if h < period.HourStart || h >= period.HourEnd {
			continue
		}

		// Apply fee
		if period.GridAdditional {
			p.GridUseDollarsPerKWH += period.DollarsPerKWH
		} else {
			p.DollarsPerKWH += period.DollarsPerKWH
		}
	}
	return p, nil
}

func (s *SiteFees) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	prices, err := s.base.GetConfirmedPrices(ctx, start, end)
	if err != nil {
		return nil, err
	}
	for i := range prices {
		prices[i], err = s.applyFees(prices[i])
		if err != nil {
			return nil, err
		}
	}
	return prices, nil
}

func (s *SiteFees) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	p, err := s.base.GetCurrentPrice(ctx)
	if err != nil {
		return types.Price{}, err
	}
	return s.applyFees(p)
}

func (s *SiteFees) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	prices, err := s.base.GetFuturePrices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]types.Price, len(prices))
	for i, p := range prices {
		out[i], err = s.applyFees(p)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
