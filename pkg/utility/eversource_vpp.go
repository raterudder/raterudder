package utility

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

const (
	// Eversource VPP defines on-peak hours as 12:00 PM to 8:00 PM (hours 12 to 20) on weekdays.
	eversourceVPPOnPeakStartHour      = 12
	eversourceVPPOnPeakEndHour        = 20
	eversourceVPPHistoryFetchCooldown = 15 * time.Minute
)

type vppDailyRate struct {
	date         time.Time
	onPeakPrice  float64
	offPeakPrice float64
}

type baseEversourceVPP struct {
	vppHistoryURL string
	client        *http.Client
	db            storage.Database
	nowFunc       func() time.Time

	mu               sync.Mutex
	cachedPrices     map[string][]types.Price // key: YYYY-MM-DD
	lastHistoryFetch time.Time
	sfGroup          singleflight.Group
}

func (c *baseEversourceVPP) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func configuredEversourceVPP(db storage.Database) *baseEversourceVPP {
	c := &baseEversourceVPP{
		client:       common.HTTPClient(10 * time.Second),
		cachedPrices: make(map[string][]types.Price),
		db:           db,
	}
	historyURL := lflag.String("eversource-vpp-history-url", "https://www.eversource.com/clp/vpp/vpphistory.aspx", "URL for Eversource VPP price history")

	lflag.Do(func() {
		c.vppHistoryURL = *historyURL
		if c.vppHistoryURL == "" {
			log.Ctx(context.Background()).Error("eversource-vpp-history-url is required")
			os.Exit(1)
		}
	})

	return c
}

// eversourceVPPPeriods generates the VPP periods for CT Eversource.
/*
Passive discharge events are limited to non-holiday weekdays during the Summer season (June,
July and August) only. Holidays excluded from Passive Dispatch events include:
Holidays for Passive Dispatch Events
Summer Independence Day July 4
Summer Juneteenth June 19
*/
func eversourceVPPPeriods(opts types.UtilityRateOptions, years []int) (types.UtilityVPPInfo, error) {
	if opts.VPPProgram != "ess-passive" {
		return types.UtilityVPPInfo{}, nil
	}

	var periods []types.UtilityVPPPeriod

	for _, year := range years {
		june19 := juneteenth(year)
		july4 := independenceDay(year)

		holidays := []string{
			june19.Format("2006-01-02"),
			july4.Format("2006-01-02"),
		}

		for _, month := range []time.Month{time.June, time.July, time.August} {
			p := types.UtilityVPPPeriod{
				TimePeriod: types.TimePeriod{
					Start: time.Date(year, month, 1, 0, 0, 0, 0, etLocation),
					End:   time.Date(year, month+1, 1, 0, 0, 0, 0, etLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 20},
					},
					DaysOfTheWeek: []time.Weekday{
						time.Monday,
						time.Tuesday,
						time.Wednesday,
						time.Thursday,
						time.Friday,
					},
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					LocationPtr:      etLocation,
				},
				ReserveSOC: 20,
			}
			periods = append(periods, p)
		}
	}

	return types.UtilityVPPInfo{
		Mandatory: periods,
	}, nil
}

type vppHeaderCell struct {
	text    string
	colspan int
}

type vppTableSection int

const (
	vppSectionNone vppTableSection = iota
	vppSectionThead
	vppSectionTbody
	vppSectionTfoot
)

type vppTableState struct {
	hasThead        bool
	skipTable       bool
	checkedSample   bool
	headerValidated bool
	resOnPeakCol    int
	resOffPeakCol   int

	section       vppTableSection
	inRow         bool
	skipRow       bool
	rowCellCount  int
	rowDate       time.Time
	rowDateStr    string
	rowOnPeakStr  string
	rowOffPeakStr string

	inCell      bool
	captureCell bool
	cellColspan int
	cellBuf     []byte

	theadRows   [][]vppHeaderCell
	currHeadRow []vppHeaderCell
}

func (t *vppTableState) validateHeaders(ctx context.Context) {
	if t.headerValidated || t.skipTable {
		return
	}
	if len(t.theadRows) < 2 {
		t.skipTable = true
		return
	}

	categoryRow := t.theadRows[0]
	subCategoryRow := t.theadRows[1]

	t.resOnPeakCol = -1
	t.resOffPeakCol = -1
	foundResidential := false

	currCol := 0
	for _, cell := range categoryRow {
		if strings.EqualFold(cell.text, "residential") {
			foundResidential = true
			subCol := 0
			for _, subCell := range subCategoryRow {
				if subCol >= currCol && subCol < currCol+cell.colspan {
					if strings.EqualFold(subCell.text, "on-peak") || strings.EqualFold(subCell.text, "on peak") {
						t.resOnPeakCol = subCol
					} else if strings.EqualFold(subCell.text, "off-peak") || strings.EqualFold(subCell.text, "off peak") {
						t.resOffPeakCol = subCol
					}
				}
				subCol += subCell.colspan
			}
			break
		}
		currCol += cell.colspan
	}

	if !foundResidential {
		log.Ctx(ctx).WarnContext(ctx, "failed to find residential category header in eversource vpp table")
		t.skipTable = true
		return
	}

	if t.resOnPeakCol < 0 || t.resOffPeakCol < 0 {
		log.Ctx(ctx).WarnContext(
			ctx,
			"failed to find on-peak or off-peak sub-header for residential in eversource vpp table",
			slog.Int("onPeakCol", t.resOnPeakCol),
			slog.Int("offPeakCol", t.resOffPeakCol),
		)
		t.skipTable = true
		return
	}

	t.headerValidated = true
}

func (t *vppTableState) flushCell(ctx context.Context, targetStart, targetEnd time.Time) {
	if !t.inCell {
		return
	}
	t.inCell = false
	if !t.captureCell || t.skipTable {
		return
	}

	trimmed := bytes.TrimSpace(t.cellBuf)
	if t.section == vppSectionThead {
		t.currHeadRow = append(t.currHeadRow, vppHeaderCell{
			text:    string(trimmed),
			colspan: t.cellColspan,
		})
		return
	}

	if t.section != vppSectionTbody || !t.headerValidated || t.skipRow {
		return
	}

	colIdx := t.rowCellCount - 1
	if colIdx == 0 {
		dateStr := string(trimmed)
		date, err := time.ParseInLocation("01/02/2006", dateStr, etLocation)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse date from eversource vpp row", slog.String("dateStr", dateStr), slog.Any("error", err))
			t.skipRow = true
			return
		}

		// Each table on Eversource's VPP history page corresponds to a single calendar month
		// (e.g. June) containing multi-year rate history (2026, 2025, 2024, ...).
		// Once we see the first valid date cell, skip the entire table if its month cannot overlap.
		if !t.checkedSample {
			t.checkedSample = true
			if !monthOverlaps(date.Month(), targetStart, targetEnd) {
				t.skipTable = true
				return
			}
		}

		if (!targetEnd.IsZero() && date.After(targetEnd)) || (!targetStart.IsZero() && date.Before(targetStart)) {
			t.skipRow = true
			return
		}

		t.rowDate = date
		t.rowDateStr = dateStr
		return
	}

	if colIdx == t.resOnPeakCol {
		t.rowOnPeakStr = string(trimmed)
	}
	if colIdx == t.resOffPeakCol {
		t.rowOffPeakStr = string(trimmed)
	}
}

func (t *vppTableState) flushRow(ctx context.Context, targetStart, targetEnd time.Time, rates *[]vppDailyRate) {
	if !t.inRow {
		return
	}
	t.flushCell(ctx, targetStart, targetEnd)
	t.inRow = false

	if t.section == vppSectionThead {
		rowCopy := make([]vppHeaderCell, len(t.currHeadRow))
		copy(rowCopy, t.currHeadRow)
		t.theadRows = append(t.theadRows, rowCopy)
		t.currHeadRow = t.currHeadRow[:0]
		return
	}

	if t.section != vppSectionTbody || !t.headerValidated || t.skipTable || t.skipRow || t.rowCellCount == 0 {
		return
	}

	if t.rowCellCount <= t.resOnPeakCol || t.rowCellCount <= t.resOffPeakCol {
		log.Ctx(ctx).WarnContext(
			ctx,
			"row matching target date has insufficient cells for residential on-peak/off-peak prices",
			slog.String("dateStr", t.rowDateStr),
			slog.Int("numCells", t.rowCellCount),
			slog.Int("onPeakCol", t.resOnPeakCol),
			slog.Int("offPeakCol", t.resOffPeakCol),
		)
		return
	}

	onPeakDollarsPerKWH, err1 := strconv.ParseFloat(t.rowOnPeakStr, 64)
	offPeakDollarsPerKWH, err2 := strconv.ParseFloat(t.rowOffPeakStr, 64)
	if err1 != nil || err2 != nil {
		log.Ctx(ctx).WarnContext(
			ctx,
			"failed to parse prices from eversource vpp row",
			slog.String("dateStr", t.rowDateStr),
			slog.String("onPeakStr", t.rowOnPeakStr),
			slog.String("offPeakStr", t.rowOffPeakStr),
			slog.Any("errOnPeak", err1),
			slog.Any("errOffPeak", err2),
		)
		return
	}

	*rates = append(*rates, vppDailyRate{
		date:         t.rowDate,
		onPeakPrice:  onPeakDollarsPerKWH,
		offPeakPrice: offPeakDollarsPerKWH,
	})
}

// parseVPPHistory streams HTML tokens from Eversource's VPP history page and extracts
// daily rates found across month tables without building a full DOM tree. If targetStart
// or targetEnd are provided (non-zero), non-overlapping month tables are skipped after
// their first date row and rows are filtered to [targetStart, targetEnd].
func parseVPPHistory(ctx context.Context, r io.Reader, targetStart, targetEnd time.Time) ([]vppDailyRate, error) {
	z := html.NewTokenizer(r)

	var rates []vppDailyRate
	var stack []vppTableState

	tagTable := []byte("table")
	tagThead := []byte("thead")
	tagTbody := []byte("tbody")
	tagTfoot := []byte("tfoot")
	tagTr := []byte("tr")
	tagTh := []byte("th")
	tagTd := []byte("td")
	attrColspan := []byte("colspan")

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			err := z.Err()
			if err == io.EOF {
				for i := len(stack) - 1; i >= 0; i-- {
					stack[i].flushRow(ctx, targetStart, targetEnd, &rates)
				}
				return rates, nil
			}
			return nil, err

		case html.StartTagToken:
			tn, hasAttr := z.TagName()
			if bytes.Equal(tn, tagTable) {
				skip := len(stack) > 0 && stack[len(stack)-1].skipTable
				stack = append(stack, vppTableState{
					skipTable:     skip,
					resOnPeakCol:  -1,
					resOffPeakCol: -1,
				})
				continue
			}

			if len(stack) == 0 {
				continue
			}
			t := &stack[len(stack)-1]
			if t.skipTable {
				continue
			}

			switch {
			case bytes.Equal(tn, tagThead):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				t.hasThead = true
				t.section = vppSectionThead

			case bytes.Equal(tn, tagTbody):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				t.section = vppSectionTbody
				if t.hasThead {
					t.validateHeaders(ctx)
				}

			case bytes.Equal(tn, tagTfoot):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				t.section = vppSectionTfoot

			case bytes.Equal(tn, tagTr):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				if t.hasThead && t.section == vppSectionNone {
					t.section = vppSectionTbody
					t.validateHeaders(ctx)
				}
				if t.section == vppSectionThead || (t.section == vppSectionTbody && t.headerValidated && !t.skipTable) {
					t.inRow = true
					t.skipRow = false
					t.rowCellCount = 0
					t.rowDate = time.Time{}
					t.rowDateStr = ""
					t.rowOnPeakStr = ""
					t.rowOffPeakStr = ""
				}

			case bytes.Equal(tn, tagTh) || bytes.Equal(tn, tagTd):
				if !t.inRow {
					continue
				}
				t.flushCell(ctx, targetStart, targetEnd)
				if t.skipTable {
					continue
				}
				colIdx := t.rowCellCount
				t.rowCellCount++
				t.inCell = true
				t.cellColspan = 1
				t.cellBuf = t.cellBuf[:0]
				if t.section == vppSectionThead {
					t.captureCell = true
					if hasAttr {
						for {
							key, val, more := z.TagAttr()
							if bytes.EqualFold(key, attrColspan) {
								if v, err := strconv.Atoi(strings.TrimSpace(string(val))); err == nil && v > 0 {
									t.cellColspan = v
								}
							}
							if !more {
								break
							}
						}
					}
				} else if t.section == vppSectionTbody {
					t.captureCell = !t.skipRow && (colIdx == 0 || colIdx == t.resOnPeakCol || colIdx == t.resOffPeakCol)
				} else {
					t.captureCell = false
				}
			}

		case html.TextToken:
			if len(stack) == 0 {
				continue
			}
			t := &stack[len(stack)-1]
			if t.inCell && t.captureCell && !t.skipTable {
				t.cellBuf = append(t.cellBuf, z.Text()...)
			}

		case html.EndTagToken:
			if len(stack) == 0 {
				continue
			}
			tn, _ := z.TagName()
			t := &stack[len(stack)-1]

			if bytes.Equal(tn, tagTable) {
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				stack = stack[:len(stack)-1]
				continue
			}
			if t.skipTable {
				continue
			}

			switch {
			case bytes.Equal(tn, tagTh) || bytes.Equal(tn, tagTd):
				t.flushCell(ctx, targetStart, targetEnd)
			case bytes.Equal(tn, tagTr):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
			case bytes.Equal(tn, tagThead) || bytes.Equal(tn, tagTbody) || bytes.Equal(tn, tagTfoot):
				t.flushRow(ctx, targetStart, targetEnd, &rates)
				t.section = vppSectionNone
			}
		}
	}
}

// monthOverlaps returns true if sampleMonth matches any month in the target range [targetStart, targetEnd].
func monthOverlaps(sampleMonth time.Month, targetStart, targetEnd time.Time) bool {
	if targetStart.IsZero() && targetEnd.IsZero() {
		return true
	}
	start := targetStart
	end := targetEnd
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}

	curr := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, etLocation)
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, etLocation)
	for !curr.After(last) {
		if curr.Month() == sampleMonth {
			return true
		}
		curr = curr.AddDate(0, 1, 0)
	}
	return false
}

func dailyRateToHourlyPrices(rate vppDailyRate) []types.Price {
	dateET := rate.date.In(etLocation)
	dayStart := time.Date(dateET.Year(), dateET.Month(), dateET.Day(), 0, 0, 0, 0, etLocation)
	dayEnd := dayStart.AddDate(0, 0, 1)
	numHours := int(dayEnd.Sub(dayStart) / time.Hour)
	prices := make([]types.Price, 0, numHours)

	weekday := dayStart.Weekday()
	isWeekday := weekday >= time.Monday && weekday <= time.Friday

	for tsStart := dayStart; tsStart.Before(dayEnd); tsStart = tsStart.Add(time.Hour) {
		tsStartET := tsStart.In(etLocation)
		tsEnd := tsStartET.Add(time.Hour)
		hour := tsStartET.Hour()

		isOnPeak := isWeekday && hour >= eversourceVPPOnPeakStartHour && hour < eversourceVPPOnPeakEndHour
		var priceDollarsPerKWH float64
		var name string
		if isOnPeak {
			priceDollarsPerKWH = rate.onPeakPrice
			name = "Eversource CT VPP On-Peak"
		} else {
			priceDollarsPerKWH = rate.offPeakPrice
			name = "Eversource CT VPP Off-Peak"
		}

		prices = append(prices, types.Price{
			TSStart:       tsStartET,
			TSEnd:         tsEnd,
			DollarsPerKWH: priceDollarsPerKWH,
			PeriodName:    name,
		})
	}
	return prices
}

func eversourceVPPHalfYearKey(t time.Time) int {
	half := 0
	if t.Month() >= time.July {
		half = 1
	}
	return t.Year()*2 + half
}

func calendarDayDistance(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	days := int(ua.Sub(ub) / (24 * time.Hour))
	if days < 0 {
		return -days
	}
	return days
}

// fillEversourceVPPWeekendRates synthesizes missing Saturday and Sunday Off-Peak rates
// in [targetStart, targetEnd] when vpphistory.aspx omits weekend rows. It copies offPeakPrice
// from the nearest parsed weekday in the same semi-annual period (Jan-Jun or Jul-Dec),
// falling back across half-year boundaries when needed, and preserves explicit weekend rows.
func fillEversourceVPPWeekendRates(rates []vppDailyRate, targetStart, targetEnd time.Time) []vppDailyRate {
	if len(rates) == 0 || targetStart.IsZero() || targetEnd.IsZero() {
		return rates
	}

	startDay := truncateDay(targetStart.In(etLocation))
	endDay := truncateDay(targetEnd.In(etLocation))
	if startDay.After(endDay) {
		return rates
	}

	existingDates := make(map[string]bool, len(rates))
	var weekdayRates []vppDailyRate
	for _, r := range rates {
		day := truncateDay(r.date.In(etLocation))
		existingDates[day.Format("2006-01-02")] = true
		if wd := day.Weekday(); wd >= time.Monday && wd <= time.Friday {
			weekdayRates = append(weekdayRates, vppDailyRate{
				date:         day,
				onPeakPrice:  r.onPeakPrice,
				offPeakPrice: r.offPeakPrice,
			})
		}
	}

	if len(weekdayRates) == 0 {
		return rates
	}

	slices.SortFunc(weekdayRates, func(a, b vppDailyRate) int {
		return a.date.Compare(b.date)
	})

	out := make([]vppDailyRate, len(rates))
	copy(out, rates)

	for curr := startDay; !curr.After(endDay); curr = curr.AddDate(0, 0, 1) {
		wd := curr.Weekday()
		if wd != time.Saturday && wd != time.Sunday {
			continue
		}
		if existingDates[curr.Format("2006-01-02")] {
			continue
		}

		targetHalf := eversourceVPPHalfYearKey(curr)
		var bestDonor vppDailyRate
		foundSameHalf := false
		bestDiff := 0

		for _, w := range weekdayRates {
			if eversourceVPPHalfYearKey(w.date) != targetHalf {
				continue
			}
			diff := calendarDayDistance(curr, w.date)
			if !foundSameHalf || diff < bestDiff {
				foundSameHalf = true
				bestDiff = diff
				bestDonor = w
			}
		}

		if !foundSameHalf {
			for i, w := range weekdayRates {
				diff := calendarDayDistance(curr, w.date)
				if i == 0 || diff < bestDiff {
					bestDiff = diff
					bestDonor = w
				}
			}
		}

		out = append(out, vppDailyRate{
			date:         curr,
			onPeakPrice:  bestDonor.offPeakPrice,
			offPeakPrice: bestDonor.offPeakPrice,
		})
	}

	return out
}

func (c *baseEversourceVPP) fetchHistoryAndCache(ctx context.Context) error {
	_, err, _ := c.sfGroup.Do("vppHistoryFetch", func() (interface{}, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", c.vppHistoryURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for eversource vpp history: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch eversource vpp history: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("eversource vpp history returned status code %d", resp.StatusCode)
		}

		now := c.now().In(etLocation)
		today := truncateDay(now)
		targetStart := today.AddDate(0, 0, -30)
		targetEnd := today.AddDate(0, 0, 2)

		rates, err := parseVPPHistory(ctx, resp.Body, targetStart, targetEnd)
		if err != nil {
			return nil, fmt.Errorf("failed to parse eversource vpp history: %w", err)
		}
		rates = fillEversourceVPPWeekendRates(rates, targetStart, targetEnd)

		tomorrowDate := today.AddDate(0, 0, 1)
		for _, r := range rates {
			if truncateDay(r.date.In(etLocation)).Equal(tomorrowDate) {
				log.Ctx(ctx).DebugContext(
					ctx,
					"eversource vpp history contains rates for tomorrow",
					slog.Time("nowET", now),
					slog.Time("tomorrowDate", tomorrowDate),
				)
				break
			}
		}

		c.mu.Lock()
		c.lastHistoryFetch = now
		toUpsert := make([]types.PriceState, 0, len(rates)*24)
		for _, r := range rates {
			hourly := dailyRateToHourlyPrices(r)
			dateStr := truncateDay(r.date.In(etLocation)).Format("2006-01-02")
			c.cachedPrices[dateStr] = hourly

			for _, p := range hourly {
				toUpsert = append(toUpsert, types.PriceState{
					Price:     p,
					Confirmed: true,
					TSUpdated: now,
				})
			}
		}
		c.mu.Unlock()

		if c.db != nil && len(toUpsert) > 0 {
			threeDaysAgo := today.AddDate(0, 0, -3)

			// Check if we already have data in the database from 3 days ago till now.
			existingRecent, err := c.db.GetUtilityPrices(ctx, "eversource_ct_vpp", threeDaysAgo, now)
			if err == nil && len(existingRecent) > 0 {
				// We already have recent data in DB; ignore historical rates older than 3 days ago.
				filteredUpsert := make([]types.PriceState, 0, len(toUpsert))
				for _, ps := range toUpsert {
					if !ps.Price.TSStart.Before(threeDaysAgo) {
						filteredUpsert = append(filteredUpsert, ps)
					}
				}
				toUpsert = filteredUpsert
			}
			// If len(existingRecent) == 0, we don't have data for recent days, so we upsert all 30 days.

			if len(toUpsert) > 0 {
				if err := c.db.UpsertUtilityPrices(ctx, "eversource_ct_vpp", toUpsert, 0); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to upsert eversource vpp prices to database", slog.Any("error", err))
				}
			}
		}
		return nil, nil
	})
	return err
}

func (c *baseEversourceVPP) getPricesForRange(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	startET := start.In(etLocation)
	endET := end.In(etLocation)
	if !startET.Before(endET) {
		return []types.Price{}, nil
	}

	firstDay := truncateDay(startET)
	lastDay := truncateDay(endET.Add(-time.Nanosecond))
	now := c.now().In(etLocation)
	today := truncateDay(now)

	// Step 1: Check if all days in [firstDay, lastDay] are already in memory cache.
	c.mu.Lock()
	allInCache := true
	onlyFutureMissing := true
	cachedPrices := []types.Price{}
	currDay := firstDay
	for !currDay.After(lastDay) {
		dateStr := currDay.Format("2006-01-02")
		prices, ok := c.cachedPrices[dateStr]
		if !ok {
			allInCache = false
			if !currDay.After(today) {
				onlyFutureMissing = false
			}
			currDay = currDay.AddDate(0, 0, 1)
			continue
		}
		for _, p := range prices {
			if !p.TSStart.Before(startET) && p.TSStart.Before(endET) {
				cachedPrices = append(cachedPrices, p)
			}
		}
		currDay = currDay.AddDate(0, 0, 1)
	}
	lastHistoryFetch := c.lastHistoryFetch
	c.mu.Unlock()

	if allInCache {
		return cachedPrices, nil
	}

	if onlyFutureMissing && !lastHistoryFetch.IsZero() && now.Sub(lastHistoryFetch) < eversourceVPPHistoryFetchCooldown {
		log.Ctx(ctx).DebugContext(
			ctx,
			"eversource vpp history not stale enough to fetch",
			slog.Time("start", startET),
			slog.Time("end", endET),
			slog.Time("lastHistoryFetch", lastHistoryFetch),
		)
		return cachedPrices, nil
	}

	// Step 2: Query database for the entire range in a single range query.
	if c.db != nil {
		dbStart := firstDay
		dbEnd := lastDay.AddDate(0, 0, 1)
		dbPrices, err := c.db.GetUtilityPrices(ctx, "eversource_ct_vpp", dbStart, dbEnd)
		if err == nil && len(dbPrices) > 0 {
			c.mu.Lock()
			byDate := make(map[string][]types.Price)
			for _, p := range dbPrices {
				dateStr := truncateDay(p.Price.TSStart.In(etLocation)).Format("2006-01-02")
				byDate[dateStr] = append(byDate[dateStr], p.Price)
			}
			for dStr, pList := range byDate {
				slices.SortFunc(pList, func(a, b types.Price) int {
					return a.TSStart.Compare(b.TSStart)
				})
				c.cachedPrices[dStr] = pList
			}
			c.mu.Unlock()

			// Check if EVERY day in [firstDay, lastDay] is present in the DB results.
			allDaysFound := true
			currDay := firstDay
			for !currDay.After(lastDay) {
				dateStr := currDay.Format("2006-01-02")
				if len(byDate[dateStr]) == 0 {
					allDaysFound = false
					break
				}
				currDay = currDay.AddDate(0, 0, 1)
			}

			if allDaysFound {
				var result []types.Price
				for _, p := range dbPrices {
					if !p.Price.TSStart.Before(startET) && p.Price.TSStart.Before(endET) {
						result = append(result, p.Price)
					}
				}
				return result, nil
			}
		}
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"fetching eversource vpp prices",
		slog.Time("start", start),
		slog.Time("end", end),
	)

	// Step 3: Fetch history from web if missing from cache and DB.
	if err := c.fetchHistoryAndCache(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	result := []types.Price{}
	currDay = firstDay
	for !currDay.After(lastDay) {
		dateStr := currDay.Format("2006-01-02")
		if prices, ok := c.cachedPrices[dateStr]; ok {
			for _, p := range prices {
				if !p.TSStart.Before(startET) && p.TSStart.Before(endET) {
					result = append(result, p)
				}
			}
		}
		currDay = currDay.AddDate(0, 0, 1)
	}
	return result, nil
}

func (c *baseEversourceVPP) getPricesForDate(ctx context.Context, date time.Time) ([]types.Price, error) {
	dayStart := truncateDay(date.In(etLocation))
	dayEnd := dayStart.AddDate(0, 0, 1)
	return c.getPricesForRange(ctx, dayStart, dayEnd)
}

func (c *baseEversourceVPP) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	now := c.now().In(etLocation)
	prices, err := c.getPricesForDate(ctx, now)
	if err != nil {
		return types.Price{}, err
	}
	for _, p := range prices {
		if p.Contains(now) {
			return p, nil
		}
	}
	return types.Price{}, fmt.Errorf("no current eversource vpp price found for %s", now.Format(time.RFC3339))
}

func (c *baseEversourceVPP) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := c.now().In(etLocation)
	today := truncateDay(now)
	tomorrowEnd := today.AddDate(0, 0, 2)

	prices, err := c.getPricesForRange(ctx, today, tomorrowEnd)
	if err != nil {
		return nil, err
	}

	var futurePrices []types.Price
	for _, p := range prices {
		if !p.TSEnd.Before(now) {
			futurePrices = append(futurePrices, p)
		}
	}
	return futurePrices, nil
}

func (c *baseEversourceVPP) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return c.getPricesForRange(ctx, start, end)
}
