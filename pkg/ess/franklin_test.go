package ess

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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
	t.Run("Login Flow", func(t *testing.T) {
		// Mock Server
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				// Verify payload
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "user@example.com", r.Form.Get("account"))
				assert.Equal(t, "pass", r.Form.Get("password"))

				// Return success token
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"token": "fake-token-123",
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
			username:    "user@example.com",
			md5Password: "pass",
			gatewayID:   "GW123",
		}

		err := f.ensureLogin(context.Background())
		require.NoError(t, err, "login should succeed")

		assert.Equal(t, "fake-token-123", f.tokenStr, "token should match")
	})

	t.Run("AutoFetchGatewayID", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"token": "tok",
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": []map[string]interface{}{
						{"id": "AUTO-GW-123"},
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
			// No gatewayID
		}

		err := f.ensureLogin(context.Background())
		require.NoError(t, err, "login should succeed")
		assert.Equal(t, "AUTO-GW-123", f.gatewayID, "Should auto-fetch gateway ID")
	})

	t.Run("GetStatus", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"totalCap": 30.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"globalGridChargeMax": 15.0, "gridFeedMaxFlag": 2, "gridMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 138224.0, "workMode": 2, "soc": 88.5}, // Matches current SOC -> Standby
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 138224.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				runtimeData := map[string]interface{}{
					"soc":   88.5,
					"p_fhp": 1500.0,
					"mode":  138224.0, // Self consumption ID
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
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
		assert.True(t, status.CanExportSolar, "CanExportSolar should be true")
		assert.True(t, status.CanExportBattery, "CanExportBattery should be true")
		assert.True(t, status.CanImportBattery, "CanImportBattery should be true")
		assert.True(t, status.ElevatedMinBatterySOC, "ElevatedMinBatterySOC should be true")
		assert.True(t, status.BatteryAboveMinSOC, "BatteryAboveMinSOC should be true")
	})

	t.Run("GetStatus Alarms", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"runtimeData": map[string]interface{}{
							"soc": 50.0,
						},
						"currentAlarmVOList": []map[string]interface{}{
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
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"runtimeData": map[string]interface{}{
							"soc": 50.0,
						},
						"currentAlarmVOList": []map[string]interface{}{
							{
								"logName":          "SIM card not inserted",
								"alarmExplanation": "Ignore this",
								"alarmCode":        "E001",
								"time":             "2023-10-27 12:00:00",
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
		require.Len(t, status.Alarms, 1, "should have only 1 alarm (SIM card alarm should be ignored)")
		assert.Equal(t, "Real Alarm", status.Alarms[0].Name)
	})

	t.Run("SetModes", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 11111.0, "workMode": 1}, // TOU
					{"id": 22222.0, "workMode": 2}, // Self-consumption
					{"id": 33333.0, "workMode": 3}, // Backup
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
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

				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
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

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify the expected call was made
		require.Len(t, callOrder, 1, "updateTouModeV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0])
	})

	t.Run("SetModes Charge", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 10.0, "workMode": 1},
					{"id": 20.0, "workMode": 2, "editSocFlag": true},
					{"id": 30.0, "workMode": 3},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				// We expect it to enable generic grid charging (flag=2)
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				assert.EqualValues(t, 2, data["gridMaxFlag"], "gridMaxFlag should be 2")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateTouModeV2" {
				callOrder = append(callOrder, "updateTouModeV2")
				require.NoError(t, r.ParseForm())
				// ChargeAny sets SOC to 100
				assert.Equal(t, "100", r.Form.Get("soc"), "soc should be 100")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
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

		// SetModes(ChargeAny)
		err := f.ApplySettings(context.Background(), types.Settings{GridChargeBatteries: true})
		require.NoError(t, err, "ApplySettings should succeed")
		err = f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify both calls were made
		require.Len(t, callOrder, 2, "both updateTouModeV2 and setPowerControlV2 should be called")
		assert.Equal(t, "updateTouModeV2", callOrder[0], "updateTouModeV2 should be called first")
		assert.Equal(t, "setPowerControlV2", callOrder[1], "setPowerControlV2 should be called second")
	})

	t.Run("SetModes Both Mode and PowerControl Updates", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "100", r.Form.Get("soc"), "soc should be 100 for ChargeAny")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": nil})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 1 (solar only export)
				assert.EqualValues(t, 1, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 1 for SolarModeAny with GridExportSolar=true")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
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

		// Set settings to enable grid export for solar
		err := f.ApplySettings(context.Background(), types.Settings{
			MinBatterySOC:   20,
			GridExportSolar: true,
		})
		require.NoError(t, err)

		// This should update both SOC (to 100 for charging) AND power control (to enable solar export)
		err = f.SetModes(context.Background(), types.BatteryModeChargeAny, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify both API calls were made
		require.Len(t, callOrder, 2, "both updateSocV2 and setPowerControlV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0], "updateSocV2 should be called first")
		assert.Equal(t, "setPowerControlV2", callOrder[1], "setPowerControlV2 should be called second")
	})

	t.Run("SetModes NoChange", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
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
		err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeNoChange)
		require.NoError(t, err, "SetModes should succeed (noop)")
	})

	t.Run("SetModes Partial NoChange", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2, "soc": 55.0},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 2},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 3 (no export) since SolarModeAny with GridExportSolar=false (default)
				assert.EqualValues(t, 3, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 3 for no export")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
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

		err := f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify only setPowerControlV2 was called (BatteryModeNoChange doesn't update mode/SOC)
		require.Len(t, callOrder, 1, "only setPowerControlV2 should be called")
		assert.Equal(t, "setPowerControlV2", callOrder[0])
	})

	t.Run("SetModes UpdateSOC Only", func(t *testing.T) {
		var callOrder []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "soc": 55.0, "canEditReserveSOC": true},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 20.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/updateSocV2" {
				callOrder = append(callOrder, "updateSocV2")
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "20", r.Form.Get("soc"), "soc should be updated to MinBatterySOC")
				assert.Equal(t, "2", r.Form.Get("workMode"))
				assert.Equal(t, "1", r.Form.Get("electricityType"))

				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": nil})
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

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange)
		require.NoError(t, err, "SetModes should succeed")

		// Verify only updateSocV2 was called (not updateTouModeV2)
		require.Len(t, callOrder, 1, "only updateSocV2 should be called")
		assert.Equal(t, "updateSocV2", callOrder[0])
	})

	t.Run("SetModes Ignores Storm Hedge", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"runtimeData": map[string]interface{}{"mode": 6},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 11.0, "workMode": 2, "electricityType": 1},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 11.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				t.Error("Should not call getPowerControlSetting")
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

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange)
		assert.ErrorContains(t, err, "device is in storm hedge mode")
	})

	t.Run("SetModes Ignores Emergency Mode", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 11.0, "workMode": 3, "electricityType": 1},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 11.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				t.Error("Should not call getPowerControlSetting")
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

		err = f.SetModes(context.Background(), types.BatteryModeLoad, types.SolarModeNoChange)
		assert.ErrorContains(t, err, "device is in backup mode")
	})

	t.Run("GetEnergyHistory", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"zoneInfo": "America/Chicago",
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{},
				})
				return
			}
			if r.URL.Path == "/api-energy/power/getFhpPowerByDay" {
				dayTime := r.URL.Query().Get("dayTime")
				// We expect the day in America/Chicago.
				// Start is 2026-02-01 18:00 UTC -> 2026-02-01 12:00 CST.
				assert.Equal(t, "2026-02-01", dayTime, "dayTime should match")

				// Return mock data with 3 timestamps to define 2 intervals in the 12:00 hour
				// 12:00:00 -> 12:15:00 (15 min = 0.25h)
				// 12:15:00 -> 13:00:00 (45 min = 0.75h)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"deviceTimeArray": []string{
							"2026-02-01 12:00:00",
							"2026-02-01 12:15:00",
							"2026-02-01 13:00:00",
						},
						// SocArray length must match
						"socArray": []float64{50.0, 40.0, 50.0},
						// SolarToHome:
						// 1st interval: 4.0 kW * 0.25 h = 1.0 kWh
						// 2nd interval: 0.0 kW * 0.75 h = 0.0 kWh
						// Total = 1.0
						"powerSolarHomeArray": []float64{4.0, 0.0, 0.0},

						// BatteryToHome:
						// 1st interval: 8.0 kW * 0.25 h = 2.0 kWh
						// 2nd interval: 4.0 kW * 0.75 h = 3.0 kWh
						// Total = 5.0
						"powerFhpHomeArray": []float64{8.0, 4.0, 0.0},

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
		start, _ := time.Parse(time.RFC3339, "2026-02-01T18:00:00Z")
		end, _ := time.Parse(time.RFC3339, "2026-02-01T19:00:00Z")

		stats, err := f.GetEnergyHistory(context.Background(), start, end)
		require.NoError(t, err, "GetEnergyHistory should succeed")
		require.Len(t, stats, 1, "should have 1 stat for the hour")

		s := stats[0]
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
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "user@example.com", r.Form.Get("account"))

					// Should send the MD5 of "myrawpassword"
					hash := md5.Sum([]byte("myrawpassword"))
					expectedHash := hex.EncodeToString(hash[:])
					assert.Equal(t, expectedHash, r.Form.Get("password"))

					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result": map[string]interface{}{
							"token": randomStr,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result": []map[string]interface{}{
							{"id": "GW-123"},
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
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
			assert.Equal(t, "", newCreds.Franklin.Password, "Raw password should be cleared")

			hash := md5.Sum([]byte("myrawpassword"))
			assert.Equal(t, hex.EncodeToString(hash[:]), newCreds.Franklin.MD5Password, "MD5 hash should be set")
		})

		t.Run("AutoFetchGatewayID", func(t *testing.T) {
			token := "temp-token-123"
			expectedGatewayID := "AUTO-GW-999"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "user@example.com", r.Form.Get("account"))
					assert.Equal(t, "pass", r.Form.Get("password"))

					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result": map[string]interface{}{
							"token": token,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getHomeGatewayList" {
					// Verify token is passed in header
					assert.Equal(t, token, r.Header.Get("logintoken"))

					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result": []map[string]interface{}{
							{"id": expectedGatewayID},
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
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
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result": map[string]interface{}{
							"token": token,
						},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
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
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"token": "brand-new-token"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
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
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"token": "should-not-be-called"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
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
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"token": "fresh-token-xyz"},
					})
					return
				}
				if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0},
					})
					return
				}
				http.Error(w, "not found: "+r.URL.Path, 404)
			}))
			defer ts.Close()

			// Simulate a Franklin instance that already has a different user cached.
			f := &Franklin{
				client:      ts.Client(),
				baseURL:     ts.URL,
				username:    "old-user@example.com",
				md5Password: "old-pass",
				tokenStr:    "old-token",
			}

			creds := types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:    "new-user@example.com",
					MD5Password: "new-pass",
					GatewayID:   "gw1",
					Token:       "old-stored-token", // stale — credentials changed
				},
			}

			newCreds, changed, err := f.Authenticate(context.Background(), creds)
			require.NoError(t, err)
			assert.True(t, changed, "changed should be true because credentials changed and a new token was obtained")
			assert.Equal(t, 1, loginCalls, "login should be called when credentials have changed")
			assert.Equal(t, "fresh-token-xyz", newCreds.Franklin.Token, "new token should be written back into credentials")
			assert.Equal(t, "fresh-token-xyz", f.tokenStr)
		})
	})

	t.Run("Token Retry", func(t *testing.T) {
		var callCount int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "new-token"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				token := r.Header.Get("logintoken")
				if token == "expired-token" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    401,
						"success": false,
						"message": "Token expired",
					})
					return
				}
				if token == "new-token" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    200,
						"success": true,
						"result":  map[string]interface{}{"totalCap": 30.0, "timeZone": "UTC"},
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
			client:      ts.Client(),
			baseURL:     ts.URL,
			username:    "user",
			md5Password: "pass",
			tokenStr:    "expired-token",
			gatewayID:   "g",
		}

		_, err := f.getDeviceInfo(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "new-token", f.tokenStr)
		// 1. getDeviceInfo (expired)
		// 2. login
		// 3. getDeviceInfo (success)
		assert.Equal(t, 3, callCount)
	})

	t.Run("Login Failure No Retry", func(t *testing.T) {
		var callCount int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if r.URL.Path == "/hes-gateway/terminal/initialize/appUserOrInstallerLogin" {
				json.NewEncoder(w).Encode(map[string]interface{}{
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
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"token": "tok"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceInfoV2" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"totalCap": 30.0, "timeZone": "UTC"},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"gridMaxFlag": 0, "gridFeedMaxFlag": 0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 1.0, "workMode": 1},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result":  map[string]interface{}{"list": list, "currendId": 1.0},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": map[string]interface{}{
						"runtimeData": map[string]interface{}{
							"soc":  50.0,
							"mode": 6, // Storm Hedge
						},
					},
				})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/weather/getProgressingStormList" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    200,
					"success": true,
					"result": []map[string]interface{}{
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
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"token": "tok"}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/getDeviceCompositeInfo" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getGatewayTouListV2" {
				list := []map[string]interface{}{
					{"id": 20.0, "workMode": 2, "electricityType": 1, "editSocFlag": true},
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"list": list, "currendId": 20.0}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/getPowerControlSetting" {
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{"gridMaxFlag": 1, "gridFeedMaxFlag": 3}})
				return
			}
			if r.URL.Path == "/hes-gateway/terminal/tou/setPowerControlV2" {
				callOrder = append(callOrder, "setPowerControlV2")
				var data map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&data))
				// Should set gridFeedMaxFlag to 2 (battery and solar export)
				assert.EqualValues(t, 2, data["gridFeedMaxFlag"], "gridFeedMaxFlag should be 2 for Both Export")
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "success": true, "result": map[string]interface{}{}})
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

		err = f.SetModes(context.Background(), types.BatteryModeNoChange, types.SolarModeAny)
		require.NoError(t, err, "SetModes should succeed")

		// Verify setPowerControlV2 was called
		require.Len(t, callOrder, 1, "setPowerControlV2 should be called")
		assert.Equal(t, "setPowerControlV2", callOrder[0])
	})
}
