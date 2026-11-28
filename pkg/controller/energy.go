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
const homeLoadPredictionRecencyDecay = 0.95

// sameWeekdayWeeklyDecay represents the age decay factor applied weekly as
// Pow(sameWeekdayWeeklyDecay, ageWeeks) for matching weekdays (e.g. comparing Saturdays to Saturdays).
// Because matching weekdays only occur every 7 days, applying standard daily decay (0.95^ageDays) would
// cause rapid degradation (0.95^7 ≈ 70% after 1 week, 0.95^14 ≈ 49% after 2 weeks), allowing recent
// non-matching weekdays to overshadow weekly recurring activities (e.g. weekend chores or laundry days).
const sameWeekdayWeeklyDecay = 0.90

// sameWeekdayWeightMultiplier scales the weight of matching weekdays (e.g. comparing Saturdays to Saturdays)
// by 2.5x. Combined with sameWeekdayWeeklyDecay, this ensures recurring weekly activity profiles retain
// sufficient voting weight in the prediction pool without being overwhelmed by recent non-matching days.
const sameWeekdayWeightMultiplier = 2.5

// neighborHourWeightMultiplier blends adjacent hours (h-1 and h+1) into the target hour h's prediction pool.
// We tried 0.25 (which reduced cost regression slightly more on normal days) and 0.00 (which completely disabled
// adjacent blending). We chose 0.50 because some blending is necessary to handle time-shifted household loads
// (e.g. if the AC starts at 8:30 AM instead of 9:00 AM on a given day).
const neighborHourWeightMultiplier = 0.5

// neighborHourStandbyFloorMultiplier represents the standby threshold multiplier used to exclude low-power
// sleep or standby neighbor hours from blending into adjacent active hours (e.g. preventing a 06:00 AM sleeping
// hour from diluting a 07:00 AM waking hour).
//
// Derivation: Across 40 production sites (over 108,000 hourly data points), nighttime non-EV sleeping usage
// averages 1.2x to 1.5x of empirical standby load (P01). When residents wake up, active household consumption
// jumps to 2.0x to 7.0x standby. Setting this cutoff at 1.8x cleanly separates sleep/standby neighbor hours
// from active waking hours, eliminating sleep dilution without falsely trimming active hours.
const neighborHourStandbyFloorMultiplier = 1.8

// dayOfWeekOutlierRatioThreshold defines the minimum historical ratio of target weekday consumption
// (evaluated across morning, afternoon, evening, waking, and whole-day windows) relative to preceding days
// required to qualify as an outlier weekday period.
// Statistical analysis across 40 production sites showed that genuine day-of-week routines (e.g. weekend chores,
// mid-week laundry, or work-from-home days) exhibit ratios of 1.25x to 1.90x compared to preceding days.
const dayOfWeekOutlierRatioThreshold = 1.25

// dayOfWeekOutlierMorningTStatThreshold defines the minimum Welch's t-statistic required to confirm
// that a morning (06:00-10:59) day-of-week load surge is statistically significant and repeatable.
// Sweeping t-thresholds across 118 Summer+Fall production site-windows showed that raising this from 1.70
// to 2.30 (Bonferroni-style multiple-testing adjustment across 7 weekdays and 5 period windows) cuts
// false-positive weekday triggers on steady households while preserving genuine weekend/weekday morning routines.
const dayOfWeekOutlierMorningTStatThreshold = 2.30

// dayOfWeekOutlierNonMorningTStatThreshold defines the minimum Welch's t-statistic required to confirm
// that an afternoon (11:00-16:59), evening (17:00-21:59), waking (06:00-21:59), or whole-day (00:00-23:59)
// day-of-week load surge is statistically significant when temperatures match preceding days.
const dayOfWeekOutlierNonMorningTStatThreshold = 2.50

// dayOfWeekOutlierMinTargetSamples defines the minimum number of valid historical occurrences
// of the target weekday (e.g. at least 3 Fridays in the 4-5 week history window) required before
// running Welch's t-test for day-of-week or weekday-block outlier detection.
const dayOfWeekOutlierMinTargetSamples = 3

// dayOfWeekOutlierMinPrecedingSamples defines the minimum combined number of valid historical
// occurrences across the 2 comparison weekdays (p1 = wd-1 and p2 = wd-2, or Thursday+Friday for Sunday,
// corresponding to at least 3 weeks * 2 weekdays/week = 6 days) required to form a stable baseline
// distribution for Welch's t-test.
const dayOfWeekOutlierMinPrecedingSamples = 6

// dayOfWeekOutlierWeightMultiplier scales the voting weight of matching weekdays by an additional 3.0x
// (boosting sameWeekdayWeightMultiplier from 2.5x to 7.5x) when a day of the week is identified as a historical outlier.
// This allows the distinct historical distribution of that day to dominate the percentile prediction pool.
const dayOfWeekOutlierWeightMultiplier = 3.0

// dayOfWeekOutlierDownWeightMultiplier down-weights recent non-matching days (from 1.0x to 0.33x) during
// morning hours (06:00-10:59) when a day of the week is identified as a historical outlier. This prevents
// lower-load preceding weekday mornings from pulling down the percentile ranking of recurring morning routines
// while keeping afternoon and evening temperature recency intact.
const dayOfWeekOutlierDownWeightMultiplier = 0.33

// tempSimilarityScale acts as the denominator in the exponential temperature similarity function:
// exp(-tempDiff / tempSimilarityScale). We originally evaluated 1.5, but found that raising it to 3.0
// provides much better accuracy (lower MAE) and less overage on normal/spring days, while the gated
// safeguard takes care of protecting the battery during extreme summer heatwaves.
const tempSimilarityScale = 3.0

// tempDeadbandCoreLowC (15°C) .. tempDeadbandCoreHighC (21°C) defines the mild-weather core where
// neither electric heating nor AC cooling is typically active, with a smooth 2°C linear ramp out to
// tempDeadbandOuterLowC (13°C) and tempDeadbandOuterHighC (23°C). Inside the core, temperature
// differences are compressed by tempDeadbandSlope (0.25x) so mild days (e.g., 15°C vs. 21°C) retain
// ~61% weight instead of losing 86.5%. Outside [13°C, 23°C], full 1:1 separation is preserved with
// zero boundary cliff so heating and cooling days are never blurred into mild-weather days.
const (
	tempDeadbandOuterLowC  = 13.0
	tempDeadbandCoreLowC   = 15.0
	tempDeadbandCoreHighC  = 21.0
	tempDeadbandOuterHighC = 23.0
	tempDeadbandSlope      = 0.25
)

// Afternoon-to-evening thermal lag parameters: after a hot (>= 26.5°C) afternoon, building thermal
// mass keeps indoor cooling loads elevated into the evening (18:00-23:00) even after outdoor air
// cools into the 19-26°C range. Blending 50% of the daily peak temperature difference into evening
// temperature similarity ensures hot-afternoon evenings match other hot-afternoon evenings rather
// than cool-day evenings at the same instantaneous temperature, consistently across h-1/h/h+1.
const (
	eveningThermalLagStartHour    = 18
	eveningThermalLagEndHour      = 23
	eveningThermalLagMinPeakTempC = 26.5
	eveningThermalLagWeight       = 0.5
)

// minWeatherHoursForDailyAverage specifies the minimum number of hourly weather forecast readings required
// (at least 12 out of 24 hours) to calculate a representative 24-hour daily average temperature.
// This prevents partial weather records (e.g. a single morning or late-night reading) from distorting the day's temperature.
const minWeatherHoursForDailyAverage = 12

// minTempSimilarityWeightSum defines the minimum cumulative exponential temperature similarity weight
// (sum of exp(-tempDiff / 3.0)) required to consider a historical pool adequately temperature-matched.
// Since each day within 1-2°C contributes ~0.51-0.72 weight, a sum of 1.5 corresponds to having at least
// 2 to 3 historical days with closely matching temperatures.
const minTempSimilarityWeightSum = 1.5

// allDaysTempSimilarityDampenThreshold defines the cumulative temperature similarity weight across all valid
// historical days (any weekday) required to dampen Option G shifts when temperature-matched and unweighted
// weekday expectations diverge. Because matching weekdays already contribute >= 1.5 when entering that branch,
// requiring 2.5 ensures at least ~3-5 total temperature-similar days exist across the month so the hourly
// model's own temperature weighting already captures the weather effect.
const allDaysTempSimilarityDampenThreshold = 2.5

// weatherDrivenSurgeTempDeltaC is the minimum daily average temperature difference (2.0°C / ~3.6°F)
// between a recent day and prior occurrences of the same weekday to classify a load increase as a
// weather-driven HVAC surge rather than an occupancy shift (e.g. guests in town).
const weatherDrivenSurgeTempDeltaC = 2.0

// defaultStrategyPercentile represents the percentile used for the Default load prediction strategy.
// We previously evaluated 65p (which caused +23% cost regression on normal days) and 50p (median).
// However, empirical analysis across 40 production sites (over 2,400 site-days) demonstrated that
// 50p caused chronic under-prediction (35,990 kWh under vs 18,817 kWh over) due to the strongly
// right-skewed distribution of residential appliance usage (HVAC, laundry, cooking) where Mean exceeds
// Median by 0.25-0.31 kWh/hr. Raising this from 50p to 55p reduces total system under-prediction by ~4,000 kWh
// (-11%) while adding negligible MAE error (+0.012 kWh) and preventing premature battery depletion.
const defaultStrategyPercentile = 0.55

// conservativeStrategyPercentile represents the percentile used for the Conservative strategy.
// We originally set this to 80p (80th percentile) to provide a robust safety buffer. However,
// a comprehensive 25-site simulation study over a 3-week period showed that 80p systematically
// overpredicted load by an average of 22.4 kWh per site daily, resulting in excessive grid pre-charging
// and high electricity bills. Lowering this to 70p (70th percentile) reduces daily total prediction
// error to 16.8 kWh (a 25% improvement) while still maintaining a robust safety buffer.
const conservativeStrategyPercentile = 0.70

// extremeHeatwaveThresholdC represents the temperature threshold above the historical maximum temperature
// seen for a given hour. If today's forecast exceeds the historical maximum plus this threshold, the safeguard is triggered.
const extremeHeatwaveThresholdC = 2.0

// extremeHeatwaveMinTempC represents the minimum forecasted temperature required to trigger the heatwave safeguard.
// This prevents triggering a safeguard boost during cooler seasons (e.g. going from 15°C to 18°C).
const extremeHeatwaveMinTempC = 28.0

// extremeHeatwaveLoadMultiplier represents the safety boost multiplier applied to the predicted load
// when today is an extreme temperature outlier (e.g. 1.20 increases predicted load by 20%).
const extremeHeatwaveLoadMultiplier = 1.20

// loadShiftOutlierIQRExpansion represents the IQR multiplier threshold used to detect
// abnormal daily active loads and today's cumulative active load shifts (e.g. vacations or visitor stays).
// We ran parameter sweeps across 30 production sites and found that 1.2 on active energy above standby
// baseline load provides optimal sensitivity to catch real shifts without triggering false positives on normal days.
const loadShiftOutlierIQRExpansion = 1.2

// loadShiftRecencyDecay represents the age decay factor applied exponentially
// when a structural load shift (vacation or visitor stay) has been detected.
// We ran sweeps over 30 production sites and chose 0.30 because it makes yesterday's
// data dominate the prediction profile, allowing the model to adapt within 24-48 hours.
const loadShiftRecencyDecay = 0.30

// loadShiftEscapeHours represents the number of consecutive completed hours
// of non-outlier usage required to early-escape an active load shift.
// We ran sweeps over 30 production sites and selected 4 hours: 1-2 hours is highly
// susceptible to false escapes from appliance cycles, while 4 hours prevents false escapes
// but still exits vacation mode in time for overnight optimizing.
const loadShiftEscapeHours = 4

// standbyActiveEnergyFloor represents the absolute minimum active energy (kWh/hr)
// expected to detect human occupancy. An active average below this threshold (0.02,
// or ~20 Watts) is physically negligible and serves as our absolute floor for vacation detection.
const standbyActiveEnergyFloor = 0.02

// loadShiftOutlierFloorFraction represents the minimum active average consumption floor
// as a fraction of baseline Q1 load. When baseline variance is very high, standard IQR bounds
// standard formulas fall to zero. 25% of baseline Q1 provides a robust, site-adaptive floor.
const loadShiftOutlierFloorFraction = 0.25

// loadShiftOutlierCeilingCap represents the maximum active average consumption ceiling
// as a fraction of baseline Q1 load to identify a low-outlier vacation day.
// A value of 0.55 ensures that a vacation day requires at least a 45% reduction from normal.
const loadShiftOutlierCeilingCap = 0.55

// vacationMorningFlatnessStdDevCeiling represents the maximum morning standard deviation (kWh)
// across completed morning hours (7:00 AM to current hour) expected for an unoccupied vacation morning.
// While an empty home with background standby draws near-zero variance (< 0.05 kWh), periodic HVAC
// or furnace cycling on hot/cold days can cause mild hourly fluctuations (~0.10 - 0.15 kWh).
// Setting this ceiling to 0.15 kWh captures vacation mornings even with periodic HVAC cycling,
// while remaining far below normal human occupancy morning volatility (0.30 - 1.50+ kWh).
const vacationMorningFlatnessStdDevCeiling = 0.15

// vacationBaselineMinStdDevKWH is the minimum baseline Q1 daily standard deviation (in kWh) required
// before relative volatility drop gating (vacationVolatilityDropFraction * q1StdDev) can trigger vacation
// detection, preventing false triggers on ultra-low-variance households where 25% of Q1 stddev is near zero.
const vacationBaselineMinStdDevKWH = 0.25

// vacationVolatilityDropFraction is the fraction (25%) of the baseline Q1 daily standard deviation below
// which a completed day's or morning's load standard deviation qualifies as flat/unoccupied for Condition B
// vacation detection.
const vacationVolatilityDropFraction = 0.25

// calmMorningRatioThreshold defines the maximum ratio of observed morning load (07:00 up to min(12, currentHour))
// to the unboosted all-days P50 morning baseline on a historical weekday-outlier day (e.g. a Friday/weekend chore day)
// below which remaining hours today are capped down to the unboosted all-days percentile rather than keeping the 3x
// same-weekday surge boost.
const calmMorningRatioThreshold = 0.90

// EnergyHistoryDataset defines the JSON structure for historical energy modeling test datasets.
type EnergyHistoryDataset struct {
	SiteID            string                   `json:"siteID"`
	Period            string                   `json:"period"`
	TimeZone          string                   `json:"timeZone"`
	SimStart          time.Time                `json:"simStart"`
	SimEnd            time.Time                `json:"simEnd"`
	EVChargingStandby bool                     `json:"evChargingStandby,omitempty"`
	EnergyHistory     []types.DailyEnergyStats `json:"energyHistory"`
	WeatherHistory    []types.Weather          `json:"weatherHistory"`
}

// recentDiffDetail stores detailed calculation values for a given recent date
// during z-score baseline shift computation, useful for debug log inspection.
type recentDiffDetail struct {
	Date            string    `json:"date"`
	ActualLoadKWH   float64   `json:"actualLoadKWH"`
	ExpectedLoadKWH float64   `json:"expectedLoadKWH"`
	DiffKWH         float64   `json:"diffKWH"`
	SameWDValuesKWH []float64 `json:"sameWdValuesKWH"`
}

type dayPoints struct {
	date    string
	dayTime time.Time
	weekday time.Weekday
	loads   []float64
	points  []types.EnergyStats
}

func tempDeadbandFactor(tempC float64) float64 {
	if tempC <= tempDeadbandOuterLowC || tempC >= tempDeadbandOuterHighC {
		return 0.0
	}
	if tempC < tempDeadbandCoreLowC {
		return (tempC - tempDeadbandOuterLowC) / (tempDeadbandCoreLowC - tempDeadbandOuterLowC)
	}
	if tempC > tempDeadbandCoreHighC {
		return (tempDeadbandOuterHighC - tempC) / (tempDeadbandOuterHighC - tempDeadbandCoreHighC)
	}
	return 1.0
}

// tempSimilarityDiffC computes the effective temperature distance (°C) between two temperatures
// for exponential similarity weighting, smoothly compressing mild-weather differences inside
// [13°C, 23°C] (full 0.25x compression inside [15°C, 21°C]) while preserving 1:1 separation
// whenever either temperature lies in an active heating (<=13°C) or cooling (>=23°C) regime.
func tempSimilarityDiffC(t1, t2 float64) float64 {
	blend := min(tempDeadbandFactor(t1), tempDeadbandFactor(t2))
	scale := 1.0 - blend*(1.0-tempDeadbandSlope)
	return math.Abs(t1-t2) * scale
}

// eveningThermalLagDiffC returns the additional effective temperature distance (°C) for evening
// hours (18:00-23:00) when either the target or candidate day had a hot afternoon peak (>= 26.5°C).
func eveningThermalLagDiffC(targetPeakC float64, hasTargetPeak bool, histPeakC float64, hasHistPeak bool) float64 {
	if !hasTargetPeak || !hasHistPeak {
		return 0.0
	}
	if targetPeakC < eveningThermalLagMinPeakTempC && histPeakC < eveningThermalLagMinPeakTempC {
		return 0.0
	}
	return eveningThermalLagWeight * tempSimilarityDiffC(targetPeakC, histPeakC)
}

// BuildHourlyEnergyModel averages usage and solar by hour of day from history,
// taking into account weekend days and day-of-the-week differences, while ignoring outlier days
// (e.g. vacation days with significantly below-average usage) and aligning AC temperature baseline calculations.
// It implements an adaptive baseline shifting algorithm that reacts to structural changes in load
// (e.g. guests in town) while remaining robust against intermittent, unpredictable loads (e.g. EV charging).
// Returns the 24 hourly TimeProfiles, SimulationParams, and inferred site-adaptive EV charging
// thresholds (siteEVMinThresholdKW, siteEVMinStepKW).
func (c *Controller) BuildHourlyEnergyModel(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
) ([]TimeProfile, types.SimulationParams, float64, float64) {
	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz != "" && now.Location() != nil && now.Location().String() == tz {
			return now.Location()
		}
		return resolveEnergyStatsLocation(tz, fallback, locCache)
	}

	dayMap, standbyLoad, overallAvgLoad, loc := buildHistoryDayMapAndStandby(ctx, history, now.Location(), locCache)

	todayStr := now.In(loc).Format("2006-01-02")
	yesterdayStr := now.In(loc).AddDate(0, 0, -1).Format("2006-01-02")
	currentHour := now.In(loc).Hour()

	// Build mapping of temperature forecast hours for quick access during HVAC/EV outlier filtering
	// and hourly temperature similarity weighting.
	// Keys are normalized to UTC because Go map[time.Time] equality compares the internal *time.Location pointer.
	weatherByHour := make(map[time.Time]float64)
	for _, w := range weather {
		for _, hw := range w.ForecastHours {
			weatherByHour[hw.TSHourStart.UTC()] = hw.TemperatureC
		}
	}

	ignoredOutlierHours, outlierRefLoad, hourBaselineRef, siteEVMinThresholdKW, siteEVMinStepKW := detectIntermittentEVAndLoadSpikes(ctx, dayMap, weatherByHour, settings, standbyLoad)
	hvac := detectWeatherHVACSensitivity(dayMap, weatherByHour, loc)
	if hvac.hasElectricHeating || hvac.hasElectricCooling {
		log.Ctx(ctx).DebugContext(
			ctx,
			"detected weather HVAC sensitivity",
			slog.Bool("hasElectricHeating", hvac.hasElectricHeating),
			slog.Bool("heatingWindowDetected", hvac.heatingWindowDetected),
			slog.Any("heatingHours", activeHourList(hvac.heatingHours)),
			slog.Bool("hasElectricCooling", hvac.hasElectricCooling),
			slog.Bool("coolingWindowDetected", hvac.coolingWindowDetected),
			slog.Any("coolingHours", activeHourList(hvac.coolingHours)),
		)
	}

	// Build deduplicated maps of daily temperatures (average, min, max, morning average) from weather
	// for IQR weather-surge exemption and Option G temperature-similarity matching.
	type dedupWeatherHour struct {
		dateStr string
		localHr int
		tempC   float64
	}
	latestWeatherByUTC := make(map[time.Time]dedupWeatherHour)
	for _, w := range weather {
		hwLoc := getLocation(w.TimeLocation, loc)
		for _, hw := range w.ForecastHours {
			if hw.TSHourStart.IsZero() {
				continue
			}
			localTS := hw.TSHourStart.In(hwLoc)
			latestWeatherByUTC[hw.TSHourStart.Truncate(time.Hour).UTC()] = dedupWeatherHour{
				dateStr: localTS.Format("2006-01-02"),
				localHr: localTS.Hour(),
				tempC:   hw.TemperatureC,
			}
		}
	}
	dayAvgTempCMap := make(map[string]float64)
	dayTempHourCountMap := make(map[string]int)
	dayMinTempCMap := make(map[string]float64)
	dayMaxTempCMap := make(map[string]float64)
	dayMorningTempCSumMap := make(map[string]float64)
	dayMorningTempCountMap := make(map[string]int)
	for _, dwh := range latestWeatherByUTC {
		dateStr := dwh.dateStr
		dayAvgTempCMap[dateStr] += dwh.tempC
		if dayTempHourCountMap[dateStr] == 0 {
			dayMinTempCMap[dateStr] = dwh.tempC
			dayMaxTempCMap[dateStr] = dwh.tempC
		} else {
			if dwh.tempC < dayMinTempCMap[dateStr] {
				dayMinTempCMap[dateStr] = dwh.tempC
			}
			if dwh.tempC > dayMaxTempCMap[dateStr] {
				dayMaxTempCMap[dateStr] = dwh.tempC
			}
		}
		dayTempHourCountMap[dateStr]++
		if dwh.localHr >= 0 && dwh.localHr <= 8 {
			dayMorningTempCSumMap[dateStr] += dwh.tempC
			dayMorningTempCountMap[dateStr]++
		}
	}
	getDayAvgTemp := func(dStr string) (float64, bool) {
		// Require at least minWeatherHoursForDailyAverage (12 hours) of unique hourly forecast readings
		// to compute a trustworthy daily average temperature for date dStr.
		if count := dayTempHourCountMap[dStr]; count >= minWeatherHoursForDailyAverage {
			return dayAvgTempCMap[dStr] / float64(count), true
		}
		return 0, false
	}
	getDayMaxTemp := func(dStr string) (float64, bool) {
		if count := dayTempHourCountMap[dStr]; count >= minWeatherHoursForDailyAverage {
			return dayMaxTempCMap[dStr], true
		}
		return 0, false
	}
	// Compute mean forecast temperature across the upcoming 24-hour planning horizon (now..now+23h)
	// so Option G's ongoing-regime check reflects the actual hours being forecasted rather than only todayStr.
	var forecastWindowTempSumC float64
	var forecastWindowTempCount int
	nowHrUTC := now.Truncate(time.Hour).UTC()
	for offset := 0; offset < 24; offset++ {
		if tC, ok := weatherByHour[nowHrUTC.Add(time.Duration(offset)*time.Hour)]; ok {
			forecastWindowTempSumC += tC
			forecastWindowTempCount++
		}
	}
	forecastWindowTempC := 0.0
	hasForecastWindowTemp := forecastWindowTempCount >= minWeatherHoursForDailyAverage
	if hasForecastWindowTemp {
		forecastWindowTempC = forecastWindowTempSumC / float64(forecastWindowTempCount)
	}

	// Count how many historical days have at least 18 hours of valid load data.
	// When history has >= 4 complete days, we require >= 18 hours for a day to participate
	// in daily-average statistics (IQR, vacation detection, and Option G), preventing partial
	// telemetry days from masquerading as low-load days while preserving compatibility with
	// sparse synthetic unit tests.
	completeHistDays := 0
	for dateStr, d := range dayMap {
		if dateStr != todayStr && len(d.loads) >= 18 {
			completeHistDays++
		}
	}
	isCompleteHistDay := func(dateStr string, d *dayPoints) bool {
		if dateStr == todayStr || d == nil || len(d.loads) == 0 {
			return false
		}
		if completeHistDays >= 4 && len(d.loads) < 18 {
			return false
		}
		return true
	}

	// Calculate daily averages for outlier detection.
	// Any hour flagged as an intermittent EV or load spike is replaced with its baseline reference load
	// when computing daily averages so that a 30+ kWh EV charging session doesn't distort the entire day's
	// average or trigger a false daily IQR outlier or Option G shift.
	var dailyAverages []float64
	dayAveragesMap := make(map[string]float64)
	for _, d := range dayMap {
		if len(d.loads) == 0 {
			continue
		}
		var sum float64
		var count int
		for _, pt := range d.points {
			if pt.HomeKWH <= 0.0 {
				continue
			}
			val := pt.HomeKWH
			hr := pt.TSHourStart.Hour()
			key := dayHourKey{date: d.date, hour: hr}
			if ignoredOutlierHours[key] {
				if ref, ok := outlierRefLoad[key]; ok && ref > 0 {
					val = ref
				} else if ref, ok := hourBaselineRef[hr]; ok && ref > 0 {
					val = ref
				} else {
					val = standbyLoad
				}
			}
			sum += val
			count++
		}
		if count == 0 {
			continue
		}
		avg := sum / float64(count)
		if isCompleteHistDay(d.date, d) {
			dayAveragesMap[d.date] = avg
			dailyAverages = append(dailyAverages, avg)
		} else if d.date == todayStr {
			dayAveragesMap[d.date] = avg
		}
	}

	detectedShift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, currentHour, standbyLoad)

	historicalVacationDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)

	// Calculate median historical daily average temperature across complete historical days
	// to detect genuine cold snaps or heatwaves for upper-IQR exemption.
	var completeDayAvgTemps []float64
	for dateStr, d := range dayMap {
		if isCompleteHistDay(dateStr, d) {
			if t, ok := getDayAvgTemp(dateStr); ok {
				completeDayAvgTemps = append(completeDayAvgTemps, t)
			}
		}
	}
	hasMedianDayTemp := len(completeDayAvgTemps) > 0
	medianDayAvgTempC := 0.0
	if hasMedianDayTemp {
		medianDayAvgTempC = medianSortedOrCopy(completeDayAvgTemps)
	}

	isWeatherExtremeUpperIQRExempt := func(dateStr string) bool {
		if !hasMedianDayTemp {
			return false
		}
		dayAvgTempC, hasDayWeather := getDayAvgTemp(dateStr)
		if !hasDayWeather {
			return false
		}
		if hvac.hasElectricHeating && dayAvgTempC <= medianDayAvgTempC-weatherDrivenSurgeTempDeltaC {
			morningAvgTempC := dayAvgTempC
			if cnt := dayMorningTempCountMap[dateStr]; cnt > 0 {
				morningAvgTempC = dayMorningTempCSumMap[dateStr] / float64(cnt)
			}
			if morningAvgTempC < coldHeatingMaxTempC || dayMinTempCMap[dateStr] < coldHeatingMaxTempC {
				return true
			}
		}
		if hvac.hasElectricCooling && dayAvgTempC >= medianDayAvgTempC+weatherDrivenSurgeTempDeltaC {
			if dayMaxTempCMap[dateStr] >= extremeHeatwaveMinTempC {
				return true
			}
		}
		return false
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
			if detectedShift == "none" && historicalVacationDays[dateStr] {
				continue
			}
			// When in an active vacation shift, retain today and yesterday in validDaysMap without IQR filtering.
			if detectedShift != "none" && (dateStr == todayStr || dateStr == yesterdayStr) {
				validDaysMap[dateStr] = true
				continue
			}
			if isCompleteHistDay(dateStr, dayMap[dateStr]) && avg >= lowerBound {
				if avg <= upperBound {
					validDaysMap[dateStr] = true
				} else if isWeatherExtremeUpperIQRExempt(dateStr) {
					validDaysMap[dateStr] = true
					dayAvgTempC, _ := getDayAvgTemp(dateStr)
					log.Ctx(ctx).DebugContext(
						ctx,
						"exempting extreme-weather HVAC day from upper IQR filtering",
						slog.String("date", dateStr),
						slog.Float64("dayAvgLoadKWH", avg),
						slog.Float64("upperBoundKWH", upperBound),
						slog.Float64("dayAvgTempC", dayAvgTempC),
						slog.Float64("medianDayAvgTempC", medianDayAvgTempC),
					)
				}
			}
		}
	} else {
		// Fallback: If we have fewer than 4 days of history, we cannot establish standard deviation or IQR safely.
		// Therefore, we treat all complete historical days as valid and skip IQR filtering.
		for dateStr := range dayAveragesMap {
			if detectedShift == "none" && historicalVacationDays[dateStr] {
				continue
			}
			// When in an active vacation shift, retain today and yesterday in validDaysMap without IQR filtering.
			if detectedShift != "none" && (dateStr == todayStr || dateStr == yesterdayStr) {
				validDaysMap[dateStr] = true
				continue
			}
			if isCompleteHistDay(dateStr, dayMap[dateStr]) {
				validDaysMap[dateStr] = true
			}
		}
	}

	// Detect any days of the week (and time-of-day periods) that historically stand out with statistically
	// significant higher load compared to their preceding days (e.g. weekend chores, laundry afternoons, WFH days).
	dayOfWeekOutliers := detectDayOfWeekOutliers(ctx, loc, dayMap, validDaysMap, todayStr, ignoredOutlierHours, weatherByHour)

	// Calculate solar predictions using existing package functions.
	// CalculateWeatherSolar handles forecasted temperatures and clear sky indices,
	// while CalculateSmoothedSolar is used as a fallback if weather data is unavailable.
	var weatherSolar map[int64]WeatherSolar
	var smoothedSolar map[int]float64

	var params types.SimulationParams

	hasSolarHistory := false
	for _, h := range history {
		if h.SolarKWH > 0.05 {
			hasSolarHistory = true
			break
		}
	}

	if hasSolarHistory {
		if len(weather) > 0 {
			locInfo := types.SiteLocation{
				Latitude:  weather[0].Latitude,
				Longitude: weather[0].Longitude,
				TimeZone:  weather[0].TimeLocation,
			}
			optParams := settings.GetOptimizationParams()
			weatherSolar, params = CalculateWeatherSolar(ctx, now, history, weather, locInfo, optParams.CloudCoverDeratePercent)
		} else {
			smoothedSolar = CalculateSmoothedSolar(ctx, now, history, settings)
		}
	} else if len(weather) > 0 {
		params = types.SimulationParams{
			PanelAzimuth: 180.0,
			PanelTilt:    defaultSolarTilt,
		}
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
		if dateStr != todayStr {
			recentValidDates = append(recentValidDates, dateStr)
			if len(recentValidDates) >= 7 {
				break
			}
		}
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
	// Precompute clean (post-EV-filter) exact-hour (temp, load) observations across all valid
	// historical days for per-hour HVAC slope extrapolation and temperature-matched pool inclusion.
	var allValidExactHourTempLoads [24][]hourTempLoad
	validDateHourTemp := make(map[string][24]float64)
	validDateHasHourTemp := make(map[string][24]bool)
	for dateStr, ok := range validDaysMap {
		if !ok || dateStr == todayStr || (detectedShift == "none" && historicalVacationDays[dateStr]) {
			continue
		}
		d := dayMap[dateStr]
		if d == nil {
			continue
		}
		var temps [24]float64
		var hasTemps [24]bool
		for _, pt := range d.points {
			if pt.HomeKWH <= 0.0 {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if hr < 0 || hr >= 24 || ignoredOutlierHours[dayHourKey{date: dateStr, hour: hr}] {
				continue
			}
			if histTemp, hasHistTemp := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]; hasHistTemp {
				allValidExactHourTempLoads[hr] = append(allValidExactHourTempLoads[hr], hourTempLoad{
					tempC:   histTemp,
					loadKWH: pt.HomeKWH,
				})
				temps[hr] = histTemp
				hasTemps[hr] = true
			}
		}
		validDateHourTemp[dateStr] = temps
		validDateHasHourTemp[dateStr] = hasTemps
	}
	seasonalMedians := computeSeasonalHourMedians(allValidExactHourTempLoads)

	// We calculate the difference between actual daily averages and their respective weekday baseline expectations
	// for the last 4 valid days. If these differences are consistent (low variance), the standard deviation remains small,
	// yielding a high z-score. If they are volatile (high variance due to random EV charging), the standard deviation is large,
	// yielding a low z-score.
	var diffs []float64
	var recentDiffDetails []recentDiffDetail

	for i := 0; i < len(recentValidDates) && i < 4; i++ {
		dStr := recentValidDates[i]
		actualLoad := dayAveragesMap[dStr]
		d := dayMap[dStr]
		if d != nil && !d.dayTime.IsZero() {
			dWD := d.weekday
			targetDayTemp, hasTargetTemp := getDayAvgTemp(dStr)

			var otherVals []float64
			var otherWeights []float64
			var otherHasTemp []bool
			var allSameWDVals []float64
			var sumOtherTemp float64
			var countOtherTemp int
			var tempSimW float64

			for otherDateStr, ok := range validDaysMap {
				// Compare this day to all OTHER days of the same weekday in history to establish baseline expectation.
				// We exclude the day itself and todayStr to prevent self-bias or partial-day distortion.
				if ok && otherDateStr != dStr && otherDateStr != todayStr {
					od := dayMap[otherDateStr]
					if od != nil && !od.dayTime.IsZero() && od.weekday == dWD {
						load := dayAveragesMap[otherDateStr]
						allSameWDVals = append(allSameWDVals, load)
						otherDayTemp, hasOtherTemp := getDayAvgTemp(otherDateStr)

						// Continuous Temperature-Similarity Weighting + Standby Protection:
						// - Why this was chosen: It achieved the lowest overall hourly MAE (0.921 kWh/hr, a 1.7%
						//   improvement across 9,437 test hours and 40 sites) while cutting seasonal under-prediction bias by 33%.
						//   It reuses the exponential temperature similarity function (math.Exp(-ΔT / tempSimilarityScale)),
						//   providing smooth, physically proportional weighting across seasonal shifts without threshold artifacts.
						// - Note on alternative considered (Discrete Temp-Matching ±3.5°C): Evaluated a hard ±3.5°C window
						//   requiring >= 2 matching historical days. While it reduced aggregate negative bias more aggressively
						//   during autumn cool-downs (-52%), its hard boundary (e.g. 3.4°C vs 3.6°C) introduced threshold step
						//   discontinuities and higher hourly MAE (0.927 kWh/hr) compared to continuous similarity weighting.
						w := 1.0
						hasBothTemp := hasTargetTemp && hasOtherTemp
						if hasBothTemp {
							w = math.Exp(-tempSimilarityDiffC(targetDayTemp, otherDayTemp) / tempSimilarityScale)
							sumOtherTemp += otherDayTemp
							countOtherTemp++
							tempSimW += w
						}
						otherVals = append(otherVals, load)
						otherWeights = append(otherWeights, w)
						otherHasTemp = append(otherHasTemp, hasBothTemp)
					}
				}
			}

			// When some prior occurrences of this weekday have weather and others do not, scale missing-weather
			// days by the average temperature similarity weight rather than leaving them at 1.0 (0°C match).
			if hasTargetTemp && countOtherTemp > 0 && countOtherTemp < len(otherWeights) {
				avgW := tempSimW / float64(countOtherTemp)
				for idx := range otherWeights {
					if !otherHasTemp[idx] {
						otherWeights[idx] = avgW
					}
				}
			}

			var expectedLoad float64
			var hasExpected bool

			var sumW, sumWeightedLoad float64
			for idx, val := range otherVals {
				w := otherWeights[idx]
				sumW += w
				sumWeightedLoad += val * w
			}

			// Sum temperature similarity weights across all valid historical days (regardless of weekday).
			// Because the hourly model (avgLoadA) pulls from both same-weekday history and recentValidDates
			// and weights every point by exp(-tempDiff / 3.0), a high allDaysSimW indicates that avgLoadA
			// already has similar-temperature days to model weather-driven load changes without Option G
			// applying a redundant shift on top.
			var allDaysSimW float64
			if hasTargetTemp {
				for otherDateStr, ok := range validDaysMap {
					if ok && otherDateStr != dStr && otherDateStr != todayStr {
						if otherTemp, hasOT := getDayAvgTemp(otherDateStr); hasOT {
							allDaysSimW += math.Exp(-tempSimilarityDiffC(targetDayTemp, otherTemp) / tempSimilarityScale)
						}
					}
				}
			}

			hasSufficientTempMatch := (!hasTargetTemp && sumW >= minTempSimilarityWeightSum) || (hasTargetTemp && tempSimW >= minTempSimilarityWeightSum)
			if hasSufficientTempMatch && sumW > 0 {
				expectedLoad = sumWeightedLoad / sumW
				// When the broader historical pool has ample temperature-matching days (allDaysSimW >= 2.5,
				// ~3-5 days with closely matching temperatures) and the temperature-weighted expectation
				// exceeds the unweighted weekday average by >15%, the recent day's elevated load is
				// primarily weather-driven and already captured by avgLoadA's hourly temperature weighting.
				// Blending expectedLoad halfway toward actualLoad dampens the residual diff so Option G
				// does not double-count the weather effect.
				if hasTargetTemp && len(allSameWDVals) > 0 && allDaysSimW >= allDaysTempSimilarityDampenThreshold {
					var sumOther float64
					for _, ov := range allSameWDVals {
						sumOther += ov
					}
					unweightedExpected := sumOther / float64(len(allSameWDVals))
					if math.Abs(expectedLoad-unweightedExpected) > 0.15*unweightedExpected && actualLoad > unweightedExpected {
						expectedLoad = 0.5*expectedLoad + 0.5*actualLoad
					}
				}
				hasExpected = true
			} else if len(allSameWDVals) > 0 {
				var sumOther float64
				for _, ov := range allSameWDVals {
					sumOther += ov
				}
				unweightedExpected := sumOther / float64(len(allSameWDVals))
				// Fallback when prior occurrences of this exact weekday did not have sufficient temperature-matched
				// history (tempSimW < 1.5, which only occurs when prior same-weekday temperatures were far from
				// targetDayTemp or fewer than 2 prior occurrences of this weekday had weather):
				// - If actualLoad <= unweightedExpected (a cool-day load drop), setting expectedLoad = actualLoad
				//   produces diff = 0 so seasonal cooling does not trigger a downward shift.
				// - If actualLoad > unweightedExpected, we check whether the surge is already accounted for or transient:
				//   (a) other weekdays in history had similar temperatures (allDaysSimW >= 1.5) so the hourly model's
				//       temperature weighting already accounts for this temperature regime, OR
				//   (b) this day's temperature differed by >= 2.0°C from prior same-weekday temperatures AND the upcoming
				//       24-hour forecast window has already moved away from that temperature regime (a transient weather spike).
				//   When allDaysSimW < 1.5 AND the upcoming 24-hour forecast window remains in the same unprecedented
				//   cold/hot regime, we keep expectedLoad = unweightedExpected so Option G can bridge the gap until
				//   cold/hot history builds.
				if hasTargetTemp && len(weather) > 0 {
					if actualLoad > unweightedExpected {
						isWeatherDrivenSurge := false
						if allDaysSimW >= allDaysTempSimilarityDampenThreshold {
							isWeatherDrivenSurge = true
						} else if countOtherTemp > 0 {
							meanOtherTempC := sumOtherTemp / float64(countOtherTemp)
							deltaFromBaselineC := targetDayTemp - meanOtherTempC
							if tempSimilarityDiffC(targetDayTemp, meanOtherTempC) >= weatherDrivenSurgeTempDeltaC {
								sameDirection := (deltaFromBaselineC < 0 && forecastWindowTempC < meanOtherTempC) ||
									(deltaFromBaselineC > 0 && forecastWindowTempC > meanOtherTempC)
								ongoingRegime := hasForecastWindowTemp && sameDirection &&
									tempSimilarityDiffC(forecastWindowTempC, meanOtherTempC) >= weatherDrivenSurgeTempDeltaC
								if !ongoingRegime {
									isWeatherDrivenSurge = true
								}
							}
						}
						if !isWeatherDrivenSurge {
							expectedLoad = unweightedExpected
						} else {
							expectedLoad = actualLoad
						}
					} else {
						expectedLoad = actualLoad
					}
					hasExpected = true
				} else {
					expectedLoad = unweightedExpected
					hasExpected = true
				}
			}

			if hasExpected {
				diff := actualLoad - expectedLoad
				diffs = append(diffs, diff)

				recentDiffDetails = append(recentDiffDetails, recentDiffDetail{
					Date:            dStr,
					ActualLoadKWH:   actualLoad,
					ExpectedLoadKWH: expectedLoad,
					DiffKWH:         diff,
					SameWDValuesKWH: otherVals,
				})
			}
		}
	}

	meanDiff := 0.0
	stdDevDiff := 0.1
	zScoreG := 0.0
	scaleG := 0.0
	appliedShift := 0.0

	// We require at least 3 valid diff days to calculate a meaningful variance and z-score,
	// and only apply Option G when not in an active vacation load shift.
	if detectedShift == "none" && len(diffs) >= 3 {
		var sumDiff float64
		for _, diff := range diffs {
			sumDiff += diff
		}
		meanDiff = sumDiff / float64(len(diffs))

		// stdDevDiff measures volatility. If load changes consistently (family in town), stdDevDiff is small, z-score is high.
		stdDevDiff = getStdDev(diffs)

		zScoreG = meanDiff / stdDevDiff
		// We scale the shift smoothly starting from z-score 1.2 (ignored noise) up to 2.2 (fully applied).
		// This prevents threshold jitter.
		scaleG = min(1.0, max(0.0, (math.Abs(zScoreG)-1.2)*1.0))
		appliedShift = meanDiff * scaleG
		if settings.HomeLoadPredictionStrategy == "conservative" && appliedShift > 0 {
			appliedShift = 0
		}
	}

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

	var rawAvgLoadA [24]float64
	var rawExpectedLoadA [24]float64
	var rawP75LoadA [24]float64
	var rawSolar [24]float64
	var hasHour [24]bool
	var targetTimes [24]time.Time

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
		hasHour[h] = true
		targetTimes[h] = targetTime
		wd := targetTime.Weekday()

		// Build the Option A (same-weekday) historical date pool for target weekday wd.
		// To compute a stable weighted percentile without being skewed by a single noisy day,
		// we require at least 3 historical days in the base pool:
		// - Primary: all valid historical dates matching the exact target weekday wd (e.g. all Tuesdays).
		// - Fallback 1 (if < 3 exact weekday matches, e.g. short history or filtered vacation/outlier days):
		//   expand to all valid days in the same day-type category (Weekend: Sat+Sun, or Weekday: Mon..Fri).
		// - Fallback 2 (if still < 3 days): use all valid historical days regardless of weekday.
		var selectedDayDatesA []string
		for dateStr := range validDaysMap {
			if d := dayMap[dateStr]; d != nil && !d.dayTime.IsZero() && d.weekday == wd {
				selectedDayDatesA = append(selectedDayDatesA, dateStr)
			}
		}
		if len(selectedDayDatesA) < 3 {
			var fallbackDates []string
			isWeekend := wd == time.Saturday || wd == time.Sunday
			for dateStr := range validDaysMap {
				if d := dayMap[dateStr]; d != nil && !d.dayTime.IsZero() {
					wday := d.weekday
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

		// Build de-duplicated set of dates combining matching weekdays and recent 7 days.
		// We always keep recentValidDates (even on day-of-week outlier days) so the model remains
		// responsive to vacations or guests in town, relying on weight multipliers rather than excluding recent days.
		selectedDatesMap := make(map[string]bool)
		for _, dateStr := range selectedDayDatesA {
			selectedDatesMap[dateStr] = true
		}
		for _, dateStr := range recentValidDates {
			selectedDatesMap[dateStr] = true
		}
		if dToday := dayMap[todayStr]; dToday != nil && len(dToday.loads) > 0 {
			selectedDatesMap[todayStr] = true
		}

		// Target weather time and temperature for current hour h
		targetTimeHr := targetTime.Truncate(time.Hour).UTC()
		targetTemp, hasTargetTemp := weatherByHour[targetTimeHr]
		var targetAfternoonPeakC float64
		hasTargetAfternoonPeak := false
		if hasTargetTemp && h >= eveningThermalLagStartHour && h <= eveningThermalLagEndHour {
			targetDateStr := targetTime.In(loc).Format("2006-01-02")
			targetAfternoonPeakC, hasTargetAfternoonPeak = getDayMaxTemp(targetDateStr)
		}

		// Include temperature-matching valid days across all weekdays when forecasting an active
		// heating or cooling hour (or an evening following a hot afternoon) so non-recent
		// other-weekday cold snaps or heatwaves aren't dropped.
		isHeatingHour := hvac.heatingHours[h] && targetTemp <= coldHeatingMaxTempC
		isColdTarget := targetTemp <= coldHeatingBaseTempC
		isCoolingHour := hvac.coolingHours[h] && targetTemp >= hotCoolingMinTempC
		isHotAfternoonEvening := hasTargetAfternoonPeak && targetAfternoonPeakC >= eveningThermalLagMinPeakTempC
		if hasTargetTemp && (isHeatingHour || isColdTarget || isCoolingHour || (hvac.hasElectricCooling && isHotAfternoonEvening)) {
			for dateStr, hasTemps := range validDateHasHourTemp {
				if !hasTemps[h] {
					continue
				}
				if hvac.hasElectricCooling && isHotAfternoonEvening && !isHeatingHour && !isColdTarget && !isCoolingHour {
					histPeakC, hasHistPeak := getDayMaxTemp(dateStr)
					if !hasHistPeak || histPeakC < eveningThermalLagMinPeakTempC {
						continue
					}
					if tempSimilarityDiffC(validDateHourTemp[dateStr][h], targetTemp)+eveningThermalLagDiffC(targetAfternoonPeakC, true, histPeakC, true) <= tempSimilarityScale*1.5 {
						selectedDatesMap[dateStr] = true
					}
					continue
				}
				if tempSimilarityDiffC(validDateHourTemp[dateStr][h], targetTemp) <= tempSimilarityScale {
					selectedDatesMap[dateStr] = true
				}
			}
		}

		// Gather points at hours h, h-1, h+1 on all selected dates.
		// We gather neighboring hours to account for daily variations in household activity timing.
		var pts []weightedPoint
		var ptHasTemp []bool
		var sumTempMult float64
		var countTempMult int
		maxHistTemp := -999.0
		minHistTemp := math.MaxFloat64
		hasHistTempForHour := false
		var exactTempPts []weightedPoint
		var loggedDampenedNeighbor [24]bool

		var sortedSelectedDates []string
		for dateStr := range selectedDatesMap {
			sortedSelectedDates = append(sortedSelectedDates, dateStr)
		}
		sort.Strings(sortedSelectedDates)

		prevHr := (h - 1 + 24) % 24
		nextHr := (h + 1) % 24
		isMorning := outlierWindows[outlierPeriodMorning].containsHour(h)
		// Apply the 3x same-weekday outlier boost whenever hour h falls within any detected outlier period
		// (morning, afternoon, evening, waking, or whole-day) for target weekday wd.
		isOutlierHour := detectedShift == "none" && dayOfWeekOutliers[wd].coversHour(h)

		for _, dateStr := range sortedSelectedDates {
			if detectedShift == "none" && historicalVacationDays[dateStr] {
				continue
			}
			d := dayMap[dateStr]
			if d == nil || d.dayTime.IsZero() {
				continue
			}
			dTime := d.dayTime
			ageDays := int(targetTime.Sub(dTime).Hours() / 24)
			if ageDays < 0 {
				ageDays = 0
			}

			// Base weight based on age decay.
			var baseWeight float64

			if detectedShift != "none" {
				baseWeight = math.Pow(loadShiftRecencyDecay, float64(ageDays))
				if d.weekday == wd {
					baseWeight *= sameWeekdayWeightMultiplier
				}
			} else if d.weekday == wd {
				ageWeeks := float64(ageDays) / 7.0
				sameWdMultiplier := sameWeekdayWeightMultiplier
				if isOutlierHour {
					sameWdMultiplier *= dayOfWeekOutlierWeightMultiplier
				}
				baseWeight = math.Pow(sameWeekdayWeeklyDecay, ageWeeks) * sameWdMultiplier
			} else {
				baseWeight = math.Pow(homeLoadPredictionRecencyDecay, float64(ageDays))
				// Down-weight non-matching weekdays only in the morning so afternoon/evening recency stays intact.
				if isOutlierHour && isMorning {
					baseWeight *= dayOfWeekOutlierDownWeightMultiplier
				}
			}

			addCandidatePoint := func(pt types.EnergyStats, ptDateStr string, hpLocalHour int, mult float64) {
				if mult <= 0 || pt.HomeKWH <= 0.0 || ignoredOutlierHours[dayHourKey{date: ptDateStr, hour: hpLocalHour}] {
					return
				}
				finalWeight := baseWeight * mult
				hasTemp := false
				if hasTargetTemp {
					ptTime := pt.TSHourStart.Truncate(time.Hour).UTC()
					if histTemp, hasHistTemp := weatherByHour[ptTime]; hasHistTemp {
						tempDiff := tempSimilarityDiffC(targetTemp, histTemp)
						if hasTargetAfternoonPeak {
							histPeakC, hasHistPeak := getDayMaxTemp(dateStr)
							tempDiff += eveningThermalLagDiffC(targetAfternoonPeakC, hasTargetAfternoonPeak, histPeakC, hasHistPeak)
						}
						tempMult := math.Exp(-tempDiff / tempSimilarityScale)
						finalWeight *= tempMult
						hasTemp = true
						sumTempMult += tempMult
						countTempMult++

						// Track historical min/max temperature and weighted pool temperatures for this exact hour (excluding h-1/h+1 neighbors)
						if hpLocalHour == h {
							if histTemp > maxHistTemp {
								maxHistTemp = histTemp
							}
							if histTemp < minHistTemp {
								minHistTemp = histTemp
							}
							hasHistTempForHour = true
							exactTempPts = append(exactTempPts, weightedPoint{Value: histTemp, Weight: finalWeight})
						}
					}
				}
				pts = append(pts, weightedPoint{
					Value:  pt.HomeKWH,
					Weight: finalWeight,
				})
				ptHasTemp = append(ptHasTemp, hasTemp)
			}

			neighborMult := func(hpLocalHour int, loadKWH float64) float64 {
				mult := computeNeighborMultiplier(h, hpLocalHour, loadKWH, standbyLoad, targetTemp, hasTargetTemp, hvac, seasonalMedians)
				if mult == sharpHVACNeighborMultiplier && hpLocalHour >= 0 && hpLocalHour < 24 && !loggedDampenedNeighbor[hpLocalHour] {
					loggedDampenedNeighbor[hpLocalHour] = true
					log.Ctx(ctx).DebugContext(
						ctx,
						"dampening neighbor hour blend for sharp HVAC setback",
						slog.Int("hour", h),
						slog.Int("neighborHour", hpLocalHour),
						slog.Float64("multiplier", mult),
						slog.Float64("targetTempC", targetTemp),
					)
				}
				return mult
			}

			// For h == 0 and h == 23, look up the true chronological neighbor on the adjacent calendar day
			// (23:00 on d-1 or 00:00 on d+1). We exclude todayStr when looking forward from yesterday's 23:00
			// so incomplete today data does not bleed into historical days, and never wrap h == 0 or h == 23
			// to the opposite end (23 hours away) of the same day.
			var prevDayPoints *dayPoints
			if h == 0 {
				prevDateStr := dTime.AddDate(0, 0, -1).Format("2006-01-02")
				if !(detectedShift == "none" && historicalVacationDays[prevDateStr]) {
					prevDayPoints = dayMap[prevDateStr]
				}
			}
			var nextDayPoints *dayPoints
			if h == 23 {
				nextDateStr := dTime.AddDate(0, 0, 1).Format("2006-01-02")
				if nextDateStr != todayStr && !(detectedShift == "none" && historicalVacationDays[nextDateStr]) {
					nextDayPoints = dayMap[nextDateStr]
				}
			}

			for _, pt := range d.points {
				if pt.HomeKWH <= 0.0 {
					continue
				}
				hpLocalHour := pt.TSHourStart.Hour()
				if hpLocalHour == h {
					addCandidatePoint(pt, d.date, hpLocalHour, 1.0)
				} else if hpLocalHour == prevHr && h > 0 {
					addCandidatePoint(pt, d.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
				} else if hpLocalHour == nextHr && h < 23 {
					addCandidatePoint(pt, d.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
				}
			}

			if h == 0 && prevDayPoints != nil {
				for _, pt := range prevDayPoints.points {
					if pt.HomeKWH <= 0.0 {
						continue
					}
					hpLocalHour := pt.TSHourStart.Hour()
					if hpLocalHour == 23 {
						addCandidatePoint(pt, prevDayPoints.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
					}
				}
			}
			if h == 23 && nextDayPoints != nil {
				for _, pt := range nextDayPoints.points {
					if pt.HomeKWH <= 0.0 {
						continue
					}
					hpLocalHour := pt.TSHourStart.Hour()
					if hpLocalHour == 0 {
						addCandidatePoint(pt, nextDayPoints.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
					}
				}
			}
		}

		// When no points have weather (countTempMult == 0), no temperature multiplier is applied (1.0x).
		// When some historical days have weather and others do not, leaving missing-weather points at 1.0x
		// would treat them as a perfect 0°C match (exp(0) == 1.0) and over-weight them by 2x-5x relative to
		// weather-penalized points. Scaling by avgTempMult keeps missing-weather days neutral.
		if hasTargetTemp && countTempMult > 0 && countTempMult < len(pts) {
			avgTempMult := sumTempMult / float64(countTempMult)
			for i := range pts {
				if !ptHasTemp[i] {
					pts[i].Weight *= avgTempMult
				}
			}
		}

		// Compute weighted percentile and weighted pool mean of the gathered points.
		// AvgHomeLoadKWH uses the strategy percentile (for low-MAE near-term forecasts), while
		// ExpectedHomeLoadKWH preserves the weighted pool mean (additive across multi-hour horizons).
		pct := defaultStrategyPercentile
		if settings.HomeLoadPredictionStrategy == "conservative" {
			pct = conservativeStrategyPercentile
		}
		var avgLoadA, expectedLoadA, p75LoadA float64
		if len(pts) > 0 {
			avgLoadA = getWeightedPercentile(pts, pct)
			expectedLoadA = getWeightedMean(pts)
			if settings.HomeLoadPredictionStrategy == "conservative" {
				expectedLoadA = max(avgLoadA, expectedLoadA)
			}
			p75LoadA = getWeightedPercentile(pts, 0.75)
		} else if ref, ok := hourBaselineRef[h]; ok && ref > 0 {
			// If all candidate days in the selected pool were filtered out as EV/spike outliers for hour h,
			// fall back to the cross-day 30th-percentile baseline reference for this hour.
			avgLoadA = ref
			expectedLoadA = ref
			p75LoadA = ref
		} else {
			// Sparse history fallback: when no historical points exist for this specific hour,
			// fall back to the site's overall average load rather than collapsing to standby refrigerator floor.
			avgLoadA = overallAvgLoad
			expectedLoadA = overallAvgLoad
			p75LoadA = overallAvgLoad
		}

		// Apply symmetric extreme-weather safeguards and weighted-median pool temperature-gap extrapolation
		// for both hot heatwaves (cooling) and cold snaps (electric heating). Using the weighted median
		// (0.50) of exact-hour temperatures resists distant mild-day tails while decaying to zero once
		// cold/hot snap days represent half the weighted pool.
		effectivePoolTempC := 0.0
		hasEffectivePoolTemp := len(exactTempPts) > 0
		if hasEffectivePoolTemp {
			effectivePoolTempC = getWeightedPercentile(exactTempPts, 0.50)
		}
		if extraKWH, reason := computeExtremeWeatherExtraKWH(
			hvac,
			allValidExactHourTempLoads[h],
			h,
			targetTemp,
			hasTargetTemp,
			hasHistTempForHour,
			minHistTemp,
			maxHistTemp,
			effectivePoolTempC,
			hasEffectivePoolTemp,
			avgLoadA,
		); extraKWH > 0 {
			log.Ctx(ctx).DebugContext(
				ctx,
				"applying extreme weather HVAC load adjustment",
				slog.Int("hour", h),
				slog.String("reason", reason),
				slog.Float64("targetTempC", targetTemp),
				slog.Float64("minHistTempC", minHistTemp),
				slog.Float64("maxHistTempC", maxHistTemp),
				slog.Float64("effectivePoolTempC", effectivePoolTempC),
				slog.Float64("baseLoadKWH", avgLoadA),
				slog.Float64("extraLoadKWH", extraKWH),
			)
			avgLoadA += extraKWH
			expectedLoadA += extraKWH
			p75LoadA += extraKWH
		}

		// Solar prediction.
		// WeatherSolar uses forecasted values (preferred), falling back to SmoothedSolar if forecast is absent.
		avgSolar := 0.0
		if len(weather) > 0 {
			if ws, ok := weatherSolar[targetTime.Truncate(time.Hour).Unix()]; ok {
				avgSolar = ws.SolarKWH
			}
		} else {
			avgSolar = smoothedSolar[h]
		}

		rawAvgLoadA[h] = avgLoadA
		rawExpectedLoadA[h] = expectedLoadA
		rawP75LoadA[h] = p75LoadA
		rawSolar[h] = avgSolar
	}

	if detectedShift == "none" {
		applyOutlierDayCalmMorningDampening(
			ctx,
			&rawAvgLoadA,
			&rawExpectedLoadA,
			&rawP75LoadA,
			hasHour,
			targetTimes,
			now,
			loc,
			currentHour,
			todayStr,
			dayMap,
			validDaysMap,
			historicalVacationDays,
			dayOfWeekOutliers,
			ignoredOutlierHours,
			weatherByHour,
			standbyLoad,
			settings,
		)
	}

	var sumActiveLoadA float64
	var activeHourCount int
	for h := 0; h < 24; h++ {
		if hasHour[h] {
			sumActiveLoadA += max(0.0, rawAvgLoadA[h]-standbyLoad)
			activeHourCount++
		}
	}
	dayMeanActiveLoadA := 0.0
	if activeHourCount > 0 {
		dayMeanActiveLoadA = sumActiveLoadA / float64(activeHourCount)
	}

	result := make([]TimeProfile, 24)
	for h := 0; h < 24; h++ {
		if !hasHour[h] {
			result[h] = TimeProfile{Hour: h}
			continue
		}
		avgLoadA := rawAvgLoadA[h]
		expectedLoadA := rawExpectedLoadA[h]
		p75LoadA := rawP75LoadA[h]

		// Apply the adaptive shift derived via Option G.
		// Negative shifts subtract strictly from active load above standbyLoad to protect the empirical standby floor.
		// Positive shifts are weighted proportionally to each hour's active load share so overnight sleeping hours
		// are not artificially inflated by daytime/evening load shifts.
		var finalHomeLoadACAdj, finalExpectedHomeLoad, finalP75HomeLoad float64
		if appliedShift < 0 {
			activeAvg := max(0.0, avgLoadA-standbyLoad)
			activeExpected := max(0.0, expectedLoadA-standbyLoad)
			activeP75 := max(0.0, p75LoadA-standbyLoad)
			finalHomeLoadACAdj = standbyLoad + max(0.0, activeAvg+appliedShift)
			finalExpectedHomeLoad = standbyLoad + max(0.0, activeExpected+appliedShift)
			finalP75HomeLoad = standbyLoad + max(0.0, activeP75+appliedShift)
		} else {
			shiftWeight := 1.0
			if dayMeanActiveLoadA > 0.1 {
				activeAvg := max(0.0, avgLoadA-standbyLoad)
				shiftWeight = min(1.5, activeAvg/dayMeanActiveLoadA)
			}
			finalHomeLoadACAdj = max(0.9*standbyLoad, avgLoadA+appliedShift*shiftWeight)
			finalExpectedHomeLoad = max(0.9*standbyLoad, expectedLoadA+appliedShift*shiftWeight)
			finalP75HomeLoad = max(0.9*standbyLoad, p75LoadA+appliedShift*shiftWeight)
		}
		if settings.HomeLoadPredictionStrategy == "conservative" {
			finalExpectedHomeLoad = max(finalHomeLoadACAdj, finalExpectedHomeLoad)
		}

		result[h] = TimeProfile{
			TSHourStart:         targetTimes[h],
			Hour:                h,
			AvgSolarKWH:         rawSolar[h],
			AvgHomeLoadKWH:      finalHomeLoadACAdj,
			ExpectedHomeLoadKWH: finalExpectedHomeLoad,
			P75HomeLoadKWH:      finalP75HomeLoad,
		}
	}

	params.DetectedShift = detectedShift
	return result, params, siteEVMinThresholdKW, siteEVMinStepKW
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

func getPopulationStdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	mean := getMean(values)
	var sumSqDiff float64
	for _, val := range values {
		sumSqDiff += (val - mean) * (val - mean)
	}
	return math.Sqrt(sumSqDiff / float64(len(values)))
}

func getHomeKWHPointsStdDev(points []types.EnergyStats) float64 {
	if len(points) == 0 {
		return 0.0
	}
	var sum float64
	for _, p := range points {
		sum += p.HomeKWH
	}
	avg := sum / float64(len(points))
	var varSum float64
	for _, p := range points {
		varSum += (p.HomeKWH - avg) * (p.HomeKWH - avg)
	}
	return math.Sqrt(varSum / float64(len(points)))
}

type outlierPeriod string

const (
	outlierPeriodMorning   outlierPeriod = "morning"
	outlierPeriodAfternoon outlierPeriod = "afternoon"
	outlierPeriodEvening   outlierPeriod = "evening"
	outlierPeriodWaking    outlierPeriod = "waking"
	outlierPeriodWholeDay  outlierPeriod = "wholeDay"
)

type outlierWindowDef struct {
	startHr        int
	endHr          int
	minHours       int
	isMorning      bool
	tStatThreshold float64
}

func (w outlierWindowDef) containsHour(h int) bool {
	return h >= w.startHr && h <= w.endHr
}

var outlierPeriodOrder = []outlierPeriod{
	outlierPeriodMorning,
	outlierPeriodAfternoon,
	outlierPeriodEvening,
	outlierPeriodWaking,
	outlierPeriodWholeDay,
}

var outlierWindows = map[outlierPeriod]outlierWindowDef{
	outlierPeriodMorning:   {startHr: 6, endHr: 10, minHours: 3, isMorning: true, tStatThreshold: dayOfWeekOutlierMorningTStatThreshold},
	outlierPeriodAfternoon: {startHr: 11, endHr: 16, minHours: 4, tStatThreshold: dayOfWeekOutlierNonMorningTStatThreshold},
	outlierPeriodEvening:   {startHr: 17, endHr: 21, minHours: 3, tStatThreshold: dayOfWeekOutlierNonMorningTStatThreshold},
	outlierPeriodWaking:    {startHr: 6, endHr: 21, minHours: 10, tStatThreshold: dayOfWeekOutlierNonMorningTStatThreshold},
	outlierPeriodWholeDay:  {startHr: 0, endHr: 23, minHours: 16, tStatThreshold: dayOfWeekOutlierNonMorningTStatThreshold},
}

// weekdayOutlierPeriods records which time-of-day windows historically exhibit a statistically
// significant recurring load surge on a given day of the week.
type weekdayOutlierPeriods struct {
	Morning   bool
	Afternoon bool
	Evening   bool
	Waking    bool
	WholeDay  bool
}

func (p weekdayOutlierPeriods) any() bool {
	return p.Morning || p.Afternoon || p.Evening || p.Waking || p.WholeDay
}

func (p weekdayOutlierPeriods) coversHour(h int) bool {
	if p.WholeDay && outlierWindows[outlierPeriodWholeDay].containsHour(h) {
		return true
	}
	if p.Waking && outlierWindows[outlierPeriodWaking].containsHour(h) {
		return true
	}
	if p.Morning && outlierWindows[outlierPeriodMorning].containsHour(h) {
		return true
	}
	if p.Afternoon && outlierWindows[outlierPeriodAfternoon].containsHour(h) {
		return true
	}
	if p.Evening && outlierWindows[outlierPeriodEvening].containsHour(h) {
		return true
	}
	return false
}

// detectDayOfWeekOutliers identifies which weekend days (Saturday, Sunday) and time-of-day periods
// (defined in outlierWindows) historically exhibit a statistically significant increase in energy usage
// compared to their 2 preceding days (or Thursday+Friday for Sunday).
// Evaluating sub-periods alongside waking/whole-day prevents a quiet afternoon from diluting a high Saturday
// morning routine while also catching weekend routines whose hours shift across the afternoon or span across
// the morning-afternoon boundary. Non-morning periods skip classification when the target day's mean period
// temperature differed from preceding days by >= weatherDrivenSurgeTempDeltaC so hot summer HVAC spells on
// specific calendar dates do not masquerade as recurring weekend chore schedules.
// Weekdays (Monday-Friday) are intentionally skipped: backtesting across 50 production sites showed that
// weekday triggers (especially Monday compared against quieter weekends and small-sample Thu/Fri noise)
// caused net MAE and over-prediction regressions while weekend triggers improved weekend accuracy.
func detectDayOfWeekOutliers(
	ctx context.Context,
	loc *time.Location,
	dayMap map[string]*dayPoints,
	validDaysMap map[string]bool,
	todayStr string,
	ignoredOutlierHours map[dayHourKey]bool,
	weatherByHour map[time.Time]float64,
) map[time.Weekday]weekdayOutlierPeriods {
	outliers := make(map[time.Weekday]weekdayOutlierPeriods)

	type parsedValidDay struct {
		dateStr string
		dTime   time.Time
		nextDay time.Time
		wd      time.Weekday
		day     *dayPoints
	}

	var sortedDateKeys []string
	for dateStr, ok := range validDaysMap {
		if ok && dateStr != todayStr {
			sortedDateKeys = append(sortedDateKeys, dateStr)
		}
	}
	sort.Strings(sortedDateKeys)

	parsedDays := make([]parsedValidDay, 0, len(sortedDateKeys))
	for _, dateStr := range sortedDateKeys {
		d := dayMap[dateStr]
		if d == nil {
			continue
		}
		dTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			continue
		}
		wd := dTime.Weekday()
		if wd != time.Thursday && wd != time.Friday && wd != time.Saturday && wd != time.Sunday {
			continue
		}
		parsedDays = append(parsedDays, parsedValidDay{
			dateStr: dateStr,
			dTime:   dTime,
			nextDay: dTime.AddDate(0, 0, 1),
			wd:      wd,
			day:     d,
		})
	}

	type dayWindowStats struct {
		wd      time.Weekday
		avgKWH  float64
		hasLoad bool
		tempC   float64
		hasTemp bool
	}

	for _, period := range outlierPeriodOrder {
		win := outlierWindows[period]
		winLen := win.endHr - win.startHr + 1
		stats := make([]dayWindowStats, 0, len(parsedDays))

		for _, pd := range parsedDays {
			var loadSumKWH float64
			var loadCount int
			for _, pt := range pd.day.points {
				if pt.HomeKWH <= 0.0 {
					continue
				}
				hr := pt.TSHourStart.In(loc).Hour()
				if !win.containsHour(hr) {
					continue
				}
				if ignoredOutlierHours[dayHourKey{date: pd.dateStr, hour: hr}] {
					continue
				}
				loadSumKWH += pt.HomeKWH
				loadCount++
			}

			ds := dayWindowStats{wd: pd.wd}
			if loadCount >= win.minHours {
				ds.avgKWH = loadSumKWH / float64(loadCount)
				ds.hasLoad = true
			}

			if len(weatherByHour) > 0 {
				var tempSumC float64
				var tempCount int
				for ts := pd.dTime; ts.Before(pd.nextDay); ts = ts.Add(time.Hour) {
					hr := ts.In(loc).Hour()
					if !win.containsHour(hr) {
						continue
					}
					if tc, hasTC := weatherByHour[ts.UTC()]; hasTC {
						tempSumC += tc
						tempCount++
					}
				}
				if tempCount > 0 && tempCount*2 >= winLen {
					ds.tempC = tempSumC / float64(tempCount)
					ds.hasTemp = true
				}
			}

			stats = append(stats, ds)
		}

		// Test weekend days (Saturday and Sunday) against their 2 immediately preceding calendar days
		// (p1 = wd-1, p2 = wd-2) because those recent days carry the highest exponential recency weights
		// (0.95^1 and 0.95^2) in the hourly prediction pool and pull down weekend forecasts when weekends
		// have a much higher load profile.
		// - For Saturday (wd = 6), p1 and p2 are Friday and Thursday (the last 2 workdays).
		// - For Sunday (wd = 0), p1 and p2 are Saturday and Friday. When a household is home all weekend,
		//   both Saturday and Sunday are elevated, so comparing Sunday against Saturday + Friday mixes a high
		//   weekend day (Saturday) into the baseline pool—inflating both the baseline mean and variance so
		//   Welch's t-test fails. Therefore, Sunday also falls back to testing against Thursday + Friday
		//   (the same 2 pre-weekend workdays that Saturday is tested against).
		for _, wd := range []time.Weekday{time.Sunday, time.Saturday} {
			wdInt := int(wd)
			p1 := time.Weekday((wdInt - 1 + 7) % 7)
			p2 := time.Weekday((wdInt - 2 + 7) % 7)

			var targetLoadsKWH, prevLoadsKWH, sunTFLoadsKWH []float64
			var targetTempsC, prevTempsC, sunTFTempsC []float64

			for _, ds := range stats {
				if !ds.hasLoad {
					continue
				}
				if ds.wd == wd {
					targetLoadsKWH = append(targetLoadsKWH, ds.avgKWH)
					if ds.hasTemp {
						targetTempsC = append(targetTempsC, ds.tempC)
					}
				} else {
					if ds.wd == p1 || ds.wd == p2 {
						prevLoadsKWH = append(prevLoadsKWH, ds.avgKWH)
						if ds.hasTemp {
							prevTempsC = append(prevTempsC, ds.tempC)
						}
					}
					// Sunday special case: also test against Thursday + Friday to capture weekend routines
					// where Saturday is also high.
					if wd == time.Sunday && (ds.wd == time.Thursday || ds.wd == time.Friday) {
						sunTFLoadsKWH = append(sunTFLoadsKWH, ds.avgKWH)
						if ds.hasTemp {
							sunTFTempsC = append(sunTFTempsC, ds.tempC)
						}
					}
				}
			}

			evalCandidate := func(pLoadsKWH, pTempsC []float64) (bool, float64, float64) {
				if len(targetLoadsKWH) < dayOfWeekOutlierMinTargetSamples || len(pLoadsKWH) < dayOfWeekOutlierMinPrecedingSamples {
					return false, 0, 0
				}
				meanTargetKWH := getMean(targetLoadsKWH)
				meanPrevKWH := getMean(pLoadsKWH)
				r := meanTargetKWH / math.Max(0.1, meanPrevKWH)
				t := calculateWelchT(targetLoadsKWH, pLoadsKWH)
				if r < dayOfWeekOutlierRatioThreshold || t < win.tStatThreshold {
					return false, r, t
				}
				if !win.isMorning && len(targetTempsC) >= 2 && len(pTempsC) >= 3 {
					if math.Abs(getMean(targetTempsC)-getMean(pTempsC)) >= weatherDrivenSurgeTempDeltaC {
						return false, r, t
					}
				}
				return true, r, t
			}

			isOutlier, ratio, tStat := evalCandidate(prevLoadsKWH, prevTempsC)
			detectedBy := string(period)
			prevSamples := len(prevLoadsKWH)

			if !isOutlier && wd == time.Sunday {
				if ok, rTF, tTF := evalCandidate(sunTFLoadsKWH, sunTFTempsC); ok {
					isOutlier = true
					ratio = rTF
					tStat = tTF
					detectedBy = string(period) + "VsThuFri"
					prevSamples = len(sunTFLoadsKWH)
				}
			}

			if isOutlier {
				flags := outliers[wd]
				switch period {
				case outlierPeriodMorning:
					flags.Morning = true
				case outlierPeriodAfternoon:
					flags.Afternoon = true
				case outlierPeriodEvening:
					flags.Evening = true
				case outlierPeriodWaking:
					flags.Waking = true
				case outlierPeriodWholeDay:
					flags.WholeDay = true
				}
				outliers[wd] = flags
				log.Ctx(ctx).DebugContext(
					ctx,
					"detected day-of-week outlier load surge",
					slog.String("weekday", wd.String()),
					slog.String("period", string(period)),
					slog.String("detectedBy", detectedBy),
					slog.Float64("surgeRatio", ratio),
					slog.Float64("tStat", tStat),
					slog.Int("targetSamples", len(targetLoadsKWH)),
					slog.Int("precedingSamples", prevSamples),
					slog.Any("flags", flags),
				)
			}
		}
	}

	return outliers
}

func getMean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0.0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// calculateWelchT computes the Welch's t-test statistic (unequal variances t-test) between two sample slices:
//
//	t = (mean_a - mean_b) / sqrt( (std_a^2 / N_a) + (std_b^2 / N_b) )
//
// Why this is important for load forecasting:
//  1. Unequal Variances (Heteroscedasticity): Household consumption on high-activity/chore days (e.g. Saturdays)
//     exhibits much higher variance (due to intermittent laundry, cooking, or waking times) than quiet weekday mornings.
//     Standard Student's t-test assumes equal variances (homoscedasticity) and pools variance, which distorts significance.
//  2. Unequal Sample Sizes (N_a != N_b): In a typical 4-5 week history window, we evaluate ~4-5 target weekday occurrences
//     against 8-10 preceding weekday occurrences (e.g. comparing Saturdays to Thursdays and Fridays). Welch's t-test
//     handles differing sample counts without bias.
//  3. Noise Filter vs Simple Ratio: Checking only a ratio (e.g. mean_a / mean_b >= 1.25) is susceptible to false positives
//     when sample sizes are small or when a single high-power appliance cycle skews a small sample. Requiring t >= 2.30-2.50
//     (multiple-testing-adjusted one-tailed significance across weekdays and time periods) ensures the surge represents a
//     statistically genuine, repeatable routine.
func calculateWelchT(a, b []float64) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0.0
	}
	meanA := getMean(a)
	meanB := getMean(b)
	stdA := getStdDev(a)
	stdB := getStdDev(b)
	se := math.Sqrt((stdA*stdA)/float64(len(a)) + (stdB*stdB)/float64(len(b)))
	return (meanA - meanB) / math.Max(0.01, se)
}

type weightedPoint struct {
	Value  float64
	Weight float64
}

// getWeightedMean computes the weighted arithmetic mean of a slice of weighted data points.
// Points with non-positive, NaN, or infinite weights or values are ignored.
func getWeightedMean(points []weightedPoint) float64 {
	if len(points) == 0 {
		return 0.0
	}
	var sumVal, sumWeight float64
	for _, p := range points {
		if !(p.Weight > 0) || math.IsInf(p.Weight, 0) || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		sumVal += p.Value * p.Weight
		sumWeight += p.Weight
	}
	if sumWeight > 0 {
		return sumVal / sumWeight
	}
	for _, p := range points {
		if !math.IsNaN(p.Value) && !math.IsInf(p.Value, 0) {
			return p.Value
		}
	}
	return 0.0
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

	// Sort by value ascending, and break ties by weight descending for determinism
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].Value != points[j].Value {
			return points[i].Value < points[j].Value
		}
		return points[i].Weight > points[j].Weight
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
			diff := pList[k+1] - pList[k]
			if diff <= 1e-9 {
				return points[k].Value
			}
			ratio := (percentile - pList[k]) / diff
			return points[k].Value + ratio*(points[k+1].Value-points[k].Value)
		}
	}

	return points[n-1].Value
}

// detectLoadShift automatically identifies if the house is undergoing a structural
// change in energy consumption patterns (vacation mode / shift down) using yesterday's daily total
// and today's cumulative morning sum.
// Note: We intentionally DO NOT shift up (Visitor Mode). High energy usage spikes (e.g. charging EVs,
// running AC on hot days) can easily cause single or multi-day false positives. Shifting up aggressively
// reserves battery capacity when it isn't always a sustained structural change, leading to suboptimal
// economics. We rely on the standard predictive modeling (Q3/median) to naturally accommodate increased usage.
func detectLoadShift(
	ctx context.Context,
	now time.Time,
	loc *time.Location,
	dayMap map[string]*dayPoints,
	dayAveragesMap map[string]float64,
	todayStr string,
	yesterdayStr string,
	currentHour int,
	standbyLoad float64,
) string {
	const dailyAveragesRequired = 4
	// Identify baseline days of normal occupancy (excluding yesterday, today, and past daily outliers).
	// We evaluate Active Energy (energy above the site's empirical standby baseline load)
	// to cleanly separate human active occupancy from constant standby power (refrigerators, routers, modems).
	dailyActiveMap := make(map[string]float64)
	var dailyActiveAveragesForBaseline []float64
	for dateStr, avg := range dayAveragesMap {
		activeAvg := max(0.0, avg-standbyLoad)
		dailyActiveMap[dateStr] = activeAvg
		if dateStr != todayStr && dateStr != yesterdayStr {
			dailyActiveAveragesForBaseline = append(dailyActiveAveragesForBaseline, activeAvg)
		}
	}

	var dailyLowerBoundActive, dailyUpperBoundActive float64
	var baselineDays []string

	if len(dailyActiveAveragesForBaseline) < dailyAveragesRequired {
		return "none"
	}

	sorted := make([]float64, len(dailyActiveAveragesForBaseline))
	copy(sorted, dailyActiveAveragesForBaseline)
	sort.Float64s(sorted)
	n := len(sorted)
	q1 := sorted[int(math.Round(float64(n-1)*0.25))]
	q2 := sorted[int(math.Round(float64(n-1)*0.50))]
	q3 := sorted[int(math.Round(float64(n-1)*0.75))]
	dailyQ2Active := q2
	dailyQ3Active := q3

	// Calculate yesterday's standard deviation (requiring >= 18 hours so a partial day with missing
	// afternoon/evening telemetry cannot falsely trigger vacation mode).
	yStdDev := 1.0
	hasYStdDev := false
	if yPts, ok := dayMap[yesterdayStr]; ok && len(yPts.points) >= 18 {
		yStdDev = getHomeKWHPointsStdDev(yPts.points)
		hasYStdDev = true
	}

	// If a prolonged vacation spans > 25% of the history window (e.g. 9+ days in a 35-day window),
	// Q1 collapses down to the vacation load level (< 25% of median active energy Q2) while yesterday
	// remains flat (< vacationMorningFlatnessStdDevCeiling). Anchor Q1 to Q2 so vacation mode stays active.
	if q1 < q2*loadShiftOutlierFloorFraction && hasYStdDev && yStdDev < vacationMorningFlatnessStdDevCeiling {
		log.Ctx(ctx).DebugContext(
			ctx,
			"prolonged vacation collapsed Q1 active load in detectLoadShift, anchoring Q1 to median Q2",
			slog.Float64("q1ActiveKWH", q1),
			slog.Float64("q2ActiveKWH", q2),
			slog.Float64("yesterdayStdDevKWH", yStdDev),
		)
		q1 = q2
	}

	iqr := q3 - q1
	// Calculate the daily lower bound for active energy to detect vacations.
	// We cannot simply use (q1 - loadShiftOutlierIQRExpansion*iqr) because:
	// 1. Minimum Depth Requirement (q1 * ceilingCap): If a site has extremely consistent load (IQR near 0),
	//    the standard formula would equal Q1. This would cause a normal day that is just slightly
	//    below Q1 to falsely trigger a vacation. Capping the bound at a fraction of Q1 ensures that
	//    a vacation requires a meaningful structural drop (at least a reduction below ceilingCap).
	// 2. Adaptive Floor (loadShiftOutlierFloorFraction * q1): If a site has massive variance (huge IQR),
	//    the standard formula could be negative. Since active energy is bounded at 0, it would never fall
	//    below a negative lower bound, causing us to miss vacations. Wrapping the bound with a minimum of
	//    25% of Q1 (and an absolute floor of 0.02) guarantees vacation mode triggers correctly.
	dailyLowerBoundActive = min(q1*loadShiftOutlierCeilingCap, max(standbyActiveEnergyFloor, q1*loadShiftOutlierFloorFraction, q1-loadShiftOutlierIQRExpansion*iqr))
	// Calculate the daily upper bound for active energy to identify high outliers.
	// Note: Unlike the lower bound, we don't need max/min constraints here because:
	// 1. No Upper Boundary: Energy usage can scale infinitely upwards, so there is no physical limit
	//    (like 0.0 for the lower bound) that the formula could cross to make outlier detection impossible.
	// 2. Loose vs Strict Filtering: Since we no longer shift load upwards (Visitor Mode is disabled),
	//    the upper bound is used solely to filter outlier days from baselineDays. If a home is extremely
	//    consistent (IQR ≈ 0) and we over-filter a few normal days slightly above Q3, the baseline is
	//    still fine as long as we have 4 baseline days. But under-filtering a massive visitor day would
	//    skew the baseline upwards, which would fail to trigger vacation mode.
	dailyUpperBoundActive = q3 + loadShiftOutlierIQRExpansion*iqr

	for dateStr, activeAvg := range dailyActiveMap {
		if dateStr != todayStr && dateStr != yesterdayStr {
			if activeAvg >= dailyLowerBoundActive && activeAvg <= dailyUpperBoundActive {
				baselineDays = append(baselineDays, dateStr)
			}
		}
	}

	if len(baselineDays) < dailyAveragesRequired {
		log.Ctx(ctx).DebugContext(
			ctx,
			"missing enough baseline days for load shift detection",
			slog.Int("currentHour", currentHour),
			slog.Float64("dailyLowerBound", dailyLowerBoundActive),
			slog.Float64("dailyUpperBound", dailyUpperBoundActive),
			slog.Any("baselineDays", baselineDays),
		)
		return "none"
	}

	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz == "" {
			return fallback
		}
		if now.Location() != nil && now.Location().String() == tz {
			return now.Location()
		}
		if l, ok := locCache[tz]; ok && l != nil {
			return l
		}
		if l, err := time.LoadLocation(tz); err == nil {
			locCache[tz] = l
			return l
		}
		return fallback
	}

	// Calculate Q1 of standard deviations across baseline normal days for site-adaptive volatility checks
	var baselineStdDevs []float64
	for _, dStr := range baselineDays {
		if d, ok := dayMap[dStr]; ok && len(d.points) >= 18 {
			baselineStdDevs = append(baselineStdDevs, getHomeKWHPointsStdDev(d.points))
		}
	}

	q1StdDev := 1.0
	if len(baselineStdDevs) >= dailyAveragesRequired-1 {
		sortedStd := make([]float64, len(baselineStdDevs))
		copy(sortedStd, baselineStdDevs)
		sort.Float64s(sortedStd)
		q1StdDev = sortedStd[int(math.Round(float64(len(sortedStd)-1)*0.25))]
	}

	// Yesterday is the first completed day of the suspected shift.
	// Verifying that yesterday was a completed daily outlier (hasYStdDev: >= 18 hours) is a prerequisite
	// for triggering a shift. We allow yesterdayIsLow to trigger if active avg is below dailyLowerBoundActive
	// OR if active avg is below ceiling cap (55% Q1) AND stddev is adaptively low (< 0.25 * q1StdDev) AND q1StdDev >= 0.25.
	// This gating ensures continuous high flat load (e.g. EV charging at 7.2 kW) is never misclassified as vacation.
	yActive, yExists := dailyActiveMap[yesterdayStr]
	yesterdayIsLow := yExists && hasYStdDev && (yActive < dailyLowerBoundActive || (yActive < q1*loadShiftOutlierCeilingCap && q1StdDev >= vacationBaselineMinStdDevKWH && yStdDev < vacationVolatilityDropFraction*q1StdDev))
	// High-standby households (e.g. 0.5-0.8 kWh constant background load) with mild non-HVAC days can
	// compress active Q1 near zero while normal median/upper-quartile active load remains significant.
	// Allow yesterdayIsLow when active load drops below 55% of min(Q2, Q3) AND full-day load is flat
	// (< 0.15 kWh and < 25% of baseline Q1 stddev).
	if !yesterdayIsLow && yExists && hasYStdDev {
		activeRefKWH := min(dailyQ2Active, dailyQ3Active)
		if yActive < activeRefKWH*loadShiftOutlierCeilingCap && q1StdDev >= vacationBaselineMinStdDevKWH && yStdDev < min(vacationMorningFlatnessStdDevCeiling, vacationVolatilityDropFraction*q1StdDev) {
			yesterdayIsLow = true
			log.Ctx(ctx).DebugContext(
				ctx,
				"overriding yesterdayIsLow for high-standby flat vacation day",
				slog.String("yesterday", yesterdayStr),
				slog.Float64("yesterdayActiveKWH", yActive),
				slog.Float64("activeRefKWH", activeRefKWH),
				slog.Float64("yesterdayStdDevKWH", yStdDev),
				slog.Float64("baselineQ1StdDevKWH", q1StdDev),
			)
		}
	}
	detectedShift := "none"

	// Compute hourly metrics (Q1 and Q3) across baseline days for early escape checks.
	// Q1 (25th percentile) and Q3 (75th percentile) define the boundaries of normal occupancy hourly loads.
	hourQ1s := make(map[int]float64)
	for h := 0; h < 24; h++ {
		var hourLoads []float64
		for _, dStr := range baselineDays {
			if d, ok := dayMap[dStr]; ok {
				for _, pt := range d.points {
					if pt.TSHourStart.In(getLocation(pt.TimeLocation, loc)).Hour() == h && pt.HomeKWH > 0 {
						hourLoads = append(hourLoads, pt.HomeKWH)
					}
				}
			}
		}
		if len(hourLoads) >= dailyAveragesRequired-1 {
			sort.Float64s(hourLoads)
			n := len(hourLoads)
			hourQ1s[h] = hourLoads[int(math.Round(float64(n-1)*0.25))]
		} else {
			hourQ1s[h] = 0.0
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
		var todayMorningLoads []float64
		if todayPts, ok := dayMap[todayStr]; ok {
			for _, pt := range todayPts.points {
				h := pt.TSHourStart.In(getLocation(pt.TimeLocation, loc)).Hour()
				if h >= 7 && h < currentHour {
					todaySum += max(0.0, pt.HomeKWH-standbyLoad)
					todayCount++
				}
				// Include hour 6 in morning volatility check so currentHour == 9 has 3 completed hours (6, 7, 8).
				if h >= 6 && h < currentHour {
					todayMorningLoads = append(todayMorningLoads, pt.HomeKWH)
				}
			}
		}

		if todayCount > 0 {
			todayMorningStdDev := 1.0
			hasMorningStdDev := false
			if len(todayMorningLoads) >= 3 {
				todayMorningStdDev = getPopulationStdDev(todayMorningLoads)
				hasMorningStdDev = true
			}

			var baselineSums []float64
			for _, dStr := range baselineDays {
				var bSum float64
				var bCount int
				if d, ok := dayMap[dStr]; ok {
					for _, pt := range d.points {
						h := pt.TSHourStart.In(getLocation(pt.TimeLocation, loc)).Hour()
						if h >= 7 && h < currentHour {
							bSum += max(0.0, pt.HomeKWH-standbyLoad)
							bCount++
						}
					}
				}
				if bCount > 0 {
					baselineSums = append(baselineSums, bSum)
				}
			}

			sumLowerBound := 0.0
			if len(baselineSums) >= dailyAveragesRequired-1 {
				sort.Float64s(baselineSums)
				n := len(baselineSums)
				q1 := baselineSums[int(math.Round(float64(n-1)*0.25))]
				q2 := baselineSums[int(math.Round(float64(n-1)*0.50))]
				q3 := baselineSums[int(math.Round(float64(n-1)*0.75))]
				iqr := q3 - q1
				effIQR := max(iqr, 0.1)

				// Floor the active sum lower bound to ensure vacation detection works even with high baseline variance.
				// standbyActiveEnergyFloor represents the absolute minimum hourly average active load expected on vacation.
				sumLowerBound = min(q1*loadShiftOutlierCeilingCap, max(float64(todayCount)*standbyActiveEnergyFloor, q1*loadShiftOutlierFloorFraction, q1-loadShiftOutlierIQRExpansion*effIQR))

				// Vacation Mode Trigger:
				// If yesterday was a completed vacation day (yesterdayIsLow), we maintain vacation mode today if:
				// 1. todaySum < sumLowerBound: Today's active energy sum (hours 7 to current hour) is below the lower bound, OR
				// 2. Today's morning load is below 55% of baseline Q1 AND exhibits unoccupied flatness
				//    (standard deviation below 0.15 kWh or 25% of normal site volatility), OR
				// 3. On high-standby sites where mild days compress morning active Q1, today's morning active sum is
				//    below 55% of min(Q2, Q3) AND morning load stddev is strictly below 0.15 kWh.
				morningFlatAndLow := hasMorningStdDev && todaySum < q1*loadShiftOutlierCeilingCap && todayMorningStdDev < max(vacationMorningFlatnessStdDevCeiling, vacationVolatilityDropFraction*q1StdDev)
				if !morningFlatAndLow && hasMorningStdDev {
					morningRefKWH := min(q2, q3)
					if todaySum < morningRefKWH*loadShiftOutlierCeilingCap && todayMorningStdDev < vacationMorningFlatnessStdDevCeiling {
						morningFlatAndLow = true
						if yesterdayIsLow {
							log.Ctx(ctx).DebugContext(
								ctx,
								"overriding morningFlatAndLow for high-standby flat vacation morning",
								slog.String("today", todayStr),
								slog.Int("currentHour", currentHour),
								slog.Float64("todaySumKWH", todaySum),
								slog.Float64("morningRefKWH", morningRefKWH),
								slog.Float64("todayMorningStdDevKWH", todayMorningStdDev),
							)
						}
					}
				}
				if yesterdayIsLow && (todaySum < sumLowerBound || morningFlatAndLow) {
					detectedShift = "down"
				}
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
			// - Requiring 4 consecutive hours filters out transient noise (e.g. water heater cycles during vacation).
			if detectedShift != "none" {
				escape := true
				var hourLoad float64
				var comparisonHourLoad float64
				var escapeLoads []float64
				for i := 1; i <= loadShiftEscapeHours; i++ {
					checkHour := currentHour - i

					var found bool
					if targetPts, ok := dayMap[todayStr]; ok {
						for _, pt := range targetPts.points {
							h := pt.TSHourStart.In(getLocation(pt.TimeLocation, loc)).Hour()
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
					escapeLoads = append(escapeLoads, hourLoad)

					if detectedShift == "down" {
						comparisonHourLoad = hourQ1s[checkHour]
						if hourLoad < comparisonHourLoad {
							escape = false
							break
						}
					}
				}
				// Guard against premature vacation escape on high-standby homes where constant standby draw
				// matches or slightly exceeds mild-day hourly Q1 (hourQ1s): if all 4 hours remain flat
				// (< 0.15 kWh stddev) and their active load stays below 55% of normal daily active load,
				// the household is still at flat unoccupied standby rather than returning home.
				if escape && detectedShift == "down" && len(escapeLoads) == loadShiftEscapeHours {
					escapeAvgKWH := getMean(escapeLoads)
					escapeStdDevKWH := getPopulationStdDev(escapeLoads)
					escapeActiveKWH := max(0.0, escapeAvgKWH-standbyLoad)
					activeRefKWH := min(dailyQ2Active, dailyQ3Active)
					if escapeActiveKWH < activeRefKWH*loadShiftOutlierCeilingCap && escapeStdDevKWH < vacationMorningFlatnessStdDevCeiling {
						escape = false
						log.Ctx(ctx).DebugContext(
							ctx,
							"prevented premature vacation escape on flat high-standby load",
							slog.Int("currentHour", currentHour),
							slog.Float64("escapeAvgKWH", escapeAvgKWH),
							slog.Float64("escapeActiveKWH", escapeActiveKWH),
							slog.Float64("escapeStdDevKWH", escapeStdDevKWH),
							slog.Float64("activeRefKWH", activeRefKWH),
							slog.Float64("standbyLoadKWH", standbyLoad),
						)
					}
				}
				if escape {
					log.Ctx(ctx).DebugContext(
						ctx,
						"early load shift escape",
						slog.String("shiftType", detectedShift),
						slog.Int("currentHour", currentHour),
						slog.Bool("yesterdayIsLow", yesterdayIsLow),
						slog.Float64("todaySum", todaySum),
						slog.Int("todayCount", todayCount),
						slog.Float64("sumLowerBound", sumLowerBound),
						slog.Any("baselineSums", baselineSums),
						slog.Float64("hourLoad", hourLoad),
						slog.Float64("comparisonHourLoad", comparisonHourLoad),
						slog.Float64("dailyLowerBound", dailyLowerBoundActive),
						slog.Float64("dailyUpperBound", dailyUpperBoundActive),
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
					slog.Float64("yesterdayActive", yActive),
					slog.Float64("yesterdayStdDev", yStdDev),
					slog.Float64("baselineQ1StdDev", q1StdDev),
					slog.Float64("todayMorningStdDev", todayMorningStdDev),
					slog.Float64("todaySum", todaySum),
					slog.Int("todayCount", todayCount),
					slog.Float64("sumLowerBound", sumLowerBound),
					slog.Any("baselineSums", baselineSums),
					slog.Float64("dailyLowerBound", dailyLowerBoundActive),
					slog.Float64("dailyUpperBound", dailyUpperBoundActive),
					slog.Any("baselineDays", baselineDays),
				)
			}
		}
	} else if currentHour < 9 && yesterdayIsLow {
		tIsLow := yesterdayIsLow

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
					if pt.TSHourStart.In(getLocation(pt.TimeLocation, loc)).Hour() == checkHour {
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
		} else {
			// Verify each of the last x-1 completed hours to ensure they do not contradict the active shift:
			// - Vacation (tIsLow): If any hour exceeds 1.5x the median, it indicates active occupancy.
			var numMedians int
			for _, pt := range lastLoads {
				h := pt.Hour
				if med, hasMed := hourMedians[h]; hasMed && med > 0 {
					numMedians++
					if tIsLow && pt.Load > med*1.5 {
						tIsLow = false
					}
				}
			}
			if numMedians < lookbackHours-1 {
				tIsLow = false
			}
		}

		if tIsLow {
			detectedShift = "down"
		} else {
			log.Ctx(ctx).DebugContext(
				ctx,
				"discontinued vacation shift in early morning check",
				slog.Int("currentHour", currentHour),
				slog.Int("lastLoadsCount", len(lastLoads)),
				slog.Float64("decayFactor", loadShiftRecencyDecay),
				slog.Bool("yesterdayIsLow", yesterdayIsLow),
				slog.Any("last6Loads", lastLoads),
				slog.Float64("dailyLowerBound", dailyLowerBoundActive),
				slog.Float64("dailyUpperBound", dailyUpperBoundActive),
			)
		}

		if detectedShift != "none" {
			log.Ctx(ctx).InfoContext(
				ctx,
				"detected continued load shift, applying decay factor shift",
				slog.String("shiftType", detectedShift),
				slog.Float64("decayFactor", loadShiftRecencyDecay),
				slog.Int("currentHour", currentHour),
				slog.Bool("yesterdayIsLow", yesterdayIsLow),
				slog.Any("last6Loads", lastLoads),
				slog.Float64("dailyLowerBound", dailyLowerBoundActive),
				slog.Float64("dailyUpperBound", dailyUpperBoundActive),
			)
		}
	}

	return detectedShift
}

// identifyHistoricalVacationDays identifies dates in history that represent vacation days
// (abnormally low active load or flat, low-volatility signature) so they can be excluded from the
// prediction pool when in normal occupancy mode (detectedShift == "none").
func identifyHistoricalVacationDays(
	ctx context.Context,
	dailyAverages []float64,
	dayAveragesMap map[string]float64,
	dayMap map[string]*dayPoints,
	todayStr string,
	yesterdayStr string,
	standbyLoad float64,
) map[string]bool {
	historicalVacationDays := make(map[string]bool)
	if len(dailyAverages) < 4 {
		return historicalVacationDays
	}

	// Step 1: Filter out high outlier days (e.g. EV charging spikes or extreme heatwaves)
	// using raw daily averages to establish a clean normal occupancy baseline.
	sortedRaw := make([]float64, len(dailyAverages))
	copy(sortedRaw, dailyAverages)
	sort.Float64s(sortedRaw)
	nR := len(sortedRaw)
	q1R := sortedRaw[int(math.Round(float64(nR-1)*0.25))]
	q2R := sortedRaw[int(math.Round(float64(nR-1)*0.50))]
	q3R := sortedRaw[int(math.Round(float64(nR-1)*0.75))]
	iqrR := q3R - q1R
	upperBoundRaw := q3R + 1.5*iqrR

	// Step 2: Collect Active Energy (energy above standby baseline load) for non-outlier historical days.
	// Active energy isolates human activity (HVAC, lighting, cooking) from constant background power (modems, refrigerators).
	var normalActiveAverages []float64
	for dateStr, avg := range dayAveragesMap {
		if dateStr != todayStr && dateStr != yesterdayStr && avg <= upperBoundRaw {
			normalActiveAverages = append(normalActiveAverages, max(0.0, avg-standbyLoad))
		}
	}

	if len(normalActiveAverages) < 3 {
		return historicalVacationDays
	}

	sort.Float64s(normalActiveAverages)
	nA := len(normalActiveAverages)
	// Step 3: Use Q2 (median) and Q3 (75th percentile) of active averages as normal occupancy anchors.
	q2A := normalActiveAverages[int(math.Round(float64(nA-1)*0.50))]
	q3A := normalActiveAverages[int(math.Round(float64(nA-1)*0.75))]

	if q3A <= standbyActiveEnergyFloor {
		return historicalVacationDays
	}

	// Step 4: Calculate active energy thresholds relative to normal occupancy Q3 active load.
	// normalOccupancyActiveFloor (55% of Q3): Floor used to collect normal occupancy days for stddev calculation.
	// magnitudeDropActiveFloor (25% of Q3): Floor used for Condition A magnitude drop detection.
	normalOccupancyActiveFloor := max(standbyActiveEnergyFloor, q3A*loadShiftOutlierCeilingCap)
	magnitudeDropActiveFloor := max(standbyActiveEnergyFloor, q3A*loadShiftOutlierFloorFraction)

	// Step 5: Compute baseline daily load volatility (standard deviation) across normal occupancy days.
	// This establishes the site's natural daily volatility signature (q1StdDevA).
	var stdDevsA []float64
	for dateStr, avg := range dayAveragesMap {
		if dateStr != todayStr && dateStr != yesterdayStr && avg <= upperBoundRaw {
			activeAvg := max(0.0, avg-standbyLoad)
			if activeAvg >= normalOccupancyActiveFloor {
				if d := dayMap[dateStr]; d != nil && len(d.points) >= 18 {
					stdDevsA = append(stdDevsA, getHomeKWHPointsStdDev(d.points))
				}
			}
		}
	}

	q1StdDevA := 0.0
	if len(stdDevsA) >= 3 {
		sort.Float64s(stdDevsA)
		q1StdDevA = stdDevsA[int(math.Round(float64(len(stdDevsA)-1)*0.25))]
	}

	if q1R < q2R*loadShiftOutlierFloorFraction {
		log.Ctx(ctx).DebugContext(
			ctx,
			"prolonged vacation collapsed Q1 raw load in identifyHistoricalVacationDays, anchoring rawCeiling to Q2",
			slog.Float64("q1RawKWH", q1R),
			slog.Float64("q2RawKWH", q2R),
			slog.Float64("q3RawKWH", q3R),
		)
	}

	// Step 6: Evaluate each historical date against dual vacation criteria:
	// - Condition A (Magnitude Drop): Active energy dropped below 25% of normal Q3 active baseline (magnitudeDropActiveFloor).
	// - Condition B (Gated Volatility Drop): Active energy is below 55% of Q3 AND load volatility dropped below 25%
	//   of normal site volatility (indicating a flat, unoccupied household load signature).
	hsVacCandidates := make(map[string]bool)
	nonYesterdayHSVacCount := 0
	for dateStr, avg := range dayAveragesMap {
		// We skip today (an incomplete day). Yesterday is evaluated here because historicalVacationDays
		// is only used when detectedShift == "none" (normal occupancy mode); if the household returned
		// from vacation today, yesterday's completed vacation day must be excluded from the normal pool.
		if dateStr == todayStr {
			continue
		}
		activeAvg := max(0.0, avg-standbyLoad)
		dStdDev := 1.0
		hasDStdDev := false
		if d := dayMap[dateStr]; d != nil && len(d.points) >= 18 {
			dStdDev = getHomeKWHPointsStdDev(d.points)
			hasDStdDev = true
		}

		// Require raw daily average to fall below 55% of baseline Q1 (or Q2 when active load is at standby
		// or a prolonged vacation > 25% of history collapsed Q1R below 25% of median load Q2R).
		rawCeiling := q1R * loadShiftOutlierCeilingCap
		if activeAvg <= standbyActiveEnergyFloor || q1R < q2R*loadShiftOutlierFloorFraction {
			rawCeiling = q2R * loadShiftOutlierCeilingCap
		}

		if avg < rawCeiling && activeAvg < normalOccupancyActiveFloor && (activeAvg < magnitudeDropActiveFloor || (hasDStdDev && q1StdDevA >= vacationBaselineMinStdDevKWH && dStdDev < vacationVolatilityDropFraction*q1StdDevA)) {
			historicalVacationDays[dateStr] = true
			log.Ctx(ctx).DebugContext(
				ctx,
				"detected historical vacation day, excluding from post-vacation prediction pool",
				slog.String("date", dateStr),
				slog.Float64("avgHomeLoad", avg),
				slog.Float64("activeAvg", activeAvg),
				slog.Float64("rawCeilingKWH", rawCeiling),
				slog.Float64("magnitudeDropActiveFloor", magnitudeDropActiveFloor),
				slog.Float64("dayStdDev", dStdDev),
				slog.Float64("q1StdDev", q1StdDevA),
			)
			continue
		}

		// When constant standby load already accounts for >= 45% of raw Q1 daily load, raw total load
		// cannot drop by 45% (< 0.55 * q1R) even when active human usage drops near zero on vacation.
		// Collect flat unoccupied candidate days (< 55% of median active load Q2A and < min(0.15 kWh, 0.25 * q1StdDevA))
		// and require consecutive multi-day corroboration below so isolated mild non-HVAC days are not excluded.
		if standbyLoad >= q1R*(1.0-loadShiftOutlierCeilingCap) {
			hsActiveCeilingKWH := min(normalOccupancyActiveFloor, q2A*loadShiftOutlierCeilingCap)
			if activeAvg < hsActiveCeilingKWH && hasDStdDev && q1StdDevA >= vacationBaselineMinStdDevKWH && dStdDev < min(vacationMorningFlatnessStdDevCeiling, vacationVolatilityDropFraction*q1StdDevA) {
				hsVacCandidates[dateStr] = true
				if dateStr != yesterdayStr {
					nonYesterdayHSVacCount++
				}
			}
		}
	}

	if len(hsVacCandidates) > 0 && 2*nonYesterdayHSVacCount < nA {
		for dateStr := range hsVacCandidates {
			dt, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			prevDate := dt.AddDate(0, 0, -1).Format("2006-01-02")
			nextDate := dt.AddDate(0, 0, 1).Format("2006-01-02")
			if hsVacCandidates[prevDate] || historicalVacationDays[prevDate] || hsVacCandidates[nextDate] || historicalVacationDays[nextDate] {
				historicalVacationDays[dateStr] = true
				avg := dayAveragesMap[dateStr]
				activeAvg := max(0.0, avg-standbyLoad)
				dStdDev := 0.0
				if d := dayMap[dateStr]; d != nil {
					dStdDev = getHomeKWHPointsStdDev(d.points)
				}
				log.Ctx(ctx).DebugContext(
					ctx,
					"detected high-standby multi-day historical vacation day, excluding from post-vacation prediction pool",
					slog.String("date", dateStr),
					slog.Float64("avgHomeLoadKWH", avg),
					slog.Float64("activeAvgKWH", activeAvg),
					slog.Float64("medianActiveKWH", q2A),
					slog.Float64("dayStdDevKWH", dStdDev),
					slog.Float64("q1StdDevKWH", q1StdDevA),
				)
			}
		}
	} else if len(hsVacCandidates) > 0 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"ignoring high-standby vacation candidates because candidate count is too large relative to valid days",
			slog.Int("hsVacCandidatesCount", len(hsVacCandidates)),
			slog.Int("nonYesterdayHSVacCount", nonYesterdayHSVacCount),
			slog.Int("validDaysCount", nA),
		)
	}

	return historicalVacationDays
}

// applyOutlierDayCalmMorningDampening caps remaining hours of today down to the unboosted all-days
// recency-weighted percentile when today is a historical weekday-outlier day (e.g. a Friday/weekend
// chore day) whose observed morning load (07:00 up to min(12, currentHour)) is calmer than 90% of
// the normal all-days P50 morning baseline across at least 2 observed hours.
// This prevents a single outlier weekday's 3x same-weekday multiplier from over-forecasting the rest
// of today (and triggering unnecessary grid charging) when today actually behaves like a normal day.
func applyOutlierDayCalmMorningDampening(
	ctx context.Context,
	rawAvgLoadA *[24]float64,
	rawExpectedLoadA *[24]float64,
	rawP75LoadA *[24]float64,
	hasHour [24]bool,
	targetTimes [24]time.Time,
	now time.Time,
	loc *time.Location,
	currentHour int,
	todayStr string,
	dayMap map[string]*dayPoints,
	validDaysMap map[string]bool,
	historicalVacationDays map[string]bool,
	dayOfWeekOutliers map[time.Weekday]weekdayOutlierPeriods,
	ignoredOutlierHours map[dayHourKey]bool,
	weatherByHour map[time.Time]float64,
	standbyLoad float64,
	settings types.Settings,
) {
	// Only evaluate from 09:00 onward (ensuring at least 2 completed hours in [07:00, 09:00))
	// and only on weekdays where historical loads qualified for a same-weekday boost.
	nowLocal := now.In(loc)
	if currentHour < 9 || !dayOfWeekOutliers[nowLocal.Weekday()].any() {
		return
	}
	hvac := detectWeatherHVACSensitivity(dayMap, weatherByHour, loc)
	todayPts := dayMap[todayStr]
	if todayPts == nil {
		return
	}

	// Collect today's observed waking morning hours [07:00, min(12, currentHour)),
	// skipping any flagged EV/spike hours so an isolated morning spike does not skew the check.
	endMornHr := min(12, currentHour)
	obsByHr := make(map[int]float64)
	for _, pt := range todayPts.points {
		if pt.HomeKWH <= 0.0 {
			continue
		}
		hr := pt.TSHourStart.In(loc).Hour()
		if hr >= 7 && hr < endMornHr && !ignoredOutlierHours[dayHourKey{date: todayStr, hour: hr}] {
			obsByHr[hr] = pt.HomeKWH
		}
	}
	if len(obsByHr) < 2 {
		return
	}

	// Gather valid non-vacation historical days (excluding today) for the unboosted all-days pool.
	var sortedDates []string
	for dateStr, ok := range validDaysMap {
		if ok && dateStr != todayStr && !historicalVacationDays[dateStr] {
			sortedDates = append(sortedDates, dateStr)
		}
	}
	if len(sortedDates) < 4 {
		return
	}
	sort.Strings(sortedDates)

	var allValidExactHourTempLoads [24][]hourTempLoad
	for _, dateStr := range sortedDates {
		d := dayMap[dateStr]
		if d == nil {
			continue
		}
		for _, pt := range d.points {
			if pt.HomeKWH <= 0.0 {
				continue
			}
			hr := pt.TSHourStart.In(loc).Hour()
			if hr < 0 || hr >= 24 || ignoredOutlierHours[dayHourKey{date: dateStr, hour: hr}] {
				continue
			}
			if histTemp, ok := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]; ok {
				allValidExactHourTempLoads[hr] = append(allValidExactHourTempLoads[hr], hourTempLoad{
					tempC:   histTemp,
					loadKWH: pt.HomeKWH,
				})
			}
		}
	}
	seasonalMedians := computeSeasonalHourMedians(allValidExactHourTempLoads)

	// Precompute daily max temperatures per local date from weatherByHour to mirror the main
	// model's afternoon-to-evening thermal lag weighting in computeAllDaysHourPts.
	dampDayMaxTempC := make(map[string]float64)
	dampDayTempCount := make(map[string]int)
	for tsUTC, tempC := range weatherByHour {
		dStr := tsUTC.In(loc).Format("2006-01-02")
		if dampDayTempCount[dStr] == 0 || tempC > dampDayMaxTempC[dStr] {
			dampDayMaxTempC[dStr] = tempC
		}
		dampDayTempCount[dStr]++
	}
	getDampDayMaxTemp := func(dStr string) (float64, bool) {
		if dampDayTempCount[dStr] >= minWeatherHoursForDailyAverage {
			return dampDayMaxTempC[dStr], true
		}
		return 0, false
	}

	// computeAllDaysHourPts mirrors the main model's recency decay, +/-1h neighbor smoothing,
	// cross-midnight stitches, temperature similarity weighting, and extreme-weather extrapolation for hour h,
	// but treats every historical weekday equally (1x weight) without any same-weekday boost.
	computeAllDaysHourPts := func(h int, targetTime time.Time, basePct float64) ([]weightedPoint, float64) {
		targetTemp, hasTargetTemp := weatherByHour[targetTime.Truncate(time.Hour).UTC()]
		var targetAfternoonPeakC float64
		hasTargetAfternoonPeak := false
		if hasTargetTemp && h >= eveningThermalLagStartHour && h <= eveningThermalLagEndHour {
			targetAfternoonPeakC, hasTargetAfternoonPeak = getDampDayMaxTemp(targetTime.In(loc).Format("2006-01-02"))
		}
		var pts []weightedPoint
		var ptHasTemp []bool
		var sumTempMult float64
		var countTempMult int
		maxHistTemp := -999.0
		minHistTemp := math.MaxFloat64
		hasHistTempForHour := false
		var exactTempPts []weightedPoint
		prevHr := (h - 1 + 24) % 24
		nextHr := (h + 1) % 24

		// Mirror the waking-hour ramp guard and sharp HVAC peak dampening.
		neighborMult := func(hpLocalHour int, loadKWH float64) float64 {
			return computeNeighborMultiplier(h, hpLocalHour, loadKWH, standbyLoad, targetTemp, hasTargetTemp, hvac, seasonalMedians)
		}

		for _, dateStr := range sortedDates {
			d := dayMap[dateStr]
			if d == nil || d.dayTime.IsZero() {
				continue
			}
			ageDays := max(0, int(targetTime.Sub(d.dayTime).Hours()/24))
			baseWeight := math.Pow(homeLoadPredictionRecencyDecay, float64(ageDays))

			addPt := func(pt types.EnergyStats, ptDateStr string, hpLocalHour int, mult float64) {
				if mult <= 0 || pt.HomeKWH <= 0.0 || ignoredOutlierHours[dayHourKey{date: ptDateStr, hour: hpLocalHour}] {
					return
				}
				finalWeight := baseWeight * mult
				hasTemp := false
				if hasTargetTemp {
					if histTemp, ok := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]; ok {
						tempDiff := tempSimilarityDiffC(targetTemp, histTemp)
						if hasTargetAfternoonPeak {
							histPeakC, hasHistPeak := getDampDayMaxTemp(dateStr)
							tempDiff += eveningThermalLagDiffC(targetAfternoonPeakC, hasTargetAfternoonPeak, histPeakC, hasHistPeak)
						}
						tempMult := math.Exp(-tempDiff / tempSimilarityScale)
						finalWeight *= tempMult
						hasTemp = true
						sumTempMult += tempMult
						countTempMult++
						if hpLocalHour == h {
							if histTemp > maxHistTemp {
								maxHistTemp = histTemp
							}
							if histTemp < minHistTemp {
								minHistTemp = histTemp
							}
							hasHistTempForHour = true
							exactTempPts = append(exactTempPts, weightedPoint{Value: histTemp, Weight: finalWeight})
						}
					}
				}
				pts = append(pts, weightedPoint{Value: pt.HomeKWH, Weight: finalWeight})
				ptHasTemp = append(ptHasTemp, hasTemp)
			}

			for _, pt := range d.points {
				if pt.HomeKWH <= 0.0 {
					continue
				}
				hpLocalHour := pt.TSHourStart.In(loc).Hour()
				if hpLocalHour == h {
					addPt(pt, d.date, hpLocalHour, 1.0)
				} else if hpLocalHour == prevHr && h > 0 {
					addPt(pt, d.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
				} else if hpLocalHour == nextHr && h < 23 {
					addPt(pt, d.date, hpLocalHour, neighborMult(hpLocalHour, pt.HomeKWH))
				}
			}

			// Stitch across midnight boundaries (hour 23 of previous day -> hour 0; hour 0 of next day -> hour 23).
			if h == 0 {
				prevDateStr := d.dayTime.AddDate(0, 0, -1).Format("2006-01-02")
				if !historicalVacationDays[prevDateStr] {
					if prevDay := dayMap[prevDateStr]; prevDay != nil {
						for _, pt := range prevDay.points {
							if pt.HomeKWH > 0.0 && pt.TSHourStart.In(loc).Hour() == 23 {
								addPt(pt, prevDay.date, 23, neighborMult(23, pt.HomeKWH))
							}
						}
					}
				}
			}
			if h == 23 {
				nextDateStr := d.dayTime.AddDate(0, 0, 1).Format("2006-01-02")
				if nextDateStr != todayStr && !historicalVacationDays[nextDateStr] {
					if nextDay := dayMap[nextDateStr]; nextDay != nil {
						for _, pt := range nextDay.points {
							if pt.HomeKWH > 0.0 && pt.TSHourStart.In(loc).Hour() == 0 {
								addPt(pt, nextDay.date, 0, neighborMult(0, pt.HomeKWH))
							}
						}
					}
				}
			}
		}

		// Apply average temperature similarity weight to points missing historical weather so they
		// compete fairly against weather-matched points without artificial weight inflation.
		if hasTargetTemp && countTempMult > 0 && countTempMult < len(pts) {
			avgTempMult := sumTempMult / float64(countTempMult)
			for i := range pts {
				if !ptHasTemp[i] {
					pts[i].Weight *= avgTempMult
				}
			}
		}

		extraKWH := 0.0
		if basePct > 0 && len(pts) > 0 {
			baseLoadKWH := getWeightedPercentile(pts, basePct)
			effectivePoolTempC := 0.0
			hasEffectivePoolTemp := len(exactTempPts) > 0
			if hasEffectivePoolTemp {
				effectivePoolTempC = getWeightedPercentile(exactTempPts, 0.50)
			}
			extraKWH, _ = computeExtremeWeatherExtraKWH(
				hvac,
				allValidExactHourTempLoads[h],
				h,
				targetTemp,
				hasTargetTemp,
				hasHistTempForHour,
				minHistTemp,
				maxHistTemp,
				effectivePoolTempC,
				hasEffectivePoolTemp,
				baseLoadKWH,
			)
		}
		return pts, extraKWH
	}

	// Compare today's observed waking-morning sum against the unboosted all-days P50 baseline
	// over the exact same observed hours; bail out unless today is strictly calmer (< 90% of P50).
	var obsMornSumKWH, baseP50MornSumKWH float64
	var mornCount int
	for m := 7; m < endMornHr; m++ {
		obsKWH, hasObs := obsByHr[m]
		if !hasObs {
			continue
		}
		mTargetTime := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), m, 0, 0, 0, loc)
		pts, _ := computeAllDaysHourPts(m, mTargetTime, 0)
		if len(pts) == 0 {
			continue
		}
		obsMornSumKWH += obsKWH
		baseP50MornSumKWH += getWeightedPercentile(pts, 0.50)
		mornCount++
	}

	if mornCount < 2 || baseP50MornSumKWH <= 0.1 {
		return
	}
	if obsMornSumKWH >= calmMorningRatioThreshold*baseP50MornSumKWH {
		log.Ctx(ctx).DebugContext(
			ctx,
			"skipping weekday-outlier calm-morning dampening because observed morning is not calm",
			slog.String("date", todayStr),
			slog.String("weekday", nowLocal.Weekday().String()),
			slog.Int("currentHour", currentHour),
			slog.Int("morningHoursChecked", mornCount),
			slog.Float64("observedMorningSumKWH", obsMornSumKWH),
			slog.Float64("baselineP50MorningSumKWH", baseP50MornSumKWH),
		)
		return
	}

	pct := defaultStrategyPercentile
	if settings.HomeLoadPredictionStrategy == "conservative" {
		pct = conservativeStrategyPercentile
	}

	// Cap remaining hours that fall on today (leaving tomorrow's profile hours untouched) down to
	// the unboosted all-days percentile whenever the weekday-boosted forecast exceeds it.
	cappedHours := 0
	for h := currentHour; h < 24; h++ {
		if !hasHour[h] || targetTimes[h].In(loc).Format("2006-01-02") != todayStr {
			continue
		}
		pts, extraKWH := computeAllDaysHourPts(h, targetTimes[h], pct)
		if len(pts) == 0 {
			continue
		}
		allDaysPctKWH := getWeightedPercentile(pts, pct)
		allDaysExpectedKWH := getWeightedMean(pts)
		if settings.HomeLoadPredictionStrategy == "conservative" {
			allDaysExpectedKWH = max(allDaysPctKWH, allDaysExpectedKWH)
		}
		allDaysP75KWH := getWeightedPercentile(pts, 0.75)
		if extraKWH > 0 {
			allDaysPctKWH += extraKWH
			allDaysExpectedKWH += extraKWH
			allDaysP75KWH += extraKWH
		}
		capped := false
		if allDaysPctKWH < rawAvgLoadA[h] {
			rawAvgLoadA[h] = allDaysPctKWH
			rawP75LoadA[h] = max(rawAvgLoadA[h], min(rawP75LoadA[h], allDaysP75KWH))
			capped = true
		}
		if rawExpectedLoadA != nil && allDaysExpectedKWH < rawExpectedLoadA[h] {
			rawExpectedLoadA[h] = allDaysExpectedKWH
			capped = true
		}
		if capped {
			cappedHours++
		}
	}

	if cappedHours > 0 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"dampened weekday-outlier forecast after calm morning",
			slog.String("date", todayStr),
			slog.String("weekday", nowLocal.Weekday().String()),
			slog.Int("currentHour", currentHour),
			slog.Int("morningHoursChecked", mornCount),
			slog.Float64("observedMorningSumKWH", obsMornSumKWH),
			slog.Float64("baselineP50MorningSumKWH", baseP50MornSumKWH),
			slog.Int("cappedHours", cappedHours),
		)
	}
}

// TimeProfile holds modeled average solar production and home load for an hour.
type TimeProfile struct {
	TSHourStart         time.Time
	Hour                int
	AvgSolarKWH         float64
	AvgHomeLoadKWH      float64
	ExpectedHomeLoadKWH float64
	P75HomeLoadKWH      float64
}

func findTimeProfile(model []TimeProfile, t time.Time) TimeProfile {
	truncT := t.Truncate(time.Hour)
	for _, p := range model {
		if !p.TSHourStart.IsZero() && p.TSHourStart.Equal(truncT) {
			return p
		}
	}
	return findTimeProfileByHour(model, t.Hour())
}

func findTimeProfileByHour(model []TimeProfile, hour int) TimeProfile {
	for _, p := range model {
		if p.Hour == hour {
			return p
		}
	}
	return TimeProfile{Hour: hour}
}

func (c *Controller) calculateSolarTrend(ctx context.Context, now time.Time, history []types.EnergyStats, model []TimeProfile, settings types.Settings) float64 {
	if len(history) < 2 {
		return 1.0
	}

	// Index history by time for easy lookups
	statsByTime := make(map[time.Time]types.EnergyStats)
	var latestTime time.Time
	currentHour := now.Truncate(time.Hour)
	for _, h := range history {
		t := h.TSHourStart.In(now.Location())
		if t.Equal(currentHour) {
			continue
		}
		statsByTime[t] = h
		// get the latest time today
		if t.Year() == now.Year() && t.YearDay() == now.YearDay() && t.After(latestTime) {
			latestTime = t
		}
	}

	if latestTime.IsZero() {
		log.Ctx(ctx).DebugContext(
			ctx,
			"no recent data",
			slog.Time("now", now),
			slog.Int("len(history)", len(history)),
		)
		return 1.0
	}

	// We need the last 2 hours of data
	t1 := latestTime
	t2 := t1.Add(-1 * time.Hour)

	s1, ok1 := statsByTime[t1]
	s2, ok2 := statsByTime[t2]

	if !ok1 || !ok2 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"not enough recent data",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
		)
		return 1.0
	}

	// Calculate recent actual solar
	recentSolar := s1.SolarKWH + s2.SolarKWH

	// Calculate model expected solar for these hours
	m1 := findTimeProfile(model, t1)
	m2 := findTimeProfile(model, t2)

	modelSolar := m1.AvgSolarKWH + m2.AvgSolarKWH

	// If model expects no solar (e.g. night), we can't calculate a meaningful
	// trend ratio.
	if modelSolar < 0.001 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"model expects no solar",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
			slog.Float64("modelSolar", modelSolar),
		)
		return 1.0
	}

	// check for > 10% variation
	diff := recentSolar - modelSolar
	if diff < 0 {
		diff = -diff
	}

	if diff/modelSolar > 0.10 {
		return min(settings.SolarTrendRatioMax, max(0.0, recentSolar/modelSolar))
	}

	return 1.0
}
