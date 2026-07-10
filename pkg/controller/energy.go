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

// homeLoadPredictionRecencyDecay represents the age decay factor applied exponentially
// as Pow(recencyDecay, ageDays). A value of 0.95 weights a data point from 7 days ago
// at ~70% and 14 days ago at ~50%, giving strong bias to recent household usage patterns.
var homeLoadPredictionRecencyDecay = 0.95

// sameWeekdayWeightMultiplier scales the weight of matching weekdays (e.g. comparing Thursdays to Thursdays)
// by 1.5x. This helps capture weekly recurring activities (e.g., laundry days) without ignoring other recent days.
var sameWeekdayWeightMultiplier = 1.5

// neighborHourWeightMultiplier blends adjacent hours (h-1 and h+1) into the target hour h's prediction pool.
// We tried 0.25 (which reduced cost regression slightly more on normal days) and 0.00 (which completely disabled
// adjacent blending). We chose 0.50 because some blending is necessary to handle time-shifted household loads
// (e.g. if the AC starts at 8:30 AM instead of 9:00 AM on a given day).
var neighborHourWeightMultiplier = 0.5

// tempSimilarityScale acts as the denominator in the exponential temperature similarity function:
// exp(-tempDiff / tempSimilarityScale). We originally evaluated 1.5, but found that raising it to 3.0
// provides much better accuracy (lower MAE) and less overage on normal/spring days, while the gated
// safeguard takes care of protecting the battery during extreme summer heatwaves.
var tempSimilarityScale = 3.0

// defaultStrategyPercentile represents the percentile used for the Default load prediction strategy.
// We originally set this to 65p (65th percentile). However, simulation backtesting showed 65p caused
// a significant cost regression (+23%) on normal days. To prevent overinflating normal spring/fall usage,
// we set this to 50p (median), and handle extreme summer heatwaves using a separate temperature-based boost.
var defaultStrategyPercentile = 0.50

// conservativeStrategyPercentile represents the percentile used for the Conservative strategy.
// We tried 90p, but backtesting showed extreme overpredictions (e.g. predicting 150 kWh vs 57 kWh actual)
// that led to excessive grid charging. 80p provides a robust safety buffer on peak days (missing by only ~5 kWh
// on July 1 on jrollercoasters) without causing completely prohibitive costs.
var conservativeStrategyPercentile = 0.80

// extremeHeatwaveThresholdC represents the temperature threshold above the historical maximum temperature
// seen for a given hour. If today's forecast exceeds the historical maximum plus this threshold, the safeguard is triggered.
var extremeHeatwaveThresholdC = 2.0

// extremeHeatwaveMinTempC represents the minimum forecasted temperature required to trigger the heatwave safeguard.
// This prevents triggering a safeguard boost during cooler seasons (e.g. going from 15°C to 18°C).
var extremeHeatwaveMinTempC = 28.0

// extremeHeatwaveLoadMultiplier represents the safety boost multiplier applied to the predicted load
// when today is an extreme temperature outlier (e.g. 1.20 increases predicted load by 20%).
var extremeHeatwaveLoadMultiplier = 1.20

// loadShiftOutlierIQRExpansion represents the IQR multiplier threshold used to detect
// abnormal daily loads and today's cumulative load shifts (e.g. vacations or visitor stays).
// We ran parameter sweeps across 30 production sites and found that 1.2 provides optimal
// sensitivity to catch real shifts without triggering false positives on normal days.
var loadShiftOutlierIQRExpansion = 1.2

// loadShiftRecencyDecay represents the age decay factor applied exponentially
// when a structural load shift (vacation or visitor stay) has been detected.
// We ran sweeps over 30 production sites and chose 0.30 because it makes yesterday's
// data dominate the prediction profile, allowing the model to adapt within 24-48 hours.
var loadShiftRecencyDecay = 0.30

// loadShiftEscapeHours represents the number of consecutive completed hours
// of non-outlier usage required to early-escape an active load shift.
// We ran sweeps over 30 production sites and selected 4 hours: 1-2 hours is highly
// susceptible to false escapes from appliance cycles, while 4 hours prevents false escapes
// but still exits vacation mode in time for overnight optimizing.
var loadShiftEscapeHours = 4

// recentDiffDetail stores detailed calculation values for a given recent date
// during z-score baseline shift computation, useful for debug log inspection.
type recentDiffDetail struct {
	Date         string    `json:"date"`
	ActualLoad   float64   `json:"actualLoad"`
	ExpectedLoad float64   `json:"expectedLoad"`
	Diff         float64   `json:"diff"`
	SameWDValues []float64 `json:"sameWdValues"`
}

// hourPoint represents an energy load measurement at a specific date
// for a particular hour, keeping track of its source date for debug logs.
type hourPoint struct {
	Date string  `json:"date"`
	Load float64 `json:"load"`
}

type dayPoints struct {
	date   string
	loads  []float64
	points []types.EnergyStats
}

// BuildHourlyEnergyModel averages usage and solar by hour of day from history,
// taking into account weekend days and day-of-the-week differences, while ignoring outlier days
// (e.g. vacation days with significantly below-average usage) and aligning AC temperature baseline calculations.
// It implements an adaptive baseline shifting algorithm that reacts to structural changes in load
// (e.g. guests in town) while remaining robust against intermittent, unpredictable loads (e.g. EV charging).
func (c *Controller) BuildHourlyEnergyModel(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
) (map[int]TimeProfile, types.SimulationParams) {
	loc := now.Location()
	for _, h := range history {
		if !h.TSHourStart.IsZero() {
			loc = h.TSHourStart.Location()
			break
		}
	}

	// Group history by calendar date string (YYYY-MM-DD) in the site's local timezone.
	// This helps us analyze overall daily patterns and compute daily averages.
	dayMap := make(map[string]*dayPoints)

	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		dateStr := h.TSHourStart.In(loc).Format("2006-01-02")

		d, exists := dayMap[dateStr]
		if !exists {
			d = &dayPoints{date: dateStr}
			dayMap[dateStr] = d
		}
		d.points = append(d.points, h)
		// We only consider positive, active loads (> 0.0 KWH) to filter out telemetry drops or empty hours.
		if h.HomeKWH > 0.0 {
			d.loads = append(d.loads, h.HomeKWH)
		}
	}

	// Calculate daily averages for outlier detection.
	// This average represents the baseline consumption profile of each day.
	var dailyAverages []float64
	dayAveragesMap := make(map[string]float64)
	for _, d := range dayMap {
		if len(d.loads) == 0 {
			dayAveragesMap[d.date] = 0.0
			dailyAverages = append(dailyAverages, 0.0)
			continue
		}
		var sum float64
		for _, l := range d.loads {
			sum += l
		}
		avg := sum / float64(len(d.loads))
		dayAveragesMap[d.date] = avg
		dailyAverages = append(dailyAverages, avg)
	}

	todayStr := now.In(loc).Format("2006-01-02")
	yesterdayStr := now.In(loc).AddDate(0, 0, -1).Format("2006-01-02")
	currentHour := now.In(loc).Hour()

	detectedShift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, currentHour)

	// We use the Interquartile Range (IQR) method to identify and filter out anomaly days
	// (e.g. vacation days when usage is abnormally low, or days with extreme charging spikes/events).
	//
	// Why len(dailyAverages) >= 4:
	// Statistically, IQR requires at least 4 data points to calculate meaningful 25th (Q1) and
	// 75th (Q3) percentiles. If we have fewer than 4 days, any attempt to define quartiles will collapse,
	// potentially leading to division by zero or flagging normal variation as outliers.
	validDaysMap := make(map[string]bool)
	if len(dailyAverages) >= 4 {
		sortedAverages := make([]float64, len(dailyAverages))
		copy(sortedAverages, dailyAverages)
		sort.Float64s(sortedAverages)

		n := len(sortedAverages)
		q1Idx := int(math.Round(float64(n-1) * 0.25))
		q3Idx := int(math.Round(float64(n-1) * 0.75))
		q1 := sortedAverages[q1Idx]
		q3 := sortedAverages[q3Idx]
		iqr := q3 - q1

		// Outliers are defined as being outside [Q1 - 1.5 * IQR, Q3 + 1.5 * IQR].
		// This is a robust statistical standard that scales with the natural volatility of the home's consumption.
		lowerBound := q1 - 1.5*iqr
		upperBound := q3 + 1.5*iqr

		for dateStr, avg := range dayAveragesMap {
			if (detectedShift != "none" && (dateStr == todayStr || dateStr == yesterdayStr)) || (avg >= lowerBound && avg <= upperBound) {
				validDaysMap[dateStr] = true
			} else {
				// Debug log now includes the entire dailyAverages dataset and sorted values
				// to allow developers to inspect the distribution and bounds calculation.
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring outlier day in improved model",
					slog.String("date", dateStr),
					slog.Float64("avgHomeLoad", avg),
					slog.Float64("lowerBound", lowerBound),
					slog.Float64("upperBound", upperBound),
					slog.Float64("q1", q1),
					slog.Float64("q3", q3),
					slog.Float64("iqr", iqr),
					slog.Int("totalDays", len(dailyAverages)),
					slog.Any("dailyAverages", dailyAverages),
					slog.Any("sortedAverages", sortedAverages),
				)
			}
		}
	} else {
		// Fallback: If we have fewer than 4 days of history, we cannot establish standard deviation or IQR safely.
		// Therefore, we treat all days as valid and skip IQR filtering.
		for dateStr := range dayAveragesMap {
			validDaysMap[dateStr] = true
		}
	}

	// Calculate solar predictions using existing package functions.
	// CalculateWeatherSolar handles forecasted temperatures and clear sky indices,
	// while CalculateSmoothedSolar is used as a fallback if weather data is unavailable.
	var weatherSolar map[int64]WeatherSolar
	var smoothedSolar map[int]float64

	var params types.SimulationParams

	if len(weather) > 0 {
		locInfo := types.SiteLocation{
			Latitude:  weather[0].Latitude,
			Longitude: weather[0].Longitude,
			TimeZone:  weather[0].TimeLocation,
		}
		weatherSolar, params = CalculateWeatherSolar(ctx, now, history, weather, locInfo)
	} else {
		smoothedSolar = CalculateSmoothedSolar(ctx, now, history, settings)
	}

	// Build mapping of temperature forecast hours for quick access during AC adjustments.
	weatherByHour := make(map[time.Time]float64)
	for _, w := range weather {
		for _, hw := range w.ForecastHours {
			weatherByHour[hw.TSHourStart.UTC()] = hw.TemperatureC
		}
	}

	// Home thermal mass lags behind ambient outdoor temperature. To model AC load response,
	// we use a 3-hour weighted rolling average temperature window:
	// - 30% weight for hour (T-1)
	// - 50% weight for hour (T-2)
	// - 20% weight for hour (T-3)
	// If any of these preceding hours are missing from the forecast, we cannot compute the lag.
	getRollingTemp := func(targetTime time.Time) (float64, bool) {
		t1, ok1 := weatherByHour[targetTime.Add(-1*time.Hour).UTC()]
		t2, ok2 := weatherByHour[targetTime.Add(-2*time.Hour).UTC()]
		t3, ok3 := weatherByHour[targetTime.Add(-3*time.Hour).UTC()]
		if !ok1 || !ok2 || !ok3 {
			return 0, false
		}
		return (0.3 * t1) + (0.5 * t2) + (0.2 * t3), true
	}

	// Find the last 7 valid days in history (reverse chronological order) to evaluate recent trends.
	var sortedValidDates []string
	for dateStr, ok := range validDaysMap {
		if ok {
			sortedValidDates = append(sortedValidDates, dateStr)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(sortedValidDates)))

	var recentValidDates []string
	for _, dateStr := range sortedValidDates {
		if len(recentValidDates) >= 7 {
			break
		}
		recentValidDates = append(recentValidDates, dateStr)
	}

	// We evaluated several options to handle weekend/weekday differences, EV charging spikes, and guests in town:
	//
	// - Option A (Same Weekday Baseline):
	//   Isolates historical days matching the exact target weekday (e.g. all previous Fridays).
	//   * Advantages: Perfect for capturing weekday-specific signatures (e.g. laundry on Fridays, baking on Sundays).
	//   * Drawbacks: Slow to adapt. If a structural shift occurs (e.g., family moves in, raising load from 1.0 KWH to 2.0 KWH),
	//     Option A will average in all old low-usage history, taking several weeks to reflect the new baseline.
	//
	// - Option B (Recent 7 Days average):
	//   Uses the last 7 valid days in history, ignoring day-of-week distinctions.
	//   * Advantages: Adapts instantly (within a few days) to structural load changes.
	//   * Drawbacks: Loses all weekday specificity. A peak that only occurs on Sundays is smeared across all days,
	//     making predictions flat and inaccurate for specific days of the week.
	//
	// - Option C (50/50 Blend of A & B) & Option D (Inverse-Variance Blending):
	//   Combines Option A and Option B linearly (either with static 0.5 weights or weighted by their historical variance).
	//   * Advantages: Represents a mathematical compromise between responsiveness and specificity.
	//   * Drawbacks: Can still dilute day-of-week peaks on normal weeks, and spreads random one-off spikes (like EV charging
	//     on recent days) across all weekday forecasts.
	//
	// - Option E (Z-score Hard Threshold Blend) & Option F (Z-score Smooth Blend):
	//   Computes a z-score of the recent 7-day average relative to the historical weekday baseline. If the z-score indicates
	//   a significant deviation, it blends Option B in to adapt.
	//   * Advantages: Avoids diluting the weekday profile during normal operations.
	//   * Drawbacks: Blending Option B directly still compromises the hourly "shape" of the forecast with the flat 7-day average.
	//     Hard thresholds (Option E) can cause sudden jumps (jitter) in consecutive model runs.
	//
	// - Option G (Adaptive Shifted Option A - Selected):
	//   We use Option A (Same Weekday) as our base shape to preserve sharp, hour-specific peaks. We then calculate a flat
	//   constant shift (offset) to apply to the entire day if recent loads have deviated.
	//   To do this, we compute the daily average difference (actual - expected) over the last 4 valid days.
	//   * Advantages:
	//     1. Retains the high-fidelity hourly profile shape (e.g. Friday laundry peak is kept at the correct hour).
	//     2. Adapts quickly to structural changes by shifting the entire profile up or down.
	//     3. Immune to intermittent EV charging spikes. Because an EV charge occurs only on some days, it increases
	//        the standard deviation (variance) of the differences. In the formula: zScore = meanDiff / stdDevDiff,
	//        a larger standard deviation shrinks the z-score, keeping it below our threshold and preventing a false shift.
	//
	// We calculate the difference between actual daily averages and their respective weekday baseline expectations
	// for the last 4 valid days. If these differences are consistent (low variance), the standard deviation remains small,
	// yielding a high z-score. If they are volatile (high variance due to random EV charging), the standard deviation is large,
	// yielding a low z-score.
	var diffs []float64
	var recentDiffDetails []recentDiffDetail

	for i := 0; i < len(recentValidDates) && i < 4; i++ {
		dStr := recentValidDates[i]
		actualLoad := dayAveragesMap[dStr]
		dTime, err := time.ParseInLocation("2006-01-02", dStr, loc)
		if err == nil {
			dWD := dTime.Weekday()
			var otherVals []float64
			for otherDateStr, ok := range validDaysMap {
				// Compare this day to all OTHER days of the same weekday in history to establish baseline expectation.
				// We exclude the day itself to prevent self-bias.
				if ok && otherDateStr != dStr {
					oTime, err2 := time.ParseInLocation("2006-01-02", otherDateStr, loc)
					if err2 == nil && oTime.Weekday() == dWD {
						otherVals = append(otherVals, dayAveragesMap[otherDateStr])
					}
				}
			}
			if len(otherVals) > 0 {
				var sumOther float64
				for _, ov := range otherVals {
					sumOther += ov
				}
				expectedLoad := sumOther / float64(len(otherVals))
				diff := actualLoad - expectedLoad
				diffs = append(diffs, diff)

				recentDiffDetails = append(recentDiffDetails, recentDiffDetail{
					Date:         dStr,
					ActualLoad:   actualLoad,
					ExpectedLoad: expectedLoad,
					Diff:         diff,
					SameWDValues: otherVals,
				})
			}
		}
	}

	meanDiff := 0.0
	if len(diffs) > 0 {
		var sumDiff float64
		for _, diff := range diffs {
			sumDiff += diff
		}
		meanDiff = sumDiff / float64(len(diffs))
	}

	// stdDevDiff measures volatility. If load changes consistently (family in town), stdDevDiff is small, z-score is high.
	stdDevDiff := getStdDev(diffs)

	zScoreG := meanDiff / stdDevDiff
	// We scale the shift smoothly starting from z-score 1.2 (ignored noise) up to 2.2 (fully applied).
	// This prevents threshold jitter.
	scaleG := math.Min(1.0, math.Max(0.0, (math.Abs(zScoreG)-1.2)*1.0))
	appliedShift := meanDiff * scaleG

	// Debug logs are enriched with recentValidDates, validDaysCount, and full recentDiffDetails
	// to allow developers to audit how the z-score and baseline shift are derived.
	log.Ctx(ctx).DebugContext(
		ctx,
		"calculated adaptive load shift in improved model",
		slog.Float64("meanDiff", meanDiff),
		slog.Float64("stdDevDiff", stdDevDiff),
		slog.Float64("zScore", zScoreG),
		slog.Float64("scale", scaleG),
		slog.Float64("appliedShift", appliedShift),
		slog.Int("diffsCount", len(diffs)),
		slog.Any("diffs", diffs),
		slog.Any("recentValidDates", recentValidDates),
		slog.Int("validDaysCount", len(validDaysMap)),
		slog.Any("recentDiffDetails", recentDiffDetails),
	)

	result := make(map[int]TimeProfile)

	// Predict hourly profile for each hour of the upcoming 24-hour cycle.
	for h := 0; h < 24; h++ {
		var targetTime time.Time
		tCur := now.In(loc)
		for i := 0; i < 24; i++ {
			if tCur.Hour() == h {
				targetTime = tCur.Truncate(time.Hour)
				break
			}
			tCur = tCur.Add(time.Hour)
		}
		if targetTime.IsZero() {
			continue
		}
		wd := targetTime.Weekday()

		// We attempt to gather historical days matching the exact weekday (Option A).
		//
		// Why len(selectedDayDatesA) < 3:
		// If we have fewer than 3 matching weekdays (e.g., we only have 1 or 2 Fridays in history),
		// the average is highly susceptible to single-day noise.
		//
		// Cascade:
		// 1. Same Weekday (ideal).
		// 2. Weekend vs Weekday Grouping (if < 3 same weekdays, we pull in all weekend days if target is weekend,
		//    or all weekdays if target is weekday).
		// 3. All Valid Days (if weekend/weekday group still has < 3 days).
		var selectedDayDatesA []string
		for dateStr := range validDaysMap {
			dayTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
			if err == nil && dayTime.Weekday() == wd {
				selectedDayDatesA = append(selectedDayDatesA, dateStr)
			}
		}
		if len(selectedDayDatesA) < 3 {
			var fallbackDates []string
			isWeekend := wd == time.Saturday || wd == time.Sunday
			for dateStr := range validDaysMap {
				dayTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
				if err == nil {
					wday := dayTime.Weekday()
					fallbackWeekend := wday == time.Saturday || wday == time.Sunday
					if isWeekend == fallbackWeekend {
						fallbackDates = append(fallbackDates, dateStr)
					}
				}
			}
			if len(fallbackDates) >= 3 {
				selectedDayDatesA = fallbackDates
			} else {
				var allValidDates []string
				for dateStr := range validDaysMap {
					allValidDates = append(allValidDates, dateStr)
				}
				selectedDayDatesA = allValidDates
			}
		}

		// Gather historical energy data points for hour h on the selected days,
		// preserving the date metadata for detailed outlier logging.
		var pointsA []hourPoint
		for _, dateStr := range selectedDayDatesA {
			d := dayMap[dateStr]
			if d == nil {
				continue
			}
			for _, pt := range d.points {
				if pt.TSHourStart.In(loc).Hour() == h {
					pointsA = append(pointsA, hourPoint{Date: dateStr, Load: pt.HomeKWH})
				}
			}
		}

		floor := settings.IgnoreHourUsageFloorKWH
		if floor == 0 {
			floor = 0.5
		}

		// Filter points and log ignored outliers using pairwise comparison.
		// NOTE: Pairwise outlier filtering is disabled as it is redundant under the new weighted
		// percentile (50p/median) model. The median is naturally robust against single-point load spikes
		// (e.g. oven, EV charging), whereas filtering runs the risk of discarding genuine heatwave A/C load spikes.
		var validPointsA []hourPoint
		if false && len(pointsA) >= 3 && settings.IgnoreHourUsageOverMultiple > 1 {
			var outlierIdx []int
			for i, p := range pointsA {
				isOutlier := true
				for j, other := range pointsA {
					if i == j {
						continue
					}
					limit := math.Max(other.Load, floor) * settings.IgnoreHourUsageOverMultiple
					if p.Load <= limit {
						isOutlier = false
						break
					}
				}
				if isOutlier {
					outlierIdx = append(outlierIdx, i)
				}
			}

			if len(outlierIdx) == 1 {
				// We found exactly one outlier, ignore it
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring hourly outlier data point in improved model",
					slog.Int("hour", h),
					slog.String("weekday", wd.String()),
					slog.String("date", pointsA[outlierIdx[0]].Date),
					slog.Float64("outlierLoad", pointsA[outlierIdx[0]].Load),
					slog.Float64("floor", floor),
					slog.Float64("multiple", settings.IgnoreHourUsageOverMultiple),
					slog.Any("rawPoints", pointsA),
				)
				validPointsA = make([]hourPoint, 0, len(pointsA)-1)
				for i, pt := range pointsA {
					if i != outlierIdx[0] {
						validPointsA = append(validPointsA, pt)
					}
				}
			} else {
				validPointsA = pointsA
			}
		} else {
			validPointsA = pointsA
		}

		// Keep track of any matching-weekday date that was excluded as an hourly outlier
		excludedDates := make(map[string]bool)
		if len(pointsA) != len(validPointsA) {
			for _, p := range pointsA {
				found := false
				for _, vp := range validPointsA {
					if vp.Date == p.Date {
						found = true
						break
					}
				}
				if !found {
					excludedDates[p.Date] = true
					break
				}
			}
		}

		// 1. Build de-duplicated set of dates combining matching weekdays and recent 7 days.
		// De-duplicating via map ensures that matching weekdays which also happen to fall
		// within the last week are not double-counted (which would skew weight percentiles).
		selectedDatesMap := make(map[string]bool)
		for _, dateStr := range selectedDayDatesA {
			selectedDatesMap[dateStr] = true
		}
		for _, dateStr := range recentValidDates {
			selectedDatesMap[dateStr] = true
		}

		// 2. Build temperature lookup map
		tempByHour := make(map[time.Time]float64)
		for _, w := range weather {
			for _, hw := range w.ForecastHours {
				tempByHour[hw.TSHourStart.UTC()] = hw.TemperatureC
			}
		}

		// Target weather time and temperature for current hour h
		targetTimeHr := time.Date(targetTime.Year(), targetTime.Month(), targetTime.Day(), h, 0, 0, 0, loc).UTC()
		targetTemp, hasTargetTemp := tempByHour[targetTimeHr]

		// 3. Gather points at hours h, h-1, h+1 on all selected dates.
		// We gather neighboring hours to account for daily variations in household activity timing.
		var pts []weightedPoint
		maxHistTemp := -999.0
		hasHistTempForHour := false

		for dateStr := range selectedDatesMap {
			d := dayMap[dateStr]
			if d == nil {
				continue
			}
			dTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
			if err != nil {
				continue
			}
			ageDays := int(targetTime.Sub(dTime).Hours() / 24)
			if ageDays < 0 {
				ageDays = 0
			}

			// Base weight based on age decay
			decayFactor := homeLoadPredictionRecencyDecay
			if detectedShift != "none" {
				decayFactor = loadShiftRecencyDecay
			}
			baseWeight := math.Pow(decayFactor, float64(ageDays))

			// Apply same-weekday weight multiplier boost
			if dTime.Weekday() == wd {
				baseWeight *= sameWeekdayWeightMultiplier
			}

			for _, pt := range d.points {
				if pt.HomeKWH <= 0.0 {
					continue
				}
				hpLocalHour := pt.TSHourStart.In(loc).Hour()

				// Calculate hour position weight multiplier.
				// We blend adjacent hours (h-1, h+1) with a 0.50 multiplier, and the exact hour h with 1.0.
				// We use modulo arithmetic ((h - 1 + 24) % 24) to properly wrap boundary conditions at midnight/noon.
				mult := 0.0
				if hpLocalHour == h {
					// Check if this point was excluded as an hourly outlier during same-weekday outlier filtering
					if excludedDates[dateStr] {
						continue
					}
					mult = 1.0
				} else {
					prevHr := (h - 1 + 24) % 24
					nextHr := (h + 1) % 24
					if hpLocalHour == prevHr || hpLocalHour == nextHr {
						mult = neighborHourWeightMultiplier
					}
				}

				if mult > 0 {
					finalWeight := baseWeight * mult

					// Apply temperature similarity weight.
					// Compares forecasted temperature for target hour h with actual temperature at historical point.
					// Narrows similarity scale to penalize temperature deviations exponentially,
					// ensuring historical cool days get virtually zero weight during a hot summer heatwave.
					if hasTargetTemp {
						ptTime := time.Date(dTime.Year(), dTime.Month(), dTime.Day(), hpLocalHour, 0, 0, 0, loc).UTC()
						if histTemp, hasHistTemp := tempByHour[ptTime]; hasHistTemp {
							tempDiff := math.Abs(targetTemp - histTemp)
							finalWeight *= math.Exp(-tempDiff / tempSimilarityScale)

							// Track the maximum temperature in history for this exact hour
							if hpLocalHour == h {
								if histTemp > maxHistTemp {
									maxHistTemp = histTemp
									hasHistTempForHour = true
								}
							}
						}
					}

					pts = append(pts, weightedPoint{
						Value:  pt.HomeKWH,
						Weight: finalWeight,
					})
				}
			}
		}

		// 4. Compute weighted percentile of the gathered points
		pct := defaultStrategyPercentile
		if settings.HomeLoadPredictionStrategy == "conservative" {
			pct = conservativeStrategyPercentile
		}
		avgLoadA := getWeightedPercentile(pts, pct)

		// Apply extreme heatwave safeguard: if today's forecasted temp is at least extremeHeatwaveThresholdC
		// hotter than the hottest temperature seen in history for this hour, AND is above the minimum hot-day threshold
		// (extremeHeatwaveMinTempC), apply the safety boost to protect the battery.
		if hasTargetTemp && hasHistTempForHour && targetTemp > maxHistTemp+extremeHeatwaveThresholdC && targetTemp > extremeHeatwaveMinTempC {
			avgLoadA *= extremeHeatwaveLoadMultiplier
		}

		// We adjust baseline home load based on temperature deviations from the historical baseline.
		//
		// Gating conditions:
		// 1. AC settings are active (increase percent > 0, max increase > 0).
		// 2. Weather forecast is available.
		// 3. We have at least minRequiredTemps historical rolling temperature readings for the same hour.
		//
		// Why we restrict past temperatures to selectedDayDatesA:
		// To align the temperature baseline exactly with the load prediction days. If we are predicting for
		// a Sunday, the historical temperature baseline should be calculated ONLY from Sundays, preventing
		// weekday temperature profiles from distorting the reference point.
		//
		// Rationale for Two-Way Correction:
		// Historically, the model acted as a one-way ratchet (only increasing load if today was hotter).
		// If history contained a heatwave and today is a cold front, we would over-predict load.
		// We now apply correction in both directions:
		// - Shifting load up if today is hotter (AC runs more).
		// - Shifting load down if today is colder (AC runs less or is off).
		// We define a 1.0°C deadband to avoid making micro-adjustments for negligible temp differences.
		// We enforce a safety cap (max 50% decrease or settings.ACUsageMaxIncreasePercent) to ensure we do
		// not over-reduce the load, maintaining a realistic home standby/base load profile.
		avgLoadAACAdj := avgLoadA
		if settings.ACUsageIncreasePercentPerDegree > 0 && settings.ACUsageMaxIncreasePercent > 0 && len(weather) > 0 {
			todayTemp, hasTodayTemp := getRollingTemp(targetTime)

			var pastTemps []float64
			for _, dateStr := range selectedDayDatesA {
				pastDayTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
				if err == nil {
					pastHourTime := time.Date(pastDayTime.Year(), pastDayTime.Month(), pastDayTime.Day(), h, 0, 0, 0, loc)
					if ptemp, ok := getRollingTemp(pastHourTime); ok {
						pastTemps = append(pastTemps, ptemp)
					}
				}
			}

			minRequiredTemps := 3
			if len(selectedDayDatesA) < minRequiredTemps {
				minRequiredTemps = len(selectedDayDatesA)
			}
			if minRequiredTemps < 2 {
				minRequiredTemps = 2
			}

			if hasTodayTemp && len(pastTemps) >= minRequiredTemps {
				var sumPastTemp float64
				for _, t := range pastTemps {
					sumPastTemp += t
				}
				baselineTemp := sumPastTemp / float64(len(pastTemps))

				const tempDeadband = 3.0

				if todayTemp > baselineTemp+tempDeadband {
					// Hotter today than historical baseline -> scale load up
					if todayTemp > settings.ACBaseTemperatureC {
						effInc := todayTemp - math.Max(baselineTemp, settings.ACBaseTemperatureC)
						if effInc > 0 {
							ratio := (settings.ACUsageIncreasePercentPerDegree / 100.0) * effInc
							maxRatio := settings.ACUsageMaxIncreasePercent / 100.0
							if ratio > maxRatio {
								ratio = maxRatio
							}
							avgLoadAACAdj = avgLoadA + (avgLoadA * ratio)
						}
					}
				} else if todayTemp < baselineTemp-tempDeadband {
					// Cooler today than historical baseline -> scale load down (AC is running less/off)
					if baselineTemp > settings.ACBaseTemperatureC {
						effDec := baselineTemp - math.Max(todayTemp, settings.ACBaseTemperatureC)
						if effDec > 0 {
							ratio := (settings.ACUsageIncreasePercentPerDegree / 100.0) * effDec
							// Safety limit: cap reduction at 50% or the configured max increase percent
							maxDecRatio := math.Min(0.5, settings.ACUsageMaxIncreasePercent/100.0)
							if ratio > maxDecRatio {
								ratio = maxDecRatio
							}
							avgLoadAACAdj = avgLoadA - (avgLoadA * ratio)
						}
					}
				}
			}
		}

		// Solar prediction.
		// WeatherSolar uses forecasted values (preferred), falling back to SmoothedSolar if forecast is absent.
		avgSolar := 0.0
		if len(weather) > 0 {
			if ws, ok := weatherSolar[targetTime.Truncate(time.Hour).Unix()]; ok {
				avgSolar = ws.ImprovedSolar
			}
		} else {
			avgSolar = smoothedSolar[h]
		}

		// Apply the adaptive shift derived via Option G.
		// We enforce a minimum load floor of 0.1 KWH to avoid predicting negative or zero usage.
		finalHomeLoadACAdj := math.Max(0.1, avgLoadAACAdj+appliedShift)

		result[h] = TimeProfile{
			Hour:           h,
			AvgSolarKWH:    avgSolar,
			AvgHomeLoadKWH: finalHomeLoadACAdj,
		}
	}

	return result, params
}

func getStdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0.1
	}
	var sum float64
	for _, val := range values {
		sum += val
	}
	mean := sum / float64(len(values))
	var sumSqDiff float64
	for _, val := range values {
		sumSqDiff += (val - mean) * (val - mean)
	}
	std := math.Sqrt(sumSqDiff / float64(len(values)-1))
	if std < 0.1 {
		return 0.1
	}
	return std
}

type weightedPoint struct {
	Value  float64
	Weight float64
}

// getWeightedPercentile calculates the percentile value from a slice of weighted data points.
// Unlike a standard percentile, each point is assigned a fractional rank based on its cumulative weight.
// This is critical for our age-decayed and temperature-scaled load points:
// 1. Sort points in ascending order of value.
// 2. Compute the mid-point percentile rank for each sorted value: p_i = (cumWeight_prev + 0.5 * weight_i) / totalWeight.
// 3. Find the interval [k, k+1] containing the target percentile.
// 4. Perform linear interpolation between points[k] and points[k+1] to estimate the percentile value.
func getWeightedPercentile(points []weightedPoint, percentile float64) float64 {
	if len(points) == 0 {
		return 0.0
	}
	if len(points) == 1 {
		return points[0].Value
	}

	// Sort by value ascending
	sort.Slice(points, func(i, j int) bool {
		return points[i].Value < points[j].Value
	})

	var totalWeight float64
	for _, p := range points {
		totalWeight += p.Weight
	}
	if totalWeight == 0 {
		return points[0].Value
	}

	// Compute mid-points pList representing the percentile rank boundary of each sorted point.
	// Each point's percentile rank corresponds to the cumulative weight up to its midpoint.
	pList := make([]float64, len(points))
	var cumWeight float64
	for i, pt := range points {
		pList[i] = (cumWeight + 0.5*pt.Weight) / totalWeight
		cumWeight += pt.Weight
	}

	// If target percentile falls below or above the range of the midpoints, clamp to boundaries.
	if percentile <= pList[0] {
		return points[0].Value
	}
	n := len(points)
	if percentile >= pList[n-1] {
		return points[n-1].Value
	}

	// Linearly interpolate between the two matching points in the interval
	for k := 0; k < n-1; k++ {
		if pList[k] <= percentile && percentile <= pList[k+1] {
			ratio := (percentile - pList[k]) / (pList[k+1] - pList[k])
			return points[k].Value + ratio*(points[k+1].Value-points[k].Value)
		}
	}

	return points[n-1].Value
}

// detectLoadShift automatically identifies if the house is undergoing a structural
// change in energy consumption patterns (such as a vacation mode / shift down, or visitor
// mode / shift up) using yesterday's daily total and today's cumulative morning sum.
func detectLoadShift(
	ctx context.Context,
	now time.Time,
	loc *time.Location,
	dayMap map[string]*dayPoints,
	dayAveragesMap map[string]float64,
	todayStr string,
	yesterdayStr string,
	currentHour int,
) string {
	const dailyAveragesRequired = 4
	// Identify baseline days of normal occupancy (excluding yesterday, today, and past daily outliers).
	// Yesterday and today are excluded because they represent the potential active shift period.
	// Filtering out daily average outliers from history ensures we compare today's usage against
	// a clean representation of the home's "normal occupancy" days.
	var dailyAveragesForBaseline []float64
	for dateStr, avg := range dayAveragesMap {
		if dateStr != todayStr && dateStr != yesterdayStr {
			dailyAveragesForBaseline = append(dailyAveragesForBaseline, avg)
		}
	}

	var dailyLowerBound, dailyUpperBound float64
	var baselineDays []string

	if len(dailyAveragesForBaseline) < dailyAveragesRequired {
		return "none"
	}

	sorted := make([]float64, len(dailyAveragesForBaseline))
	copy(sorted, dailyAveragesForBaseline)
	sort.Float64s(sorted)
	n := len(sorted)
	q1 := sorted[int(math.Round(float64(n-1)*0.25))]
	q3 := sorted[int(math.Round(float64(n-1)*0.75))]
	iqr := q3 - q1
	dailyLowerBound = q1 - loadShiftOutlierIQRExpansion*iqr
	dailyUpperBound = q3 + loadShiftOutlierIQRExpansion*iqr

	for dateStr, avg := range dayAveragesMap {
		if dateStr != todayStr && dateStr != yesterdayStr {
			if avg >= dailyLowerBound && avg <= dailyUpperBound {
				baselineDays = append(baselineDays, dateStr)
			}
		}
	}

	if len(baselineDays) < dailyAveragesRequired {
		log.Ctx(ctx).DebugContext(
			ctx,
			"missing enough baseline days for load shift detection",
			slog.Int("currentHour", currentHour),
			slog.Float64("dailyLowerBound", dailyLowerBound),
			slog.Float64("dailyUpperBound", dailyUpperBound),
			slog.Any("baselineDays", baselineDays),
		)
		return "none"
	}

	// Yesterday is the first completed day of the suspected shift.
	// Verifying that yesterday was a daily outlier is a prerequisite for triggering a shift.
	// This prevents transient, one-off events (like a single day-trip away) from triggering a shift,
	// since structural changes (vacations or guest stays) require consecutive outlier days.
	yAvg, yExists := dayAveragesMap[yesterdayStr]
	yesterdayIsLow := yExists && yAvg < dailyLowerBound
	yesterdayIsHigh := yExists && yAvg > dailyUpperBound
	detectedShift := "none"

	// Compute hourly metrics (Q1 and Q3) across baseline days for early escape checks.
	// Q1 (25th percentile) and Q3 (75th percentile) define the boundaries of normal occupancy hourly loads.
	hourQ1s := make(map[int]float64)
	hourQ3s := make(map[int]float64)
	for h := 0; h < 24; h++ {
		var hourLoads []float64
		for _, dStr := range baselineDays {
			if d, ok := dayMap[dStr]; ok {
				for _, pt := range d.points {
					if pt.TSHourStart.In(loc).Hour() == h && pt.HomeKWH > 0 {
						hourLoads = append(hourLoads, pt.HomeKWH)
					}
				}
			}
		}
		if len(hourLoads) >= dailyAveragesRequired-1 {
			sort.Float64s(hourLoads)
			n := len(hourLoads)
			hourQ1s[h] = hourLoads[int(math.Round(float64(n-1)*0.25))]
			hourQ3s[h] = hourLoads[int(math.Round(float64(n-1)*0.75))]
		} else {
			hourQ1s[h] = 0.0
			hourQ3s[h] = 999.0
		}
	}

	// We split today's verification logic by run time (before/after 9:00 AM):
	//
	// 1. After 9:00 AM (currentHour >= 9):
	//    We have enough active daytime hours to compute a robust cumulative sum (from 7:00 AM to currentHour-1).
	//    Comparing today's cumulative sum to historical baseline sums over the exact same hour window
	//    filters out hourly load spikes (e.g. dryer cycles) and telemetry dropouts.
	//
	// 2. Before 9:00 AM (currentHour < 9 - Early Morning):
	//    There is too little morning data to form a reliable cumulative sum. We instead check each hour so far
	//    today against the historical hourly baseline medians to confirm that no load contradicts the shift direction
	//    (e.g., no high charging load during a vacation morning, no low load during a visitor morning).
	if currentHour >= 9 {
		var todaySum float64
		var todayCount int
		if todayPts, ok := dayMap[todayStr]; ok {
			for _, pt := range todayPts.points {
				h := pt.TSHourStart.In(loc).Hour()
				if h >= 7 && h < currentHour {
					todaySum += pt.HomeKWH
					todayCount++
				}
			}
		}

		if todayCount > 0 {
			var baselineSums []float64
			for _, dStr := range baselineDays {
				var bSum float64
				if d, ok := dayMap[dStr]; ok {
					for _, pt := range d.points {
						h := pt.TSHourStart.In(loc).Hour()
						if h >= 7 && h < currentHour {
							bSum += pt.HomeKWH
						}
					}
				}
				baselineSums = append(baselineSums, bSum)
			}

			sort.Float64s(baselineSums)
			n := len(baselineSums)
			q1 := baselineSums[int(math.Round(float64(n-1)*0.25))]
			q3 := baselineSums[int(math.Round(float64(n-1)*0.75))]
			iqr := q3 - q1
			effIQR := math.Max(iqr, 0.1)

			sumLowerBound := q1 - loadShiftOutlierIQRExpansion*effIQR
			sumUpperBound := q3 + loadShiftOutlierIQRExpansion*effIQR

			if yesterdayIsLow && todaySum < sumLowerBound {
				detectedShift = "down"
			} else if yesterdayIsHigh && todaySum > sumUpperBound {
				detectedShift = "up"
			}

			// Apply 4-Hour Early Escape Override:
			// If a user returns home from vacation at e.g. 5:00 PM, today's cumulative sum will remain low
			// for the rest of the day due to the many low hours earlier. This would trap the model in vacation mode
			// through the evening and night, failing to charge the battery overnight.
			//
			// To solve this, we exit the shift mode early if the last 4 complete hours return to normal occupancy levels:
			// - For Vacation Escape: All 4 hours are >= hourQ1s[checkHour] (not a low outlier).
			//   Using Q1 (25th percentile) instead of the lower bound is critical because standby load on vacation
			//   (0.4 - 0.7 kWh/hr) is consistently below Q1, but can easily hover above the absolute lower bound
			//   (which can approach zero), causing false escapes.
			// - For Visitor Escape: All 4 hours are <= hourQ3s[checkHour] (not a high outlier).
			// - Requiring 4 consecutive hours filters out transient noise (e.g. water heater cycles during vacation).
			if detectedShift != "none" {
				escape := true
				var hourLoad float64
				var comparisonHourLoad float64
				for i := 1; i <= loadShiftEscapeHours; i++ {
					checkHour := currentHour - i
					var found bool
					if todayPts, ok := dayMap[todayStr]; ok {
						for _, pt := range todayPts.points {
							h := pt.TSHourStart.In(loc).Hour()
							if h == checkHour {
								hourLoad = pt.HomeKWH
								found = true
								break
							}
						}
					}
					if !found {
						escape = false
						break
					}

					if detectedShift == "down" {
						comparisonHourLoad = hourQ1s[checkHour]
						if hourLoad < comparisonHourLoad {
							escape = false
							break
						}
					} else if detectedShift == "up" {
						comparisonHourLoad = hourQ3s[checkHour]
						if hourLoad > comparisonHourLoad {
							escape = false
							break
						}
					}
				}
				if escape {
					log.Ctx(ctx).DebugContext(
						ctx,
						"early load shift escape",
						slog.String("shiftType", detectedShift),
						slog.Int("currentHour", currentHour),
						slog.Bool("yesterdayIsLow", yesterdayIsLow),
						slog.Bool("yesterdayIsHigh", yesterdayIsHigh),
						slog.Float64("todaySum", todaySum),
						slog.Int("todayCount", todayCount),
						slog.Float64("sumLowerBound", sumLowerBound),
						slog.Float64("sumUpperBound", sumUpperBound),
						slog.Any("baselineSums", baselineSums),
						slog.Float64("hourLoad", hourLoad),
						slog.Float64("comparisonHourLoad", comparisonHourLoad),
						slog.Float64("dailyLowerBound", dailyLowerBound),
						slog.Float64("dailyUpperBound", dailyUpperBound),
						slog.Any("baselineDays", baselineDays),
					)
					detectedShift = "none"
				}
			}

			if detectedShift != "none" {
				log.Ctx(ctx).InfoContext(
					ctx,
					"detected load shift, applying decay factor shift",
					slog.String("shiftType", detectedShift),
					slog.Float64("decayFactor", loadShiftRecencyDecay),
					slog.Int("currentHour", currentHour),
					slog.Bool("yesterdayIsLow", yesterdayIsLow),
					slog.Bool("yesterdayIsHigh", yesterdayIsHigh),
					slog.Float64("todaySum", todaySum),
					slog.Int("todayCount", todayCount),
					slog.Float64("sumLowerBound", sumLowerBound),
					slog.Float64("sumUpperBound", sumUpperBound),
					slog.Any("baselineSums", baselineSums),
					slog.Float64("dailyLowerBound", dailyLowerBound),
					slog.Float64("dailyUpperBound", dailyUpperBound),
					slog.Any("baselineDays", baselineDays),
				)
			}
		}
	} else if currentHour < 9 && (yesterdayIsLow || yesterdayIsHigh) {
		tIsLow := yesterdayIsLow
		tIsHigh := yesterdayIsHigh

		const lookbackHours = 6

		// Gather actual loads of the last 6 completed hours (which can roll back into yesterday).
		// This prevents false shift detections at midnight/early morning when today has 0 or 1 hours of data,
		// and ensures we check a continuous sliding window of recent hours.
		var lastLoads []struct {
			Hour int
			Load float64
		}
		for i := 1; i <= lookbackHours; i++ {
			checkTime := now.In(loc).Add(time.Duration(-i) * time.Hour)
			checkDateStr := checkTime.Format("2006-01-02")
			checkHour := checkTime.Hour()

			var foundLoad float64
			var found bool
			if d, ok := dayMap[checkDateStr]; ok {
				for _, pt := range d.points {
					if pt.TSHourStart.In(loc).Hour() == checkHour {
						foundLoad = pt.HomeKWH
						found = true
						break
					}
				}
			}
			if found {
				lastLoads = append(lastLoads, struct {
					Hour int
					Load float64
				}{Hour: checkHour, Load: foundLoad})
			}
		}

		// Compute historical medians for the hours we need to verify.
		hourMedians := make(map[int]float64)
		for _, pt := range lastLoads {
			h := pt.Hour
			if _, ok := hourMedians[h]; !ok {
				var hLoads []float64
				for _, dStr := range baselineDays {
					if d, ok2 := dayMap[dStr]; ok2 {
						for _, bPt := range d.points {
							if bPt.TSHourStart.In(loc).Hour() == h && bPt.HomeKWH > 0 {
								hLoads = append(hLoads, bPt.HomeKWH)
							}
						}
					}
				}
				// require at least 2 days to compute a median
				if len(hLoads) > 1 {
					sort.Float64s(hLoads)
					hourMedians[h] = hLoads[len(hLoads)/2]
				} else {
					hourMedians[h] = 0.0
				}
			}
		}

		// If we have less than x-1 hours of recent data, we cannot reliably confirm the early morning hours,
		// so we do not trigger any shift.
		if len(lastLoads) < lookbackHours-1 {
			tIsLow = false
			tIsHigh = false
		} else {
			// Verify each of the last x-1 completed hours to ensure they do not contradict the active shift:
			// - Vacation (tIsLow): If any hour exceeds 1.5x the median, it indicates active occupancy.
			// - Visitor (tIsHigh): If any hour falls below 0.5x the median, it indicates low occupancy.
			var numMedians int
			for _, pt := range lastLoads {
				h := pt.Hour
				if med, hasMed := hourMedians[h]; hasMed && med > 0 {
					numMedians++
					if tIsLow && pt.Load > med*1.5 {
						tIsLow = false
					}
					if tIsHigh && pt.Load < med*0.5 {
						tIsHigh = false
					}
				}
			}
			if numMedians < lookbackHours-1 {
				tIsLow = false
				tIsHigh = false
			}
		}

		if tIsLow {
			detectedShift = "down"
		} else if tIsHigh {
			detectedShift = "up"
		}

		if detectedShift != "none" {
			log.Ctx(ctx).InfoContext(
				ctx,
				"detected continued load shift, applying decay factor shift",
				slog.String("shiftType", detectedShift),
				slog.Float64("decayFactor", loadShiftRecencyDecay),
				slog.Int("currentHour", currentHour),
				slog.Bool("yesterdayIsLow", yesterdayIsLow),
				slog.Bool("yesterdayIsHigh", yesterdayIsHigh),
				slog.Any("last6Loads", lastLoads),
				slog.Float64("dailyLowerBound", dailyLowerBound),
				slog.Float64("dailyUpperBound", dailyUpperBound),
			)
		}
	}

	return detectedShift
}
