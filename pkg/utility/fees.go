package utility

import (
	"context"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// SiteComEd wraps BaseComEd to apply site-specific settings and fees.
type SiteFees struct {
	base     UtilityPrices
	mu       sync.RWMutex
	siteID   string
	provider string
	applied  bool
	periods  []types.UtilityFeesPeriod
	name     string
	options  types.UtilityRateOptions
	vppInfo  types.UtilityVPPInfo
}

// Name returns the name of the utility rate
func (s *SiteFees) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *SiteFees) settingsMatch(settings types.Settings) bool {
	s.mu.RLock()
	applied := s.applied
	provider := s.provider
	name := s.name
	options := s.options
	s.mu.RUnlock()

	if !applied || provider != settings.UtilityProvider || name != settings.UtilityRate {
		return false
	}
	if sm, ok := s.base.(settingsMatcher); ok {
		return sm.settingsMatch(settings)
	}
	return options == settings.UtilityRateOptions
}

// ApplySettings implements the Utility interface
func (s *SiteFees) ApplySettings(ctx context.Context, settings types.Settings) error {
	s.mu.RLock()
	unchanged := s.applied &&
		s.provider == settings.UtilityProvider &&
		s.name == settings.UtilityRate &&
		s.options == settings.UtilityRateOptions
	s.mu.RUnlock()
	if unchanged {
		return nil
	}

	fees, err := getUtilityRateFees(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	vppInfo, err := getUtilityVPPInfo(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.periods = fees
	s.provider = settings.UtilityProvider
	s.name = settings.UtilityRate
	s.options = settings.UtilityRateOptions
	s.vppInfo = vppInfo
	s.applied = true
	s.mu.Unlock()

	return nil
}

// GetVPPInfo implements the Utility interface.
func (s *SiteFees) GetVPPInfo(context.Context) (types.UtilityVPPInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vppInfo, nil
}

// GetPeriods implements the Utility interface, returning named import TOU periods when they cover all 24 hours of the day.
func (s *SiteFees) GetPeriods(context.Context) ([]types.TimePeriod, error) {
	s.mu.RLock()
	periods := s.periods
	s.mu.RUnlock()

	var res []types.TimePeriod
	for _, p := range periods {
		if !p.SeparateGenerationCredit && !p.SeparateBatteryGenerationCredit && !p.GridAdditional && p.Name != "" {
			res = append(res, p.TimePeriod)
		}
	}
	if len(res) == 0 {
		return nil, nil
	}
	if !namedPeriodsCoverDay(res, time.Now()) {
		return nil, nil
	}
	return res, nil
}

func namedPeriodsCoverDay(periods []types.TimePeriod, now time.Time) bool {
	loc := time.UTC
	var candidateLoc *time.Location
	first := true
	minuteSet := map[int]bool{0: true}
	for _, p := range periods {
		if p.LocationPtr != nil {
			if first {
				candidateLoc = p.LocationPtr
				first = false
			} else if p.LocationPtr.String() != candidateLoc.String() {
				candidateLoc = nil
			}
		}
		for _, h := range p.Hours {
			if h.MinuteStart >= 0 && h.MinuteStart < 60 {
				minuteSet[h.MinuteStart] = true
			}
			if h.MinuteEnd >= 0 && h.MinuteEnd < 60 {
				minuteSet[h.MinuteEnd] = true
			}
		}
	}
	if candidateLoc != nil {
		loc = candidateLoc
	}

	nowInLoc := now.In(loc)
	dayStart := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, loc)
	nextDay := dayStart.AddDate(0, 0, 1)
	for ts := dayStart; ts.Before(nextDay); ts = ts.Add(time.Hour) {
		for minute := range minuteSet {
			checkTS := ts.Add(time.Duration(minute) * time.Minute)
			if !checkTS.Before(nextDay) {
				continue
			}
			covered := false
			for i := range periods {
				if ok, _, err := periods[i].Contains(checkTS); err == nil && ok {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}
	return true
}

func (s *SiteFees) applyFees(p types.Price) (types.Price, error) {
	s.mu.RLock()
	periods := s.periods
	s.mu.RUnlock()
	// TODO: we might need to split out the price into multiple prices if the price
	// spans multiple different periods but for now we ignore that
	// we don't end up checking the price's end time since we assume that as long
	// as the periods start is after the price's start then it applies to that price
	// see the TODO above to understand when we might need to care about end times
	return types.ApplyUtilityFeesPeriods(p, periods)
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
