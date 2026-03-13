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
	periods  []types.UtilityFeesPeriod
	location *time.Location
	name     string
}

func (t *genericTOU) Name() string {
	return t.name
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

		t.periods = []types.UtilityFeesPeriod{
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
		t.name = "example"
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
			switch {
			case period.GenerationCredit:
				p.GenerationCreditDollarsPerKWH += period.DollarsPerKWH
				if period.SeparateGenerationCredit {
					p.SeparateGenerationCredit = true
				}
			case period.GridAdditional:
				p.GridUseDollarsPerKWH += period.DollarsPerKWH
			default:
				p.DollarsPerKWH += period.DollarsPerKWH
			}
		}
	}

	return p, nil
}

// GetCurrentPrice returns the current price.
func (t *genericTOU) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return t.priceForTime(time.Now())
}

// GetFuturePrices returns the next 48 hours of prices.
func (t *genericTOU) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().Truncate(time.Hour)
	prices := make([]types.Price, 0, 48)

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

// GetConfirmedPrices returns all the prices for a specific time range since TOU
// prices are always the same for a given hour.
func (t *genericTOU) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var prices []types.Price

	current := start.Truncate(time.Hour)

	for current.Before(end) {
		p, err := t.priceForTime(current)
		if err != nil {
			return nil, err
		}

		// even if the end time goes beyond the end time, we still want to include it
		// since it will cover the time period between start and end
		if !p.TSStart.Before(start) && !p.TSStart.After(end) {
			prices = append(prices, p)
		}

		current = current.Add(time.Hour)
	}

	return prices, nil
}
