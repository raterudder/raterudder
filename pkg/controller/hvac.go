package controller

import (
	"math"
	"sort"
	"time"
)

const (
	// coldHeatingBaseTempC is the heating degree day (HDD) base temperature (10.0°C / 50°F)
	// used to detect electric heating / heat pump load sensitivity across overnight and morning hours.
	coldHeatingBaseTempC = 10.0

	// coldHeatingMaxTempC is the maximum hourly temperature (7.2°C / 45°F) at which overnight/morning
	// expectedColdKWH floors and elevated tempRef values unlock on electric-heating hours, and at or below
	// which BuildHourlyEnergyModel enables cold-snap extrapolation and cold setback neighbor dampening.
	coldHeatingMaxTempC = 7.2

	// Minimum sample and temperature-range gates ensuring a site has both enough history and enough
	// cold weather variability before fitting a heating regression.
	coldHeatingMinDays        = 6
	coldHeatingMinColdDays    = 4
	coldHeatingMinTempSpreadC = 4.0 // 4.0°C / ~7.2°F min-to-max spread across window/hour temps

	// hvacMinActiveDegreeSpreadC is the minimum spread (3.0°C / ~5.4°F) required in active degree-day
	// values (HDD below coldHeatingBaseTempC or CDD above hotCoolingBaseTempC) before fitting a linear
	// HVAC slope, preventing tiny dips past the base temperature from producing exaggerated slopes.
	hvacMinActiveDegreeSpreadC = 3.0

	// Window-level electric heating gates applied to the 00:00-08:59 daily median load vs. HDD(10°C).
	// Using the Winsorized daily window median resists intermittent EV charges while catching whole-night heat pumps.
	// coldHeatingMinSlopeKWHPerC is set above 400-600W gas furnace blowers (~0.02-0.03 kWh/°C) to avoid
	// false positives on Gas+EV sites.
	coldHeatingMinCorrR        = 0.45
	coldHeatingMinSlopeKWHPerC = 0.045

	// Standalone per-hour heating thresholds applied to EV-capped (Winsorized at EVMinThresholdKW) loads
	// when the broad 00:00-08:59 window median does not trigger (e.g., morning-only setback recovery heaters).
	// Tighter r/slope/active-median gates, requiring hvacStandaloneMinActiveHours qualifying hours, and
	// requiring nighttime active P30 >= hvacStandaloneNightMinActiveP30KWH prevent isolated noisy hours,
	// alternating cold-night EV sessions, or gas furnace blowers from unlocking EV suppression overnight.
	coldHeatingHourMinCorrR            = 0.48
	coldHeatingHourMinSlopeKWHPerC     = 0.10
	coldHeatingHourMinActiveMedianKWH  = 2.2
	hvacStandaloneMinActiveHours       = 2
	hvacStandaloneNightMinActiveP30KWH = 1.5

	// Window-confirmed per-hour heating thresholds used only after the 00:00-08:59 window median has already
	// confirmed whole-night electric heating, allowing moderate per-hour slopes to remain protected from EV stripping.
	coldHeatingConfirmedHourMinCorrR        = 0.35
	coldHeatingConfirmedHourMinSlopeKWHPerC = 0.04

	// Mild-weather sanity gate and asymmetric temperature matching window for cold heating:
	// when at least 3 cold (< 10°C) and 3 mild (>= 12°C) observations exist at an hour, the cold median must
	// exceed the mild median; when matching historical temperatures for computePointRefLoad, tighten the
	// warmer side to +1.5°C (vs. -3.0°C colder side) so 39-40°F non-aux mornings cannot dilute freezing aux-heat loads.
	coldHeatingMildCompareTempC     = 12.0 // 53.6°F
	coldHeatingAsymmetricUpperBandC = 1.5

	// hotCoolingBaseTempC is the cooling degree day (CDD) base temperature (22.0°C / 71.6°F)
	// and hotCoolingMinTempC (26.0°C / 78.8°F) is the active-cooling hourly threshold at which
	// expectedHotKWH floors, elevated tempRef references, and cooling setback neighbor dampening unlock.
	hotCoolingBaseTempC = 22.0
	hotCoolingMinTempC  = 26.0

	// Minimum sample and temperature-range gates ensuring enough history and warm-to-hot variability
	// before fitting an electric cooling regression.
	hotCoolingMinDays        = 6
	hotCoolingMinHotDays     = 4
	hotCoolingMinTempSpreadC = 4.0 // 4.0°C / ~7.2°F min-to-max spread across window/hour temps

	// Window-level electric cooling gates applied to the 11:00-19:00 daytime median load vs. CDD(22°C).
	hotCoolingMinCorrR        = 0.45
	hotCoolingMinSlopeKWHPerC = 0.045

	// Standalone per-hour cooling thresholds applied to EV-capped (Winsorized at EVMinThresholdKW) loads
	// for targeted AC peak hours when the full daytime window median does not trigger.
	hotCoolingHourMinCorrR           = 0.48
	hotCoolingHourMinSlopeKWHPerC    = 0.10
	hotCoolingHourMinActiveMedianKWH = 2.2

	// Window-confirmed per-hour cooling thresholds applied once the 11:00-19:00 window confirms electric AC,
	// extending protection to warm late-evening cooling hours (e.g., 23:00) outside the daytime percentile window.
	hotCoolingConfirmedHourMinCorrR        = 0.35
	hotCoolingConfirmedHourMinSlopeKWHPerC = 0.04

	// Mild-weather sanity gate and asymmetric temperature matching window for warm cooling:
	// hot-hour (> 22°C) median must exceed mild (<= 20°C / 68°F) median when both have >= 3 samples, and the
	// cooler matching bound tightens to -1.5°C (vs. +3.0°C hotter side) so mild evenings do not dilute peak AC loads.
	hotCoolingMildCompareTempC     = 20.0 // 68.0°F
	hotCoolingAsymmetricLowerBandC = 1.5

	// hvacExpectedRefMaxKWH caps the linear Winsorized expectedColdKWH / expectedHotKWH reference floor
	// in computePointRefLoad (1.5x EVMinThresholdKW = 7.2 kWh) so a steep Winsorized slope cannot extrapolate
	// unboundedly and mask stacked EV charges during extreme weather.
	hvacExpectedRefMaxKWH = EVMinThresholdKW * 1.5
)

type weatherHVACSensitivity struct {
	hasElectricHeating      bool
	heatingWindowDetected   bool
	heatingHours            [24]bool
	heatingHourSlopeKWHPerC [24]float64
	heatingHourInterceptKWH [24]float64
	hasElectricCooling      bool
	coolingWindowDetected   bool
	coolingHours            [24]bool
	coolingHourSlopeKWHPerC [24]float64
	coolingHourInterceptKWH [24]float64
}

func activeHourList(flags [24]bool) []int {
	var hours []int
	for hr, active := range flags {
		if active {
			hours = append(hours, hr)
		}
	}
	return hours
}

func linearRegression(xs, ys []float64) (float64, float64, float64) {
	n := len(xs)
	if n < 3 || len(ys) != n {
		return 0, 0, 0
	}
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)
	var num, denX, denY float64
	for i := 0; i < n; i++ {
		dx := xs[i] - meanX
		dy := ys[i] - meanY
		num += dx * dy
		denX += dx * dx
		denY += dy * dy
	}
	if denX <= 1e-9 || denY <= 1e-9 {
		return 0, 0, 0
	}
	slope := num / denX
	intercept := meanY - slope*meanX
	return num / math.Sqrt(denX*denY), slope, intercept
}

func medianSortedOrCopy(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 0 {
		return (s[mid-1] + s[mid]) / 2.0
	}
	return s[mid]
}

func floorPercentileSortedOrCopy(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	idx := int(math.Floor(float64(len(s)-1) * p))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

type hvacModeParams struct {
	windowStartHr                  int
	windowEndHr                    int
	windowMinHours                 int
	baseTempC                      float64
	isCold                         bool
	minDays                        int
	minActiveDays                  int
	minTempSpreadC                 float64
	minActiveDegreeSpreadC         float64
	windowMinCorrR                 float64
	windowMinSlopeKWHPerC          float64
	hourMinCorrR                   float64
	hourMinSlopeKWHPerC            float64
	minActiveMedianKWH             float64
	standaloneNightMinActiveP30KWH float64
	confirmedHourMinCorrR          float64
	confirmedHourMinSlopeKWHPerC   float64
	mildCompareTempC               float64
}

func detectSingleHVACMode(
	dayMap map[string]*dayPoints,
	weatherByHour map[time.Time]float64,
	loc *time.Location,
	p hvacModeParams,
) (windowDetected bool, hourActive [24]bool, hourSlopeKWHPerC [24]float64, hourInterceptKWH [24]float64) {
	if len(weatherByHour) == 0 || len(dayMap) < p.minDays {
		return false, hourActive, hourSlopeKWHPerC, hourInterceptKWH
	}

	degreeDeltaC := func(tempC float64) float64 {
		if p.isCold {
			return max(0.0, p.baseTempC-tempC)
		}
		return max(0.0, tempC-p.baseTempC)
	}
	isActiveTemp := func(tempC float64) bool {
		if p.isCold {
			return tempC < p.baseTempC
		}
		return tempC > p.baseTempC
	}
	isMildTemp := func(tempC float64) bool {
		if p.isCold {
			return tempC >= p.mildCompareTempC
		}
		return tempC <= p.mildCompareTempC
	}

	// Window-level Winsorized daily median check (00:00-08:59 for heating, 11:00-19:00 for cooling).
	// Winsorizing each hour at EVMinThresholdKW before taking the daily median ensures even 5+ hour
	// EV sessions inside the window cannot masquerade as electric heating or cooling.
	var xsWin, ysWin []float64
	activeDays := 0
	minWinTempC := math.MaxFloat64
	maxWinTempC := -math.MaxFloat64
	minWinDegDeltaC := math.MaxFloat64
	maxWinDegDeltaC := -math.MaxFloat64

	// Per-hour collection for Winsorized regression.
	var xsHr [24][]float64
	var ysHr [24][]float64
	var activeLoadsHr [24][]float64
	var mildLoadsHr [24][]float64
	var activeCountHr [24]int
	var minHrTempC [24]float64
	var maxHrTempC [24]float64
	var minHrDegDeltaC [24]float64
	var maxHrDegDeltaC [24]float64
	for hr := 0; hr < 24; hr++ {
		minHrTempC[hr] = math.MaxFloat64
		maxHrTempC[hr] = -math.MaxFloat64
		minHrDegDeltaC[hr] = math.MaxFloat64
		maxHrDegDeltaC[hr] = -math.MaxFloat64
	}

	for _, d := range dayMap {
		if d == nil {
			continue
		}
		var winLoads []float64
		var winTempCSum float64
		for _, pt := range d.points {
			if pt.HomeKWH <= 0 {
				continue
			}
			hr := pt.TSHourStart.Hour()
			if loc != nil {
				hr = pt.TSHourStart.In(loc).Hour()
			}
			if hr < 0 || hr >= 24 {
				continue
			}
			tempC, ok := weatherByHour[pt.TSHourStart.Truncate(time.Hour).UTC()]
			if !ok {
				continue
			}
			cappedLoadKWH := min(pt.HomeKWH, EVMinThresholdKW)
			if hr >= p.windowStartHr && hr <= p.windowEndHr {
				winLoads = append(winLoads, cappedLoadKWH)
				winTempCSum += tempC
			}
			degDeltaC := degreeDeltaC(tempC)
			xsHr[hr] = append(xsHr[hr], degDeltaC)
			ysHr[hr] = append(ysHr[hr], cappedLoadKWH)
			if tempC < minHrTempC[hr] {
				minHrTempC[hr] = tempC
			}
			if tempC > maxHrTempC[hr] {
				maxHrTempC[hr] = tempC
			}
			if degDeltaC < minHrDegDeltaC[hr] {
				minHrDegDeltaC[hr] = degDeltaC
			}
			if degDeltaC > maxHrDegDeltaC[hr] {
				maxHrDegDeltaC[hr] = degDeltaC
			}
			if isActiveTemp(tempC) {
				activeCountHr[hr]++
				activeLoadsHr[hr] = append(activeLoadsHr[hr], cappedLoadKWH)
			} else if isMildTemp(tempC) {
				mildLoadsHr[hr] = append(mildLoadsHr[hr], cappedLoadKWH)
			}
		}

		if len(winLoads) >= p.windowMinHours {
			avgTempC := winTempCSum / float64(len(winLoads))
			if avgTempC < minWinTempC {
				minWinTempC = avgTempC
			}
			if avgTempC > maxWinTempC {
				maxWinTempC = avgTempC
			}
			winDegDeltaC := degreeDeltaC(avgTempC)
			if winDegDeltaC < minWinDegDeltaC {
				minWinDegDeltaC = winDegDeltaC
			}
			if winDegDeltaC > maxWinDegDeltaC {
				maxWinDegDeltaC = winDegDeltaC
			}
			if isActiveTemp(avgTempC) {
				activeDays++
			}
			xsWin = append(xsWin, winDegDeltaC)
			ysWin = append(ysWin, medianSortedOrCopy(winLoads))
		}
	}

	if len(xsWin) >= p.minDays &&
		activeDays >= p.minActiveDays &&
		maxWinTempC-minWinTempC >= p.minTempSpreadC &&
		maxWinDegDeltaC-minWinDegDeltaC >= p.minActiveDegreeSpreadC {
		r, slopeKWHPerC, _ := linearRegression(xsWin, ysWin)
		if r >= p.windowMinCorrR && slopeKWHPerC >= p.windowMinSlopeKWHPerC {
			windowDetected = true
		}
	}

	activeHoursCount := 0
	for hr := 0; hr < 24; hr++ {
		if len(xsHr[hr]) < p.minDays || activeCountHr[hr] < p.minActiveDays {
			continue
		}
		if maxHrTempC[hr]-minHrTempC[hr] < p.minTempSpreadC || maxHrDegDeltaC[hr]-minHrDegDeltaC[hr] < p.minActiveDegreeSpreadC {
			continue
		}
		activeMedKWH := medianSortedOrCopy(activeLoadsHr[hr])
		if len(mildLoadsHr[hr]) >= 3 && len(activeLoadsHr[hr]) >= 3 {
			if activeMedKWH <= medianSortedOrCopy(mildLoadsHr[hr]) {
				continue
			}
		}
		r, slopeKWHPerC, interceptKWH := linearRegression(xsHr[hr], ysHr[hr])
		standaloneHourOK := !p.isCold || hr <= 9
		standaloneP30OK := (!p.isCold && !isNighttimeEVHour(hr)) || floorPercentileSortedOrCopy(activeLoadsHr[hr], hourlyOutlierBaselinePercentile) >= p.standaloneNightMinActiveP30KWH
		standaloneMatch := standaloneHourOK && standaloneP30OK && r >= p.hourMinCorrR && slopeKWHPerC >= p.hourMinSlopeKWHPerC && activeMedKWH >= p.minActiveMedianKWH
		windowConfirmedMatch := windowDetected && r >= p.confirmedHourMinCorrR && slopeKWHPerC >= p.confirmedHourMinSlopeKWHPerC
		if standaloneMatch || windowConfirmedMatch {
			hourActive[hr] = true
			hourSlopeKWHPerC[hr] = slopeKWHPerC
			hourInterceptKWH[hr] = interceptKWH
			activeHoursCount++
		}
	}

	if !windowDetected && activeHoursCount < hvacStandaloneMinActiveHours {
		hourActive = [24]bool{}
		hourSlopeKWHPerC = [24]float64{}
		hourInterceptKWH = [24]float64{}
	}

	return windowDetected, hourActive, hourSlopeKWHPerC, hourInterceptKWH
}

// detectWeatherHVACSensitivity identifies whether a site exhibits statistically significant
// cold-weather electric heating and/or warm-weather electric cooling, and pinpoints which hours
// of the day are temperature-sensitive.
//
// Heating combines:
//   - Step A: Overnight/morning (00:00-08:59) Winsorized daily median load vs HDD(10°C), catching whole-night heat pumps.
//   - Step B: Per-hour EV-capped (Winsorized) load vs HDD(10°C), catching morning-only setback heaters
//     (e.g. 05:00-08:59) without unlocking EV-prone sleeping hours or false-positiving on gas blowers.
//
// Cooling mirrors this with daytime (11:00-19:00) Winsorized daily median load and per-hour EV-capped load vs CDD(22°C).
// When loc is non-nil, hourly indices are resolved in loc rather than each timestamp's own location.
func detectWeatherHVACSensitivity(dayMap map[string]*dayPoints, weatherByHour map[time.Time]float64, loc *time.Location) weatherHVACSensitivity {
	var res weatherHVACSensitivity

	winHeat, hrsHeat, slpHeat, intHeat := detectSingleHVACMode(dayMap, weatherByHour, loc, hvacModeParams{
		windowStartHr:                  0,
		windowEndHr:                    8,
		windowMinHours:                 6,
		baseTempC:                      coldHeatingBaseTempC,
		isCold:                         true,
		minDays:                        coldHeatingMinDays,
		minActiveDays:                  coldHeatingMinColdDays,
		minTempSpreadC:                 coldHeatingMinTempSpreadC,
		minActiveDegreeSpreadC:         hvacMinActiveDegreeSpreadC,
		windowMinCorrR:                 coldHeatingMinCorrR,
		windowMinSlopeKWHPerC:          coldHeatingMinSlopeKWHPerC,
		hourMinCorrR:                   coldHeatingHourMinCorrR,
		hourMinSlopeKWHPerC:            coldHeatingHourMinSlopeKWHPerC,
		minActiveMedianKWH:             coldHeatingHourMinActiveMedianKWH,
		standaloneNightMinActiveP30KWH: hvacStandaloneNightMinActiveP30KWH,
		confirmedHourMinCorrR:          coldHeatingConfirmedHourMinCorrR,
		confirmedHourMinSlopeKWHPerC:   coldHeatingConfirmedHourMinSlopeKWHPerC,
		mildCompareTempC:               coldHeatingMildCompareTempC,
	})
	res.heatingWindowDetected = winHeat
	res.heatingHours = hrsHeat
	res.heatingHourSlopeKWHPerC = slpHeat
	res.heatingHourInterceptKWH = intHeat
	res.hasElectricHeating = winHeat
	for _, active := range hrsHeat {
		if active {
			res.hasElectricHeating = true
			break
		}
	}

	winCool, hrsCool, slpCool, intCool := detectSingleHVACMode(dayMap, weatherByHour, loc, hvacModeParams{
		windowStartHr:                  11,
		windowEndHr:                    19,
		windowMinHours:                 5,
		baseTempC:                      hotCoolingBaseTempC,
		isCold:                         false,
		minDays:                        hotCoolingMinDays,
		minActiveDays:                  hotCoolingMinHotDays,
		minTempSpreadC:                 hotCoolingMinTempSpreadC,
		minActiveDegreeSpreadC:         hvacMinActiveDegreeSpreadC,
		windowMinCorrR:                 hotCoolingMinCorrR,
		windowMinSlopeKWHPerC:          hotCoolingMinSlopeKWHPerC,
		hourMinCorrR:                   hotCoolingHourMinCorrR,
		hourMinSlopeKWHPerC:            hotCoolingHourMinSlopeKWHPerC,
		minActiveMedianKWH:             hotCoolingHourMinActiveMedianKWH,
		standaloneNightMinActiveP30KWH: hvacStandaloneNightMinActiveP30KWH,
		confirmedHourMinCorrR:          hotCoolingConfirmedHourMinCorrR,
		confirmedHourMinSlopeKWHPerC:   hotCoolingConfirmedHourMinSlopeKWHPerC,
		mildCompareTempC:               hotCoolingMildCompareTempC,
	})
	res.coolingWindowDetected = winCool
	res.coolingHours = hrsCool
	res.coolingHourSlopeKWHPerC = slpCool
	res.coolingHourInterceptKWH = intCool
	res.hasElectricCooling = winCool
	for _, active := range hrsCool {
		if active {
			res.hasElectricCooling = true
			break
		}
	}

	return res
}

// Cold-snap load extrapolation parameters applied on confirmed electric-heating hours when today's
// forecast drops below minHistTempC or effectivePoolTempC by >= extremeColdSnapThresholdC.
const (
	// extremeColdSnapThresholdC is the temperature drop (2.0°C) below either the historical
	// minimum temperature or the weighted-median pool temperature required to trigger extrapolation.
	extremeColdSnapThresholdC = 2.0

	// extremeColdSnapMaxMultiplier caps cold-snap load extrapolation relative to the unextrapolated
	// hourly prediction (e.g. 1.60 caps the boost at +60%).
	extremeColdSnapMaxMultiplier = 1.60

	// extremeColdSnapMaxAddKWH caps the absolute hourly kWh increase added by cold-snap extrapolation.
	extremeColdSnapMaxAddKWH = 6.0

	// extremeColdSnapFallbackMultiplier is the conservative +20% boost multiplier applied when an
	// electric-heating site hits a new cold low (> 2.0°C below minHistTempC) at an overnight/morning
	// hour before a reliable linear slope is available (fewer than extremeColdSnapMinColdPoints samples
	// or low correlation r < coldHeatingMinCorrR).
	extremeColdSnapFallbackMultiplier = 1.20

	// extremeColdSnapMinColdPoints is the minimum number of cold (< coldHeatingBaseTempC) historical
	// observations at a specific hour required to fit an hourly linear cold-extrapolation slope.
	extremeColdSnapMinColdPoints = 6

	// Symmetric cooling pool-gap extrapolation parameters applied on confirmed electric-cooling hours
	// when today's forecast exceeds maxHistTempC or effectivePoolTempC by >= extremeHeatwaveThresholdC:
	// - extremeHeatwaveMaxMultiplier / extremeHeatwaveMaxAddKWH: cap relative (+50%) and absolute (+5.0 kWh) boosts
	// - extremeHeatwaveMinHotPoints: minimum hot (> hotCoolingBaseTempC) samples at that hour to fit a CDD slope
	extremeHeatwaveMaxMultiplier = 1.50
	extremeHeatwaveMaxAddKWH     = 5.0
	extremeHeatwaveMinHotPoints  = 6

	// Sharp HVAC setback/recovery neighbor-blend dampening parameters:
	// when a confirmed heating or cooling hour's seasonal median load is at least sharpHVACPeakRatioThreshold
	// times an adjacent hour's seasonal median (e.g. a 05:00 heating recovery ramp next to a quiet 04:00),
	// reduce that neighbor's blend weight from neighborHourWeightMultiplier (0.50) to sharpHVACNeighborMultiplier (0.15).
	sharpHVACPeakRatioThreshold = 1.4
	sharpHVACNeighborMultiplier = 0.15
)

type hourTempLoad struct {
	tempC   float64
	loadKWH float64
}

type seasonalHourMedians struct {
	coldMedianKWH [24]float64
	coldCount     [24]int
	hotMedianKWH  [24]float64
	hotCount      [24]int
}

func computeSeasonalHourMedians(byHour [24][]hourTempLoad) seasonalHourMedians {
	var res seasonalHourMedians
	for hr := 0; hr < 24; hr++ {
		var coldVals, hotVals []float64
		for _, tl := range byHour[hr] {
			if tl.tempC < coldHeatingBaseTempC {
				coldVals = append(coldVals, tl.loadKWH)
			}
			if tl.tempC > hotCoolingBaseTempC {
				hotVals = append(hotVals, tl.loadKWH)
			}
		}
		res.coldCount[hr] = len(coldVals)
		if len(coldVals) >= 3 {
			res.coldMedianKWH[hr] = medianSortedOrCopy(coldVals)
		}
		res.hotCount[hr] = len(hotVals)
		if len(hotVals) >= 3 {
			res.hotMedianKWH[hr] = medianSortedOrCopy(hotVals)
		}
	}
	return res
}

// computeNeighborMultiplier returns the relative weight multiplier applied to a candidate historical
// point from adjacent hour hpLocalHour when estimating target hour h:
//   - Returns 0.0 for sleeping-hour standby points bleeding into waking morning hours (06:00-11:59).
//   - Returns sharpHVACNeighborMultiplier (0.15) symmetrically when either hour h or neighbor hpLocalHour
//     is a confirmed active heating/cooling hour with a sharp seasonal step (>= sharpHVACPeakRatioThreshold
//     relative to the other hour), preventing quiet setback hours from dragging down steep recovery ramps
//     and preventing recovery ramps from inflating adjacent quiet hours.
//   - Otherwise returns neighborHourWeightMultiplier (0.50).
func computeNeighborMultiplier(
	h int,
	hpLocalHour int,
	loadKWH float64,
	standbyLoad float64,
	targetTempC float64,
	hasTargetTemp bool,
	hvac weatherHVACSensitivity,
	medians seasonalHourMedians,
) float64 {
	if h >= 6 && h <= 11 && hpLocalHour < 9 && loadKWH <= standbyLoad*neighborHourStandbyFloorMultiplier {
		return 0.0
	}
	if hasTargetTemp && h >= 0 && h < 24 && hpLocalHour >= 0 && hpLocalHour < 24 {
		if targetTempC <= coldHeatingMaxTempC && medians.coldCount[h] >= 3 && medians.coldCount[hpLocalHour] >= 3 {
			if hvac.heatingHours[h] && medians.coldMedianKWH[h] >= sharpHVACPeakRatioThreshold*medians.coldMedianKWH[hpLocalHour] {
				return sharpHVACNeighborMultiplier
			}
			if hvac.heatingHours[hpLocalHour] && medians.coldMedianKWH[hpLocalHour] >= sharpHVACPeakRatioThreshold*medians.coldMedianKWH[h] {
				return sharpHVACNeighborMultiplier
			}
		}
		if targetTempC >= hotCoolingMinTempC && medians.hotCount[h] >= 3 && medians.hotCount[hpLocalHour] >= 3 {
			if hvac.coolingHours[h] && medians.hotMedianKWH[h] >= sharpHVACPeakRatioThreshold*medians.hotMedianKWH[hpLocalHour] {
				return sharpHVACNeighborMultiplier
			}
			if hvac.coolingHours[hpLocalHour] && medians.hotMedianKWH[hpLocalHour] >= sharpHVACPeakRatioThreshold*medians.hotMedianKWH[h] {
				return sharpHVACNeighborMultiplier
			}
		}
	}
	return neighborHourWeightMultiplier
}

type extremeWeatherGapParams struct {
	isCold        bool
	baseTempC     float64
	minPoints     int
	minCorrR      float64
	maxMultiplier float64
	maxAddKWH     float64
}

func computeSlopeGapExtraKWH(
	tempLoads []hourTempLoad,
	effectiveGapC float64,
	baseLoadKWH float64,
	p extremeWeatherGapParams,
) float64 {
	if baseLoadKWH <= 0 || effectiveGapC <= 0 {
		return 0
	}
	activeCount := 0
	xs := make([]float64, 0, len(tempLoads))
	ys := make([]float64, 0, len(tempLoads))
	for _, tl := range tempLoads {
		if p.isCold {
			if tl.tempC < p.baseTempC {
				activeCount++
			}
			xs = append(xs, max(0.0, p.baseTempC-tl.tempC))
		} else {
			if tl.tempC > p.baseTempC {
				activeCount++
			}
			xs = append(xs, max(0.0, tl.tempC-p.baseTempC))
		}
		ys = append(ys, tl.loadKWH)
	}
	if activeCount < p.minPoints {
		return 0
	}
	r, slopeKWHPerC, _ := linearRegression(xs, ys)
	if r < p.minCorrR || slopeKWHPerC <= 0 {
		return 0
	}
	deltaKWH := slopeKWHPerC * effectiveGapC
	maxExtraKWH := min(baseLoadKWH*(p.maxMultiplier-1.0), p.maxAddKWH)
	return min(deltaKWH, maxExtraKWH)
}

func computeExtremeWeatherExtraKWH(
	hvac weatherHVACSensitivity,
	allValidExactHourTempLoads []hourTempLoad,
	hr int,
	targetTempC float64,
	hasTargetTemp bool,
	hasHistTempForHour bool,
	minHistTempC float64,
	maxHistTempC float64,
	effectivePoolTempC float64,
	hasEffectivePoolTemp bool,
	baseLoadKWH float64,
) (float64, string) {
	if !hasTargetTemp || !hasHistTempForHour || baseLoadKWH <= 0 {
		return 0, ""
	}

	var extraKWH float64
	reason := ""

	// Hot heatwave safeguard + symmetric cooling pool-gap extrapolation.
	if targetTempC > extremeHeatwaveMinTempC {
		if targetTempC > maxHistTempC+extremeHeatwaveThresholdC {
			extraKWH = baseLoadKWH * (extremeHeatwaveLoadMultiplier - 1.0)
			reason = "heatwaveSafeguard"
		}
		if hvac.coolingHours[hr] {
			hotGapC := targetTempC - maxHistTempC
			if hasEffectivePoolTemp {
				hotGapC = max(hotGapC, targetTempC-effectivePoolTempC)
			}
			if hotGapC >= extremeHeatwaveThresholdC {
				coolingExtra := computeSlopeGapExtraKWH(allValidExactHourTempLoads, hotGapC, baseLoadKWH, extremeWeatherGapParams{
					isCold:        false,
					baseTempC:     hotCoolingBaseTempC,
					minPoints:     extremeHeatwaveMinHotPoints,
					minCorrR:      hotCoolingMinCorrR,
					maxMultiplier: extremeHeatwaveMaxMultiplier,
					maxAddKWH:     extremeHeatwaveMaxAddKWH,
				})
				if coolingExtra > extraKWH {
					extraKWH = coolingExtra
					reason = "coolingPoolGap"
				}
			}
		}
	}

	// Cold heating safeguard + pool-gap extrapolation on confirmed electric-heating hours.
	if hvac.heatingHours[hr] && targetTempC <= coldHeatingMaxTempC {
		minGapC := minHistTempC - targetTempC
		if (hr <= 10 || hr >= 20) && minGapC > extremeColdSnapThresholdC {
			fallbackExtra := min(baseLoadKWH*(extremeColdSnapFallbackMultiplier-1.0), extremeColdSnapMaxAddKWH)
			if fallbackExtra > extraKWH {
				extraKWH = fallbackExtra
				reason = "coldSnapFallback"
			}
		}
		coldGapC := minGapC
		if hasEffectivePoolTemp {
			coldGapC = max(coldGapC, effectivePoolTempC-targetTempC)
		}
		if coldGapC >= extremeColdSnapThresholdC {
			coldExtra := computeSlopeGapExtraKWH(allValidExactHourTempLoads, coldGapC, baseLoadKWH, extremeWeatherGapParams{
				isCold:        true,
				baseTempC:     coldHeatingBaseTempC,
				minPoints:     extremeColdSnapMinColdPoints,
				minCorrR:      coldHeatingMinCorrR,
				maxMultiplier: extremeColdSnapMaxMultiplier,
				maxAddKWH:     extremeColdSnapMaxAddKWH,
			})
			if coldExtra > extraKWH {
				extraKWH = coldExtra
				reason = "heatingPoolGap"
			}
		}
	}

	return extraKWH, reason
}
