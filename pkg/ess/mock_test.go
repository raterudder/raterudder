package ess

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) GetSettings(ctx context.Context, siteID string) (types.Settings, int, error) {
	args := m.Called(ctx, siteID)
	return args.Get(0).(types.Settings), args.Int(1), args.Error(2)
}

func (m *MockDatabase) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int) error {
	args := m.Called(ctx, siteID, settings, version)
	return args.Error(0)
}

func (m *MockDatabase) UpsertPrice(ctx context.Context, siteID string, price types.Price, version int) error {
	args := m.Called(ctx, siteID, price, version)
	return args.Error(0)
}

func (m *MockDatabase) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	args := m.Called(ctx, siteID, action)
	return args.Error(0)
}

func (m *MockDatabase) UpsertEnergyHistory(ctx context.Context, siteID string, stats types.EnergyStats, version int) error {
	args := m.Called(ctx, siteID, stats, version)
	return args.Error(0)
}

func (m *MockDatabase) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	args := m.Called(ctx, siteID, state)
	return args.Error(0)
}

func (m *MockDatabase) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	args := m.Called(ctx, siteID)
	return args.Get(0).(types.ESSMockState), args.Error(1)
}

func (m *MockDatabase) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, siteID, start, end)
	return args.Get(0).([]types.Price), args.Error(1)
}

func (m *MockDatabase) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	args := m.Called(ctx, siteID, start, end)
	return args.Get(0).([]types.Action), args.Error(1)
}

func (m *MockDatabase) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error) {
	args := m.Called(ctx, siteID, start, end)
	return args.Get(0).([]types.EnergyStats), args.Error(1)
}

func (m *MockDatabase) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	return args.Get(0).(time.Time), args.Int(1), args.Error(2)
}

func (m *MockDatabase) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	return args.Get(0).(time.Time), args.Int(1), args.Error(2)
}

func (m *MockDatabase) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	args := m.Called(ctx, siteID)
	return args.Get(0).(types.Site), args.Error(1)
}

func (m *MockDatabase) ListSites(ctx context.Context) ([]types.Site, error) {
	args := m.Called(ctx)
	return args.Get(0).([]types.Site), args.Error(1)
}

func (m *MockDatabase) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) GetUser(ctx context.Context, userID string) (types.User, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(types.User), args.Error(1)
}

func (m *MockDatabase) CreateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) UpdateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestMockESS(t *testing.T) {
	t.Run("GetStatus", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)

		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)
		ctx := context.Background()

		// 1. Test first run (no state)
		db.On("GetESSMockState", ctx, "test-site").Return(types.ESSMockState{}, nil).Once()
		db.On("UpdateESSMockState", ctx, "test-site", mock.Anything).Return(nil).Once()

		status, err := ess.GetStatus(ctx)
		require.NoError(t, err)
		assert.NotEqual(t, 50.0, status.BatterySOC)
		assert.Equal(t, 10.0, status.BatteryCapacityKWH)

		// 2. Test subsequent run (with state)
		lastTime := time.Now().Add(-1 * time.Hour)
		state := types.ESSMockState{
			Timestamp:   lastTime,
			BatterySOC:  60.0,
			BatteryMode: types.BatteryModeChargeAny,
		}
		db.On("GetESSMockState", ctx, "test-site").Return(state, nil).Once()
		db.On("UpdateESSMockState", ctx, "test-site", mock.Anything).Return(nil).Once()

		status, err = ess.GetStatus(ctx)
		require.NoError(t, err)
		// SOC should have changed based on random usage (usually down if Solar is low)
		assert.NotEqual(t, 60.0, status.BatterySOC)
		assert.True(t, status.BatterySOC >= 0 && status.BatterySOC <= 100, status.BatterySOC)

		db.AssertExpectations(t)
	})

	t.Run("Authenticate", func(t *testing.T) {
		ess := newMock("test-site")

		// 1. Nil credentials
		creds, isNew, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)
		assert.True(t, isNew)
		assert.NotNil(t, creds.Mock)
		assert.Equal(t, "America/Chicago", creds.Mock.Location)
		assert.Equal(t, "simple", creds.Mock.Strategy)

		// 2. Existing credentials
		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{
				Strategy: "simple",
				Location: "America/New_York",
			},
		}
		creds, isNew, err = ess.Authenticate(context.Background(), existingCreds)
		require.NoError(t, err)
		assert.False(t, isNew)
		assert.Equal(t, "America/New_York", creds.Mock.Location)
		assert.Equal(t, "simple", creds.Mock.Strategy)
	})

	t.Run("SetModes", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)
		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)
		ctx := context.Background()

		// initial state
		initialState := types.ESSMockState{
			Timestamp:  time.Now().Add(-time.Hour),
			BatterySOC: 50.0,
		}

		db.On("GetESSMockState", ctx, "test-site").Return(initialState, nil).Once()

		// SetModes advances state and updates the modes
		db.On("UpdateESSMockState", ctx, "test-site", mock.MatchedBy(func(s types.ESSMockState) bool {
			return s.BatteryMode == types.BatteryModeStandby && s.SolarMode == types.SolarModeNoExport
		})).Return(nil).Once()

		err = ess.SetModes(ctx, types.BatteryModeStandby, types.SolarModeNoExport)
		require.NoError(t, err)

		db.AssertExpectations(t)
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)
		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)
		ctx := context.Background()

		// 1. Empty state backfill
		db.On("GetESSMockState", ctx, "test-site").Return(types.ESSMockState{}, nil).Once()

		loc, _ := time.LoadLocation("America/Chicago")
		now := time.Now().In(loc)
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

		// Should save after advancing to now, but it should ONLY save today's data in the state map
		db.On("UpdateESSMockState", ctx, "test-site", mock.MatchedBy(func(s types.ESSMockState) bool {
			// DailyHistory should only contain items from same day as the timestamp
			// after the midnight reset logic fires.
			for _, stats := range s.DailyHistory {
				if stats.TSHourStart.Before(midnight) {
					return false // Contains old data!
				}
			}
			return len(s.DailyHistory) > 0
		})).Return(nil).Once()

		end := time.Now().In(loc)
		start := end.Add(-72 * time.Hour)
		history, err := ess.GetEnergyHistory(ctx, start, end)
		require.NoError(t, err)
		assert.NotEmpty(t, history)

		// Assert we have at least exactly 24 elements for yesterday and some for today
		var yesterdayDataCount int
		for _, v := range history {
			// Count how many are returned that are before the local midnight
			if v.TSHourStart.Before(midnight) {
				yesterdayDataCount++
			}
		}

		// depending on when start was there may be 23 or 24 depending on exactly when we cross the hour
		assert.GreaterOrEqual(t, yesterdayDataCount, 23, "Should return yesterday's backfilled data")

		db.AssertExpectations(t)
	})

	t.Run("EnergyStatsFields", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)
		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)

		// Set settings to enable solar export
		err = ess.ApplySettings(context.Background(), types.Settings{
			GridExportSolar: true,
			MinBatterySOC:   20,
		})
		require.NoError(t, err)

		ctx := context.Background()
		db.On("GetESSMockState", ctx, "test-site").Return(types.ESSMockState{}, nil).Once()
		db.On("UpdateESSMockState", ctx, "test-site", mock.Anything).Return(nil).Once()

		end := time.Now()
		start := end.Add(-24 * time.Hour)
		history, err := ess.GetEnergyHistory(ctx, start, end)
		require.NoError(t, err)
		assert.NotEmpty(t, history)

		foundDetailed := false
		for _, stat := range history {
			// check if we have any hour with both solar and battery activity
			if stat.SolarKWH > 0 && (stat.SolarToBatteryKWH > 0 || stat.SolarToGridKWH > 0 || stat.SolarToHomeKWH > 0) {
				foundDetailed = true
				// Basic sum check
				assert.InDelta(t, stat.SolarToHomeKWH+stat.SolarToBatteryKWH+stat.SolarToGridKWH, stat.SolarKWH, 0.001, "Solar components should sum to total solar")
				// In the mock, battery only discharges to home
				if stat.BatteryUsedKWH > 0 {
					assert.InDelta(t, stat.BatteryToHomeKWH, stat.BatteryUsedKWH, 0.001)
				}
			}
		}
		assert.True(t, foundDetailed, "Should have found hours with solar data to verify detailed fields")
	})

	t.Run("Force charge below min SOC", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)
		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)

		// Set min SOC to 30
		err = ess.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC: 30,
		})
		require.NoError(t, err)

		ctx := context.Background()
		// Start with 10% SOC and Standby mode
		state := types.ESSMockState{
			Timestamp:   time.Now().Add(-1 * time.Hour),
			BatterySOC:  10.0,
			BatteryMode: types.BatteryModeStandby,
		}

		db.On("GetESSMockState", ctx, "test-site").Return(state, nil).Once()
		db.On("UpdateESSMockState", ctx, "test-site", mock.MatchedBy(func(s types.ESSMockState) bool {
			// SOC should have increased because it was below 30%
			return s.BatterySOC > 10.0
		})).Return(nil).Once()

		status, err := ess.GetStatus(ctx)
		require.NoError(t, err)
		assert.Greater(t, status.BatterySOC, 10.0, "Battery should have charged even in Standby mode because it was below MinBatterySOC")
	})

	t.Run("Repeated GetStatus", func(t *testing.T) {
		db := new(MockDatabase)
		ConfigureMock(db)
		ess := newMock("test-site")
		_, _, err := ess.Authenticate(context.Background(), types.Credentials{})
		require.NoError(t, err)
		ctx := context.Background()

		// 1. Initial
		db.On("GetESSMockState", ctx, "test-site").Return(types.ESSMockState{}, nil).Once()

		var savedState types.ESSMockState
		db.On("UpdateESSMockState", ctx, "test-site", mock.Anything).Run(func(args mock.Arguments) {
			savedState = args.Get(2).(types.ESSMockState)
		}).Return(nil).Once()

		status1, err := ess.GetStatus(ctx)
		require.NoError(t, err)

		// 2. Second time
		db.On("GetESSMockState", ctx, "test-site").Return(savedState, nil).Once()
		db.On("UpdateESSMockState", ctx, "test-site", mock.Anything).Return(nil).Once()

		status2, err := ess.GetStatus(ctx)
		require.NoError(t, err)
		// Timestamp should advance slightly or be equal
		assert.True(t, status2.Timestamp.After(status1.Timestamp) || status2.Timestamp.Equal(status1.Timestamp))

		db.AssertExpectations(t)
	})
}
