package controller

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/energy_history/*.json
var energyHistoryFS embed.FS

// energyHistoryBaseline tracks MAE alongside total under-prediction and over-prediction kWh.
// Tracking all three prevents changes that lower or maintain MAE on right-skewed loads by shifting
// predictions uniformly down (which spikes under-prediction during peak hours) or up (which spikes
// unnecessary early-morning grid charging).
type energyHistoryBaseline struct {
	maeKWH          float64
	underPredictKWH float64
	overPredictKWH  float64
}

var energyHistoryBaselines = map[string]energyHistoryBaseline{
	// site3 (America/Chicago): 13 vacation days + post-vacation recovery across varying temps (13.5..30.8°C)
	"site3_late-summer.json": {maeKWH: 0.965, underPredictKWH: 1518.0, overPredictKWH: 903.0},
	// site5 (America/New_York): configured EVChargingPeriods (00:00-07:00), 4 vacation days, and 1.67x heavy weekends
	"site5_late-summer.json": {maeKWH: 0.880, underPredictKWH: 1756.5, overPredictKWH: 451.7},
	// site6 (America/Los_Angeles): steady coastal temperatures (13.8..26.9°C) and tight daily load distribution
	"site6_late-summer.json": {maeKWH: 0.362, underPredictKWH: 512.8, overPredictKWH: 395.9},
	// site11 (America/Chicago): wide late-summer temperature swings / heatwaves (13.7..31.0°C) driving large AC loads
	"site11_late-summer.json": {maeKWH: 0.971, underPredictKWH: 1132.1, overPredictKWH: 1303.8},
	// site12 (America/Los_Angeles): strong day-of-week / weekend load surges (1.71x weekend-to-weekday ratio) + unconfigured EV spikes
	"site12_late-summer.json": {maeKWH: 0.688, underPredictKWH: 1548.3, overPredictKWH: 176.0},
}

type energyEvalMetrics struct {
	hoursCount      int
	daysCount       int
	totalAbsErrKWH  float64
	totalSqErrKWH2  float64
	underPredictKWH float64
	overPredictKWH  float64
	actualKWH       float64
	predKWH         float64
}

func (m *energyEvalMetrics) add(actualKWH, predKWH float64) {
	m.hoursCount++
	diffKWH := predKWH - actualKWH
	m.totalAbsErrKWH += math.Abs(diffKWH)
	m.totalSqErrKWH2 += diffKWH * diffKWH
	if diffKWH < 0 {
		m.underPredictKWH += -diffKWH
	} else {
		m.overPredictKWH += diffKWH
	}
	m.actualKWH += actualKWH
	m.predKWH += predKWH
}

func (m energyEvalMetrics) maeKWH() float64 {
	if m.hoursCount == 0 {
		return 0
	}
	return m.totalAbsErrKWH / float64(m.hoursCount)
}

func (m energyEvalMetrics) rmseKWH() float64 {
	if m.hoursCount == 0 {
		return 0
	}
	return math.Sqrt(m.totalSqErrKWH2 / float64(m.hoursCount))
}

func (m energyEvalMetrics) biasKWH() float64 {
	if m.hoursCount == 0 {
		return 0
	}
	return (m.predKWH - m.actualKWH) / float64(m.hoursCount)
}

// TestEnergyHistory evaluates BuildHourlyEnergyModel against real-world recorded site energy and weather
// histories across multiple daily planning hours (04:00, 10:00, 14:00) to prevent regressions in
// home load forecasting accuracy (MAE, under-prediction, and over-prediction).
func TestEnergyHistory(t *testing.T) {
	silentLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := log.With(context.Background(), silentLogger)

	defaultLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	files, err := energyHistoryFS.ReadDir("testdata/energy_history")
	require.NoError(t, err)
	require.NotEmpty(t, files, "expected energy history datasets in testdata/energy_history")

	// Evaluate at 04:00 (early-morning grid charge planning), 10:00 (mid-morning update with morning actuals),
	// and 14:00 (pre-peak afternoon update).
	runHours := []int{4, 10, 14}

	var mu sync.Mutex
	var globalMetrics energyEvalMetrics
	var globalBaseAbsErrKWH float64
	var globalBaseUnderKWH float64
	var globalBaseOverKWH float64
	var globalBaseHours int

	t.Cleanup(func() {
		if globalMetrics.hoursCount == 0 {
			return
		}
		t.Logf("====== GLOBAL ENERGY MODEL RESULTS ======")
		t.Logf("Evaluated Runs : %d (%d hours)", globalMetrics.daysCount, globalMetrics.hoursCount)
		t.Logf("Global MAE     : %.3f kWh/hr (RMSE: %.3f kWh/hr, Bias: %+.3f kWh/hr)",
			globalMetrics.maeKWH(), globalMetrics.rmseKWH(), globalMetrics.biasKWH())
		t.Logf("Under-Predict  : %.1f kWh | Over-Predict: %.1f kWh",
			globalMetrics.underPredictKWH, globalMetrics.overPredictKWH)
		if globalBaseHours > 0 {
			baseGlobalMAE := globalBaseAbsErrKWH / float64(globalBaseHours)
			t.Logf("Baseline MAE   : %.3f kWh/hr | Base Under: %.1f kWh | Base Over: %.1f kWh",
				baseGlobalMAE, globalBaseUnderKWH, globalBaseOverKWH)
			assert.LessOrEqual(t, math.Round(globalMetrics.maeKWH()*1000)/1000, baseGlobalMAE+0.005,
				"Global energy model MAE across all sites should meet or beat baseline")
			assert.LessOrEqual(t, math.Round(globalMetrics.underPredictKWH*10)/10, globalBaseUnderKWH*1.02+10.0,
				"Global under-prediction kWh should not regress significantly")
			assert.LessOrEqual(t, math.Round(globalMetrics.overPredictKWH*10)/10, globalBaseOverKWH*1.02+10.0,
				"Global over-prediction kWh should not regress significantly")
		}
		t.Logf("=========================================")
	})

	for _, file := range files {
		fileName := file.Name()
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			fileBytes, err := energyHistoryFS.ReadFile("testdata/energy_history/" + fileName)
			require.NoError(t, err)

			var dataset EnergyHistoryDataset
			err = json.Unmarshal(fileBytes, &dataset)
			require.NoError(t, err)

			fileLoc := defaultLoc
			if dataset.TimeZone != "" {
				if l, err := time.LoadLocation(dataset.TimeZone); err == nil {
					fileLoc = l
				}
			} else if len(dataset.EnergyHistory) > 0 && dataset.EnergyHistory[0].TimeLocation != "" {
				if l, err := time.LoadLocation(dataset.EnergyHistory[0].TimeLocation); err == nil {
					fileLoc = l
				}
			}

			settings := types.Settings{}
			settings, _, err = types.MigrateSettings(settings, 0, "production")
			require.NoError(t, err)
			settings.Location = &types.SiteLocation{TimeZone: fileLoc.String()}
			settings.EVChargingPeriods = dataset.EVChargingPeriods

			var allHours []types.EnergyStats
			dayHourlyMap := make(map[string]map[int]float64)
			historyByUTC := make(map[time.Time]float64)

			for _, day := range dataset.EnergyHistory {
				for _, h := range day.Hourly {
					if h.TSHourStart.IsZero() {
						continue
					}
					allHours = append(allHours, h)
					historyByUTC[h.TSHourStart.UTC()] = h.HomeKWH
					localT := h.TSHourStart.In(fileLoc)
					dStr := localT.Format("2006-01-02")
					if dayHourlyMap[dStr] == nil {
						dayHourlyMap[dStr] = make(map[int]float64)
					}
					dayHourlyMap[dStr][localT.Hour()] = h.HomeKWH
				}
			}

			var validLoads []float64
			for _, h := range allHours {
				if h.HomeKWH > 0.05 {
					validLoads = append(validLoads, h.HomeKWH)
				}
			}
			standbyLoadKWH := 0.1
			if len(validLoads) > 0 {
				sort.Float64s(validLoads)
				standbyLoadKWH = max(0.1, validLoads[int(float64(len(validLoads)-1)*0.01)])
			}

			// BuildHourlyEnergyModel strips EV charging spikes inside configured EVChargingPeriods to predict
			// underlying household load, so target actuals are sanitized the same way before scoring.
			findNonEVActualKWH := func(localTS time.Time, rawLoadKWH float64) float64 {
				if len(settings.EVChargingPeriods) > 0 && rawLoadKWH >= EVMinThresholdKW {
					for _, evp := range settings.EVChargingPeriods {
						if inEV, _, err := evp.Contains(localTS); err == nil && inEV {
							for dayOffset := 1; dayOffset <= 7; dayOffset++ {
								prevUTC := localTS.AddDate(0, 0, -dayOffset).Truncate(time.Hour).UTC()
								if prevKWH, ok := historyByUTC[prevUTC]; ok && prevKWH < EVMinThresholdKW && prevKWH > 0.05 {
									return prevKWH
								}
							}
							utcTS := localTS.Truncate(time.Hour).UTC()
							for lb := 1; lb <= 12; lb++ {
								if prevKWH, ok := historyByUTC[utcTS.Add(-time.Duration(lb)*time.Hour)]; ok && prevKWH < EVMinThresholdKW && prevKWH > 0.05 {
									return prevKWH
								}
							}
							return standbyLoadKWH
						}
					}
				}
				return rawLoadKWH
			}

			simStartStr := dataset.SimStart.UTC().Format("2006-01-02")
			simEndStr := dataset.SimEnd.UTC().Format("2006-01-02")

			var evalDates []string
			for dStr, hMap := range dayHourlyMap {
				if dStr >= simStartStr && dStr < simEndStr && len(hMap) >= 20 {
					evalDates = append(evalDates, dStr)
				}
			}
			sort.Strings(evalDates)
			require.NotEmpty(t, evalDates, "no valid evaluation days found for %s", fileName)

			c := NewController()
			byHour := make(map[int]energyEvalMetrics)
			var overall energyEvalMetrics

			for _, dStr := range evalDates {
				dayStart, err := time.ParseInLocation("2006-01-02", dStr, fileLoc)
				require.NoError(t, err)

				histWindowStart := dayStart.AddDate(0, 0, -35)
				priorHours := 0
				for _, h := range allHours {
					if !h.TSHourStart.Before(histWindowStart) && h.TSHourStart.Before(dayStart) {
						priorHours++
					}
				}
				if priorHours < 7*20 {
					continue
				}

				for _, runHr := range runHours {
					now := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), runHr, 0, 0, 0, fileLoc)
					hStart := now.AddDate(0, 0, -35)

					var histSlice []types.EnergyStats
					for _, h := range allHours {
						if !h.TSHourStart.Before(hStart) && h.TSHourStart.Before(now) {
							histSlice = append(histSlice, h)
						}
					}

					wEnd := dayStart.AddDate(0, 0, 2)
					var weatherSlice []types.Weather
					for _, w := range dataset.WeatherHistory {
						if !w.TSDayStart.Before(hStart) && w.TSDayStart.Before(wEnd) {
							weatherSlice = append(weatherSlice, w)
						}
					}

					model, _ := c.BuildHourlyEnergyModel(ctx, now, histSlice, weatherSlice, settings)

					hm := byHour[runHr]
					hm.daysCount++

					hMap := dayHourlyMap[dStr]
					for targetHr := runHr; targetHr < 24; targetHr++ {
						actualRawKWH, ok := hMap[targetHr]
						if !ok || actualRawKWH <= 0.05 {
							continue
						}
						targetTS := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), targetHr, 0, 0, 0, fileLoc)
						actualKWH := findNonEVActualKWH(targetTS, actualRawKWH)
						predKWH := model[targetHr].AvgHomeLoadKWH

						hm.add(actualKWH, predKWH)
						overall.add(actualKWH, predKWH)
					}
					byHour[runHr] = hm
					overall.daysCount++
				}
			}

			maeRounded := math.Round(overall.maeKWH()*1000.0) / 1000.0
			underRounded := math.Round(overall.underPredictKWH*10.0) / 10.0
			overRounded := math.Round(overall.overPredictKWH*10.0) / 10.0

			base, hasBaseline := energyHistoryBaselines[fileName]
			if !hasBaseline {
				t.Errorf("Missing baseline in energyHistoryBaselines for %q: {maeKWH: %.3f, underPredictKWH: %.1f, overPredictKWH: %.1f}",
					fileName, maeRounded, underRounded, overRounded)
			} else {
				t.Logf("%-24s | Hrs: %4d | MAE: %.3f (base %.3f, %+.3f) [04h:%.3f 10h:%.3f 14h:%.3f] | RMSE: %.3f | Under: %6.1f (base %6.1f) | Over: %6.1f (base %6.1f)",
					fileName, overall.hoursCount, overall.maeKWH(), base.maeKWH, overall.maeKWH()-base.maeKWH,
					byHour[4].maeKWH(), byHour[10].maeKWH(), byHour[14].maeKWH(),
					overall.rmseKWH(), overall.underPredictKWH, base.underPredictKWH, overall.overPredictKWH, base.overPredictKWH)
			}

			mu.Lock()
			globalMetrics.hoursCount += overall.hoursCount
			globalMetrics.daysCount += overall.daysCount
			globalMetrics.totalAbsErrKWH += overall.totalAbsErrKWH
			globalMetrics.totalSqErrKWH2 += overall.totalSqErrKWH2
			globalMetrics.underPredictKWH += overall.underPredictKWH
			globalMetrics.overPredictKWH += overall.overPredictKWH
			globalMetrics.actualKWH += overall.actualKWH
			globalMetrics.predKWH += overall.predKWH
			if hasBaseline {
				globalBaseHours += overall.hoursCount
				globalBaseAbsErrKWH += base.maeKWH * float64(overall.hoursCount)
				globalBaseUnderKWH += base.underPredictKWH
				globalBaseOverKWH += base.overPredictKWH
			}
			mu.Unlock()

			if hasBaseline {
				assert.LessOrEqual(t, maeRounded, base.maeKWH+0.010,
					"Energy model MAE should not regress beyond baseline for %s", fileName)
				assert.LessOrEqual(t, underRounded, base.underPredictKWH*1.03+5.0,
					"Energy model under-prediction kWh should not regress beyond baseline for %s", fileName)
				assert.LessOrEqual(t, overRounded, base.overPredictKWH*1.03+5.0,
					"Energy model over-prediction kWh should not regress beyond baseline for %s", fileName)
			}
		})
	}
}
