package ess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFranklin(t *testing.T) {
	t.Run("GetStatus", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0, "peHwVerList": []int{0, 20}},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"globalGridChargeMax": 15.0, "gridFeedMaxFlag": 2, "gridMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 138224.0, "workMode": 2, "soc": 88.5}, // Matches current SOC -> Standby
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 138224.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]any{
					"soc":       88.5,
					"p_fhp":     1500.0,
					"mode":      138224.0, // Self consumption ID
					"timestamp": time.Now().Unix(),
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid":           true,
						"runtimeData":     runtimeData,
						"currentWorkMode": 2,
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
			settings:    types.Settings{MinBatterySOC: 10},
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err, "GetStatus should succeed")

		assert.Equal(t, 88.5, status.BatterySOC, "BatterySOC should match")
		assert.Equal(t, 30.0, status.BatteryCapacityKWH, "BatteryCapacityKWH should match")
		assert.Equal(t, 13.0, status.MaxBatteryChargeKW, "MaxBatteryChargeKW should match 5kW + 8kW")
		assert.Equal(t, 15.0, status.MaxBatteryDischargeKW, "MaxBatteryDischargeKW should match 5kW + 10kW")
		assert.True(t, status.ElevatedMinBatterySOC, "ElevatedMinBatterySOC should be true")
		assert.True(t, status.BatteryAboveMinSOC, "BatteryAboveMinSOC should be true")
		assert.False(t, status.ManagedTOUMode, "ManagedTOUMode should be false for self-consumption")
		assert.NotEmpty(t, status.TimeLocation, "TimeLocation should be populated")
	})

	t.Run("GetStatus ManagedTOUMode", func(t *testing.T) {
		t.Run("Direct Solar Export", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0, "peHwVerList": []int{0}}})
					return
				}
				if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"globalGridChargeMax": 15.0}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
					list := []map[string]any{
						{"id": 11111.0, "workMode": 1, "soc": 20.0, "name": "Direct Solar Export"}, // RateRudder provisioned TOU active
					}
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"list": list, "currendId": 11111.0},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid":           true,
							"runtimeData":     map[string]any{"soc": 50.0, "timestamp": time.Now().Unix()},
							"currentWorkMode": 1,
						},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:      ts.Client(),
				baseURL:     ts.URL,
				username:    "u",
				md5Password: "p",
				gatewayID:   "g",
				settings:    types.Settings{MinBatterySOC: 10},
			}

			status, err := f.GetStatus(context.Background())
			require.NoError(t, err, "GetStatus should succeed")
			assert.True(t, status.ManagedTOUMode, "ManagedTOUMode should be true when workMode is 1 and name is Direct Solar Export")
		})

		t.Run("Customer Utility Plan", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0, "peHwVerList": []int{0}}})
					return
				}
				if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
					json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"globalGridChargeMax": 15.0}})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
					list := []map[string]any{
						{"id": 11111.0, "workMode": 1, "soc": 20.0, "name": "Time of Use"}, // Customer TOU active
					}
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"list": list, "currendId": 11111.0},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid":           true,
							"runtimeData":     map[string]any{"soc": 50.0, "timestamp": time.Now().Unix()},
							"currentWorkMode": 1,
						},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:      ts.Client(),
				baseURL:     ts.URL,
				username:    "u",
				md5Password: "p",
				gatewayID:   "g",
				settings:    types.Settings{MinBatterySOC: 10},
			}

			status, err := f.GetStatus(context.Background())
			require.NoError(t, err, "GetStatus should succeed")
			assert.False(t, status.ManagedTOUMode, "Customer utility TOU must not report ManagedTOUMode")
		})
	})

	t.Run("GetStatus Invalid", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": []map[string]any{}}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": false,
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		_, err := f.GetStatus(context.Background())
		require.Error(t, err)
		assert.ErrorContains(t, err, "getDeviceCompositeInfo returned invalid status")
	})

	t.Run("GetStatus Invalid Status Retry Success", func(t *testing.T) {
		var compositeAttempts int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": []map[string]any{}}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				compositeAttempts++
				if compositeAttempts < 3 {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid": false,
						},
					})
				} else {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid": true,
							"runtimeData": map[string]any{
								"soc": 75.0,
							},
						},
					})
				}
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 75.0, status.BatterySOC)
		assert.Equal(t, 3, compositeAttempts)
	})

	t.Run("GetStatus Stale Runtime Data Retry", func(t *testing.T) {
		var compositeAttempts int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": []map[string]any{}}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				compositeAttempts++
				if compositeAttempts == 1 {
					assert.Equal(t, "1", r.URL.Query().Get("refreshFlag"))
					// Stale data timestamp (6 minutes old)
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid": true,
							"runtimeData": map[string]any{
								"soc":       50.0,
								"timestamp": time.Now().Add(-6 * time.Minute).Unix(),
							},
						},
					})
				} else {
					assert.Equal(t, "0", r.URL.Query().Get("refreshFlag"))
					// Fresh data timestamp
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"valid": true,
							"runtimeData": map[string]any{
								"soc":       80.0,
								"timestamp": time.Now().Unix(),
							},
						},
					})
				}
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 80.0, status.BatterySOC)
		assert.Equal(t, 2, compositeAttempts)
	})

	t.Run("GetStatus Retry On Rate Limit (429)", func(t *testing.T) {
		compositeAttempts := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": []map[string]any{}}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				compositeAttempts++
				if compositeAttempts == 1 {
					assert.Equal(t, "1", r.URL.Query().Get("refreshFlag"))
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}
				assert.Equal(t, "0", r.URL.Query().Get("refreshFlag"))
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       75.0,
							"timestamp": time.Now().Unix(),
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 75.0, status.BatterySOC)
		assert.Equal(t, 2, compositeAttempts)
	})

	t.Run("GetStatus Retry Retains RefreshFlag 0 On Attempt 3", func(t *testing.T) {
		compositeAttempts := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"totalCap": 30.0}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": []map[string]any{}}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				compositeAttempts++
				if compositeAttempts == 1 {
					assert.Equal(t, "1", r.URL.Query().Get("refreshFlag"))
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}
				if compositeAttempts == 2 {
					assert.Equal(t, "0", r.URL.Query().Get("refreshFlag"))
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}
				assert.Equal(t, "0", r.URL.Query().Get("refreshFlag"))
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       70.0,
							"timestamp": time.Now().Unix(),
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 70.0, status.BatterySOC)
		assert.Equal(t, 3, compositeAttempts)
	})

	t.Run("GetStatus Grid Status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": []map[string]any{}},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"offGirdFlag": 1,
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, status.GridUnavailable)
	})

	t.Run("GetStatus VPP Active", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": []map[string]any{}},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":  75.0,
							"mode": 9,
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, status.VPPActive)
	})

	t.Run("GetStatus VPP Applicable and Future Event", func(t *testing.T) {
		var futureEventStart string
		var futureEventEnd string
		var latestEventStatus int
		var ehEventsResult []map[string]any

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"list": []map[string]any{},
						"vppSocVo": map[string]any{
							"vppSocDisplayFlag": true,
							"vppSoc":            20.0,
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc": 75.0,
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/queryProgramDetails" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"latestEventId":        "def9ae16-24d0-4333-9d5d-d6cf8452d64a",
						"latestEventStartTime": futureEventStart,
						"latestEventEndTime":   futureEventEnd,
						"programName":          "Eversource-ess",
						"vppSoc":               20.0,
						"latestEventStatus":    latestEventStatus,
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/queryEHEvents" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  ehEventsResult,
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		// 1. One future event in program details, matching one in queryEHEvents (status = 2, OptOut = false)
		futureEventStart = time.Now().UTC().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
		futureEventEnd = time.Now().UTC().Add(4 * time.Hour).Format("2006-01-02 15:04:05")
		latestEventStatus = 2
		ehEventsResult = []map[string]any{
			{
				"eventId":     "def9ae16-24d0-4333-9d5d-d6cf8452d64a",
				"vppSoc":      15.0,
				"startTime":   futureEventStart,
				"endTime":     futureEventEnd,
				"eventStatus": 2,
			},
		}
		status, err := f.GetStatus(context.Background())
		require.NoError(t, err)
		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, "Eversource-ess", status.VPPEvents[0].Description)
			assert.Equal(t, 15.0, status.VPPEvents[0].VPPSoc)
			assert.False(t, status.VPPEvents[0].OptOut)
		}

		// 2. Multiple upcoming events in queryEHEvents (deduplicated against program details and sorted by TSStart)
		futureEventStart = time.Now().UTC().Add(5 * time.Hour).Format("2006-01-02 15:04:05")
		futureEventEnd = time.Now().UTC().Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		latestEventStatus = 2
		ehEventsResult = []map[string]any{
			{
				"eventId":     "id1",
				"vppSoc":      20.0,
				"startTime":   time.Now().UTC().Add(3 * time.Hour).Format("2006-01-02 15:04:05"),
				"endTime":     time.Now().UTC().Add(5 * time.Hour).Format("2006-01-02 15:04:05"),
				"eventStatus": 3, // OptOut = true
			},
			{
				"eventId":     "id2",
				"vppSoc":      25.0,
				"startTime":   time.Now().UTC().Add(1 * time.Hour).Format("2006-01-02 15:04:05"),
				"endTime":     time.Now().UTC().Add(3 * time.Hour).Format("2006-01-02 15:04:05"),
				"eventStatus": 2, // OptOut = false
			},
			{
				"eventId":     "def9ae16-24d0-4333-9d5d-d6cf8452d64a",
				"vppSoc":      30.0,
				"startTime":   futureEventStart,
				"endTime":     futureEventEnd,
				"eventStatus": 0, // OptOut = false
			},
		}
		status, err = f.GetStatus(context.Background())
		require.NoError(t, err)
		if assert.Len(t, status.VPPEvents, 3) {
			// Sorting order verification:
			// Event id2 (1 hour out) -> Event id1 (3 hours out) -> Event def9ae16 (5 hours out)
			assert.Equal(t, 25.0, status.VPPEvents[0].VPPSoc)
			assert.False(t, status.VPPEvents[0].OptOut)

			assert.Equal(t, 20.0, status.VPPEvents[1].VPPSoc)
			assert.True(t, status.VPPEvents[1].OptOut)

			assert.Equal(t, 30.0, status.VPPEvents[2].VPPSoc)
			assert.False(t, status.VPPEvents[2].OptOut)
		}

		// 3. Fallback to queryProgramDetails if queryEHEvents is empty
		futureEventStart = time.Now().UTC().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
		futureEventEnd = time.Now().UTC().Add(4 * time.Hour).Format("2006-01-02 15:04:05")
		latestEventStatus = 4 // OptOut = true
		ehEventsResult = []map[string]any{}
		status, err = f.GetStatus(context.Background())
		require.NoError(t, err)
		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, 20.0, status.VPPEvents[0].VPPSoc)
			assert.True(t, status.VPPEvents[0].OptOut)
		}

		// 4. Past latest event (no events returned even if queryEHEvents has future events)
		futureEventStart = time.Now().UTC().Add(-4 * time.Hour).Format("2006-01-02 15:04:05")
		futureEventEnd = time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
		latestEventStatus = 2
		ehEventsResult = []map[string]any{
			{
				"eventId":     "id1",
				"vppSoc":      20.0,
				"startTime":   time.Now().UTC().Add(3 * time.Hour).Format("2006-01-02 15:04:05"),
				"endTime":     time.Now().UTC().Add(5 * time.Hour).Format("2006-01-02 15:04:05"),
				"eventStatus": 2,
			},
		}
		status, err = f.GetStatus(context.Background())
		require.NoError(t, err)
		assert.Empty(t, status.VPPEvents)
	})

	t.Run("GetStatus Alarms", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc": 50.0,
						},
						"currentAlarmVOList": []map[string]any{
							{
								"logName":          "Test Alarm",
								"alarmExplanation": "Test Description",
								"alarmCode":        "E123",
								"time":             "2023-10-27 12:00:00",
							},
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
			settings:    types.Settings{MinBatterySOC: 10},
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err, "GetStatus should succeed")
		require.Len(t, status.Alarms, 1, "should have 1 alarm")
		assert.Equal(t, "Test Alarm", status.Alarms[0].Name)
		assert.Equal(t, "Test Description", status.Alarms[0].Description)
		assert.Equal(t, "E123", status.Alarms[0].Code)

		expectedTime, _ := time.Parse(time.DateTime, "2023-10-27 12:00:00")
		assert.Equal(t, expectedTime.UTC(), status.Alarms[0].Timestamp.UTC())
	})

	t.Run("GetStatus Alarms Filtered", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc": 50.0,
						},
						"currentAlarmVOList": []map[string]any{
							{
								"logName":          "SIM card not inserted",
								"alarmExplanation": "Ignore this",
								"alarmCode":        "E001",
								"time":             "2023-10-27 12:00:00",
							},
							{
								"logName":          "No PV Current Detected",
								"alarmExplanation": "Ignore this too",
								"alarmCode":        "1032",
								"time":             "2023-10-27 12:00:02",
							},
							{
								"logName":          "Real Alarm",
								"alarmExplanation": "Don't ignore this",
								"alarmCode":        "E002",
								"time":             "2023-10-27 12:00:01",
							},
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
			settings:    types.Settings{MinBatterySOC: 10},
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err, "GetStatus should succeed")
		if assert.Len(t, status.Alarms, 1, "should have only 1 alarm (SIM card and PV alarms should be ignored)") {
			assert.Equal(t, "Real Alarm", status.Alarms[0].Name)
		}
	})

	t.Run("SetModes", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111.0, "workMode": 1}, // TOU
					{"id": 22222.0, "workMode": 2}, // Self-consumption
					{"id": 33333.0, "workMode": 3}, // Backup
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				// We expect SetModes(BatteryModeLoad) -> soc=MinBatterySOC (e.g. 20)
				// This test setup is specific to how SetModes is implemented
				// For Load/SelfConsumption, it sets mode 2 (self-consumption).
				assert.Equal(t, "2", r.Form.Get("workMode"), "workMode should be 2")
				assert.Equal(t, "22222", r.Form.Get("currendId"), "currendId should match")

				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		// Set settings so MinBatterySOC is set
		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err, "ApplySettings should succeed")

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.True(t, changed)

		// Verify the expected call was made
		require.Len(t, callOrder, 1, "updateTouModeV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Smart Dispatch Mode Transitions To Self-Consumption", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true, "currentWorkMode": 7},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111.0, "workMode": 1},
					{"id": 22222.0, "workMode": 2, "editSocFlag": true},
					{"id": 33333.0, "workMode": 3},
					{"id": 186440.0, "workMode": 7, "name": "Smart Energy Dispatch"},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 186440.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "2", r.Form.Get("workMode"))
				assert.Equal(t, "22222", r.Form.Get("currendId"))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}
		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		require.Len(t, callOrder, 1)
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Charge Fallback Backup", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 10.0, "workMode": 1},
					{"id": 20.0, "workMode": 2, "editSocFlag": true},
					{"id": 30.0, "workMode": 3},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				// Fallback to Backup Mode (workMode 3) when gridMaxFlag is 1 (disabled)
				assert.Equal(t, "3", r.Form.Get("workMode"), "workMode should be 3 for backup fallback")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{GridChargeBatteries: true})
		require.NoError(t, err, "ApplySettings should succeed")
		changed, err := f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.True(t, changed)

		require.Len(t, callOrder, 1, "updateTouModeV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Both Mode and PowerControl Updates", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "editSocFlag": true},
					{"id": 30.0, "workMode": 3},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "100", r.Form.Get("soc"), "soc should be 100 for ChargeAny")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": nil})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				assert.EqualValues(t, 1, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 1 for SolarModeAny with GridExportSolar=true")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC:   20,
			GridExportSolar: true,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.True(t, changed)

		require.Len(t, callOrder, 1, "updateSocV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0])
	})

	t.Run("SetModes NoChange", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			http.Error(w, "should not be called: "+r.URL.Path+" "+r.Method, 500)
		}))
		defer ts.Close()
		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			tokenStr:  "valid-token",
			gatewayID: "anything",
		}
		changed, err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed (noop)")
		assert.False(t, changed)
	})

	t.Run("SetModes Partial NoChange", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "soc": 55.0},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 3 (no export) since SolarModeAny with GridExportSolar=false (default)
				assert.EqualValues(t, 3, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 3 for no export")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		changed, err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.False(t, changed)

		// Verify no calls were made (BatteryModeNoChange and power control updates disabled)
		require.Empty(t, callOrder, "no power control update calls should be made")
	})

	t.Run("SetModes UpdateSOC Only", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "soc": 55.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "20", r.Form.Get("soc"), "soc should be updated to MinBatterySOC")
				assert.Equal(t, "2", r.Form.Get("workMode"))
				assert.Equal(t, "1", r.Form.Get("electricityType"))

				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": nil})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				t.Error("Should not call updateTouModeV2")
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.True(t, changed)

		// Verify only updateSocV2 was called (not updateTouModeV2)
		require.Len(t, callOrder, 1, "only updateSocV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0])
	})

	t.Run("SetModes Saved Successfully Warning", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "soc": 55.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				json.NewEncoder(w).Encode(map[string]any{
					"code":    201,
					"success": false,
					"message": "Saved successfully. The data will be synchronized later",
				})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed despite the warning response")
		assert.True(t, changed)

		require.Len(t, callOrder, 1, "updateSocV2 should be called")
	})

	t.Run("SetModes Avoid Small SOC Update", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc": 55.9,
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "soc": 56.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				t.Error("Should not call updateSocV2")
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				t.Error("Should not call updateTouModeV2")
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{MinBatterySOC: 20})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.False(t, changed)

		require.Empty(t, callOrder, "no updates should be called")
	})

	t.Run("SetModes Storm Hedge Grid Charge", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid":       true,
						"runtimeData": map[string]any{"mode": 6}, // Storm Hedge (TOUID == 6)
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11.0, "workMode": 2, "electricityType": 1},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 11.0},
				})
				return
			}
			http.Error(w, "should not be called: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("SetModes Backup Transition To SelfConsumption", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20.0, "workMode": 2, "electricityType": 1},
					{"id": 30.0, "workMode": 3, "electricityType": 1},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 30.0}, // currently in backup mode 3
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"gridFeedMax":     -1.0,
						"gridFeedMaxFlag": 3,
						"gridMax":         -1.0,
						"gridMaxFlag":     1,
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "2", r.Form.Get("workMode"), "should transition workMode to 2 (self consumption)")
				assert.Equal(t, "20", r.Form.Get("soc"), "should set reserve soc to MinBatterySOC 20")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: false,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange, types.ModesOptions{})
		require.NoError(t, err)
		assert.True(t, changed)
		require.Len(t, callOrder, 1)
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes SolarExport State 1 Dispatch 1", func(t *testing.T) {
		var saveDispatchCalled bool
		var updateTouModeCalled bool
		var savedPayload map[string]any
		now := time.Now()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       70.0,
							"timestamp": now.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111.0, "workMode": 1, "soc": 50.0}, // TOU
					{"id": 22222.0, "workMode": 2, "soc": 20.0}, // Self-consumption
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 22222.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				require.NoError(t, json.NewDecoder(r.Body).Decode(&savedPayload))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				updateTouModeCalled = true
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "1", r.Form.Get("workMode"))
				assert.Equal(t, "11111", r.Form.Get("currendId"))
				assert.Equal(t, "20", r.Form.Get("soc"))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := now.Add(4 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled, "saveTouDispatch should be called")
		assert.True(t, updateTouModeCalled, "updateTouModeV2 should be called")

		// Verify 24h schedule strategy structure and fallback
		strategyList, ok := savedPayload["strategyList"].([]any)
		require.True(t, ok)
		strategy := strategyList[0].(map[string]any)
		dayTypeList := strategy["dayTypeVoList"].([]any)
		dayType := dayTypeList[0].(map[string]any)
		detailVoList := dayType["detailVoList"].([]any)

		roundedStart := roundTOUPeriodStart(now)
		roundedUntil := roundTOUPeriodEnd(until)
		expectedStart := roundedStart.Format("15:04")
		expectedEnd := roundedUntil.Format("15:04")
		if (roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0) || roundedUntil.Day() != roundedStart.Day() {
			expectedEnd = "24:00"
		}

		crossesMidnight := roundedUntil.Day() != roundedStart.Day() && !(roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0)

		// Check windows
		for _, v := range detailVoList {
			w := v.(map[string]any)
			if w["startHourTime"] == expectedStart {
				assert.EqualValues(t, franklinDispatchAPowerToHome, w["dispatchId"], "active window should have dispatchId: 1")
				assert.Equal(t, expectedEnd, w["endHourTime"])
			} else if crossesMidnight && w["startHourTime"] == "00:00" {
				assert.EqualValues(t, franklinDispatchAPowerToHome, w["dispatchId"], "morning window should have dispatchId: 1")
			} else {
				assert.EqualValues(t, franklinDispatchSelfConsumption, w["dispatchId"], "off-peak fallback window should have dispatchId: 6")
			}
		}
	})

	t.Run("SetModes SolarExport State 2 Dispatch 2", func(t *testing.T) {
		var saveDispatchCalled bool
		var updateTouModeCalled bool
		var savedPayload map[string]any
		now := time.Now()

		currentTouID := 22222.0
		currentTouSOC := 20.0

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       65.4,
							"timestamp": now.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111.0, "workMode": 1, "soc": currentTouSOC}, // TOU
					{"id": 22222.0, "workMode": 2, "soc": 20.0},          // Self-consumption
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": currentTouID},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"template": map[string]any{
							"id": 1,
						},
						"strategyList": savedPayload["strategyList"],
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				require.NoError(t, json.NewDecoder(r.Body).Decode(&savedPayload))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				updateTouModeCalled = true
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "1", r.Form.Get("workMode"))
				assert.Equal(t, "11111", r.Form.Get("currendId"))
				assert.Equal(t, "65", r.Form.Get("soc"), "reserve SOC should be locked to floor of current SOC")
				currentTouID = 11111.0
				currentTouSOC = 65.0
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := now.Add(4 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled, "saveTouDispatch should be called")
		assert.True(t, updateTouModeCalled, "updateTouModeV2 should be called")

		// Verify active window has dispatchId: 2 (aPower on standby)
		strategyList, ok := savedPayload["strategyList"].([]any)
		require.True(t, ok)
		strategy := strategyList[0].(map[string]any)
		dayTypeList := strategy["dayTypeVoList"].([]any)
		dayType := dayTypeList[0].(map[string]any)
		detailVoList := dayType["detailVoList"].([]any)

		roundedStart := roundTOUPeriodStart(now)
		roundedUntil := roundTOUPeriodEnd(until)
		expectedStart := roundedStart.Format("15:04")
		expectedEnd := roundedUntil.Format("15:04")
		if (roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0) || roundedUntil.Day() != roundedStart.Day() {
			expectedEnd = "24:00"
		}

		crossesMidnight := roundedUntil.Day() != roundedStart.Day() && !(roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0)

		for _, v := range detailVoList {
			w := v.(map[string]any)
			if w["startHourTime"] == expectedStart {
				assert.EqualValues(t, franklinDispatchAPowerOnStandby, w["dispatchId"], "active window should have dispatchId: 2")
				assert.Equal(t, expectedEnd, w["endHourTime"])
			} else if crossesMidnight && w["startHourTime"] == "00:00" {
				assert.EqualValues(t, franklinDispatchAPowerOnStandby, w["dispatchId"], "morning window should have dispatchId: 2")
			} else {
				assert.EqualValues(t, franklinDispatchSelfConsumption, w["dispatchId"], "off-peak fallback window should have dispatchId: 6")
			}
		}

		// Second call: already in TOU mode, schedule matches -> saveTouDispatch skipped
		saveDispatchCalled = false
		updateTouModeCalled = false
		changed2, err2 := f.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err2)
		assert.False(t, changed2)
		assert.False(t, saveDispatchCalled, "saveTouDispatch should be skipped when schedule already matches and already in TOU mode")
	})

	t.Run("SetModes Cross Midnight Dispatch", func(t *testing.T) {
		var saveDispatchCalled bool
		var savedPayload map[string]any
		loc := time.UTC
		fixedStart := time.Date(2026, 7, 15, 22, 0, 0, 0, loc)
		fixedUntil := time.Date(2026, 7, 16, 2, 0, 0, 0, loc)

		currentTouID := 22222.0
		currentTouSOC := 20.0

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       65.4,
							"timestamp": fixedStart.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111.0, "workMode": 1, "soc": currentTouSOC},
					{"id": 22222.0, "workMode": 2, "soc": 20.0},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": currentTouID},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"template": map[string]any{
							"id": 1,
						},
						"strategyList": savedPayload["strategyList"],
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				require.NoError(t, json.NewDecoder(r.Body).Decode(&savedPayload))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				currentTouID = 11111.0
				currentTouSOC = 65.0
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
			runtimeDataCache: franklinDeviceCompositeInfoResult{
				Valid: true,
				RuntimeData: franklinRuntimeData{
					SOC:       65.4,
					Timestamp: fixedStart.Unix(),
				},
			},
			runtimeDataExpiry: time.Now().Add(1 * time.Hour),
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: fixedUntil,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled)

		// Verify 3 segments in savedPayload
		strategyList := savedPayload["strategyList"].([]any)
		strategy := strategyList[0].(map[string]any)
		dayTypeList := strategy["dayTypeVoList"].([]any)
		dayType := dayTypeList[0].(map[string]any)
		detailVoList := dayType["detailVoList"].([]any)
		require.Len(t, detailVoList, 3, "cross-midnight window should produce 3 segments")

		seg1 := detailVoList[0].(map[string]any)
		assert.Equal(t, "00:00", seg1["startHourTime"])
		assert.Equal(t, "02:00", seg1["endHourTime"])
		assert.EqualValues(t, 2, seg1["waveType"])
		assert.EqualValues(t, franklinDispatchAPowerOnStandby, seg1["dispatchId"])

		seg2 := detailVoList[1].(map[string]any)
		assert.Equal(t, "02:00", seg2["startHourTime"])
		assert.Equal(t, "22:00", seg2["endHourTime"])
		assert.EqualValues(t, 0, seg2["waveType"])
		assert.EqualValues(t, franklinDispatchSelfConsumption, seg2["dispatchId"])

		seg3 := detailVoList[2].(map[string]any)
		assert.Equal(t, "22:00", seg3["startHourTime"])
		assert.Equal(t, "24:00", seg3["endHourTime"])
		assert.EqualValues(t, 2, seg3["waveType"])
		assert.EqualValues(t, franklinDispatchAPowerOnStandby, seg3["dispatchId"])

		// Convert detailVoList into []franklinTOUStrategy for isFranklinScheduleMatch testing
		var parsedStrategies []franklinTOUStrategy
		marshaled, err := json.Marshal(strategyList)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(marshaled, &parsedStrategies))

		// 1. Before midnight: 22:30 on 7/15 should match
		nowBeforeMidnight := time.Date(2026, 7, 15, 22, 30, 0, 0, loc)
		assert.True(t, isFranklinScheduleMatch(parsedStrategies, fixedUntil, franklinDispatchAPowerOnStandby, nowBeforeMidnight),
			"should match before midnight")

		// 2. After midnight: 00:30 on 7/16 should match
		nowAfterMidnight := time.Date(2026, 7, 16, 0, 30, 0, 0, loc)
		assert.True(t, isFranklinScheduleMatch(parsedStrategies, fixedUntil, franklinDispatchAPowerOnStandby, nowAfterMidnight),
			"should match after midnight")

		// 3. Before window starts: 21:30 on 7/15 should NOT match
		nowBeforeStart := time.Date(2026, 7, 15, 21, 30, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(parsedStrategies, fixedUntil, franklinDispatchAPowerOnStandby, nowBeforeStart),
			"should not match before window starts")

		// 4. After window ends: 02:30 on 7/16 should NOT match
		nowAfterEnd := time.Date(2026, 7, 16, 2, 30, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(parsedStrategies, fixedUntil, franklinDispatchAPowerOnStandby, nowAfterEnd),
			"should not match after window ends")

		// 5. Different target until (e.g. 03:00) should NOT match
		diffUntil := time.Date(2026, 7, 16, 3, 0, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(parsedStrategies, diffUntil, franklinDispatchAPowerOnStandby, nowBeforeMidnight),
			"should not match different target until")

		// 6. Calling SetModes again before midnight with matching schedule skips saveTouDispatch
		saveDispatchCalled = false
		changed2, err2 := f.SetModes(context.Background(), types.BatteryModeStandby, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: fixedUntil,
		})
		require.NoError(t, err2)
		assert.False(t, changed2)
		assert.False(t, saveDispatchCalled, "saveTouDispatch should be skipped when schedule matches across midnight")
	})

	t.Run("SetModes Respects Season and DayType When Matching TOU Schedule", func(t *testing.T) {
		loc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// 1. Direct unit tests for isFranklinScheduleMatch with multiple day types
		// Multi-dayType schedules do not match RateRudder's single standard schedule structure and return false (prompting overwrite)
		multiDayTypeStrategies := []franklinTOUStrategy{
			{
				SeasonName: "All Year",
				Month:      "1,2,3,4,5,6,7,8,9,10,11,12",
				DayTypeVoList: []franklinDayTypeVo{
					{
						DayName: "workDay",
						DayType: 1,
						DetailVoList: []franklinDetailVoItem{
							{
								StartHourTime: "00:00",
								EndHourTime:   "24:00",
								WaveType:      0,
								DispatchID:    int(franklinDispatchSelfConsumption),
							},
						},
					},
					{
						DayName: "weekend",
						DayType: 2,
						DetailVoList: []franklinDetailVoItem{
							{
								StartHourTime: "18:00",
								EndHourTime:   "20:00",
								WaveType:      2,
								DispatchID:    int(franklinDispatchAPowerToHome),
							},
						},
					},
				},
			},
		}

		targetUntil := time.Date(2026, 7, 15, 20, 0, 0, 0, loc)

		// Wednesday (Weekday): 2026-07-15 18:30 -> should NOT match (multi-dayType schedule)
		nowWednesday := time.Date(2026, 7, 15, 18, 30, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(multiDayTypeStrategies, targetUntil, franklinDispatchAPowerToHome, nowWednesday),
			"should not match when schedule has multiple day types")

		// Saturday (Weekend): 2026-07-18 18:30 -> should NOT match (multi-dayType schedule)
		nowSaturday := time.Date(2026, 7, 18, 18, 30, 0, 0, loc)
		targetUntilSat := time.Date(2026, 7, 18, 20, 0, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(multiDayTypeStrategies, targetUntilSat, franklinDispatchAPowerToHome, nowSaturday),
			"should not match multi-dayType schedule even on weekend")

		// 2. Direct unit tests for isFranklinScheduleMatch with multiple seasons
		// Multi-season schedules do not match RateRudder's single standard schedule structure and return false (prompting overwrite)
		multiSeasonStrategies := []franklinTOUStrategy{
			{
				SeasonName: "Winter",
				Month:      "1,2,3,10,11,12",
				DayTypeVoList: []franklinDayTypeVo{
					{
						DayName: "everyDay",
						DayType: 3,
						DetailVoList: []franklinDetailVoItem{
							{
								StartHourTime: "18:00",
								EndHourTime:   "20:00",
								WaveType:      2,
								DispatchID:    int(franklinDispatchAPowerToHome),
							},
						},
					},
				},
			},
			{
				SeasonName: "Summer",
				Month:      "4,5,6,7,8,9",
				DayTypeVoList: []franklinDayTypeVo{
					{
						DayName: "everyDay",
						DayType: 3,
						DetailVoList: []franklinDetailVoItem{
							{
								StartHourTime: "00:00",
								EndHourTime:   "24:00",
								WaveType:      0,
								DispatchID:    int(franklinDispatchSelfConsumption),
							},
						},
					},
				},
			},
		}

		// Summer: 2026-07-15 18:30 -> should NOT match (multi-season schedule)
		assert.False(t, isFranklinScheduleMatch(multiSeasonStrategies, targetUntil, franklinDispatchAPowerToHome, nowWednesday),
			"should not match multi-season schedule in summer")

		// Winter: 2026-12-15 18:30 -> should NOT match (multi-season schedule)
		nowWinter := time.Date(2026, 12, 15, 18, 30, 0, 0, loc)
		targetUntilWinter := time.Date(2026, 12, 15, 20, 0, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(multiSeasonStrategies, targetUntilWinter, franklinDispatchAPowerToHome, nowWinter),
			"should not match multi-season schedule in winter")

		// Standard RateRudder schedule (1 strategy "All Year", 1 dayType "everyDay")
		standardRateRudderStrategies := []franklinTOUStrategy{
			{
				SeasonName: "All Year",
				Month:      "1,2,3,4,5,6,7,8,9,10,11,12",
				DayTypeVoList: []franklinDayTypeVo{
					{
						DayName: "everyDay",
						DayType: 3,
						DetailVoList: []franklinDetailVoItem{
							{
								StartHourTime: "00:00",
								EndHourTime:   "18:00",
								WaveType:      0,
								DispatchID:    int(franklinDispatchSelfConsumption),
							},
							{
								StartHourTime: "18:00",
								EndHourTime:   "20:00",
								WaveType:      2,
								DispatchID:    int(franklinDispatchAPowerToHome),
							},
							{
								StartHourTime: "20:00",
								EndHourTime:   "24:00",
								WaveType:      0,
								DispatchID:    int(franklinDispatchSelfConsumption),
							},
						},
					},
				},
			},
		}

		// Matches standard schedule with flexible start (already active at 18:30) and matching end (20:00)
		assert.True(t, isFranklinScheduleMatch(standardRateRudderStrategies, targetUntil, franklinDispatchAPowerToHome, nowWednesday),
			"should match standard RateRudder schedule when active and target end matches")
		// Does NOT match when dispatch differs
		assert.False(t, isFranklinScheduleMatch(standardRateRudderStrategies, targetUntil, franklinDispatchAPowerToHomeAndGrid, nowWednesday),
			"should not match when dispatch differs")
		// Does NOT match when end time differs
		diffTargetUntil := time.Date(2026, 7, 15, 21, 0, 0, 0, loc)
		assert.False(t, isFranklinScheduleMatch(standardRateRudderStrategies, diffTargetUntil, franklinDispatchAPowerToHome, nowWednesday),
			"should not match when target end differs")

		// 3. SetModes integration test: gateway returns multiDayTypeStrategies
		// Today is Wednesday (Weekday), so SetModes MUST save new schedule because weekday does not have the dispatch
		var saveDispatchCalled bool

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/device/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"zoneInfo": "America/Chicago"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/device/getRuntimeDataV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"soc":        80.0,
						"timestamp":  nowWednesday.Unix(),
						"mode":       22222,
						"run_status": 0,
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       80.0,
							"timestamp": time.Now().Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"currendId": 22222,
						"list": []map[string]any{
							{"id": 11111, "name": "Direct Solar Export", "soc": 20.0, "workMode": 1, "editSocFlag": true},
							{"id": 22222, "name": "Self-Consumption", "soc": 20.0, "workMode": 2, "editSocFlag": true},
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 2}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"template": map[string]any{
							"countryId":  2,
							"provinceId": 39,
						},
						"strategyList": multiDayTypeStrategies,
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"id": 22222}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		require.NoError(t, f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			GridExportSolar:    true,
			MinBatterySOC:      20,
		}))

		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: targetUntil,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled, "saveTouDispatch MUST be called when today's day type differs, even if another day type has the dispatch")
	})

	t.Run("SetModes BatteryExport Dispatch 7", func(t *testing.T) {
		var saveDispatchCalled bool
		var updateTouModeCalled bool
		var savedPayload map[string]any
		now := time.Now()

		currentTouID := 22222.0
		currentTouSOC := 20.0

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       75.0,
							"timestamp": now.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 11111, "workMode": 1, "name": "Dynamic TOU", "eleType": 1, "soc": currentTouSOC},
					{"id": 22222, "workMode": 2, "name": "Self Consumption", "eleType": 1, "soc": 20.0},
					{"id": 33333, "workMode": 3, "name": "Emergency Backup", "eleType": 1, "soc": 100.0},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": currentTouID},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				require.NoError(t, json.NewDecoder(r.Body).Decode(&savedPayload))
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				updateTouModeCalled = true
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "1", r.Form.Get("workMode"))
				assert.Equal(t, "11111", r.Form.Get("currendId"))
				assert.Equal(t, "20", r.Form.Get("soc"), "reserve SOC should be set to minSOC for battery export")
				currentTouID = 11111.0
				currentTouSOC = 20.0
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := now.Add(2 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeExport, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled, "saveTouDispatch should be called")
		assert.True(t, updateTouModeCalled, "updateTouModeV2 should be called")

		// Verify active window has dispatchId: 7 (franklinDispatchAPowerToHomeAndGrid)
		strategyList, ok := savedPayload["strategyList"].([]any)
		require.True(t, ok)
		strategy := strategyList[0].(map[string]any)
		dayTypeList := strategy["dayTypeVoList"].([]any)
		dayType := dayTypeList[0].(map[string]any)
		detailVoList := dayType["detailVoList"].([]any)

		roundedStart := roundTOUPeriodStart(now)
		roundedUntil := roundTOUPeriodEnd(until)
		expectedStart := roundedStart.Format("15:04")
		expectedEnd := roundedUntil.Format("15:04")
		if (roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0) || roundedUntil.Day() != roundedStart.Day() {
			expectedEnd = "24:00"
		}

		for _, v := range detailVoList {
			w := v.(map[string]any)
			if w["startHourTime"] == expectedStart {
				assert.EqualValues(t, franklinDispatchAPowerToHomeAndGrid, w["dispatchId"], "active window should have dispatchId: 7")
				assert.Equal(t, expectedEnd, w["endHourTime"])
			} else {
				assert.EqualValues(t, franklinDispatchSelfConsumption, w["dispatchId"], "off-peak fallback window should have dispatchId: 6")
			}
		}
	})

	t.Run("SetModes SolarExport Provisions Missing TOU Mode", func(t *testing.T) {
		var saveDispatchCalled bool
		var updateTouModeCalled bool
		var touListCalls int
		now := time.Now()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       50.0,
							"timestamp": now.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				touListCalls++
				var list []map[string]any
				if touListCalls == 1 {
					// Initially, customer only has Self-Consumption (workMode: 2)
					list = []map[string]any{
						{"id": 22222.0, "workMode": 2, "soc": 20.0},
					}
				} else {
					// After saveTouDispatch, gateway provisions TOU (workMode: 1)
					list = []map[string]any{
						{"id": 33333.0, "workMode": 1, "soc": 20.0},
						{"id": 22222.0, "workMode": 2, "soc": 20.0},
					}
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 22222.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveDispatchCalled = true
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				updateTouModeCalled = true
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "1", r.Form.Get("workMode"))
				assert.Equal(t, "33333", r.Form.Get("currendId"), "should switch to newly provisioned TOU template ID")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := now.Add(4 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, saveDispatchCalled, "saveTouDispatch should provision schedule")
		assert.True(t, updateTouModeCalled, "updateTouModeV2 should activate provisioned TOU mode")
		assert.GreaterOrEqual(t, touListCalls, 2, "should re-fetch available modes after provisioning")
	})

	t.Run("SetModes SolarExport Fails When TOU Mode Unobtainable", func(t *testing.T) {
		now := time.Now()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":       50.0,
							"timestamp": now.Unix(),
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				// Gateway never reports workMode: 1
				list := []map[string]any{
					{"id": 22222.0, "workMode": 2, "soc": 20.0},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 22222.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": []map[string]any{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 1},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := now.Add(4 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		assert.False(t, changed)
		assert.ErrorContains(t, err, "franklin tou mode not available")
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"zoneInfo": "America/Chicago",
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/api-energy/power/getFhpPowerByDay" {
				dayTime := r.URL.Query().Get("dayTime")
				// We expect the day in America/Chicago.
				// Start is 2026-02-01 18:00 UTC -> 2026-02-01 12:00 CST.
				if dayTime == "2026-02-02" {
					// Extra day due to one-day-lookahead
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{},
					})
					return
				}
				assert.Equal(t, "2026-02-01", dayTime, "dayTime should match")

				// Return mock data with 3 timestamps to define 2 intervals in the 12:00 hour
				// 12:00:00 -> 12:15:00 (15 min = 0.25h)
				// 12:15:00 -> 13:00:00 (45 min = 0.75h)
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"deviceTimeArray": []string{
							"2026-02-01 12:05:00",
							"2026-02-01 12:15:00",
							"2026-02-01 13:00:00",
						},
						// SocArray length must match
						"socArray": []float64{50.0, 40.0, 50.0},
						// SolarToHome:
						// 12:05:00 (period 12:00 to 12:05): 12.0 kW * (5/60) h = 1.0 kWh
						// 12:15:00 (period 12:05 to 12:15): 0.0 kW * (10/60) h = 0.0 kWh
						// 13:00:00 (period 12:15 to 13:00): 0.0 kW * (45/60) h = 0.0 kWh
						// Total = 1.0
						"powerSolarHomeArray": []float64{12.0, 0.0, 0.0},

						// BatteryToHome:
						// 12:05:00 (period 12:00 to 12:05): 24.0 kW * (5/60) h = 2.0 kWh
						// 12:15:00 (period 12:05 to 12:15): 18.0 kW * (10/60) h = 3.0 kWh
						// 13:00:00 (period 12:15 to 13:00): 0.0 kW * (45/60) h = 0.0 kWh
						// Total = 5.0
						"powerFhpHomeArray": []float64{24.0, 18.0, 0.0},

						// Arrays must be same length (3)
						"powerSolarGirdArray": []float64{0.0, 0.0, 0.0},
						"powerSolarFhpArray":  []float64{0.0, 0.0, 0.0},
						"powerGirdFhpArray":   []float64{0.0, 0.0, 0.0},
						"powerGirdHomeArray":  []float64{0.0, 0.0, 0.0},
						"powerFhpGirdArray":   []float64{0.0, 0.0, 0.0},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		// Requesting 12:00 to 13:00 in Chicago time
		// 12:00 CST is 18:00 UTC
		start, err := time.Parse(time.RFC3339, "2026-02-01T18:00:00Z")
		require.NoError(t, err)
		end, err := time.Parse(time.RFC3339, "2026-02-01T19:00:00Z")
		require.NoError(t, err)

		stats, err := f.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err, "GetEnergyHistory should succeed")
		require.Len(t, stats, 1, "should have 1 stat for the hour")

		s := stats[0].Hourly[0]
		// HomeKWH = SolarToHome + GridToHome + BatToHome
		// SolarToHome = 1.0
		// BatToHome = 5.0
		// GridToHome = 0
		// Total Home = 6.0
		assert.InDelta(t, 6.0, s.HomeKWH, 0.01, "HomeKWH mismatch")

		assert.InDelta(t, 1.0, s.SolarKWH, 0.01, "SolarKWH mismatch")
		assert.InDelta(t, 5.0, s.BatteryUsedKWH, 0.01, "BatteryUsedKWH mismatch")
		assert.Equal(t, 40.0, s.MinBatterySOC, "MinBatterySOC mismatch")
		assert.Equal(t, 50.0, s.MaxBatterySOC, "MaxBatterySOC mismatch")
	})

	t.Run("Authenticate", func(t *testing.T) {
		t.Run("MD5HashRawPassword", func(t *testing.T) {
			randomStr := "temp-token-md5"
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					assert.Empty(t, r.Header.Get("logintoken"))
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "user@example.com", r.Form.Get("account"))

					// Should send the MD5 of "myrawpassword"
					assert.Equal(t, "270f69c4e37e60424744310f20018ff2", r.Form.Get("password"))

					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"token": randomStr,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": []map[string]any{
							{"id": "GW-123"},
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username: "user@example.com",
					Password: "myrawpassword",
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, randomStr, newCreds.Franklin.Token)
			assert.Equal(t, "myrawpassword", newCreds.Franklin.Password, "Raw password should not be cleared")
			assert.Empty(t, newCreds.Franklin.MD5Password, "MD5 hash should not be set")
			assert.Equal(t, "270f69c4e37e60424744310f20018ff2", f.md5Password, "Internal MD5 hash state should be set")
		})

		t.Run("AutoFetchGatewayID", func(t *testing.T) {
			token := "temp-token-123"
			expectedGatewayID := "AUTO-GW-999"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "user@example.com", r.Form.Get("account"))
					assert.Equal(t, "pass", r.Form.Get("password"))

					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"token": token,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
					// Verify token is passed in header
					assert.Equal(t, token, r.Header.Get("logintoken"))

					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": []map[string]any{
							{"id": expectedGatewayID},
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "user@example.com",
					MD5Password: "pass",
					// Empty GatewayID
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, expectedGatewayID, newCreds.Franklin.GatewayID)
		})

		t.Run("ExistingGatewayID", func(t *testing.T) {
			token := "temp-token-456"
			existingID := "EXISTING-GW"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							"token": token,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "user@example.com",
					MD5Password: "pass",
					GatewayID:   existingID,
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			// changed=true because a fresh token was obtained (no stored token)
			assert.True(t, changed)
			assert.Equal(t, existingID, newCreds.Franklin.GatewayID)
			assert.Equal(t, token, newCreds.Franklin.Token, "token should be stored in credentials")
		})

		t.Run("TokenStoredInCredentials", func(t *testing.T) {
			var loginCalls int
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					loginCalls++
					assert.Empty(t, r.Header.Get("logintoken"))
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"token": "brand-new-token"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "user@example.com",
					MD5Password: "pass",
					GatewayID:   "gw1",
					// No Token — first call, must login
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.True(t, changed, "changed should be true because a new token was obtained")
			assert.Equal(t, "brand-new-token", newCreds.Franklin.Token, "token should be written back into credentials")
			assert.Equal(t, 1, loginCalls, "login should be called exactly once")
			assert.Equal(t, "brand-new-token", f.tokenStr, "in-memory token should match")
		})

		t.Run("UsesStoredTokenSkipsLogin", func(t *testing.T) {
			var loginCalls int
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					loginCalls++
					assert.Empty(t, r.Header.Get("logintoken"))
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"token": "should-not-be-called"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "user@example.com",
					MD5Password: "pass",
					GatewayID:   "gw1",
					Token:       "stored-token-abc",
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.False(t, changed, "changed should be false — nothing new to persist")
			assert.Equal(t, 0, loginCalls, "login should NOT be called when a stored token exists")
			assert.Equal(t, "stored-token-abc", f.tokenStr, "in-memory token should be restored from credentials")
			assert.Equal(t, "stored-token-abc", newCreds.Franklin.Token)
		})

		t.Run("StaleCredentialsForcesLogin", func(t *testing.T) {
			var loginCalls int
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					loginCalls++
					assert.Empty(t, r.Header.Get("logintoken"))
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"token": "new-token"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					token := r.Header.Get("logintoken")
					if token == "expired-token" {
						json.NewEncoder(w).Encode(map[string]any{
							"code":    401,
							"success": false,
							"message": "invalid token!",
						})
						return
					}
					if token == "new-token" {
						json.NewEncoder(w).Encode(map[string]any{
							"code":    200,
							"success": true,
							"result":  map[string]any{"totalCap": 30.0, "timeZone": "UTC"},
						})
						return
					}
					http.Error(w, "unexpected token: "+token, 400)
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:  ts.Client(),
				baseURL: ts.URL,
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "user@example.com",
					MD5Password: "pass",
					GatewayID:   "gw1",
					Token:       "expired-token",
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.True(t, changed, "changed should be true because credentials changed and a new token was obtained")
			assert.Equal(t, 1, loginCalls, "login should be called when credentials have changed")
			assert.Equal(t, "new-token", newCreds.Franklin.Token, "new token should be written back into credentials")
			assert.Equal(t, "new-token", f.tokenStr)
		})
	})

	t.Run("Login Failure No Retry", func(t *testing.T) {
		var callCount int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    401,
					"success": false,
					"message": "Bad password",
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:  ts.Client(),
			baseURL: ts.URL,
		}

		// Use Authenticate which calls login -> doRequest
		creds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:    "user",
				MD5Password: "wrongpass",
			},
		}

		_, _, err := f.Authenticate(context.Background(), creds)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Bad password")
		assert.Equal(t, 1, callCount)
	})

	t.Run("GetStatus StormHedge", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":  50.0,
							"mode": 6, // Storm Hedge
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/weather/getProgressingStormList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{
							"id":           61621,
							"onset":        "2026-02-18 10:00:00",
							"severity":     "Severe",
							"durationTime": 600, // This is expected to be mapped to DurationMins
						},
					},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
			settings:    types.Settings{MinBatterySOC: 10},
		}

		status, err := f.GetStatus(context.Background())
		require.NoError(t, err, "GetStatus should succeed")
		assert.True(t, status.EmergencyMode, "should be in emergency mode")
		require.Len(t, status.Storms, 1, "should have 1 storm")
		assert.Equal(t, "Severe", status.Storms[0].Description)

		expectedStart, _ := time.Parse(time.DateTime, "2026-02-18 10:00:00")
		// The json above uses UTC for timeZone in getDeviceInfoV2, so we expect UTC.
		assert.Equal(t, expectedStart.UTC(), status.Storms[0].TSStart.UTC())

		// 600 minutes = 10 hours
		expectedEnd := expectedStart.Add(10 * time.Hour)
		assert.Equal(t, expectedEnd.UTC(), status.Storms[0].TSEnd.UTC())
	})

	t.Run("SetModes Both Solar and Battery Export", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"valid": true}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 20, "workMode": 2, "electricityType": 1, "soc": 20.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": list, "currendId": 20}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
						{"id": 2, "modelName": "aPower 2", "peHwVersion": 20, "ratedCap": 15000, "chargePower": 8000, "dischargePower": 10000, "derateFlag": 1},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"gridMaxFlag": 1, "gridFeedMaxFlag": 3}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 2 (battery and solar export)
				assert.EqualValues(t, 2, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 2 for Both Export")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		// Set settings to enable grid export for solar AND batteries
		err := f.ApplySettings(context.Background(), types.Settings{
			GridExportSolar:     true,
			GridExportBatteries: true,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeAny, types.ModesOptions{})
		require.NoError(t, err, "SetModes should succeed")
		assert.False(t, changed)

		// Verify no calls were made since power control updates are disabled
		require.Empty(t, callOrder)
	})

	t.Run("SetModes Charge with ChargeToSOC", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"valid": true},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 10.0, "workMode": 1},
					{"id": 20.0, "workMode": 2, "editSocFlag": true},
					{"id": 30.0, "workMode": 3},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": []map[string]any{
						{"id": 1, "modelName": "aPower X", "peHwVersion": 0, "ratedCap": 13600, "chargePower": 5000, "dischargePower": 5000, "derateFlag": 0},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "85", r.Form.Get("soc"), "soc should be 85")
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
		})
		require.NoError(t, err)

		changed, err := f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{ChargeToSOC: 85})
		require.NoError(t, err)
		assert.True(t, changed)

		require.Len(t, callOrder, 1)
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Direct Solar Export Preserves Existing Country and Province", func(t *testing.T) {
		var detailCalled, saveCalled int
		var savedCountryID, savedProvinceID int
		var savedElectricCompany, savedEleCompanyFullName string

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]any{
					"soc":       75.0,
					"mode":      138224,
					"timestamp": time.Now().Unix(),
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"valid": true, "runtimeData": runtimeData}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 44279, "workMode": 1, "name": "TOU Mode", "soc": 20.0, "editSocFlag": true},
					{"id": 138224, "workMode": 2, "name": "Self-Consumption", "soc": 20.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": list, "currendId": 138224}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 2}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				detailCalled++
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"template": map[string]any{
							"countryId":          2,
							"provinceId":         39,
							"electricCompany":    "PG&E",
							"eleCompanyFullName": "Pacific Gas and Electric",
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveCalled++
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tpl, ok := body["template"].(map[string]any); ok {
					savedCountryID = int(tpl["countryId"].(float64))
					savedProvinceID = int(tpl["provinceId"].(float64))
					savedElectricCompany = tpl["electricCompany"].(string)
					savedEleCompanyFullName = tpl["eleCompanyFullName"].(string)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"id": 44279}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			GridExportSolar:    true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := time.Now().Add(2 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)

		assert.Equal(t, 1, detailCalled, "getTouDispatchDetail should be called once")
		assert.Equal(t, 1, saveCalled, "saveTouDispatch should be called once")
		assert.Equal(t, 2, savedCountryID, "countryId should match existing gateway setting")
		assert.Equal(t, 39, savedProvinceID, "provinceId should match existing gateway setting (California)")
		assert.Equal(t, "RateRudder", savedElectricCompany, "electricCompany should always be overwritten with RateRudder")
		assert.Equal(t, "RateRudder Dynamic Solar Export", savedEleCompanyFullName, "eleCompanyFullName should always be overwritten with RateRudder")

		// Subsequent call should fetch fresh template and not use cache
		_, err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until.Add(time.Hour),
		})
		require.NoError(t, err)
		assert.Equal(t, 2, detailCalled, "getTouDispatchDetail should be called fresh each time")
	})

	t.Run("SetModes Direct Solar Export Fallback When Detail Fails", func(t *testing.T) {
		var savedCountryID, savedProvinceID int
		var savedElectricCompany, savedEleCompanyFullName string

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]any{
					"soc":       75.0,
					"mode":      138224,
					"timestamp": time.Now().Unix(),
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"valid": true, "runtimeData": runtimeData}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 44279, "workMode": 1, "name": "TOU Mode", "soc": 20.0, "editSocFlag": true},
					{"id": 138224, "workMode": 2, "name": "Self-Consumption", "soc": 20.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": list, "currendId": 138224}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 2}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				http.Error(w, "server error", 500)
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tpl, ok := body["template"].(map[string]any); ok {
					savedCountryID = int(tpl["countryId"].(float64))
					savedProvinceID = int(tpl["provinceId"].(float64))
					savedElectricCompany = tpl["electricCompany"].(string)
					savedEleCompanyFullName = tpl["eleCompanyFullName"].(string)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"id": 44279}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			GridExportSolar:    true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := time.Now().Add(2 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)

		assert.Equal(t, 2, savedCountryID, "should fallback to countryId 2 (US)")
		assert.Equal(t, 39, savedProvinceID, "should fallback to provinceId 39 (CA)")
		assert.Equal(t, "RateRudder", savedElectricCompany, "should fallback to electricCompany RateRudder")
		assert.Equal(t, "RateRudder Dynamic Solar Export", savedEleCompanyFullName)
	})

	t.Run("SetModes Direct Solar Export Defaults When Country and Province Are Zero", func(t *testing.T) {
		var saveCalled int
		var savedCountryID, savedProvinceID int

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]any{
					"soc":       75.0,
					"mode":      138224,
					"timestamp": time.Now().Unix(),
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"valid": true, "runtimeData": runtimeData}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 44279, "workMode": 1, "name": "TOU Mode", "soc": 20.0, "editSocFlag": true},
					{"id": 138224, "workMode": 2, "name": "Self-Consumption", "soc": 20.0, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"list": list, "currendId": 138224}})
				return
			}
			if r.URL.Path == "/hes-gateway/common/getPowerCapConfigList" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  []map[string]any{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"gridMaxFlag": 2, "gridFeedMaxFlag": 2}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getTouDispatchDetail" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"template": map[string]any{
							"countryId":          0,
							"provinceId":         0,
							"electricCompany":    "Local Electric",
							"eleCompanyFullName": "Local Electric Utility",
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/saveTouDispatch" {
				saveCalled++
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tpl, ok := body["template"].(map[string]any); ok {
					savedCountryID = int(tpl["countryId"].(float64))
					savedProvinceID = int(tpl["provinceId"].(float64))
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"id": 44279}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{}})
				return
			}
			http.Error(w, "not found "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "u",
			md5Password: "p",
			gatewayID:   "g",
		}

		err := f.ApplySettings(context.Background(), types.Settings{
			ManageTOUSchedules: true,
			GridExportSolar:    true,
			MinBatterySOC:      20,
		})
		require.NoError(t, err)

		until := time.Now().Add(2 * time.Hour)
		changed, err := f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeExport, types.ModesOptions{
			TSScheduleModeUntil: until,
		})
		require.NoError(t, err)
		assert.True(t, changed)

		assert.Equal(t, 1, saveCalled, "saveTouDispatch should be called once")
		assert.Equal(t, 2, savedCountryID, "should default zero countryId to 2 (US)")
		assert.Equal(t, 39, savedProvinceID, "should default zero provinceId to 39 (CA)")
	})

	t.Run("GetEnergyHistory Deduplication and Next Day", func(t *testing.T) {
		dayCalls := map[string]int{}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"zoneInfo": "UTC"}})
				return
			}
			if r.URL.Path == "/api-energy/power/getFhpPowerByDay" {
				dayTime := r.URL.Query().Get("dayTime")
				dayCalls[dayTime]++
				switch dayTime {
				case "2026-03-28":
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							// Day 1 ends with 00:00:00
							"deviceTimeArray":     []string{"2026-03-28 23:55:00", "2026-03-29 00:00:00"},
							"socArray":            []float64{50.0, 50.0},
							"powerSolarHomeArray": []float64{120.0, 120.0}, // 120kW * 5min / 60min = 10kWh
							"powerFhpHomeArray":   []float64{0.0, 0.0},
							"powerSolarGirdArray": []float64{0.0, 0.0},
							"powerSolarFhpArray":  []float64{0.0, 0.0},
							"powerGirdFhpArray":   []float64{0.0, 0.0},
							"powerGirdHomeArray":  []float64{0.0, 0.0},
							"powerFhpGirdArray":   []float64{0.0, 0.0},
						},
					})
				case "2026-03-29":
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							// Day 2 starts with 00:00:00 (duplicate) and continues
							"deviceTimeArray":     []string{"2026-03-29 00:00:00", "2026-03-29 00:05:00"},
							"socArray":            []float64{50.0, 50.0},
							"powerSolarHomeArray": []float64{120.0, 120.0},
							"powerFhpHomeArray":   []float64{0.0, 0.0},
							"powerSolarGirdArray": []float64{0.0, 0.0},
							"powerSolarFhpArray":  []float64{0.0, 0.0},
							"powerGirdFhpArray":   []float64{0.0, 0.0},
							"powerGirdHomeArray":  []float64{0.0, 0.0},
							"powerFhpGirdArray":   []float64{0.0, 0.0},
						},
					})
				case "2026-03-30":
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result": map[string]any{
							// Day 3 starts with 00:00:00 (duplicate) and continues
							"deviceTimeArray":     []string{"2026-03-30 00:00:00"},
							"socArray":            []float64{40.0},
							"powerSolarHomeArray": []float64{180.0},
							"powerFhpHomeArray":   []float64{0.0},
							"powerSolarGirdArray": []float64{0.0},
							"powerSolarFhpArray":  []float64{0.0},
							"powerGirdFhpArray":   []float64{0.0},
							"powerGirdHomeArray":  []float64{0.0},
							"powerFhpGirdArray":   []float64{0.0},
						},
					})
				default:
					t.Errorf("unexpected dayTime: %s", dayTime)
					http.Error(w, "unexpected dayTime", 400)
				}
				return
			}
			http.Error(w, "not found", 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			gatewayID:   "g",
			username:    "u",
			md5Password: "p",
		}

		// Requesting 23:00 on Day 1 to 01:00 on Day 2
		start, err := time.Parse(time.RFC3339, "2026-03-28T23:00:00Z")
		require.NoError(t, err)
		end, err := time.Parse(time.RFC3339, "2026-03-29T01:00:00Z")
		require.NoError(t, err)

		stats, err := f.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err)

		// Check for duplicates
		seen := make(map[time.Time]int)
		for _, dayStats := range stats {
			for _, s := range dayStats.Hourly {
				seen[s.TSHourStart]++
			}
		}

		for ts, count := range seen {
			assert.Equal(t, 1, count, "Duplicate TSHourStart found: %v", ts)
		}

		// Collect flat list to verify
		var flatStats []types.EnergyStats
		for _, ds := range stats {
			assert.NotEmpty(t, ds.TimeLocation, "DailyEnergyStats TimeLocation should be populated")
			for _, h := range ds.Hourly {
				assert.NotEmpty(t, h.TimeLocation, "EnergyStats TimeLocation should be populated")
			}
			flatStats = append(flatStats, ds.Hourly...)
		}

		// Verify we have stats for 3 hours
		if !assert.Len(t, flatStats, 3, "Expected 3 hours (23:00, 00:00, and 23:00)") {
			for i, s := range flatStats {
				t.Logf("Stats[%d]: %v", i, s.TSHourStart)
			}
		}

		// Hour 23:00 should have only the 23:55-00:00 interval (10 kWh)
		// Plus whatever the default 5min interval for the 23:55 point gave (another 10 kWh?)
		// Actually, Day 1 first point is 23:55. It gets 5min default.
		// Day 1 second point is 00:00. It gets 5min (00:00 - 23:55).
		// Total for 23:00 bucket = 2 * 10 = 20 kWh.
		h23 := flatStats[0]
		expected23, err := time.Parse(time.RFC3339, "2026-03-28T23:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, expected23.Unix(), h23.TSHourStart.Unix())
		assert.InDelta(t, 20.0, h23.SolarKWH, 0.01)

		// Hour 00:00 should have only the 00:00-00:05 interval (10 kWh)
		h00 := flatStats[1]
		expected00, err := time.Parse(time.RFC3339, "2026-03-29T00:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, expected00.Unix(), h00.TSHourStart.Unix())
		assert.InDelta(t, 10.0, h00.SolarKWH, 0.01)

		// hour 23 should have the next day's 00:00 point
		h23 = flatStats[len(flatStats)-1]
		expected23, err = time.Parse(time.RFC3339, "2026-03-29T23:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, expected23.Unix(), h23.TSHourStart.Unix())
		// ignore the value because the difference between the points is huge since
		// we have sparse data

		// Verify we fetched all days
		assert.Equal(t, 1, dayCalls["2026-03-28"])
		assert.Equal(t, 1, dayCalls["2026-03-29"])
		assert.Equal(t, 1, dayCalls["2026-03-30"])
	})

	t.Run("GetEnergyHistory Future Points", func(t *testing.T) {
		now := time.Now().UTC()
		pastTime := now.Add(-10 * time.Minute)
		futureTime := now.Add(10 * time.Minute)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"zoneInfo": "UTC"}})
				return
			}
			if r.URL.Path == "/api-energy/power/getFhpPowerByDay" {
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"deviceTimeArray": []string{
							pastTime.Format("2006-01-02 15:04:05"),
							futureTime.Format("2006-01-02 15:04:05"),
						},
						"socArray":            []float64{50.0, 60.0},
						"powerSolarHomeArray": []float64{10.0, 20.0},
						"powerFhpHomeArray":   []float64{0.0, 0.0},
						"powerSolarGirdArray": []float64{0.0, 0.0},
						"powerSolarFhpArray":  []float64{0.0, 0.0},
						"powerGirdFhpArray":   []float64{0.0, 0.0},
						"powerGirdHomeArray":  []float64{0.0, 0.0},
						"powerFhpGirdArray":   []float64{0.0, 0.0},
					},
				})
				return
			}
			http.Error(w, "not found", 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:      ts.Client(),
			baseURL:     ts.URL,
			gatewayID:   "g",
			username:    "u",
			md5Password: "p",
		}

		start := now.Add(-24 * time.Hour)
		end := now.Add(24 * time.Hour)

		stats, err := f.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err)

		// We expect only the past point to be present
		foundFuture := false
		foundPast := false
		for _, ds := range stats {
			for _, h := range ds.Hourly {
				// The hourly bucket for pastTime should exist.
				// pastTime is now - 10m. Hourly bucket is hour start.
				// We don't check the exact value here but rather that the future point didn't contribute.
				// Since we only have two points, let's look at the raw points in aggregatePointsIntoHours if we could.
				// But we can check if the SOC or Power values reflect the future point.
				if h.MaxBatterySOC == 60.0 {
					foundFuture = true
				}
				if h.MaxBatterySOC == 50.0 {
					foundPast = true
				}
			}
		}

		assert.True(t, foundPast, "should have found the past data point")
		assert.False(t, foundFuture, "should not have found the future data point")
	})

	t.Run("GetAvailableModes Current Tou ID Not Found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 83450.0, "workMode": 1, "soc": 15.0, "name": "TOU"},
					{"id": 55594.0, "workMode": 2, "soc": 10.0, "name": "自发自用"},
					{"id": 100626.0, "workMode": 3, "soc": 100.0, "name": "仅备电"},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 9322.0},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		modes, err := f.getAvailableModes(context.Background())
		require.NoError(t, err)

		assert.Equal(t, 83450, modes.currentMode.ID)
		assert.Equal(t, franklinWorkModeTimeOfUse, modes.currentMode.WorkMode)
		assert.Equal(t, "TOU", modes.currentMode.Name)
	})

	t.Run("GetAvailableModes Smart Energy Dispatch", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]any{
					{"id": 83450.0, "workMode": 1, "soc": 15.0, "name": "TOU", "oldIndex": 1},
					{"id": 55594.0, "workMode": 2, "soc": 10.0, "name": "Self-Consumption", "oldIndex": 0},
					{"id": 100626.0, "workMode": 3, "soc": 100.0, "name": "Emergency Backup", "oldIndex": 2},
					{"id": 186440.0, "workMode": 7, "soc": 20.0, "name": "Smart Energy Dispatch", "oldIndex": 3},
				}
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result":  map[string]any{"list": list, "currendId": 186440.0},
				})
				return
			}
			http.Error(w, "not found: "+r.URL.Path, 404)
		}))
		defer ts.Close()

		f := &Franklin{
			client:    ts.Client(),
			baseURL:   ts.URL,
			gatewayID: "g",
		}

		modes, err := f.getAvailableModes(context.Background())
		require.NoError(t, err)

		assert.Equal(t, 186440, modes.currentMode.ID)
		assert.Equal(t, franklinWorkModeSmartEnergyDispatch, modes.currentMode.WorkMode)
		assert.Equal(t, "Smart Energy Dispatch", modes.currentMode.Name)
		assert.Equal(t, "Smart Energy Dispatch", modes.smartEnergyDispatch.Name)
		assert.Equal(t, 186440, modes.smartEnergyDispatch.ID)
		assert.Equal(t, "Smart Energy Dispatch", franklinWorkModeSmartEnergyDispatch.String())
	})

	t.Run("GridSettings", func(t *testing.T) {
		t.Run("ChargeFromGridAndExportAll", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"gridFeedMaxFlag": 2, "gridMaxFlag": 2},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:    ts.Client(),
				baseURL:   ts.URL,
				gatewayID: "g",
			}

			gs, err := f.GridSettings(context.Background())
			require.NoError(t, err)
			assert.True(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.True(t, gs.GridExportBatteries)
		})

		t.Run("NoChargeFromGridAndSolarOnlyExport", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"gridFeedMaxFlag": 1, "gridMaxFlag": 1},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:    ts.Client(),
				baseURL:   ts.URL,
				gatewayID: "g",
			}

			gs, err := f.GridSettings(context.Background())
			require.NoError(t, err)
			assert.False(t, gs.GridChargeBatteries)
			assert.True(t, gs.GridExportSolar)
			assert.False(t, gs.GridExportBatteries)
		})

		t.Run("NoExport", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
					json.NewEncoder(w).Encode(map[string]any{
						"code":    200,
						"success": true,
						"result":  map[string]any{"gridFeedMaxFlag": 3, "gridMaxFlag": 1},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			f := &Franklin{
				client:    ts.Client(),
				baseURL:   ts.URL,
				gatewayID: "g",
			}

			gs, err := f.GridSettings(context.Background())
			require.NoError(t, err)
			assert.False(t, gs.GridChargeBatteries)
			assert.False(t, gs.GridExportSolar)
			assert.False(t, gs.GridExportBatteries)
		})
	})
}
