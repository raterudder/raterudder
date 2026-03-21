package utility

import (
	"archive/zip"
	"bytes"
	"io"

	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"

	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	ercotReportTypeDAM  = "12331"
	ercotReportTypeRTM  = "12301"
	ercotReportTypeSCED = "12300"
)

type ercotDocumentListRes struct {
	ListDocsByRptTypeRes struct {
		DocumentList []struct {
			Document struct {
				DocID        string `json:"DocID"`
				Extension    string `json:"Extension"`
				FriendlyName string `json:"FriendlyName"`
			} `json:"Document"`
		} `json:"DocumentList"`
	} `json:"ListDocsByRptTypeRes"`
}

type BaseERCOT struct {
	client      *http.Client
	listAPIURL  string
	downloadURL string

	mu           sync.Mutex
	damCacheTime time.Time
	damCache     map[string][]types.Price

	rtmCacheTime time.Time
	rtmCache     map[string][]types.Price
	rtmRange     time.Time
}

func NewBaseERCOT() *BaseERCOT {
	return &BaseERCOT{
		client:      common.HTTPClient(time.Minute),
		listAPIURL:  "https://www.ercot.com/misapp/servlets/IceDocListJsonWS",
		downloadURL: "https://www.ercot.com/misdownload/servlets/mirDownload",
		damCache:    make(map[string][]types.Price),
		rtmCache:    make(map[string][]types.Price),
	}
}

func (e *BaseERCOT) fetchDocList(ctx context.Context, reportType string) (*ercotDocumentListRes, error) {
	url := fmt.Sprintf("%s?reportTypeId=%s", e.listAPIURL, reportType)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for doc list: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch doc list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching doc list: %d", resp.StatusCode)
	}

	var res ercotDocumentListRes
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode doc list: %w", err)
	}

	return &res, nil
}

func (e *BaseERCOT) fetchZipCSV(ctx context.Context, docID string) ([][]string, error) {
	url := fmt.Sprintf("%s?doclookupId=%s", e.downloadURL, docID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for download: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download doc %s: %w", docID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code downloading doc %s: %d", docID, resp.StatusCode)
	}

	// Pass the response body directly to the zip reader.
	// Since zip.NewReader requires an io.ReaderAt and a size, we use our custom ReaderAtWrapper
	// which allows passing the body directly without manually creating a byte slice buffer here.
	// As requested, read the entire zip response body into a byte slice to satisfy zip.NewReader.
	// zip.NewReader requires an io.ReaderAt and exact size, so buffering into memory is required here.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read doc body: %w", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, zipFile := range zipReader.File {
		if strings.HasSuffix(zipFile.Name, ".csv") {
			f, err := zipFile.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open csv from zip: %w", err)
			}
			defer f.Close()

			csvReader := csv.NewReader(f)
			csvReader.FieldsPerRecord = -1 // Allow variable number of fields
			records, err := csvReader.ReadAll()
			if err != nil {
				return nil, fmt.Errorf("failed to read csv: %w", err)
			}
			return records, nil
		}
	}

	return nil, fmt.Errorf("no csv found in zip doc %s", docID)
}

func parseHeaderMap(header []string) map[string]int {
	m := make(map[string]int)
	for i, col := range header {
		col = strings.TrimPrefix(col, "\xef\xbb\xbf")
		m[strings.TrimSpace(col)] = i
	}
	return m
}

func (e *BaseERCOT) getDAM(ctx context.Context, loadZone string) ([]types.Price, error) {
	e.mu.Lock()
	if time.Since(e.damCacheTime) < 5*time.Minute {
		if cached, ok := e.damCache[loadZone]; ok {
			e.mu.Unlock()
			return cached, nil
		}
		// If cache is valid but the zone isn't in it, return an empty array instead of bypassing cache
		e.mu.Unlock()
		return nil, fmt.Errorf("no data found for load zone %s", loadZone)
	}
	e.mu.Unlock()

	log.Ctx(ctx).DebugContext(ctx, "fetching ercot day ahead prices")
	docList, err := e.fetchDocList(ctx, ercotReportTypeDAM)
	if err != nil {
		return nil, err
	}

	var docID string
	for _, doc := range docList.ListDocsByRptTypeRes.DocumentList {
		if strings.HasSuffix(doc.Document.FriendlyName, "csv") && doc.Document.Extension == "zip" {
			docID = doc.Document.DocID
			break
		}
	}

	if docID == "" {
		return nil, fmt.Errorf("no dam csv zip found")
	}

	records, err := e.fetchZipCSV(ctx, docID)
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("dam csv is empty or missing header")
	}

	headers := parseHeaderMap(records[0])

	idxDate, okDate := headers["DeliveryDate"]
	idxHour, okHour := headers["HourEnding"]
	idxPoint, okPoint := headers["SettlementPoint"]
	idxPrice, okPrice := headers["SettlementPointPrice"]

	if !okDate || !okHour || !okPoint || !okPrice {
		return nil, fmt.Errorf("dam csv missing required headers")
	}

	pricesByZone := make(map[string][]types.Price)

	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) <= idxPrice {
			continue
		}

		zone := record[idxPoint]
		dateStr := record[idxDate]
		hourStr := record[idxHour]

		hourStr = strings.Split(hourStr, ":")[0]

		dateParts := strings.Split(dateStr, "/")
		if len(dateParts) != 3 {
			log.Ctx(ctx).WarnContext(ctx, "invalid dam date format", slog.String("val", dateStr))
			continue
		}

		month, err := strconv.Atoi(dateParts[0])
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse dam month", slog.String("val", dateParts[0]))
			continue
		}
		day, err := strconv.Atoi(dateParts[1])
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse dam day", slog.String("val", dateParts[1]))
			continue
		}
		year, err := strconv.Atoi(dateParts[2])
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse dam year", slog.String("val", dateParts[2]))
			continue
		}
		hour, err := strconv.Atoi(hourStr)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse dam hour", slog.String("val", hourStr))
			continue
		}

		hour--
		if hour == 24 {
			hour = 23
		}

		start := time.Date(year, time.Month(month), day, hour, 0, 0, 0, ctLocation)
		end := start.Add(time.Hour)

		priceVal, err := strconv.ParseFloat(strings.TrimSpace(record[idxPrice]), 64)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse ercot dam price", slog.String("val", record[idxPrice]))
			continue
		}

		priceVal = priceVal / 1000.0

		pricesByZone[zone] = append(pricesByZone[zone], types.Price{
			TSStart:       start,
			TSEnd:         end,
			DollarsPerKWH: priceVal,
		})
	}

	for zone, prices := range pricesByZone {
		sort.Slice(prices, func(i, j int) bool {
			return prices[i].TSStart.Before(prices[j].TSStart)
		})
		pricesByZone[zone] = prices
	}

	e.mu.Lock()
	e.damCache = pricesByZone
	e.damCacheTime = time.Now()
	e.mu.Unlock()

	if prices, ok := pricesByZone[loadZone]; ok {
		return prices, nil
	}
	return nil, fmt.Errorf("no data found for load zone %s", loadZone)
}

func (e *BaseERCOT) getRTM(ctx context.Context, loadZone string, startRange, endRange time.Time) ([]types.Price, error) {
	e.mu.Lock()
	if time.Since(e.rtmCacheTime) < 5*time.Minute {
		if !startRange.Before(e.rtmRange) {
			if cached, ok := e.rtmCache[loadZone]; ok {
				e.mu.Unlock()
				return e.filterPrices(cached, startRange, endRange), nil
			}
			e.mu.Unlock()
			return nil, fmt.Errorf("no data found for load zone %s", loadZone)
		}
	}
	e.mu.Unlock()

	log.Ctx(ctx).DebugContext(ctx, "fetching ercot real time prices")
	docList, err := e.fetchDocList(ctx, ercotReportTypeRTM)
	if err != nil {
		return nil, err
	}

	var docIDs []string

	for _, doc := range docList.ListDocsByRptTypeRes.DocumentList {
		if strings.HasSuffix(doc.Document.FriendlyName, "csv") && doc.Document.Extension == "zip" {
			parts := strings.Split(doc.Document.FriendlyName, "_")
			if len(parts) >= 3 {
				dateStr := parts[1] // 20260321
				if len(dateStr) == 8 {
					year, err1 := strconv.Atoi(dateStr[0:4])
					month, err2 := strconv.Atoi(dateStr[4:6])
					day, err3 := strconv.Atoi(dateStr[6:8])

					if err1 != nil || err2 != nil || err3 != nil {
						continue
					}

					docDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, ctLocation)

					if !docDate.Before(truncateDayERCOT(startRange)) {
						docIDs = append(docIDs, doc.Document.DocID)
					}
				}
			}
		}
	}

	var pricesMu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(docIDs))

	all15MinPrices := make(map[string][]types.Price)
	sem := make(chan struct{}, 10)

	for _, docID := range docIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()

			records, err := e.fetchZipCSV(ctx, id)
			if err != nil {
				errCh <- err
				return
			}

			if len(records) < 2 {
				return
			}

			headers := parseHeaderMap(records[0])

			idxDate, okDate := headers["DeliveryDate"]
			idxHour, okHour := headers["DeliveryHour"]
			idxInt, okInt := headers["DeliveryInterval"]
			idxZone, okZone := headers["SettlementPointName"]
			idxPrice, okPrice := headers["SettlementPointPrice"]

			if !okDate || !okHour || !okInt || !okZone || !okPrice {
				return
			}

			docPrices := make(map[string][]types.Price)
			for i := 1; i < len(records); i++ {
				record := records[i]
				if len(record) <= idxPrice {
					continue
				}

				zone := record[idxZone]
				dateStr := record[idxDate]
				hourStr := record[idxHour]
				intervalStr := record[idxInt]

				dateParts := strings.Split(dateStr, "/")
				if len(dateParts) != 3 {
					log.Ctx(ctx).WarnContext(ctx, "invalid rtm date format", slog.String("val", dateStr))
					continue
				}

				month, err1 := strconv.Atoi(dateParts[0])
				day, err2 := strconv.Atoi(dateParts[1])
				year, err3 := strconv.Atoi(dateParts[2])
				hour, err4 := strconv.Atoi(hourStr)
				interval, err5 := strconv.Atoi(intervalStr)

				if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to parse rtm date or time fields")
					continue
				}

				hour--
				min := (interval - 1) * 15

				start := time.Date(year, time.Month(month), day, hour, min, 0, 0, ctLocation)
				end := start.Add(15 * time.Minute)

				priceVal, err := strconv.ParseFloat(strings.TrimSpace(record[idxPrice]), 64)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to parse rtm price", slog.String("val", record[idxPrice]))
					continue
				}

				docPrices[zone] = append(docPrices[zone], types.Price{
					TSStart:       start,
					TSEnd:         end,
					DollarsPerKWH: priceVal / 1000.0,
				})
			}

			pricesMu.Lock()
			for z, pts := range docPrices {
				all15MinPrices[z] = append(all15MinPrices[z], pts...)
			}
			pricesMu.Unlock()

		}(docID)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		log.Ctx(ctx).WarnContext(ctx, "error fetching ercot rtm document", slog.Any("error", err))
	}

	hourlyPricesByZone := make(map[string][]types.Price)

	for zone, pts := range all15MinPrices {
		sort.Slice(pts, func(i, j int) bool {
			return pts[i].TSStart.Before(pts[j].TSStart)
		})

		var hourlyPrices []types.Price
		if len(pts) > 0 {
			var currentStart time.Time
			var sum float64
			var count int

			for _, p := range pts {
				pStartHour := time.Date(p.TSStart.Year(), p.TSStart.Month(), p.TSStart.Day(), p.TSStart.Hour(), 0, 0, 0, ctLocation)

				if currentStart.IsZero() {
					currentStart = pStartHour
					sum = p.DollarsPerKWH
					count = 1
				} else if currentStart.Equal(pStartHour) {
					sum += p.DollarsPerKWH
					count++
				} else {
					hourlyPrices = append(hourlyPrices, types.Price{
						TSStart:       currentStart,
						TSEnd:         currentStart.Add(time.Hour),
						DollarsPerKWH: sum / float64(count),
					})
					currentStart = pStartHour
					sum = p.DollarsPerKWH
					count = 1
				}
			}

			if count > 0 {
				hourlyPrices = append(hourlyPrices, types.Price{
					TSStart:       currentStart,
					TSEnd:         currentStart.Add(time.Hour),
					DollarsPerKWH: sum / float64(count),
				})
			}
		}
		hourlyPricesByZone[zone] = hourlyPrices
	}

	e.mu.Lock()
	e.rtmCache = hourlyPricesByZone
	e.rtmCacheTime = time.Now()
	e.rtmRange = startRange
	e.mu.Unlock()

	if prices, ok := hourlyPricesByZone[loadZone]; ok {
		return e.filterPrices(prices, startRange, endRange), nil
	}

	return nil, fmt.Errorf("no data found for load zone %s", loadZone)
}

func (e *BaseERCOT) filterPrices(prices []types.Price, start, end time.Time) []types.Price {
	var res []types.Price
	for _, p := range prices {
		if (p.TSStart.Equal(start) || p.TSStart.After(start)) && p.TSEnd.Before(end.Add(time.Second)) {
			res = append(res, p)
		}
	}
	return res
}

func (e *BaseERCOT) getSCED(ctx context.Context, loadZone string) (types.Price, error) {
	log.Ctx(ctx).DebugContext(ctx, "fetching ercot sced estimates for current price")
	docList, err := e.fetchDocList(ctx, ercotReportTypeSCED)
	if err != nil {
		return types.Price{}, err
	}

	var docID string
	for _, doc := range docList.ListDocsByRptTypeRes.DocumentList {
		if strings.HasSuffix(doc.Document.FriendlyName, "csv") && doc.Document.Extension == "zip" {
			docID = doc.Document.DocID
			break
		}
	}

	if docID == "" {
		return types.Price{}, fmt.Errorf("no sced csv zip found")
	}

	records, err := e.fetchZipCSV(ctx, docID)
	if err != nil {
		return types.Price{}, err
	}

	if len(records) < 2 {
		return types.Price{}, fmt.Errorf("sced csv is empty or missing header")
	}

	headers := parseHeaderMap(records[0])

	idxZone, okZone := headers["SettlementPoint"]
	idxPrice, okPrice := headers["SettlementPointPrice"]

	if !okZone || !okPrice {
		return types.Price{}, fmt.Errorf("sced csv missing required headers")
	}

	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) <= idxPrice {
			continue
		}

		if record[idxZone] != loadZone {
			continue
		}

		priceVal, err := strconv.ParseFloat(strings.TrimSpace(record[idxPrice]), 64)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse ercot sced price", slog.String("val", record[idxPrice]))
			continue
		}

		now := time.Now().In(ctLocation)
		start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, ctLocation)
		end := start.Add(time.Hour)

		return types.Price{
			TSStart:       start,
			TSEnd:         end,
			DollarsPerKWH: priceVal / 1000.0,
		}, nil
	}

	return types.Price{}, fmt.Errorf("no sced price found for zone %s", loadZone)
}

func (e *BaseERCOT) GetCurrentPrice(ctx context.Context, loadZone string) (types.Price, error) {
	now := time.Now().In(ctLocation)

	startRange := now.Add(-2 * time.Hour)
	prices, err := e.getRTM(ctx, loadZone, startRange, now)
	if err == nil && len(prices) > 0 {
		return prices[len(prices)-1], nil
	}

	price, err := e.getSCED(ctx, loadZone)
	if err == nil {
		return price, nil
	}

	return types.Price{}, fmt.Errorf("no current price available for ercot")
}

func (e *BaseERCOT) GetFuturePrices(ctx context.Context, loadZone string) ([]types.Price, error) {
	return e.getDAM(ctx, loadZone)
}

func (e *BaseERCOT) GetConfirmedPrices(ctx context.Context, loadZone string, start, end time.Time) ([]types.Price, error) {
	return e.getRTM(ctx, loadZone, start, end)
}

func truncateDayERCOT(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
