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

func mockInfo() types.ESSProviderInfo {
	return types.ESSProviderInfo{
		ID:   "mock",
		Name: "Mock ESS",
		Credentials: []types.ESSCredentialField{
			{
				Field:       "strategy",
				Name:        "Strategy",
				Type:        types.ESSCredentialFieldTypeSelect,
				Choices:     []types.ESSCredentialFieldChoice{{Value: "simple", Name: "Simple"}},
				Default:     "simple",
				Required:    true,
				Description: "The simulation strategy (e.g., 'simple')",
			},
		},
		Hidden: true,
	}
}

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

func (m *MockESS) Name() string {
	return "mock"
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
			if state.BatteryMode == types.BatteryModeStandby || state.BatteryMode == types.BatteryModeChargeAny {
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

		targetSOC := 100
		if state.ChargeToSOC != 0 {
			targetSOC = state.ChargeToSOC
		}
		// if we're supposed to be charging, or if we're below min SOC, pull from the grid
		// whatever solar isn't giving us
		if (state.BatteryMode == types.BatteryModeChargeAny && state.BatterySOC < float64(targetSOC)) || state.BatterySOC < m.settings.MinBatterySOC {
			currentChargeKW := -stepBatteryKW
			if currentChargeKW < maxChargeRateKW {
				extraChargeKW := maxChargeRateKW - currentChargeKW
				spaceToTargetSOC := (float64(targetSOC) - state.BatterySOC) / 100.0 * capacityKWH
				if spaceToTargetSOC < 0 {
					spaceToTargetSOC = 0
				}
				limitKWH := spaceToTargetSOC
				if state.BatterySOC < m.settings.MinBatterySOC {
					limitKWH = maxChargeKWH
				}
				if extraChargeKW*durationHours > limitKWH {
					extraChargeKW = limitKWH / durationHours
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
			stats.TimeLocation = m.location.String()
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

// GridSettings returns the grid-related capabilities for the mock ESS.
func (m *MockESS) GridSettings(ctx context.Context) (types.GridSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return types.GridSettings{
		GridChargeBatteries: true,
		GridExportSolar:     true,
		GridExportBatteries: false,
	}, nil
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

	now := time.Now().In(m.location)
	batteryKW, solarKW, homeKW, gridKW := m.advanceState(&state, now)

	if err := mockDB.UpdateESSMockState(ctx, m.siteID, state); err != nil {
		return types.SystemStatus{}, err
	}

	return types.SystemStatus{
		Timestamp:             now,
		TimeLocation:          m.location.String(),
		BatterySOC:            state.BatterySOC,
		BatteryKW:             batteryKW,
		SolarKW:               solarKW,
		HomeKW:                homeKW,
		GridKW:                gridKW,
		BatteryCapacityKWH:    10.0,
		MaxBatteryChargeKW:    5.0,
		MaxBatteryDischargeKW: 5.0,
		ElevatedMinBatterySOC: state.BatteryMode != types.BatteryModeLoad,
		BatteryAboveMinSOC:    false,
	}, nil
}

// SetModes updates the stored battery and solar target modes the mock should adhere to.
func (m *MockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := mockDB.GetESSMockState(ctx, m.siteID)
	if err != nil {
		return false, err
	}

	changed := state.BatteryMode != bat || state.SolarMode != sol || state.ChargeToSOC != opts.ChargeToSOC || (opts.MinimumSOC != 0 && state.MinimumSOC != opts.MinimumSOC)

	// advance time to now with current modes before switching
	m.advanceState(&state, time.Now())

	// now set the modes so the next time we can apply them
	state.BatteryMode = bat
	state.SolarMode = sol
	state.ChargeToSOC = opts.ChargeToSOC
	if opts.MinimumSOC != 0 {
		state.MinimumSOC = opts.MinimumSOC
	}

	if err := mockDB.UpdateESSMockState(ctx, m.siteID, state); err != nil {
		return false, err
	}
	return changed, nil
}

// GetEnergyHistory returns historical daily energy data between a start and end time.
// It also ensures the state simulation continues accurately up to the current wall-clock time.
func (m *MockESS) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := mockDB.GetESSMockState(ctx, m.siteID)
	if err != nil {
		return nil, err
	}

	rawHourlyStatsMap := make(map[string]types.EnergyStats)
	now := time.Now().In(m.location)

	if state.Timestamp.IsZero() {
		// Start from the beginning of the first requested day
		state.Timestamp = getMidnight(start.In(m.location))
		state.BatterySOC = 50.0
	}

	// Advance state until 'now', collecting all history points along the way
	// We do this by advancing one day at a time, collecting history, then crossing midnight
	for state.Timestamp.Before(now) {
		currentMidnight := getMidnight(state.Timestamp.In(m.location))
		nextMidnight := currentMidnight.AddDate(0, 0, 1)

		// Target is just before the next midnight, or 'now'
		target := nextMidnight.Add(-time.Millisecond)
		if target.After(now) {
			target = now
		}

		m.advanceState(&state, target)

		// Collect everything currently in DailyHistory
		for k, v := range state.DailyHistory {
			rawHourlyStatsMap[k] = v
		}

		if target.Equal(now) {
			break
		}

		// Advance to exactly midnight to trigger the next day reset
		m.advanceState(&state, nextMidnight)
	}

	if err := mockDB.UpdateESSMockState(ctx, m.siteID, state); err != nil {
		return nil, err
	}

	var rawHourlyStats []types.EnergyStats
	for _, stats := range rawHourlyStatsMap {
		rawHourlyStats = append(rawHourlyStats, stats)
	}

	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, m.location)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, m.location)

	dailyMap := make(map[string][]types.EnergyStats)
	var sortedDayKeys []string

	for _, stats := range rawHourlyStats {
		dayStart := time.Date(stats.TSHourStart.Year(), stats.TSHourStart.Month(), stats.TSHourStart.Day(), 0, 0, 0, 0, m.location)
		if dayStart.Before(startDay) || dayStart.After(endDay) {
			continue
		}
		dayKey := dayStart.Format("2006-01-02")
		if _, exists := dailyMap[dayKey]; !exists {
			sortedDayKeys = append(sortedDayKeys, dayKey)
		}
		dailyMap[dayKey] = append(dailyMap[dayKey], stats)
	}

	sort.Strings(sortedDayKeys)

	for _, k := range sortedDayKeys {
		list := dailyMap[k]
		sort.Slice(list, func(i, j int) bool {
			return list[i].TSHourStart.Before(list[j].TSHourStart)
		})
		dailyMap[k] = list
	}

	var history []types.DailyEnergyStats
	for _, key := range sortedDayKeys {
		dayStart, err := time.ParseInLocation("2006-01-02", key, m.location)
		if err != nil {
			return nil, fmt.Errorf("failed to parse day key %s: %w", key, err)
		}

		history = append(history, types.DailyEnergyStats{
			TSDayStart:   dayStart,
			TimeLocation: m.location.String(),
			Hourly:       dailyMap[key],
		})
	}

	return history, nil
}
