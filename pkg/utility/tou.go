package utility

import (
	"context"
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
}

func (t *genericTOU) Name() string {
	return t.name
}

func (t *genericTOU) ApplySettings(ctx context.Context, settings types.Settings) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	fees, err := getUtilityRateFees(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	t.periods = fees
	t.name = settings.UtilityRate
	return nil
}

func (t *genericTOU) priceForTime(target time.Time) (types.Price, error) {
	t.mu.Lock()
	periods := t.periods
	t.mu.Unlock()

	// check if all of the periods have the same timezone and if they do then
	// apply that timezone to the type timestamps
	var timeZone string
	var lastLoc *time.Location
	for i := range periods {
		var tz string
		if periods[i].LocationPtr != nil {
			lastLoc = periods[i].LocationPtr
			tz = periods[i].LocationPtr.String()
		} else if periods[i].Location != "" {
			tz = periods[i].Location
		}
		if timeZone == "" {
			timeZone = tz
		} else if timeZone != tz {
			timeZone = ""
			lastLoc = nil
			break
		}
	}
	if timeZone != "" {
		target = target.In(lastLoc)
	}

	// Truncate to the start of the hour in the target's location
	start := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), 0, 0, 0, target.Location())

	p := types.Price{
		Provider:      "tou",
		TSStart:       start,
		TSEnd:         start.Add(time.Hour),
		DollarsPerKWH: 0,
	}

	return types.ApplyUtilityFeesPeriods(p, periods)
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
		if !p.TSStart.Before(start) && p.TSStart.Before(end) {
			prices = append(prices, p)
		}

		current = current.Add(time.Hour)
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

	DollarsPerKWH                 float64
	GenerationCreditDollarsPerKWH float64
	Description                   string
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
	OtherDollarsPerKWH                 float64
	OtherGenerationCreditDollarsPerKWH float64
	OtherDescription                   string

	// SeparateGenerationCredit is true if this period also contains solar
	// generation credit pricing. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	SeparateGenerationCredit bool

	// OnlySeparateGenerationCredit is true if this period is for solar
	// generation credits only. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	OnlySeparateGenerationCredit bool
}

func buildPeriods(loc string, simplified []touSimplifiedPeriod) []types.UtilityFeesPeriod {
	locPtr, err := time.LoadLocation(loc)
	if err != nil {
		panic(err)
	}
	var periods []types.UtilityFeesPeriod
	for _, s := range simplified {
		var ps []types.UtilityPeriod
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
				ps = append(ps, types.UtilityPeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, locPtr),
					End:         time.Date(s.Year+1, time.January, 1, 0, 0, 0, 0, locPtr),
					LocationPtr: locPtr,
				})
				ps = append(ps, types.UtilityPeriod{
					Start:       time.Date(s.Year, time.January, 1, 0, 0, 0, 0, locPtr),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, locPtr),
					LocationPtr: locPtr,
				})
			} else {
				ps = append(ps, types.UtilityPeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, locPtr),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, locPtr),
					LocationPtr: locPtr,
				})
			}
		} else if len(s.Months) > 0 {
			if s.Year == 0 {
				panic("Year must be set when Months are set")
			}
			for _, m := range s.Months {
				ps = append(ps, types.UtilityPeriod{
					Start:       time.Date(s.Year, m, 1, 0, 0, 0, 0, locPtr),
					End:         time.Date(s.Year, m+1, 1, 0, 0, 0, 0, locPtr),
					LocationPtr: locPtr,
				})
			}
		} else {
			ps = append(ps, types.UtilityPeriod{
				LocationPtr: locPtr,
			})
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

				dollarsPerKWH := hd.DollarsPerKWH
				if s.OnlySeparateGenerationCredit {
					dollarsPerKWH = hd.GenerationCreditDollarsPerKWH
				}

				if hd.GenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
					panic("GenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
				}

				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod:            p,
					DollarsPerKWH:            dollarsPerKWH,
					Description:              hd.Description,
					SeparateGenerationCredit: s.OnlySeparateGenerationCredit,
				})
				if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
					periods = append(periods, types.UtilityFeesPeriod{
						UtilityPeriod:            p,
						DollarsPerKWH:            hd.GenerationCreditDollarsPerKWH,
						Description:              hd.Description,
						SeparateGenerationCredit: true,
					})
				}
			}
		}

		if s.OtherDollarsPerKWH > 0 || s.OtherDescription != "" {
			if s.OtherGenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
				panic("OtherGenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
			}
			for i := range ps {
				// to group days with identical gaps, we'll map bitmasks to days containing
				// all of the existing covered periods
				type hourBitmask [24]bool
				maskToDays := make(map[hourBitmask][]time.Weekday)

				// loop over each day and check if it matches any of the hours and days
				for d := time.Sunday; d <= time.Saturday; d++ {
					var mask hourBitmask
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

						// if the day matches then add the hours to the mask
						if matches {
							if len(hd.Hours) == 0 {
								// if hours is empty, the period applies to all day
								// so mark everything
								for h := range 24 {
									mask[h] = true
								}
							} else {
								for _, hRange := range hd.Hours {
									for h := hRange.HourStart; h < hRange.HourEnd; h++ {
										mask[h] = true
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
					for h := range 24 {
						// if the hour is not in the mask, then it's a start of a gap
						if !mask[h] {
							if start == -1 {
								start = h
							}
						} else {
							// if the hour is in the mask, then it's the end of a gap
							if start != -1 {
								gaps = append(gaps, types.UtilityHourPeriod{HourStart: start, HourEnd: h})
								start = -1
							}
						}
					}
					// if start is still -1, then the gap is the whole day and we can leave
					// hours empty but otherwise we need to make the gap for the rest of the day
					if start != -1 {
						gaps = append(gaps, types.UtilityHourPeriod{HourStart: start, HourEnd: 24})
					}

					if len(gaps) > 0 {
						period := ps[i]
						period.Hours = gaps
						period.DaysOfTheWeek = dows

						dollarsPerKWH := s.OtherDollarsPerKWH
						if s.OnlySeparateGenerationCredit {
							dollarsPerKWH = s.OtherGenerationCreditDollarsPerKWH
						}

						periods = append(periods, types.UtilityFeesPeriod{
							UtilityPeriod:            period,
							DollarsPerKWH:            dollarsPerKWH,
							Description:              s.OtherDescription,
							SeparateGenerationCredit: s.OnlySeparateGenerationCredit,
						})
						if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
							periods = append(periods, types.UtilityFeesPeriod{
								UtilityPeriod:            period,
								DollarsPerKWH:            s.OtherGenerationCreditDollarsPerKWH,
								Description:              s.OtherDescription,
								SeparateGenerationCredit: true,
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
	return []types.UtilityProviderInfo{
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
								UtilityPeriod: types.UtilityPeriod{
									Hours: []types.UtilityHourPeriod{
										{HourStart: 0, HourEnd: 6},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.01,
								Description:   "Night",
							},
							{
								UtilityPeriod: types.UtilityPeriod{
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 12},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.02,
								Description:   "Morning",
							},
							{
								UtilityPeriod: types.UtilityPeriod{
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
			},
		},
		{
			ID:   "rutherford_electric",
			Name: "Rutherford Electric",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "rutherford_electric_tod",
					Name: "Time of Day Service",
					GetFees: getStaticGetFees(
						buildPeriods(
							"America/New_York",
							[]touSimplifiedPeriod{
								// May - September
								{
									Year:       2026,
									MonthStart: time.May,
									MonthEnd:   time.September,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 19},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "May - September On-Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "May - September Super Off-Peak",
										},
									},
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "May - September Off-Peak",
									SeparateGenerationCredit:           true,
								},
								// October and April
								{
									Year:   2026,
									Months: []time.Month{time.October, time.April},
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 7, HourEnd: 9},
												{HourStart: 13, HourEnd: 19},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "October and April On-Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "October and April Super Off-Peak",
										},
									},
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "October and April Off-Peak",
									SeparateGenerationCredit:           true,
								},
								// November - March
								{
									Year:       2026,
									MonthStart: time.November,
									MonthEnd:   time.March,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 7, HourEnd: 9},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "November - March On-Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "November - March Super Off-Peak",
										},
									},
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "November - March Off-Peak",
									SeparateGenerationCredit:           true,
								},
							},
						),
					),
				},
			},
		},
		// source: https://www.ladwp.com/account/customer-service/electric-rates/residential-rates
		{
			ID:   "ladwp",
			Name: "Los Angeles Department of Water & Power",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "ladwp_r1a",
					Name: "Standard Residential Rate",
					GetFees: getStaticGetFees(
						buildPeriods(
							"America/Los_Angeles",
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
								// TODO: get July onward rates once they're posted
							},
						),
					),
				},
				{
					ID:   "ladwp_r1b",
					Name: "Time-of-Use (TOU) Residential",
					GetFees: getStaticGetFees(
						buildPeriods(
							"America/Los_Angeles",
							[]touSimplifiedPeriod{
								{
									Year:       2026,
									MonthStart: time.January,
									MonthEnd:   time.March,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge High Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge Low Peak",
										},
									},
									OtherDollarsPerKWH: 0.25293,
									OtherDescription:   "January - March Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.April,
									MonthEnd:   time.May,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge High Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge Low Peak",
										},
									},
									OtherDollarsPerKWH: 0.24884,
									OtherDescription:   "April - May Total Consumption Charge Base",
								},
								{
									Year:   2026,
									Months: []time.Month{time.June},
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.33078,
											Description:   "June Total Consumption Charge High Peak",
										},
										{
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "June Total Consumption Charge Low Peak",
										},
									},
									OtherDollarsPerKWH: 0.24494,
									OtherDescription:   "June Total Consumption Charge Base",
								},
							},
						),
					),
				},
			},
		},
	}
}
