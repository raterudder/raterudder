package controller

import (
	"context"
	"embed"
	"encoding/json"
	"math"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/history/*.json
var historyFS embed.FS

var fileBaselines = map[string]float64{
	"site1_march.json": -4.845,
	"site1_may.json":   -15.827,
	"site2_march.json": 9.192,
	"site2_may.json":   0.612,
	"site3_march.json": -1.741,
	"site3_may.json":   -6.409,
	"site4_may.json":   3.358,
}

func findEnergyStats(history []types.DailyEnergyStats, ts time.Time, loc *time.Location) (types.EnergyStats, bool) {
	targetHour := ts.In(loc).Truncate(time.Hour)
	for _, day := range history {
		for _, hour := range day.Hourly {
			if hour.TSHourStart.In(loc).Truncate(time.Hour).Equal(targetHour) {
				return hour, true
			}
		}
	}
	return types.EnergyStats{}, false
}

func findPrice(priceHistory []types.Price, ts time.Time) (types.Price, bool) {
	for _, p := range priceHistory {
		if p.Contains(ts) {
			return p, true
		}
	}
	// Fallback to exact match of TSStart truncated to hour
	targetHour := ts.UTC().Truncate(time.Hour)
	for _, p := range priceHistory {
		if p.TSStart.UTC().Truncate(time.Hour).Equal(targetHour) {
			return p, true
		}
	}
	return types.Price{}, false
}

func calculateMinFuturePrice(priceHistory []types.Price, start time.Time) float64 {
	minPrice := -999.0
	hasPrice := false
	windowEnd := start.Add(24 * time.Hour)
	for _, p := range priceHistory {
		if !p.TSStart.Before(start) && p.TSStart.Before(windowEnd) {
			cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
			if !hasPrice || cost < minPrice {
				minPrice = cost
				hasPrice = true
			}
		}
	}
	if !hasPrice {
		return 0.0
	}
	return minPrice
}

func TestDecideHistory(t *testing.T) {
	ctx := context.Background()
	c := NewController()

	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	files, err := historyFS.ReadDir("testdata/history")
	require.NoError(t, err)

	var totalSimCost float64
	var totalBaselineCost float64

	for _, file := range files {
		fileName := file.Name()
		t.Run(fileName, func(t *testing.T) {
			fileBytes, err := historyFS.ReadFile("testdata/history/" + fileName)
			require.NoError(t, err)

			var dataset types.ControllerHistoryDataset
			err = json.Unmarshal(fileBytes, &dataset)
			require.NoError(t, err)

			// Load boundaries directly from the JSON dataset
			simStart := dataset.SimStart.In(loc)
			simEnd := dataset.SimEnd.In(loc)

			// Find initial state from first action in the range
			var initialSOC float64 = 50.0
			var capacityKWH float64 = 13.6
			var maxChargeKW float64 = 5.0
			var maxDischargeKW float64 = 5.0
			var firstActionFound bool
			var firstActionTime time.Time

			for _, action := range dataset.ActionHistory {
				tsLocal := action.Timestamp.In(loc)
				if !tsLocal.Before(simStart) && tsLocal.Before(simEnd) {
					initialSOC = action.SystemStatus.BatterySOC
					if action.SystemStatus.BatteryCapacityKWH > 0 {
						capacityKWH = action.SystemStatus.BatteryCapacityKWH
					}
					if action.SystemStatus.MaxBatteryChargeKW > 0 {
						maxChargeKW = action.SystemStatus.MaxBatteryChargeKW
					}
					if action.SystemStatus.MaxBatteryDischargeKW > 0 {
						maxDischargeKW = action.SystemStatus.MaxBatteryDischargeKW
					}
					firstActionTime = action.Timestamp
					firstActionFound = true
					break
				}
			}

			if !firstActionFound {
				t.Fatalf("No action found in the simulation range %s to %s for %s", simStart, simEnd, fileName)
			}

			// Adjust simStart to the hour of the first action to align the evaluation window
			simStart = firstActionTime.In(loc).Truncate(time.Hour)

			// Instantiate default settings
			settings := types.Settings{}
			settings, _, err = types.MigrateSettings(settings, 0)
			require.NoError(t, err)

			// Site-specific settings overrides
			settings.Location = &types.SiteLocation{TimeZone: "America/Chicago"}
			settings.GridChargeBatteries = true
			settings.GridExportSolar = true

			if dataset.SiteID == "site1" {
				settings.UtilityRateOptions.NetMeteringCredits = true
			} else if dataset.SiteID == "site2" {
				settings.GridExportSolar = false
			}

			// Filter actions that fall within the simulation range
			var activeActions []types.Action
			for _, a := range dataset.ActionHistory {
				tsLocal := a.Timestamp.In(loc)
				if !tsLocal.Before(simStart) && tsLocal.Before(simEnd) {
					activeActions = append(activeActions, a)
				}
			}

			// Run simulation
			simSOC := initialSOC
			var simCost float64
			var simCredit float64

			for i := 0; i < len(activeActions); i++ {
				action := activeActions[i]
				tCurrent := action.Timestamp

				var tNext time.Time
				if i < len(activeActions)-1 {
					tNext = activeActions[i+1].Timestamp
				} else {
					tNext = tCurrent.Add(1 * time.Hour) // default final step to 1 hour
				}

				duration := tNext.Sub(tCurrent)
				if duration <= 0 {
					continue
				}

				// Find current price
				currentPrice, ok := findPrice(dataset.PriceHistory, tCurrent)
				if !ok {
					if action.CurrentPrice != nil {
						currentPrice = *action.CurrentPrice
					} else {
						t.Fatalf("No current price found for timestamp %s", tCurrent)
					}
				}

				// Fetch future prices (next 24 hours)
				var futurePrices []types.Price
				futureEnd := tCurrent.Add(24 * time.Hour)
				for _, p := range dataset.PriceHistory {
					if !p.TSStart.Before(tCurrent) && p.TSStart.Before(futureEnd) {
						futurePrices = append(futurePrices, p)
					}
				}

				// Build mocked history (future 24 hours)
				var mockHistory []types.EnergyStats
				for k := 0; k < 24; k++ {
					tFuture := tCurrent.Truncate(time.Hour).Add(time.Duration(k) * time.Hour)
					stat, _ := findEnergyStats(dataset.EnergyHistory, tFuture, loc)
					mockHistory = append(mockHistory, types.EnergyStats{
						TSHourStart:   tFuture,
						HomeKWH:       stat.HomeKWH,
						SolarKWH:      stat.SolarKWH,
						GridExportKWH: stat.GridExportKWH,
						MaxBatterySOC: 50.0, // so it doesn't trigger curtailed logic
					})
				}

				// Build mocked weather (future 24 hours)
				var forecastHours []types.HourlyWeather
				var firstSolarTime time.Time
				var lastSolarTime time.Time
				for k := 0; k < 24; k++ {
					tFuture := tCurrent.Truncate(time.Hour).Add(time.Duration(k) * time.Hour)
					stat, _ := findEnergyStats(dataset.EnergyHistory, tFuture, loc)
					if stat.SolarKWH > 0.05 {
						if firstSolarTime.IsZero() {
							firstSolarTime = tFuture
						}
						lastSolarTime = tFuture
					}
					forecastHours = append(forecastHours, types.HourlyWeather{
						TSHourStart:  tFuture,
						GTI:          100.0 * stat.SolarKWH,
						TemperatureC: 25.0,
					})
				}

				if firstSolarTime.IsZero() {
					firstSolarTime = tCurrent.Truncate(time.Hour).Add(6 * time.Hour)
				}
				if lastSolarTime.IsZero() {
					lastSolarTime = tCurrent.Truncate(time.Hour).Add(19 * time.Hour)
				}

				mockWeather := []types.Weather{
					{
						TSDayStart:    tCurrent.In(loc).Truncate(24 * time.Hour),
						TimeLocation:  "America/Chicago",
						TSSunrise:     firstSolarTime,
						TSSunset:      lastSolarTime,
						ForecastHours: forecastHours,
					},
				}

				// Populate current status for Decision
				simStatus := action.SystemStatus
				simStatus.Timestamp = tCurrent
				simStatus.BatterySOC = simSOC
				simStatus.BatteryAboveMinSOC = simSOC > settings.MinBatterySOC
				simStatus.BatteryCapacityKWH = capacityKWH
				simStatus.MaxBatteryChargeKW = maxChargeKW
				simStatus.MaxBatteryDischargeKW = maxDischargeKW

				// Call Decide
				decision, err := c.Decide(ctx, simStatus, currentPrice, futurePrices, mockHistory, mockWeather, settings)
				require.NoError(t, err)

				decidedMode := decision.Action.BatteryMode

				// Run minute-by-minute simulation for the interval [tCurrent, tNext]
				stepMinutes := int(math.Ceil(duration.Minutes()))
				for m := 0; m < stepMinutes; m++ {
					tMin := tCurrent.Add(time.Duration(m) * time.Minute)
					dt := 1.0 / 60.0 // 1 minute in hours

					// Get hourly stats for this minute
					stat, _ := findEnergyStats(dataset.EnergyHistory, tMin, loc)
					homeKW := stat.HomeKWH
					solarKW := stat.SolarKWH

					// Get price for this minute
					pMin, okMin := findPrice(dataset.PriceHistory, tMin)
					var gridImportPrice float64
					var gridExportPrice float64
					if okMin {
						gridImportPrice = pMin.DollarsPerKWH + pMin.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tMin)
						} else if pMin.SeparateGenerationCredit {
							gridExportPrice = pMin.GenerationCreditDollarsPerKWH
						} else {
							gridExportPrice = pMin.DollarsPerKWH
						}
					} else {
						gridImportPrice = currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tMin)
							if gridExportPrice == 0.0 {
								gridExportPrice = currentPrice.DollarsPerKWH
							}
						} else if currentPrice.SeparateGenerationCredit {
							gridExportPrice = currentPrice.GenerationCreditDollarsPerKWH
						} else {
							gridExportPrice = currentPrice.DollarsPerKWH
						}
					}

					// Calculate battery SOC progression
					energy := simSOC * capacityKWH / 100.0
					minEnergy := (settings.MinBatterySOC / 100.0) * capacityKWH

					var pBattCharge float64
					var pBattDischarge float64

					switch decidedMode {
					case types.BatteryModeChargeAny:
						targetCharge := math.Min(maxChargeKW, (capacityKWH-energy)/dt)
						surplusSolar := math.Max(0.0, solarKW-homeKW)
						solarCharge := math.Min(targetCharge, surplusSolar)
						gridCharge := 0.0
						if settings.GridChargeBatteries {
							gridCharge = targetCharge - solarCharge
						}
						pBattCharge = solarCharge + gridCharge
						pBattDischarge = 0.0

					case types.BatteryModeLoad:
						netLoad := homeKW - solarKW
						if netLoad > 0 {
							pBattDischarge = math.Min(netLoad, math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)/dt)))
							pBattCharge = 0.0
						} else {
							surplusSolar := solarKW - homeKW
							pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/dt))
							pBattDischarge = 0.0
						}

					default: // Standby, ChargeSolar, etc.
						netLoad := homeKW - solarKW
						if netLoad < 0 {
							surplusSolar := solarKW - homeKW
							pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/dt))
						} else {
							pBattCharge = 0.0
						}
						pBattDischarge = 0.0
					}

					energy = energy + (pBattCharge-pBattDischarge)*dt
					simSOC = energy / capacityKWH * 100.0
					if simSOC < 0.0 {
						simSOC = 0.0
					}
					if simSOC > 100.0 {
						simSOC = 100.0
					}

					// Grid power flow
					gridKW := homeKW - solarKW + pBattCharge - pBattDischarge
					if gridKW > 0 {
						simCost += gridKW * dt * gridImportPrice
					} else if gridKW < 0 {
						exportKWH := math.Abs(gridKW) * dt
						if settings.GridExportSolar {
							simCredit += exportKWH * gridExportPrice
						}
					}
				}
			}

			baseNetCost, hasBaseline := fileBaselines[fileName]
			if !hasBaseline {
				// Calculate actual baseline cost for the period hour-by-hour dynamically
				var baselineCostVal float64
				var baselineCreditVal float64
				hourSteps := int(simEnd.Sub(simStart).Hours())

				for h := 0; h < hourSteps; h++ {
					tHour := simStart.Add(time.Duration(h) * time.Hour)
					stat, _ := findEnergyStats(dataset.EnergyHistory, tHour, loc)
					pMin, okHour := findPrice(dataset.PriceHistory, tHour)

					var gridImportPrice float64
					var gridExportPrice float64
					if okHour {
						gridImportPrice = pMin.DollarsPerKWH + pMin.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tHour)
						} else if pMin.SeparateGenerationCredit {
							gridExportPrice = pMin.GenerationCreditDollarsPerKWH
						} else {
							gridExportPrice = pMin.DollarsPerKWH
						}
					} else {
						var closestPrice types.Price
						var minDiff time.Duration = 999 * time.Hour
						for _, act := range activeActions {
							diff := act.Timestamp.Sub(tHour)
							if diff < 0 {
								diff = -diff
							}
							if diff < minDiff && act.CurrentPrice != nil {
								minDiff = diff
								closestPrice = *act.CurrentPrice
							}
						}
						gridImportPrice = closestPrice.DollarsPerKWH + closestPrice.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tHour)
							if gridExportPrice == 0.0 {
								gridExportPrice = closestPrice.DollarsPerKWH
							}
						} else if closestPrice.SeparateGenerationCredit {
							gridExportPrice = closestPrice.GenerationCreditDollarsPerKWH
						} else {
							gridExportPrice = closestPrice.DollarsPerKWH
						}
					}

					baselineCostVal += stat.GridImportKWH * gridImportPrice
					if settings.GridExportSolar {
						baselineCreditVal += stat.GridExportKWH * gridExportPrice
					}
				}
				baseNetCost = baselineCostVal - baselineCreditVal
			}

			simNetCost := simCost - simCredit

			savings := baseNetCost - simNetCost
			var pctSavings float64
			if baseNetCost != 0 {
				pctSavings = (savings / baseNetCost) * 100.0
			}

			t.Logf("\n--- Simulation Results for %s ---", fileName)
			if !hasBaseline {
				t.Logf("Baseline Net Cost : $%.3f (Calculated dynamically)", baseNetCost)
			} else {
				t.Logf("Baseline Net Cost : $%.3f", baseNetCost)
			}
			t.Logf("Simulated Net Cost: $%.3f (Cost: $%.3f, Credit: $%.3f)", simNetCost, simCost, simCredit)
			t.Logf("Net Savings       : $%.3f (%.2f%%)", savings, pctSavings)
			t.Logf("Final Simulated SOC: %.2f%%", simSOC)
			if !hasBaseline {
				t.Logf("Suggested baseline to add to fileBaselines map: %.3f", simNetCost)
			}
			t.Log("---------------------------------")

			totalSimCost += simNetCost
			totalBaselineCost += baseNetCost

			if hasBaseline {
				// Round to 3 decimal places to avoid float precision issues when comparing against 3-decimal-place target baselines
				simNetCostRounded := math.Round(simNetCost*1000.0) / 1000.0
				assert.LessOrEqual(t, simNetCostRounded, baseNetCost, "Simulated net cost should be less than or equal to baseline")
			}
		})
	}

	netTotalSavings := totalBaselineCost - totalSimCost
	var pctTotalSavings float64
	if totalBaselineCost != 0 {
		pctTotalSavings = (netTotalSavings / totalBaselineCost) * 100.0
	}
	t.Logf("\n====== GLOBAL RESULTS ======")
	t.Logf("Total Baseline Cost : $%.3f", totalBaselineCost)
	t.Logf("Total Simulated Cost: $%.3f", totalSimCost)
	t.Logf("Total Net Savings   : $%.3f (%.2f%%)", netTotalSavings, pctTotalSavings)
	t.Logf("============================")
}
