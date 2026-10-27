package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var planBaselines = map[string]float64{
	"site1_march.json":           -3.896,
	"site1_may.json":             -15.612,
	"site1_september.json":       38.126,
	"site2_april.json":           0.734,
	"site2_march.json":           9.766,
	"site2_may.json":             1.485,
	"site2_september.json":       1.565,
	"site3_march.json":           -1.715,
	"site3_may.json":             -6.903,
	"site4_late-may.json":        1.061,
	"site4_may.json":             2.383,
	"site4_october.json":         -0.789,
	"site4_september.json":       -4.228,
	"site5_june.json":            20.499,
	"site5_october.json":         -1.107,
	"site5_september.json":       14.945,
	"site7_june.json":            32.014,
	"site8_june.json":            -10.374,
	"site9_september.json":       -14.344,
	"site10_september.json":      6.367,
	"site11_october.json":        -13.453,
	"site13_october.json":        12.415,
	"site14_october.json":        15.832,
	"site15_october.json":        5.079,
	"site15_september.json":      3.368,
	"site16_late-september.json": 3.010,
	"site5_mid-october.json":     1.790,
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
		t.Log("\n====== GLOBAL PLAN REPLAY RESULTS ======")
		t.Logf("Total Baseline Cost : $%.3f", totalBaselineCost)
		t.Logf("Total Simulated Cost: $%.3f", totalSimCost)
		t.Logf("Total Net Savings   : $%.3f (%.2f%%)", netTotalSavings, pctTotalSavings)
		t.Log("========================================")
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

			var dcCoupled bool
			if dataset.SiteID == "site1" {
				settings.UtilityRateOptions.NetMeteringCredits = true
			} else if dataset.SiteID == "site2" {
				if fileName == "site2_march.json" || fileName == "site2_april.json" || fileName == "site2_may.json" {
					settings.GridExportSolar = false
				}
			} else if dataset.SiteID == "site4" {
				settings.MinBatterySOC = 5
				if fileName == "site4_october.json" {
					settings.GridExportBatteries = true
					settings.ManageTOUSchedules = true
					settings.MinBatteryExportDifferenceDollarsPerKWH = 0.02
					settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
						{UtilityPeriodName: "On-Peak", MinBatterySOC: 5},
						{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20},
						{UtilityPeriodName: "Super Off-Peak", MinBatterySOC: 40},
					}
				}
			} else if dataset.SiteID == "site5" {
				settings.UtilityRateOptions.VPPProgram = "ess-passive"
				settings.MinBatterySOC = 5
				if fileName == "site5_october.json" || fileName == "site5_mid-october.json" {
					settings.MinBatterySOC = 25
					settings.GridExportBatteries = true
					settings.ManageTOUSchedules = true
					settings.MinBatteryExportDifferenceDollarsPerKWH = 0.04
					settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
						{UtilityPeriodName: "On-Peak", MinBatterySOC: 25},
						{UtilityPeriodName: "Off-Peak", MinBatterySOC: 25},
					}
				}
			} else if dataset.SiteID == "site7" {
				settings.GridChargeBatteries = false
				dcCoupled = true
			} else if dataset.SiteID == "site8" {
				settings.GridChargeBatteries = false
				settings.GridExportBatteries = true
				dcCoupled = true
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
				settings.ManageTOUSchedules = true
			} else if dataset.SiteID == "site11" {
				settings.MinBatterySOC = 20
				settings.GridExportBatteries = true
				settings.ManageTOUSchedules = true
			} else if dataset.SiteID == "site13" {
				settings.MinBatterySOC = 10
				settings.GridExportBatteries = true
				settings.ManageTOUSchedules = true
				settings.MinBatteryExportDifferenceDollarsPerKWH = 0.07
				dcCoupled = true
			} else if dataset.SiteID == "site14" {
				settings.MinBatterySOC = 30
				settings.GridExportBatteries = true
				settings.ManageTOUSchedules = true
				settings.MinBatteryExportDifferenceDollarsPerKWH = 0.07
				settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 1}}}, MinBatterySOC: 30},
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 6}}}, MinBatterySOC: 50},
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}}}, MinBatterySOC: 30},
				}
			} else if dataset.SiteID == "site15" {
				settings.MinBatterySOC = 20
				settings.GridExportBatteries = true
				settings.ManageTOUSchedules = true
				settings.MinBatteryExportDifferenceDollarsPerKWH = 0.07
				settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}}}, MinBatterySOC: 20},
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 16}}}, MinBatterySOC: 80},
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}}}, MinBatterySOC: 20},
					{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 21, HourEnd: 24}}}, MinBatterySOC: 80},
				}
				dcCoupled = true
			} else if dataset.SiteID == "site16" {
				settings.MinBatterySOC = 10
				settings.GridExportSolar = false
				settings.GridExportBatteries = false
				settings.ManageTOUSchedules = true
				settings.MinBatteryExportDifferenceDollarsPerKWH = 0.07
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
				simStatus.BatteryAboveMinSOC = simSOC > settings.GetMinBatterySOC(ctx, simStatus.Timestamp, fileLoc, currentPrice)
				simStatus.BatteryCapacityKWH = capacityKWH
				simStatus.MaxBatteryChargeKW = maxChargeKW
				simStatus.MaxBatteryDischargeKW = maxDischargeKW
				simStatus.DCCoupled = simStatus.DCCoupled || dcCoupled
				var currentVPPEvents []types.VPPEvent
				for _, ev := range allDatasetVPPEvents {
					if ev.TSEnd.After(tCurrent) {
						currentVPPEvents = append(currentVPPEvents, ev)
					}
				}
				simStatus.VPPEvents = currentVPPEvents

				decision, plan, err := c.Plan(ctx, simStatus, currentPrice, futurePrices, mockHistory, mockWeather, settings, lastAction)
				require.NoError(t, err)
				violations := findDominatedPlanPeriods(plan)
				if len(violations) > 0 {
					for idx, p := range plan.Periods {
						t.Logf("[%2d] %s-%s Mode:%v Reason:%v Imp:$%.4f Exp:$%.4f SOC:%.1f->%.1f Solar:%.2f Load:%.2f ImpKWH:%.2f ExpKWH:%.2f",
							idx, p.TSStart.In(fileLoc).Format("15:04"), p.TSEnd.In(fileLoc).Format("15:04"),
							p.BatteryMode, p.Reason, p.ImportDollars, p.ExportDollars,
							p.StartSOC, p.EndSOC, p.SolarKWH, p.LoadKWH, p.GridImportKWH, p.GridExportKWH)
					}
				}
				assert.Empty(t, violations, "Plan at %s must not contain dominated Hold->Export or Export->Hold periods", simStatus.Timestamp.Format(time.RFC3339))

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
						if !settings.GridExportSolar && !settings.GridExportBatteries {
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
						if !settings.GridExportSolar && !settings.GridExportBatteries {
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

					effectivePrice := currentPrice
					if okMin {
						effectivePrice = pMin
					}
					effectiveMinSOC := settings.GetMinBatterySOC(ctx, tMin.In(fileLoc), fileLoc, effectivePrice)
					energy := simSOC * capacityKWH / 100.0
					minEnergy := (effectiveMinSOC / 100.0) * capacityKWH
					oneWayEff := math.Sqrt(resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, simStatus))

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
								targetGridCharge := math.Max(0.0, (targetEnergyLimit-energy)/(dt*oneWayEff))
								gridCharge = math.Min(maxChargeKW, targetGridCharge)
								gridCharge = math.Max(0.0, gridCharge-solarCharge)
							}
						}
						pBattCharge = math.Min(maxChargeKW, solarCharge+gridCharge)
						pBattCharge = math.Min(pBattCharge, (capacityKWH-energy)/(dt*oneWayEff))

					case types.BatteryModeExport:
						exportMinEnergy := minEnergy
						if decision.Action.ChargeToSOC > 0 {
							exportMinEnergy = math.Max(exportMinEnergy, capacityKWH*math.Min(100.0, float64(decision.Action.ChargeToSOC))/100.0)
						}
						pBattDischarge = math.Min(maxDischargeKW, math.Max(0.0, (energy-exportMinEnergy)*oneWayEff/dt))
						pBattCharge = 0.0

					case types.BatteryModeLoad:
						if decision.Action.SolarMode == types.SolarModeExport {
							pBattDischarge = math.Min(homeKW, math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)*oneWayEff/dt)))
							pBattCharge = 0.0
						} else {
							netLoad := homeKW - solarKW
							if netLoad > 0 {
								pBattDischarge = math.Min(netLoad, math.Min(maxDischargeKW, math.Max(0.0, (energy-minEnergy)*oneWayEff/dt)))
								pBattCharge = 0.0
							} else {
								surplusSolar := solarKW - homeKW
								pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/(dt*oneWayEff)))
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
								pBattCharge = math.Min(surplusSolar, math.Min(maxChargeKW, (capacityKWH-energy)/(dt*oneWayEff)))
							} else {
								pBattCharge = 0.0
							}
							pBattDischarge = 0.0
						}
					}

					energy = energy + (pBattCharge*oneWayEff-pBattDischarge/oneWayEff)*dt
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
						if settings.GridExportSolar || (settings.GridExportBatteries && decidedMode == types.BatteryModeExport) {
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
			t.Logf("%-20s | Cash: $%7.3f | SOC: %5.1f%% -> %5.1f%% (dE: %+6.2f kWh, AssetVal: $%+6.3f) | PlanAdjNetCost: $%7.3f | PlanBase: $%7.3f | DecideBase: $%7.3f | SavVsDecide: $%+6.3f (%5.1f%%) | Modes: %v | Reasons: %v | Cost: $%.2f Credit: $%.2f",
				fileName, simNetCost, initialSOC, simSOC, deltaEnergyKWH, batteryAssetValue, simAdjustedNetCost, baseNetCost, decideBaseCost, savingsVsDecide, pctSavingsVsDecide, modeCounts, reasonCounts, simCost, simCredit)

			mu.Lock()
			totalSimCost += simAdjustedNetCost
			totalBaselineCost += baseNetCost
			mu.Unlock()

			if hasBaseline {
				simAdjustedNetCostRounded := math.Round(simAdjustedNetCost*1000.0) / 1000.0
				allowedBuffer := 0.10
				assert.LessOrEqual(t, simAdjustedNetCostRounded, baseNetCost+allowedBuffer, "Plan simulated adjusted net cost should meet or beat baseline")
			}
		})
	}
}

// findDominatedPlanPeriods checks a generated Plan for dominated interval pairs:
//  1. HoldThenCheaperExport: Standby above reserve with net grid import at rate I, followed later by
//     BatteryModeExport at export rate E < I - epsilon while the battery remains above reserve in between.
//  2. ExportThenCostlierHold: BatteryModeExport at export rate E, followed later by Standby above reserve
//     with net grid import at rate I > E + epsilon before any recharge or solar refill to full.
func findDominatedPlanPeriods(plan types.Plan) []string {
	var violations []string
	periods := plan.Periods
	for i := 0; i < len(periods); i++ {
		p := periods[i]
		// 1. HoldThenCheaperExport
		if p.BatteryMode == types.BatteryModeStandby &&
			p.Reason != types.ActionReasonVPPPrep &&
			p.Reason != types.ActionReasonVPPActive &&
			p.StartSOC > p.ReserveSOC+1.0 &&
			p.LoadKWH > p.SolarKWH+0.05 &&
			p.GridImportKWH > 0.05 {
			for j := i + 1; j < len(periods); j++ {
				q := periods[j]
				if q.Reason == types.ActionReasonVPPPrep || q.Reason == types.ActionReasonVPPActive {
					break
				}
				if q.StartSOC <= q.ReserveSOC+1.0 || q.EndSOC <= q.ReserveSOC+1.0 || q.BatteryMode == types.BatteryModeChargeAny || q.EndSOC >= 95.0 {
					break
				}
				if q.BatteryMode == types.BatteryModeExport &&
					q.GridExportKWH > q.SolarKWH+0.05 &&
					q.ExportDollars < p.ImportDollars-dominatedPeriodPriceGapDollars {
					violations = append(violations, fmt.Sprintf(
						"HoldThenCheaperExport: Standby at %s (import $%.4f, SOC %.1f%%) before Export at %s (export $%.4f, EndSOC %.1f%%)",
						p.TSStart.Format("15:04"), p.ImportDollars, p.StartSOC,
						q.TSStart.Format("15:04"), q.ExportDollars, q.EndSOC,
					))
					break
				}
			}
		}

		// 2. ExportThenCostlierHold
		if p.BatteryMode == types.BatteryModeExport &&
			p.Reason != types.ActionReasonVPPActive &&
			p.GridExportKWH > p.SolarKWH+0.05 {
			for j := i + 1; j < len(periods); j++ {
				q := periods[j]
				if q.Reason == types.ActionReasonVPPPrep || q.Reason == types.ActionReasonVPPActive {
					break
				}
				if q.BatteryMode == types.BatteryModeChargeAny || q.EndSOC >= 95.0 {
					break
				}
				if q.BatteryMode == types.BatteryModeStandby &&
					q.StartSOC > q.ReserveSOC+1.0 &&
					q.LoadKWH > q.SolarKWH+0.05 &&
					q.GridImportKWH > 0.05 &&
					q.ImportDollars > p.ExportDollars+dominatedPeriodPriceGapDollars {
					violations = append(violations, fmt.Sprintf(
						"ExportThenCostlierHold: Export at %s (export $%.4f, EndSOC %.1f%%) before Standby at %s (import $%.4f, SOC %.1f%%)",
						p.TSStart.Format("15:04"), p.ExportDollars, p.EndSOC,
						q.TSStart.Format("15:04"), q.ImportDollars, q.StartSOC,
					))
					break
				}
			}
		}
	}
	return violations
}
