package utility

// Tariff Audit Metadata
// - Provider(s): ameren (Ameren Illinois)
// - Official Sources:
//   - https://www.ameren.com/bill/rates/residential (Ameren Residential Electric Rates Portal)
//   - https://plugin.illinois.gov/understanding-the-price-to-compare/price-to-compare-ameren-illinois.html (ICC PlugIn Illinois Price to Compare)
//   - https://docs.misoenergy.org/marketreports (MISO Day-Ahead Ex-Post LMP AMIL.BGS6)
//   - Ill. C. C. No. 1 - Electric Service: Rate PBR-R / Rate DS-1 (5th Rev. Sheets 8.015-8.016, Docket No. 25-0083) & PBR-R AAF (3.65%); Rider RBA (3.09% 2026, 1.35% 2027); Rider EE ($0.00644/kWh); Rider REA ($0.00470/kWh); Rider TS (Jan 2026 $0.02629/kWh, Jun 2026 $0.02765/kWh); Rider NMOS (Supply + Transmission Net Metering); Rider BGS & Rider RTP 115th-125th Informational Sheets (Jan-Nov 2026, Nov 2026 PEA +0.262c/kWh); Distribution Loss Factors (1.05009 through May 2026, 1.04895 Jun 2026 - May 2027)
// - Effective Window: 2026-01-01 through 2027-12-31 (monthly BGS-1 & RTP-1 supply schedules verified through 2026-11-30 with Nov 2026 PEA +0.262c/kWh carried forward through 2027-12-31; loss factor 1.04895 & Rider TS $0.02765/kWh effective 2026-06-01 through 2027-05-31 carried forward; Rate DS-1 & Rider RBA filed through 2027-12-31)
// - Revision Cadence: Monthly (Rider BGS-1 & Rider RTP-1 Informational Sheets / PEA) & Annual (Jan 1 Rate DS-1, PBR-R AAF, Rider RBA, Rider EE, EDT; Jun 1 BGS-1/RTP-1 procurement, Rider TS, Rider REA, Distribution Loss Factors)
// - Next Audit Due: 2026-12-05 (Check December 2026 126th Informational Sheet for BGS-1 and RTP-1 PEA update)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Rate DS-1 Tier 1 (0-800 kWh/mo non-summer, flat summer) distribution + mandatory volumetric riders (Rider EE, Rider REA, Illinois EDT, and PBR-R AAF + Rider RBA applied to Rate DS-1 + EDT) as GridAdditional: true; sets GridAdditional: false on Rider TS, Rider RTP-1 (Supplier Charge + Rider PER + PEA), and BGS-1 supply per Rider NMOS (supply + transmission export credit by default, or full retail credit when Pre-2025 NetMeteringCredits is enabled); keeps raw MISO LMP * amerenLossFactor in baseAmerenSmart for database cache compatibility; excludes fixed monthly Customer/Meter/Facilities charges and sub-threshold de minimis riders (MAP-R, EDITA, EDR, USS, CSESC, EEA, ASEC+MSC, CEAC, ETAC, and new Rider EUA splits).

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/singleflight"
)

type baseAmerenSmart struct {
	misoAPIURL string
	cpnodeID   string
	client     *http.Client
	db         storage.Database
	nowFunc    func() time.Time

	mu           sync.Mutex
	cachedPrices map[string][]types.Price
	sfGroup      singleflight.Group
}

func (c *baseAmerenSmart) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func configuredAmerenSmart(db storage.Database) *baseAmerenSmart {
	c := &baseAmerenSmart{
		client:       common.HTTPClient(10 * time.Second),
		cachedPrices: make(map[string][]types.Price),
		cpnodeID:     "AMIL.BGS6",
		db:           db,
	}
	misoURL := lflag.String("miso-api-url", "https://docs.misoenergy.org/marketreports", "URL for the MISO API")

	lflag.Do(func() {
		c.misoAPIURL = *misoURL
	})

	return c
}

// GetCurrentPrice gets the current price for Ameren PSP rate plan which is the
// day ahead price for the current hour.
func (c *baseAmerenSmart) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting current ameren price")

	// MISO operates in fixed EST (UTC-5)
	now := c.now().In(estLocation)

	// Ameren PSP uses day-ahead prices for real-time without true-ups
	prices, err := c.getPricesForDate(ctx, now)
	if err != nil {
		return types.Price{}, err
	}

	for _, p := range prices {
		if p.Contains(now) {
			return p, nil
		}
	}

	return types.Price{}, fmt.Errorf("no current price found for ameren")
}

// GetConfirmedPrices gets the confirmed prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *baseAmerenSmart) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var cached []types.Price
	// MISO operates in fixed EST (UTC-5)
	curr := start.In(estLocation).Truncate(time.Hour)
	func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for curr.Before(end) {
			currDate := truncateDay(curr)
			dateStr := currDate.Format("20060102")
			if prices, ok := c.cachedPrices[dateStr]; ok {
				for _, p := range prices {
					if !p.TSStart.Before(curr) && p.TSStart.Before(end) {
						cached = append(cached, p)
					}
				}
				curr = currDate.AddDate(0, 0, 1)
			} else {
				break
			}
		}
	}()

	if !curr.Before(end) {
		return cached, nil
	}

	// assume we need at least 24 more hours
	confirmed := make([]types.Price, len(cached), len(cached)+24)
	copy(confirmed, cached)

	today := truncateDay(c.now().In(estLocation))
	// Fetch for each day in the remaining range
	for d := truncateDay(curr); !d.After(end) && !d.After(today); d = d.AddDate(0, 0, 1) {
		prices, err := c.getPricesForDate(ctx, d)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch ameren prices for date", slog.Time("date", d), slog.Any("error", err))
			continue
		}

		for _, p := range prices {
			// if the price starts before the start of the range it should be included
			// because at least some part of the price is within the range
			if !p.TSStart.Before(curr) && p.TSStart.Before(end) {
				confirmed = append(confirmed, p)
			}
		}
	}

	return confirmed, nil
}

// GetFuturePrices gets the future prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *baseAmerenSmart) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	// MISO operates in fixed EST (UTC-5)
	now := c.now().In(estLocation)
	today := truncateDay(now)
	tomorrow := today.AddDate(0, 0, 1)

	pricesToday, err := c.getPricesForDate(ctx, today)
	if err != nil {
		return nil, err
	}

	pricesTomorrow, err := c.getPricesForDate(ctx, tomorrow)
	if err != nil {
		log.Ctx(ctx).DebugContext(ctx, "tomorrow ameren prices not yet available", slog.Any("error", err))
		// it's fine if tomorrow isn't available yet
	}

	var future []types.Price
	nowHour := now.Truncate(time.Hour)
	for _, p := range append(pricesToday, pricesTomorrow...) {
		// by truncating we ensure we get the current hour as well since that hour
		// isn't over yet
		if p.TSStart.After(nowHour) {
			future = append(future, p)
		}
	}

	return future, nil
}

func (c *baseAmerenSmart) getPricesForDate(ctx context.Context, date time.Time) ([]types.Price, error) {
	// MISO operates in fixed EST (UTC-5)
	dateEST := date.In(estLocation)
	dateStr := dateEST.Format("20060102")

	c.mu.Lock()
	if prices, ok := c.cachedPrices[dateStr]; ok && len(prices) > 0 {
		c.mu.Unlock()
		return prices, nil
	}
	c.mu.Unlock()

	res, err, _ := c.sfGroup.Do(dateStr, func() (interface{}, error) {
		// Recheck the cache inside the singleflight block
		c.mu.Lock()
		if prices, ok := c.cachedPrices[dateStr]; ok && len(prices) > 0 {
			c.mu.Unlock()
			return prices, nil
		}
		c.mu.Unlock()

		// Then check database
		if c.db != nil {
			log.Ctx(ctx).DebugContext(ctx,
				"ameren prices not found in cache, checking database",
				slog.String("date", dateStr),
			)
			start := truncateDay(dateEST).In(ctLocation)
			end := start.Add(24 * time.Hour)
			expectedHours := 24
			dbPrices, err := c.db.GetUtilityPrices(ctx, "ameren_psp", start, end)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get ameren prices from database", slog.Any("error", err))
			} else if len(dbPrices) >= expectedHours {
				prices := make([]types.Price, 0, len(dbPrices))
				for _, p := range dbPrices {
					price := p.Price
					price.TSStart = price.TSStart.In(ctLocation)
					price.TSEnd = price.TSEnd.In(ctLocation)
					prices = append(prices, price)
				}
				c.mu.Lock()
				c.cachedPrices[dateStr] = prices
				c.mu.Unlock()
				return prices, nil
			}
			if err == nil && len(dbPrices) > 0 {
				log.Ctx(ctx).WarnContext(ctx, "incomplete ameren prices found in database, falling back to api",
					slog.String("date", dateStr),
					slog.Int("got", len(dbPrices)),
					slog.Int("expected", expectedHours))
			}
		}

		prices, err := c.fetchMISODayAhead(ctx, dateEST, c.cpnodeID)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.cachedPrices[dateStr] = prices
		var toUpsert []types.PriceState
		nowUpsert := c.now()
		for _, p := range prices {
			toUpsert = append(toUpsert, types.PriceState{
				Price:     p,
				Confirmed: true, // all ameren prices are confirmed
				TSUpdated: nowUpsert,
			})
		}
		c.mu.Unlock()

		if c.db != nil {
			if err := c.db.UpsertUtilityPrices(ctx, "ameren_psp", toUpsert, 0); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to upsert ameren prices to database", slog.Any("error", err))
			}
		}

		return prices, nil
	})
	if err != nil {
		return nil, err
	}
	prices := res.([]types.Price)
	copied := make([]types.Price, len(prices))
	copy(copied, prices)
	return copied, nil
}

func (c *baseAmerenSmart) fetchMISODayAhead(ctx context.Context, date time.Time, cpnode string) ([]types.Price, error) {
	// MISO operates in fixed EST (UTC-5)
	dateEST := date.In(estLocation)
	dateStr := dateEST.Format("20060102")
	url := fmt.Sprintf("%s/%s_da_expost_lmp.csv", c.misoAPIURL, dateStr)
	log.Ctx(ctx).DebugContext(ctx, "fetching miso day ahead prices for ameren", slog.String("date", dateStr), slog.String("url", url))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// if today fails, try yesterday + 1 da? For now just return error
		return nil, fmt.Errorf("miso da api status: %d", resp.StatusCode)
	}

	reader := csv.NewReader(resp.Body)
	reader.FieldsPerRecord = -1
	// csv format is weird, first few rows are empty or headers.
	// let's just find the row that starts with AMIL.AMER
	var prices []types.Price
	colMap := make(map[string]int)

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "miso csv parse error", slog.Any("error", err), slog.String("url", url))
			// ignore parse errors for bad rows
			continue
		}
		if len(record) == 0 {
			continue
		}

		// Look for header row
		if len(colMap) == 0 {
			if record[0] == "Node" {
				for i, col := range record {
					colMap[strings.TrimSpace(strings.ToLower(col))] = i
				}
				_, okType := colMap["type"]
				_, okVal := colMap["value"]
				_, okNode := colMap["node"]
				if !okType || !okVal || !okNode {
					log.Ctx(ctx).WarnContext(ctx, "miso csv missing expected columns", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
					return nil, fmt.Errorf("miso csv missing expected columns")
				}
				for i := 1; i <= 24; i++ {
					_, okHe := colMap[fmt.Sprintf("he %d", i)]
					if !okHe {
						log.Ctx(ctx).WarnContext(ctx, "miso csv missing column for hour", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
						return nil, fmt.Errorf("miso csv missing column for hour")
					}
				}
			}
			continue
		}

		typeIdx := colMap["type"]
		valIdx := colMap["value"]
		nodeIdx := colMap["node"]

		// we only care about the LMP price for the given
		if len(record) > max(typeIdx, valIdx, nodeIdx) && record[nodeIdx] == cpnode && record[typeIdx] == "Loadzone" && record[valIdx] == "LMP" {
			// HE 1 to HE 24
			for i := 1; i <= 24; i++ {
				heIdx := colMap[fmt.Sprintf("he %d", i)]
				if heIdx >= len(record) {
					log.Ctx(ctx).WarnContext(ctx, "miso csv missing data for hour", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
					continue
				}
				hrStr := record[heIdx]
				val, err := strconv.ParseFloat(hrStr, 64)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "miso csv parse error", slog.Any("error", err), slog.String("value", hrStr), slog.Int("hour", i), slog.String("url", url))
					continue
				}

				// MISO publishes in fixed Eastern Standard Time (EST, UTC-5) year-round.
				// HE 1 is Hour Ending 1:00 AM EST (00:00 - 01:00 EST), which is 23:00 - 00:00 CST in winter or 00:00 - 01:00 CDT in summer.
				tEST := time.Date(dateEST.Year(), dateEST.Month(), dateEST.Day(), i-1, 0, 0, 0, estLocation)
				t := tEST.In(ctLocation)
				lmp := val / 1000.0 // $/MWh to $/kWh

				// Rider PSP says EC = [LMP + ASEC + MSC] * LossFactor
				// ASEC and MSC are excluded as they nearly always cancel out.
				// Loss multiplier source: Ameren Illinois Company d/b/a Ameren Illinois
				// Loss Multiplier and Loss Factor (secondary voltage, residential).
				// Loss factors are published annually; update this table each June.
				lossFactor := amerenLossFactor(t)
				prices = append(prices, types.Price{
					Provider:      "ameren_psp",
					TSStart:       t,
					TSEnd:         t.Add(time.Hour),
					DollarsPerKWH: lmp * lossFactor,
				})
			}
			break
		}
	}

	if len(prices) != 24 {
		log.Ctx(ctx).WarnContext(ctx, "missing miso day-ahead prices for ameren", slog.String("date", dateStr), slog.String("url", url))
		return nil, fmt.Errorf("missing miso day-ahead prices for ameren")
	}

	return prices, nil
}

// amerenLossFactor returns the applicable residential secondary voltage loss
// multiplier for the given hour.  Ameren publishes updated values each June;
// add new rows here when new values are released.
// see https://www.ameren.com/bill/rates/residential (Distrubtion Loss Factors pdf)
func amerenLossFactor(t time.Time) float64 {
	switch {
	case t.Before(time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation)):
		return 1.05009
	case t.Before(time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation)):
		return 1.04895
	default:
		return 1.04895
	}
}

func amerenUtilityInfo() types.UtilityProviderInfo {
	netMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringCredits",
		Name:        "Pre-2025 Full Net Metering",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Enable if you are grandfathered into Ameren's pre-2025 full net metering program. You are credited for your supply and delivery charges at the full retail rate.",
		Default:     false,
	}

	return types.UtilityProviderInfo{
		ID:   "ameren",
		Name: "Ameren",
		Rates: append([]types.UtilityRateInfo{
			{
				ID:      "ameren_psp",
				Name:    "Power Smart Pricing (Ameren IL)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: getAmerenAdditionalFees,
			},
			{
				ID:      "ameren_bgs",
				Name:    "Basic Generation Service (BGS) (Ameren IL)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: getAmerenBGSFees,
			},
		}, amerenMORateInfos()...),
	}
}

const (
	// Rider TS (Transmission Service Charge) - GridAdditional: false per Rider NMOS
	amerenILTSJanMay2026DollarsPerKWH = 0.02629
	amerenILTSJun2026DollarsPerKWH    = 0.02765

	// Rate DS-1 (Rate PBR-R) Tier 1 (0-800 kWh/mo non-summer, flat summer) Distribution Delivery Charge
	amerenILDS1NonSummer2026DollarsPerKWH = 0.04572
	amerenILDS1Summer2026DollarsPerKWH    = 0.07811
	amerenILDS1NonSummer2027DollarsPerKWH = 0.04687
	amerenILDS1Summer2027DollarsPerKWH    = 0.08009

	// Mandatory Illinois Delivery Riders (GridAdditional: true)
	amerenILRiderEEDollarsPerKWH  = 0.00644   // Rider EE - Energy Efficiency Programs
	amerenILRiderREADollarsPerKWH = 0.00470   // Rider REA - Renewable Energy Adjustment
	amerenILEDT2026DollarsPerKWH  = 0.0013567 // Illinois Electricity Distribution Tax (2026)
	amerenILEDT2027DollarsPerKWH  = 0.00126   // Illinois Electricity Distribution Tax (2027)
	amerenILPBRRAAFFactor         = 0.0365    // Rate PBR-R Annual Adjustment Factor (3.65%)
	amerenILRiderRBA2026Factor    = 0.0309    // Rider RBA Revenue Balancing Adjustment (2026: 3.09%)
	amerenILRiderRBA2027Factor    = 0.0135    // Rider RBA Revenue Balancing Adjustment (2027: 1.35%)
)

// getAmerenDeliveryFees returns the shared Ameren Illinois transmission (Rider TS)
// and residential distribution delivery (Rate DS-1 + mandatory volumetric riders)
// fee periods used by both Power Smart Pricing (ameren_psp) and BGS-1 (ameren_bgs).
func getAmerenDeliveryFees() []types.UtilityFeesPeriod {
	// Rider PSP and Rider BGS residential delivery fees follow Rate DS-1 (defined in Rate PBR-R).
	// Summer = June 1 – September 30. Non-summer = remainder of the year.
	//
	// NOTE: The DS-1 non-summer distribution delivery charge is officially TIERED,
	// so we apply the Tier 1 (<= 800 kWh/month) rate as a conservative hourly estimate.
	// The summer rate is flat.
	//
	// Under Rider NMOS (post-2025 net metering), excess solar/distributed exports are
	// credited for supply and transmission (Rider TS), while delivery charges remain
	// non-creditable unless Pre-2025 Full Net Metering (NetMeteringCredits) is enabled.
	// Sub-threshold de minimis riders (< 1% / < $0.0025/kWh: MAP-R, EDITA, EDR, USS,
	// CSESC, EEA, ASEC+MSC, CEAC, ETAC, and new Rider EUA splits) are excluded.
	return []types.UtilityFeesPeriod{
		// ── Rider TS: Transmission Service Charge (GridAdditional: false per Rider NMOS) ──
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILTSJanMay2026DollarsPerKWH,
			GridAdditional: false,
			Description:    "Ameren IL Rider TS Transmission Service Charge (Jan-May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILTSJun2026DollarsPerKWH,
			GridAdditional: false,
			Description:    "Ameren IL Rider TS Transmission Service Charge (Jun 2026 - Dec 2027)",
		},

		// ── 2026 Rate DS-1 Distribution Delivery Charge ──────────────────────
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1NonSummer2026DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1Summer2026DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1NonSummer2026DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},

		// ── 2027 Rate DS-1 Distribution Delivery Charge ──────────────────────
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1NonSummer2027DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1Summer2027DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILDS1NonSummer2027DollarsPerKWH,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027-Q4)",
		},

		// ── Mandatory Delivery Riders (Rider EE, Rider REA, EDT, PBR-R AAF & Rider RBA) ──
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILRiderEEDollarsPerKWH,
			GridAdditional: true,
			Description:    "Ameren IL Rider EE Energy Efficiency Programs",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILRiderREADollarsPerKWH,
			GridAdditional: true,
			Description:    "Ameren IL Rider REA Renewable Energy Adjustment",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILEDT2026DollarsPerKWH,
			GridAdditional: true,
			Description:    "Ameren IL Electricity Distribution Tax (2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  amerenILEDT2027DollarsPerKWH,
			GridAdditional: true,
			Description:    "Ameren IL Electricity Distribution Tax (2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1NonSummer2026DollarsPerKWH + amerenILEDT2026DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2026Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Non-summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1Summer2026DollarsPerKWH + amerenILEDT2026DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2026Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1NonSummer2026DollarsPerKWH + amerenILEDT2026DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2026Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Non-summer 2026-Q4)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1NonSummer2027DollarsPerKWH + amerenILEDT2027DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2027Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Non-summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1Summer2027DollarsPerKWH + amerenILEDT2027DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2027Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  (amerenILDS1NonSummer2027DollarsPerKWH + amerenILEDT2027DollarsPerKWH) * (amerenILPBRRAAFFactor + amerenILRiderRBA2027Factor),
			GridAdditional: true,
			Description:    "Ameren IL Rate PBR-R AAF & Rider RBA (Non-summer 2027-Q4)",
		},
	}
}

func getAmerenAdditionalFees(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	delivery := getAmerenDeliveryFees()

	// Rider RTP (RTP-1 Supplier Charge + Rider PER + monthly PEA) from the 115th-125th
	// Informational Sheets Supplemental to Sheet Nos. 23-23.012 and 25-25.014 of Ill. C. C. No. 1.
	// Modeled with GridAdditional: false (and empty Name so SiteFees.GetPeriods remains nil)
	// so Rider NMOS credits full supply + Rider RTP + Rider TS on exports.
	// November 2026 PEA (+0.262¢/kWh) is carried forward through 2028-01-01 until superseded.
	rtpSupply := []types.UtilityFeesPeriod{
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00745,
			Description:   "Ameren IL Rider RTP Supply Charge (Jan 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00682,
			Description:   "Ameren IL Rider RTP Supply Charge (Feb 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00622,
			Description:   "Ameren IL Rider RTP Supply Charge (Mar 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00321,
			Description:   "Ameren IL Rider RTP Supply Charge (Apr 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.01340,
			Description:   "Ameren IL Rider RTP Supply Charge (May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.03056,
			Description:   "Ameren IL Rider RTP Supply Charge (Jun 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.03016,
			Description:   "Ameren IL Rider RTP Supply Charge (Jul 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.02967,
			Description:   "Ameren IL Rider RTP Supply Charge (Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.02911,
			Description:   "Ameren IL Rider RTP Supply Charge (Sep 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00591,
			Description:   "Ameren IL Rider RTP Supply Charge (Oct 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.December, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00881,
			Description:   "Ameren IL Rider RTP Supply Charge (Nov 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.December, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00881,
			Description:   "Ameren IL Rider RTP Supply Charge (Dec 2026 - May 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.03373,
			Description:   "Ameren IL Rider RTP Supply Charge (Summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.00881,
			Description:   "Ameren IL Rider RTP Supply Charge (Non-summer 2027-Q4)",
		},
	}

	return append(delivery, rtpSupply...), nil
}

func getAmerenBGSFees(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	delivery := getAmerenDeliveryFees()

	// Ameren Illinois BGS-1 supply charges (Retail Purchased Electricity + Rider PER +Rider EUA + monthly PEA)
	// per 115th-125th Informational Sheets and ICC PlugIn Illinois Price to Compare.
	// Tier 1 non-summer (0-800 kWh/month) and flat summer rates modeled as GridAdditional: false.
	// November 2026 PEA (+0.262¢/kWh) is carried forward through 2028-01-01 until superseded.
	bgsSupply := []types.UtilityFeesPeriod{
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.05684,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Jan 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.05621,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Feb 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.05561,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Mar 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.05260,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Apr 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.06279,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.08506,
			Description:   "Ameren IL BGS-1 Summer Supply Charge (Jun 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.08466,
			Description:   "Ameren IL BGS-1 Summer Supply Charge (Jul 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.08417,
			Description:   "Ameren IL BGS-1 Summer Supply Charge (Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.08361,
			Description:   "Ameren IL BGS-1 Summer Supply Charge (Sep 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07648,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Oct 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.December, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07938,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Nov 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.December, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07938,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Dec 2026 - May 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.08823,
			Description:   "Ameren IL BGS-1 Summer Supply Charge (Summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07938,
			Description:   "Ameren IL BGS-1 Non-Summer Supply Charge (Non-summer 2027-Q4)",
		},
	}

	return append(delivery, bgsSupply...), nil
}
