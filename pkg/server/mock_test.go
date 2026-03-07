package server

import (
	"context"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/mock"
)

type mockStorage struct {
	mock.Mock
}

var _ storage.Database = (*mockStorage)(nil)

func (m *mockStorage) GetSettings(ctx context.Context, siteID string) (types.Settings, int, error) {
	args := m.Called(ctx, siteID)
	// return empty if not specified, or checks args
	if len(args) > 0 {
		return args.Get(0).(types.Settings), args.Int(1), args.Error(2)
	}
	return types.Settings{}, 0, nil
}

func (m *mockStorage) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int) error {
	args := m.Called(ctx, siteID, settings, version)
	return args.Error(0)
}

func (m *mockStorage) UpsertPrice(ctx context.Context, siteID string, price types.Price, version int) error {
	args := m.Called(ctx, siteID, price, version)
	return args.Error(0)
}

func (m *mockStorage) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	args := m.Called(ctx, siteID, action)
	return args.Error(0)
}

func (m *mockStorage) UpsertEnergyHistory(ctx context.Context, siteID string, stats types.EnergyStats, version int) error {
	args := m.Called(ctx, siteID, stats, version)
	return args.Error(0)
}

func (m *mockStorage) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	args := m.Called(ctx, siteID, state)
	return args.Error(0)
}

func (m *mockStorage) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.ESSMockState), args.Error(1)
	}
	return types.ESSMockState{}, nil
}

func (m *mockStorage) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Price), args.Error(1)
	}
	return nil, nil
}

func (m *mockStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Action), args.Error(1)
	}
	return nil, nil
}

func (m *mockStorage) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.EnergyStats), args.Error(1)
	}
	return nil, nil
}

func (m *mockStorage) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *mockStorage) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *mockStorage) GetUser(ctx context.Context, email string) (types.User, error) {
	args := m.Called(ctx, email)
	if len(args) > 0 {
		return args.Get(0).(types.User), args.Error(1)
	}
	return types.User{}, nil
}

func (m *mockStorage) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.Site), args.Error(1)
	}
	return types.Site{}, nil
}

func (m *mockStorage) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *mockStorage) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *mockStorage) CreateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockStorage) UpdateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockStorage) ListSites(ctx context.Context) ([]types.Site, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.Site), args.Error(1)
	}
	return nil, nil
}

func (m *mockStorage) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockUtility struct {
	mock.Mock
}

func (m *mockUtility) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.Price), args.Error(1)
	}
	return types.Price{}, nil
}
func (m *mockUtility) LastConfirmedPrice(ctx context.Context) (types.Price, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.Price), args.Error(1)
	}
	return types.Price{}, nil
}
func (m *mockUtility) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.Price), args.Error(1)
	}
	return nil, nil
}
func (m *mockUtility) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Price), args.Error(1)
	}
	return nil, nil
}
func (m *mockUtility) ApplySettings(ctx context.Context, settings types.Settings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

type mockESS struct {
	mock.Mock
}

func (m *mockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.SystemStatus), args.Error(1)
	}
	return types.SystemStatus{}, nil
}
func (m *mockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	args := m.Called(ctx, bat, sol)
	return args.Error(0)
}
func (m *mockESS) ApplySettings(ctx context.Context, settings types.Settings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *mockESS) Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error) {
	args := m.Called(ctx, creds)
	if len(args) > 0 {
		return args.Get(0).(types.Credentials), args.Bool(1), args.Error(2)
	}
	return creds, false, nil
}
func (m *mockESS) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	args := m.Called(ctx, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.EnergyStats), args.Error(1)
	}
	return nil, nil
}
func (m *mockESS) Validate() error {
	args := m.Called()
	return args.Error(0)
}
