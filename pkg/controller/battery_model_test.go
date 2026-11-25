package controller

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeSyntheticDischargeBlock(
	start time.Time,
	hours int,
	capKWH, startSOC, loadKW, trueDisEff, trueFixedKW float64,
) []types.EnergyStats {
	out := make([]types.EnergyStats, 0, hours)
	soc := startSOC
	for h := 0; h < hours; h++ {
		deltaKWH := loadKW/trueDisEff + trueFixedKW
		endSOC := soc - (deltaKWH/capKWH)*100.0
		out = append(out, types.EnergyStats{
			TSHourStart:    start.Add(time.Duration(h) * time.Hour),
			MinBatterySOC:  endSOC,
			MaxBatterySOC:  soc,
			BatteryUsedKWH: loadKW,
			HomeKWH:        loadKW,
		})
		soc = endSOC
	}
	return out
}

func makeSyntheticChargeBlock(
	start time.Time,
	hours int,
	capKWH, startSOC, chargeKW, trueChgEff float64,
) []types.EnergyStats {
	out := make([]types.EnergyStats, 0, hours)
	soc := startSOC
	for h := 0; h < hours; h++ {
		deltaKWH := chargeKW * trueChgEff
		endSOC := soc + (deltaKWH/capKWH)*100.0
		out = append(out, types.EnergyStats{
			TSHourStart:       start.Add(time.Duration(h) * time.Hour),
			MinBatterySOC:     soc,
			MaxBatterySOC:     endSOC,
			BatteryChargedKWH: chargeKW,
			HomeKWH:           0.5,
		})
		soc = endSOC
	}
	return out
}

func TestBaselineBatteryModel(t *testing.T) {
	t.Run("ACDefault", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0, DCCoupled: false}
		m := BaselineBatteryModel(0.90, status)
		assert.Equal(t, "baseline", m.Source)
		assert.Equal(t, 30.0, m.EffectiveCapacityKWH)
		assert.InDelta(t, math.Sqrt(0.90), m.DischargeEff, 1e-9)
		assert.InDelta(t, math.Sqrt(0.90), m.ChargeEff, 1e-9)
		assert.Zero(t, m.DischargeFixedKW)
	})

	t.Run("DCCoupledFloor", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 13.5, DCCoupled: true}
		m := BaselineBatteryModel(0.90, status)
		assert.Equal(t, "baseline", m.Source)
		assert.InDelta(t, math.Sqrt(0.96), m.DischargeEff, 1e-9)
		assert.InDelta(t, math.Sqrt(0.96), m.ChargeEff, 1e-9)
	})

	t.Run("InvalidEfficiencyFallsBack", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 15.0}
		m := BaselineBatteryModel(math.NaN(), status)
		assert.InDelta(t, math.Sqrt(defaultRoundTripEfficiency), m.DischargeEff, 1e-9)
		assert.InDelta(t, math.Sqrt(defaultRoundTripEfficiency), m.ChargeEff, 1e-9)
	})

	t.Run("ZeroValueResolveEfficienciesFallback", func(t *testing.T) {
		var zero BatteryModel
		chg, dis, fixed := zero.resolveEfficiencies(0.90)
		assert.InDelta(t, math.Sqrt(0.90), chg, 1e-9)
		assert.InDelta(t, math.Sqrt(0.90), dis, 1e-9)
		assert.Zero(t, fixed)
	})
}

func TestLearnBatteryModel(t *testing.T) {
	ctx := context.Background()
	baseStart := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

	t.Run("EmptyAndSparseHistoryFallsBackToBaseline", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		mEmpty := LearnBatteryModel(ctx, nil, status, settings)
		assert.Equal(t, "baseline", mEmpty.Source)
		assert.Zero(t, mEmpty.DischargeSamples)
		assert.Zero(t, mEmpty.ChargeSamples)

		// Fewer than batteryModelMinSamples (10) blocks
		var sparse []types.EnergyStats
		for d := 0; d < 5; d++ {
			sparse = append(sparse, makeSyntheticDischargeBlock(
				baseStart.AddDate(0, 0, d).Add(12*time.Hour), 4, 30.0, 90.0, 1.0+0.3*float64(d), 0.85, 0.15,
			)...)
		}
		mSparse := LearnBatteryModel(ctx, sparse, status, settings)
		assert.Equal(t, "baseline", mSparse.Source)
		assert.Zero(t, mSparse.DischargeSamples)

		// Zero capacity guard
		mZeroCap := LearnBatteryModel(ctx, sparse, types.SystemStatus{BatteryCapacityKWH: 0}, settings)
		assert.Equal(t, "baseline", mZeroCap.Source)
	})

	t.Run("LearnsDischargeOverheadAndAsymmetricEfficiencies", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		const (
			trueDisEff  = 0.87
			trueFixedKW = 0.15
			trueChgEff  = 0.91
		)
		var history []types.EnergyStats
		for d := 0; d < 24; d++ {
			day := baseStart.AddDate(0, 0, d)
			// Varied discharge loads (0.5..3.5 kW) so slope and intercept separate cleanly
			disLoadKW := 0.5 + 0.5*float64(d%7)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, disLoadKW, trueDisEff, trueFixedKW,
			)...)
			// Varied charge rates (2.0..6.0 kW)
			chgKW := 2.0 + 0.8*float64(d%6)
			history = append(history, makeSyntheticChargeBlock(
				day.Add(1*time.Hour), 3, 30.0, 25.0, chgKW, trueChgEff,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "learned", m.Source)
		assert.GreaterOrEqual(t, m.DischargeSamples, 20)
		assert.GreaterOrEqual(t, m.ChargeSamples, 20)
		assert.InDelta(t, trueDisEff, m.DischargeEff, 0.03)
		assert.InDelta(t, trueFixedKW, m.DischargeFixedKW, 0.04)
		assert.InDelta(t, trueChgEff, m.ChargeEff, 0.03)
	})

	t.Run("LearnsOverProvisionedCapacityAboveOne", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 10.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		var history []types.EnergyStats
		for d := 0; d < 24; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 0.4 + 0.25*float64(d%6)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(14*time.Hour), 3, 10.0, 95.0, disLoadKW, 1.04, 0.0,
			)...)
			chgKW := 1.0 + 0.4*float64(d%5)
			history = append(history, makeSyntheticChargeBlock(
				day.Add(2*time.Hour), 2, 10.0, 25.0, chgKW, 1.03,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "learned", m.Source)
		assert.Greater(t, m.DischargeEff, 1.0)
		assert.LessOrEqual(t, m.DischargeEff, batteryModelMaxEffBound)
		assert.Greater(t, m.ChargeEff, 0.99)
		assert.LessOrEqual(t, m.ChargeEff, batteryModelMaxEffBound)
	})

	t.Run("IgnoresHeatingOutageEmergencyAndVPPFlags", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		var history []types.EnergyStats
		for d := 0; d < 18; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 0.6 + 0.4*float64(d%6)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, disLoadKW, 0.88, 0.14,
			)...)
		}
		// Add heavily corrupted blocks marked with operational anomaly flags
		flags := []func(*types.EnergyStats){
			func(h *types.EnergyStats) { h.HadBatteryHeating = true },
			func(h *types.EnergyStats) { h.HadGridUnavailable = true },
			func(h *types.EnergyStats) { h.HadEmergencyMode = true },
			func(h *types.EnergyStats) { h.HadVPPActive = true },
		}
		for i, setFlag := range flags {
			day := baseStart.AddDate(0, 0, 20+i)
			bad := makeSyntheticDischargeBlock(day.Add(12*time.Hour), 4, 30.0, 90.0, 1.0, 0.40, 2.5)
			for k := range bad {
				setFlag(&bad[k])
			}
			history = append(history, bad...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "blended", m.Source)
		assert.Equal(t, 18, m.DischargeSamples)
		assert.InDelta(t, 0.88, m.DischargeEff, 0.03)
		assert.InDelta(t, 0.14, m.DischargeFixedKW, 0.04)
	})

	t.Run("RobustAgainstCorruptedOutlierBlocks", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		var history []types.EnergyStats
		for d := 0; d < 20; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 0.5 + 0.5*float64(d%6)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, disLoadKW, 0.88, 0.12,
			)...)
		}
		// Inject 3 unflagged corrupted telemetry blocks (wild SOC jump)
		for i := 0; i < 3; i++ {
			day := baseStart.AddDate(0, 0, 21+i)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, 1.0, 0.35, 1.5,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "blended", m.Source)
		assert.Equal(t, 20, m.DischargeSamples, "MAD filter must drop the 3 corrupted outlier blocks")
		assert.InDelta(t, 0.88, m.DischargeEff, 0.03)
		assert.InDelta(t, 0.12, m.DischargeFixedKW, 0.04)
	})

	t.Run("RejectsNoisyLowR2Data", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		var history []types.EnergyStats
		// Uncorrelated SOC deltas relative to metered energy -> R² << 0.80
		effPattern := []float64{0.60, 1.25, 0.72, 1.18, 0.65, 1.22}
		for d := 0; d < 18; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 1.0 + 0.3*float64(d%5)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, disLoadKW, effPattern[d%len(effPattern)], 0.0,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "baseline", m.Source)
		assert.Zero(t, m.DischargeSamples)
	})

	t.Run("ClampsExtremeFixedOverheadToCapacityCap", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		// Synthetic 0.90 kW fixed overhead exceeds 1.5% * 30 kWh = 0.45 kW cap
		var history []types.EnergyStats
		for d := 0; d < 20; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 0.5 + 0.4*float64(d%6)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 3, 30.0, 92.0, disLoadKW, 0.92, 0.90,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "blended", m.Source)
		assert.LessOrEqual(t, m.DischargeFixedKW, batteryModelMaxFixedOverheadFraction*30.0+1e-9)
		assert.GreaterOrEqual(t, m.DischargeEff, batteryModelMinEffBound)
		assert.LessOrEqual(t, m.DischargeEff, batteryModelMaxEffBound)
	})

	t.Run("UnsortedHistoryProducesIdenticalModel", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		var history []types.EnergyStats
		for d := 0; d < 18; d++ {
			day := baseStart.AddDate(0, 0, d)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 4, 30.0, 90.0, 0.5+0.4*float64(d%6), 0.87, 0.14,
			)...)
		}
		reversed := slices.Clone(history)
		slices.Reverse(reversed)

		m1 := LearnBatteryModel(ctx, history, status, settings)
		m2 := LearnBatteryModel(ctx, reversed, status, settings)
		assert.Equal(t, m1, m2)
	})

	t.Run("BoundaryExclusionNearReserveAndFullSOC", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 35.0}

		// Discharge blocks that touch 35.5% (<= minReserveSOC+1.0 = 36%) must be excluded
		var history []types.EnergyStats
		for d := 0; d < 16; d++ {
			day := baseStart.AddDate(0, 0, d)
			history = append(history, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 3, 30.0, 42.0, 1.0, 0.85, 0.15,
			)...)
		}
		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "baseline", m.Source)
		assert.Zero(t, m.DischargeSamples)
	})

	t.Run("ChargeOnlyBlendedOutliersAndBoundaryExclusion", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		// 18 clean charge blocks (true ChargeEff = 0.90) + 3 corrupted charge outlier blocks.
		var history []types.EnergyStats
		for d := 0; d < 18; d++ {
			day := baseStart.AddDate(0, 0, d)
			chgKW := 2.0 + 0.8*float64(d%6)
			history = append(history, makeSyntheticChargeBlock(
				day.Add(1*time.Hour), 3, 30.0, 25.0, chgKW, 0.90,
			)...)
		}
		for i := 0; i < 3; i++ {
			day := baseStart.AddDate(0, 0, 20+i)
			history = append(history, makeSyntheticChargeBlock(
				day.Add(1*time.Hour), 3, 30.0, 25.0, 3.0, 0.35,
			)...)
		}

		m := LearnBatteryModel(ctx, history, status, settings)
		assert.Equal(t, "blended", m.Source)
		assert.Zero(t, m.DischargeSamples)
		assert.Equal(t, 18, m.ChargeSamples, "MAD filter must drop the 3 corrupted charge outlier blocks")
		assert.InDelta(t, 0.90, m.ChargeEff, 0.03)
		assert.InDelta(t, math.Sqrt(defaultRoundTripEfficiency), m.DischargeEff, 1e-9, "discharge must stay at baseline")

		// Noisy low-R² charge data must fall back to baseline.
		var noisyChg []types.EnergyStats
		effPattern := []float64{0.55, 1.30, 0.65, 1.25, 0.60, 1.28}
		for d := 0; d < 18; d++ {
			day := baseStart.AddDate(0, 0, d)
			chgKW := 2.0 + 0.5*float64(d%5)
			noisyChg = append(noisyChg, makeSyntheticChargeBlock(
				day.Add(1*time.Hour), 3, 30.0, 25.0, chgKW, effPattern[d%len(effPattern)],
			)...)
		}
		mNoisy := LearnBatteryModel(ctx, noisyChg, status, settings)
		assert.Equal(t, "baseline", mNoisy.Source)
		assert.Zero(t, mNoisy.ChargeSamples)

		// Charge blocks reaching MaxBatterySOC >= 99.0% must be excluded.
		var nearFullChg []types.EnergyStats
		for d := 0; d < 16; d++ {
			day := baseStart.AddDate(0, 0, d)
			nearFullChg = append(nearFullChg, makeSyntheticChargeBlock(
				day.Add(1*time.Hour), 2, 30.0, 96.0, 1.5, 0.85,
			)...)
		}
		mNearFull := LearnBatteryModel(ctx, nearFullChg, status, settings)
		assert.Equal(t, "baseline", mNearFull.Source)
		assert.Zero(t, mNearFull.ChargeSamples)
	})

	t.Run("NegativeInterceptClampedOrRejected", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		// Slight negative intercept (-0.12 kW, within [-0.01*30 = -0.30, 0)) must clamp to 0 and re-solve slope.
		var slightNeg []types.EnergyStats
		for d := 0; d < 20; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 1.0 + 0.5*float64(d%6)
			slightNeg = append(slightNeg, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 3, 30.0, 90.0, disLoadKW, 0.90, -0.12,
			)...)
		}
		mClamped := LearnBatteryModel(ctx, slightNeg, status, settings)
		assert.Equal(t, "blended", mClamped.Source)
		assert.Zero(t, mClamped.DischargeFixedKW, "small negative intercept must clamp to 0")
		assert.GreaterOrEqual(t, mClamped.DischargeEff, batteryModelMinEffBound)
		assert.LessOrEqual(t, mClamped.DischargeEff, batteryModelMaxEffBound)

		// Strongly negative intercept (-0.60 kW < -0.01*30 = -0.30 kW) must reject the fit and fall back to baseline.
		var strongNeg []types.EnergyStats
		for d := 0; d < 20; d++ {
			day := baseStart.AddDate(0, 0, d)
			disLoadKW := 1.5 + 0.5*float64(d%6)
			strongNeg = append(strongNeg, makeSyntheticDischargeBlock(
				day.Add(12*time.Hour), 3, 30.0, 90.0, disLoadKW, 0.85, -0.60,
			)...)
		}
		mRejected := LearnBatteryModel(ctx, strongNeg, status, settings)
		assert.Equal(t, "baseline", mRejected.Source)
		assert.Zero(t, mRejected.DischargeSamples)
	})

	t.Run("IsolatedSingleHoursIgnoredAndLongRunsSplit", func(t *testing.T) {
		status := types.SystemStatus{BatteryCapacityKWH: 30.0}
		settings := types.Settings{MinBatterySOC: 20.0}

		// 18 isolated 1-hour discharge records separated by 2-hour gaps -> 0 multi-hour blocks -> baseline.
		var isolated []types.EnergyStats
		for i := 0; i < 18; i++ {
			isolated = append(isolated, makeSyntheticDischargeBlock(
				baseStart.Add(time.Duration(i*3)*time.Hour), 1, 30.0, 90.0, 1.0+0.3*float64(i%5), 0.85, 0.15,
			)...)
		}
		mIso := LearnBatteryModel(ctx, isolated, status, settings)
		assert.Equal(t, "baseline", mIso.Source)
		assert.Zero(t, mIso.DischargeSamples)

		// 6 contiguous 10-hour runs split at batteryModelMaxBlockHours (6h + 4h = 2 blocks/run = 12 blocks >= 10).
		var longRuns []types.EnergyStats
		for d := 0; d < 6; d++ {
			day := baseStart.AddDate(0, 0, d)
			longRuns = append(longRuns, makeSyntheticDischargeBlock(
				day.Add(10*time.Hour), 10, 30.0, 95.0, 0.4+0.2*float64(d), 0.88, 0.12,
			)...)
		}
		mSplit := LearnBatteryModel(ctx, longRuns, status, settings)
		assert.Equal(t, "blended", mSplit.Source)
		assert.Equal(t, 12, mSplit.DischargeSamples, "six 10-hour runs must split into twelve 6h+4h blocks")
	})
}

func makeWeatherDay(day time.Time, tempC float64) types.Weather {
	hrs := make([]types.HourlyWeather, 24)
	for h := 0; h < 24; h++ {
		hrs[h] = types.HourlyWeather{
			TSHourStart:  day.Add(time.Duration(h) * time.Hour),
			TemperatureC: tempC,
		}
	}
	return types.Weather{TSDayStart: day, ForecastHours: hrs}
}

func TestLearnBatteryHeaterModel(t *testing.T) {
	ctx := context.Background()
	baseDay := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	status30 := types.SystemStatus{BatteryCapacityKWH: 30.0}
	settings := types.Settings{MinBatterySOC: 20.0}
	bm30 := BaselineBatteryModel(0.90, status30)

	t.Run("NoFlaggedHoursAndIndoorColdSiteRemainUndetected", func(t *testing.T) {
		var history []types.EnergyStats
		var weather []types.Weather
		for d := 0; d < 10; d++ {
			day := baseDay.AddDate(0, 0, d)
			weather = append(weather, makeWeatherDay(day, -3.0))
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart:    day.Add(time.Duration(h) * time.Hour),
					MinBatterySOC:  60.0,
					MaxBatterySOC:  62.0,
					BatteryUsedKWH: 0.6,
				})
			}
		}
		hm := LearnBatteryHeaterModel(ctx, history, bm30, weather, settings)
		assert.False(t, hm.Detected)
		assert.Zero(t, hm.FlaggedHours)
		assert.Zero(t, hm.PeakPowerKW)
	})

	t.Run("SingleFlaggedHourBelowThresholdRemainsUndetected", func(t *testing.T) {
		history := []types.EnergyStats{
			{
				TSHourStart:       baseDay.Add(7 * time.Hour),
				MinBatterySOC:     55.0,
				MaxBatterySOC:     60.0,
				BatteryUsedKWH:    0.6,
				HadBatteryHeating: true,
			},
		}
		hm := LearnBatteryHeaterModel(ctx, history, bm30, []types.Weather{makeWeatherDay(baseDay, 1.0)}, settings)
		assert.False(t, hm.Detected)
		assert.Equal(t, 1, hm.FlaggedHours)
	})

	t.Run("LearnsTriggerTempAndShrunkPeakPowerFromFlaggedHours", func(t *testing.T) {
		// 6 flagged cold discharge hours with ~0.78 kW extra drain on a 30 kWh battery (2 units * ~390 W).
		var history []types.EnergyStats
		var weather []types.Weather
		temps := []float64{-1.0, 0.5, 1.5, 2.5, 4.0, 5.0}
		for i, tempC := range temps {
			day := baseDay.AddDate(0, 0, i)
			weather = append(weather, makeWeatherDay(day, tempC))
			usedKWH := 0.80
			expectedBaseDropKWH := usedKWH / bm30.DischargeEff
			actualDropKWH := expectedBaseDropKWH + 0.78
			endSOC := 70.0 - (actualDropKWH/30.0)*100.0
			history = append(history, types.EnergyStats{
				TSHourStart:       day.Add(7 * time.Hour),
				MinBatterySOC:     endSOC,
				MaxBatterySOC:     70.0,
				BatteryUsedKWH:    usedKWH,
				HadBatteryHeating: true,
			})
		}

		hm := LearnBatteryHeaterModel(ctx, history, bm30, weather, settings)
		assert.True(t, hm.Detected)
		assert.Equal(t, 6, hm.FlaggedHours)
		// P85 of [-1, 0.5, 1.5, 2.5, 4, 5] is 4.25 -> +1.0 margin = 5.25°C.
		assert.InDelta(t, 5.25, hm.TriggerTempC, 0.05)
		assert.InDelta(t, 0.78, hm.PeakPowerKW, 0.05)
	})

	t.Run("SmallNTwoFlaggedHoursInterpolatesPercentile", func(t *testing.T) {
		// With exactly 2 flagged hours (1.0°C and 5.0°C), linear interpolation at P85 gives 4.4°C (+1 = 5.4°C),
		// not the minimum 1.0°C (+1 = 2.0°C).
		day0 := baseDay
		day1 := baseDay.AddDate(0, 0, 1)
		weather := []types.Weather{makeWeatherDay(day0, 1.0), makeWeatherDay(day1, 5.0)}
		history := []types.EnergyStats{
			{TSHourStart: day0.Add(7 * time.Hour), MinBatterySOC: 50, MaxBatterySOC: 53, BatteryUsedKWH: 0.0, HadBatteryHeating: true},
			{TSHourStart: day1.Add(7 * time.Hour), MinBatterySOC: 50, MaxBatterySOC: 54, BatteryUsedKWH: 0.0, HadBatteryHeating: true},
		}
		hm := LearnBatteryHeaterModel(ctx, history, bm30, weather, settings)
		assert.True(t, hm.Detected)
		assert.InDelta(t, 5.4, hm.TriggerTempC, 0.05, "n=2 P85 must linearly interpolate rather than returning minimum")
	})

	t.Run("StandbyFlaggedHoursAndAnomalyFiltering", func(t *testing.T) {
		// Standby flagged hours (BatteryUsedKWH < 0.10, no solar-to-battery) contribute power samples,
		// while outage/emergency/VPP/solar-charging hours are excluded.
		day0 := baseDay
		day1 := baseDay.AddDate(0, 0, 1)
		day2 := baseDay.AddDate(0, 0, 2)
		weather := []types.Weather{makeWeatherDay(day0, 2.0), makeWeatherDay(day1, 3.0), makeWeatherDay(day2, 2.0)}
		history := []types.EnergyStats{
			// Clean standby flagged hours: 3% SOC drop on 30 kWh = 0.90 kW
			{TSHourStart: day0.Add(7 * time.Hour), MinBatterySOC: 57, MaxBatterySOC: 60, HadBatteryHeating: true},
			{TSHourStart: day1.Add(7 * time.Hour), MinBatterySOC: 57, MaxBatterySOC: 60, HadBatteryHeating: true},
			// SolarToBatteryKWH >= 0.02 -> excluded from power samples (still counted as flagged hour)
			{TSHourStart: day2.Add(7 * time.Hour), MinBatterySOC: 50, MaxBatterySOC: 60, SolarToBatteryKWH: 0.5, HadBatteryHeating: true},
			// Outage / Emergency / VPP -> skipped completely
			{TSHourStart: day2.Add(8 * time.Hour), MinBatterySOC: 30, MaxBatterySOC: 60, HadBatteryHeating: true, HadGridUnavailable: true},
			{TSHourStart: day2.Add(9 * time.Hour), MinBatterySOC: 30, MaxBatterySOC: 60, HadBatteryHeating: true, HadEmergencyMode: true},
			{TSHourStart: day2.Add(10 * time.Hour), MinBatterySOC: 30, MaxBatterySOC: 60, HadBatteryHeating: true, HadVPPActive: true},
		}
		hm := LearnBatteryHeaterModel(ctx, history, bm30, weather, settings)
		assert.True(t, hm.Detected)
		assert.Equal(t, 3, hm.FlaggedHours, "outage/emergency/VPP hours must be skipped")
		assert.InDelta(t, (2*0.90+3*0.78)/5.0, hm.PeakPowerKW, 0.02)
	})

	t.Run("InvalidCapacityAndNaNWeatherGuards", func(t *testing.T) {
		badBM := BatteryModel{EffectiveCapacityKWH: math.NaN()}
		hm := LearnBatteryHeaterModel(ctx, []types.EnergyStats{{HadBatteryHeating: true}, {HadBatteryHeating: true}}, badBM, nil, settings)
		assert.False(t, hm.Detected)

		// NaN weather temperatures fall back to heaterDefaultTriggerTempC (5.0°C)
		nanWeather := []types.Weather{makeWeatherDay(baseDay, math.NaN()), makeWeatherDay(baseDay.AddDate(0, 0, 1), math.Inf(1))}
		hist := []types.EnergyStats{
			{TSHourStart: baseDay.Add(7 * time.Hour), HadBatteryHeating: true},
			{TSHourStart: baseDay.AddDate(0, 0, 1).Add(7 * time.Hour), HadBatteryHeating: true},
		}
		hmNaN := LearnBatteryHeaterModel(ctx, hist, bm30, nanWeather, settings)
		assert.True(t, hmNaN.Detected)
		assert.Equal(t, batteryHeaterDefaultTriggerTempC, hmNaN.TriggerTempC)
		assert.InDelta(t, batteryHeaterPeakPriorCapFrac*30.0, hmNaN.PeakPowerKW, 1e-6)
	})
}

func TestHeaterPowerKW(t *testing.T) {
	hm := BatteryHeaterModel{
		Detected:     true,
		TriggerTempC: 5.0,
		PeakPowerKW:  0.80,
		FlaggedHours: 10,
	}

	t.Run("NoDoubleFiringWhenActiveHeatingStartsAt0700", func(t *testing.T) {
		loc := time.UTC
		t0 := time.Date(2026, 11, 20, 7, 0, 0, 0, loc)
		var st BatteryHeaterState
		totalEnergyKWH := 0.0
		stepHrs := 1.0 / 3.0 // 20-minute intervals

		// 6 consecutive 20-min steps (07:00..09:00) with initialHeating=true and temp=1°C (< 5°C).
		// Must consume exactly 0.50h * 0.80 kW = 0.40 kWh total, NOT double-fire a second burst!
		for i := 0; i < 6; i++ {
			ts := t0.Add(time.Duration(i*20) * time.Minute)
			var kw float64
			kw, st = hm.HeaterPowerKW(ts, stepHrs, float64(i)*stepHrs, true, 1.0, true, st)
			totalEnergyKWH += kw * stepHrs
			switch i {
			case 0:
				assert.InDelta(t, 0.80, kw, 1e-9, "step 0 (0..20m) gets full peak power")
			case 1:
				assert.InDelta(t, 0.40, kw, 1e-9, "step 1 (20..40m) gets prorated remaining 10m = 0.5 * peak")
			default:
				assert.Zero(t, kw, "steps >= 2 must be 0 kW (no second morning burst)")
			}
		}
		assert.InDelta(t, 0.40, totalEnergyKWH, 1e-9)
	})

	t.Run("NonUTCTimezoneUsesLocalMorningWindow", func(t *testing.T) {
		chicago, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// 07:00 UTC is 01:00 CST (not morning) -> must NOT trigger morning burst.
		t1amCST := time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC).In(chicago)
		kw1, st := hm.HeaterPowerKW(t1amCST, 1.0/3.0, 2.0, false, 0.0, true, BatteryHeaterState{})
		assert.Zero(t, kw1)

		// 13:00 UTC is 07:00 CST (morning) -> MUST trigger morning burst.
		t7amCST := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC).In(chicago)
		kw7, _ := hm.HeaterPowerKW(t7amCST, 1.0/3.0, 8.0, false, 0.0, true, st)
		assert.InDelta(t, 0.80, kw7, 1e-9)
	})

	t.Run("MultiDayResetAcrossCalendarDays", func(t *testing.T) {
		day1 := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
		day2 := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)

		// Exhaust Day 1's 0.50h burst
		kw1, st := hm.HeaterPowerKW(day1, 0.50, 1.0, false, 0.0, true, BatteryHeaterState{})
		assert.InDelta(t, 0.80, kw1, 1e-9)
		kw1After, st := hm.HeaterPowerKW(day1.Add(30*time.Minute), 0.50, 1.5, false, 0.0, true, st)
		assert.Zero(t, kw1After)

		// Day 2 morning resets budget and fires again
		kw2, _ := hm.HeaterPowerKW(day2, 0.50, 25.0, false, 0.0, true, st)
		assert.InDelta(t, 0.80, kw2, 1e-9, "morning burst budget must reset on Day 2")
	})

	t.Run("WarmTempMissingTempAndInvalidDurationGuards", func(t *testing.T) {
		tMorning := time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC)

		// Warm temp (>= TriggerTempC) -> 0
		kwWarm, _ := hm.HeaterPowerKW(tMorning, 1.0/3.0, 1.0, false, 10.0, true, BatteryHeaterState{})
		assert.Zero(t, kwWarm)

		// Missing temp -> morning burst is 0, but active initialHeating still works
		kwNoTemp, _ := hm.HeaterPowerKW(tMorning, 1.0/3.0, 1.0, false, 0.0, false, BatteryHeaterState{})
		assert.Zero(t, kwNoTemp)
		kwActiveNoTemp, _ := hm.HeaterPowerKW(tMorning, 1.0/3.0, 0.0, true, 0.0, false, BatteryHeaterState{})
		assert.InDelta(t, 0.80, kwActiveNoTemp, 1e-9)

		// NaN temp / zero duration / NaN duration -> 0
		kwNaNTemp, _ := hm.HeaterPowerKW(tMorning, 1.0/3.0, 1.0, false, math.NaN(), true, BatteryHeaterState{})
		assert.Zero(t, kwNaNTemp)
		kwZeroDur, _ := hm.HeaterPowerKW(tMorning, 0.0, 0.0, true, 0.0, true, BatteryHeaterState{})
		assert.Zero(t, kwZeroDur)
		kwNaNDur, _ := hm.HeaterPowerKW(tMorning, math.NaN(), 0.0, true, 0.0, true, BatteryHeaterState{})
		assert.Zero(t, kwNaNDur)
	})
}
