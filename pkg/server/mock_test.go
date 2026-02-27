package server

import (
	"context"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/mock"
)

type mockStorage = storagemock.MockDatabase

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
