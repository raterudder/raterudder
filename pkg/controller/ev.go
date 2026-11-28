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
	// EVMinThresholdKW is the default minimum household load required to consider full-hour Level 2 EV charging active.
	EVMinThresholdKW = 4.8
	// EVMinStepKW is the default minimum step increase above baseline load required to confirm full-hour Level 2 EV charging.
	EVMinStepKW = 3.5

	// evAdaptiveMinHistoryHours is the minimum number of hourly history points required before
	// DetectEVCharging infers a site-specific Level 2 charger threshold from historical EV sessions.
	evAdaptiveMinHistoryHours = 72

	// evAdaptiveMaxThresholdKW and evAdaptiveMaxStepKW cap adaptive Level 2 thresholds on
	// high-power charger sites (e.g. 48A/11.5 kW or dual-charger homes) so partial-hour or
	// variable-rate charging still triggers while rejecting 4.8-6.7 kW non-EV spikes (dryers, ovens, HVAC).
	evAdaptiveMaxThresholdKW     = 6.8
	evAdaptiveMaxStepKW          = 5.4
	evAdaptiveStepToLoadOffsetKW = 1.4

	// Site-adaptive EV threshold inference parameters:
	//   - highCore: candidate EV hour with load >= 8.2 kW, step >= 6.5 kW, and neither adjacent hour
	//     exceeding load + 1.5 kW (excludes partial ramp/shoulder hours).
	//   - derived threshold: minStep = clamp(q35(coreSteps) * 0.72, EVMinStepKW, 5.4),
	//     minLoad = clamp(minStep + 1.4, EVMinThresholdKW, 6.8).
	//   - slow-night guard: if >= 2 overnight sessions contain 2+ consecutive 4.8-7.79 kW hours
	//     without an adjacent >= 8.2 kW hour (mixed slow/fast EV household), preserve 4.8/3.5 kW defaults.
	evAdaptiveCoreStepQuantile       = 0.35
	evAdaptiveCoreStepScale          = 0.72
	evAdaptiveHighCoreMinKW          = 8.2
	evAdaptiveHighCoreMinStepKW      = 6.5
	evAdaptiveMinHighCoreHours       = 4
	evAdaptiveCoreNeighborMarginKW   = 1.5
	evAdaptiveSlowNightMaxKW         = 7.8
	evAdaptiveSlowNightNeighborMaxKW = 8.2
	evAdaptiveMinSlowNightSessions   = 2
	evActualAnchorMinAdaptiveKW      = 8.0

	// evShoulderMinThresholdKW is the minimum household load required to flag a partial-charge shoulder hour
	// (e.g. the first or last hour of an EV charging session where the charger ran for 25-40 minutes)
	// when immediately adjacent to a confirmed actual EV anchor hour.
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

	// standbyLoadPercentile is the quantile (1st percentile) of positive historical hourly loads (> 0.05 kWh)
	// used to estimate a household's baseline overnight/background standby floor when no quieter non-EV
	// overnight hour reference is available (clamped to >= 0.1 kWh).
	standbyLoadPercentile = 0.01
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

func isUTCLikeLocation(loc *time.Location) bool {
	if loc == nil || loc == time.UTC {
		return true
	}
	name := loc.String()
	return name == "" || name == "UTC"
}

func resolveEnergyStatsLocation(tz string, fallback *time.Location, cache map[string]*time.Location) *time.Location {
	if tz != "" {
		if fallback != nil && fallback.String() == tz {
			return fallback
		}
		if cache != nil {
			if cached, ok := cache[tz]; ok && cached != nil {
				return cached
			}
		}
		if loaded, err := time.LoadLocation(tz); err == nil {
			if cache != nil {
				cache[tz] = loaded
			}
			return loaded
		}
	}
	if fallback != nil {
		return fallback
	}
	return time.UTC
}

// IsEVStandbyEligible returns true if the site has opted into EV charging standby for ts:
// EVChargingStandby is enabled and ts falls within the nighttime window (20:00-06:59 local time).
// Real-time EV standby intentionally does not trigger during daytime hours (07:00-19:59) when
// using solar to charge the EV is assumed to be desired.
func IsEVStandbyEligible(settings types.Settings, ts time.Time) bool {
	if !settings.EVChargingStandby {
		return false
	}
	localTS := ts
	if isUTCLikeLocation(localTS.Location()) && settings.Location != nil && settings.Location.TimeZone != "" {
		if loc, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			localTS = localTS.In(loc)
		}
	}
	return isNighttimeEVHour(localTS.Hour())
}

// DetectEVCharging checks if the current instantaneous household load reflects active EV charging,
// incorporating site settings (such as EVChargingStandby windows and timezone) when inferring
// site-adaptive charger thresholds and evaluating historical load baselines.
// When sufficient multi-day history (>= 72 hours) is provided, it infers site-specific Level 2
// charger thresholds from historical EV sessions (capped at 6.8 kW total / 5.4 kW step for 48A+
// chargers without slow-night sessions); otherwise it evaluates >= 4.8 kW total / >= 3.5 kW step.
func DetectEVCharging(ctx context.Context, currentLoad float64, history []types.EnergyStats, settings types.Settings) (bool, float64) {
	minThresholdKW := EVMinThresholdKW
	minStepKW := EVMinStepKW
	if len(history) >= evAdaptiveMinHistoryHours {
		minThresholdKW, minStepKW = inferSiteEVThresholdsFromStats(ctx, history, settings)
	}
	return detectEVChargingWithThresholds(ctx, currentLoad, history, settings, minThresholdKW, minStepKW)
}

func detectEVChargingWithThresholds(ctx context.Context, currentLoad float64, history []types.EnergyStats, settings types.Settings, minThresholdKW, minStepKW float64) (bool, float64) {
	if minThresholdKW <= 0 {
		minThresholdKW = EVMinThresholdKW
	}
	if minStepKW <= 0 {
		minStepKW = EVMinStepKW
	}
	if currentLoad < minThresholdKW {
		return false, 0
	}

	baselineKW := calculateRecentBaseline(ctx, history, minThresholdKW, settings)
	stepKW := currentLoad - baselineKW
	isEV := stepKW >= minStepKW

	log.Ctx(ctx).DebugContext(ctx, "evaluating ev charging detection",
		slog.Float64("currentLoad", currentLoad),
		slog.Float64("baselineKW", baselineKW),
		slog.Float64("stepKW", stepKW),
		slog.Float64("minThresholdKW", minThresholdKW),
		slog.Float64("minStepKW", minStepKW),
		slog.Bool("isEV", isEV),
	)

	return isEV, stepKW
}

// FilterCompletedHourlyStats returns hourly stats with non-zero timestamps strictly before the start
// of now's local hour, excluding in-progress current-hour buckets whose partial kWh totals can distort
// live EV charging baseline calculations.
func FilterCompletedHourlyStats(history []types.EnergyStats, now time.Time) []types.EnergyStats {
	if len(history) == 0 || now.IsZero() {
		return history
	}
	currentHourStart := now.Add(
		-time.Duration(now.Minute())*time.Minute -
			time.Duration(now.Second())*time.Second -
			time.Duration(now.Nanosecond())*time.Nanosecond,
	)
	completed := make([]types.EnergyStats, 0, len(history))
	for _, h := range history {
		if !h.TSHourStart.IsZero() && h.TSHourStart.Before(currentHourStart) {
			completed = append(completed, h)
		}
	}
	return completed
}

// FilterTODHomeLoadEVSpikes replaces historical EV charging spikes in a time-of-day load slice
// with the non-EV baseline reference load so intermittent EV charging sessions do not inflate
// high-home-load notification percentile thresholds.
func FilterTODHomeLoadEVSpikes(loads []float64, targetHour int) []float64 {
	if len(loads) < 3 {
		return loads
	}
	sorted := make([]float64, len(loads))
	copy(sorted, loads)
	sort.Float64s(sorted)
	refLoad := sorted[int(math.Round(float64(len(sorted)-1)*hourlyOutlierBaselinePercentile))]

	// For high-frequency nighttime EV chargers (where >= 70% of readings in the window are EV charging),
	// anchor the reference load to the median of quiet non-EV readings (< 3.0 kWh) if available.
	if isNighttimeEVHour(targetHour) && refLoad >= 3.0 {
		var quietLoads []float64
		for _, v := range sorted {
			if v < 3.0 {
				quietLoads = append(quietLoads, v)
			}
		}
		if len(quietLoads) > 0 {
			refLoad = quietLoads[len(quietLoads)/2]
		}
	}

	limit := max(refLoad, hourlyOutlierFloorKWH) * hourlyOutlierMultiple
	filtered := make([]float64, len(loads))
	for i, v := range loads {
		if v >= EVMinThresholdKW && v-refLoad >= EVMinStepKW && v > limit {
			filtered[i] = refLoad
		} else {
			filtered[i] = v
		}
	}
	return filtered
}

// calculateRecentBaseline computes the non-EV baseline from recent hourly history.
// It inspects the preceding 1-6 hours, filtering out elevated hours to find the true household base load.
// When multi-day history is available and the recent 1-6 hour median is elevated (>= 2.5 kW, e.g. from
// late-afternoon/early-evening central AC before an overnight EV session starts), it also checks the same
// hour of day on prior days and takes the lower baseline so evening HVAC does not mask nighttime EV charging.
func calculateRecentBaseline(ctx context.Context, history []types.EnergyStats, minThresholdKW float64, settings types.Settings) float64 {
	if minThresholdKW <= 0 {
		minThresholdKW = EVMinThresholdKW
	}
	if len(history) == 0 {
		return 1.0 // Default baseline assumption when no history is present
	}

	locCache := make(map[string]*time.Location)
	var settingsLoc *time.Location
	if settings.Location != nil && settings.Location.TimeZone != "" {
		settingsLoc = resolveEnergyStatsLocation(settings.Location.TimeZone, nil, locCache)
	}

	// Copy and localize timestamps before sorting descending (most recent first)
	hCopy := make([]types.EnergyStats, len(history))
	for i, h := range history {
		hCopy[i] = h
		if !h.TSHourStart.IsZero() {
			fallbackLoc := h.TSHourStart.Location()
			if settingsLoc != nil && isUTCLikeLocation(fallbackLoc) {
				fallbackLoc = settingsLoc
			}
			ptLoc := resolveEnergyStatsLocation(h.TimeLocation, fallbackLoc, locCache)
			hCopy[i].TSHourStart = h.TSHourStart.In(ptLoc)
		}
	}
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
		if !isNighttimeEVHour(hr) && hCopy[i].HomeKWH >= 3.8 && hCopy[i].HomeKWH <= sustainedDaytimeHVACMaxKW {
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
		// If an hour was below the site's EV threshold (or is part of sustained daytime HVAC <= 7.5 kW),
		// it is a candidate baseline.
		if hCopy[i].HomeKWH > 0.05 && (hCopy[i].HomeKWH < minThresholdKW || (sustainedDaytimeHVAC && !isNighttimeEVHour(hr) && hCopy[i].HomeKWH <= sustainedDaytimeHVACMaxKW)) {
			baselines = append(baselines, hCopy[i].HomeKWH)
		}
	}

	var recentMedian float64
	if len(baselines) == 0 {
		foundLookback := false
		// If all recent 6 hours were elevated, look further back (up to 24 hours) for the first hour < minThresholdKW
		for i := 6; i < len(hCopy) && i < 24; i++ {
			if hCopy[i].HomeKWH < minThresholdKW && hCopy[i].HomeKWH > 0.05 {
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

// quantileSortedFloat64 returns the linearly interpolated q-th quantile from an already sorted slice.
func quantileSortedFloat64(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 || q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

func adjacentDayHourKeys(d *dayPoints, dateStr string, hr int) (dayHourKey, dayHourKey) {
	prevKey := dayHourKey{date: dateStr, hour: hr - 1}
	if hr == 0 && d != nil && !d.dayTime.IsZero() {
		prevKey = dayHourKey{date: d.dayTime.AddDate(0, 0, -1).Format("2006-01-02"), hour: 23}
	}
	nextKey := dayHourKey{date: dateStr, hour: hr + 1}
	if hr == 23 && d != nil && !d.dayTime.IsZero() {
		nextKey = dayHourKey{date: d.dayTime.AddDate(0, 0, 1).Format("2006-01-02"), hour: 0}
	}
	return prevKey, nextKey
}

type evOutlierContext struct {
	allRefByHour        [24]float64
	hasAllRef           [24]bool
	hourBaselineRef     map[int]float64
	dayP65Map           map[string]float64
	dayP75Map           map[string]float64
	computePointRefLoad func(pt types.EnergyStats, hr int, dayP65, dayP75 float64, hasDayStats bool) float64
}

func prepareEVOutlierContext(
	ctx context.Context,
	dayMap map[string]*dayPoints,
	weatherByHour map[time.Time]float64,
	settings types.Settings,
	standbyLoad float64,
) evOutlierContext {
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
				if IsEVStandbyEligible(settings, pt.TSHourStart) {
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

	// Anchor overnight hours where frequent EV charging (5-7 nights/week, or configured EV periods)
	// pulled the 30th percentile into an elevated EV tier despite the home having a quiet overnight baseline.
	var anchoredOvernightEV [24]bool
	for hr := 0; hr < 24; hr++ {
		if !hasAllRef[hr] {
			continue
		}
		prevRef := allRefByHour[hr]
		anchored := false
		// Case 1: EV charging standby is enabled at hour hr (EVChargingStandby 20:00-06:59)
		// and charging occurred on >= 70% of days (including 7/7 nights), pulling the 30th percentile >= EVMinThresholdKW
		// (4.8 kW). Anchor down to the median of non-EV hours at hr, or fallbackOvernightRef when every day charged.
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
			// Case 2: Configured nighttime EV hour whose 30th percentile sits in the 3.0-4.79 kW partial/shoulder EV tier
			// (e.g., partial first/last hour of nightly charging sessions) while another overnight hour sleeps quietly
			// (< 2.5 kW). Anchor down to the median of quiet (< 3.0 kWh) readings at hr, or fallbackOvernightRef if none.
		} else if hasConfiguredEVAtHour[hr] && isNighttimeEVHour(hr) && allRefByHour[hr] >= 3.0 && minOvernightRef < 2.5 {
			var quietOvernightLoads []float64
			for _, v := range allHourNonEVLoads[hr] {
				if v < 3.0 {
					quietOvernightLoads = append(quietOvernightLoads, v)
				}
			}
			if len(quietOvernightLoads) > 0 {
				sort.Float64s(quietOvernightLoads)
				allRefByHour[hr] = quietOvernightLoads[len(quietOvernightLoads)/2]
			} else {
				allRefByHour[hr] = fallbackOvernightRef
			}
			hourBaselineRef[hr] = allRefByHour[hr]
			anchored = true
			// Case 3: Unconfigured nighttime hour (EVChargingStandby disabled) across >= 7 days of history where
			// frequent EV charging (5-6 nights/week) lifted the 30th percentile >= 3.0 kW even though the home sleeps
			// below 2.5 kW overnight. Anchor to the median of quiet (< 3.0 kWh) hours at hr if available; otherwise
			// only anchor to fallbackOvernightRef when hasIntermittentHighFreqNightEV proves overnight spikes are
			// intermittent rather than a continuous 7/7 overnight appliance (e.g. pool pump).
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
			anchoredOvernightEV[hr] = true
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
					if val-baseRef >= EVMinStepKW && val > max(baseRef, hourlyOutlierFloorKWH)*hourlyOutlierMultiple {
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

	// Pass nil for loc so detectWeatherHVACSensitivity indexes hours via pt.TSHourStart.Hour(),
	// matching allHourLoads, allHourWithTemp, and computePointRefLoad (dayMap points are already
	// localized by buildHistoryDayMapAndStandby).
	hvac := detectWeatherHVACSensitivity(dayMap, weatherByHour, nil, nil, EVMinThresholdKW)

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
			isColdHeatingHour := hvac.heatingHours[hr] && ptTemp <= coldHeatingMaxTempC
			isHotCoolingHour := hvac.coolingHours[hr] && ptTemp >= hotCoolingMinTempC
			if isColdHeatingHour {
				expectedColdKWH := min(
					hvac.heatingHourInterceptKWH[hr]+hvac.heatingHourSlopeKWHPerC[hr]*max(0.0, coldHeatingBaseTempC-ptTemp),
					hvacExpectedRefMaxKWH,
				)
				if expectedColdKWH > refLoad {
					refLoad = expectedColdKWH
				}
			}
			if isHotCoolingHour {
				expectedHotKWH := min(
					hvac.coolingHourInterceptKWH[hr]+hvac.coolingHourSlopeKWHPerC[hr]*max(0.0, ptTemp-hotCoolingBaseTempC),
					hvacExpectedRefMaxKWH,
				)
				if expectedHotKWH > refLoad {
					refLoad = expectedHotKWH
				}
			}

			var tempMatchedLoads []float64
			for _, hlt := range allHourWithTemp[hr] {
				if !hlt.hasTemp {
					continue
				}
				var matched bool
				switch {
				case isColdHeatingHour:
					matched = hlt.tempC <= ptTemp+coldHeatingAsymmetricUpperBandC && hlt.tempC >= ptTemp-tempSimilarityScale
				case isHotCoolingHour:
					matched = hlt.tempC >= ptTemp-hotCoolingAsymmetricLowerBandC && hlt.tempC <= ptTemp+tempSimilarityScale
				default:
					matched = math.Abs(hlt.tempC-ptTemp) <= tempSimilarityScale
				}
				if matched {
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
				// Only allow temperature-matched reference to raise refLoad above EVMinThresholdKW during
				// daytime HVAC hours or confirmed cold-heating/hot-cooling hours (excluding overnight hours
				// already anchored down for frequent/configured EV charging so EV sessions do not mask each other).
				allowElevatedTempRef := isDaytimeHVACHour ||
					((isColdHeatingHour || isHotCoolingHour) && !(isNighttimeEVHour(hr) && anchoredOvernightEV[hr]))
				if tempRef > refLoad && (allowElevatedTempRef || tempRef < EVMinThresholdKW) {
					refLoad = tempRef
				}
			}
		}
		return refLoad
	}

	return evOutlierContext{
		allRefByHour:        allRefByHour,
		hasAllRef:           hasAllRef,
		hourBaselineRef:     hourBaselineRef,
		dayP65Map:           dayP65Map,
		dayP75Map:           dayP75Map,
		computePointRefLoad: computePointRefLoad,
	}
}

func inferSiteEVThresholds(ctx context.Context, dayMap map[string]*dayPoints, evCtx evOutlierContext) (float64, float64) {
	totalPoints := 0
	for _, d := range dayMap {
		if d != nil {
			totalPoints += len(d.points)
		}
	}
	if totalPoints < evAdaptiveMinHistoryHours {
		return EVMinThresholdKW, EVMinStepKW
	}

	candLoad := make(map[dayHourKey]float64)
	candStep := make(map[dayHourKey]float64)
	candDay := make(map[dayHourKey]*dayPoints)
	for dateStr, d := range dayMap {
		dayP65, hasDayStats := evCtx.dayP65Map[dateStr]
		dayP75 := evCtx.dayP75Map[dateStr]
		for _, pt := range d.points {
			if pt.HomeKWH < EVMinThresholdKW {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || !evCtx.hasAllRef[hr] {
				continue
			}
			refLoad := evCtx.computePointRefLoad(pt, hr, dayP65, dayP75, hasDayStats)
			limit := max(refLoad, hourlyOutlierFloorKWH) * hourlyOutlierMultiple
			step := pt.HomeKWH - refLoad
			if step >= EVMinStepKW && pt.HomeKWH > limit {
				key := dayHourKey{date: dateStr, hour: hr}
				if prev, ok := candLoad[key]; !ok || pt.HomeKWH > prev {
					candLoad[key] = pt.HomeKWH
					candStep[key] = step
					candDay[key] = d
				}
			}
		}
	}

	var highCoreSteps []float64
	for key, load := range candLoad {
		prevKey, nextKey := adjacentDayHourKeys(candDay[key], key.date, key.hour)
		if candLoad[prevKey] >= load+evAdaptiveCoreNeighborMarginKW || candLoad[nextKey] >= load+evAdaptiveCoreNeighborMarginKW {
			continue
		}
		if load >= evAdaptiveHighCoreMinKW && candStep[key] >= evAdaptiveHighCoreMinStepKW {
			highCoreSteps = append(highCoreSteps, candStep[key])
		}
	}

	if len(highCoreSteps) < evAdaptiveMinHighCoreHours {
		return EVMinThresholdKW, EVMinStepKW
	}

	// "Slow night" guard: a "slow night" is an overnight (20:00-06:59) EV session charging at moderate
	// Level 2 power (4.8-7.79 kW, e.g., a 24A/32A charger, PHEV, or slower second EV in a multi-EV home)
	// across >= 2 consecutive candidate hours without an adjacent >= 8.2 kW high-power hour.
	// When a home has both high-power core hours (>= 8.2 kW) and >= 2 distinct overnight slow-night sessions,
	// we preserve the default 4.8/3.5 kW thresholds so future 4.8-6.7 kW sessions by the slower EV aren't missed.
	isSlowNightCand := func(k dayHourKey) bool {
		if !isNighttimeEVHour(k.hour) {
			return false
		}
		load, ok := candLoad[k]
		if !ok || load < EVMinThresholdKW || load >= evAdaptiveSlowNightMaxKW {
			return false
		}
		prevKey, nextKey := adjacentDayHourKeys(candDay[k], k.date, k.hour)
		return candLoad[prevKey] < evAdaptiveSlowNightNeighborMaxKW && candLoad[nextKey] < evAdaptiveSlowNightNeighborMaxKW
	}

	slowNightSessions := make(map[string]bool)
	for key := range candLoad {
		if !isSlowNightCand(key) {
			continue
		}
		d := candDay[key]
		prevKey, nextKey := adjacentDayHourKeys(d, key.date, key.hour)
		if isSlowNightCand(prevKey) || isSlowNightCand(nextKey) {
			// Group post-midnight hours (00:00-06:59) into the previous evening's overnight session date.
			sessionDate := key.date
			if key.hour <= 6 && d != nil && !d.dayTime.IsZero() {
				sessionDate = d.dayTime.AddDate(0, 0, -1).Format("2006-01-02")
			}
			slowNightSessions[sessionDate] = true
		}
	}

	if len(slowNightSessions) >= evAdaptiveMinSlowNightSessions {
		log.Ctx(ctx).DebugContext(
			ctx,
			"slow night EV sessions detected, keeping default EV charging thresholds",
			slog.Int("highCoreHours", len(highCoreSteps)),
			slog.Int("slowNightSessions", len(slowNightSessions)),
			slog.Float64("siteMinThresholdKW", EVMinThresholdKW),
			slog.Float64("siteMinStepKW", EVMinStepKW),
		)
		return EVMinThresholdKW, EVMinStepKW
	}

	sort.Float64s(highCoreSteps)
	quantileStepKW := quantileSortedFloat64(highCoreSteps, evAdaptiveCoreStepQuantile)
	siteMinStepKW := min(evAdaptiveMaxStepKW, max(EVMinStepKW, quantileStepKW*evAdaptiveCoreStepScale))
	siteMinThresholdKW := min(evAdaptiveMaxThresholdKW, max(EVMinThresholdKW, siteMinStepKW+evAdaptiveStepToLoadOffsetKW))

	log.Ctx(ctx).DebugContext(
		ctx,
		"inferred site-adaptive EV charging thresholds",
		slog.Int("highCoreHours", len(highCoreSteps)),
		slog.Int("slowNightSessions", len(slowNightSessions)),
		slog.Float64("quantileStepKW", quantileStepKW),
		slog.Float64("siteMinThresholdKW", siteMinThresholdKW),
		slog.Float64("siteMinStepKW", siteMinStepKW),
	)

	return siteMinThresholdKW, siteMinStepKW
}

func buildHistoryDayMapAndStandby(
	ctx context.Context,
	history []types.EnergyStats,
	defaultLoc *time.Location,
	locCache map[string]*time.Location,
) (map[string]*dayPoints, float64, float64, *time.Location) {
	if locCache == nil {
		locCache = make(map[string]*time.Location)
	}
	loc := defaultLoc
	if isUTCLikeLocation(loc) {
		for _, h := range history {
			if h.TSHourStart.IsZero() || h.TimeLocation == "" {
				continue
			}
			if loc == nil || h.TimeLocation != loc.String() {
				loc = resolveEnergyStatsLocation(h.TimeLocation, loc, locCache)
			}
		}
	}
	if loc == nil {
		loc = time.UTC
	}

	var validLoads []float64
	var validLoadsSum float64
	for _, h := range history {
		if h.HomeKWH > 0.05 {
			validLoads = append(validLoads, h.HomeKWH)
			validLoadsSum += h.HomeKWH
		}
	}
	standbyLoad := 0.1
	overallAvgLoad := 0.0
	if len(validLoads) > 0 {
		sortedLoads := make([]float64, len(validLoads))
		copy(sortedLoads, validLoads)
		sort.Float64s(sortedLoads)
		idx := int(float64(len(sortedLoads)-1) * standbyLoadPercentile)
		standbyLoad = max(0.1, sortedLoads[idx])
		overallAvgLoad = validLoadsSum / float64(len(validLoads))
	}

	dayMap := make(map[string]*dayPoints)
	for _, h := range history {
		if h.TSHourStart.IsZero() || h.HomeKWH <= 0.0 {
			continue
		}
		ptLoc := resolveEnergyStatsLocation(h.TimeLocation, loc, locCache)
		localTS := h.TSHourStart.In(ptLoc)
		dateStr := localTS.Format("2006-01-02")

		d, exists := dayMap[dateStr]
		if !exists {
			dTime, err := time.ParseInLocation("2006-01-02", dateStr, ptLoc)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to parse dateStr", slog.String("dateStr", dateStr), slog.Any("err", err))
				continue
			}
			d = &dayPoints{
				date:    dateStr,
				dayTime: dTime,
				weekday: dTime.Weekday(),
			}
			dayMap[dateStr] = d
		}
		// Copy before localizing timestamp so the caller's history slice is not mutated.
		sanitized := h
		sanitized.TSHourStart = localTS
		d.points = append(d.points, sanitized)
		d.loads = append(d.loads, h.HomeKWH)
	}

	// Normalize merged 2-hour fall-back buckets on 25-hour autumn DST transition days.
	// When hourly telemetry is aggregated or deduplicated by local wall-clock hour across the repeated
	// 01:00 hour, a single 01:00 record spanning two elapsed hours up to 02:00 contains 2 hours of energy
	// and would otherwise appear as a 2x load spike.
	for _, d := range dayMap {
		if d == nil || d.dayTime.IsZero() || d.dayTime.AddDate(0, 0, 1).Sub(d.dayTime) != 25*time.Hour {
			continue
		}
		var idx1 []int
		var ts2 time.Time
		for i, pt := range d.points {
			switch pt.TSHourStart.Hour() {
			case 1:
				idx1 = append(idx1, i)
			case 2:
				if ts2.IsZero() {
					ts2 = pt.TSHourStart
				}
			}
		}
		if len(idx1) == 1 && !ts2.IsZero() {
			i1 := idx1[0]
			if ts2.Sub(d.points[i1].TSHourStart) == 2*time.Hour {
				d.points[i1].HomeKWH *= 0.5
				if i1 < len(d.loads) {
					d.loads[i1] *= 0.5
				}
			}
		}
	}

	return dayMap, standbyLoad, overallAvgLoad, loc
}

func inferSiteEVThresholdsFromStats(ctx context.Context, history []types.EnergyStats, settings types.Settings) (float64, float64) {
	if len(history) < evAdaptiveMinHistoryHours {
		return EVMinThresholdKW, EVMinStepKW
	}
	locCache := make(map[string]*time.Location)
	var fallbackLoc *time.Location
	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		if h.TimeLocation != "" {
			fallbackLoc = resolveEnergyStatsLocation(h.TimeLocation, fallbackLoc, locCache)
		} else if !isUTCLikeLocation(h.TSHourStart.Location()) {
			fallbackLoc = h.TSHourStart.Location()
		}
	}
	if isUTCLikeLocation(fallbackLoc) && settings.Location != nil && settings.Location.TimeZone != "" {
		fallbackLoc = resolveEnergyStatsLocation(settings.Location.TimeZone, fallbackLoc, locCache)
	}
	dayMap, standbyLoad, _, _ := buildHistoryDayMapAndStandby(ctx, history, fallbackLoc, locCache)
	evCtx := prepareEVOutlierContext(ctx, dayMap, nil, settings, standbyLoad)
	return inferSiteEVThresholds(ctx, dayMap, evCtx)
}

// detectIntermittentEVAndLoadSpikes performs automatic detection of intermittent Level 2 EV charging
// and large unusual one-off household load spikes, returning the flagged outlier hours, their replacement
// baseline loads, per-hour baseline references, and the site-specific EV detection thresholds.
//
// Why we focus on Level 2 EV charging (and adapt thresholds to charger size):
//   - Level 1 chargers (12A on a 120V/15A circuit) draw ~1.4 kW, overlapping normal household appliances
//     and being small enough to absorb without distorting battery plans.
//   - Level 2 chargers typically run on 30A-60A circuits (5.8-11.5 kW). On homes without high-power EV
//     sessions, thresholds stay at 4.8 kW total / 3.5 kW step. On sites with >= 4 local core EV hours
//     >= 8.2 kW (and >= 6.5 kW step above refLoad) and fewer than 2 multi-hour slow-night EV sessions
//     (4.8-7.79 kW without an adjacent >= 8.2 kW anchor), thresholds adapt up to 6.8 kW total / 5.4 kW step
//     so 4.8-6.7 kW appliance/HVAC spikes (dryers, ovens, central AC) are preserved instead of stripped.
//
// Two-pass evaluation:
//   - Pass 1 identifies full-hour spikes meeting the site-adaptive total & step thresholds and > 2.5x refLoad
//     (including within EVChargingStandby windows, requiring a real step above reference load). Confirmed
//     sessions >= actualAnchorMinKW (4.8 kW on default sites, >= 8.0 kW on adaptive high-power sites)
//     become actualEVAnchor hours.
//   - Pass 2 flags adjacent 1-hop partial-charge shoulder hours (>= 3.2 kWh, step >= 2.0 kWh, > 2.0x refLoad)
//     across any hour of the day immediately before or after a confirmed Pass 1 actualEVAnchor without chaining.
func detectIntermittentEVAndLoadSpikes(
	ctx context.Context,
	dayMap map[string]*dayPoints,
	weatherByHour map[time.Time]float64,
	settings types.Settings,
	standbyLoad float64,
) (map[dayHourKey]bool, map[dayHourKey]float64, map[int]float64, float64, float64) {
	evCtx := prepareEVOutlierContext(ctx, dayMap, weatherByHour, settings, standbyLoad)
	siteMinThresholdKW, siteMinStepKW := inferSiteEVThresholds(ctx, dayMap, evCtx)

	actualAnchorMinKW := siteMinThresholdKW
	if siteMinThresholdKW > EVMinThresholdKW+0.5 {
		actualAnchorMinKW = max(siteMinThresholdKW, evActualAnchorMinAdaptiveKW)
	}

	ignoredOutlierHours := make(map[dayHourKey]bool)
	outlierRefLoad := make(map[dayHourKey]float64)

	// Pass 1: Detect full-hour EV charging sessions and load spikes using site-adaptive thresholds.
	// Both standby-eligible and non-standby hours require a real step above reference load so
	// sustained non-EV household loads during evening/overnight standby windows are not stripped.
	pass1Hours := 0
	actualEVAnchor := make(map[dayHourKey]bool)
	for dateStr, d := range dayMap {
		dayP65, hasDayStats := evCtx.dayP65Map[dateStr]
		dayP75 := evCtx.dayP75Map[dateStr]
		for _, pt := range d.points {
			if pt.HomeKWH < siteMinThresholdKW {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || !evCtx.hasAllRef[hr] {
				continue
			}
			refLoad := evCtx.computePointRefLoad(pt, hr, dayP65, dayP75, hasDayStats)
			limit := max(refLoad, hourlyOutlierFloorKWH) * hourlyOutlierMultiple
			if pt.HomeKWH-refLoad >= siteMinStepKW && pt.HomeKWH > limit {
				key := dayHourKey{date: dateStr, hour: hr}
				if !ignoredOutlierHours[key] {
					pass1Hours++
				}
				ignoredOutlierHours[key] = true
				outlierRefLoad[key] = refLoad
				if pt.HomeKWH >= actualAnchorMinKW {
					actualEVAnchor[key] = true
				}
			}
		}
	}

	// Pass 2: Detect partial-charge shoulder hours (>= 3.2 kWh, step >= 2.0 kWh, > 2.0x refLoad)
	// immediately adjacent (1-hop only, no chaining) to a confirmed Pass 1 actual EV anchor.
	for dateStr, d := range dayMap {
		dayP65, hasDayStats := evCtx.dayP65Map[dateStr]
		dayP75 := evCtx.dayP75Map[dateStr]
		for _, pt := range d.points {
			if pt.HomeKWH < evShoulderMinThresholdKW || pt.HomeKWH >= siteMinThresholdKW {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || !evCtx.hasAllRef[hr] {
				continue
			}
			key := dayHourKey{date: dateStr, hour: hr}
			if ignoredOutlierHours[key] {
				continue
			}
			prevKey, nextKey := adjacentDayHourKeys(d, dateStr, hr)
			if !actualEVAnchor[prevKey] && !actualEVAnchor[nextKey] {
				continue
			}
			refLoad := evCtx.computePointRefLoad(pt, hr, dayP65, dayP75, hasDayStats)
			limit := max(refLoad, hourlyOutlierFloorKWH) * evShoulderMultiple
			if pt.HomeKWH-refLoad >= evShoulderMinStepKW && pt.HomeKWH > limit {
				ignoredOutlierHours[key] = true
				outlierRefLoad[key] = refLoad
			}
		}
	}

	if len(ignoredOutlierHours) > 0 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"ignoring intermittent EV or hourly load spikes in energy model",
			slog.Int("ignoredHours", len(ignoredOutlierHours)),
			slog.Int("pass1Hours", pass1Hours),
			slog.Int("actualEVAnchorHours", len(actualEVAnchor)),
			slog.Int("shoulderHours", len(ignoredOutlierHours)-pass1Hours),
			slog.Float64("siteMinThresholdKW", siteMinThresholdKW),
			slog.Float64("siteMinStepKW", siteMinStepKW),
		)
	}

	return ignoredOutlierHours, outlierRefLoad, evCtx.hourBaselineRef, siteMinThresholdKW, siteMinStepKW
}
