package controller

import (
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectWeatherHVACSensitivity(t *testing.T) {
	buildDayMap := func(history []types.EnergyStats, loc *time.Location) map[string]*dayPoints {
		dayMap := make(map[string]*dayPoints)
		for _, h := range history {
			if h.HomeKWH <= 0 {
				continue
			}
			ts := h.TSHourStart
			if loc != nil {
				ts = ts.In(loc)
			}
			dateStr := ts.Format("2006-01-02")
			d, ok := dayMap[dateStr]
			if !ok {
				dayLoc := time.UTC
				if loc != nil {
					dayLoc = loc
				}
				dTime, _ := time.ParseInLocation("2006-01-02", dateStr, dayLoc)
				d = &dayPoints{
					date:    dateStr,
					dayTime: dTime,
					weekday: dTime.Weekday(),
				}
				dayMap[dateStr] = d
			}
			d.points = append(d.points, h)
			d.loads = append(d.loads, h.HomeKWH)
		}
		return dayMap
	}

	t.Run("NarrowActiveDegreeDaySpreadRejected", func(t *testing.T) {
		// 10 mild days at 16.0C (HDD=0, 1.0 kWh) + 4 days dipping just below 10C to 9.5C (HDD=0.5, 1.2 kWh).
		// Raw temp spread is 6.5C (>= 4.0C), but active HDD spread is only 0.5C (< 3.0C), so it must not
		// falsely classify a home as electric heating with an exaggerated 0.40 kWh/C slope.
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		for d := 0; d < 14; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0
			load := 1.0
			if d >= 10 {
				tempC = 9.5
				load = 1.2
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.UTC()] = tempC
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}

		hvac := detectWeatherHVACSensitivity(buildDayMap(history, nil), weatherByHour, nil, nil, EVMinThresholdKW)
		assert.False(t, hvac.hasElectricHeating, "0.5C active HDD spread must not trigger electric heating")
		assert.False(t, hvac.heatingWindowDetected)
		assert.Empty(t, activeHourList(hvac.heatingHours))
	})

	t.Run("FiveHourEVInWindowDoesNotTriggerHeating", func(t *testing.T) {
		// 16 days ramping 16C -> 1C with 5-hour 9.6 kW overnight EV sessions (00:00-04:59) on 1 mild day
		// and 3 cold days. Winsorizing winLoads and gating standalone nighttime P30 must prevent EV from
		// triggering electric heating.
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		evDays := map[int]bool{3: true, 11: true, 13: true, 15: true}
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.UTC()] = tempC
				load := 0.8
				if evDays[d] && h <= 4 {
					load = 9.6
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}

		hvac := detectWeatherHVACSensitivity(buildDayMap(history, nil), weatherByHour, nil, nil, EVMinThresholdKW)
		assert.False(t, hvac.heatingWindowDetected, "Winsorized window median must resist 5-hour EV sessions")
		assert.False(t, hvac.hasElectricHeating, "5-hour EV sessions on cold nights must not trigger electric heating")
	})

	t.Run("AlternatingColdNightEVDoesNotTriggerStandaloneHeating", func(t *testing.T) {
		// Gas+EV home (0.8 + 0.025*HDD) with 2-hour 8.5 kW EV sessions (01:00-02:00) on alternating cold nights.
		// Nighttime standalone P30 remains near 0.8 kWh (< 1.5 kWh), preventing false standalone heating detection.
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		evDays := map[int]bool{7: true, 9: true, 11: true, 13: true, 15: true}
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.UTC()] = tempC
				load := 0.8 + 0.025*max(0.0, 10.0-tempC)
				if evDays[d] && (h == 1 || h == 2) {
					load = 8.5
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}

		hvac := detectWeatherHVACSensitivity(buildDayMap(history, nil), weatherByHour, nil, nil, EVMinThresholdKW)
		assert.False(t, hvac.hasElectricHeating, "alternating cold-night EV on a Gas+EV home must not trigger heating")
		assert.False(t, hvac.heatingHours[1])
		assert.False(t, hvac.heatingHours[2])
	})

	t.Run("NonUTCLocationUsesLocalizedHours", func(t *testing.T) {
		// Timestamps stored in UTC while site operates in America/New_York (EST = UTC-5 in mid-November).
		// Morning heating occurs at 05:00-06:00 EST (10:00-11:00 UTC); passing loc must index hours 5 and 6.
		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		startLocal := time.Date(2026, 11, 4, 0, 0, 0, 0, nyLoc)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		for d := 0; d < 16; d++ {
			dayLocal := startLocal.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				localTS := time.Date(dayLocal.Year(), dayLocal.Month(), dayLocal.Day(), h, 0, 0, 0, nyLoc)
				utcTS := localTS.UTC()
				weatherByHour[utcTS] = tempC
				load := 1.0
				if h == 5 || h == 6 {
					load = 1.0 + 0.55*max(0.0, 10.0-tempC)
				}
				history = append(history, types.EnergyStats{TSHourStart: utcTS, HomeKWH: load})
			}
		}

		hvac := detectWeatherHVACSensitivity(buildDayMap(history, nyLoc), weatherByHour, nyLoc, nil, EVMinThresholdKW)
		assert.True(t, hvac.hasElectricHeating)
		assert.True(t, hvac.heatingHours[5], "local 05:00 EST heating hour must be detected")
		assert.True(t, hvac.heatingHours[6], "local 06:00 EST heating hour must be detected")
		assert.False(t, hvac.heatingHours[10], "UTC hour 10 must not be misindexed when loc=America/New_York")
		assert.False(t, hvac.heatingHours[11], "UTC hour 11 must not be misindexed when loc=America/New_York")
	})

	t.Run("DaytimeEVSpikesDoNotTriggerStandaloneHeating", func(t *testing.T) {
		// Gas+EV home with intermittent 8.5 kW EV sessions on alternating cold days during both
		// morning (08:00-09:00) and afternoon (15:00-16:00). Standalone cold heating must be
		// restricted to hr <= 9 AND require active P30 >= 1.5 kWh so intermittent EV spikes
		// cannot falsely set hasElectricHeating or heatingHours.
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		evDays := map[int]bool{7: true, 9: true, 11: true, 13: true, 15: true}
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.UTC()] = tempC
				load := 0.8 + 0.025*max(0.0, 10.0-tempC)
				if evDays[d] && (h == 8 || h == 9 || h == 15 || h == 16) {
					load = 8.5
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}

		hvac := detectWeatherHVACSensitivity(buildDayMap(history, nil), weatherByHour, nil, nil, EVMinThresholdKW)
		assert.False(t, hvac.hasElectricHeating, "intermittent daytime EV spikes on cold days must not trigger heating")
		assert.False(t, hvac.heatingHours[8])
		assert.False(t, hvac.heatingHours[9])
		assert.False(t, hvac.heatingHours[15])
		assert.False(t, hvac.heatingHours[16])
	})

	t.Run("OptionsSkipIgnoredOutliersAdaptCoolingCapAndProtectGasBlower", func(t *testing.T) {
		// Part 1: High-load AC site (siteEVMinThresholdKW=6.8 kW) with daytime cooling up to 6.5 kW
		// plus intermittent 10 kW EV spikes flagged in ignoredOutlierHours.
		startSummer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var summerHist []types.EnergyStats
		summerWeather := make(map[time.Time]float64)
		ignoredSummer := make(map[dayHourKey]bool)
		for d := 0; d < 14; d++ {
			dayTime := startSummer.AddDate(0, 0, d)
			dateStr := dayTime.Format("2006-01-02")
			tempC := 19.0 + float64(d)*1.2 // 19.0C .. 34.6C
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				summerWeather[ts.UTC()] = tempC
				load := 1.0
				if h >= 11 && h <= 19 {
					load = 1.0 + 0.45*max(0.0, tempC-hotCoolingBaseTempC) // up to ~6.67 kW
				}
				if d < 4 && h == 15 {
					load = 10.5
					ignoredSummer[dayHourKey{date: dateStr, hour: h}] = true
				}
				summerHist = append(summerHist, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}
		hvacAdaptive := detectWeatherHVACSensitivity(buildDayMap(summerHist, nil), summerWeather, nil, ignoredSummer, 6.8)
		assert.True(t, hvacAdaptive.hasElectricCooling)
		assert.True(t, hvacAdaptive.coolingWindowDetected)
		assert.True(t, hvacAdaptive.coolingHours[15])
		hvacDefaultCap := detectWeatherHVACSensitivity(buildDayMap(summerHist, nil), summerWeather, nil, ignoredSummer, EVMinThresholdKW)
		assert.Greater(t, hvacAdaptive.coolingHourSlopeKWHPerC[15], hvacDefaultCap.coolingHourSlopeKWHPerC[15],
			"site-adaptive cooling cap (6.8 kW) should avoid clipping 6.5 kW AC slope at 4.8 kW")

		// Part 2: Gas-heated home (0.5 + 0.055*HDD, cold window median < 1.2 kWh) with flagged EV hours
		// must not falsely trigger window-level electric heating when EV hours are skipped.
		startWinter := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var winterHist []types.EnergyStats
		winterWeather := make(map[time.Time]float64)
		ignoredWinter := make(map[dayHourKey]bool)
		for d := 0; d < 14; d++ {
			dayTime := startWinter.AddDate(0, 0, d)
			dateStr := dayTime.Format("2006-01-02")
			tempC := 15.0 - float64(d) // 15C .. 2C
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				winterWeather[ts.UTC()] = tempC
				load := 0.5 + 0.055*max(0.0, coldHeatingBaseTempC-tempC) // max ~0.94 kW (< 1.2 kW)
				if d%3 == 0 && (h == 1 || h == 2) {
					load = 9.0
					ignoredWinter[dayHourKey{date: dateStr, hour: h}] = true
				}
				winterHist = append(winterHist, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}
		hvacGas := detectWeatherHVACSensitivity(buildDayMap(winterHist, nil), winterWeather, nil, ignoredWinter, 6.8)
		assert.False(t, hvacGas.heatingWindowDetected, "low-load gas furnace blower must not trigger heating window when EV hours are skipped")
		assert.False(t, hvacGas.hasElectricHeating)

		// Part 3: Window-level mild-weather sanity gate — if mild-day window median >= active-day
		// window median (e.g. cold days have rising slope inside <10C but stay below mild-day baseline),
		// heatingWindowDetected must stay false even when active median >= 1.2 kWh.
		var mildGateHist []types.EnergyStats
		mildGateWeather := make(map[time.Time]float64)
		for d := 0; d < 10; d++ {
			dayTime := startWinter.AddDate(0, 0, d)
			var tempC, load float64
			if d < 4 {
				// Mild days (>= 12C) with higher non-heating load (2.4 kWh)
				tempC = 14.0 + float64(d)
				load = 2.4
			} else {
				// Cold days (8C down to -2C) with positive HDD slope but lower median (~1.6 kWh <= 2.4 kWh)
				tempC = 8.0 - float64(d-4)*2.0
				load = 1.2 + 0.08*max(0.0, coldHeatingBaseTempC-tempC)
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				mildGateWeather[ts.UTC()] = tempC
				mildGateHist = append(mildGateHist, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
			}
		}
		hvacMildGate := detectWeatherHVACSensitivity(buildDayMap(mildGateHist, nil), mildGateWeather, nil, nil, math.NaN())
		assert.False(t, hvacMildGate.heatingWindowDetected, "window mild-weather sanity check must reject cold median <= mild median")
	})
}

func TestComputeExtremeWeatherExtraKWH(t *testing.T) {
	t.Run("ColdSnapFallback20PersistsWhenSlopeCorrelationLow", func(t *testing.T) {
		// Even with 6 cold points (>= extremeColdSnapMinColdPoints) whose hourly linear slope has
		// r < 0.45, a record cold snap (> 2C below minHistTempC) at an overnight/morning hour
		// still retains the +20% fallback safeguard rather than dropping to 0%.
		var hvac weatherHVACSensitivity
		hvac.hasElectricHeating = true
		hvac.heatingHours[6] = true

		noisyColdLoads := []hourTempLoad{
			{tempC: 9.0, loadKWH: 4.0},
			{tempC: 8.0, loadKWH: 2.0},
			{tempC: 7.0, loadKWH: 3.5},
			{tempC: 6.0, loadKWH: 2.1},
			{tempC: 5.0, loadKWH: 3.6},
			{tempC: 4.0, loadKWH: 2.2},
		}
		extraKWH, reason := computeExtremeWeatherExtraKWH(
			hvac,
			noisyColdLoads,
			6,
			-1.0,
			true,
			true,
			4.0,
			15.0,
			4.0,
			true,
			3.0,
		)
		assert.Equal(t, "coldSnapFallback", reason)
		assert.InDelta(t, 0.6, extraKWH, 0.01, "record cold snap with 6 noisy cold points must retain +20% fallback")
	})
}

func TestComputeNeighborMultiplier(t *testing.T) {
	t.Run("MorningStandbyZeroTakesPrecedenceOverHVACDampening", func(t *testing.T) {
		var hvac weatherHVACSensitivity
		hvac.heatingHours[6] = true
		var medians seasonalHourMedians
		medians.coldCount[5], medians.coldCount[6] = 5, 5
		medians.coldMedianKWH[5], medians.coldMedianKWH[6] = 1.0, 5.0

		mult := computeNeighborMultiplier(6, 5, 0.5, 0.5, 1.0, true, hvac, medians)
		assert.Equal(t, 0.0, mult, "sleeping standby floor check must return 0.0 before sharp HVAC dampening")
	})

	t.Run("CoolingSetbackSymmetricAndBoundaryHours", func(t *testing.T) {
		var hvac weatherHVACSensitivity
		hvac.coolingHours[15] = true
		hvac.heatingHours[0] = true
		var medians seasonalHourMedians
		medians.hotCount[14], medians.hotCount[15] = 5, 5
		medians.hotMedianKWH[14], medians.hotMedianKWH[15] = 1.2, 4.8
		medians.coldCount[23], medians.coldCount[0] = 5, 5
		medians.coldMedianKWH[23], medians.coldMedianKWH[0] = 1.0, 4.0

		// Forward cooling setback (predicting peak 15:00 from quiet 14:00)
		assert.InDelta(t, sharpHVACNeighborMultiplier, computeNeighborMultiplier(15, 14, 1.2, 0.5, 31.0, true, hvac, medians), 0.001)
		// Reverse cooling setback (predicting quiet 14:00 from peak 15:00)
		assert.InDelta(t, sharpHVACNeighborMultiplier, computeNeighborMultiplier(14, 15, 4.8, 0.5, 31.0, true, hvac, medians), 0.001)
		// Midnight boundary (00:00 peak vs 23:00 quiet)
		assert.InDelta(t, sharpHVACNeighborMultiplier, computeNeighborMultiplier(0, 23, 1.0, 0.5, 1.0, true, hvac, medians), 0.001)
		// Mild temperature keeps standard 0.50 neighbor weight
		assert.InDelta(t, neighborHourWeightMultiplier, computeNeighborMultiplier(15, 14, 1.2, 0.5, 20.0, true, hvac, medians), 0.001)
	})
}
