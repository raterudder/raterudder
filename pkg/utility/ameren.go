package utility

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
	"github.com/raterudder/raterudder/pkg/types"
)

func amerenUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "ameren",
		Name: "Ameren",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "ameren_psp",
				Name: "Power Smart Pricing (Ameren IL)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringCredits",
						Name:        "Pre-2025 Full Net Metering",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are grandfathered into Ameren's pre-2025 full net metering program. You are credited for your supply and delivery charges at the full retail rate.",
						Default:     false,
					},
				},
			},
		},
	}
}

type BaseAmerenSmart struct {
	misoAPIURL string
	cpnodeID   string
	client     *http.Client

	mu           sync.Mutex
	cachedPrices map[string][]types.Price
}

func configuredAmerenSmart() *BaseAmerenSmart {
	c := &BaseAmerenSmart{
		client:       common.HTTPClient(time.Minute),
		cachedPrices: make(map[string][]types.Price),
		cpnodeID:     "AMIL.BGS6",
	}
	misoURL := lflag.String("miso-api-url", "https://docs.misoenergy.org/marketreports", "URL for the MISO API")

	lflag.Do(func() {
		c.misoAPIURL = *misoURL
	})

	return c
}

// GetCurrentPrice gets the current price for Ameren PSP rate plan which is the
// day ahead price for the current hour.
func (c *BaseAmerenSmart) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting current ameren price")

	now := time.Now().In(etLocation)

	// Ameren PSP uses day-ahead prices for real-time without true-ups
	prices, err := c.getPricesForDate(ctx, now)
	if err != nil {
		return types.Price{}, err
	}

	for _, p := range prices {
		if !now.Before(p.TSStart) && now.Before(p.TSEnd) {
			return p, nil
		}
	}

	return types.Price{}, fmt.Errorf("no current price found for ameren")
}

// GetConfirmedPrices gets the confirmed prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *BaseAmerenSmart) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var confirmed []types.Price

	today := truncateDay(time.Now().In(etLocation))
	// Fetch for each day in the range
	start = start.In(etLocation)
	end = end.In(etLocation)
	for d := start; !d.After(end) && !d.After(today); d = d.AddDate(0, 0, 1) {
		prices, err := c.getPricesForDate(ctx, d)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch ameren prices for date", slog.Time("date", d), slog.Any("error", err))
			continue
		}

		for _, p := range prices {
			if !p.TSStart.Before(start) && p.TSEnd.Before(end) {
				confirmed = append(confirmed, p)
			}
		}
	}

	return confirmed, nil
}

// GetFuturePrices gets the future prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *BaseAmerenSmart) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().In(etLocation)
	today := truncateDay(now)
	tomorrow := today.AddDate(0, 0, 1) // AddDate is DST-safe, unlike Add(24*time.Hour)

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
	for _, p := range append(pricesToday, pricesTomorrow...) {
		if p.TSStart.After(now.Truncate(time.Hour)) {
			future = append(future, p)
		}
	}

	return future, nil
}

func (c *BaseAmerenSmart) getPricesForDate(ctx context.Context, date time.Time) ([]types.Price, error) {
	dateStr := date.Format("20060102")

	c.mu.Lock()
	if prices, ok := c.cachedPrices[dateStr]; ok && len(prices) > 0 {
		c.mu.Unlock()
		return prices, nil
	}
	c.mu.Unlock()

	prices, err := c.fetchMISODayAhead(ctx, date, c.cpnodeID)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cachedPrices[dateStr] = prices
	c.mu.Unlock()

	return prices, nil
}

func (c *BaseAmerenSmart) fetchMISODayAhead(ctx context.Context, date time.Time, cpnode string) ([]types.Price, error) {
	dateStr := date.Format("20060102")
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

				// HE1 = 00:00 - 01:00 EST
				t := time.Date(date.Year(), date.Month(), date.Day(), i-1, 0, 0, 0, etLocation)
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
// see https://www.icc.illinois.gov/downloads/public/filing/4/390832.pdf
func amerenLossFactor(t time.Time) float64 {
	switch {
	case t.Before(time.Date(2026, time.June, 1, 0, 0, 0, 0, etLocation)):
		return 1.05009
	// TODO: 2026-2027 loss factor
	default:
		return 1.05009
	}
}

func getAmerenAdditionalFees(types.UtilityRateOptions) ([]types.UtilityAdditionalFeesPeriod, error) {
	// Rider PSP says the delivery fees are "Residential - Rate DS-1"
	// RATE PBR-R defines Rate DS-1.
	// Summer = June 1 – September 30.  Non-summer = remainder of the year.
	//
	// NOTE: The DS-1 non-summer distribution delivery charge is officially TIERED
	// but because we calculate fees at an hourly level without knowing monthly
	// cumulative consumption, we apply the first-tier rate as a
	// conservative estimate. This slightly overstates costs for customers
	// with monthly usage above 800 kWh. The summer rate is flat.
	//
	// The Ameren Illinois Transmission Service Charge is a separate per-kWh
	// charge included in the all-in price-to-compare. As of January 2026 it is
	// 2.629¢/kWh and applies regardless of season or time-of-day.
	return []types.UtilityAdditionalFeesPeriod{
		// ── 2026 Transmission Service Charge ──────────────────────────────────
		// Applies all hours, both summer and non-summer.
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH: 0.02629,
			Description:   "Ameren IL Transmission Service Charge (2026)",
		},
		// TODO: add 2027 transmission service charge

		// ── 2026 Distribution Delivery Charge ────────────────────────────────
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.04572, // first-tier non-summer rate; see tiering note above
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.07811,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2026)",
		},
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.04572, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},

		// ── 2027 Distribution Delivery Charge ────────────────────────────────
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.04687, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027)",
		},
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.08009,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2027)",
		},
		{
			UtilityPeriod: types.UtilityPeriod{
				Start:     time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:       time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				HourStart: 0,
				HourEnd:   24,
			},
			DollarsPerKWH:  0.04687, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027-Q4)",
		},
	}, nil
}
