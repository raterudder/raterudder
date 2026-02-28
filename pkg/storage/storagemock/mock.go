package storagemock

import (
	"context"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/mock"
)

type MockDatabase struct {
	mock.Mock
}

var _ storage.Database = (*MockDatabase)(nil)

func (m *MockDatabase) GetSettings(ctx context.Context, siteID string) (types.Settings, int, error) {
	args := m.Called(ctx, siteID)
	// return empty if not specified, or checks args
	if len(args) > 0 {
		return args.Get(0).(types.Settings), args.Int(1), args.Error(2)
	}
	return types.Settings{}, 0, nil
}

func (m *MockDatabase) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int) error {
	args := m.Called(ctx, siteID, settings, version)
	return args.Error(0)
}

func (m *MockDatabase) UpsertPrices(ctx context.Context, siteID string, prices []types.Price, version int) error {
	args := m.Called(ctx, siteID, prices, version)
	return args.Error(0)
}

func (m *MockDatabase) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	args := m.Called(ctx, siteID, action)
	return args.Error(0)
}

func (m *MockDatabase) UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.EnergyStats, version int) error {
	args := m.Called(ctx, siteID, stats, version)
	return args.Error(0)
}

func (m *MockDatabase) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	args := m.Called(ctx, siteID, state)
	return args.Error(0)
}

func (m *MockDatabase) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.ESSMockState), args.Error(1)
	}
	return types.ESSMockState{}, nil
}

func (m *MockDatabase) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Price), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.Action), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error) {
	args := m.Called(ctx, siteID, start, end)
	if len(args) > 0 {
		return args.Get(0).([]types.EnergyStats), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *MockDatabase) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(time.Time), args.Int(1), args.Error(2)
	}
	return time.Time{}, 0, nil
}

func (m *MockDatabase) GetUser(ctx context.Context, email string) (types.User, error) {
	args := m.Called(ctx, email)
	if len(args) > 0 {
		return args.Get(0).(types.User), args.Error(1)
	}
	return types.User{}, nil
}

func (m *MockDatabase) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	args := m.Called(ctx, siteID)
	if len(args) > 0 {
		return args.Get(0).(types.Site), args.Error(1)
	}
	return types.Site{}, nil
}

func (m *MockDatabase) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	args := m.Called(ctx, siteID, site)
	return args.Error(0)
}

func (m *MockDatabase) CreateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) UpdateUser(ctx context.Context, user types.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockDatabase) ListSites(ctx context.Context) ([]types.Site, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.Site), args.Error(1)
	}
	return nil, nil
}

func (m *MockDatabase) GetLatestAction(ctx context.Context, siteID string) (*types.Action, error) {
	args := m.Called(ctx, siteID)
	val := args.Get(0)
	if val == nil {
		return nil, args.Error(1)
	}
	return val.(*types.Action), args.Error(1)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}
