package controller

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// EVMinThresholdKW is the minimum household load required to consider full-hour Level 2 EV charging active.
	EVMinThresholdKW = 4.8
	// EVMinStepKW is the minimum step increase above baseline load required to confirm full-hour Level 2 EV charging.
	EVMinStepKW = 3.5

	// evShoulderMinThresholdKW is the minimum household load required to flag a partial-charge shoulder hour
	// (e.g. the first or last hour of an EV charging session where the charger ran for 25-40 minutes)
	// when immediately adjacent to a confirmed full-hour (>= 4.8 kW) EV spike.
	evShoulderMinThresholdKW = 3.2
	// evShoulderMinStepKW is the minimum step increase above baseline reference load required to flag
	// an adjacent partial-charge shoulder hour.
	evShoulderMinStepKW = 2.0
	// evShoulderMultiple is the minimum multiple above the baseline reference load required to flag
	// an adjacent partial-charge shoulder hour.
	evShoulderMultiple = 2.0

	// hourlyOutlierBaselinePercentile is the percentile (30th) of historical positive loads at a given
	// hour of day used as the cross-day baseline reference for automatic EV and load-spike filtering.
	// Because an EV charged up to 4 days a week (~57% of days) still leaves >= 43% of historical days
	// at normal household load, the 30th percentile reliably anchors to a non-EV day while still selecting
	// index 1 (the middle element) when history has only 3 days (round(2 * 0.30) = 1).
	hourlyOutlierBaselinePercentile = 0.30

	// hourlyOutlierSameDayHVACPercentile is the intra-day hourly load percentile (65th, i.e. the 9th highest
	// hour of a 24-hour day) used to floor the outlier reference on complete days (>= 18 hours).
	// On hot summer days where central AC runs across the day (9+ hours), the 65th percentile rises with
	// the HVAC load so legitimate afternoon AC peaks are not mistaken for EV spikes. Conversely, a 1-6 hour
	// daytime or nighttime EV charging session leaves the 65th percentile at normal household load.
	hourlyOutlierSameDayHVACPercentile = 0.65

	// hourlyOutlierMultiple is the minimum multiple above the baseline reference load required (along with
	// EVMinThresholdKW and EVMinStepKW) to classify an hourly reading as an intermittent EV charge or
	// unusual one-off load spike.
	hourlyOutlierMultiple = 2.5

	// hourlyOutlierFloorKWH is the minimum reference load floor (in kWh) used when computing the relative
	// outlier threshold (max(refLoad, hourlyOutlierFloorKWH) * hourlyOutlierMultiple).
	hourlyOutlierFloorKWH = 0.5

	// sustainedDaytimeHVACMaxKW is the maximum daytime hourly load (in kW) that can be included in the
	// recent 6-hour baseline when sustained daytime HVAC (>= 3 hours >= 3.8 kW) is active, preventing
	// continuous 4.8-7.5 kW afternoon AC from being stripped when transitioning into 20:00-21:00 while
	// still excluding full-power daytime EV charging spikes (> 7.5 kW).
	sustainedDaytimeHVACMaxKW = 7.5

	// elevatedRecentBaselineThresholdKW is the recent 1-6 hour median baseline threshold (in kW) above
	// which calculateRecentBaseline also checks the 30th percentile of the same hour of day across prior
	// days, ensuring warm-afternoon AC before an overnight EV session does not mask nighttime EV charging.
	elevatedRecentBaselineThresholdKW = 2.5
)

type dayHourKey struct {
	date string
	hour int
}

// isNighttimeEVHour returns true if the local hour falls within the nighttime EV charging window
// (20:00 to 06:59, ending at 07:00 AM). We exclude 07:00+ because morning household activity and
// solar generation begin ramping after 07:00 AM.
func isNighttimeEVHour(hr int) bool {
	return hr >= 20 || hr <= 6
}

// isEVStandbyEligible returns true if the site has opted into EV charging standby for ts:
// either EVChargingStandby is enabled and ts falls within the nighttime window (20:00-06:59 local time),
// or ts falls within a legacy configured EVChargingPeriods window.
// Real-time EV standby intentionally does not trigger during daytime hours (07:00-19:59) when
// using solar to charge the EV is assumed to be desired.
func isEVStandbyEligible(settings types.Settings, ts time.Time) bool {
	localTS := ts
	if (localTS.Location() == nil || localTS.Location() == time.UTC) && settings.Location != nil && settings.Location.TimeZone != "" {
		if loc, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			localTS = localTS.In(loc)
		}
	}
	if settings.EVChargingStandby && isNighttimeEVHour(localTS.Hour()) {
		return true
	}
	for _, evp := range settings.EVChargingPeriods {
		inEVPeriod, _, err := evp.Contains(localTS)
		if err == nil && inEVPeriod {
			return true
		}
	}
	return false
}

// detectEVCharging checks if the current instantaneous household load reflects active EV charging
// by evaluating whether the load is >= 4.8 kW and represents a step increase of >= 3.5 kW above
// recent baseline household load.
func detectEVCharging(ctx context.Context, currentLoad float64, history []types.EnergyStats) (bool, float64) {
	if currentLoad < EVMinThresholdKW {
		return false, 0
	}

	baselineKW := calculateRecentBaseline(ctx, history)
	stepKW := currentLoad - baselineKW
	isEV := stepKW >= EVMinStepKW

	log.Ctx(ctx).DebugContext(ctx, "evaluating ev charging detection",
		slog.Float64("currentLoad", currentLoad),
		slog.Float64("baselineKW", baselineKW),
		slog.Float64("stepKW", stepKW),
		slog.Float64("minStepKW", EVMinStepKW),
		slog.Bool("isEV", isEV),
	)

	return isEV, stepKW
}

// calculateRecentBaseline computes the non-EV baseline from recent hourly history.
// It inspects the preceding 1-6 hours, filtering out elevated hours to find the true household base load.
// When multi-day history is available and the recent 1-6 hour median is elevated (>= 2.5 kW, e.g. from
// late-afternoon/early-evening central AC before an overnight EV session starts), it also checks the same
// hour of day on prior days and takes the lower baseline so evening HVAC does not mask nighttime EV charging.
func calculateRecentBaseline(ctx context.Context, history []types.EnergyStats) float64 {
	if len(history) == 0 {
		return 1.0 // Default baseline assumption when no history is present
	}

	// Sort history descending by timestamp (most recent first)
	hCopy := make([]types.EnergyStats, len(history))
	copy(hCopy, history)
	sort.Slice(hCopy, func(i, j int) bool {
		return hCopy[i].TSHourStart.After(hCopy[j].TSHourStart)
	})

	// Check whether daytime/early-evening hours (07:00-19:59) in the recent 6-hour window reflect
	// sustained afternoon HVAC (>= 3 hours >= 3.8 kW). If so, do not strip >= 4.8 kW daytime hours
	// when transitioning into 20:00-21:00, preventing sustained late-afternoon AC from looking like
	// a fresh step increase at 20:00.
	daytimeHighCount := 0
	for i := 0; i < len(hCopy) && i < 6; i++ {
		hr := hCopy[i].TSHourStart.Hour()
		if !isNighttimeEVHour(hr) && hCopy[i].HomeKWH >= 3.8 {
			daytimeHighCount++
		}
	}
	sustainedDaytimeHVAC := daytimeHighCount >= 3
	if sustainedDaytimeHVAC {
		log.Ctx(ctx).DebugContext(
			ctx,
			"sustained daytime HVAC detected in recent baseline window, retaining daytime loads up to cap",
			slog.Int("daytimeHighCount", daytimeHighCount),
			slog.Float64("sustainedDaytimeHVACMaxKW", sustainedDaytimeHVACMaxKW),
		)
	}

	var baselines []float64
	for i := 0; i < len(hCopy) && i < 6; i++ {
		hr := hCopy[i].TSHourStart.Hour()
		// If an hour was below the EV threshold (or is part of sustained daytime HVAC <= 7.5 kW),
		// it is a candidate baseline.
		if hCopy[i].HomeKWH > 0.05 && (hCopy[i].HomeKWH < EVMinThresholdKW || (sustainedDaytimeHVAC && !isNighttimeEVHour(hr) && hCopy[i].HomeKWH <= sustainedDaytimeHVACMaxKW)) {
			baselines = append(baselines, hCopy[i].HomeKWH)
		}
	}

	var recentMedian float64
	if len(baselines) == 0 {
		foundLookback := false
		// If all recent 6 hours were elevated, look further back (up to 24 hours) for the first hour < EVMinThresholdKW
		for i := 6; i < len(hCopy) && i < 24; i++ {
			if hCopy[i].HomeKWH < EVMinThresholdKW && hCopy[i].HomeKWH > 0.05 {
				recentMedian = hCopy[i].HomeKWH
				foundLookback = true
				break
			}
		}
		if !foundLookback {
			// If still none, look for the minimum load in recent history
			minVal := hCopy[0].HomeKWH
			for i := 1; i < len(hCopy) && i < 24; i++ {
				if hCopy[i].HomeKWH < minVal {
					minVal = hCopy[i].HomeKWH
				}
			}
			recentMedian = minVal
		}
		log.Ctx(ctx).DebugContext(
			ctx,
			"all recent 6 hours were elevated in calculateRecentBaseline, falling back to extended lookback",
			slog.Bool("foundLookbackUnderThreshold", foundLookback),
			slog.Float64("recentMedianKW", recentMedian),
			slog.Int("historyLen", len(hCopy)),
		)
	} else {
		sort.Float64s(baselines)
		recentMedian = baselines[len(baselines)/2]
	}

	// If multi-day history is present and recent hours were elevated (>= 2.5 kW, e.g. late-afternoon AC
	// before an overnight EV session), check the 30th percentile of the target hour across prior days.
	// Using the 30th percentile of all readings at that hour (rather than cherry-picking < 4.8 kW readings)
	// rescues overnight EV sessions after warm afternoons without creating false steps on homes whose
	// normal evening load is >= 4.8 kW.
	if !sustainedDaytimeHVAC && recentMedian >= elevatedRecentBaselineThresholdKW && len(hCopy) > 6 && !hCopy[0].TSHourStart.IsZero() {
		refLoc := hCopy[0].TSHourStart.Location()
		targetHr := (hCopy[0].TSHourStart.Hour() + 1) % 24
		var sameHourLoads []float64
		for i := 6; i < len(hCopy); i++ {
			hr := hCopy[i].TSHourStart.In(refLoc).Hour()
			if hr == targetHr && hCopy[i].HomeKWH > 0.05 {
				sameHourLoads = append(sameHourLoads, hCopy[i].HomeKWH)
			}
		}
		if len(sameHourLoads) >= 3 {
			sort.Float64s(sameHourLoads)
			sameHourP30 := sameHourLoads[int(math.Round(float64(len(sameHourLoads)-1)*hourlyOutlierBaselinePercentile))]
			log.Ctx(ctx).DebugContext(
				ctx,
				"recent baseline hours were elevated, comparing against same-hour historical 30th percentile",
				slog.Int("targetHour", targetHr),
				slog.Float64("recentMedianKW", recentMedian),
				slog.Float64("sameHourP30KW", sameHourP30),
				slog.Int("sameHourSampleCount", len(sameHourLoads)),
			)
			if sameHourP30 < recentMedian {
				return sameHourP30
			}
		}
	}

	return recentMedian
}

// detectIntermittentEVAndLoadSpikes performs automatic, zero-config detection of intermittent Level 2 EV
// charging (e.g. afternoon solar EV charging 1-4 days/week, or nighttime charging 1-6 nights/week when
// EVChargingPeriods is not configured), user-configured EV standby periods, and large unusual one-off loads.
//
// Why we focus on Level 2 EV charging (and not Level 1):
//   - Level 1 chargers (12A on a 120V/15A circuit) draw only ~1.4 kW, which overlaps completely with normal
//     daytime household loads (central AC, heat pumps, pool pumps, electric dryers, cooking). Attempting to
//     filter ~1.4 kW during the day would strip genuine HVAC and appliance usage. Moreover, a 1.4 kW daytime
//     load is small enough to be absorbed by normal solar variance without materially distorting battery plans.
//   - Level 2 chargers in residential homes are most commonly installed on 40A breakers (32A continuous = 7.7 kW)
//     or 50A breakers (40A continuous = 9.6 kW), with some on 30A (5.8 kW) or 60A (11.5 kW) circuits.
//   - Our thresholds (EVMinThresholdKW = 4.8 kW total load, EVMinStepKW = 3.5 kW step above refLoad, and
//     > 2.5x refLoad) comfortably detect both 7.7 kW (40A) and 9.6 kW (50A) Level 2 chargers (as well as
//     5.8 kW and 11.5 kW units) while staying above standard household appliances (< 4.8 kW, such as 3.0 kW dryers).
//
// Why each hour is evaluated independently (with a second pass for partial-charge shoulder hours):
//   - Homeowners may plug in for a full multi-hour charge (e.g. 3-5 hours) or only top off for 1 hour (or even
//     40-45 minutes within a single hour). Evaluating each hour independently against its cross-day and intra-day
//     reference load catches both brief 1-hour top-offs and multi-hour sessions (up to 6-8 hours) alike:
//     1. Cross-day baseline (allRefByHour[hr] at 30th percentile, plus non-EV overnight anchoring): Even if an EV
//     charges 4 days/week (~57% of days) at the same hours, >= 43% of days do not have EV charging, so the 30th
//     percentile anchors to non-EV load. For high-frequency nighttime chargers (5-6 nights/week, 70-85% of nights)
//     where the 30th percentile at 00:00-04:00 is pulled into the EV tier (>= 3.0 kW) despite the home having a
//     quiet overnight baseline (< 2.5 kW) at other overnight hours, we anchor allRefByHour[hr] to the median of
//     the non-EV (< 4.8 kW) readings at that hour.
//     2. Intra-day HVAC protection (dayP65, and dayP75 on hot >= 28°C hours, applied during daytime/evening hours 09:00-22:00):
//     On complete days (>= 18 hours), sustained central AC raises the day's 65th/75th percentile (computed after
//     excluding obvious cross-day EV spikes), whereas a 1-6 hour EV session leaves those percentiles at normal
//     household load. Overnight hours (00:00-08:00, 23:00) do not use daytime dayP65/dayP75 so afternoon AC on
//     warm days never masks unconfigured overnight Level 2 EV charging or inflates overnight baseline replacements.
//     3. Temperature-similar historical reference: When hourly weather is available, we also check the 30th
//     percentile of historical loads at hour hr within ±3.0°C (tempSimilarityScale) so hot-afternoon AC peaks
//     that repeat on similarly hot days are never mistaken for EV spikes.
//     4. Partial-charge shoulder hours (Pass 2): When an EV starts or finishes charging mid-hour (e.g. 25-40 minutes
//     at 7.7 kW = 3.2-4.7 kWh), that boundary hour falls just below the 4.8 kW full-hour threshold. In a second pass,
//     any hour immediately adjacent to a confirmed Pass 1 EV spike that exceeds 3.2 kWh, >= 2.0 kWh step above refLoad,
//     and > 2.0x refLoad is also flagged.
func detectIntermittentEVAndLoadSpikes(
	ctx context.Context,
	dayMap map[string]*dayPoints,
	weatherByHour map[time.Time]float64,
	settings types.Settings,
	standbyLoad float64,
) (map[dayHourKey]bool, map[dayHourKey]float64, map[int]float64) {
	ignoredOutlierHours := make(map[dayHourKey]bool)
	outlierRefLoad := make(map[dayHourKey]float64)
	hourBaselineRef := make(map[int]float64)

	type hourLoadWithTemp struct {
		load    float64
		tempC   float64
		hasTemp bool
	}
	var allHourLoads [24][]float64
	var allHourNonEVLoads [24][]float64
	var allHourWithTemp [24][]hourLoadWithTemp
	var hasConfiguredEVAtHour [24]bool

	for _, d := range dayMap {
		for _, pt := range d.points {
			if pt.HomeKWH <= 0.0 {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr >= 0 && hr < 24 {
				allHourLoads[hr] = append(allHourLoads[hr], pt.HomeKWH)
				if pt.HomeKWH < EVMinThresholdKW {
					allHourNonEVLoads[hr] = append(allHourNonEVLoads[hr], pt.HomeKWH)
				}
				if isEVStandbyEligible(settings, pt.TSHourStart) {
					hasConfiguredEVAtHour[hr] = true
				}
				ptTemp, hasTemp := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]
				allHourWithTemp[hr] = append(allHourWithTemp[hr], hourLoadWithTemp{
					load:    pt.HomeKWH,
					tempC:   ptTemp,
					hasTemp: hasTemp,
				})
			}
		}
	}

	var allRefByHour [24]float64
	var hasAllRef [24]bool
	minOvernightRef := math.MaxFloat64
	var nonEVOvernightRefs []float64
	hasIntermittentHighFreqNightEV := false

	// Compute the initial 30th-percentile cross-day reference load for each hour of the day
	// and collect overnight statistics (minimum overnight reference, non-EV overnight references,
	// and whether any overnight hour exhibits high-frequency intermittent EV charging).
	for hr := 0; hr < 24; hr++ {
		if len(allHourLoads[hr]) >= 3 {
			sorted := make([]float64, len(allHourLoads[hr]))
			copy(sorted, allHourLoads[hr])
			sort.Float64s(sorted)
			ref := sorted[int(math.Round(float64(len(sorted)-1)*hourlyOutlierBaselinePercentile))]
			allRefByHour[hr] = ref
			hasAllRef[hr] = true
			hourBaselineRef[hr] = ref
			if isNighttimeEVHour(hr) {
				if ref < minOvernightRef {
					minOvernightRef = ref
				}
				if ref < EVMinThresholdKW {
					nonEVOvernightRefs = append(nonEVOvernightRefs, ref)
				}
				// If an overnight hour has a 30th-percentile load >= 3.0 kWh across at least 7 days
				// of history (1 full week), yet still has at least 1 non-EV night (< 4.8 kWh), that
				// indicates high-frequency (e.g. 5-6 nights/week) intermittent EV charging rather than
				// a permanent 24/7 base load.
				if ref >= 3.0 && len(allHourLoads[hr]) >= 7 && len(allHourNonEVLoads[hr]) >= 1 {
					hasIntermittentHighFreqNightEV = true
				}
			}
		}
	}

	fallbackOvernightRef := standbyLoad
	if fallbackOvernightRef <= 0 {
		fallbackOvernightRef = hourlyOutlierFloorKWH
	}
	if len(nonEVOvernightRefs) > 0 {
		sort.Float64s(nonEVOvernightRefs)
		fallbackOvernightRef = nonEVOvernightRefs[len(nonEVOvernightRefs)/2]
	}

	// Anchor overnight hours where frequent EV charging (5-6 nights/week, or 7 nights/week with configured EV periods)
	// pulled the 30th percentile into the EV tier (>= 3.0 kWh) despite the home having a quiet overnight baseline.
	for hr := 0; hr < 24; hr++ {
		if !hasAllRef[hr] {
			continue
		}
		prevRef := allRefByHour[hr]
		anchored := false
		if hasConfiguredEVAtHour[hr] && allRefByHour[hr] >= EVMinThresholdKW {
			if len(allHourNonEVLoads[hr]) > 0 {
				sortedNonEV := make([]float64, len(allHourNonEVLoads[hr]))
				copy(sortedNonEV, allHourNonEVLoads[hr])
				sort.Float64s(sortedNonEV)
				allRefByHour[hr] = sortedNonEV[len(sortedNonEV)/2]
			} else {
				allRefByHour[hr] = fallbackOvernightRef
			}
			hourBaselineRef[hr] = allRefByHour[hr]
			anchored = true
		} else if isNighttimeEVHour(hr) && allRefByHour[hr] >= 3.0 && len(allHourLoads[hr]) >= 7 && minOvernightRef < 2.5 {
			var quietOvernightLoads []float64
			for _, v := range allHourNonEVLoads[hr] {
				if v < 3.0 {
					quietOvernightLoads = append(quietOvernightLoads, v)
				}
			}
			if len(quietOvernightLoads) >= 1 {
				sort.Float64s(quietOvernightLoads)
				allRefByHour[hr] = quietOvernightLoads[len(quietOvernightLoads)/2]
				hourBaselineRef[hr] = allRefByHour[hr]
				anchored = true
			} else if hasIntermittentHighFreqNightEV {
				allRefByHour[hr] = fallbackOvernightRef
				hourBaselineRef[hr] = allRefByHour[hr]
				anchored = true
			}
		}
		if anchored {
			log.Ctx(ctx).DebugContext(
				ctx,
				"anchoring elevated overnight hourly reference load to non-EV baseline",
				slog.Int("hour", hr),
				slog.Float64("prevRefLoadKWH", prevRef),
				slog.Float64("anchoredRefLoadKWH", allRefByHour[hr]),
				slog.Float64("minOvernightRefKWH", minOvernightRef),
				slog.Bool("hasConfiguredEV", hasConfiguredEVAtHour[hr]),
				slog.Bool("hasIntermittentHighFreqNightEV", hasIntermittentHighFreqNightEV),
			)
		}
	}

	dayP65Map := make(map[string]float64)
	dayP75Map := make(map[string]float64)
	for dateStr, d := range dayMap {
		if len(d.loads) >= 18 {
			// Replace overnight (00:00-08:00, 23:00) EV spikes with their cross-day overnight baseline
			// when computing same-day HVAC percentiles so an overnight EV session cannot combine with
			// a few afternoon EV hours to inflate dayP65/dayP75, while keeping sustained daytime AC intact.
			sortedDay := make([]float64, 0, len(d.points))
			for _, pt := range d.points {
				if pt.HomeKWH <= 0.0 {
					continue
				}
				val := pt.HomeKWH
				hr := pt.TSHourStart.Hour()
				if (hr < 9 || hr > 22) && hr >= 0 && hr < 24 && hasAllRef[hr] && val >= EVMinThresholdKW {
					baseRef := allRefByHour[hr]
					if isEVStandbyEligible(settings, pt.TSHourStart) || (val-baseRef >= EVMinStepKW && val > max(baseRef, hourlyOutlierFloorKWH)*hourlyOutlierMultiple) {
						val = baseRef
					}
				}
				sortedDay = append(sortedDay, val)
			}
			if len(sortedDay) >= 18 {
				sort.Float64s(sortedDay)
				dayP65Map[dateStr] = sortedDay[int(math.Round(float64(len(sortedDay)-1)*hourlyOutlierSameDayHVACPercentile))]
				dayP75Map[dateStr] = sortedDay[int(math.Round(float64(len(sortedDay)-1)*0.75))]
			}
		}
	}

	computePointRefLoad := func(pt types.EnergyStats, hr int, dayP65, dayP75 float64, hasDayStats bool) float64 {
		refLoad := allRefByHour[hr]
		// Apply same-day HVAC percentiles (dayP65/dayP75) only during daytime/evening hours (09:00-22:00)
		// when solar/outdoor heat drives central AC. Applying daytime AC percentiles to overnight sleeping
		// hours (00:00-08:00, 23:00) would mask unconfigured 7.7 kW overnight EV charging on warm summer days
		// and inflate overnight replacement loads with daytime AC levels.
		isDaytimeHVACHour := hr >= 9 && hr <= 22
		if isDaytimeHVACHour && hasDayStats && dayP65 > refLoad {
			refLoad = dayP65
		}
		if ptTemp, hasPtTemp := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]; hasPtTemp {
			if isDaytimeHVACHour && hasDayStats && ptTemp >= extremeHeatwaveMinTempC && dayP75 > refLoad {
				refLoad = dayP75
			}
			var tempMatchedLoads []float64
			for _, hlt := range allHourWithTemp[hr] {
				if hlt.hasTemp && math.Abs(hlt.tempC-ptTemp) <= tempSimilarityScale {
					tempMatchedLoads = append(tempMatchedLoads, hlt.load)
				}
			}
			if len(tempMatchedLoads) >= 3 {
				sort.Float64s(tempMatchedLoads)
				// For small sample sizes (3-4 temperature-matched days), use math.Floor so that 2 EV
				// charging sessions during a 3-day hot spell select index 0 (the non-EV day) rather
				// than rounding up to index 1 (an EV spike).
				idx := int(math.Round(float64(len(tempMatchedLoads)-1) * hourlyOutlierBaselinePercentile))
				if len(tempMatchedLoads) < 5 {
					idx = int(math.Floor(float64(len(tempMatchedLoads)-1) * hourlyOutlierBaselinePercentile))
				}
				tempRef := tempMatchedLoads[idx]
				// Only allow temperature-matched reference to raise refLoad during daytime HVAC hours
				// or when tempRef is below EVMinThresholdKW so frequent overnight EV sessions at similar
				// nighttime temperatures do not mask each other.
				if tempRef > refLoad && (isDaytimeHVACHour || tempRef < EVMinThresholdKW) {
					refLoad = tempRef
				}
			}
		}
		return refLoad
	}

	// Pass 1: Detect full-hour (>= 4.8 kW) EV charging sessions and load spikes.
	confirmedPass1 := make(map[dayHourKey]bool)
	for dateStr, d := range dayMap {
		dayP65, hasDayStats := dayP65Map[dateStr]
		dayP75 := dayP75Map[dateStr]
		for _, pt := range d.points {
			if pt.HomeKWH < EVMinThresholdKW {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || !hasAllRef[hr] {
				continue
			}
			refLoad := computePointRefLoad(pt, hr, dayP65, dayP75, hasDayStats)
			limit := max(refLoad, hourlyOutlierFloorKWH) * hourlyOutlierMultiple
			if isEVStandbyEligible(settings, pt.TSHourStart) || (pt.HomeKWH-refLoad >= EVMinStepKW && pt.HomeKWH > limit) {
				key := dayHourKey{date: dateStr, hour: hr}
				ignoredOutlierHours[key] = true
				confirmedPass1[key] = true
				outlierRefLoad[key] = refLoad
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring intermittent EV or hourly load spike in energy model",
					slog.Int("hour", hr),
					slog.String("weekday", d.weekday.String()),
					slog.String("date", dateStr),
					slog.Float64("outlierLoadKWH", pt.HomeKWH),
					slog.Float64("refLoadKWH", refLoad),
				)
			}
		}
	}

	// Pass 2: Detect nighttime partial-charge shoulder hours (>= 3.2 kWh, step >= 2.0 kWh, > 2.0x refLoad)
	// immediately adjacent (1-hop only, no chaining) to a Pass 1 confirmed EV spike.
	// Restricted to nighttime hours (20:00-06:59) because daytime 3.2-4.7 kWh loads overlap with normal
	// HVAC, electric dryers, and cooking appliances.
	for dateStr, d := range dayMap {
		dayP65, hasDayStats := dayP65Map[dateStr]
		dayP75 := dayP75Map[dateStr]
		for _, pt := range d.points {
			if pt.HomeKWH < evShoulderMinThresholdKW || pt.HomeKWH >= EVMinThresholdKW {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || !hasAllRef[hr] || !isNighttimeEVHour(hr) {
				continue
			}
			key := dayHourKey{date: dateStr, hour: hr}
			if ignoredOutlierHours[key] {
				continue
			}
			prevKey := dayHourKey{date: dateStr, hour: hr - 1}
			if hr == 0 && !d.dayTime.IsZero() {
				prevKey = dayHourKey{date: d.dayTime.AddDate(0, 0, -1).Format("2006-01-02"), hour: 23}
			}
			nextKey := dayHourKey{date: dateStr, hour: hr + 1}
			if hr == 23 && !d.dayTime.IsZero() {
				nextKey = dayHourKey{date: d.dayTime.AddDate(0, 0, 1).Format("2006-01-02"), hour: 0}
			}
			if !confirmedPass1[prevKey] && !confirmedPass1[nextKey] {
				continue
			}
			refLoad := computePointRefLoad(pt, hr, dayP65, dayP75, hasDayStats)
			limit := max(refLoad, hourlyOutlierFloorKWH) * evShoulderMultiple
			if pt.HomeKWH-refLoad >= evShoulderMinStepKW && pt.HomeKWH > limit {
				ignoredOutlierHours[key] = true
				outlierRefLoad[key] = refLoad
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring adjacent EV shoulder hour in energy model",
					slog.Int("hour", hr),
					slog.String("weekday", d.weekday.String()),
					slog.String("date", dateStr),
					slog.Float64("shoulderLoadKWH", pt.HomeKWH),
					slog.Float64("refLoadKWH", refLoad),
				)
			}
		}
	}

	return ignoredOutlierHours, outlierRefLoad, hourBaselineRef
}
