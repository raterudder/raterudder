package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

func main() {
	os.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8087")
	s := storage.Configured()
	lflag.Configure()

	ctx := context.Background()

	log.Ctx(ctx).InfoContext(ctx, "seeding mock data")

	// Use a new random source
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate some actions for today
	now := time.Now()
	// Midnight to now
	start := now.Truncate(24 * time.Hour)

	// Simulation state
	const (
		BatteryCapacityKWH = 27.2 // 2-battery system
		MaxBatteryKW       = 10.0
		HomeAvgKW          = 1.5
		SolarPeakKW        = 8.0
		DeliveryFee        = 0.04
	)
	currentSOC := 40.0 // Start at 40%

	// Create actions every hour
	for t := start; t.Before(now); t = t.Add(time.Hour) {
		hour := t.Hour()

		// 1. Determine Price Scenario
		basePrice := 0.08
		if hour >= 6 && hour < 9 {
			basePrice = 0.22 // Morning Peak
		} else if hour >= 10 && hour < 15 {
			basePrice = 0.05 // Mid-day Lull
		} else if hour >= 17 && hour < 21 {
			basePrice = 0.35 // Evening Peak
		} else if hour >= 21 {
			basePrice = 0.10 // Night
		}
		// Jitter
		basePrice += (rng.Float64() * 0.02) - 0.01

		currentPrice := &types.Price{
			Provider:             "comed_besh",
			DollarsPerKWH:        basePrice,
			GridUseDollarsPerKWH: DeliveryFee,
			TSStart:              t,
			TSEnd:                t.Add(time.Hour),
		}

		// 2. Determine System Status
		// Solar (bell curve)
		solarKW := 0.0
		if hour > 6 && hour < 19 {
			dist := math.Abs(float64(hour) - 13.0)
			solarKW = SolarPeakKW * math.Exp(-(dist*dist)/12.0)
		}

		// Home usage
		homeKW := HomeAvgKW + (rng.Float64() * 1.0)
		if hour >= 7 && hour < 9 {
			homeKW += 2.0 // Breakfast
		} else if hour >= 18 && hour < 22 {
			homeKW += 4.0 // Evening activities
		}

		// Battery Activity & Decision logic simulation
		var mode types.BatteryMode
		var reason types.ActionReason
		var desc string
		var hitDeficitAt time.Time
		var hitCapacityAt time.Time

		// Strategy simulation
		if hour < 6 {
			// Night - charge if cheap
			if currentPrice.DollarsPerKWH+DeliveryFee < 0.10 && currentSOC < 60 {
				mode = types.BatteryModeChargeAny
				reason = types.ActionReasonAlwaysChargeBelowThreshold
				desc = "Overnight charging"
			} else {
				mode = types.BatteryModeStandby
				reason = types.ActionReasonSufficientBattery
				desc = "Overnight standby"
			}
		} else if hour < 9 {
			// Morning peak
			mode = types.BatteryModeLoad
			reason = types.ActionReasonArbitrageSave
			desc = "Morning peak discharge"
			hitDeficitAt = t.Add(5 * time.Hour)
		} else if hour < 17 {
			// Day time solar charging
			if solarKW > homeKW && currentSOC < 95 {
				mode = types.BatteryModeChargeSolar
				reason = types.ActionReasonSufficientBattery
				desc = "Solar charging"
				hitCapacityAt = t.Add(3 * time.Hour)
			} else {
				mode = types.BatteryModeStandby
				reason = types.ActionReasonSufficientBattery
				desc = "Daytime standby"
			}
		} else if hour < 22 {
			// Evening peak
			mode = types.BatteryModeLoad
			reason = types.ActionReasonArbitrageSave
			desc = "Evening peak discharge"
		} else {
			mode = types.BatteryModeStandby
			reason = types.ActionReasonSufficientBattery
			desc = "Post-peak standby"
		}

		// System Status Simulation updates
		batKW := 0.0
		switch mode {
		case types.BatteryModeChargeAny:
			batKW = -MaxBatteryKW
		case types.BatteryModeChargeSolar:
			// Charge with excess solar only
			surplus := solarKW - homeKW
			if surplus > 0 {
				batKW = -math.Min(surplus, MaxBatteryKW)
			}
		case types.BatteryModeLoad:
			// Discharge to cover home usage
			needed := homeKW - solarKW
			if needed > 0 {
				batKW = math.Min(needed, MaxBatteryKW)
			} else {
				batKW = 0 // Solar covers it
			}
		case types.BatteryModeStandby, types.BatteryModeNoChange:
			batKW = 0.0
		}

		// Update SOC based on batKW (kWh = kW * 1h)
		socDelta := (-batKW / BatteryCapacityKWH) * 100.0
		currentSOC += socDelta
		if currentSOC > 100 {
			currentSOC = 100
			batKW = 0 // actually stopped charging
		}
		if currentSOC < 5 { // Reserve 5%
			currentSOC = 5
			batKW = 0 // actually stopped discharging
		}

		stat := types.SystemStatus{
			Timestamp:             t,
			BatterySOC:            currentSOC,
			BatteryKW:             batKW,
			SolarKW:               solarKW,
			HomeKW:                homeKW,
			GridKW:                homeKW - solarKW - batKW,
			BatteryCapacityKWH:    BatteryCapacityKWH,
			MaxBatteryChargeKW:    MaxBatteryKW,
			MaxBatteryDischargeKW: MaxBatteryKW,
			BatteryAboveMinSOC:    currentSOC > 20,
		}

		action := types.Action{
			Timestamp:         t,
			BatteryMode:       mode,
			SolarMode:         types.SolarModeAny,
			TargetBatteryMode: mode,
			TargetSolarMode:   types.SolarModeAny,
			Reason:            reason,
			Description:       fmt.Sprintf("Mock: %s", desc),
			CurrentPrice:      currentPrice,
			SystemStatus:      stat,
			HitDeficitAt:      hitDeficitAt,
			HitCapacityAt:     hitCapacityAt,
			DryRun:            rng.Float64() > 0.1,
		}

		// Future Price (mocking "some time later")
		futureHourT := t.Add(4 * time.Hour)
		futurePriceVal := 0.12
		if futureHourT.Hour() >= 17 && futureHourT.Hour() < 21 {
			futurePriceVal = 0.35
		}
		action.FuturePrice = &types.Price{
			Provider:             "comed_besh",
			DollarsPerKWH:        futurePriceVal,
			GridUseDollarsPerKWH: DeliveryFee,
			TSStart:              futureHourT,
			TSEnd:                futureHourT.Add(time.Hour),
		}

		if err := s.InsertAction(ctx, types.SiteIDNone, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to seed action", "error", err)
			os.Exit(1)
		}

		if action.CurrentPrice != nil {
			if err := s.UpsertPrices(ctx, types.SiteIDNone, []types.Price{*action.CurrentPrice}, types.CurrentPriceHistoryVersion); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to seed price", "error", err)
				os.Exit(1)
			}
		}

		// Seed EnergyStats
		eStat := types.EnergyStats{
			TSHourStart:   t,
			MinBatterySOC: math.Max(0, currentSOC-2),
			MaxBatterySOC: math.Min(100, currentSOC+2),
			HomeKWH:       stat.HomeKW,
			SolarKWH:      stat.SolarKW,
		}

		// Simplify energy distribution for seeding
		if stat.BatteryKW > 0 {
			// Discharging
			eStat.BatteryUsedKWH = stat.BatteryKW
			eStat.BatteryToHomeKWH = math.Min(stat.BatteryKW, stat.HomeKW)
		} else if stat.BatteryKW < 0 {
			// Charging
			eStat.BatteryChargedKWH = -stat.BatteryKW
			if solarKW > homeKW {
				surplus := solarKW - homeKW
				eStat.SolarToBatteryKWH = math.Min(surplus, -stat.BatteryKW)
			}
		}

		if solarKW > 0 {
			eStat.SolarToHomeKWH = math.Min(solarKW, homeKW)
			if solarKW > (eStat.SolarToHomeKWH + eStat.SolarToBatteryKWH) {
				eStat.SolarToGridKWH = solarKW - eStat.SolarToHomeKWH - eStat.SolarToBatteryKWH
			}
		}

		if stat.GridKW > 0 {
			eStat.GridImportKWH = stat.GridKW
		} else {
			eStat.GridExportKWH = -stat.GridKW
		}

		if err := s.UpsertEnergyHistories(ctx, types.SiteIDNone, []types.EnergyStats{eStat}, types.CurrentEnergyStatsVersion); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to seed energy stats", "error", err)
			os.Exit(1)
		}

		fmt.Printf("Seeded action at %s: %s (Price: $%.3f, SOC: %.0f%%, Solar: %.1fkW)\n",
			t.Format(time.Kitchen), action.Description, action.CurrentPrice.DollarsPerKWH, currentSOC, solarKW)
	}

	log.Ctx(ctx).InfoContext(ctx, "seeded mock data successfully")
}
