package utility

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// TOUUtilityImpl implements a generic Time-Of-Use utility provider using hardcoded rates.
type TOUUtilityImpl struct {
	mu      sync.Mutex
	siteID  string
	periods []types.UtilityAdditionalFeesPeriod
}

func (t *TOUUtilityImpl) ApplySettings(ctx context.Context, settings types.Settings) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if settings.UtilityRate == "example" {
		t.periods = []types.UtilityAdditionalFeesPeriod{
			{
				UtilityPeriod: types.UtilityPeriod{
					HourStart: 0,
					HourEnd:   6,
				},
				DollarsPerKWH: 0.01,
				Description:   "Night",
			},
			{
				UtilityPeriod: types.UtilityPeriod{
					HourStart: 6,
					HourEnd:   12,
				},
				DollarsPerKWH: 0.02,
				Description:   "Morning",
			},
			{
				UtilityPeriod: types.UtilityPeriod{
					HourStart: 12,
					HourEnd:   24,
				},
				DollarsPerKWH: 0.10,
				Description:   "Afternoon/Evening",
			},
		}
	} else {
		return fmt.Errorf("unsupported tou rate: %s", settings.UtilityRate)
	}
	return nil
}

func (t *TOUUtilityImpl) priceForTime(target time.Time) (types.Price, error) {
	t.mu.Lock()
	periods := t.periods
	t.mu.Unlock()

	p := types.Price{
		Provider:      "tou",
		TSStart:       target.Truncate(time.Hour),
		TSEnd:         target.Truncate(time.Hour).Add(time.Hour),
		DollarsPerKWH: 0,
	}

	for _, period := range periods {
		// Use period.Contains to check if the time is within the period
		contains, err := period.Contains(p.TSStart)
		if err != nil {
			return p, err
		}
		if contains {
			if period.GridAdditional {
				p.GridUseDollarsPerKWH += period.DollarsPerKWH
			} else {
				p.DollarsPerKWH += period.DollarsPerKWH
			}
		}
	}

	return p, nil
}

func (t *TOUUtilityImpl) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return t.priceForTime(time.Now())
}

func (t *TOUUtilityImpl) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().Truncate(time.Hour)
	var prices []types.Price

	// Generate prices for the next 48 hours
	for i := 1; i <= 48; i++ {
		target := now.Add(time.Duration(i) * time.Hour)
		p, err := t.priceForTime(target)
		if err != nil {
			return nil, err
		}
		prices = append(prices, p)
	}

	return prices, nil
}

func (t *TOUUtilityImpl) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var prices []types.Price

	current := start.Truncate(time.Hour)

	for current.Before(end) {
		p, err := t.priceForTime(current)
		if err != nil {
			return nil, err
		}

		if !p.TSStart.Before(start) && !p.TSEnd.After(end) {
			prices = append(prices, p)
		}

		current = current.Add(time.Hour)
	}

	return prices, nil
}
