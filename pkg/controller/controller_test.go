package controller

import (
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestDecide(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	baseSettings := types.Settings{
		MinBatterySOC:                       20.0,
		AlwaysChargeUnderDollarsPerKWH:      -0.01,
		GridChargeBatteries:                 true,
		GridExportSolar:                     true,
		MinArbitrageDifferenceDollarsPerKWH: 0.01,
		SolarTrendRatioMax:                  3.0,
		SolarBellCurveMultiplier:            1.0,
	}

	now := time.Now().Truncate(time.Hour)
	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	// Create dummy history for 1kW load constant
	history := []types.EnergyStats{}
	// Create no load history
	noLoadHistory := []types.EnergyStats{}

	// Generate history covering all hours
	ts := now.Add(-24 * time.Hour)
	for i := 0; i < 48; i++ { // 2 days
		history = append(history, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  1.0,
			SolarKWH:       0.0,
			BatteryUsedKWH: 0.0,
			HomeKWH:        1.0,
		})
		noLoadHistory = append(noLoadHistory, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  0.0,
			SolarKWH:       0.0,
			BatteryUsedKWH: 0.0,
			HomeKWH:        0.0,
		})
		ts = ts.Add(1 * time.Hour)
	}

	t.Run("Negative Price without Net Metering -> Charge, No Export Solar", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.01}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeNoExport, decision.Action.SolarMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
	})

	t.Run("Negative Price with Net Metering -> Charge, Export Solar", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.01}
		settings := baseSettings
		settings.UtilityRateOptions.NetMeteringCredits = true
		settings.SolarNetMeteringCreditsValue = "highest"
		// future price 0.20
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.20},
		}

		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode, "Solar export should NOT be disabled when net metering is on")
		// The description should NOT mention export disabled due to negative price.
		assert.NotContains(t, decision.Action.Description, "Export Disabled due to Negative Price")
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
	})

	t.Run("Low Price -> Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.00, GridUseDollarsPerKWH: -0.01}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.TargetBatteryMode)
		assert.NotEqual(t, types.SolarModeNoChange, decision.Action.TargetSolarMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
		assert.Equal(t, baseStatus.BatterySOC, decision.Action.SystemStatus.BatterySOC)
	})

	t.Run("High Price Now -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		// Provide cheap power for next 24 hours to ensure we definitely wait
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.04, GridUseDollarsPerKWH: 0.04,
			})
		}

		// min battery soc to elevated to pretend we're in standby
		status := baseStatus
		status.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		// Should Load (Use battery now because current price is high vs future low)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
	})

	t.Run("Low Battery + High Price -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		// Future is cheap for long time
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.04, GridUseDollarsPerKWH: 0.04,
			})
		}

		// Battery 30% needs charging to cover load. Cheap now.
		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 30.0
		lowBattStatus.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, decision)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
	})

	t.Run("Deficit detected -> Charge Now (Cheapest Option)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		// Future is expensive!
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.50,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 20.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.TargetBatteryMode)
		assert.Contains(t, decision.Action.Description, "Projected Deficit")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set for deficit charge")
		assert.NotZero(t, decision.Action.FuturePrice.DollarsPerKWH, "FuturePrice should be set for deficit charge")
	})

	t.Run("Peak Survival -> Already Hit Capacity Before Peak", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		// First 5 hours are low price, then high price
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i >= 5 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is low, but solar is going to charge it up to 100% before the peak
		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 20.0
		lowBattStatus.SolarKW = 10.0 // huge solar, will fill battery quickly
		lowBattStatus.HomeKW = 1.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		// It should NOT charge now because we're going to hit capacity anyway
		assert.NotEqual(t, types.ActionReasonChargeSurvivePeak, decision.Action.Reason)
	})

	t.Run("Deficit detected -> Charge Later due to MinDeficitPriceDifference", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		// Future is expensive, but difference is small
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.12,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 21.0
		// pretend we're charging from the grid now
		lowBattStatus.GridKW = 2.0
		lowBattStatus.BatteryKW = -1.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.05 // Require 5 cents diff
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.10    // High arbitrage threshold to avoid interference

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		// Should not charge now, so it should be Standby
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Deficit predicted")
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Action.Reason)
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
	})

	t.Run("Battery At Reserve -> Below Min SOC", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05},
		}

		status := baseStatus
		status.BatterySOC = 19.9
		status.BatteryAboveMinSOC = false
		status.HomeKW = 1.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "at reserve")
	})

	t.Run("Battery At Reserve -> Deficit within 5 minutes", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05},
		}

		status := baseStatus
		// 10kWh capacity, 20% min = 2kWh reserve.
		// 20.01% = 2.001kWh. 0.001kWh above reserve.
		// 1kW load = 0.001 hour = 0.06 minutes.
		status.BatterySOC = 20.01
		status.BatteryAboveMinSOC = true
		status.HomeKW = 1.0
		status.SolarKW = 0.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "at reserve")
	})

	t.Run("Not Battery At Reserve -> Deficit at 10 minutes", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05},
		}

		status := baseStatus
		// 10kWh capacity, 20% min = 2kWh reserve.
		// 1kW load.
		// We want hit at 10 minutes = 1/6 hour = 0.1666 kWh above reserve.
		// SOC = 20% + (0.1666 / 10 * 100)% = 21.666%
		status.BatterySOC = 21.666
		status.BatteryAboveMinSOC = true
		status.HomeKW = 1.0
		status.SolarKW = 0.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		// Should NOT be BatteryAtReserve (10 mins > 5 mins)
		assert.NotEqual(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
	})

	t.Run("Battery SOC < Reserve but ElevatedMinBatterySOC -> No BatteryAtReserve Trigger", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 50.0
		// ElevatedMinBatterySOC is true, representing e.g. a charge-hold state.
		// Franklin driver set BatteryAboveMinSOC to false because it's < 100%.
		status.BatteryAboveMinSOC = false
		status.ElevatedMinBatterySOC = true
		status.HomeKW = 1.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		// It should NOT trigger Battery At Reserve.
		// Instead, it should trigger Arbitrage Charge (Rule 3)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.NotEqual(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.Equal(t, types.ActionReasonArbitrageChargeSave, decision.Action.Reason)
	})

	t.Run("Deficit detected -> Charge Now (Absolute Cheapest Is Now)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05} // ultra cheap right now
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10 // more expensive later
			if i == 5 {
				price = 0.50 // huge peak
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 35.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.01 // Requires saving 0.01, but we're cheapest now

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		// It should charge NOW because it's cheaper now than any future time before deficit
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Projected Deficit")
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
	})

	t.Run("Deficit detected -> Delay Charge (Future is equally cheap)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.05 // same as now
			if i >= 20 {
				price = 0.50 // huge peak later
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 35.0 // Need some charge but not a massive amount
		lowBattStatus.GridKW = 2.0
		lowBattStatus.BatteryKW = 1.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.01
		settings.MinArbitrageDifferenceDollarsPerKWH = 2.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		// It should DELAY because future has equally cheap hours before the spike!
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Waiting to charge")
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
	})

	t.Run("Waiting To Charge (Charge Before Peak)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			if i == 2 {
				price = 0.05 // Cheap charge time (before peak)
			} else if i == 6 {
				price = 0.50 // Peak price
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 80.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.GridKW = 2.0
		lowBattStatus.BatteryKW = -1.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.01
		settings.MinArbitrageDifferenceDollarsPerKWH = 2.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Waiting to charge")
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
	})

	t.Run("Deficit Save For Peak (Peak Before Charge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			if i == 2 {
				price = 0.50 // Peak price (before charge)
			} else if i == 6 {
				price = 0.05 // Cheap charge time
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 80.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.GridKW = 2.0
		lowBattStatus.BatteryKW = -1.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.01
		settings.MinArbitrageDifferenceDollarsPerKWH = 2.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Deficit predicted")
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
	})

	t.Run("Arbitrage Opportunity -> Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Use Default Status (50%). No immediate deficit.
		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, noLoadHistory, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.TargetBatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, decision.Action.Reason)
		assert.Equal(t, 0.50, decision.Action.FuturePrice.DollarsPerKWH, "FuturePrice should be the peak future price")
		assert.Equal(t, baseStatus.BatterySOC, decision.Action.SystemStatus.BatterySOC)
	})

	t.Run("Arbitrage Hold (Battery Full) -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		fullStatus := baseStatus
		fullStatus.BatterySOC = 99.0

		decision, err := c.Decide(ctx, fullStatus, currentPrice, futurePrices, noLoadHistory, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.TargetBatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Action.Reason)
		if assert.NotNil(t, decision.Action.FuturePrice) {
			assert.Equal(t, 0.50, decision.Action.FuturePrice.DollarsPerKWH, "FuturePrice should be the peak future price")
		}
	})

	t.Run("Arbitrage Constraint -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			if i <= 5 {
				price = 0.05 // Cheap to delay deficit charge
			} else if i == 6 {
				price = 0.50 // High but blocked by constraint
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.40
		// Arbitrage: 0.50 - 0.20 = 0.30 < 0.40. No Charge.
		// Deficit: ChargeNow(0.20) > Future(0.05). No Charge.

		status := baseStatus
		status.BatteryKW = 1.0 // Force discharge

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		// Deficit (History) + High Future Price -> Standby (Save)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})

	t.Run("Arbitrage Hold (No Grid Charge) -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		noGridChargeSettings := baseSettings
		noGridChargeSettings.GridChargeBatteries = false

		status := baseStatus
		// SOC is sufficient but we can't charge from grid.
		// There's an arbitrage opportunity (Save) because we have home load.

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, noGridChargeSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldSave, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Grid charging disabled")
	})

	t.Run("Battery Charging Disabled -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		status := baseStatus
		status.BatteryChargingDisabled = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldSave, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Charging disabled")
	})

	t.Run("Zero Capacity -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}

		zeroCapStatus := baseStatus
		zeroCapStatus.BatteryCapacityKWH = 0
		zeroCapStatus.BatteryKW = 1.0 // Force discharge

		decision, err := c.Decide(ctx, zeroCapStatus, currentPrice, nil, noLoadHistory, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Capacity 0")
		assert.Equal(t, types.ActionReasonMissingBattery, decision.Action.Reason)
	})

	t.Run("Default to Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		// No Future Prices (Flat).

		status := baseStatus
		status.BatteryKW = 1.0 // Force discharge

		// Use No Load History to avoid Deficit
		decision, err := c.Decide(ctx, status, currentPrice, nil, noLoadHistory, nil, baseSettings)
		require.NoError(t, err)

		// No deficit, default to Load
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Sufficient Battery + Moderate Price -> Load", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		// Flat prices
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10,
			})
		}

		// Sufficient Battery:
		// Low Load History (0.1kW * 24 = 2.4kWh needed).
		// Base Status has 5kWh capacity? No, Base Status has 10kWh cap, 50% SOC = 5kWh available.
		// 5kWh > 2.4kWh. No deficit.

		lowLoadHistory := []types.EnergyStats{}
		for i := 0; i < 48; i++ {
			lowLoadHistory = append(lowLoadHistory, types.EnergyStats{
				TSHourStart:   now.Add(time.Duration(i-48) * time.Hour),
				HomeKWH:       0.1,
				GridImportKWH: 0.1,
			})
		}

		// pretend we're charging
		elevatedSOCStatus := baseStatus
		elevatedSOCStatus.ElevatedMinBatterySOC = true
		decision, err := c.Decide(ctx, elevatedSOCStatus, currentPrice, futurePrices, lowLoadHistory, nil, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Sufficient battery")
		assert.Equal(t, types.ActionReasonSufficientBattery, decision.Action.Reason)
		assert.Equal(t, elevatedSOCStatus.BatterySOC, decision.Action.SystemStatus.BatterySOC)
		assert.True(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be zero for sufficient battery")
		assert.Zero(t, decision.Action.FuturePrice, "FuturePrice should be zero for sufficient battery")
	})

	t.Run("Deficit + Moderate Price + High Future Price -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 5 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Use No Grid Charge settings to test Standby/Load logic without charging triggers
		noGridSettings := baseSettings
		noGridSettings.GridChargeBatteries = false
		noGridSettings.MinArbitrageDifferenceDollarsPerKWH = 2.0

		usingBatteryStatus := baseStatus
		usingBatteryStatus.BatteryKW = 1.0

		// Available 5kWh. Deficit!
		decision, err := c.Decide(ctx, usingBatteryStatus, currentPrice, futurePrices, history, nil, noGridSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Deficit predicted")
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Action.Reason)
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
		if assert.NotNil(t, decision.Action.FuturePrice) {
			assert.Equal(t, 0.50, decision.Action.FuturePrice.DollarsPerKWH)
		}
	})

	t.Run("Deficit + High Price (Peak) -> Load", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.50}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.50
			if i >= 5 {
				price = 0.10
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Use No Grid Charge settings to test Peak Load logic without charging triggers
		noGridSettings := baseSettings
		noGridSettings.GridChargeBatteries = false

		// pretend we're charging
		elevatedSOCStatus := baseStatus
		elevatedSOCStatus.ElevatedMinBatterySOC = true
		decision, err := c.Decide(ctx, elevatedSOCStatus, currentPrice, futurePrices, history, nil, noGridSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Deficit predicted but Current Price is Peak")
		assert.Equal(t, types.ActionReasonArbitrageSave, decision.Action.Reason)
		assert.False(t, decision.Action.HitDeficitAt.IsZero(), "HitDeficitAt should be set")
		assert.Zero(t, decision.Action.FuturePrice, "FuturePrice should be zero for peak discharge")
	})

	t.Run("ExplicitModes", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		baseSettings := types.Settings{
			MinBatterySOC: 20.0,
		}
		now := time.Now().Truncate(time.Hour)
		baseStatus := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         50.0,
			BatteryCapacityKWH: 10.0,
			BatteryKW:          0.0,
			SolarKW:            0.0,
			HomeKW:             1.0,
			BatteryAboveMinSOC: true,
		}
		// Normal prices, no charge triggers
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		history := []types.EnergyStats{}

		t.Run("Already Charging -> NoOverride", func(t *testing.T) {
			// Setup scenario where it SHOULD charge (Very low price)
			cheapPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.05, GridUseDollarsPerKWH: -0.05} // Neg price charges always

			status := baseStatus
			status.BatteryKW = -5.0             // Already Charging
			status.ElevatedMinBatterySOC = true // Previously this would trigger NoChange optimization

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.TargetBatteryMode)
		})

		t.Run("Already Charging (Not Elevated) -> ChargeAny", func(t *testing.T) {
			// Setup scenario where it SHOULD charge (Very low price)
			cheapPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.05, GridUseDollarsPerKWH: -0.05} // Neg price charges always

			status := baseStatus
			status.BatteryKW = -5.0              // Already Charging
			status.ElevatedMinBatterySOC = false // Not elevated means we need to reissue command

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		})

		t.Run("Battery Full -> NoOverride", func(t *testing.T) {
			cheapPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.05, GridUseDollarsPerKWH: -0.05}

			status := baseStatus
			status.BatterySOC = 100.0
			status.ElevatedMinBatterySOC = true

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.TargetBatteryMode)
		})

		t.Run("Battery Full (Not Elevated) -> ChargeAny", func(t *testing.T) {
			cheapPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.05, GridUseDollarsPerKWH: -0.05}

			status := baseStatus
			status.BatterySOC = 100.0
			status.ElevatedMinBatterySOC = false

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		})

		t.Run("Standby Logic: Discharging -> Load", func(t *testing.T) {
			status := baseStatus
			status.BatteryKW = 2.0 // Discharging

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			// Discharging (-2.0) -> Load (Allow Discharge)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
		})

		t.Run("Standby Logic: Charging from Grid -> Load", func(t *testing.T) {
			status := baseStatus
			// Battery charging at 3kW
			status.BatteryKW = -3.0
			// Solar 1kW, Home 1kW -> Surplus 0kW
			status.SolarKW = 1.0
			status.HomeKW = 1.0
			// Grid Import 3kW (used for battery)
			status.GridKW = 3.0

			// Logic: BatteryKW (3) > SolarSurplus (0) AND GridKW > 0  => ChargingFromGrid = true
			// Should switch to Standby to stop grid charging

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
		})

		t.Run("Standby Logic: Charging from Solar -> Load", func(t *testing.T) {
			status := baseStatus
			// Battery charging at 1kW
			status.BatteryKW = -1.0
			// Solar 2.5kW, Home 1kW -> Surplus 1.5kW
			status.SolarKW = 2.5
			status.HomeKW = 1.0
			// Grid Export 0.5kW (GridKW = -0.5)
			status.GridKW = -0.5

			// Logic: BatteryKW (1) <= SolarSurplus (1.5). IsChargingFromGrid = false.
			// Since BatteryKW > 0 and Not Grid Charging -> NoChange.

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			// Charging from Solar -> Load (Allow Discharge/Solar) -> Load (Ensure not Standby)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
		})

		t.Run("Standby Logic: Idle -> Load", func(t *testing.T) {
			status := baseStatus
			status.BatteryKW = 0.0

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			// Idle -> Load
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode)
		})

		t.Run("Solar Mode Match -> NoChange Removed", func(t *testing.T) {
			status := baseStatus

			baseSettings.GridExportSolar = true

			// Decide usually sets SolarModeAny unless price is negative

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode)
			assert.Equal(t, types.SolarModeAny, decision.Action.TargetSolarMode)
		})

		t.Run("Explicit Integration check", func(t *testing.T) {
			status := baseStatus
			status.BatteryKW = 0.0 // Idle

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode)
		})

		t.Run("Solar No Export", func(t *testing.T) {
			status := baseStatus

			baseSettings.GridExportSolar = false

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.SolarModeNoExport, decision.Action.SolarMode)
		})
	})

	t.Run("SolarTrend", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()

		// Use a fixed time at noon on a summer day so the test is deterministic
		// and the solar history data aligns with daylight hours.
		fixedNow := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)

		baseSettings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 true,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 0.01,
			SolarTrendRatioMax:                  3.0,
			SolarBellCurveMultiplier:            1.0,
		}

		baseStatus := types.SystemStatus{
			Timestamp:          fixedNow,
			BatterySOC:         50.0,
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             0.5,
			SolarKW:            2.0,
			BatteryAboveMinSOC: true,
		}

		// Create price to avoid cheap charge triggers
		currentPrice := types.Price{TSStart: fixedNow, TSEnd: fixedNow.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20,
			})
		}

		// Helper to create history with a bell-curve solar profile.
		// If highTrend is true, "today" (last 24h) gets 2x solar.
		createHistory := func(highTrend bool, homeLoad float64) []types.EnergyStats {
			h := []types.EnergyStats{}
			start := fixedNow.Add(-48 * time.Hour).Truncate(time.Hour)
			end := fixedNow.Truncate(time.Hour)

			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				isToday := ts.After(fixedNow.Add(-24 * time.Hour))

				solar := 0.0
				if ts.Hour() >= 7 && ts.Hour() <= 19 {
					dist := math.Abs(float64(ts.Hour()) - 13.0)
					if dist < 6 {
						solar = 1.0 * (1.0 - (dist / 6.0))
					}
				}

				if isToday && highTrend {
					if solar > 0 {
						solar *= 2.0
					}
				}

				h = append(h, types.EnergyStats{
					TSHourStart:    ts,
					SolarKWH:       solar,
					HomeKWH:        homeLoad,
					GridImportKWH:  0.0,
					BatteryUsedKWH: 0.0,
				})
			}
			return h
		}

		t.Run("High Solar Trend -> Load (Sufficient Solar)", func(t *testing.T) {
			history := createHistory(true, 0.1)
			status := baseStatus
			status.HomeKW = 0.1
			status.ElevatedMinBatterySOC = true
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
				"Should return Load because sufficient battery. Got: %v (%s)",
				decision.Action.BatteryMode, decision.Action.Description)
		})

		t.Run("No Solar Trend -> Charge", func(t *testing.T) {
			history := createHistory(false, 2.0)
			status := baseStatus
			status.HomeKW = 2.0
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode,
				"Should predict deficit due to low solar")
			assert.Contains(t, decision.Action.Description, "Projected Deficit")
		})
	})

	t.Run("Early Morning Unnecessary Standby", func(t *testing.T) {
		// 8 AM
		now := time.Date(2025, 6, 15, 8, 0, 0, 0, time.Local)

		baseSettings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false, // Disabled to test Standby/Load decision
			GridExportSolar:                     false, // Export disabled
			MinArbitrageDifferenceDollarsPerKWH: 0.01,
		}

		baseStatus := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            80.0,
			BatteryCapacityKWH:    13.0, // typical Franklin capacity
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                1.0,
			SolarKW:               1.0,
			ElevatedMinBatterySOC: true, // Simulate we are currently in Standby/Full
			BatteryAboveMinSOC:    true,
		}

		// Current Price is moderate/high (Morning Peak)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}

		// Future Prices: all flat at same level as current
		// No higher future price means no reason to standby
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			ts := now.Add(time.Duration(i) * time.Hour)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       ts,
				TSEnd:         ts.Add(time.Hour),
				DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20,
			})
		}

		// History: Strong Solar (2 days for robust model)
		history := []types.EnergyStats{}
		start := now.Add(-48 * time.Hour).Truncate(time.Hour)
		end := now.Truncate(time.Hour)

		for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
			hour := ts.Hour()
			solar := 0.0
			if hour >= 6 && hour <= 20 {
				dist := float64(hour - 13)
				solar = 5.0 - (dist*dist)/10.0
				if solar < 0 {
					solar = 0
				}
			}

			history = append(history, types.EnergyStats{
				TSHourStart: ts,
				SolarKWH:    solar,
				HomeKWH:     1.0,
			})
		}

		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
			"Should load (discharge) because battery will refill from solar")
	})

	t.Run("PreventSolarCurtailment", func(t *testing.T) {
		now := time.Now()
		// Noon on a sunny day
		fixedNow := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())

		settings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false,
			GridExportSolar:                     false, // EXPORT DISABLED
			MinArbitrageDifferenceDollarsPerKWH: 2.0,   // High arbitrage to avoid interference
			SolarFullyChargeHeadroomBatterySOC:  5.0,   // 5% headroom (hit 95%)
			SolarBellCurveMultiplier:            1.0,
		}

		status := types.SystemStatus{
			Timestamp:          fixedNow,
			BatterySOC:         90.0, // Almost full
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             1.0,
			SolarKW:            0.0, // Currently no solar (maybe just before solar hours)
			BatteryAboveMinSOC: true,
		}

		// Current price is moderate
		currentPrice := types.Price{TSStart: fixedNow, TSEnd: fixedNow.Add(time.Hour), DollarsPerKWH: 0.10}

		// Future prices: a huge peak 6 hours from now
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 6 {
				price = 1.00 // HUGE PEAK
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		// History: 2kW load constant, but let's make it have some solar at Noon.
		history := []types.EnergyStats{}
		for i := -48; i < 0; i++ {
			ts := fixedNow.Add(time.Duration(i) * time.Hour)
			solar := 0.0
			if ts.Hour() >= 13 && ts.Hour() <= 16 {
				solar = 5.0 // High solar in afternoon
			}
			history = append(history, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
		}

		for i := range history {
			history[i].HomeKWH = 1.0
		}
		status.HomeKW = 1.0

		status.BatterySOC = 95.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings)
		require.NoError(t, err)

		assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Solar curtailment likely")
	})

	t.Run("NetMeteringCredits", func(t *testing.T) {
		now := time.Now()
		// 5 PM (Peak starts)
		fixedNow := time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, now.Location())

		settings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 2.0,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}

		status := types.SystemStatus{
			Timestamp:             fixedNow,
			BatterySOC:            50.0,
			BatteryCapacityKWH:    10.0,
			HomeKW:                3.0,
			SolarKW:               0.0,
			ElevatedMinBatterySOC: true,
			BatteryAboveMinSOC:    true,
		}

		// Current price is high (Peak)
		currentPrice := types.Price{
			DollarsPerKWH:        0.50,
			GridUseDollarsPerKWH: 0.10,
			TSStart:              fixedNow,
			TSEnd:                fixedNow.Add(time.Hour),
		}

		// Future prices: flat at same level (so this is the "peak")
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.10,
			})
		}

		// History: No solar, 3kW load constant
		history := []types.EnergyStats{}
		for i := -48; i < 0; i++ {
			history = append(history, types.EnergyStats{
				TSHourStart: fixedNow.Add(time.Duration(i) * time.Hour),
				HomeKWH:     3.0,
				SolarKWH:    0.0,
			})
		}

		t.Run("Peak Discharging -> Load (Conservative Credits Active)", func(t *testing.T) {
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings)
			require.NoError(t, err)

			assert.Equal(t, types.BatteryModeLoad, decision.Action.TargetBatteryMode, "Should prefer Load during peak even with Net Metering to conservatively avoid peak grid pulls.")
		})
	})

	t.Run("Deficit Charge Now -> No Charge If Full", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20 // Later it's more expensive to charge ($0.20 vs $0.05)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is full
		fullStatus := baseStatus
		fullStatus.BatterySOC = 100.0

		// Use History with high load (1.0kW * 24 = 24kWh needed, but capacity is 10kWh)
		// This will predict a deficit later.
		decision, err := c.Decide(ctx, fullStatus, currentPrice, futurePrices, history, nil, baseSettings)
		require.NoError(t, err)

		// It should NOT charge for deficit because already full.
		// Instead it should probably Standby (Deficit predicted, saving for peak) or Load.
		// Since current price is 0.05 and future is 0.20, it should Standby unless we expect to be full from solar.
		assert.NotEqual(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})
}
