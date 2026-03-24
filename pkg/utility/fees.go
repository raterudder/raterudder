package utility

import (
	"context"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// SiteComEd wraps BaseComEd to apply site-specific settings and fees.
type SiteFees struct {
	base    UtilityPrices
	mu      sync.Mutex
	siteID  string
	periods []types.UtilityFeesPeriod
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

	if settings.UtilityFeesPeriods != nil {
		s.periods = settings.UtilityFeesPeriods
	} else {
		// if they don't have any additional fees periods, we will need to find the
		// default for their utility provider
		fees, err := getUtilityRateFees(settings.UtilityRate, settings.UtilityRateOptions)
		if err != nil {
			return err
		}
		s.periods = fees
		s.name = settings.UtilityRate
	}

	return nil
}

func (s *SiteFees) applyFees(p types.Price) (types.Price, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// TODO: we might need to split out the price into multiple prices if the price
	// spans multiple different periods but for now we ignore that
	// we don't end up checking the price's end time since we assume that as long
	// as the periods start is after the price's start then it applies to that price
	// see the TODO above to understand when we might need to care about end times
	return types.ApplyUtilityFeesPeriods(p, s.periods)
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
