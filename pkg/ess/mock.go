package ess

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

var (
	mockDB storage.Database
)

// ConfigureMock sets the database for the mock ESS provider.
func ConfigureMock(db storage.Database) {
	mockDB = db
}

type MockESS struct {
	mu       sync.Mutex
	settings types.Settings
	siteID   string
	location *time.Location
	strategy string
}

func newMock(siteID string) *MockESS {
	return &MockESS{
		siteID: siteID,
	}
}

func mockInfo() types.ESSProviderInfo {
	return types.ESSProviderInfo{
		ID:   "mock",
		Name: "Mock ESS",
		Credentials: []types.ESSCredential{
			{
				Field:       "strategy",
				Name:        "Strategy",
				Type:        "string",
				Required:    true,
				Description: "The simulation strategy (e.g., 'simple')",
			},
		},
		Hidden: true,
	}
}

// ApplySettings saves the current system settings for use in the simulation.
func (m *MockESS) ApplySettings(ctx context.Context, settings types.Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
	return nil
}

// Authenticate prepares credentials and initializes the location for the mock.
// If no mock credentials exist, it creates defaults.
func (m *MockESS) Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error) {
	var updated bool
	if creds.Mock == nil {
		return creds, false, ErrCredentialsMissing
	}
	if creds.Mock.Strategy != "simple" {
		return creds, false, fmt.Errorf("invalid strategy: %s", creds.Mock.Strategy)
	}
	if creds.Mock.Location == "" {
		creds.Mock.Location = "America/Chicago"
		updated = true
	}
	loc, err := time.LoadLocation(creds.Mock.Location)
	if err != nil {
		return creds, false, err
	}
	m.mu.Lock()
	m.location = loc
	m.strategy = creds.Mock.Strategy
	m.mu.Unlock()
	return creds, updated, nil
}

func getMidnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (m *MockESS) advanceState(state *types.ESSMockState, now time.Time) (batteryKW, solarKW, homeKW, gridKW float64) {
	if m.strategy != "simple" {
		panic(fmt.Sprintf("unsupported strategy: %s", m.strategy))
	}

	now = now.In(m.location)

	lastMidnight := getMidnight(state.Timestamp.In(m.location))
	currentMidnight := getMidnight(now)
	if currentMidnight.After(lastMidnight) {
		if state.BatterySOC == 0 {
			state.BatterySOC = 50
		} else {
			// make sure we're at least at min SOC
			state.BatterySOC = max(state.BatterySOC, m.settings.MinBatterySOC)
		}
		state.DailyHistory = make(map[string]types.EnergyStats)
		if state.Timestamp.Before(currentMidnight) {
			state.Timestamp = currentMidnight
		}
	}
	// fill in defaults in case they're not set
	if state.DailyHistory == nil {
		state.DailyHistory = make(map[string]types.EnergyStats)
	}
	if state.BatteryMode == 0 {
		state.BatteryMode = types.BatteryModeLoad
	}
	if state.SolarMode == 0 {
		state.SolarMode = types.SolarModeAny
	}

	stepStart := state.Timestamp
	capacityKWH := 10.0
	maxChargeRateKW := 5.0
	maxDischargeRateKW := 5.0

	// Use at most 5 minute steps
	for stepStart.Before(now) {
		stepEnd := stepStart.Add(5 * time.Minute)
		if stepEnd.After(now) {
			stepEnd = now
		}

		durationHours := stepEnd.Sub(stepStart).Hours()
		if durationHours <= 0 {
			break
		}

		stepMid := stepStart.Add(stepEnd.Sub(stepStart) / 2)

		hour := float64(stepMid.Hour()) + float64(stepMid.Minute())/60.0

		// Predictable home load 1.5 - 2.5 kW on a sine wave that peaks every 2 hours
		stepHomeKW := 1.5 + 0.5*math.Sin(hour*math.Pi)
		if stepHomeKW < 1.0 {
			stepHomeKW = 1.0
		}

		// Solar generation: Bell curve peak at 13:00
		stepSolarKW := 0.0
		if hour >= 6 && hour <= 19 {
			stepSolarKW = 3.0 * math.Sin((hour-6)/13*math.Pi)
		}

		net := stepSolarKW - stepHomeKW
		stepBatteryKW := 0.0
		stepGridKW := 0.0

		// calculate how much space is available and how much energy is stored
		spaceKWH := (100.0 - state.BatterySOC) / 100.0 * capacityKWH

		usableSOC := state.BatterySOC - m.settings.MinBatterySOC
		if usableSOC < 0 {
			usableSOC = 0
		}
		usableEnergyKWH := (usableSOC / 100.0) * capacityKWH

		maxChargeKWH := spaceKWH
		maxDischargeKWH := usableEnergyKWH

		// if we have excess solar what do we do with it?
		if net > 0 {
			// if battery is in standby or load mode, we don't charge it, unless we're below min SOC
			if (state.BatteryMode == types.BatteryModeStandby || state.BatteryMode == types.BatteryModeLoad) && state.BatterySOC >= m.settings.MinBatterySOC {
				stepBatteryKW = 0
			} else {
				tryChargeKW := min(net, maxChargeRateKW)
				// don't let it charge more than full
				if tryChargeKW*durationHours > maxChargeKWH {
					tryChargeKW = maxChargeKWH / durationHours
				}
				stepBatteryKW = -tryChargeKW
			}

			// how much energy is left after charging the battery, if any, export it
			// or curtail it
			remainingExcess := net - (-stepBatteryKW)
			if m.settings.GridExportSolar || state.SolarMode == types.SolarModeAny || state.SolarMode == types.SolarModeNoChange {
				stepGridKW = -remainingExcess
			} else {
				stepSolarKW -= remainingExcess
				stepGridKW = 0
			}
		} else {
			absNet := -net
			// if we have a deficit, do we have enough battery to cover it?
			if state.BatteryMode == types.BatteryModeStandby || state.BatteryMode == types.BatteryModeChargeAny || state.BatteryMode == types.BatteryModeChargeSolar {
				stepBatteryKW = 0
			} else {
				tryDischargeKW := min(absNet, maxDischargeRateKW)
				// don't let it discharge more than we have
				if tryDischargeKW*durationHours > maxDischargeKWH {
					tryDischargeKW = maxDischargeKWH / durationHours
				}
				stepBatteryKW = tryDischargeKW
			}

			remainingDeficit := absNet - stepBatteryKW
			stepGridKW = remainingDeficit
		}

		// if we're supposed to be charging, or if we're below min SOC, pull from the grid
		// whatever solar isn't giving us
		if (state.BatteryMode == types.BatteryModeChargeAny && state.BatterySOC < 100) || state.BatterySOC < m.settings.MinBatterySOC {
			currentChargeKW := -stepBatteryKW
			if currentChargeKW < maxChargeRateKW {
				extraChargeKW := maxChargeRateKW - currentChargeKW
				if extraChargeKW*durationHours > maxChargeKWH {
					extraChargeKW = maxChargeKWH / durationHours
				}
				stepBatteryKW -= extraChargeKW
				stepGridKW += extraChargeKW
			}
		}

		deltaKWH := -stepBatteryKW * durationHours
		state.BatterySOC += (deltaKWH / capacityKWH) * 100.0
		if state.BatterySOC > 100 {
			state.BatterySOC = 100
		}
		if state.BatterySOC < 0 {
			state.BatterySOC = 0
		}

		tsHourStart := stepStart.Truncate(time.Hour)
		hourKey := tsHourStart.UTC().Format(time.RFC3339)
		stats := state.DailyHistory[hourKey]
		if stats.TSHourStart.IsZero() {
			stats.TSHourStart = tsHourStart
			stats.MinBatterySOC = 100.0
		}

		if state.BatterySOC < stats.MinBatterySOC {
			stats.MinBatterySOC = state.BatterySOC
		}
		if state.BatterySOC > stats.MaxBatterySOC {
			stats.MaxBatterySOC = state.BatterySOC
		}

		solarKWH := stepSolarKW * durationHours
		homeKWH := stepHomeKW * durationHours

		stats.SolarKWH += solarKWH
		stats.HomeKWH += homeKWH

		if stepBatteryKW < 0 {
			chargeKWH := -stepBatteryKW * durationHours
			stats.BatteryChargedKWH += chargeKWH
			if solarKWH > homeKWH {
				stats.SolarToBatteryKWH += math.Min(solarKWH-homeKWH, chargeKWH)
			}
		} else {
			dischargeKWH := stepBatteryKW * durationHours
			stats.BatteryUsedKWH += dischargeKWH
			stats.BatteryToHomeKWH += dischargeKWH
		}

		if stepGridKW > 0 {
			stats.GridImportKWH += stepGridKW * durationHours
		} else {
			exportKWH := -stepGridKW * durationHours
			stats.GridExportKWH += exportKWH
			stats.SolarToGridKWH += exportKWH
		}

		stats.SolarToHomeKWH += math.Min(solarKWH, homeKWH)

		state.DailyHistory[hourKey] = stats

		batteryKW = stepBatteryKW
		solarKW = stepSolarKW
		homeKW = stepHomeKW
		gridKW = stepGridKW

		stepStart = stepEnd
	}

	state.Timestamp = now
	return batteryKW, solarKW, homeKW, gridKW
}

// GetStatus computes the current simulated values for home usage, solar generation,
// and battery status based on elapsed time, then updates and returns that state.
func (m *MockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := mockDB.GetESSMockState(ctx, m.siteID)
	if err != nil {
		return types.SystemStatus{}, err
	}

	now := time.Now()
	batteryKW, solarKW, homeKW, gridKW := m.advanceState(&state, now)

	if err := mockDB.UpdateESSMockState(ctx, m.siteID, state); err != nil {
		return types.SystemStatus{}, err
	}

	return types.SystemStatus{
		Timestamp:             now,
		BatterySOC:            state.BatterySOC,
		EachBatterySOC:        []float64{state.BatterySOC},
		BatteryKW:             batteryKW,
		SolarKW:               solarKW,
		HomeKW:                homeKW,
		GridKW:                gridKW,
		BatteryCapacityKWH:    10.0,
		MaxBatteryChargeKW:    5.0,
		MaxBatteryDischargeKW: 5.0,
		CanExportSolar:        state.SolarMode != types.SolarModeNoExport,
		CanExportBattery:      true,
		CanImportBattery:      state.BatteryMode == types.BatteryModeChargeAny,
		ElevatedMinBatterySOC: state.BatteryMode != types.BatteryModeLoad,
		BatteryAboveMinSOC:    false,
	}, nil
}

// SetModes updates the stored battery and solar target modes the mock should adhere to.
func (m *MockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := mockDB.GetESSMockState(ctx, m.siteID)
	if err != nil {
		return err
	}

	// advance time to now with current modes before switching
	m.advanceState(&state, time.Now())

	// now set the modes so the next time we can apply them
	state.BatteryMode = bat
	state.SolarMode = sol

	return mockDB.UpdateESSMockState(ctx, m.siteID, state)
}

// GetEnergyHistory returns historical hourly energy data between a start and end time.
// It also ensures the state simulation continues accurately up to the current wall-clock time.
func (m *MockESS) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := mockDB.GetESSMockState(ctx, m.siteID)
	if err != nil {
		return nil, err
	}

	var history []types.EnergyStats
	var needsSave bool
	now := time.Now().In(m.location)

	if state.Timestamp.IsZero() {
		// Backfill: previous day and today up to 'now'
		currentMidnight := getMidnight(now)
		previousMidnight := currentMidnight.Add(-time.Second)

		// advance through all of yesterday, up to almost midnight
		m.advanceState(&state, previousMidnight)
		needsSave = true

		// then collect all of the history
		for _, stats := range state.DailyHistory {
			if !stats.TSHourStart.Before(start) && stats.TSHourStart.Before(end) {
				history = append(history, stats)
			}
		}
		// fallthrough to collecting today's history
	}

	if state.Timestamp.Before(now) {
		// If we have state but it's older than 'now', advance it up to 'now'
		m.advanceState(&state, now)
		needsSave = true
	}

	if needsSave {
		if err := mockDB.UpdateESSMockState(ctx, m.siteID, state); err != nil {
			return nil, err
		}
	}

	for _, stats := range state.DailyHistory {
		if !stats.TSHourStart.Before(start) && stats.TSHourStart.Before(end) {
			history = append(history, stats)
		}
	}

	sort.Slice(history, func(i, j int) bool {
		return history[i].TSHourStart.Before(history[j].TSHourStart)
	})

	return history, nil
}
