package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat = "singleFamilyWithoutElectricHeat"
	ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat  = "multiFamilyWithoutElectricHeat"
	ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat    = "singleFamilyElectricHeat"
	ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat     = "multiFamilyElectricHeat"
)

// comEdUtilityInfo returns metadata about ComEd and its supported rate plans.
func comEdUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "comed",
		Name: "ComEd",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "comed_besh",
				Name: "Hourly Pricing Program (BESH)",
				Options: []types.UtilityRateOption{
					{
						Field: "rateClass",
						Name:  "Rate Class",
						Type:  types.UtilityOptionTypeSelect,
						Choices: []types.UtilityOptionChoice{
							{Value: ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, Name: "Residential Single Family Without Electric Space Heat"},
							{Value: ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat, Name: "Residential Multi Family Without Electric Space Heat"},
							{Value: ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat, Name: "Residential Single Family With Electric Space Heat"},
							{Value: ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat, Name: "Residential Multi Family With Electric Space Heat"},
						},
						Default: ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat,
					},
					{
						Field:       "variableDeliveryRate",
						Name:        "Delivery Time-of-Day (DTOD)",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are enrolled in ComEd's Delivery Time-of-Day pricing. 30%-47% cheaper than fixed delivery rates in off-peak hours but 2x more expensive in on-peak hours (1pm-7pm).",
						Default:     false,
					},
					{
						Field:       "netMeteringCredits",
						Name:        "Pre-2025 Full Net Metering",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are grandfathered into ComEd's pre-2025 full net metering program. You are credited for your supply and delivery charges at the full retail rate.",
						Default:     false,
					},
				},
			},
		},
	}
}

// COMED_RESID_AGG
const pjmComedPNodeID = "116472935"

// BaseComEdHourly implements the UtilityPrices interface for ComEd Hourly Energy Pricing (BESH).
type BaseComEdHourly struct {
	apiURL    string
	pjmAPIKey string
	pjmAPIURL string
	client    *http.Client
	db        storage.Database

	mu               sync.Mutex
	lastFetchTime    time.Time
	cachedPrices     []types.Price
	lastFutureFetch  time.Time
	cachedFuture     []types.Price
	historicalPrices map[int64]types.Price // Cache for historical prices (key: unix timestamp of start)
}

// configuredComEd sets up flags for ComEd and returns the instance.
// It uses lflag to register command-line flags for configuration.
func configuredComEdHourly(db storage.Database) *BaseComEdHourly {
	c := &BaseComEdHourly{
		client:           common.HTTPClient(time.Minute),
		historicalPrices: make(map[int64]types.Price),
		db:               db,
	}
	apiURL := lflag.String("comed-api-url", "https://hourlypricing.comed.com/api", "URL for the ComEd Hourly Pricing API")
	pjmURL := lflag.String("pjm-api-url", "https://api.pjm.com/api/v1/da_hrl_lmps", "URL for the PJM API")
	pjmKey := lflag.String("pjm-api-key", "", "API Key for PJM Data Miner 2 (optional)")

	lflag.Do(func() {
		c.apiURL = *apiURL
		if c.apiURL == "" {
			log.Ctx(context.Background()).Error("comed-api-url is required")
			os.Exit(1)
		}
		if _, err := url.Parse(c.apiURL); err != nil {
			log.Ctx(context.Background()).Error("failed to parse comed url", slog.String("url", c.apiURL), slog.Any("error", err))
			os.Exit(1)
		}
		c.pjmAPIURL = *pjmURL
		c.pjmAPIKey = *pjmKey
		if c.pjmAPIURL != "" {
			if _, err := url.Parse(c.pjmAPIURL); err != nil {
				log.Ctx(context.Background()).Error("failed to parse pjm url", slog.String("url", c.pjmAPIURL), slog.Any("error", err))
				os.Exit(1)
			}
		}
	})

	return c
}

// apiResponse represents the structure of the JSON returned by ComEd.
type comedPriceEntry struct {
	MillisUTC string `json:"millisUTC"`
	Price     string `json:"price"`
}

// GetConfirmedPrices returns confirmed prices for a specific time range.
// This requests 5-minute feed data and averages it into hourly buckets.
func (c *BaseComEdHourly) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	ctx = log.With(ctx, log.Ctx(ctx).With(slog.Time("start", start), slog.Time("end", end)))

	// Check if all needed hours are in cache
	c.mu.Lock()
	var cached []types.Price

	// iterate hourly
	curr := start.Truncate(time.Hour)
	allCached := true
	for curr.Before(end) {
		// key is unixtimestamp of start of hour
		if p, ok := c.historicalPrices[curr.Unix()]; ok {
			cached = append(cached, p)
		} else {
			allCached = false
		}
		curr = curr.Add(time.Hour)
	}
	c.mu.Unlock()

	if allCached {
		log.Ctx(ctx).DebugContext(ctx, "confirmed prices found in cache")
		return cached, nil
	}

	// Then check database
	if c.db != nil {
		dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", start, end)
		if err == nil && len(dbPrices) > 0 {
			// verify if all confirmed
			allConfirmed := true
			for _, p := range dbPrices {
				if !p.Confirmed {
					allConfirmed = false
					break
				}
			}

			if allConfirmed {
				log.Ctx(ctx).DebugContext(ctx, "confirmed prices found in database")
				var prices []types.Price
				c.mu.Lock()
				for _, p := range dbPrices {
					prices = append(prices, p.Price)
					c.historicalPrices[p.TSStart.Unix()] = p.Price
				}
				c.mu.Unlock()
				return prices, nil
			}
		}
	}

	log.Ctx(ctx).DebugContext(ctx, "fetching confirmed price history from api")
	prices, err := c.fetchPricesRange(ctx, start, end)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(ctLocation)
	confirmedPrices := make([]types.Price, 0, len(prices))
	var earliest time.Time
	var latest time.Time
	toUpsert := make([]types.PriceState, 0, len(prices))
	for i := len(prices) - 1; i >= 0; i-- {
		p := prices[i]
		confirmed := p.isConfirmed(now)

		if confirmed {
			confirmedPrices = append(confirmedPrices, p.Price)

			if earliest.IsZero() || p.TSStart.Before(earliest) {
				earliest = p.TSStart
			}
			if p.TSEnd.After(latest) {
				latest = p.TSEnd
			}
		} else {
			// if we don't have a full hour of data and it's recent, log that we are waiting
			if p.sampleCount < 12 && p.TSEnd.After(now.Add(-45*time.Minute)) && !p.TSEnd.After(now.Add(-5*time.Minute)) {
				log.Ctx(ctx).WarnContext(
					ctx,
					"waiting for more price data for hour",
					slog.Time("tsStart", p.TSStart),
					slog.Time("tsEnd", p.TSEnd),
					slog.Int("sampleCount", p.sampleCount))
			}
		}

		toUpsert = append(toUpsert, types.PriceState{
			Price:     p.Price,
			Confirmed: confirmed,
			TSUpdated: now,
		})
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"got comed confirmed prices",
		slog.Time("earliest", earliest),
		slog.Time("latest", latest),
		slog.Int("count", len(confirmedPrices)),
	)

	// Update cache with confirmed prices
	c.mu.Lock()
	for _, p := range confirmedPrices {
		c.historicalPrices[p.TSStart.Unix()] = p
	}
	c.mu.Unlock()

	if c.db != nil {
		if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed prices to database", slog.Any("error", err))
		}
	}

	return confirmedPrices, nil
}

type priceWithSampleCount struct {
	types.Price
	sampleCount int
}

func (p priceWithSampleCount) isConfirmed(now time.Time) bool {
	// don't confirm prices that ended within 5 minutes ago
	if p.TSEnd.After(now.Add(-5 * time.Minute)) {
		return false
	}

	// TODO: if we have less than 12 samples what does ComEd do in that case?
	// is it actually just the API that's missing data or is the underlying PJM data
	// missing as well? What does ComEd charge for that hour?

	// don't confirm prices that have less than 12 samples and ended within 45
	// minutes ago
	if p.sampleCount < 12 && p.TSEnd.After(now.Add(-45*time.Minute)) {
		return false
	}
	return true
}

// fetchPricesRange retrieves prices from the ComEd API for a specific range.
func (c *BaseComEdHourly) fetchPricesRange(ctx context.Context, start, end time.Time) ([]priceWithSampleCount, error) {
	start = start.In(ctLocation)
	end = end.In(ctLocation)

	u, err := url.Parse(c.apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid api url: %w", err)
	}

	params := url.Values{}
	params.Set("type", "5minutefeed")
	params.Set("datestart", start.Format("200601021504"))
	params.Set("dateend", end.Format("200601021504"))
	params.Set("format", "json")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	log.Ctx(ctx).DebugContext(ctx, "fetching prices from comed", slog.String("url", u.String()))

	resp, err := c.client.Do(req)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to fetch prices", "error", err)
		return nil, fmt.Errorf("failed to fetch prices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comed api returned status: %d", resp.StatusCode)
	}

	var data []comedPriceEntry
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		// Sometimes ComEd returns empty body or non-json on error or no data
		log.Ctx(ctx).ErrorContext(ctx, "failed to decode comed response", slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	log.Ctx(ctx).DebugContext(
		ctx,
		"fetched prices",
		slog.Int("count", len(data)),
		slog.String("start", start.Format(time.RFC3339)),
		slog.String("end", end.Format(time.RFC3339)),
	)

	// Map to group prices by hour
	type hourlyData struct {
		start    time.Time
		sum      float64
		count    int
		lastTime time.Time
	}
	hours := make(map[int64]*hourlyData) // Key by unix hour to handle map keys

	for _, item := range data {
		ms, err := strconv.ParseInt(item.MillisUTC, 10, 64)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse comed millisUTC", slog.String("value", item.MillisUTC), slog.Any("error", err))
			continue
		}
		centsPerKWH, err := strconv.ParseFloat(item.Price, 64)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse comed price", slog.String("value", item.Price), slog.Any("error", err))
			continue
		}

		tsEnd := time.UnixMilli(ms).In(ctLocation)
		hourStart := tsEnd.Truncate(time.Hour)
		key := hourStart.Unix()

		if _, exists := hours[key]; !exists {
			hours[key] = &hourlyData{start: hourStart}
		}
		h := hours[key]
		h.sum += centsPerKWH
		h.count++
		if tsEnd.After(h.lastTime) {
			h.lastTime = tsEnd
		}
	}

	var prices []priceWithSampleCount
	for _, h := range hours {
		avgCents := h.sum / float64(h.count)
		prices = append(prices, priceWithSampleCount{
			Price: types.Price{
				Provider: "comed_besh",
				TSStart:  h.start,
				// we used to set TSEnd to be the last value we saw but since the ComEd
				// data is sometimes unreliable that meant that there are some hours where
				// we say the end is only 40 minutes into the hour because of missing
				// data which is inaccurate
				TSEnd:         h.start.Add(time.Hour),
				DollarsPerKWH: avgCents / 100, // Cents to Dollars
			},
			sampleCount: h.count,
		})
	}

	// Sort by TSStart
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].TSStart.Before(prices[j].TSStart)
	})

	return prices, nil
}

// GetCurrentPrice returns the latest hourly-averaged price.
func (c *BaseComEdHourly) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	now := time.Now().In(ctLocation)

	c.mu.Lock()
	// we only need to fetch if it's been a new 5 minute block
	if !c.lastFetchTime.IsZero() && !now.Truncate(5*time.Minute).After(c.lastFetchTime) {
		if len(c.cachedPrices) > 0 {
			latest := c.cachedPrices[len(c.cachedPrices)-1]
			c.mu.Unlock()
			return latest, nil
		}
	}
	c.mu.Unlock()

	// Then check database
	if c.db != nil {
		start := now.Truncate(time.Hour)
		dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", start, now)
		if err == nil && len(dbPrices) > 0 {
			sort.Slice(dbPrices, func(i, j int) bool {
				return dbPrices[i].TSStart.Before(dbPrices[j].TSStart)
			})
			// use the latest price in the range
			latest := dbPrices[len(dbPrices)-1]
			// if it was updated recently and contains the current time then use it
			// we chose 2 minutes because the price should update every 5 minutes and
			// we don't want to have cached a stale price that just recently updated
			if latest.Contains(now) && time.Since(latest.TSUpdated) < 2*time.Minute {
				log.Ctx(ctx).DebugContext(ctx, "current price found in database and is fresh")
				c.mu.Lock()
				// Update memory cache since we found a fresh one in DB
				// But we need more than one price for the cache normally, or do we?
				// getCachedCurrentPrices fetched 6 hours. Let's just cache this one if that's all we have.
				c.cachedPrices = []types.Price{latest.Price}
				c.lastFetchTime = now
				c.mu.Unlock()
				return latest.Price, nil
			}
		}
	}

	// Fetch enough history to get at least the last few hours complete.
	// 6 hours back should be plenty to get full hours even with delays.
	start := now.Add(-6 * time.Hour).Truncate(time.Hour)
	prices, err := c.fetchPricesRange(ctx, start, now)
	if err != nil {
		return types.Price{}, err
	}

	if len(prices) == 0 {
		return types.Price{}, fmt.Errorf("no prices returned for current window")
	}

	rawPrices := make([]types.Price, len(prices))
	nowUpsert := time.Now()
	var toUpsert []types.PriceState
	for i, p := range prices {
		rawPrices[i] = p.Price
		toUpsert = append(toUpsert, types.PriceState{
			Price:     p.Price,
			Confirmed: p.isConfirmed(now),
			TSUpdated: nowUpsert,
		})
	}

	c.mu.Lock()
	c.cachedPrices = rawPrices
	c.lastFetchTime = now
	c.mu.Unlock()

	if c.db != nil {
		if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed current prices to database", slog.Any("error", err))
		}
	}

	latest := rawPrices[len(rawPrices)-1]
	log.Ctx(ctx).DebugContext(
		ctx,
		"got current price from api",
		slog.Float64("price", latest.DollarsPerKWH),
		slog.Time("ts", latest.TSStart),
	)
	return latest, nil
}

// GetFuturePrices returns predicted or day-ahead prices.
// Prefers PJM API if configured, otherwise returns nothing
func (c *BaseComEdHourly) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	if c.pjmAPIKey == "" {
		return nil, nil
	}

	c.mu.Lock()
	// TODO: instead we should only update if we're running out of future prices
	// but what if they change?
	if !c.lastFutureFetch.IsZero() && time.Since(c.lastFutureFetch) < 15*time.Minute {
		prices := c.cachedFuture
		c.mu.Unlock()
		return prices, nil
	}
	c.mu.Unlock()

	// Check database for future prices
	if c.db != nil {
		now := time.Now().In(etLocation)
		// we want prices from now until at least tomorrow night
		end := now.Add(48 * time.Hour)
		dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", now.Truncate(time.Hour), end)
		if err == nil && len(dbPrices) > 0 {
			// PJM updates the day-ahead prices at 1:30pm ET at which point we would
			// have 11 hours of prices, so if we have less than 11 we are past 1:30pm ET
			// and should fetch new prices
			if len(dbPrices) >= 11 {
				log.Ctx(ctx).DebugContext(ctx, "future prices found in database")
				var prices []types.Price
				for _, p := range dbPrices {
					prices = append(prices, p.Price)
				}
				c.mu.Lock()
				c.cachedFuture = prices
				c.lastFutureFetch = time.Now()
				c.mu.Unlock()
				return prices, nil
			}
		}
	}

	prices, err := c.fetchPJMDayAhead(ctx, pjmComedPNodeID)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cachedFuture = prices
	c.lastFutureFetch = time.Now()
	var toUpsert []types.PriceState
	nowUpsert := time.Now()
	nowHour := time.Now().In(ctLocation).Truncate(time.Hour)
	for _, p := range prices {
		// don't store future prices that are the current hour or earlier since they
		// might overwrite more recent or confirmed prices from the actual utility
		if !p.TSStart.After(nowHour) {
			continue
		}
		toUpsert = append(toUpsert, types.PriceState{
			Price:     p,
			Confirmed: false, // not confirmed for ComEd future prices
			TSUpdated: nowUpsert,
		})
	}
	c.mu.Unlock()

	if c.db != nil {
		if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed future prices to database", slog.Any("error", err))
		}
	}

	return prices, nil
}

// PJM API Support

type pjmItem struct {
	DatetimeBeginningEPT string  `json:"datetime_beginning_ept"`
	TotalLMPDA           float64 `json:"total_lmp_da"`
}

func (c *BaseComEdHourly) fetchPJMDayAhead(ctx context.Context, pnodeID string) ([]types.Price, error) {
	now := time.Now().In(etLocation)
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	dateRange := fmt.Sprintf("%s 00:00 to %s 23:59", today, tomorrow)

	u, err := url.Parse(c.pjmAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pjm url (%s): %w", c.pjmAPIURL, err)
	}
	q := u.Query()
	q.Set("pnode_id", pnodeID)
	q.Set("datetime_beginning_ept", dateRange)
	q.Set("format", "json")
	q.Set("fields", "datetime_beginning_ept,total_lmp_da")
	// download true removes the metadata and returns only the data
	q.Set("download", "true")
	q.Set("startRow", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", c.pjmAPIKey)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/json")

	log.Ctx(ctx).DebugContext(
		ctx,
		"fetching pjm prices",
		slog.String("url", u.String()),
		slog.String("pnodeID", pnodeID),
	)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pjm api status: %d", resp.StatusCode)
	}

	var res []pjmItem
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var prices []types.Price
	var earliest time.Time
	var latest time.Time
	for _, item := range res {
		// Parse EPT time
		t, err := time.ParseInLocation("2006-01-02T15:04:05", item.DatetimeBeginningEPT, etLocation)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse pjm time", slog.String("time", item.DatetimeBeginningEPT), slog.Any("error", err))
			continue
		}
		// make sure it's truncated to the hour
		t = t.Truncate(time.Hour)

		// Convert $/MWh to $/kWh

		// HEC = LMP x (1 MWh/ 1000 kWh) x BUF x ISUF x (1 + DLF)
		// Residential Single Family Without Electric Space Heat 0.0517 0.0459
		// Residential Multi Family Without Electric Space Heat 0.0532 0.0468
		// Residential Single Family With Electric Space Heat 0.0554 0.0473
		// Residential Multi Family With Electric Space Heat 0.0567 0.0497
		hec := (item.TotalLMPDA / 1000) * 1.0124 * 1.0002 * (1.0 + .047)

		prices = append(prices, types.Price{
			Provider:      "comed_besh",
			TSStart:       t,
			TSEnd:         t.Add(time.Hour),
			DollarsPerKWH: hec,
		})
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"fetched pjm prices",
		slog.Int("count", len(prices)),
		slog.String("pnodeID", pnodeID),
		slog.Time("earliest", earliest),
		slog.Time("latest", latest),
	)
	return prices, nil
}

// TODO: support Basic Electric Service Time of Use Pricing (Rate BEST) once pricing is announced
/*
Basic Electric Service Time of Use Electricity
Charges (BESTECs) Effective Prior to the June
Summer BESTEC (6) Nonsummer BESTEC
Morning Period Electricity Charge (MPEC)
Mid-Day Peak Period Electricity Charge (MDPPEC)
Evening Period Electricity Charge (EPEC)
Overnight Period Electricity Charge (OPEC)
NOTES:
(1) This informational sheet is supplemental to Rate BEST – Basic Electric Service Time of Use Pricing
(Rate BEST).
(2) The energy prices apply to energy provided every day during the following Central Prevailing Time
(CPT) periods: MPECs from 6:00 a.m. to 1:00 p.m., MDPPECs from 1:00 p.m. to 7:00 p.m., EPECs
from 7:00 p.m. to 9:00 p.m., and OPECs from 9:00 p.m. to 6:00 a.m.
(3) BESTECs are applied in the Supply Section on retail customer bills for each period pursuant to Rate
BEST.
(4) BESTECs include Residential Supply Base Uncollectible Cost Factors (SBUFR) as listed in
Informational Sheet No. 21.
(5) BESTECs incorporate Residential Incremental Supply Uncollectible Cost Factors (ISUFR) as listed in
Informational Sheet No. 20.
(6) The Summer BESTECs are applicable in the June, July, August, and September monthly billing
periods.
*/

func getComEdAdditionalFees(ro types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	// TODO: include 2027 prices once they are announced

	// The incremental distribution uncollectible cost factor applicable for residential
	// retail customers (IDUFR) equals the applicable IDUFR listed in Informational
	// Sheet No. 20.
	iduf := 1.0090 // for 2026

	// The applicable Delivery Reconciliation Adjustment Factor listed in Informational
	// Sheet No. 9.
	draf := 0.0 // for 2026

	// The applicable Excess Deferred Income Tax Factor listed in Informational Sheet No. 62.
	edaf := 0.0 // for 2026

	// The applicable Total Plan Adjustment Factor listed in Informational Sheet No. 65.
	tpaf := 0.06551 // for 2026

	// The applicable Revenue Balancing Adjustment Factor for a Delivery Class D listed in
	// Informational Sheet No. 18.
	var rbafd float64
	switch ro.RateClass {
	case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
		rbafd = 0.007668
	case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
		rbafd = 0.011682
	case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
		rbafd = 0.069810
	case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
		rbafd = 0.064486
	default:
		return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
	}

	// DGRAD = The applicable Distributed Generation (DG) Rebate Adjustment listed in Informational
	// Sheet No. 56.
	// provided in cents per kWh
	dgrad := 0.062 / 100 // for 2026

	// PJM Service Charge (PSC) for 2026
	psc := 1.083 / 100

	fees := []types.UtilityFeesPeriod{
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				HourStart:   0,
				HourEnd:     24,
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: psc,
			Description:   "Transmission Services Charge (PSC)",
		},
	}

	if !ro.VariableDeliveryRate {
		var dfc float64
		switch ro.RateClass {
		case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
			dfc = 0.05698
		case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
			dfc = 0.04354
		case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
			dfc = 0.02712
		case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
			dfc = 0.02576
		default:
			return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
		}
		// DFC & ADJ = DFC x (IDUF + DRAF + EDAF + TPAF + RBAFD) + DGRAD
		dfcAdj := dfc*(iduf+draf+edaf+tpaf+rbafd) + dgrad

		return append(fees, types.UtilityFeesPeriod{
			UtilityPeriod: types.UtilityPeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				HourStart:   0,
				HourEnd:     24,
				LocationPtr: ctLocation,
			},
			GridAdditional: true,
			DollarsPerKWH:  dfcAdj,
			Description:    "Distribution Facilities Charge - DFC & ADJ",
		}), nil
	} else {
		// time of use distribution facilities charges
		var morningDFC float64
		var midDayDFC float64
		var eveningDFC float64
		var nightDFC float64
		switch ro.RateClass {
		case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
			// we default to single family non-electric heating
			morningDFC = 0.04009
			midDayDFC = 0.10712
			eveningDFC = 0.03747
			nightDFC = 0.02984
		case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
			morningDFC = 0.03073
			midDayDFC = 0.08689
			eveningDFC = 0.02856
			nightDFC = 0.02251
		case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
			morningDFC = 0.01999
			midDayDFC = 0.05329
			eveningDFC = 0.01890
			nightDFC = 0.01550
		case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
			morningDFC = 0.01925
			midDayDFC = 0.04975
			eveningDFC = 0.01823
			nightDFC = 0.01512
		default:
			return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
		}
		// DFC & ADJ = DFC x (IDUF + DRAF + EDAF + TPAF + RBAFD) + DGRAD
		morningDFCAdj := morningDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		midDayDFCAdj := midDayDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		eveningDFCAdj := eveningDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		nightDFCAdj := nightDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		return append(fees, []types.UtilityFeesPeriod{
			// night (midnight - 6am)
			{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
					HourStart:   0,
					HourEnd:     6,
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  nightDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Night) - DFC & ADJ",
			},
			// morning (6am - 1pm)
			{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
					HourStart:   6,
					HourEnd:     13,
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  morningDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Morning) - DFC & ADJ",
			},
			// mid day (1pm - 7pm)
			{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
					HourStart:   13,
					HourEnd:     19,
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  midDayDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Mid Day) - DFC & ADJ",
			},
			// evening (7pm - 9pm)
			{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
					HourStart:   19,
					HourEnd:     21,
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  eveningDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Evening) - DFC & ADJ",
			},
			// night (9pm - midnight)
			{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
					HourStart:   21,
					HourEnd:     24,
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  nightDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Night) - DFC & ADJ",
			},
		}...), nil
	}
}
