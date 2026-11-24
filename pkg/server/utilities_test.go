package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestHandleListUtilities(t *testing.T) {
	mockUMap := utility.NewMap(nil)
	mockE := ess.NewMap()

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockE,
		storage:    nil,
		controller: controller.NewController(),
		bypassAuth: true,
	}

	t.Run("Returns JSON array of utilities", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "public, max-age=3600", resp.Header.Get("Cache-Control"))

		var utilities []types.UtilityProviderInfo
		err := json.NewDecoder(w.Body).Decode(&utilities)
		require.NoError(t, err)
		assert.NotEmpty(t, utilities, "expected at least one utility in the response")
	})

	t.Run("Contains comed with correct structure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		var comedInfo *types.UtilityProviderInfo
		for i := range utilities {
			if utilities[i].ID == "comed" {
				comedInfo = &utilities[i]
				break
			}
		}
		require.NotNil(t, comedInfo, "comed should be present in the utilities list")
		assert.Equal(t, "Commonwealth Edison (ComEd)", comedInfo.Name)

		var comedBeshRate *types.UtilityRateInfo
		for i := range comedInfo.Rates {
			if comedInfo.Rates[i].ID == "comed_besh" {
				comedBeshRate = &comedInfo.Rates[i]
				break
			}
		}
		require.NotNil(t, comedBeshRate, "comed_besh should be present in the comed rates list")
		assert.NotEmpty(t, comedBeshRate.Name)
		require.Len(t, comedBeshRate.Options, 3)

		// rateClass option
		rateClassOpt := comedBeshRate.Options[0]
		assert.Equal(t, "rateClass", rateClassOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSelect, rateClassOpt.Type)
		assert.NotEmpty(t, rateClassOpt.Choices)

		// variableDeliveryRate option
		dtodOpt := comedBeshRate.Options[1]
		assert.Equal(t, "variableDeliveryRate", dtodOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, dtodOpt.Type)
		assert.NotEmpty(t, dtodOpt.Description)

		// netMeteringCredits option
		nmOpt := comedBeshRate.Options[2]
		assert.Equal(t, "netMeteringCredits", nmOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, nmOpt.Type)
		assert.NotEmpty(t, nmOpt.Description)
	})

	t.Run("All options have required fields", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		for _, u := range utilities {
			assert.NotEmpty(t, u.ID, "utility must have an ID")
			assert.NotEmpty(t, u.Name, "utility must have a name")
			for _, rate := range u.Rates {
				assert.NotEmpty(t, rate.ID, "rate must have an ID")
				assert.NotEmpty(t, rate.Name, "rate must have a name")
				for _, opt := range rate.Options {
					assert.NotEmpty(t, opt.Field, "option must have a Field in rate %q", rate.ID)
					assert.NotEmpty(t, opt.Name, "option must have a name in rate %q", rate.ID)
					assert.True(t,
						opt.Type == types.UtilityOptionTypeSelect || opt.Type == types.UtilityOptionTypeSwitch,
						"option %q in rate %q has invalid type %q", opt.Field, rate.ID, opt.Type)

					if opt.Type == types.UtilityOptionTypeSelect {
						assert.NotEmpty(t, opt.Choices, "select option %q in rate %q must have choices", opt.Field, rate.ID)
						for _, c := range opt.Choices {
							assert.NotEmpty(t, c.Value)
							assert.NotEmpty(t, c.Name)
						}
					}

					for depField, allowedVals := range opt.DependsOn {
						assert.NotEmpty(t, allowedVals, "dependsOn %q for option %q in rate %q must have allowed values", depField, opt.Field, rate.ID)
						foundDep := false
						for _, other := range rate.Options {
							if other.Field == depField {
								foundDep = true
								break
							}
						}
						assert.True(t, foundDep, "dependsOn %q for option %q in rate %q must reference an option on the same rate", depField, opt.Field, rate.ID)
					}
				}
			}
		}
	})

	t.Run("Accessible via setup handler through auth bypass", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockESS := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockESS)

		s := &Server{
			utilities:          mockUMap,
			ess:                mockP,
			storage:            mockStorage,
			controller:         controller.NewController(),
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}

		handler := s.setupHandler()
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))
		assert.NotEmpty(t, utilities)
	})

	t.Run("Does not clear Hidden flags when showHidden=false", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: false,
		}

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		// Just check that they parsed. Since mockUMap returns real utilities, and none are hidden right now,
		// we mainly check it runs correctly without modifying them.
		assert.NotEmpty(t, utilities)
	})

	t.Run("Clears Hidden flags when showHidden=true", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: true,
		}

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		assert.NotEmpty(t, utilities)
		for _, u := range utilities {
			assert.False(t, u.Hidden, "hidden flag should be false when showHidden is true")
		}
	})

	t.Run("Passes authMiddleware for unregistered user", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		mockStorage := &mockStorage{}
		mockStorage.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
		mockStorage.On("GetUser", mock.Anything, "google:unregistered@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		srv := &Server{
			utilities:          mockUMap,
			ess:                mockE,
			storage:            mockStorage,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		token := generateTestToken(t, oidcSrv.URL, priv, "unregistered@example.com", "unregistered@example.com")
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))
		assert.NotEmpty(t, utilities)
		assert.True(t, mockStorage.AssertExpectations(t))
	})

	t.Run("Passes authMiddleware for unauthenticated user", func(t *testing.T) {
		srv := &Server{
			utilities:          mockUMap,
			ess:                mockE,
			bypassAuth:         false,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}

		handler := srv.setupHandler()
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))
		assert.NotEmpty(t, utilities)
	})
}

func TestHandleGetPeriods(t *testing.T) {
	mockUMap := utility.NewMap(&mockStorage{})
	mockE := ess.NewMap()

	t.Run("Returns periods for TOU rate", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))

		var periods []types.TimePeriod
		err := json.NewDecoder(w.Body).Decode(&periods)
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.NotEmpty(t, periods[0].Name)
		}
	})

	t.Run("Returns null for non-TOU rate", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "null\n", w.Body.String())
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Returns periods for overridden utility rate via query params", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_bes",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods?utilityProvider=pg_e&utilityRate=pg_e_e_tou_c", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))

		var periods []types.TimePeriod
		err := json.NewDecoder(w.Body).Decode(&periods)
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.NotEmpty(t, periods[0].Name)
		}
	})

	t.Run("Does not mutate cached site provider or log rate change warning during preview", func(t *testing.T) {
		mockStorage := &mockStorage{}
		savedSettings := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
			UtilityFeesPeriods: []types.UtilityFeesPeriod{
				{
					TimePeriod:    types.TimePeriod{Name: "Saved Custom Peak"},
					DollarsPerKWH: 0.12,
				},
			},
		}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(savedSettings, types.CurrentSettingsVersion, time.Time{}, nil)

		uMap := utility.Configured(mockStorage)
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx := log.With(context.Background(), logger)

		cached, err := uMap.Site(ctx, "test-site", savedSettings)
		require.NoError(t, err)

		srv := &Server{
			utilities:  uMap,
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods?utilityProvider=pg_e&utilityRate=pg_e_e_tou_c", nil)
		reqCtx := context.WithValue(log.With(req.Context(), logger), siteIDContextKey, "test-site")
		req = req.WithContext(reqCtx)
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.NotContains(t, logBuf.String(), "site changed utility rate")

		after, err := uMap.Site(ctx, "test-site", savedSettings)
		require.NoError(t, err)
		assert.Same(t, cached, after)
		cachedPeriods, err := after.GetPeriods(ctx)
		require.NoError(t, err)
		if assert.Len(t, cachedPeriods, 1) {
			assert.Equal(t, "Saved Custom Peak", cachedPeriods[0].Name)
		}
	})

	t.Run("Changing utilityProvider clears inherited rate, options, and custom periods", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
			UtilityFeesPeriods: []types.UtilityFeesPeriod{
				{
					TimePeriod:    types.TimePeriod{Name: "Saved Custom Period"},
					DollarsPerKWH: 0.15,
				},
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		// Provider selected without rate returns 200 null rather than inheriting old rate or custom periods
		req := httptest.NewRequest("GET", "/api/utility/periods?utilityProvider=comed", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.Equal(t, "null\n", w.Body.String())

		// Switching to Origin without options clears inherited "oncor" location and returns 400
		req2 := httptest.NewRequest("GET", "/api/utility/periods?utilityProvider=origin&utilityRate=origin_solar_saver", nil)
		req2 = req2.WithContext(context.WithValue(req2.Context(), siteIDContextKey, "test-site"))
		w2 := httptest.NewRecorder()

		srv.handleGetPeriods(w2, req2)

		assert.Equal(t, http.StatusBadRequest, w2.Result().StatusCode)
	})

	t.Run("Changing utilityRate clears inherited options and custom periods", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_fixed",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
			UtilityFeesPeriods: []types.UtilityFeesPeriod{
				{
					TimePeriod:    types.TimePeriod{Name: "Saved Custom Period"},
					DollarsPerKWH: 0.15,
				},
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		// Empty utilityRate returns 200 null
		reqEmpty := httptest.NewRequest("GET", "/api/utility/periods?utilityRate=", nil)
		reqEmpty = reqEmpty.WithContext(context.WithValue(reqEmpty.Context(), siteIDContextKey, "test-site"))
		wEmpty := httptest.NewRecorder()

		srv.handleGetPeriods(wEmpty, reqEmpty)

		assert.Equal(t, http.StatusOK, wEmpty.Result().StatusCode)
		assert.Equal(t, "null\n", wEmpty.Body.String())

		// Changing rate without passing options clears inherited Location: "oncor"
		reqNewRate := httptest.NewRequest("GET", "/api/utility/periods?utilityRate=tesla_dynamic", nil)
		reqNewRate = reqNewRate.WithContext(context.WithValue(reqNewRate.Context(), siteIDContextKey, "test-site"))
		wNewRate := httptest.NewRecorder()

		srv.handleGetPeriods(wNewRate, reqNewRate)

		assert.Equal(t, http.StatusBadRequest, wNewRate.Result().StatusCode)
		assert.Contains(t, wNewRate.Body.String(), "tdu location is required to determine ercot load zone")
	})

	t.Run("Explicit empty utilityRateOptions overrides saved options", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods?utilityRateOptions=%7B%7D", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "tdu location is required to determine ercot load zone")
	})

	t.Run("Returns 400 and logs warning on invalid JSON or invalid utility options", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		var jsonLogBuf bytes.Buffer
		jsonLogger := slog.New(slog.NewJSONHandler(&jsonLogBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		reqBadJSON := httptest.NewRequest("GET", "/api/utility/periods?utilityRateOptions=%7Binvalid", nil)
		reqBadJSON = reqBadJSON.WithContext(context.WithValue(log.With(reqBadJSON.Context(), jsonLogger), siteIDContextKey, "test-site"))
		wBadJSON := httptest.NewRecorder()

		srv.handleGetPeriods(wBadJSON, reqBadJSON)

		assert.Equal(t, http.StatusBadRequest, wBadJSON.Result().StatusCode)
		assert.Contains(t, wBadJSON.Body.String(), "invalid utility rate options")
		assert.Contains(t, jsonLogBuf.String(), `"level":"WARN"`)
		assert.NotContains(t, jsonLogBuf.String(), `"level":"ERROR"`)

		var optLogBuf bytes.Buffer
		optLogger := slog.New(slog.NewJSONHandler(&optLogBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		reqBadOpt := httptest.NewRequest("GET", `/api/utility/periods?utilityProvider=comed&utilityRate=comed_besh&utilityRateOptions=%7B%22rateClass%22:%22invalid%22%7D`, nil)
		reqBadOpt = reqBadOpt.WithContext(context.WithValue(log.With(reqBadOpt.Context(), optLogger), siteIDContextKey, "test-site"))
		wBadOpt := httptest.NewRecorder()

		srv.handleGetPeriods(wBadOpt, reqBadOpt)

		assert.Equal(t, http.StatusBadRequest, wBadOpt.Result().StatusCode)
		assert.Contains(t, wBadOpt.Body.String(), "invalid utility provider settings")
		assert.Contains(t, optLogBuf.String(), `"level":"WARN"`)
		assert.NotContains(t, optLogBuf.String(), `"level":"ERROR"`)
	})
}
