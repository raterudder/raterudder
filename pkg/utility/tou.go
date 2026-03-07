package utility

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// genericTOU implements a generic Time-Of-Use utility provider using hardcoded rates.
type genericTOU struct {
	mu       sync.Mutex
	siteID   string
	periods  []types.UtilityAdditionalFeesPeriod
	location *time.Location
}

func (t *genericTOU) ApplySettings(ctx context.Context, settings types.Settings) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if settings.UtilityRate == "example" {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			return err
		}
		t.location = loc

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

func (t *genericTOU) priceForTime(target time.Time) (types.Price, error) {
	t.mu.Lock()
	periods := t.periods
	loc := t.location
	t.mu.Unlock()

	if loc != nil {
		target = target.In(loc)
	}

	// Truncate to the start of the hour in the target's location
	start := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), 0, 0, 0, target.Location())

	p := types.Price{
		Provider:      "tou",
		TSStart:       start,
		TSEnd:         start.Add(time.Hour),
		DollarsPerKWH: 0,
	}

	for _, period := range periods {
		// If the period has no location, give it the default location to evaluate correctly
		if period.LocationPtr == nil && period.Location == "" && loc != nil {
			period.LocationPtr = loc
		}

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

func (t *genericTOU) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return t.priceForTime(time.Now())
}

func (t *genericTOU) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
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

func (t *genericTOU) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
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
