package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var planBaselines = map[string]float64{
	"site1_march.json":      -4.361,
	"site1_may.json":        -16.043,
	"site1_september.json":  48.677,
	"site2_april.json":      0.164,
	"site2_march.json":      9.261,
	"site2_may.json":        1.411,
	"site2_september.json":  -7.383,
	"site3_march.json":      -1.930,
	"site3_may.json":        -6.952,
	"site4_late-may.json":   0.111,
	"site4_may.json":        1.799,
	"site4_september.json":  -4.648,
	"site5_june.json":       18.278,
	"site5_september.json":  45.956,
	"site7_june.json":       30.730,
	"site8_june.json":       -10.810,
	"site9_september.json":  -23.773,
	"site10_september.json": 2.146,
}

// TestPlanHistory evaluates the new Plan engine against real-world recorded site history datasets
// to benchmark financial savings and battery performance against the historical baselines established by Decide().
func TestPlanHistory(t *testing.T) {
	ctx := context.Background()
	c := NewController()

	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	files, err := historyFS.ReadDir("testdata/history")
	require.NoError(t, err)

	var mu sync.Mutex
	var totalSimCost float64
	var totalBaselineCost float64

	t.Cleanup(func() {
		netTotalSavings := totalBaselineCost - totalSimCost
		var pctTotalSavings float64
		if totalBaselineCost != 0 {
			pctTotalSavings = (netTotalSavings / totalBaselineCost) * 100.0
		}
		fmt.Fprintf(os.Stderr, "\n====== GLOBAL PLAN REPLAY RESULTS ======\n")
		fmt.Fprintf(os.Stderr, "Total Baseline Cost : $%.3f\n", totalBaselineCost)
		fmt.Fprintf(os.Stderr, "Total Simulated Cost: $%.3f\n", totalSimCost)
		fmt.Fprintf(os.Stderr, "Total Net Savings   : $%.3f (%.2f%%)\n", netTotalSavings, pctTotalSavings)
		fmt.Fprintf(os.Stderr, "========================================\n")
		assert.LessOrEqual(t, totalSimCost, totalBaselineCost+0.10, "Global simulated cost across all sites should meet or beat baseline")
	})

	for _, file := range files {
		fileName := file.Name()
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			fileBytes, err := historyFS.ReadFile("testdata/history/" + fileName)
			require.NoError(t, err)

			var dataset types.ControllerHistoryDataset
			err = json.Unmarshal(fileBytes, &dataset)
			require.NoError(t, err)

			fileLoc := loc
			if len(dataset.EnergyHistory) > 0 && dataset.EnergyHistory[0].TimeLocation != "" {
				if l, err := time.LoadLocation(dataset.EnergyHistory[0].TimeLocation); err == nil {
					fileLoc = l
				}
			} else if dataset.SiteID == "site5" {
				var err error
				fileLoc, err = time.LoadLocation("America/New_York")
				require.NoError(t, err)
			}
			simStart := dataset.SimStart.In(fileLoc)
			simEnd := dataset.SimEnd.In(fileLoc)

			var initialSOC float64 = 50.0
			var capacityKWH float64 = 13.6
			var maxChargeKW float64 = 5.0
			var maxDischargeKW float64 = 5.0
			var firstActionFound bool
			var firstActionTime time.Time

			for _, action := range dataset.ActionHistory {
				tsLocal := action.Timestamp.In(fileLoc)
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
					firstActionTime = tsLocal
					firstActionFound = true
					break
				}
			}

			if !firstActionFound {
				t.Skip("No actions found in simulation time window")
				return
			}

			// Instantiate default settings
			settings := types.Settings{}
			settings, _, err = types.MigrateSettings(settings, 0, "production")
			require.NoError(t, err)

			// Site-specific settings overrides
			settings.Location = &types.SiteLocation{TimeZone: fileLoc.String()}
			settings.GridChargeBatteries = true
			settings.GridExportSolar = true

			if strings.HasSuffix(fileName, "_september.json") {
				//settings.ManageTOUSchedules = true
			}

			if dataset.SiteID == "site1" {
				settings.UtilityRateOptions.NetMeteringCredits = true
			} else if dataset.SiteID == "site2" {
				if fileName == "site2_march.json" || fileName == "site2_april.json" || fileName == "site2_may.json" {
					settings.GridExportSolar = false
				}
			} else if dataset.SiteID == "site4" {
				settings.MinBatterySOC = 5
			} else if dataset.SiteID == "site5" {
				settings.UtilityRateOptions.VPPProgram = "ess-passive"
				settings.MinBatterySOC = 5
			} else if dataset.SiteID == "site7" {
				settings.GridChargeBatteries = false
			} else if dataset.SiteID == "site8" {
				settings.GridChargeBatteries = false
				settings.GridExportBatteries = true
			} else if dataset.SiteID == "site9" {
				settings.UtilityRateOptions.NetMeteringScheme = "nem2"
				settings.UtilityRateOptions.GenerationRate = "sdcp_power_on"
				settings.UtilityRateOptions.Location = "san_diego"
				settings.MinBatterySOC = 5
			} else if dataset.SiteID == "site10" {
				settings.UtilityRateOptions.NetMeteringScheme = "sbp"
				settings.MinBatterySOC = 20
				settings.GridChargeBatteries = false
				settings.GridExportBatteries = true
			}

			// Adjust simStart to the hour of the first action to align the evaluation window
			simStart = firstActionTime.In(fileLoc).Truncate(time.Hour)

			// Filter actions that fall within the simulation range
			var activeActions []types.Action
			for _, a := range dataset.ActionHistory {
				tsLocal := a.Timestamp.In(fileLoc)
				if !tsLocal.Before(simStart) && tsLocal.Before(simEnd) {
					activeActions = append(activeActions, a)
				}
			}

			var allDatasetVPPEvents []types.VPPEvent
			seenVPPKeys := make(map[string]bool)
			for _, a := range dataset.ActionHistory {
				for _, ev := range a.SystemStatus.VPPEvents {
					key := fmt.Sprintf("%s_%s_%s", ev.Description, ev.TSStart.Format(time.RFC3339), ev.TSEnd.Format(time.RFC3339))
					if !seenVPPKeys[key] {
						seenVPPKeys[key] = true
						// Note: Historical test data predates the mandatory flag.
						// Site5 events were all mandatory Eversource passive dispatch.
						if dataset.SiteID == "site5" && !ev.Mandatory && ev.DollarsPerKWH == 0 {
							ev.Mandatory = true
							ev.DollarsPerKWH = 0
						} else if !ev.Mandatory && ev.DollarsPerKWH == 0 {
							// TODO: Find a better way to determine the export compensation price per VPP program (e.g. via utility API query or tariff schedule lookup)
							ev.DollarsPerKWH = 2.0
						}
						allDatasetVPPEvents = append(allDatasetVPPEvents, ev)
					}
				}
			}

			simSOC := initialSOC
			simCost := 0.0
			simCredit := 0.0
			var lastGridImportPrice float64 = 0.10
			var lastAction *types.Action
			var planModes []types.BatteryMode
			var planReasons []types.ActionReason

			for i := 0; i < len(activeActions); i++ {
				action := activeActions[i]
				tCurrent := action.Timestamp

				var tNext time.Time
				if i < len(activeActions)-1 {
					tNext = activeActions[i+1].Timestamp
				} else {
					tNext = tCurrent.Add(1 * time.Hour)
				}

				duration := tNext.Sub(tCurrent)
				if duration <= 0 {
					continue
				}

				currentPrice, ok := findPrice(dataset.PriceHistory, tCurrent)
				if !ok {
					if action.CurrentPrice != nil {
						currentPrice = *action.CurrentPrice
					} else {
						currentPrice = types.Price{
							TSStart:              tCurrent,
							TSEnd:                tCurrent.Add(time.Hour),
							DollarsPerKWH:        0.10,
							GridUseDollarsPerKWH: 0.04,
						}
					}
				}
				lastGridImportPrice = currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

				var futurePrices []types.Price
				futureEnd := tCurrent.Add(24 * time.Hour)
				for _, p := range dataset.PriceHistory {
					if !p.TSStart.Before(tCurrent) && p.TSStart.Before(futureEnd) {
						futurePrices = append(futurePrices, p)
					}
				}
				// If future prices run out near the end of the recorded dataset,
				// wrap prices shifted forward by 24 hours, matching siteUpdate's fallback behavior.
				if len(futurePrices) < 4 && len(dataset.PriceHistory) > 0 {
					for _, p := range dataset.PriceHistory {
						shifted := p
						shifted.TSStart = shifted.TSStart.Add(24 * time.Hour)
						shifted.TSEnd = shifted.TSEnd.Add(24 * time.Hour)
						if !shifted.TSStart.Before(tCurrent) && shifted.TSStart.Before(futureEnd) {
							futurePrices = append(futurePrices, shifted)
						}
					}
				}

				// Build mocked history (future 24 hours)
				var mockHistory []types.EnergyStats
				for k := 0; k < 24; k++ {
					tFuture := tCurrent.Truncate(time.Hour).Add(time.Duration(k) * time.Hour)
					stat, _ := findEnergyStats(dataset.EnergyHistory, tFuture, fileLoc)
					mockHistory = append(mockHistory, types.EnergyStats{
						TSHourStart:   tFuture,
						HomeKWH:       stat.HomeKWH,
						SolarKWH:      stat.SolarKWH,
						GridExportKWH: stat.GridExportKWH,
						MaxBatterySOC: 50.0,
					})
				}

				// Build mocked weather (future 24 hours)
				var forecastHours []types.HourlyWeather
				var firstSolarTime time.Time
				var lastSolarTime time.Time
				for k := 0; k < 24; k++ {
					tFuture := tCurrent.Truncate(time.Hour).Add(time.Duration(k) * time.Hour)
					stat, _ := findEnergyStats(dataset.EnergyHistory, tFuture, fileLoc)
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
						TSDayStart:    tCurrent.In(fileLoc).Truncate(24 * time.Hour),
						TimeLocation:  settings.Location.TimeZone,
						TSSunrise:     firstSolarTime,
						TSSunset:      lastSolarTime,
						ForecastHours: forecastHours,
					},
				}

				simStatus := action.SystemStatus
				simStatus.Timestamp = tCurrent.In(fileLoc)
				simStatus.TimeLocation = fileLoc.String()
				simStatus.BatterySOC = simSOC
				simStatus.BatteryAboveMinSOC = simSOC > settings.MinBatterySOC
				simStatus.BatteryCapacityKWH = capacityKWH
				simStatus.MaxBatteryChargeKW = maxChargeKW
				simStatus.MaxBatteryDischargeKW = maxDischargeKW
				var currentVPPEvents []types.VPPEvent
				for _, ev := range allDatasetVPPEvents {
					if ev.TSEnd.After(tCurrent) {
						currentVPPEvents = append(currentVPPEvents, ev)
					}
				}
				simStatus.VPPEvents = currentVPPEvents

				decision, _, err := c.Plan(ctx, simStatus, currentPrice, futurePrices, mockHistory, mockWeather, settings, lastAction)
				require.NoError(t, err)

				decidedMode := decision.Action.BatteryMode
				planModes = append(planModes, decidedMode)
				planReasons = append(planReasons, decision.Action.Reason)
				lastAction = &decision.Action

				// Run minute-by-minute simulation for the interval [tCurrent, tNext]
				stepMinutes := int(math.Ceil(duration.Minutes()))
				for m := 0; m < stepMinutes; m++ {
					tMin := tCurrent.Add(time.Duration(m) * time.Minute)
					dt := 1.0 / 60.0

					stat, _ := findEnergyStats(dataset.EnergyHistory, tMin, fileLoc)
					homeKW := stat.HomeKWH
					solarKW := stat.SolarKWH

					pMin, okMin := findPrice(dataset.PriceHistory, tMin)
					var gridImportPrice float64
					var gridExportPrice float64
					if okMin {
						gridImportPrice = pMin.DollarsPerKWH + pMin.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tMin)
							if gridExportPrice != 0.0 {
								gridExportPrice += pMin.GenerationAdjustmentDollarsPerKWH
							}
						} else if pMin.SeparateGenerationCredit {
							gridExportPrice = pMin.GenerationCreditDollarsPerKWH
						} else {
							gridExportPrice = pMin.DollarsPerKWH + pMin.GenerationAdjustmentDollarsPerKWH
						}
					} else {
						gridImportPrice = currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
						if !settings.GridExportSolar {
							gridExportPrice = 0.0
						} else if settings.UtilityRateOptions.NetMeteringCredits {
							gridExportPrice = calculateMinFuturePrice(dataset.PriceHistory, tMin)
						} else {
							gridExportPrice = currentPrice.DollarsPerKWH
						}
					}
					for _, ev := range simStatus.VPPEvents {
						if ev.OptOut {
							continue
						}
						if ev.DollarsPerKWH > 0 && !tMin.Before(ev.TSStart) && tMin.Before(ev.TSEnd) {
							gridExportPrice = ev.DollarsPerKWH
							break
						}
					}
					lastGridImportPrice = gridImportPrice

					energy := simSOC * capacityKWH / 100.0
					minEnergy := (settings.MinBatterySOC / 100.0) * capacityKWH

					var pBattCharge float64
					var pBattDischarge float64

					switch decidedMode {
					case types.BatteryModeChargeAny:
						pBattDischarge = 0.0
						surplusSolar := math.Max(0.0, solarKW-homeKW)
						solarCharge := math.Min(maxChargeKW, surplusSolar)

						gridCharge := 0.0
						if settings.GridChargeBatteries {
							targetSOC := 100
							if decision.Action.ChargeToSOC > 0 {
								targetSOC = decision.Action.ChargeToSOC
							}
							targetEnergyLimit := capacityKWH * float64(targetSOC) / 100.0
							if energy < targetEnergyLimit {
								targetGridCharge := math.Max(0.0, (targetEnergyLimit-energy)/dt)
								gridCharge = math.Min(maxChargeKW, targetGridCharge)
								gridCharge = math.Max(0.0, gridCharge-solarCharge)
							}
						}
						pBattCharge = math.Min(maxChargeKW, solarCharge+gridCharge)
						pBattCharge = math.Min(pBattCharge, (capacityKWH-energy)/dt)

					case types.BatteryModeExport:
						pBattDischarge = math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)/dt))
						pBattCharge = 0.0

					case types.BatteryModeLoad:
						if decision.Action.SolarMode == types.SolarModeExport {
							pBattDischarge = math.Min(homeKW, math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)/dt)))
							pBattCharge = 0.0
						} else {
							netLoad := homeKW - solarKW
							if netLoad > 0 {
								pBattDischarge = math.Min(netLoad, math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)/dt)))
								pBattCharge = 0.0
							} else {
								surplusSolar := solarKW - homeKW
								pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/dt))
								pBattDischarge = 0.0
							}
						}

					default: // Standby, etc.
						if decision.Action.SolarMode == types.SolarModeExport {
							pBattCharge = 0.0
							pBattDischarge = 0.0
						} else {
							netLoad := homeKW - solarKW
							if netLoad < 0 {
								surplusSolar := solarKW - homeKW
								pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/dt))
							} else {
								pBattCharge = 0.0
							}
							pBattDischarge = 0.0
						}
					}

					energy = energy + (pBattCharge-pBattDischarge)*dt
					simSOC = energy / capacityKWH * 100.0
					if simSOC < 0.0 {
						simSOC = 0.0
					}
					if simSOC > 100.0 {
						simSOC = 100.0
					}

					gridKW := homeKW - solarKW + pBattCharge - pBattDischarge
					if gridKW > 0 {
						simCost += gridKW * dt * gridImportPrice
					} else if gridKW < 0 {
						exportKWH := math.Abs(gridKW) * dt
						if settings.GridExportSolar || settings.GridExportBatteries {
							simCredit += exportKWH * gridExportPrice
						}
					}
				}
			}

			baseNetCost, hasBaseline := planBaselines[fileName]
			decideBaseCost, _ := fileBaselines[fileName]
			if !hasBaseline {
				var baselineCostVal, baselineCreditVal float64
				for _, stat := range dataset.ActionHistory {
					statTime := stat.Timestamp.In(fileLoc)
					if statTime.Before(firstActionTime) || !statTime.Before(simEnd) {
						continue
					}
					pMin, okMin := findPrice(dataset.PriceHistory, statTime)
					var gridImportPrice float64
					var gridExportPrice float64
					if okMin {
						gridImportPrice = pMin.DollarsPerKWH + pMin.GridUseDollarsPerKWH
						if settings.GridExportSolar {
							gridExportPrice = pMin.DollarsPerKWH
						}
					} else {
						gridImportPrice = 0.10
					}
					baselineCostVal += stat.SystemStatus.HomeKW * (15.0 / 60.0) * gridImportPrice
					baselineCreditVal += stat.SystemStatus.SolarKW * (15.0 / 60.0) * gridExportPrice
				}
				baseNetCost = baselineCostVal - baselineCreditVal
			}

			simNetCost := simCost - simCredit
			startEnergyKWH := (initialSOC / 100.0) * capacityKWH
			endEnergyKWH := (simSOC / 100.0) * capacityKWH
			deltaEnergyKWH := endEnergyKWH - startEnergyKWH
			batteryAssetValue := deltaEnergyKWH * lastGridImportPrice
			simAdjustedNetCost := simNetCost - batteryAssetValue

			savingsVsDecide := decideBaseCost - simAdjustedNetCost
			var pctSavingsVsDecide float64
			if decideBaseCost != 0 {
				pctSavingsVsDecide = (savingsVsDecide / decideBaseCost) * 100.0
			}

			modeCounts := make(map[types.BatteryMode]int)
			for _, m := range planModes {
				modeCounts[m]++
			}
			reasonCounts := make(map[types.ActionReason]int)
			for _, r := range planReasons {
				reasonCounts[r]++
			}
			fmt.Fprintf(os.Stderr, "%-20s | Cash: $%7.3f | SOC: %5.1f%% -> %5.1f%% (dE: %+6.2f kWh, AssetVal: $%+6.3f) | PlanAdjNetCost: $%7.3f | PlanBase: $%7.3f | DecideBase: $%7.3f | SavVsDecide: $%+6.3f (%5.1f%%) | Modes: %v | Reasons: %v | Cost: $%.2f Credit: $%.2f\n",
				fileName, simNetCost, initialSOC, simSOC, deltaEnergyKWH, batteryAssetValue, simAdjustedNetCost, baseNetCost, decideBaseCost, savingsVsDecide, pctSavingsVsDecide, modeCounts, reasonCounts, simCost, simCredit)

			mu.Lock()
			totalSimCost += simAdjustedNetCost
			totalBaselineCost += baseNetCost
			mu.Unlock()

			if hasBaseline {
				simAdjustedNetCostRounded := math.Round(simAdjustedNetCost*1000.0) / 1000.0
				// Allow up to $0.10 regression per site
				allowedBuffer := 0.10
				assert.LessOrEqual(t, simAdjustedNetCostRounded, baseNetCost+allowedBuffer, "Plan simulated adjusted net cost should meet or beat baseline")
			}
		})
	}
}
