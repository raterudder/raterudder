package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestERCOT(t *testing.T) {
	t.Run("Real-Time Market API and DB Caching", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Now().In(ctLocation)
		dateStr := nowCT.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			// Generate 24 hours of data with HE 17 at $2,500/MWh spike
			for h := 1; h <= 24; h++ {
				price := 35.0 // $35/MWh = $0.035/kWh
				if h == 17 {
					price = 2500.0 // Super spike: $2,500/MWh = $2.50/kWh!
				}
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dateStr,
						DeliveryHour:         h,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: price,
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.token = "mock-token"

		ctx := context.Background()
		start := truncateDay(nowCT)
		end := start.Add(24 * time.Hour)

		// 1. Initial query: DB empty -> fetches from API -> persists to DB
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", start, end).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.MatchedBy(func(prices []types.PriceState) bool {
			return len(prices) == 24
		}), 0).Return(nil).Once()

		prices, confirmedEnd, err := base.getPricesForDate(ctx, nowCT, "LZ_HOUSTON")
		require.NoError(t, err)
		require.Len(t, prices, 24)
		assert.Equal(t, end, confirmedEnd)

		// Verify hour 16 (HE 17) has 100% raw wholesale price ($2.50/kWh)
		spikePrice := prices[16]
		assert.True(t, spikePrice.SeparateGenerationCredit)
		assert.InDelta(t, 2.50, spikePrice.GenerationCreditDollarsPerKWH, 1e-6)

		// 2. Second query: in-memory cache hit (no DB or API calls)
		cachedPrices, cachedConfirmedEnd, err := base.getPricesForDate(ctx, nowCT, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Equal(t, prices, cachedPrices)
		assert.Equal(t, end, cachedConfirmedEnd)

		mockDB.AssertExpectations(t)
	})

	t.Run("Missing Hour Uses Prior Hour", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Now().In(ctLocation)
		dateStr := nowCT.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			// Supply data for HE 1 ($50/MWh = $0.05/kWh) and HE 3 ($100/MWh = $0.10/kWh)
			// HE 2 is intentionally missing
			entries = append(entries, ercotSPPEntry{
				DeliveryDate:         dateStr,
				DeliveryHour:         1,
				DeliveryInterval:     1,
				SettlementPoint:      "LZ_HOUSTON",
				SettlementPointPrice: 50.0,
			})
			entries = append(entries, ercotSPPEntry{
				DeliveryDate:         dateStr,
				DeliveryHour:         3,
				DeliveryInterval:     1,
				SettlementPoint:      "LZ_HOUSTON",
				SettlementPointPrice: 100.0,
			})
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.token = "mock-token"

		start := truncateDay(nowCT)
		end := start.Add(24 * time.Hour)
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", start, end).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).Return(nil).Once()

		prices, _, err := base.getPricesForDate(context.Background(), nowCT, "LZ_HOUSTON")
		require.NoError(t, err)
		require.Len(t, prices, 24)

		// Hour 0 (HE 1): $0.05
		assert.InDelta(t, 0.05, prices[0].DollarsPerKWH, 1e-6)
		// Hour 1 (HE 2): missing, so should use Hour 0's price ($0.05)
		assert.InDelta(t, 0.05, prices[1].DollarsPerKWH, 1e-6)
		// Hour 2 (HE 3): $0.10
		assert.InDelta(t, 0.10, prices[2].DollarsPerKWH, 1e-6)
		// PeriodName should be empty
		assert.Empty(t, prices[0].PeriodName)
	})

	t.Run("DAM Future Prices and DB Caching After 1:30 PM CT", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Date(2026, time.October, 7, 14, 0, 0, 0, ctLocation) // 2:00 PM CT
		todayStart := truncateDay(nowCT)
		todayEnd := todayStart.Add(24 * time.Hour)
		todayStr := nowCT.Format("2006-01-02")

		tomorrow := nowCT.AddDate(0, 0, 1)
		tomorrowStart := truncateDay(tomorrow)
		tomorrowEnd := tomorrowStart.Add(24 * time.Hour)
		tomorrowStr := tomorrow.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			date := r.URL.Query().Get("deliveryDateFrom")
			if date == "" {
				date = r.URL.Query().Get("deliveryDate")
			}
			var entries []ercotDAMEntry
			priceMWh := 40.0 // $0.040/kWh
			if date == todayStr {
				priceMWh = 40.0
			} else if date == tomorrowStr {
				priceMWh = 60.0 // $0.060/kWh
			}
			for h := 1; h <= 24; h++ {
				entries = append(entries, ercotDAMEntry{
					DeliveryDate:         date,
					HourEnding:           h,
					DeliveryHour:         h,
					SettlementPoint:      "LZ_HOUSTON",
					SettlementPointPrice: priceMWh,
				})
			}
			_ = json.NewEncoder(w).Encode(ercotDAMAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.nowFunc = func() time.Time { return nowCT }
		base.token = "mock-token"

		ctx := context.Background()

		// Today's DAM DB query and upsert (unified ID: ercot_lz_houston, Confirmed: false)
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", todayStart, todayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24 && !p[0].Confirmed
		}), 0).Return(nil).Once()

		// Tomorrow's DAM DB query and upsert (unified ID: ercot_lz_houston, Confirmed: false)
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", tomorrowStart, tomorrowEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24 && !p[0].Confirmed
		}), 0).Return(nil).Once()

		futurePrices, err := base.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.NotEmpty(t, futurePrices)

		// Every returned future price must start after nowCT
		for _, p := range futurePrices {
			assert.True(t, p.TSStart.After(nowCT))
			assert.True(t, p.SeparateGenerationCredit)
			if p.TSStart.Before(tomorrowStart) {
				assert.InDelta(t, 0.040, p.DollarsPerKWH, 1e-6)
			} else {
				assert.InDelta(t, 0.060, p.DollarsPerKWH, 1e-6)
			}
		}

		// Subsequent call should hit in-memory cache without DB or API calls
		cachedFuture, err := base.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Equal(t, futurePrices, cachedFuture)

		mockDB.AssertExpectations(t)
	})

	t.Run("DAM Tomorrow Skipped Before 1:30 PM CT When Today Has >= 11 Hours", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Date(2026, time.October, 7, 10, 0, 0, 0, ctLocation) // 10:00 AM CT
		todayStart := truncateDay(nowCT)
		todayEnd := todayStart.Add(24 * time.Hour)
		todayStr := nowCT.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			date := r.URL.Query().Get("deliveryDateFrom")
			if date == "" {
				date = r.URL.Query().Get("deliveryDate")
			}
			if date != todayStr {
				// Tomorrow should NOT be queried
				t.Fatalf("unexpected query for date: %s before 1:30 PM CT", date)
			}
			var entries []ercotDAMEntry
			for h := 1; h <= 24; h++ {
				entries = append(entries, ercotDAMEntry{
					DeliveryDate:         date,
					HourEnding:           h,
					DeliveryHour:         h,
					SettlementPoint:      "LZ_HOUSTON",
					SettlementPointPrice: 35.0,
				})
			}
			_ = json.NewEncoder(w).Encode(ercotDAMAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.nowFunc = func() time.Time { return nowCT }
		base.token = "mock-token"

		ctx := context.Background()

		// Only today is queried and upserted
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", todayStart, todayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24 && !p[0].Confirmed
		}), 0).Return(nil).Once()

		futurePrices, err := base.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)

		// 10:00 AM has hours 11:00 AM - 11:00 PM (13 future hours >= 11)
		assert.Len(t, futurePrices, 13)
		for _, p := range futurePrices {
			assert.True(t, p.TSStart.After(nowCT))
			assert.InDelta(t, 0.035, p.DollarsPerKWH, 1e-6)
		}

		mockDB.AssertExpectations(t)
	})

	t.Run("Multi-Zone Single Lookup Batches All Zones", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Date(2026, time.October, 7, 10, 0, 0, 0, ctLocation)
		todayStart := truncateDay(nowCT)
		todayEnd := todayStart.Add(24 * time.Hour)
		todayStr := nowCT.Format("2006-01-02")

		apiCallCount := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCallCount++
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotDAMEntry
			for h := 1; h <= 24; h++ {
				// Houston price: $30/MWh
				entries = append(entries, ercotDAMEntry{
					DeliveryDate:         todayStr,
					HourEnding:           h,
					DeliveryHour:         h,
					SettlementPoint:      "LZ_HOUSTON",
					SettlementPointPrice: 30.0,
				})
				// North price: $40/MWh
				entries = append(entries, ercotDAMEntry{
					DeliveryDate:         todayStr,
					HourEnding:           h,
					DeliveryHour:         h,
					SettlementPoint:      "LZ_NORTH",
					SettlementPointPrice: 40.0,
				})
			}
			_ = json.NewEncoder(w).Encode(ercotDAMAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.nowFunc = func() time.Time { return nowCT }
		base.token = "mock-token"

		ctx := context.Background()

		// DB misses for Houston
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", todayStart, todayEnd).Return([]types.PriceState{}, nil).Once()
		// Upserts for both Houston and North from the single response!
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).Return(nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_north", mock.Anything, 0).Return(nil).Once()

		// 1. Query Houston -> triggers 1 API call, caches BOTH Houston and North
		houstonPrices, err := base.getDAMPricesForDate(ctx, nowCT, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Len(t, houstonPrices, 24)
		assert.InDelta(t, 0.030, houstonPrices[0].DollarsPerKWH, 1e-6)
		assert.Equal(t, 1, apiCallCount)

		// 2. Query North -> hits memory cache immediately, 0 additional API calls!
		northPrices, err := base.getDAMPricesForDate(ctx, nowCT, "LZ_NORTH")
		require.NoError(t, err)
		assert.Len(t, northPrices, 24)
		assert.InDelta(t, 0.040, northPrices[0].DollarsPerKWH, 1e-6)
		assert.Equal(t, 1, apiCallCount, "North should be served from memory cache populated by Houston request")

		mockDB.AssertExpectations(t)
	})

	t.Run("Current Price Falls Back to DAM", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Now().In(ctLocation)
		dateStr := nowCT.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.URL.Path, "np6-905-cd") {
				// RTM returns 404 (not published yet for this interval)
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if strings.Contains(r.URL.Path, "np4-190-cd") {
				// DAM returns 24 hours of prices at $45/MWh ($0.045/kWh)
				var entries []ercotDAMEntry
				for h := 1; h <= 24; h++ {
					entries = append(entries, ercotDAMEntry{
						DeliveryDate:         dateStr,
						HourEnding:           fmt.Sprintf("%02d:00", h),
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 45.0,
					})
				}
				_ = json.NewEncoder(w).Encode(ercotDAMAPIResponse{Data: entries})
				return
			}
			http.NotFound(w, r)
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.token = "mock-token"

		start := truncateDay(nowCT)
		end := start.Add(24 * time.Hour)
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", start, end).Return([]types.PriceState{}, nil).Twice()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).Return(nil).Once()

		price, err := base.GetCurrentPrice(context.Background(), "LZ_HOUSTON")
		require.NoError(t, err)
		assert.InDelta(t, 0.045, price.DollarsPerKWH, 1e-6)
		assert.True(t, price.Contains(nowCT))
	})

	t.Run("Subscription Key Header Sent", func(t *testing.T) {
		headersReceived := make(map[string]string)
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headersReceived["Ocp-Apim-Subscription-Key"] = r.Header.Get("Ocp-Apim-Subscription-Key")
			headersReceived["Accept"] = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: []ercotSPPEntry{}})
		}))
		defer apiServer.Close()

		base := &baseERCOT{
			apiURL: apiServer.URL,
			apiKey: "my-secret-ercot-key",
			token:  "mock-token",
			client: apiServer.Client(),
		}

		_, _, _ = base.fetchERCOTRTM(context.Background(), time.Now())
		assert.Equal(t, "my-secret-ercot-key", headersReceived["Ocp-Apim-Subscription-Key"])
		assert.Equal(t, "application/json", headersReceived["Accept"])
	})

	t.Run("Token Exchange and In-Memory Caching", func(t *testing.T) {
		tokenRequests := 0
		var receivedUser, receivedPass, receivedGrant string
		authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenRequests++
			_ = r.ParseForm()
			receivedUser = r.Form.Get("username")
			receivedPass = r.Form.Get("password")
			receivedGrant = r.Form.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id_token":            "jwt-test-id-token",
				"expires_in":          "3600",
				"id_token_expires_in": "3600",
			})
		}))
		defer authServer.Close()

		headersReceived := make(map[string]string)
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headersReceived["Ocp-Apim-Subscription-Key"] = r.Header.Get("Ocp-Apim-Subscription-Key")
			headersReceived["Authorization"] = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: []ercotSPPEntry{}})
		}))
		defer apiServer.Close()

		base := &baseERCOT{
			apiURL:   apiServer.URL,
			apiKey:   "test-sub-key",
			tokenURL: authServer.URL,
			username: "testuser@example.com",
			password: "secretpassword123",
			client:   apiServer.Client(),
		}

		// First call should exchange username/password for token
		_, _, _ = base.fetchERCOTRTM(context.Background(), time.Now())
		assert.Equal(t, 1, tokenRequests)
		assert.Equal(t, "testuser@example.com", receivedUser)
		assert.Equal(t, "secretpassword123", receivedPass)
		assert.Equal(t, "password", receivedGrant)
		assert.Equal(t, "test-sub-key", headersReceived["Ocp-Apim-Subscription-Key"])
		assert.Equal(t, "Bearer jwt-test-id-token", headersReceived["Authorization"])

		// Second call should reuse cached token without hitting authServer again
		_, _, _ = base.fetchERCOTRTM(context.Background(), time.Now())
		assert.Equal(t, 1, tokenRequests, "expected token to be reused from in-memory cache")
	})

	t.Run("Missing Credentials Returns Informative Error", func(t *testing.T) {
		base := &baseERCOT{
			apiURL: "http://example.com",
		}
		_, err := base.getToken(context.Background())
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot username and password are required")
	})

	t.Run("Empty Load Zone Returns Error", func(t *testing.T) {
		base := &baseERCOT{}
		ctx := context.Background()

		_, err := base.Zone("")
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = base.GetCurrentPrice(ctx, "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = base.GetFuturePrices(ctx, "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, _, err = base.getPricesForDate(ctx, time.Now(), "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = base.getDAMPricesForDate(ctx, time.Now(), "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = base.GetConfirmedPrices(ctx, "", time.Now(), time.Now().Add(time.Hour))
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		emptyZone := &baseERCOTZone{base: base, loadZone: ""}
		_, err = emptyZone.GetCurrentPrice(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = emptyZone.GetFuturePrices(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")

		_, err = emptyZone.GetConfirmedPrices(ctx, time.Now(), time.Now().Add(time.Hour))
		require.Error(t, err)
		assert.ErrorContains(t, err, "ercot load zone is required")
	})

	t.Run("GetConfirmedPrices Only Returns RTM and Does Not Fallback to DAM", func(t *testing.T) {
		ctx := context.Background()
		nowCT := time.Date(2026, time.October, 7, 14, 25, 0, 0, ctLocation) // 2:25 PM CT
		dayStart := truncateDay(nowCT)
		completedEnd := nowCT.Truncate(time.Hour) // 2:00 PM CT (completed window)
		tomorrowStart := dayStart.AddDate(0, 0, 1)
		todayStr := dayStart.Format("2006-01-02")
		tomorrowStr := tomorrowStart.Format("2006-01-02")

		// 1. Success case: RTM prices returned for completed window
		baseSuccess := &baseERCOT{
			nowFunc: func() time.Time { return nowCT },
			cachedPrices: map[string][]types.Price{
				"LZ_HOUSTON_" + dayStart.Format("20060102"): {
					{
						Provider:      "ercot",
						TSStart:       dayStart,
						TSEnd:         dayStart.Add(time.Hour),
						DollarsPerKWH: 0.050,
					},
				},
			},
		}
		prices, err := baseSuccess.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, completedEnd)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		assert.Equal(t, 0.050, prices[0].DollarsPerKWH)

		// 2. Failure case: RTM missing, DAM available for completed window.
		// GetConfirmedPrices should return error and NOT fall back to DAM.
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "np6-905-cd") {
				// RTM returns 404
				http.NotFound(w, r)
				return
			}
			if strings.Contains(r.URL.Path, "np4-190-cd") {
				// DAM returns 200 with data
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(ercotDAMAPIResponse{
					Data: []ercotDAMEntry{
						{
							SettlementPoint:      "LZ_HOUSTON",
							DeliveryDate:         todayStr,
							DeliveryHour:         1,
							SettlementPointPrice: 45.0,
						},
					},
				})
				return
			}
		}))
		defer apiServer.Close()

		baseFail := &baseERCOT{
			apiURL:          apiServer.URL,
			token:           "mock-token",
			client:          apiServer.Client(),
			nowFunc:         func() time.Time { return nowCT },
			cachedPrices:    make(map[string][]types.Price),
			cachedDAMPrices: make(map[string][]types.Price),
		}

		prices, err = baseFail.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, completedEnd)
		require.Error(t, err, "expected error when RTM is unavailable")
		assert.Nil(t, prices)

		// 3. Afternoon poll (14:25 CT) with fetchEnd = now + 12h (02:25 CT tomorrow):
		//    - Must clamp end to 14:00 CT and issue 0 RTM requests for tomorrow.
		//    - Must return only completed published RTM intervals (HE 1..13, 00:00..13:00),
		//      excluding carry-forward placeholders for unpublished HE 14 (13:00..14:00) and future hours (14:00..24:00).
		var todayRTMCalls, tomorrowRTMCalls atomic.Int32
		afternoonServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "np6-905-cd") {
				http.NotFound(w, r)
				return
			}
			dateFrom := r.URL.Query().Get("deliveryDateFrom")
			if dateFrom == tomorrowStr {
				tomorrowRTMCalls.Add(1)
				http.Error(w, "tomorrow rtm not published", http.StatusNotFound)
				return
			}
			if dateFrom == todayStr {
				todayRTMCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				var entries []ercotSPPEntry
				// Publish HE 1..13 (00:00 to 13:00 CT); HE 14 (13:00-14:00) not yet published, HE 15..24 in future
				for h := 1; h <= 13; h++ {
					priceMWh := 30.0
					if h == 13 {
						priceMWh = 900.0 // $0.90/kWh spike in HE 13 that fetchERCOTRTM carries forward into HE 14..24
					}
					for interval := 1; interval <= 4; interval++ {
						entries = append(entries, ercotSPPEntry{
							DeliveryDate:         todayStr,
							DeliveryHour:         h,
							DeliveryInterval:     interval,
							SettlementPoint:      "LZ_HOUSTON",
							SettlementPointPrice: priceMWh,
						})
					}
				}
				_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
				return
			}
			http.NotFound(w, r)
		}))
		defer afternoonServer.Close()

		baseAfternoon := &baseERCOT{
			apiURL:             afternoonServer.URL,
			token:              "mock-token",
			client:             afternoonServer.Client(),
			nowFunc:            func() time.Time { return nowCT },
			cachedPrices:       make(map[string][]types.Price),
			cachedPricesTime:   make(map[string]time.Time),
			cachedConfirmedEnd: make(map[string]time.Time),
			cachedDAMPrices:    make(map[string][]types.Price),
		}

		afternoonPrices, err := baseAfternoon.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, nowCT.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int32(1), todayRTMCalls.Load(), "expected 1 RTM request for today")
		assert.Equal(t, int32(0), tomorrowRTMCalls.Load(), "expected 0 RTM requests for tomorrow during afternoon poll")
		if assert.Len(t, afternoonPrices, 13, "expected only the 13 published completed hours, excluding carry-forward placeholders") {
			assert.Equal(t, dayStart, afternoonPrices[0].TSStart)
			assert.Equal(t, dayStart.Add(13*time.Hour), afternoonPrices[len(afternoonPrices)-1].TSEnd)
		}

		// 4. Empty slice when truncated start is at or after clamped end (14:00 CT >= 14:00 CT)
		emptyPrices, err := baseAfternoon.GetConfirmedPrices(ctx, "LZ_HOUSTON", nowCT, nowCT.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Empty(t, emptyPrices)
		assert.Equal(t, int32(1), todayRTMCalls.Load(), "expected 0 additional RTM requests when start >= clamped end")
		assert.Equal(t, int32(0), tomorrowRTMCalls.Load())

		// 5. Historical replay (cmd/replayplan/main.go) returns full past range when end precedes nowHourCT
		replayStart := time.Date(2026, time.October, 4, 0, 0, 0, 0, ctLocation)
		replayEnd := replayStart.AddDate(0, 0, 2).Add(2 * time.Hour) // 50 hours ending Oct 6 02:00 CT < Oct 7 14:00 CT
		replayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dateFrom := r.URL.Query().Get("deliveryDateFrom")
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			for h := 1; h <= 24; h++ {
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dateFrom,
						DeliveryHour:         h,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 40.0,
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer replayServer.Close()

		baseReplay := &baseERCOT{
			apiURL:             replayServer.URL,
			token:              "mock-token",
			client:             replayServer.Client(),
			nowFunc:            func() time.Time { return nowCT },
			cachedPrices:       make(map[string][]types.Price),
			cachedPricesTime:   make(map[string]time.Time),
			cachedConfirmedEnd: make(map[string]time.Time),
		}
		replayPrices, err := baseReplay.GetConfirmedPrices(ctx, "LZ_HOUSTON", replayStart, replayEnd)
		require.NoError(t, err)
		assert.Len(t, replayPrices, 50)
	})

	t.Run("Concurrent Multi-Zone Requests Return Correct Separate Prices", func(t *testing.T) {
		dateStr := "2026-10-06"
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			entries = append(entries, ercotSPPEntry{
				DeliveryDate:         dateStr,
				DeliveryHour:         1,
				DeliveryInterval:     1,
				SettlementPoint:      "LZ_HOUSTON",
				SettlementPointPrice: 50.0,
			})
			entries = append(entries, ercotSPPEntry{
				DeliveryDate:         dateStr,
				DeliveryHour:         1,
				DeliveryInterval:     1,
				SettlementPoint:      "LZ_NORTH",
				SettlementPointPrice: 75.0,
			})
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := &baseERCOT{
			apiURL:          apiServer.URL,
			token:           "mock-token",
			client:          apiServer.Client(),
			cachedPrices:    make(map[string][]types.Price),
			cachedDAMPrices: make(map[string][]types.Price),
		}

		ctx := context.Background()
		testDate := time.Date(2026, time.October, 6, 12, 0, 0, 0, ctLocation)

		var wg sync.WaitGroup
		var houstonPrices, northPrices []types.Price
		var houstonErr, northErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			houstonPrices, _, houstonErr = base.getPricesForDate(ctx, testDate, "LZ_HOUSTON")
		}()
		go func() {
			defer wg.Done()
			northPrices, _, northErr = base.getPricesForDate(ctx, testDate, "LZ_NORTH")
		}()
		wg.Wait()

		require.NoError(t, houstonErr)
		require.NoError(t, northErr)
		require.NotEmpty(t, houstonPrices)
		require.NotEmpty(t, northPrices)
		assert.InDelta(t, 0.050, houstonPrices[0].DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.075, northPrices[0].DollarsPerKWH, 1e-6)
	})

	t.Run("Columnar Response Parsing", func(t *testing.T) {
		rtmColumnarJSON := []byte(`{
			"fields": [
				{"name": "deliveryDate"},
				{"name": "deliveryHour"},
				{"name": "deliveryInterval"},
				{"name": "settlementPoint"},
				{"name": "settlementPointPrice"}
			],
			"data": [
				["2026-10-06", 1, 1, "LZ_HOUSTON", 45.5],
				["2026-10-06", 1, 2, "LZ_HOUSTON", 55.5]
			]
		}`)
		rtmEntries, err := parseERCOTRTMResponse(rtmColumnarJSON)
		require.NoError(t, err)
		require.Len(t, rtmEntries, 2)
		assert.Equal(t, "LZ_HOUSTON", rtmEntries[0].SettlementPoint)
		assert.Equal(t, 1, rtmEntries[0].DeliveryHour)
		assert.InDelta(t, 45.5, rtmEntries[0].SettlementPointPrice, 1e-6)

		damColumnarJSON := []byte(`{
			"fields": [
				{"name": "deliveryDate"},
				{"name": "deliveryHour"},
				{"name": "hourEnding"},
				{"name": "settlementPoint"},
				{"name": "settlementPointPrice"}
			],
			"data": [
				["2026-10-06", 1, "01:00", "LZ_NORTH", 32.75]
			]
		}`)
		damEntries, err := parseERCOTDAMResponse(damColumnarJSON)
		require.NoError(t, err)
		require.Len(t, damEntries, 1)
		assert.Equal(t, "LZ_NORTH", damEntries[0].SettlementPoint)
		assert.Equal(t, 1, damEntries[0].DeliveryHour)
		assert.InDelta(t, 32.75, damEntries[0].SettlementPointPrice, 1e-6)
	})

	t.Run("Daylight Saving Time Transitions", func(t *testing.T) {
		// Fall DST (Nov 1, 2026): 25 hours
		fallDay := time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation)
		assert.Equal(t, 25, hoursInDay(fallDay))

		// Spring DST (March 8, 2026): 23 hours
		springDay := time.Date(2026, time.March, 8, 0, 0, 0, 0, ctLocation)
		assert.Equal(t, 23, hoursInDay(springDay))

		// Standard day (Oct 7, 2026): 24 hours
		stdDay := time.Date(2026, time.October, 7, 0, 0, 0, 0, ctLocation)
		assert.Equal(t, 24, hoursInDay(stdDay))

		// Hour Ending 25 parsing in parseDAMHour
		h, ok := parseDAMHour(nil, 25)
		assert.True(t, ok)
		assert.Equal(t, 24, h)

		// Spring-Forward RTM (March 8, 2026): ERCOT skips HE 3 and reports DeliveryHour 1, 2, 4..24
		ctx := context.Background()
		springEnd := springDay.AddDate(0, 0, 1)
		springStr := springDay.Format("2006-01-02")
		var springCalls atomic.Int32
		springDB := &storagemock.MockDatabase{}
		springServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			springCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			for dh := 1; dh <= 24; dh++ {
				if dh == 3 {
					continue // Skipped on spring-forward DST day
				}
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         springStr,
						DeliveryHour:         dh,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: float64(dh * 10), // $0.010 * dh per kWh
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer springServer.Close()

		currentSpringNow := springEnd.Add(2 * time.Hour) // March 9 02:00 CDT
		baseSpring := configuredERCOT(springDB)
		baseSpring.apiURL = springServer.URL
		baseSpring.client = springServer.Client()
		baseSpring.token = "mock-token"
		baseSpring.nowFunc = func() time.Time { return currentSpringNow }

		var springUpsert []types.PriceState
		springDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", springDay, springEnd).Return([]types.PriceState{}, nil).Once()
		springDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				springUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		springConfirmed, err := baseSpring.GetConfirmedPrices(ctx, "LZ_HOUSTON", springDay, springEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(1), springCalls.Load())
		if assert.Len(t, springConfirmed, 23, "all 23 hours of spring-forward DST day must be confirmed") {
			assert.InDelta(t, 0.010, springConfirmed[0].DollarsPerKWH, 1e-6)  // HE 1 (00:00-01:00 CST)
			assert.InDelta(t, 0.020, springConfirmed[1].DollarsPerKWH, 1e-6)  // HE 2 (01:00 CST - 03:00 CDT)
			assert.InDelta(t, 0.040, springConfirmed[2].DollarsPerKWH, 1e-6)  // HE 4 (03:00-04:00 CDT)
			assert.InDelta(t, 0.240, springConfirmed[22].DollarsPerKWH, 1e-6) // HE 24 (23:00-00:00 CDT)
			assert.Equal(t, springEnd, springConfirmed[22].TSEnd)
		}
		if assert.Len(t, springUpsert, 23) {
			for i, ps := range springUpsert {
				assert.True(t, ps.Confirmed, "spring-forward hour index %d must be marked confirmed", i)
			}
		}

		// Past spring-forward day must be immutable in memory after 5-minute TTL (0 re-fetches)
		currentSpringNow = currentSpringNow.Add(10 * time.Minute)
		springConfirmedCached, err := baseSpring.GetConfirmedPrices(ctx, "LZ_HOUSTON", springDay, springEnd)
		require.NoError(t, err)
		assert.Len(t, springConfirmedCached, 23)
		assert.Equal(t, int32(1), springCalls.Load(), "completed spring-forward past day must remain immutable in memory")
		springDB.AssertExpectations(t)

		// Fall-Back RTM (Nov 1, 2026): ERCOT repeats HE 2 with DSTFlag=true for the second 01:00-02:00 CST hour
		fallEnd := fallDay.AddDate(0, 0, 1)
		fallStr := fallDay.Format("2006-01-02")
		var fallCalls atomic.Int32
		fallDB := &storagemock.MockDatabase{}
		fallServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fallCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			var rows [][]any
			addHour := func(dh int, dst bool, priceMWh float64) {
				for interval := 1; interval <= 4; interval++ {
					rows = append(rows, []any{fallStr, dh, interval, "LZ_HOUSTON", priceMWh, dst})
				}
			}
			addHour(1, false, 10.0)
			addHour(2, false, 20.0) // 01:00-02:00 CDT
			addHour(2, true, 250.0) // 01:00-02:00 CST (repeated hour)
			for dh := 3; dh <= 24; dh++ {
				addHour(dh, false, float64(dh*10))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": []map[string]string{
					{"name": "deliveryDate"},
					{"name": "deliveryHour"},
					{"name": "deliveryInterval"},
					{"name": "settlementPoint"},
					{"name": "settlementPointPrice"},
					{"name": "DSTFlag"},
				},
				"data": rows,
			})
		}))
		defer fallServer.Close()

		currentFallNow := fallEnd.Add(2 * time.Hour) // Nov 2 02:00 CST
		baseFall := configuredERCOT(fallDB)
		baseFall.apiURL = fallServer.URL
		baseFall.client = fallServer.Client()
		baseFall.token = "mock-token"
		baseFall.nowFunc = func() time.Time { return currentFallNow }

		var fallUpsert []types.PriceState
		fallDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", fallDay, fallEnd).Return([]types.PriceState{}, nil).Once()
		fallDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				fallUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		fallConfirmed, err := baseFall.GetConfirmedPrices(ctx, "LZ_HOUSTON", fallDay, fallEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(1), fallCalls.Load())
		if assert.Len(t, fallConfirmed, 25, "all 25 hours of fall-back DST day must be confirmed") {
			assert.InDelta(t, 0.010, fallConfirmed[0].DollarsPerKWH, 1e-6)  // HE 1
			assert.InDelta(t, 0.020, fallConfirmed[1].DollarsPerKWH, 1e-6)  // HE 2 CDT
			assert.InDelta(t, 0.250, fallConfirmed[2].DollarsPerKWH, 1e-6)  // HE 2 CST (DSTFlag=true)
			assert.InDelta(t, 0.030, fallConfirmed[3].DollarsPerKWH, 1e-6)  // HE 3 CST
			assert.InDelta(t, 0.240, fallConfirmed[24].DollarsPerKWH, 1e-6) // HE 24 CST
			assert.Equal(t, fallEnd, fallConfirmed[24].TSEnd)
		}
		if assert.Len(t, fallUpsert, 25) {
			for i, ps := range fallUpsert {
				assert.True(t, ps.Confirmed, "fall-back hour index %d must be marked confirmed", i)
			}
		}

		currentFallNow = currentFallNow.Add(10 * time.Minute)
		fallConfirmedCached, err := baseFall.GetConfirmedPrices(ctx, "LZ_HOUSTON", fallDay, fallEnd)
		require.NoError(t, err)
		assert.Len(t, fallConfirmedCached, 25)
		assert.Equal(t, int32(1), fallCalls.Load(), "completed fall-back past day must remain immutable in memory")
		fallDB.AssertExpectations(t)
	})

	t.Run("Token Expiry Buffer Underflow", func(t *testing.T) {
		base := &baseERCOT{
			nowFunc: func() time.Time {
				return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			},
		}
		expiresIn := 120
		buffer := 5 * time.Minute
		if time.Duration(expiresIn)*time.Second <= buffer {
			buffer = time.Duration(expiresIn/2) * time.Second
		}
		expiry := base.now().Add(time.Duration(expiresIn)*time.Second - buffer)
		assert.True(t, expiry.After(base.now()), "expiry must be in future, not past")
		assert.Equal(t, 60*time.Second, expiry.Sub(base.now()))
	})

	t.Run("Intraday Partial Confirmation in Database", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		nowCT := time.Date(2026, time.October, 7, 10, 30, 0, 0, ctLocation) // 10:30 AM CT
		dateStr := nowCT.Format("2006-01-02")

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			// Supply all 4 15-minute intervals for HE 1..10 (hours 00:00 to 10:00)
			for h := 1; h <= 10; h++ {
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dateStr,
						DeliveryHour:         h,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 40.0 + float64(h),
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.token = "mock-token"
		base.nowFunc = func() time.Time { return nowCT }

		start := truncateDay(nowCT)
		end := start.AddDate(0, 0, 1)
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", start, end).Return([]types.PriceState{}, nil).Once()

		var capturedUpsert []types.PriceState
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				capturedUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		prices, confirmedEnd, err := base.getPricesForDate(context.Background(), nowCT, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Len(t, prices, 24)
		assert.Equal(t, start.Add(10*time.Hour), confirmedEnd)

		require.NotEmpty(t, capturedUpsert)
		// For hours ending before 10:30 AM (hours 0..9, ending up to 10:00 AM), Confirmed must be true.
		// For hours ending after 10:30 AM (hour 10 ending 11:00 AM, and forward), Confirmed must be false.
		for _, ps := range capturedUpsert {
			if ps.Price.TSEnd.Before(nowCT) || ps.Price.TSEnd.Equal(nowCT) {
				assert.True(t, ps.Confirmed, "elapsed interval must be confirmed")
			} else {
				assert.False(t, ps.Confirmed, "future/in-progress interval must not be confirmed")
			}
		}
	})

	t.Run("Delayed Final Sample Excludes Partial Hour Until All Four Intervals Arrive", func(t *testing.T) {
		ctx := context.Background()
		mockDB := &storagemock.MockDatabase{}

		// Poll just after midnight (Oct 8 00:02 CT) when Oct 7 HE 24 (23:00-24:00) is missing interval 4
		yesterday := time.Date(2026, time.October, 7, 0, 0, 0, 0, ctLocation)
		yesterdayEnd := yesterday.AddDate(0, 0, 1)
		yesterdayStr := yesterday.Format("2006-01-02")
		todayStr := yesterdayEnd.Format("2006-01-02")
		currentNow := yesterdayEnd.Add(2 * time.Minute) // Oct 8 00:02 CT

		var finalSamplePublished atomic.Bool
		var yesterdayAPICalls atomic.Int32

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			dateFrom := r.URL.Query().Get("deliveryDateFrom")
			if dateFrom == todayStr {
				// Oct 8 HE 1 (00:00-01:00) has only intervals 1..3 published
				entries := []ercotSPPEntry{
					{DeliveryDate: todayStr, DeliveryHour: 1, DeliveryInterval: 1, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 20.0},
					{DeliveryDate: todayStr, DeliveryHour: 1, DeliveryInterval: 2, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 20.0},
					{DeliveryDate: todayStr, DeliveryHour: 1, DeliveryInterval: 3, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 20.0},
				}
				_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
				return
			}
			if dateFrom != yesterdayStr {
				http.NotFound(w, r)
				return
			}

			yesterdayAPICalls.Add(1)
			var entries []ercotSPPEntry
			for h := 1; h <= 23; h++ {
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         yesterdayStr,
						DeliveryHour:         h,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 30.0,
					})
				}
			}
			// HE 24: intervals 1, 2, 3 at $40/MWh, plus duplicate interval 3 so raw row count is 4 without interval 4
			entries = append(entries,
				ercotSPPEntry{DeliveryDate: yesterdayStr, DeliveryHour: 24, DeliveryInterval: 1, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 40.0},
				ercotSPPEntry{DeliveryDate: yesterdayStr, DeliveryHour: 24, DeliveryInterval: 2, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 40.0},
				ercotSPPEntry{DeliveryDate: yesterdayStr, DeliveryHour: 24, DeliveryInterval: 3, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 40.0},
				ercotSPPEntry{DeliveryDate: yesterdayStr, DeliveryHour: 24, DeliveryInterval: 3, SettlementPoint: "LZ_HOUSTON", SettlementPointPrice: 40.0},
			)
			if finalSamplePublished.Load() {
				// Delayed 4th sample arrives at $200/MWh -> full 4-interval mean is (40+40+40+200)/4 = $80/MWh ($0.080/kWh)
				entries = append(entries, ercotSPPEntry{
					DeliveryDate:         yesterdayStr,
					DeliveryHour:         24,
					DeliveryInterval:     4,
					SettlementPoint:      "LZ_HOUSTON",
					SettlementPointPrice: 200.0,
				})
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.client = apiServer.Client()
		base.token = "mock-token"
		base.nowFunc = func() time.Time { return currentNow }

		// 1. At 00:02 CT, HE 24 is missing interval 4: only 23 hours are confirmed, and DB marks HE 24 Confirmed=false
		var firstUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", yesterday, yesterdayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				firstUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmed1, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", yesterday, currentNow.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int32(1), yesterdayAPICalls.Load())
		if assert.Len(t, confirmed1, 23, "partial HE 24 must not be returned before interval 4 arrives") {
			assert.Equal(t, yesterday.Add(23*time.Hour), confirmed1[len(confirmed1)-1].TSEnd)
		}
		if assert.Len(t, firstUpsert, 24) {
			for i := 0; i < 23; i++ {
				assert.True(t, firstUpsert[i].Confirmed, "completed hour %d must be confirmed", i)
			}
			assert.False(t, firstUpsert[23].Confirmed, "partial HE 24 missing interval 4 must not be marked confirmed in DB")
		}

		// 2. After 5-minute TTL (00:08 CT) once interval 4 is published, yesterday's incomplete cache refreshes
		finalSamplePublished.Store(true)
		currentNow = yesterdayEnd.Add(8 * time.Minute)

		var secondUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", yesterday, yesterdayEnd).Return(firstUpsert, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				secondUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmed2, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", yesterday, currentNow.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int32(2), yesterdayAPICalls.Load(), "incomplete prior-day cache must refresh after TTL")
		if assert.Len(t, confirmed2, 24, "HE 24 must be confirmed once all 4 distinct intervals arrive") {
			assert.InDelta(t, 0.080, confirmed2[23].DollarsPerKWH, 1e-6)
		}
		if assert.Len(t, secondUpsert, 24) {
			assert.True(t, secondUpsert[23].Confirmed, "completed HE 24 must now be marked confirmed in DB")
			assert.InDelta(t, 0.080, secondUpsert[23].DollarsPerKWH, 1e-6)
		}

		// 3. When Hour Ending 1 itself only has intervals 1..3 (01:10 CT on Oct 8), 0 hours are confirmed
		currentNow = yesterdayEnd.Add(70 * time.Minute) // Oct 8 01:10 CT
		todayEnd := yesterdayEnd.AddDate(0, 0, 1)
		var todayUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", yesterdayEnd, todayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				todayUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmedToday, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", yesterdayEnd, currentNow.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Empty(t, confirmedToday, "partial HE 1 must not be confirmed when confirmedEnd equals dayStart")
		if assert.Len(t, todayUpsert, 24) {
			assert.False(t, todayUpsert[0].Confirmed, "partial HE 1 must not be marked confirmed in DB")
		}

		mockDB.AssertExpectations(t)
	})

	t.Run("Missing Earlier Interval Does Not Stall Confirmed Window Or Past Day Cache", func(t *testing.T) {
		ctx := context.Background()
		mockDB := &storagemock.MockDatabase{}

		dayStart := time.Date(2026, time.October, 7, 0, 0, 0, 0, ctLocation)
		dayEnd := dayStart.AddDate(0, 0, 1)
		dayStr := dayStart.Format("2006-01-02")
		currentNow := time.Date(2026, time.October, 7, 14, 25, 0, 0, ctLocation)

		var fullDayPublished atomic.Bool
		var apiCalls atomic.Int32

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			maxCompleteHour := 13
			if fullDayPublished.Load() {
				maxCompleteHour = 24
			}
			for dh := 1; dh <= maxCompleteHour; dh++ {
				for interval := 1; interval <= 4; interval++ {
					if (dh == 5 && interval == 2) || (dh == 24 && interval == 4) {
						continue // Permanently missing 15-minute interval in HE 5 and in final hour HE 24
					}
					priceMWh := 30.0
					if dh == 5 {
						priceMWh = 60.0
					}
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         dh,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: priceMWh,
					})
				}
			}
			if !fullDayPublished.Load() {
				// Frontier HE 14 (13:00-14:00) is still missing interval 4
				for interval := 1; interval <= 3; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         14,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 200.0,
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.client = apiServer.Client()
		base.token = "mock-token"
		base.nowFunc = func() time.Time { return currentNow }

		// 1. Intraday at 14:25 CT: HE 5 missing interval 2 does not block HE 6..13, while incomplete frontier HE 14 is still excluded
		var intradayUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				intradayUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		intradayConfirmed, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, currentNow.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int32(1), apiCalls.Load())
		if assert.Len(t, intradayConfirmed, 13, "complete hours after an earlier missing interval must be confirmed while excluding partial frontier hour") {
			assert.InDelta(t, 0.060, intradayConfirmed[4].DollarsPerKWH, 1e-6)
			assert.Equal(t, dayStart.Add(13*time.Hour), intradayConfirmed[12].TSEnd)
		}
		if assert.Len(t, intradayUpsert, 24) {
			assert.True(t, intradayUpsert[12].Confirmed, "completed HE 13 must be marked confirmed")
			assert.False(t, intradayUpsert[13].Confirmed, "partial frontier HE 14 must not be marked confirmed")
		}

		// 2. Just after midnight (Oct 8 00:10 CT, < 45m after HE 24 ended): HE 24 missing interval 4 still waits
		fullDayPublished.Store(true)
		currentNow = dayEnd.Add(10 * time.Minute)

		var midnightUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(intradayUpsert, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				midnightUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		midnightConfirmed, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(2), apiCalls.Load())
		assert.Len(t, midnightConfirmed, 23, "partial HE 24 within 45 minutes of dayEnd must still wait for delayed sample")

		// 3. Past 45-minute grace window (Oct 8 01:00 CT): even with HE 24 permanently missing interval 4, all 24 hours confirm and cache becomes immutable
		currentNow = dayEnd.Add(time.Hour)

		var nextDayUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(midnightUpsert, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				nextDayUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		fullConfirmed, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(3), apiCalls.Load())
		assert.Len(t, fullConfirmed, 24)
		if assert.Len(t, nextDayUpsert, 24) {
			for i, ps := range nextDayUpsert {
				assert.True(t, ps.Confirmed, "hour %d must be marked confirmed after 45-minute grace window", i)
			}
		}

		// Advance past 5-minute TTL on next day -> immutable cache hit (0 extra API/DB calls)
		currentNow = currentNow.Add(10 * time.Minute)
		cachedConfirmed, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
		require.NoError(t, err)
		assert.Len(t, cachedConfirmed, 24)
		assert.Equal(t, int32(3), apiCalls.Load(), "past day with missing interval must become immutable once dayEnd is reached")

		mockDB.AssertExpectations(t)
	})

	t.Run("Completely Missing Earlier Hour Stops Contiguous Confirmation And Keeps Past Day Refreshable", func(t *testing.T) {
		ctx := context.Background()
		mockDB := &storagemock.MockDatabase{}

		pastDay := time.Date(2026, time.October, 6, 0, 0, 0, 0, ctLocation)
		dayEnd := pastDay.AddDate(0, 0, 1)
		dayStr := pastDay.Format("2006-01-02")
		// Next day at 02:00 CT (> 45m after both HE 5 and HE 24 ended)
		currentNow := dayEnd.Add(2 * time.Hour)

		var gapFilled atomic.Bool
		var apiCalls atomic.Int32

		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			for dh := 1; dh <= 24; dh++ {
				if dh == 5 && !gapFilled.Load() {
					continue // Completely missing hour (0 intervals -> carry-forward placeholder)
				}
				priceMWh := 40.0
				switch dh {
				case 1, 2, 3:
					priceMWh = 30.0
				case 4:
					priceMWh = 500.0 // Spike that carries forward into missing HE 5
				case 5:
					priceMWh = 70.0 // True settled price once HE 5 arrives
				}
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         dh,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: priceMWh,
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		base := configuredERCOT(mockDB)
		base.apiURL = apiServer.URL
		base.client = apiServer.Client()
		base.token = "mock-token"
		base.nowFunc = func() time.Time { return currentNow }

		// 1. With HE 5 completely missing (0 intervals) and HE 6..24 all complete (0x0f),
		// latestPublishedEnd must stop at HE 5's start (04:00 CT) and not jump to dayEnd.
		var firstUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", pastDay, dayEnd).Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				firstUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmed1, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", pastDay, dayEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(1), apiCalls.Load())
		if assert.Len(t, confirmed1, 4, "confirmed prices must stop at missing HE 5 start and exclude carry-forward and subsequent hours") {
			assert.Equal(t, pastDay, confirmed1[0].TSStart)
			assert.Equal(t, pastDay.Add(4*time.Hour), confirmed1[3].TSEnd)
			assert.InDelta(t, 0.500, confirmed1[3].DollarsPerKWH, 1e-6)
		}
		if assert.Len(t, firstUpsert, 24) {
			for i := 0; i < 4; i++ {
				assert.True(t, firstUpsert[i].Confirmed, "contiguous completed hour %d before gap must be confirmed", i)
			}
			// Carry-forward placeholder HE 5 (index 4) and subsequent complete hours HE 6..24 (indices 5..23) must NOT be confirmed
			assert.InDelta(t, 0.500, firstUpsert[4].DollarsPerKWH, 1e-6, "missing HE 5 carries forward HE 4 price in raw slice")
			for i := 4; i < 24; i++ {
				assert.False(t, firstUpsert[i].Confirmed, "hour index %d at or after 0-interval gap must not be marked confirmed in DB", i)
			}
		}

		// 2. Past day with a 0-interval gap must NOT become permanently immutable in memory:
		// advancing past the 5-minute TTL triggers a fresh API lookup instead of serving stale cache forever.
		currentNow = currentNow.Add(6 * time.Minute)
		var secondUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", pastDay, dayEnd).Return(firstUpsert, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				secondUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmed2, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", pastDay, dayEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(2), apiCalls.Load(), "past day with 0-interval gap must refresh after 5-minute TTL")
		assert.Len(t, confirmed2, 4)

		// 3. Once ERCOT backfills HE 5, the next refresh after TTL confirms all 24 hours contiguously and locks immutability.
		gapFilled.Store(true)
		currentNow = currentNow.Add(6 * time.Minute)
		var thirdUpsert []types.PriceState
		mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", pastDay, dayEnd).Return(secondUpsert, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
			Run(func(args mock.Arguments) {
				thirdUpsert = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

		confirmed3, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", pastDay, dayEnd)
		require.NoError(t, err)
		assert.Equal(t, int32(3), apiCalls.Load())
		if assert.Len(t, confirmed3, 24) {
			assert.InDelta(t, 0.070, confirmed3[4].DollarsPerKWH, 1e-6, "backfilled HE 5 must replace carry-forward placeholder")
		}
		if assert.Len(t, thirdUpsert, 24) {
			for i, ps := range thirdUpsert {
				assert.True(t, ps.Confirmed, "hour index %d must be confirmed once gap is filled", i)
			}
		}

		currentNow = currentNow.Add(10 * time.Minute)
		confirmedCached, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", pastDay, dayEnd)
		require.NoError(t, err)
		assert.Len(t, confirmedCached, 24)
		assert.Equal(t, int32(3), apiCalls.Load(), "past day must become immutable once contiguous through dayEnd")

		mockDB.AssertExpectations(t)
	})

	t.Run("Partial Earlier Hour Within Grace Window Holds Back Later Complete Hour Until Settled", func(t *testing.T) {
		ctx := context.Background()
		dayStart := time.Date(2026, time.October, 7, 0, 0, 0, 0, ctLocation)
		dayEnd := dayStart.AddDate(0, 0, 1)
		dayStr := dayStart.Format("2006-01-02")

		// Path A: 4th interval of HE 10 arrives within the 45-minute grace window.
		t.Run("Fourth Interval Arrives", func(t *testing.T) {
			mockDB := &storagemock.MockDatabase{}
			// 10:35 CT is 35 minutes after HE 10 (09:00-10:00) ended (< 45m grace window)
			currentNow := dayStart.Add(10*time.Hour + 35*time.Minute)
			var he10FourthArrived atomic.Bool

			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var entries []ercotSPPEntry
				// HE 1..9 complete (4 intervals)
				for dh := 1; dh <= 9; dh++ {
					for interval := 1; interval <= 4; interval++ {
						entries = append(entries, ercotSPPEntry{
							DeliveryDate:         dayStr,
							DeliveryHour:         dh,
							DeliveryInterval:     interval,
							SettlementPoint:      "LZ_HOUSTON",
							SettlementPointPrice: 30.0,
						})
					}
				}
				// HE 10 (09:00-10:00): partial (intervals 1..3 at $40/MWh) until 4th interval ($120/MWh -> avg $60/MWh) arrives
				for interval := 1; interval <= 3; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         10,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 40.0,
					})
				}
				if he10FourthArrived.Load() {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         10,
						DeliveryInterval:     4,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 120.0,
					})
				}
				// HE 11 (10:00-11:00): already has all 4 intervals (0x0f) at $50/MWh
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         11,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 50.0,
					})
				}
				_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
			}))
			defer apiServer.Close()

			base := configuredERCOT(mockDB)
			base.apiURL = apiServer.URL
			base.client = apiServer.Client()
			base.token = "mock-token"
			base.nowFunc = func() time.Time { return currentNow }

			// 1. At 10:35 CT (< 45m after HE 10 ended), complete HE 11 must NOT jump latestPublishedEnd past partial HE 10.
			var firstUpsert []types.PriceState
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return([]types.PriceState{}, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
				Run(func(args mock.Arguments) {
					firstUpsert = args.Get(2).([]types.PriceState)
				}).Return(nil).Once()

			confirmed1, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
			require.NoError(t, err)
			if assert.Len(t, confirmed1, 9, "partial HE 10 must hold back confirmation even when HE 11 has all 4 intervals") {
				assert.Equal(t, dayStart.Add(9*time.Hour), confirmed1[8].TSEnd)
			}
			if assert.Len(t, firstUpsert, 24) {
				assert.True(t, firstUpsert[8].Confirmed, "HE 9 must be confirmed")
				assert.False(t, firstUpsert[9].Confirmed, "partial HE 10 within 45m window must not be marked confirmed in DB")
				assert.False(t, firstUpsert[10].Confirmed, "HE 11 must not be marked confirmed ahead of partial HE 10")
			}

			// 2. At 10:41 CT (still < 45m after HE 10 ended, after 5m cache TTL), HE 10's 4th interval arrives:
			// latestPublishedEnd advances contiguously across both HE 10 and HE 11 (to 11:00 CT).
			he10FourthArrived.Store(true)
			currentNow = dayStart.Add(10*time.Hour + 41*time.Minute)

			var secondUpsert []types.PriceState
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(firstUpsert, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
				Run(func(args mock.Arguments) {
					secondUpsert = args.Get(2).([]types.PriceState)
				}).Return(nil).Once()

			_, confirmedEnd, err := base.getPricesForDate(ctx, dayStart, "LZ_HOUSTON")
			require.NoError(t, err)
			assert.Equal(t, dayStart.Add(11*time.Hour), confirmedEnd, "both HE 10 and HE 11 must advance latestPublishedEnd contiguously once 4th sample arrives")
			if assert.Len(t, secondUpsert, 24) {
				assert.True(t, secondUpsert[9].Confirmed, "completed HE 10 must now be confirmed in DB")
				assert.InDelta(t, 0.060, secondUpsert[9].DollarsPerKWH, 1e-6)
			}

			// At 11:00 CT (within 5m TTL of 10:41 CT -> memory hit? 19m > 5m -> refreshes cleanly), GetConfirmedPrices returns all 11 hours.
			currentNow = dayStart.Add(11 * time.Hour)
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(secondUpsert, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).Return(nil).Once()

			confirmed2, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
			require.NoError(t, err)
			if assert.Len(t, confirmed2, 11, "both HE 10 and HE 11 must be returned contiguously") {
				assert.InDelta(t, 0.060, confirmed2[9].DollarsPerKWH, 1e-6)
				assert.InDelta(t, 0.050, confirmed2[10].DollarsPerKWH, 1e-6)
			}

			mockDB.AssertExpectations(t)
		})

		// Path B: HE 10 remains at 3 intervals, and once the 45-minute grace window elapses, both HE 10 and HE 11 confirm contiguously.
		t.Run("Grace Window Elapses", func(t *testing.T) {
			mockDB := &storagemock.MockDatabase{}
			currentNow := dayStart.Add(10*time.Hour + 35*time.Minute) // 10:35 CT (35m after HE 10 ended)

			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var entries []ercotSPPEntry
				for dh := 1; dh <= 9; dh++ {
					for interval := 1; interval <= 4; interval++ {
						entries = append(entries, ercotSPPEntry{
							DeliveryDate:         dayStr,
							DeliveryHour:         dh,
							DeliveryInterval:     interval,
							SettlementPoint:      "LZ_HOUSTON",
							SettlementPointPrice: 30.0,
						})
					}
				}
				// HE 10 permanently has only 3 intervals at $40/MWh
				for interval := 1; interval <= 3; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         10,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 40.0,
					})
				}
				// HE 11 already has all 4 intervals at $50/MWh
				for interval := 1; interval <= 4; interval++ {
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         11,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: 50.0,
					})
				}
				_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
			}))
			defer apiServer.Close()

			base := configuredERCOT(mockDB)
			base.apiURL = apiServer.URL
			base.client = apiServer.Client()
			base.token = "mock-token"
			base.nowFunc = func() time.Time { return currentNow }

			// 1. At 10:35 CT (< 45m after HE 10 ended), latestPublishedEnd stops at 09:00 CT.
			var firstUpsert []types.PriceState
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return([]types.PriceState{}, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
				Run(func(args mock.Arguments) {
					firstUpsert = args.Get(2).([]types.PriceState)
				}).Return(nil).Once()

			_, confirmedEnd1, err := base.getPricesForDate(ctx, dayStart, "LZ_HOUSTON")
			require.NoError(t, err)
			assert.Equal(t, dayStart.Add(9*time.Hour), confirmedEnd1)
			if assert.Len(t, firstUpsert, 24) {
				assert.False(t, firstUpsert[9].Confirmed)
				assert.False(t, firstUpsert[10].Confirmed)
			}

			// 2. At 10:45 CT (exact 45-minute grace boundary after HE 10 ended, > 5m TTL),
			// HE 10 settles via fallback and HE 11 (0x0f) immediately chains contiguously to 11:00 CT.
			currentNow = dayStart.Add(10*time.Hour + 45*time.Minute)
			var secondUpsert []types.PriceState
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(firstUpsert, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
				Run(func(args mock.Arguments) {
					secondUpsert = args.Get(2).([]types.PriceState)
				}).Return(nil).Once()

			_, confirmedEnd2, err := base.getPricesForDate(ctx, dayStart, "LZ_HOUSTON")
			require.NoError(t, err)
			assert.Equal(t, dayStart.Add(11*time.Hour), confirmedEnd2, "both HE 10 and HE 11 must advance latestPublishedEnd contiguously after 45m grace window")
			if assert.Len(t, secondUpsert, 24) {
				assert.True(t, secondUpsert[9].Confirmed, "HE 10 must settle after 45m grace window")
			}

			// At 11:00 CT, GetConfirmedPrices returns all 11 hours contiguously and DB marks both HE 10 and HE 11 Confirmed=true.
			currentNow = dayStart.Add(11 * time.Hour)
			var thirdUpsert []types.PriceState
			mockDB.On("GetUtilityPrices", mock.Anything, "ercot_lz_houston", dayStart, dayEnd).Return(secondUpsert, nil).Once()
			mockDB.On("UpsertUtilityPrices", mock.Anything, "ercot_lz_houston", mock.Anything, 0).
				Run(func(args mock.Arguments) {
					thirdUpsert = args.Get(2).([]types.PriceState)
				}).Return(nil).Once()

			confirmed, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, dayEnd)
			require.NoError(t, err)
			if assert.Len(t, confirmed, 11) {
				assert.InDelta(t, 0.040, confirmed[9].DollarsPerKWH, 1e-6)
				assert.InDelta(t, 0.050, confirmed[10].DollarsPerKWH, 1e-6)
			}
			if assert.Len(t, thirdUpsert, 24) {
				assert.True(t, thirdUpsert[9].Confirmed)
				assert.True(t, thirdUpsert[10].Confirmed)
				assert.False(t, thirdUpsert[11].Confirmed)
			}

			mockDB.AssertExpectations(t)
		})
	})

	t.Run("Concurrent Cache Refresh Returns Atomic Snapshot Of Prices And ConfirmedEnd", func(t *testing.T) {
		ctx := context.Background()
		dayStart := time.Date(2026, time.October, 7, 0, 0, 0, 0, ctLocation)
		dayStr := dayStart.Format("2006-01-02")
		dateKey := "LZ_HOUSTON_" + dayStart.Format("20060102")
		nowCT := time.Date(2026, time.October, 7, 14, 10, 0, 0, ctLocation)

		// Pre-populate cache with HE 1..13 confirmed ($0.030/kWh) and HE 14 partial ($0.040/kWh, 3-interval average)
		stalePrices := make([]types.Price, 24)
		for h := 0; h < 24; h++ {
			start := dayStart.Add(time.Duration(h) * time.Hour)
			price := 0.030
			if h >= 13 {
				price = 0.040 // Stale 3-interval average on HE 14 (carried forward)
			}
			stalePrices[h] = types.Price{
				Provider:                      "ercot",
				TSStart:                       start,
				TSEnd:                         start.Add(time.Hour),
				DollarsPerKWH:                 price,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: price,
			}
		}

		// API returns complete 4-interval HE 14 with full mean $0.080/kWh
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var entries []ercotSPPEntry
			for dh := 1; dh <= 14; dh++ {
				for interval := 1; interval <= 4; interval++ {
					priceMWh := 30.0
					if dh == 14 {
						priceMWh = 80.0
					}
					entries = append(entries, ercotSPPEntry{
						DeliveryDate:         dayStr,
						DeliveryHour:         dh,
						DeliveryInterval:     interval,
						SettlementPoint:      "LZ_HOUSTON",
						SettlementPointPrice: priceMWh,
					})
				}
			}
			_ = json.NewEncoder(w).Encode(ercotAPIResponse{Data: entries})
		}))
		defer apiServer.Close()

		var nowNanos atomic.Int64
		nowNanos.Store(nowCT.UnixNano())

		base := &baseERCOT{
			apiURL: apiServer.URL,
			token:  "mock-token",
			client: apiServer.Client(),
			nowFunc: func() time.Time {
				return time.Unix(0, nowNanos.Load()).In(ctLocation)
			},
			cachedPrices: map[string][]types.Price{
				dateKey: stalePrices,
			},
			cachedPricesTime: map[string]time.Time{
				// 1ms before 5-minute TTL expiration so initial readers hit fresh cache while concurrent clock advance triggers refresh
				dateKey: nowCT.Add(-5*time.Minute + time.Millisecond),
			},
			cachedConfirmedEnd: map[string]time.Time{
				dateKey: dayStart.Add(13 * time.Hour),
			},
		}

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if idx == 5 {
					// Expire TTL mid-flight to trigger concurrent cache refresh
					nowNanos.Store(nowCT.Add(2 * time.Millisecond).UnixNano())
				}
				got, err := base.GetConfirmedPrices(ctx, "LZ_HOUSTON", dayStart, nowCT)
				require.NoError(t, err)
				switch len(got) {
				case 13:
					assert.Equal(t, dayStart.Add(13*time.Hour), got[12].TSEnd)
				case 14:
					// If HE 14 is returned as confirmed, it MUST carry the refreshed 4-interval average ($0.080), never the stale $0.040 slice
					assert.InDelta(t, 0.080, got[13].DollarsPerKWH, 1e-6, "must never pair new confirmedEnd with stale 3-interval prices slice")
				default:
					t.Errorf("unexpected confirmed length: %d", len(got))
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("Integration Real API", func(t *testing.T) {
		apiKey := os.Getenv("ERCOT_API_KEY")
		username := os.Getenv("ERCOT_USERNAME")
		password := os.Getenv("ERCOT_PASSWORD")
		token := os.Getenv("ERCOT_TOKEN")
		if token == "" {
			token = os.Getenv("ERCOT_ID_TOKEN")
		}

		if apiKey == "" || (token == "" && (username == "" || password == "")) {
			t.Skip("skipping integration test: ERCOT_API_KEY and either (ERCOT_USERNAME + ERCOT_PASSWORD) or ERCOT_TOKEN must be set")
		}

		base := &baseERCOT{
			apiURL:          "https://api.ercot.com/api/public-reports",
			apiKey:          apiKey,
			username:        username,
			password:        password,
			token:           token,
			client:          common.HTTPClient(15 * time.Second),
			cachedPrices:    make(map[string][]types.Price),
			cachedDAMPrices: make(map[string][]types.Price),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var price types.Price
		var err error
		for i := 0; i < 3; i++ {
			price, err = base.GetCurrentPrice(ctx, "LZ_HOUSTON")
			if err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
		if err != nil && strings.Contains(err.Error(), "status 5") {
			t.Skipf("ercot API returned 5xx error: %v", err)
		}
		require.NoError(t, err)
		assert.NotZero(t, price.DollarsPerKWH)
		assert.False(t, price.TSStart.IsZero())

		futurePrices, err := base.GetFuturePrices(ctx, "LZ_HOUSTON")
		if err == nil {
			assert.NotEmpty(t, futurePrices)
		}
	})
}
