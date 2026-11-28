package server

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/mock"
)

type mockStorage = storagemock.MockDatabase

type atomicSettingsTestStore struct {
	mu              sync.Mutex
	settings        types.Settings
	version         int
	updatedAt       time.Time
	seq             int
	conflictRetries atomic.Int32
	onBeforeCommit  func()
}

func cloneSettingsForTest(s types.Settings) types.Settings {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	var out types.Settings
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func (st *atomicSettingsTestStore) snapshot() (types.Settings, int, time.Time, int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return cloneSettingsForTest(st.settings), st.version, st.updatedAt, st.seq
}

func (st *atomicSettingsTestStore) update(mutate storage.SettingsMutator) (types.Settings, int, time.Time, error) {
	for {
		cur, ver, curTime, snapSeq := st.snapshot()
		updated, newVer, save, err := mutate(cur, ver)
		if err != nil {
			return types.Settings{}, 0, time.Time{}, err
		}
		if !save {
			return cur, ver, curTime, nil
		}
		if st.onBeforeCommit != nil {
			st.onBeforeCommit()
		}
		st.mu.Lock()
		if st.seq != snapSeq {
			st.mu.Unlock()
			st.conflictRetries.Add(1)
			continue
		}
		st.seq++
		st.settings = cloneSettingsForTest(updated)
		st.version = newVer
		if st.updatedAt.IsZero() {
			st.updatedAt = time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		}
		st.updatedAt = st.updatedAt.Add(time.Millisecond)
		commitTime := st.updatedAt
		out := cloneSettingsForTest(st.settings)
		st.mu.Unlock()
		return out, newVer, commitTime, nil
	}
}

func attachAtomicSettingsStore(mockS *mockStorage, siteID string, initial types.Settings, initialVersion int, initialTime time.Time) *atomicSettingsTestStore {
	if initialTime.IsZero() {
		initialTime = time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	}
	st := &atomicSettingsTestStore{
		settings:  cloneSettingsForTest(initial),
		version:   initialVersion,
		updatedAt: initialTime,
		seq:       1,
	}

	mockS.On("GetSettings", mock.Anything, siteID).Return(func(_ context.Context, _ string) (types.Settings, int, time.Time, error) {
		cur, ver, curTime, _ := st.snapshot()
		return cur, ver, curTime, nil
	}).Maybe()

	mockS.On("UpdateSettings", mock.Anything, siteID, mock.Anything).Return(func(_ context.Context, _ string, mutate storage.SettingsMutator) (types.Settings, int, time.Time, error) {
		return st.update(mutate)
	}).Maybe()

	mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, mock.Anything, mock.Anything).Return(func(_ context.Context, _ string, userID string, notif types.UserNotificationSettings) error {
		_, _, _, err := st.update(func(cur types.Settings, ver int) (types.Settings, int, bool, error) {
			if reflect.DeepEqual(notif, types.UserNotificationSettings{}) {
				if len(cur.Notifications) == 0 {
					return cur, ver, false, nil
				}
				if _, ok := cur.Notifications[userID]; !ok {
					return cur, ver, false, nil
				}
				delete(cur.Notifications, userID)
				if len(cur.Notifications) == 0 {
					cur.Notifications = nil
				}
				return cur, ver, true, nil
			}
			if cur.Notifications == nil {
				cur.Notifications = make(map[string]types.UserNotificationSettings)
			} else if existing, ok := cur.Notifications[userID]; ok && reflect.DeepEqual(existing, notif) {
				return cur, ver, false, nil
			}
			cur.Notifications[userID] = notif
			return cur, ver, true, nil
		})
		return err
	}).Maybe()

	return st
}

func expectUpdateSettings(
	mockS *mockStorage,
	ctxMatcher any,
	siteIDMatcher any,
	initial types.Settings,
	initialVersion int,
	settingsMatcher func(types.Settings) bool,
	expectedVersion any,
	retTime time.Time,
	retErr error,
) *mock.Call {
	type evalResult struct {
		matched bool
		updated types.Settings
		newVer  int
		err     error
	}

	var mu sync.Mutex
	curSettings := initial
	curVer := initialVersion
	var pending evalResult
	var hasPending bool
	var everMatched bool

	return mockS.On("UpdateSettings", ctxMatcher, siteIDMatcher, mock.MatchedBy(func(mutate storage.SettingsMutator) bool {
		if mutate == nil {
			return false
		}
		mu.Lock()
		if !mockS.InUpdateSettingsCall() {
			matched := everMatched
			mu.Unlock()
			return matched
		}
		if hasPending {
			matched := pending.matched
			mu.Unlock()
			return matched
		}
		s, v := curSettings, curVer
		mu.Unlock()

		updated, newVer, save, err := mutate(s, v)
		matched := err == nil && save
		if matched {
			if expectedVer, ok := expectedVersion.(int); ok && newVer != expectedVer {
				matched = false
			}
		}
		if matched && settingsMatcher != nil && !settingsMatcher(updated) {
			matched = false
		}

		if matched {
			mu.Lock()
			pending = evalResult{
				matched: true,
				updated: updated,
				newVer:  newVer,
				err:     err,
			}
			hasPending = true
			everMatched = true
			mu.Unlock()
		}
		return matched
	})).Return(func(_ context.Context, _ string, mutate storage.SettingsMutator) (types.Settings, int, time.Time, error) {
		mu.Lock()
		res := pending
		ok := hasPending
		hasPending = false
		s, v := curSettings, curVer
		mu.Unlock()

		if !ok {
			updated, newVer, _, err := mutate(s, v)
			res = evalResult{updated: updated, newVer: newVer, err: err}
		}
		if res.err != nil {
			return types.Settings{}, 0, time.Time{}, res.err
		}
		if retErr != nil {
			return types.Settings{}, 0, retTime, retErr
		}
		mu.Lock()
		curSettings = res.updated
		curVer = res.newVer
		mu.Unlock()
		return res.updated, res.newVer, retTime, nil
	})
}

type mockUtility struct {
	mock.Mock
}

func (m *mockUtility) Name() string {
	return "mock"
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
func (m *mockUtility) GetVPPInfo(ctx context.Context) (types.UtilityVPPInfo, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.UtilityVPPInfo), args.Error(1)
	}
	return types.UtilityVPPInfo{}, nil
}
func (m *mockUtility) GetPeriods(ctx context.Context) ([]types.TimePeriod, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).([]types.TimePeriod), args.Error(1)
	}
	return nil, nil
}

type mockESS struct {
	mock.Mock
	MockName string
}

func (m *mockESS) Name() string {
	if m.MockName != "" {
		return m.MockName
	}
	return "mock"
}
func (m *mockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	args := m.Called(ctx)
	if len(args) > 0 {
		return args.Get(0).(types.SystemStatus), args.Error(1)
	}
	return types.SystemStatus{}, nil
}
func (m *mockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error) {
	args := m.Called(ctx, bat, sol, opts)
	if len(args) > 1 {
		return args.Bool(0), args.Error(1)
	}
	return false, args.Error(0)
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
func (m *mockESS) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error) {
	for _, call := range m.ExpectedCalls {
		if call.Method == "GetEnergyHistory" {
			args := m.Called(ctx, start, end)
			if len(args) > 0 {
				return args.Get(0).([]types.DailyEnergyStats), args.Error(1)
			}
			return nil, args.Error(0)
		}
	}
	return nil, nil
}
func (m *mockESS) GridSettings(ctx context.Context) (types.GridSettings, error) {
	for _, call := range m.ExpectedCalls {
		if call.Method == "GridSettings" {
			args := m.Called(ctx)
			if len(args) > 0 {
				return args.Get(0).(types.GridSettings), args.Error(1)
			}
			return types.GridSettings{}, args.Error(0)
		}
	}
	return types.GridSettings{
		GridChargeBatteries: true,
		GridExportSolar:     true,
		GridExportBatteries: false,
	}, nil
}
func (m *mockESS) Validate() error {
	args := m.Called()
	return args.Error(0)
}

type mockWeather struct {
	mock.Mock
}

func (m *mockWeather) Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error) {
	args := m.Called(ctx, countryCode, postalCode)
	return args.Get(0).(types.SiteLocation), args.Error(1)
}

func (m *mockWeather) Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error) {
	args := m.Called(ctx, loc, startDate, endDate)
	return args.Get(0).([]types.Weather), args.Error(1)
}
