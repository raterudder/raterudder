package utility

// Tariff Audit Metadata
// - Provider(s): ladwp (Los Angeles Department of Water & Power (LADWP)), mvea (Mountain View Electric Association (MVEA)), centerpoint_indiana (CenterPoint Energy Indiana South (Indiana))
// - Official Sources:
//   - LADWP: https://www.ladwp.com/account/customer-service/electric-rates/residential-rates & https://www.ladwp.com/account/customer-service/electric-rates/residential-adjustment-billing-factors (Electric Rate Ordinance No. 184133 + Q4 2026 Adjustment Billing Factors eff. Oct 1, 2026)
//   - MVEA: https://mvea.coop/rates/ (Schedules 16.01, 16.02, 16.05, 16.06, 16.08, eff. October 1, 2026)
//   - CenterPoint Energy Indiana South: https://assets.centerpointenergy.com/api/public/content/centerpoint-energy-indiana-south-electric-tariff.pdf (I.U.R.C. No. E-14: Sheet No. 10 Rate RS & Sheet No. 53 Rider EDG)
// - Effective Window: 2026-01-01 through 2027-12-31 (see provider-level Tariff Audit Metadata blocks below for schedule-specific details)
// - Revision Cadence: Quarterly (LADWP Adjustment Billing Factors) / Annual (MVEA General Rules & Regulations, CenterPoint Indiana Rider EDG)
// - Next Audit Due: 2027-01-05 (LADWP Q1 2027 Residential Adjustment Billing Factors effective Jan 1, 2027)
// - Last Verified: 2026-11-20
// - Modeling Notes: Preserves all historical 2026 inline periods intact and appends verified 2027 volumetric schedules; fixed monthly/daily customer charges excluded; see individual inline provider blocks below for detailed modeling notes.

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// genericTOU implements a generic Time-Of-Use utility provider using hardcoded rates.
type genericTOU struct {
	mu      sync.Mutex
	siteID  string
	periods []types.UtilityFeesPeriod
	name    string
	options types.UtilityRateOptions
	vppInfo types.UtilityVPPInfo
	nowFunc func() time.Time
}

func (t *genericTOU) now() time.Time {
	if t.nowFunc != nil {
		return t.nowFunc()
	}
	return time.Now()
}

func (t *genericTOU) Name() string {
	return t.name
}

func (t *genericTOU) settingsMatch(settings types.Settings) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if settings.UtilityRate != "" && t.name != settings.UtilityRate {
		return false
	}
	return t.options == settings.UtilityRateOptions
}

func (t *genericTOU) ApplySettings(ctx context.Context, settings types.Settings) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	fees, err := getUtilityRateFees(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	vppInfo, err := getUtilityVPPInfo(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	t.periods = fees
	t.name = settings.UtilityRate
	t.options = settings.UtilityRateOptions
	t.vppInfo = vppInfo
	return nil
}

func (t *genericTOU) GetVPPInfo(context.Context) (types.UtilityVPPInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.vppInfo, nil
}

func (t *genericTOU) GetPeriods(ctx context.Context) ([]types.TimePeriod, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var res []types.TimePeriod
	for _, p := range t.periods {
		if !p.SeparateGenerationCredit && !p.SeparateBatteryGenerationCredit && !p.GridAdditional && p.Name != "" {
			res = append(res, p.TimePeriod)
		}
	}
	return res, nil
}

func (t *genericTOU) priceForTime(target time.Time) (types.Price, error) {
	t.mu.Lock()
	periods := t.periods
	t.mu.Unlock()

	// check if all of the periods have the same timezone and if they do then
	// apply that timezone to the type timestamps
	var lastLoc *time.Location
	for i := range periods {
		if periods[i].LocationPtr == nil {
			continue
		}
		if lastLoc == nil {
			lastLoc = periods[i].LocationPtr
		} else if lastLoc != periods[i].LocationPtr && lastLoc.String() != periods[i].LocationPtr.String() {
			lastLoc = nil
			break
		}
	}
	if lastLoc != nil {
		target = target.In(lastLoc)
	}

	// Truncate to the start of the elapsed hour in the target's location (preserving DST offset on repeated hours)
	start := target.Add(-time.Duration(target.Minute())*time.Minute - time.Duration(target.Second())*time.Second - time.Duration(target.Nanosecond()))
	end := start.Add(time.Hour)

	// check all periods for an overlapping price period
	for _, period := range periods {
		if ok, startEnd, _ := period.Contains(target); ok {
			if startEnd.Start.After(start) && startEnd.Start.Before(end) {
				start = startEnd.Start
			}
			if startEnd.End.Before(end) && startEnd.End.After(start) {
				end = startEnd.End
			}
		}
	}

	// we do not check for other periods, if it's 6:15pm and there's a new period
	// that starts at 6:30pm then we assume the current period is defined as ending
	// at 6:30pm. similarly if it's 6:45pm and a new period started at 6:30pm we
	// assume that the current period starts at 6:30pm.

	if !end.After(start) {
		return types.Price{}, fmt.Errorf("invalid price segment duration: start %v, end %v", start, end)
	}

	p := types.Price{
		Provider:      "tou",
		TSStart:       start,
		TSEnd:         end,
		DollarsPerKWH: 0,
	}

	return types.ApplyUtilityFeesPeriods(p, periods)
}

// GetCurrentPrice returns the current price.
func (t *genericTOU) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return t.priceForTime(t.now())
}

// GetFuturePrices returns the next 48 hours of prices.
func (t *genericTOU) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	nowTime := t.now()
	now := nowTime.Truncate(time.Hour)
	prices := make([]types.Price, 0, 48)

	// Generate prices from the current hour (including any remaining sub-hour segments after nowTime)
	// through the next 48 hours.
	for i := 0; i <= 48; i++ {
		target := now.Add(time.Duration(i) * time.Hour)
		p, err := t.priceForTime(target)
		if err != nil {
			return nil, err
		}
		if p.TSStart.After(nowTime) {
			prices = append(prices, p)
		}
		end := target.Add(time.Hour)
		// if the returned price doesn't cover the full hour, retrieve the rest
		for p.TSEnd.Before(end) {
			p2, err := t.priceForTime(p.TSEnd)
			if err != nil {
				return nil, err
			}
			// the new period should be strictly AFTER the previous period end but
			// since the start is inclusive and end is exclusive, it's okay if the
			// start equals the previous end
			if p2.TSStart.Before(p.TSEnd) || !p2.TSEnd.After(p.TSEnd) {
				return nil, fmt.Errorf("invalid next price segment duration: prev end %v, next start %v", p.TSEnd, p2.TSStart)
			}
			if p2.TSStart.After(nowTime) {
				prices = append(prices, p2)
			}
			p = p2
		}
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
		if p.TSEnd.After(start) && p.TSStart.Before(end) {
			prices = append(prices, p)
		}
		next := current.Add(time.Hour)
		if p.TSEnd.Before(next) && p.TSEnd.After(current) {
			current = p.TSEnd
		} else {
			current = next
		}
	}

	return prices, nil
}

func getStaticGetFees(periods []types.UtilityFeesPeriod) func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	return func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
		return periods, nil
	}
}

type touSimplifiedHoursAndDays struct {
	// Hours is the hours that the period applies to. Start is inclusive, end is exclusive.
	Hours []types.UtilityHourPeriod

	// DaysOfTheWeek is the days of the week that the period applies to, inclusive
	DaysOfTheWeek []time.Weekday
	Weekday       bool
	Weekend       bool

	DollarsPerKWH                        float64
	GenerationCreditDollarsPerKWH        float64
	BatteryGenerationCreditDollarsPerKWH float64
	GenerationAdjustmentDollarsPerKWH    float64
	Description                          string
	Name                                 string
}

type touSimplifiedPeriod struct {
	// Year is the year that the period applies to
	Year int

	// Months is the months that the period applies to, inclusive
	MonthStart time.Month
	MonthEnd   time.Month
	Months     []time.Month

	// HoursAndDays is a list of hours and days that the period applies to.
	HoursAndDays []touSimplifiedHoursAndDays

	// OtherDollarsPerKWH is the dollars per kwh for hours and days that are not
	// in HoursAndDays
	OtherDollarsPerKWH                        float64
	OtherGenerationCreditDollarsPerKWH        float64
	OtherBatteryGenerationCreditDollarsPerKWH float64
	OtherGenerationAdjustmentDollarsPerKWH    float64
	OtherDescription                          string
	OtherName                                 string

	// SeparateGenerationCredit is true if this period also contains solar
	// generation credit pricing. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	SeparateGenerationCredit bool

	// OnlySeparateGenerationCredit is true if this period is for solar
	// generation credits only. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	OnlySeparateGenerationCredit bool

	// SeparateBatteryGenerationCredit is true if this period also contains separate
	// battery export credit pricing. BatteryGenerationCreditDollarsPerKWH will be used.
	SeparateBatteryGenerationCredit bool

	// OnlySeparateBatteryGenerationCredit is true if this period is for battery
	// export credits only. BatteryGenerationCreditDollarsPerKWH will be used.
	OnlySeparateBatteryGenerationCredit bool

	// BuyAllSellAll marks prices in this period as dual-metered Buy-All / Sell-All.
	BuyAllSellAll bool

	// SpecificDates is a list of specific dates (formatted as YYYY-MM-DD) that this period applies to.
	SpecificDates []string

	// SpecificDatesNot means it applies to all dates except the dates in SpecificDates.
	SpecificDatesNot bool
}

func buildPeriods(loc *time.Location, simplified []touSimplifiedPeriod) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, s := range simplified {
		var ps []types.TimePeriod
		// first handle months
		if s.MonthStart != 0 || s.MonthEnd != 0 {
			if s.MonthStart == 0 || s.MonthEnd == 0 {
				panic("MonthStart and MonthEnd must be set together")
			}
			if s.Year == 0 {
				panic("Year must be set when MonthStart and MonthEnd are set")
			}
			// this means it wraps around and we need to make 2 periods
			if s.MonthStart > s.MonthEnd {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year+1, time.January, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, time.January, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			} else {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			}
		} else if len(s.Months) > 0 {
			if s.Year == 0 {
				panic("Year must be set when Months are set")
			}
			for _, m := range s.Months {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, m, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, m+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			}
		} else {
			ps = append(ps, types.TimePeriod{
				LocationPtr: loc,
			})
		}

		for i := range ps {
			ps[i].SpecificDates = s.SpecificDates
			ps[i].SpecificDatesNot = s.SpecificDatesNot
		}

		for _, hd := range s.HoursAndDays {
			for _, baseP := range ps {
				p := baseP
				if len(hd.Hours) > 0 {
					p.Hours = hd.Hours
				}
				if len(hd.DaysOfTheWeek) > 0 {
					p.DaysOfTheWeek = hd.DaysOfTheWeek
				} else if hd.Weekday {
					p.DaysOfTheWeek = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
				} else if hd.Weekend {
					p.DaysOfTheWeek = []time.Weekday{time.Saturday, time.Sunday}
				}

				if hd.Name != "" {
					p.Name = hd.Name
				}

				dollarsPerKWH := hd.DollarsPerKWH
				if s.OnlySeparateGenerationCredit {
					dollarsPerKWH = hd.GenerationCreditDollarsPerKWH
				} else if s.OnlySeparateBatteryGenerationCredit {
					dollarsPerKWH = hd.BatteryGenerationCreditDollarsPerKWH
				}

				if hd.GenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
					panic("GenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
				}
				if hd.BatteryGenerationCreditDollarsPerKWH > 0 && !s.SeparateBatteryGenerationCredit && !s.OnlySeparateBatteryGenerationCredit {
					panic("BatteryGenerationCreditDollarsPerKWH is set but SeparateBatteryGenerationCredit is false")
				}

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:                        p,
					DollarsPerKWH:                     dollarsPerKWH,
					Description:                       hd.Description,
					SeparateGenerationCredit:          s.OnlySeparateGenerationCredit,
					SeparateBatteryGenerationCredit:   s.OnlySeparateBatteryGenerationCredit,
					BuyAllSellAll:                     s.BuyAllSellAll && !s.OnlySeparateGenerationCredit && !s.OnlySeparateBatteryGenerationCredit,
					GenerationAdjustmentDollarsPerKWH: hd.GenerationAdjustmentDollarsPerKWH,
				})
				if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod:                        p,
						DollarsPerKWH:                     hd.GenerationCreditDollarsPerKWH,
						Description:                       hd.Description,
						SeparateGenerationCredit:          true,
						GenerationAdjustmentDollarsPerKWH: hd.GenerationAdjustmentDollarsPerKWH,
					})
				}
				if !s.OnlySeparateBatteryGenerationCredit && s.SeparateBatteryGenerationCredit {
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod:                      p,
						DollarsPerKWH:                   hd.BatteryGenerationCreditDollarsPerKWH,
						Description:                     hd.Description,
						SeparateBatteryGenerationCredit: true,
					})
				}
			}
		}

		if s.OtherDollarsPerKWH > 0 || s.OtherDescription != "" {
			if s.OtherGenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
				panic("OtherGenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
			}
			if s.OtherBatteryGenerationCreditDollarsPerKWH > 0 && !s.SeparateBatteryGenerationCredit && !s.OnlySeparateBatteryGenerationCredit {
				panic("OtherBatteryGenerationCreditDollarsPerKWH is set but SeparateBatteryGenerationCredit is false")
			}
			for i := range ps {
				// to group days with identical gaps, we'll map bitmasks to days containing
				// all of the existing covered periods
				type minuteBitmask [1440]bool
				maskToDays := make(map[minuteBitmask][]time.Weekday)

				// loop over each day and check if it matches any of the hours and days
				for d := time.Sunday; d <= time.Saturday; d++ {
					var mask minuteBitmask
					for _, hd := range s.HoursAndDays {
						matches := false
						if len(hd.DaysOfTheWeek) > 0 {
							matches = slices.Contains(hd.DaysOfTheWeek, d)
						} else if hd.Weekday {
							matches = d >= time.Monday && d <= time.Friday
						} else if hd.Weekend {
							matches = d == time.Sunday || d == time.Saturday
						} else {
							matches = true
						}

						// if the day matches then add the minutes to the mask
						if matches {
							if len(hd.Hours) == 0 {
								// if hours is empty, the period applies to all day
								// so mark everything
								for m := range 1440 {
									mask[m] = true
								}
							} else {
								for _, hRange := range hd.Hours {
									startMin := hRange.HourStart*60 + hRange.MinuteStart
									endMin := hRange.HourEnd*60 + hRange.MinuteEnd
									for m := startMin; m < endMin; m++ {
										mask[m] = true
									}
								}
							}
						}
					}
					maskToDays[mask] = append(maskToDays[mask], d)
				}

				for mask, dows := range maskToDays {
					// identify any gaps in this mask to make a new period
					var gaps []types.UtilityHourPeriod
					start := -1
					for m := range 1440 {
						// if the minute is not in the mask, then it's a start of a gap
						if !mask[m] {
							if start == -1 {
								start = m
							}
						} else {
							// if the minute is in the mask, then it's the end of a gap
							if start != -1 {
								gaps = append(gaps, types.UtilityHourPeriod{
									HourStart:   start / 60,
									MinuteStart: start % 60,
									HourEnd:     m / 60,
									MinuteEnd:   m % 60,
								})
								start = -1
							}
						}
					}
					// if start is not -1, then we need to make the gap for the rest of the day
					if start != -1 {
						gaps = append(gaps, types.UtilityHourPeriod{
							HourStart:   start / 60,
							MinuteStart: start % 60,
							HourEnd:     24,
							MinuteEnd:   0,
						})
					}

					if len(gaps) > 0 {
						period := ps[i]
						period.Hours = gaps
						period.DaysOfTheWeek = dows

						if s.OtherName != "" {
							period.Name = s.OtherName
						}

						dollarsPerKWH := s.OtherDollarsPerKWH
						if s.OnlySeparateGenerationCredit {
							dollarsPerKWH = s.OtherGenerationCreditDollarsPerKWH
						} else if s.OnlySeparateBatteryGenerationCredit {
							dollarsPerKWH = s.OtherBatteryGenerationCreditDollarsPerKWH
						}

						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod:                        period,
							DollarsPerKWH:                     dollarsPerKWH,
							Description:                       s.OtherDescription,
							SeparateGenerationCredit:          s.OnlySeparateGenerationCredit,
							SeparateBatteryGenerationCredit:   s.OnlySeparateBatteryGenerationCredit,
							BuyAllSellAll:                     s.BuyAllSellAll && !s.OnlySeparateGenerationCredit && !s.OnlySeparateBatteryGenerationCredit,
							GenerationAdjustmentDollarsPerKWH: s.OtherGenerationAdjustmentDollarsPerKWH,
						})
						if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
							periods = append(periods, types.UtilityFeesPeriod{
								TimePeriod:                        period,
								DollarsPerKWH:                     s.OtherGenerationCreditDollarsPerKWH,
								Description:                       s.OtherDescription,
								SeparateGenerationCredit:          true,
								GenerationAdjustmentDollarsPerKWH: s.OtherGenerationAdjustmentDollarsPerKWH,
							})
						}
						if !s.OnlySeparateBatteryGenerationCredit && s.SeparateBatteryGenerationCredit {
							periods = append(periods, types.UtilityFeesPeriod{
								TimePeriod:                      period,
								DollarsPerKWH:                   s.OtherBatteryGenerationCreditDollarsPerKWH,
								Description:                     s.OtherDescription,
								SeparateBatteryGenerationCredit: true,
							})
						}
					}
				}
			}
		}
	}
	return periods
}

func touUtilityInfo() []types.UtilityProviderInfo {
	novecOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "NOVEC net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return append([]types.UtilityProviderInfo{
		{
			ID:   "tou_example",
			Name: "TOU Example",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "tou_example_1",
					Name: "TOU Example 1",
					GetFees: getStaticGetFees(
						[]types.UtilityFeesPeriod{
							{
								TimePeriod: types.TimePeriod{
									Name: "Night",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 0, HourEnd: 6},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.01,
								Description:   "Night",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Morning",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 12},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.02,
								Description:   "Morning",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Afternoon/Evening",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 12, HourEnd: 24},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.10,
								Description:   "Afternoon/Evening",
							},
						},
					),
				},
				{
					ID:   "tou_example_2",
					Name: "TOU Example 2 (6:30 AM Split)",
					GetFees: getStaticGetFees(
						[]types.UtilityFeesPeriod{
							{
								TimePeriod: types.TimePeriod{
									Name: "Night",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 0, HourEnd: 6, MinuteEnd: 30},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.01,
								Description:   "Night",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Peak Morning",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, MinuteStart: 30, HourEnd: 12},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.15,
								Description:   "Peak Morning",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Afternoon/Evening",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 12, HourEnd: 24},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.05,
								Description:   "Afternoon/Evening",
							},
						},
					),
				},
			},
		},
		rutherfordElectricUtilityInfo(),
		// Tariff Audit Metadata
		// - Provider(s): ladwp (Los Angeles Department of Water & Power (LADWP))
		// - Official Sources:
		//   - https://www.ladwp.com/account/customer-service/electric-rates/residential-rates
		//   - https://www.ladwp.com/account/customer-service/electric-rates/residential-adjustment-billing-factors
		//   - LADWP Electric Rate Ordinance No. 184133 (Schedules R-1A Standard Residential & R-1B Time-of-Use Residential) + Q4 2026 Adjustment Billing Factors ($0.20150/kWh eff. Oct 1, 2026)
		// - Effective Window: 2026-01-01 through 2027-12-31 (Q4 2026 adjustment factors $0.20150/kWh carried forward into 2027 until Q1 2027 quarterly filing)
		// - Revision Cadence: Quarterly (Adjustment Billing Factors updated Jan 1, Apr 1, Jul 1, Oct 1)
		// - Next Audit Due: 2027-01-05 (Check Q1 2027 LADWP Residential Adjustment Billing Factors effective Jan 1, 2027)
		// - Last Verified: 2026-11-20
		// - Modeling Notes: Schedule R-1A models Zone 1 Tier 1 base rate ($0.07142/kWh) plus quarterly Adjustment Billing Factors ($0.20150/kWh in Q4 2026 = $0.27292/kWh); Schedule R-1B models High Peak, Low Peak, and Base TOU rates plus quarterly Adjustment Billing Factors; fixed monthly/minimum charges excluded; 1:1 net energy metering.
		{
			ID:   "ladwp",
			Name: "Los Angeles Department of Water & Power (LADWP)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "ladwp_r1a",
					Name: "Standard Residential Rate",
					GetFees: getStaticGetFees(
						buildPeriods(
							ptLocation,
							[]touSimplifiedPeriod{
								{
									Year:               2026,
									MonthStart:         time.January,
									MonthEnd:           time.March,
									OtherDollarsPerKWH: 0.24771,
									OtherDescription:   "January - March Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									MonthStart:         time.April,
									MonthEnd:           time.May,
									OtherDollarsPerKWH: 0.24362,
									OtherDescription:   "April - May Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									Months:             []time.Month{time.June},
									OtherDollarsPerKWH: 0.24362,
									OtherDescription:   "June Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									MonthStart:         time.July,
									MonthEnd:           time.September,
									OtherDollarsPerKWH: 0.26408,
									OtherDescription:   "July - September Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									MonthStart:         time.October,
									MonthEnd:           time.December,
									OtherDollarsPerKWH: 0.27292,
									OtherDescription:   "October - December Total Consumption Charge Tier 1",
								},
								{
									Year:               2027,
									MonthStart:         time.January,
									MonthEnd:           time.December,
									OtherDollarsPerKWH: 0.27292,
									OtherDescription:   "2027 Total Consumption Charge Tier 1",
								},
							},
						),
					),
				},
				{
					ID:   "ladwp_r1b",
					Name: "Time-of-Use (TOU) Residential",
					GetFees: getStaticGetFees(
						buildPeriods(
							ptLocation,
							[]touSimplifiedPeriod{
								{
									Year:       2026,
									MonthStart: time.January,
									MonthEnd:   time.March,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.25293,
									OtherDescription:   "January - March Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.April,
									MonthEnd:   time.May,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.24884,
									OtherDescription:   "April - May Total Consumption Charge Base",
								},
								{
									Year:   2026,
									Months: []time.Month{time.June},
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.33078,
											Description:   "June Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "June Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.24494,
									OtherDescription:   "June Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.July,
									MonthEnd:   time.September,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.35124,
											Description:   "July - September Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.29284,
											Description:   "July - September Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.26540,
									OtherDescription:   "July - September Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.October,
									MonthEnd:   time.December,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "October - December Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "October - December Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.27814,
									OtherDescription:   "October - December Total Consumption Charge Base",
								},
								{
									Year:       2027,
									MonthStart: time.January,
									MonthEnd:   time.May,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "January - May Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "January - May Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.27814,
									OtherDescription:   "January - May Total Consumption Charge Base",
								},
								{
									Year:       2027,
									MonthStart: time.June,
									MonthEnd:   time.September,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.36008,
											Description:   "June - September Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "June - September Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.27424,
									OtherDescription:   "June - September Total Consumption Charge Base",
								},
								{
									Year:       2027,
									MonthStart: time.October,
									MonthEnd:   time.December,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "October - December Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.30168,
											Description:   "October - December Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.27814,
									OtherDescription:   "October - December Total Consumption Charge Base",
								},
							},
						),
					),
				},
			},
		},
		// Tariff Audit Metadata
		// - Provider(s): mvea (Mountain View Electric Association (MVEA))
		// - Official Sources:
		//   - https://mvea.coop/rates/
		//   - https://mvea.coop/wp-content/uploads/2026/08/16.01-RESIDENTIAL-DEMAND-RATE.pdf
		//   - https://mvea.coop/wp-content/uploads/2026/08/16.02-RESIDENTIAL-TIME-OF-DAY-RATE.pdf
		//   - https://mvea.coop/wp-content/uploads/2026/08/16.05-RESIDENTIAL-PREPAID-RATE.pdf
		//   - https://mvea.coop/wp-content/uploads/2026/11/16.06-RESIDENTIAL-NET-METERING-DEMAND-RATE_RM-final-v2.pdf
		//   - https://mvea.coop/wp-content/uploads/2026/11/16.08-RESIDENTIAL-NET-METERING-TOD-RATE_rm_v2.pdf
		//   - MVEA General Rules and Regulations: 16.01 Residential Demand Rate, 16.02 Residential Time-of-Day Rate, 16.05 Residential Prepaid Rate, 16.06 Residential Net Metering Demand Rate, 16.08 Residential Net Metering TOD Rate (Effective October 1, 2026)
		// - Effective Window: 2026-01-01 through until superseded (Jan-Sep 2026 pre-revision baseline preserved; October 1, 2026 tariff revision applied from 2026-10-01 through 2027-12-31)
		// - Revision Cadence: Annual (Cooperative Board General Rules & Regulations rate updates)
		// - Next Audit Due: 2027-09-01 (Check MVEA General Rules & Regulations for annual October residential rate adjustments)
		// - Last Verified: 2026-11-20
		// - Modeling Notes: Preserves Jan-Sep 2026 pre-revision periods intact and applies MVEA's October 1, 2026 tariff revision starting 2026-10-01 through 2027-12-31: 16.01/16.06 volumetric energy rate $0.11269/kWh (excludes $39.50/mo Grid Access and $2.00/kW weekday demand charge; 1:1 net metering under 16.06) and 16.02/16.08 (legacy ID mvea_16_05; 16.05 is now Prepaid) volumetric TOD energy rates ($0.34500/kWh Mon-Fri 5-9 PM excl. 6 calendar holidays without weekend shifting, $0.08625/kWh Off-Peak; selectable netMeteringScheme between "none" $0.00/kWh export credit on 16.02/16.05 and "tou_net" 1:1 TOU period kWh credit bank under 16.08).
		{
			ID:   "mvea",
			Name: "Mountain View Electric Association (MVEA)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "mvea_16_01",
					Name: "Residential Demand Rate (16.01 / 16.06)",
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Rate 16.01 / 16.06: Flat Residential Demand / Net Metering Demand Rate
						// Jan-Sep 2026: $0.12475/kWh; Oct 1, 2026+ (eff. Oct 1, 2026 tariff): $0.11269/kWh
						return buildPeriods(mtLocation, []touSimplifiedPeriod{
							{
								Year:               2026,
								MonthStart:         time.January,
								MonthEnd:           time.September,
								OtherDollarsPerKWH: 0.12475,
								OtherDescription:   "MVEA 16.01 Flat Rate",
							},
							{
								Year:               2026,
								MonthStart:         time.October,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: 0.11269,
								OtherDescription:   "MVEA 16.01 Flat Rate",
							},
							{
								Year:               2027,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: 0.11269,
								OtherDescription:   "MVEA 16.01 Flat Rate",
							},
						}), nil
					},
				},
				{
					ID:   "mvea_16_05",
					Name: "Residential Time of Day Rate (16.02 / 16.08)",
					Options: []types.UtilityRateOption{
						{
							Field:       "netMeteringScheme",
							Name:        "Net Metering",
							Type:        types.UtilityOptionTypeSelect,
							Description: "Select 16.08 Net Metering TOD if enrolled in MVEA's Residential Net Metering Time-of-Day tariff (separate On-Peak and Off-Peak 1:1 kWh credit banks), or Standard TOD (16.02 / 16.05) if no export credit applies.",
							Default:     "none",
							Choices: []types.UtilityOptionChoice{
								{Value: "none", Name: "Standard TOD - No Export Credit (16.02 / 16.05)"},
								{Value: "tou_net", Name: "Net Metering TOD - 1:1 TOU Credit Bank (16.08)"},
							},
						},
					},
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Rate 16.02 / 16.08 (legacy ID mvea_16_05; 16.05 is now Residential Prepaid): Residential Time-of-Day Service Rate
						// Jan-Sep 2026: On-Peak Mon-Sat 5-9 PM $0.32371/kWh, Off-Peak $0.08346/kWh
						// Oct 1, 2026+ (eff. Oct 1, 2026 tariff): On-Peak Mon-Fri 5-9 PM (excl. 6 calendar holidays) $0.34500/kWh, Off-Peak $0.08625/kWh
						peakHours := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}}
						mveaHolidays := func(year int) []string {
							return formatHolidays([]time.Time{
								newYearsDay(year),
								memorialDay(year),
								independenceDay(year),
								laborDay(year),
								thanksgivingDay(year),
								christmasDay(year),
							}, year)
						}
						holidays2026 := mveaHolidays(2026)
						holidays2027 := mveaHolidays(2027)
						periods := []touSimplifiedPeriod{
							{
								Year:       2026,
								MonthStart: time.January,
								MonthEnd:   time.September,
								HoursAndDays: []touSimplifiedHoursAndDays{
									{
										Name:  "On-Peak",
										Hours: peakHours,
										DaysOfTheWeek: []time.Weekday{
											time.Monday,
											time.Tuesday,
											time.Wednesday,
											time.Thursday,
											time.Friday,
											time.Saturday,
										},
										DollarsPerKWH: 0.32371,
										Description:   "MVEA 16.05 On-Peak",
									},
								},
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08346,
								OtherDescription:   "MVEA 16.05 Off-Peak",
							},
							{
								Year:               2026,
								MonthStart:         time.October,
								MonthEnd:           time.December,
								SpecificDates:      holidays2026,
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08625,
								OtherDescription:   "MVEA 16.02 Holiday Off-Peak",
							},
							{
								Year:             2026,
								MonthStart:       time.October,
								MonthEnd:         time.December,
								SpecificDates:    holidays2026,
								SpecificDatesNot: true,
								HoursAndDays: []touSimplifiedHoursAndDays{
									{
										Name:          "On-Peak",
										Hours:         peakHours,
										Weekday:       true,
										DollarsPerKWH: 0.34500,
										Description:   "MVEA 16.02 On-Peak",
									},
								},
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08625,
								OtherDescription:   "MVEA 16.02 Off-Peak",
							},
							{
								Year:               2027,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								SpecificDates:      holidays2027,
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08625,
								OtherDescription:   "MVEA 16.02 Holiday Off-Peak",
							},
							{
								Year:             2027,
								MonthStart:       time.January,
								MonthEnd:         time.December,
								SpecificDates:    holidays2027,
								SpecificDatesNot: true,
								HoursAndDays: []touSimplifiedHoursAndDays{
									{
										Name:          "On-Peak",
										Hours:         peakHours,
										Weekday:       true,
										DollarsPerKWH: 0.34500,
										Description:   "MVEA 16.02 On-Peak",
									},
								},
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08625,
								OtherDescription:   "MVEA 16.02 Off-Peak",
							},
						}

						// Export credits:
						// Prior to October 1, 2026, legacy Schedule 16.05 TOD had no export credit ($0.00/kWh).
						// Starting October 1, 2026, Standard TOD (16.02 / 16.05) still has $0.00/kWh export credit,
						// while Schedule 16.08 (Residential Net Metering TOD, netMeteringScheme == "tou_net")
						// banks On-Peak and Off-Peak kWh exports separately to offset consumption in the same TOU period.
						periods = append(periods, touSimplifiedPeriod{
							Year:                               2026,
							MonthStart:                         time.January,
							MonthEnd:                           time.September,
							OnlySeparateGenerationCredit:       true,
							OtherGenerationCreditDollarsPerKWH: 0.00,
							OtherDescription:                   "MVEA 16.05 Export Credit (No Export Credit allowed on TOD)",
						})
						if opts.NetMeteringScheme != "tou_net" {
							periods = append(periods,
								touSimplifiedPeriod{
									Year:                               2026,
									MonthStart:                         time.October,
									MonthEnd:                           time.December,
									OnlySeparateGenerationCredit:       true,
									OtherGenerationCreditDollarsPerKWH: 0.00,
									OtherDescription:                   "MVEA 16.02/16.05 Export Credit (No Export Credit allowed on TOD)",
								},
								touSimplifiedPeriod{
									Year:                               2027,
									MonthStart:                         time.January,
									MonthEnd:                           time.December,
									OnlySeparateGenerationCredit:       true,
									OtherGenerationCreditDollarsPerKWH: 0.00,
									OtherDescription:                   "MVEA 16.02/16.05 Export Credit (No Export Credit allowed on TOD)",
								},
							)
						}
						return buildPeriods(mtLocation, periods), nil
					},
				},
			},
		},
		{
			ID:   "novec",
			Name: "Northern Virginia Electric Cooperative (NOVEC)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "novec_r1",
					Name:    "Schedule R-1 (Residential Service)",
					Options: novecOptions,
					GetFees: getStaticGetFees(
						buildPeriods(
							etLocation,
							[]touSimplifiedPeriod{
								{
									OtherDollarsPerKWH: 0.11079,
									OtherDescription:   "NOVEC Schedule R-1 Residential Service",
								},
							},
						),
					),
				},
				{
					ID:      "novec_r1_ev",
					Name:    "Schedule R-1-EV (Residential Electric Vehicle Service)",
					Options: novecOptions,
					GetFees: getStaticGetFees(
						buildPeriods(
							etLocation,
							[]touSimplifiedPeriod{
								{
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name:          "On-Peak",
											Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 23}},
											DollarsPerKWH: 0.12694,
											Description:   "NOVEC Schedule R-1-EV On-Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.07320,
									OtherDescription:   "NOVEC Schedule R-1-EV Off-Peak",
								},
							},
						),
					),
				},
			},
		},
		// Tariff Audit Metadata
		// - Provider(s): centerpoint_indiana (CenterPoint Energy Indiana South (Indiana))
		// - Official Sources:
		//   - https://www.centerpointenergy.com/en-us/our-services/rates-tariffs/indiana
		//   - https://assets.centerpointenergy.com/api/public/content/centerpoint-energy-indiana-south-electric-tariff.pdf
		//   - Tariff for Electric Service I.U.R.C. No. E-14: Sheet No. 10 Rate RS - Residential Service (3rd Rev. Page 1 eff. March 5, 2026; 1st Rev. Page 2 eff. July 1, 2026) and Sheet No. 53 Rider EDG - Excess Distributed Generation Rider (1st Rev. Page 2 eff. March 1, 2026)
		// - Effective Window: 2026-03-05 through until superseded (modeled for 2026-01-01 through 2027-12-31)
		// - Revision Cadence: Annual (Rider EDG Marginal DG Price updated annually ~March 1; Rate RS base charges per IURC general rate orders; Appendices A-L tracking riders excluded)
		// - Next Audit Due: 2027-03-05 (Check IURC annual Rider EDG Marginal DG Price update and Rate RS tariff sheet)
		// - Last Verified: 2026-11-20
		// - Modeling Notes: Models Rate RS Standard Customers base volumetric sum ($0.157230 Energy + $0.041220 Base Fuel + $0.001692 Variable Production = $0.200142/kWh); excludes $11.00/mo Customer Facilities Charge and variable Appendices A-L adjustment riders; Rider EDG outflow credit modeled at $0.05561/kWh (or legacy 1:1 net metering via netMeteringScheme).
		{
			ID:   "centerpoint_indiana",
			Name: "CenterPoint Energy Indiana South (Indiana)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "centerpoint_indiana_rs",
					Name: "Rate RS - Residential Service (Indiana)",
					Options: []types.UtilityRateOption{
						{
							Field:       "netMeteringScheme",
							Name:        "Export Scheme / Rider",
							Type:        types.UtilityOptionTypeSelect,
							Description: "Select your solar export scheme.",
							Choices: []types.UtilityOptionChoice{
								{Value: "edg", Name: "Excess Distributed Generation (EDG)"},
								{Value: "net", Name: "Standard Net Metering (1:1)"},
							},
							Default: "edg",
						},
					},
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Standard Customers:
						// Energy Charge: $0.157230
						// Fuel Charge: $0.041220
						// Variable Production Charge: $0.001692
						// Total DollarsPerKWH = $0.200142
						totalDollarsPerKWH := 0.157230 + 0.041220 + 0.001692
						simplified := []touSimplifiedPeriod{
							{
								Year:               2026,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: totalDollarsPerKWH,
								OtherDescription:   "CenterPoint Indiana Rate RS Standard Charge",
							},
							{
								Year:               2027,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: totalDollarsPerKWH,
								OtherDescription:   "CenterPoint Indiana Rate RS Standard Charge",
							},
						}

						periods := buildPeriods(ctLocation, simplified)

						// Export Credits
						scheme := opts.NetMeteringScheme
						if scheme == "" {
							scheme = "edg"
						}

						if scheme == "edg" {
							for _, year := range []int{2026, 2027} {
								periods = append(periods, types.UtilityFeesPeriod{
									TimePeriod: types.TimePeriod{
										Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
										End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
										LocationPtr: ctLocation,
									},
									DollarsPerKWH:            0.05561,
									SeparateGenerationCredit: true,
									Description:              "Excess Distributed Generation Credit",
								})
							}
						}

						return periods, nil
					},
				},
			},
		},
		gpUtilityInfo(),
		xcelUtilityInfo(),
		rockyUtilityInfo(),
		srpUtilityInfo(),
		fplUtilityInfo(),
		dlcUtilityInfo(),
		sdgeUtilityInfo(),
		apsUtilityInfo(),
		tepUtilityInfo(),
		dominionUtilityInfo(),
		bgeUtilityInfo(),
		jeaUtilityInfo(),
		weUtilityInfo(),
		bwpUtilityInfo(),
		eversourceUtilityInfo(),
		pecoUtilityInfo(),
		metedUtilityInfo(),
		sawneeUtilityInfo(),
		waltonUtilityInfo(),
		carteretCravenUtilityInfo(),
		idahoUtilityInfo(),
		psegliUtilityInfo(),
		pseUtilityInfo(),
		secoUtilityInfo(),
		engieUtilityInfo(),
		originUtilityInfo(),
		globirdUtilityInfo(),
		octopusUtilityInfo(),
		epbUtilityInfo(),
		pepcoDCUtilityInfo(),
		pgeUtilityInfo(),
		sceUtilityInfo(),
		pgEUtilityInfo(),
		teslaElectricUtilityInfo(),
		greenMountainUtilityInfo(),
		directEnergyUtilityInfo(),
		austinEnergyUtilityInfo(),
		smudUtilityInfo(),
	},
		append(hawaiiUtilityInfo(), dukeUtilityInfo()...)...,
	)
}
