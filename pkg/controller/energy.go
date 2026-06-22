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

// BuildImprovedHourlyEnergyModel averages usage and solar by hour of day from history,
// taking into account weekend days and day-of-the-week differences, while ignoring outlier days
// (e.g. vacation days with significantly below-average usage) and aligning AC temperature baseline calculations.
// It implements an adaptive baseline shifting algorithm that reacts to structural changes in load
// (e.g. guests in town) while remaining robust against intermittent, unpredictable loads (e.g. EV charging).
func (c *Controller) BuildImprovedHourlyEnergyModel(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
) map[int]TimeProfile {
	loc := now.Location()
	for _, h := range history {
		if !h.TSHourStart.IsZero() {
			loc = h.TSHourStart.Location()
			break
		}
	}

	// Group history by calendar date string (YYYY-MM-DD) in the site's local timezone.
	// This helps us analyze overall daily patterns and compute daily averages.
	type dayData struct {
		date   string
		loads  []float64
		points []types.EnergyStats
	}
	dayMap := make(map[string]*dayData)

	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		dateStr := h.TSHourStart.In(loc).Format("2006-01-02")

		d, exists := dayMap[dateStr]
		if !exists {
			d = &dayData{date: dateStr}
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
			if avg >= lowerBound && avg <= upperBound {
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

	if len(weather) > 0 {
		locInfo := types.SiteLocation{
			Latitude:  weather[0].Latitude,
			Longitude: weather[0].Longitude,
			TimeZone:  weather[0].TimeLocation,
		}
		weatherSolar, _ = CalculateWeatherSolar(ctx, now, history, weather, locInfo)
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

		// We compare hourly loads pairwise within the same-weekday subset to filter out unpredictable spikes (e.g. EV charging).
		//
		// How the limit is calculated for each pair:
		// limit = Max(other.Load, floor) * settings.IgnoreHourUsageOverMultiple
		// A point is flagged as an outlier only if its load exceeds the limit of every other point in the set.
		//
		// Why we use Max(other.Load, floor):
		// For low-usage hours (e.g., 0.01 KWH overnight), multiplying by 3.0 gives 0.03 KWH.
		// A minor load of 0.04 KWH would be falsely flagged as an outlier.
		// Introducing a floor (default 0.5 KWH) prevents over-filtering during low-usage baseline hours.
		//
		// Why we require len(pointsA) >= 3:
		// We cannot statistically define an "outlier" if we have fewer than 3 comparison points.
		floor := settings.IgnoreHourUsageFloorKWH
		if floor == 0 {
			floor = 0.5
		}

		// Filter points and log ignored outliers using pairwise comparison.
		// A point is an outlier only if it is greater than math.Max(other.Load, floor) * multiple
		// for every other point in pointsA, and we only ignore it if there is exactly one outlier.
		var validPointsA []float64
		if len(pointsA) >= 3 && settings.IgnoreHourUsageOverMultiple > 1 {
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
				validPointsA = make([]float64, 0, len(pointsA)-1)
				for i, pt := range pointsA {
					if i != outlierIdx[0] {
						validPointsA = append(validPointsA, pt.Load)
					}
				}
			} else {
				validPointsA = make([]float64, len(pointsA))
				for i, pt := range pointsA {
					validPointsA[i] = pt.Load
				}
			}
		} else {
			validPointsA = make([]float64, len(pointsA))
			for i, pt := range pointsA {
				validPointsA[i] = pt.Load
			}
		}

		// Average home load from valid points (excluding loads <= 0.0 KWH to filter out offline telemetry).
		var totalLoad float64
		var countLoad float64
		for _, load := range validPointsA {
			if load > 0.0 {
				totalLoad += load
				countLoad++
			}
		}
		avgLoadA := 0.0
		if countLoad > 0 {
			avgLoadA = totalLoad / countLoad
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

	return result
}

func getMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0.0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	return sorted[(len(sorted)-1)/2]
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
