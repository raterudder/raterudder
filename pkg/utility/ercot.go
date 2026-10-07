package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
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

const (
	defaultERCOTClientID = "fec253ea-0d06-4272-a5e6-b478baeecd70"
	defaultERCOTTokenURL = "https://ercotb2c.b2clogin.com/ercotb2c.onmicrosoft.com/B2C_1_PUBAPI-ROPC-FLOW/oauth2/v2.0/token"
)

// baseERCOT implements the wholesale pricing provider for ERCOT Settlement Point Prices (SPP).
type baseERCOT struct {
	apiURL           string
	apiKey           string
	tokenURL         string
	clientID         string
	username         string
	password         string
	token            string // static token override
	client           *http.Client
	db               storage.Database
	mu               sync.Mutex
	cachedPrices     map[string][]types.Price // Key: loadZone_YYYYMMDD (RTM prices)
	cachedPricesTime map[string]time.Time     // Key: loadZone_YYYYMMDD (timestamp when cached)
	cachedDAMPrices  map[string][]types.Price // Key: loadZone_YYYYMMDD (DAM prices)
	lastFutureFetch  time.Time
	nowFunc          func() time.Time
	sfGroup          singleflight.Group

	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

func (c *baseERCOT) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func hoursInDay(dayStart time.Time) int {
	nextDay := dayStart.AddDate(0, 0, 1)
	return int(nextDay.Sub(dayStart).Hours())
}

func copyPrices(prices []types.Price) []types.Price {
	if prices == nil {
		return nil
	}
	copied := make([]types.Price, len(prices))
	copy(copied, prices)
	return copied
}

type ercotTokenResponse struct {
	IDToken          string `json:"id_token"`
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        any    `json:"expires_in"`
	IDTokenExpiresIn any    `json:"id_token_expires_in"`
	Error            string `json:"error"`
	ErrorDesc        string `json:"error_description"`
}

func parseExpiresIn(val any, idVal any) int {
	for _, raw := range []any{val, idVal} {
		switch v := raw.(type) {
		case float64:
			if int(v) > 0 {
				return int(v)
			}
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		case string:
			if sec, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && sec > 0 {
				return sec
			}
		}
	}
	return 3600
}

// getToken retrieves an active ID token, using in-memory cache until expiration.
// If username and password are provided, it exchanges them via ERCOT's Azure AD B2C ROPC flow.
func (c *baseERCOT) getToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	if c.token != "" {
		defer c.tokenMu.Unlock()
		return c.token, nil
	}
	now := c.now()
	if c.cachedToken != "" && now.Before(c.tokenExpiry) {
		token := c.cachedToken
		c.tokenMu.Unlock()
		return token, nil
	}
	c.tokenMu.Unlock()

	// If no username or password provided, cannot request token
	if c.username == "" || c.password == "" {
		return "", fmt.Errorf("ercot username and password are required to request an access token (set --ercot-username and --ercot-password, or --ercot-token)")
	}

	// Singleflight to avoid concurrent duplicate token requests
	res, err, _ := c.sfGroup.Do("ercot_token", func() (any, error) {
		c.tokenMu.Lock()
		if c.cachedToken != "" && c.now().Before(c.tokenExpiry) {
			token := c.cachedToken
			c.tokenMu.Unlock()
			return token, nil
		}
		c.tokenMu.Unlock()

		tokenURL := c.tokenURL
		if tokenURL == "" {
			tokenURL = defaultERCOTTokenURL
		}
		clientID := c.clientID
		if clientID == "" {
			clientID = defaultERCOTClientID
		}

		data := url.Values{}
		data.Set("grant_type", "password")
		data.Set("username", c.username)
		data.Set("password", c.password)
		data.Set("client_id", clientID)
		data.Set("response_type", "id_token")
		data.Set("scope", fmt.Sprintf("openid %s offline_access", clientID))

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to request ercot token: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read ercot token response: %w", err)
		}

		var tokResp ercotTokenResponse
		if err := json.Unmarshal(body, &tokResp); err != nil {
			return "", fmt.Errorf("failed to parse ercot token response (status %d): %w", resp.StatusCode, err)
		}

		if resp.StatusCode != http.StatusOK || tokResp.Error != "" {
			return "", fmt.Errorf("ercot token request failed (status %d): %s - %s", resp.StatusCode, tokResp.Error, tokResp.ErrorDesc)
		}

		tok := tokResp.IDToken
		if tok == "" {
			tok = tokResp.AccessToken
		}
		if tok == "" {
			return "", fmt.Errorf("ercot token response did not contain id_token or access_token")
		}

		expiresIn := parseExpiresIn(tokResp.ExpiresIn, tokResp.IDTokenExpiresIn)

		// Buffer by up to 5 minutes so we refresh before hard expiration
		buffer := 5 * time.Minute
		if time.Duration(expiresIn)*time.Second <= buffer {
			buffer = time.Duration(expiresIn/2) * time.Second
		}
		expiry := c.now().Add(time.Duration(expiresIn)*time.Second - buffer)

		c.tokenMu.Lock()
		c.cachedToken = tok
		c.tokenExpiry = expiry
		c.tokenMu.Unlock()

		return tok, nil
	})
	if err != nil {
		return "", err
	}
	return res.(string), nil
}

func configuredERCOT(db storage.Database) *baseERCOT {
	c := &baseERCOT{
		client:           common.HTTPClient(10 * time.Second),
		cachedPrices:     make(map[string][]types.Price),
		cachedPricesTime: make(map[string]time.Time),
		cachedDAMPrices:  make(map[string][]types.Price),
		db:               db,
		tokenURL:         defaultERCOTTokenURL,
		clientID:         defaultERCOTClientID,
	}
	apiURL := lflag.String("ercot-api-url", "https://api.ercot.com/api/public-reports", "URL for the ERCOT Public Reports API")
	apiKey := lflag.String("ercot-api-key", "", "API Subscription Key for ERCOT Public Reports API (Ocp-Apim-Subscription-Key)")
	tokenURL := lflag.String("ercot-token-url", defaultERCOTTokenURL, "URL for the ERCOT B2C OAuth2 Token API")
	username := lflag.String("ercot-username", "", "Username/Email for ERCOT Developer Portal account")
	password := lflag.String("ercot-password", "", "Password for ERCOT Developer Portal account")
	token := lflag.String("ercot-token", "", "Optional static ID Token for ERCOT Public Reports API")

	lflag.Do(func() {
		c.apiURL = *apiURL
		c.apiKey = *apiKey
		if c.apiKey == "" {
			c.apiKey = os.Getenv("ERCOT_API_KEY")
		}
		c.tokenURL = *tokenURL
		c.username = *username
		if c.username == "" {
			c.username = os.Getenv("ERCOT_USERNAME")
		}
		c.password = *password
		if c.password == "" {
			c.password = os.Getenv("ERCOT_PASSWORD")
		}
		c.token = *token
		if c.token == "" {
			c.token = os.Getenv("ERCOT_TOKEN")
			if c.token == "" {
				c.token = os.Getenv("ERCOT_ID_TOKEN")
			}
		}
	})

	return c
}

// Zone returns a UtilityPrices provider for a specific ERCOT Load Zone.
func (c *baseERCOT) Zone(loadZone string) (*baseERCOTZone, error) {
	if loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	return &baseERCOTZone{
		base:     c,
		loadZone: loadZone,
	}, nil
}

// baseERCOTZone binds baseERCOT to a specific LoadZone and implements UtilityPrices.
type baseERCOTZone struct {
	base     *baseERCOT
	loadZone string
}

func (z *baseERCOTZone) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	if z.loadZone == "" {
		return types.Price{}, fmt.Errorf("ercot load zone is required")
	}
	return z.base.GetCurrentPrice(ctx, z.loadZone)
}

func (z *baseERCOTZone) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	if z.loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	return z.base.GetFuturePrices(ctx, z.loadZone)
}

func (z *baseERCOTZone) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	if z.loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	return z.base.GetConfirmedPrices(ctx, z.loadZone, start, end)
}

type ercotSPPEntry struct {
	DeliveryDate         string  `json:"deliveryDate"`
	DeliveryHour         int     `json:"deliveryHour"`
	DeliveryInterval     int     `json:"deliveryInterval"`
	SettlementPoint      string  `json:"settlementPoint"`
	SettlementPointPrice float64 `json:"settlementPointPrice"`
}

type ercotAPIResponse struct {
	Data []ercotSPPEntry `json:"data"`
}

type ercotColumnarResponse struct {
	Fields []struct {
		Name string `json:"name"`
	} `json:"fields"`
	Data [][]any `json:"data"`
}

func cleanFieldName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func getRowString(row []any, idx int) string {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return ""
	}
	switch v := row[idx].(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getRowInt(row []any, idx int) int {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return 0
	}
	switch v := row[idx].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		part, _, _ := strings.Cut(v, ":")
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			return n
		}
	}
	return 0
}

func getRowFloat(row []any, idx int) float64 {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return 0
	}
	switch v := row[idx].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return 0
}

func parseERCOTRTMResponse(bodyBytes []byte) ([]ercotSPPEntry, error) {
	var columnar ercotColumnarResponse
	if err := json.Unmarshal(bodyBytes, &columnar); err == nil && len(columnar.Fields) > 0 {
		fieldMap := make(map[string]int)
		for i, f := range columnar.Fields {
			fieldMap[cleanFieldName(f.Name)] = i
		}
		dateIdx, hasDate := fieldMap["deliverydate"]
		hourIdx, hasHour := fieldMap["deliveryhour"]
		intervalIdx, hasInterval := fieldMap["deliveryinterval"]
		pointIdx, hasPoint := fieldMap["settlementpoint"]
		priceIdx, hasPrice := fieldMap["settlementpointprice"]

		if hasPoint && hasPrice {
			entries := make([]ercotSPPEntry, 0, len(columnar.Data))
			for _, row := range columnar.Data {
				var entry ercotSPPEntry
				entry.SettlementPoint = getRowString(row, pointIdx)
				entry.SettlementPointPrice = getRowFloat(row, priceIdx)
				if hasDate {
					entry.DeliveryDate = getRowString(row, dateIdx)
				}
				if hasHour {
					entry.DeliveryHour = getRowInt(row, hourIdx)
				}
				if hasInterval {
					entry.DeliveryInterval = getRowInt(row, intervalIdx)
				}
				entries = append(entries, entry)
			}
			return entries, nil
		}
	}

	var objResp ercotAPIResponse
	if err := json.Unmarshal(bodyBytes, &objResp); err == nil && len(objResp.Data) > 0 {
		return objResp.Data, nil
	}

	var directArray []ercotSPPEntry
	if err := json.Unmarshal(bodyBytes, &directArray); err == nil && len(directArray) > 0 {
		return directArray, nil
	}

	return nil, fmt.Errorf("failed to parse ercot api response: unrecognized json structure")
}

func (c *baseERCOT) getPricesForDate(ctx context.Context, date time.Time, loadZone string) ([]types.Price, error) {
	if loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	dateCT := date.In(ctLocation)
	dateStr := dateCT.Format("20060102")
	key := fmt.Sprintf("%s_%s", loadZone, dateStr)

	todayCT := truncateDay(c.now().In(ctLocation))
	isToday := truncateDay(dateCT).Equal(todayCT)

	c.mu.Lock()
	if prices, ok := c.cachedPrices[key]; ok {
		cachedAt := time.Time{}
		if c.cachedPricesTime != nil {
			cachedAt = c.cachedPricesTime[key]
		}
		// Past days are immutable once cached; today's cache is valid for 5 minutes (or if pre-seeded)
		if !isToday || cachedAt.IsZero() || c.now().Sub(cachedAt) < 5*time.Minute {
			c.mu.Unlock()
			return copyPrices(prices), nil
		}
	}
	c.mu.Unlock()

	// Singleflight per (loadZone, date) so callers get their own zone's prices
	sfKey := fmt.Sprintf("rtm_%s_%s", loadZone, dateStr)
	res, err, _ := c.sfGroup.Do(sfKey, func() (any, error) {
		c.mu.Lock()
		if prices, ok := c.cachedPrices[key]; ok {
			cachedAt := time.Time{}
			if c.cachedPricesTime != nil {
				cachedAt = c.cachedPricesTime[key]
			}
			if !isToday || cachedAt.IsZero() || c.now().Sub(cachedAt) < 5*time.Minute {
				c.mu.Unlock()
				return prices, nil
			}
		}
		c.mu.Unlock()

		// 1. Check Firestore database cache first (unified ercot_<loadZone> ID)
		if c.db != nil {
			start := truncateDay(dateCT)
			end := start.AddDate(0, 0, 1)
			expectedHours := hoursInDay(start)
			dbUtilityID := "ercot_" + strings.ToLower(loadZone)
			dbPrices, err := c.db.GetUtilityPrices(ctx, dbUtilityID, start, end)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to get ercot rtm prices from database",
					slog.String("utilityID", dbUtilityID),
					slog.Any("error", err),
				)
			} else if len(dbPrices) >= expectedHours {
				allConfirmed := true
				for _, p := range dbPrices {
					if !p.Confirmed {
						allConfirmed = false
						break
					}
				}
				if allConfirmed {
					var prices []types.Price
					for _, p := range dbPrices {
						prices = append(prices, p.Price)
					}
					c.mu.Lock()
					c.cachedPrices[key] = prices
					if c.cachedPricesTime != nil {
						c.cachedPricesTime[key] = c.now()
					}
					c.mu.Unlock()
					return prices, nil
				}
			}
		}

		// 2. Fetch all load zones in a single request from ERCOT RTM API using inner singleflight
		fetchKey := "fetch_rtm_" + dateStr
		fetchRes, err, _ := c.sfGroup.Do(fetchKey, func() (any, error) {
			return c.fetchERCOTRTM(ctx, dateCT)
		})
		if err != nil {
			return nil, err
		}
		byZone := fetchRes.(map[string][]types.Price)

		nowTime := c.now()
		c.mu.Lock()
		if c.cachedPricesTime == nil {
			c.cachedPricesTime = make(map[string]time.Time)
		}
		for z, prices := range byZone {
			c.cachedPrices[z+"_"+dateStr] = prices
			c.cachedPricesTime[z+"_"+dateStr] = nowTime
		}
		c.mu.Unlock()

		// 3. Persist confirmed RTM prices to Firestore database cache under unified ercot_<loadZone> ID
		if c.db != nil {
			for z, prices := range byZone {
				var toUpsert []types.PriceState
				for _, p := range prices {
					// Only completed intervals that have ended are confirmed settled prices
					confirmed := !nowTime.Before(p.TSEnd)
					toUpsert = append(toUpsert, types.PriceState{
						Price:     p,
						Confirmed: confirmed,
						TSUpdated: nowTime,
					})
				}
				if len(toUpsert) > 0 {
					dbUtilityID := "ercot_" + strings.ToLower(z)
					if err := c.db.UpsertUtilityPrices(ctx, dbUtilityID, toUpsert, 0); err != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to upsert ercot rtm prices to database",
							slog.String("zone", z),
							slog.Any("error", err),
						)
					}
				}
			}
		}

		prices, ok := byZone[loadZone]
		if !ok {
			return nil, fmt.Errorf("no ercot rtm data available for %s in zone %s", dateStr, loadZone)
		}
		return prices, nil
	})
	if err != nil {
		return nil, err
	}
	prices := res.([]types.Price)
	return copyPrices(prices), nil
}

// fetchERCOTRTM fetches 15-minute Real-Time Market (RTM) Settlement Point Prices from ERCOT for all load zones in a single request.
//
// What is "np6-905-cd" and why it was chosen:
// In ERCOT's Market Information System (MIS) and Developer API, report feeds are categorized by protocol section:
//   - "NP": Nodal Protocol.
//   - "6": Section 6 of ERCOT Nodal Protocols, governing the Real-Time Market (RTM).
//   - "905": Specific report ID for "Settlement Point Prices at Resource Nodes, Hubs and Load Zones".
//   - "-CD": Certified/Disclosed public data product available through ERCOT's public API.
//
// NP6-905-CD was chosen because it is the authoritative public feed of 15-minute settlement prices
// for electrical load zones (LZ_HOUSTON, LZ_NORTH, LZ_SOUTH, LZ_WEST). Retail electric providers with
// wholesale-indexed buyback plans (such as Tesla Electric, Almika Solar, etc.) settle residential export
// credits against these exact 15-minute settlement point prices for the customer's load zone.
func (c *baseERCOT) fetchERCOTRTM(ctx context.Context, date time.Time) (map[string][]types.Price, error) {
	dateCT := date.In(ctLocation)
	dateFormatted := dateCT.Format("2006-01-02")
	endpoint := fmt.Sprintf("%s/np6-905-cd/spp_node_zone_hub?deliveryDateFrom=%s&deliveryDateTo=%s&settlementPointType=LZ",
		c.apiURL,
		url.QueryEscape(dateFormatted),
		url.QueryEscape(dateFormatted),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)
	}
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if strings.Contains(bodyStr, "Access token") || strings.Contains(bodyStr, "token") {
			c.tokenMu.Lock()
			c.cachedToken = ""
			c.tokenExpiry = time.Time{}
			c.tokenMu.Unlock()
		}
		return nil, fmt.Errorf("ercot api unauthorized (check Ocp-Apim-Subscription-Key and ercot credentials/token): %s", bodyStr)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ercot api error status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	parsedData, err := parseERCOTRTMResponse(bodyBytes)
	if err != nil {
		return nil, err
	}

	// Group 15-minute intervals into hourly averages per settlement point
	type hourlyAccumulator struct {
		sum   float64
		count int
	}
	hourlyByZone := make(map[string]map[int]*hourlyAccumulator)

	for _, entry := range parsedData {
		zone := entry.SettlementPoint
		if zone == "" {
			continue
		}
		// ERCOT deliveryHour is 1-based (Hour Ending 1..25)
		h := entry.DeliveryHour - 1
		if h < 0 || h > 24 {
			continue
		}
		if _, ok := hourlyByZone[zone]; !ok {
			hourlyByZone[zone] = make(map[int]*hourlyAccumulator)
		}
		if _, ok := hourlyByZone[zone][h]; !ok {
			hourlyByZone[zone][h] = &hourlyAccumulator{}
		}
		hourlyByZone[zone][h].sum += entry.SettlementPointPrice
		hourlyByZone[zone][h].count++
	}

	if len(hourlyByZone) == 0 {
		return nil, fmt.Errorf("no ercot rtm data available for %s", dateFormatted)
	}

	byZone := make(map[string][]types.Price)
	dayStart := truncateDay(dateCT)
	numHours := hoursInDay(dayStart)

	for zone, hourlyMap := range hourlyByZone {
		var lastWholesaleDollarsPerKWH float64
		for h := 0; h < numHours; h++ {
			if acc, ok := hourlyMap[h]; ok && acc.count > 0 {
				lastWholesaleDollarsPerKWH = (acc.sum / float64(acc.count)) / 1000.0
				break
			}
		}

		var prices []types.Price
		for h := 0; h < numHours; h++ {
			start := dayStart.Add(time.Duration(h) * time.Hour)
			end := start.Add(time.Hour)

			if acc, ok := hourlyMap[h]; ok && acc.count > 0 {
				lastWholesaleDollarsPerKWH = (acc.sum / float64(acc.count)) / 1000.0
			}

			prices = append(prices, types.Price{
				Provider:                      "ercot",
				TSStart:                       start,
				TSEnd:                         end,
				DollarsPerKWH:                 lastWholesaleDollarsPerKWH,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: lastWholesaleDollarsPerKWH,
			})
		}
		byZone[zone] = prices
	}

	return byZone, nil
}

// ercotDAMEntry represents a single Day-Ahead Market Settlement Point Price entry from ERCOT.
type ercotDAMEntry struct {
	DeliveryDate         string  `json:"deliveryDate"`
	HourEnding           any     `json:"hourEnding"`
	DeliveryHour         int     `json:"deliveryHour"`
	SettlementPoint      string  `json:"settlementPoint"`
	SettlementPointPrice float64 `json:"settlementPointPrice"`
}

type ercotDAMAPIResponse struct {
	Data []ercotDAMEntry `json:"data"`
}

func parseERCOTDAMResponse(bodyBytes []byte) ([]ercotDAMEntry, error) {
	var columnar ercotColumnarResponse
	if err := json.Unmarshal(bodyBytes, &columnar); err == nil && len(columnar.Fields) > 0 {
		fieldMap := make(map[string]int)
		for i, f := range columnar.Fields {
			fieldMap[cleanFieldName(f.Name)] = i
		}
		dateIdx, hasDate := fieldMap["deliverydate"]
		hourIdx, hasHour := fieldMap["deliveryhour"]
		heIdx, hasHE := fieldMap["hourending"]
		pointIdx, hasPoint := fieldMap["settlementpoint"]
		priceIdx, hasPrice := fieldMap["settlementpointprice"]

		if hasPoint && hasPrice {
			entries := make([]ercotDAMEntry, 0, len(columnar.Data))
			for _, row := range columnar.Data {
				var entry ercotDAMEntry
				entry.SettlementPoint = getRowString(row, pointIdx)
				entry.SettlementPointPrice = getRowFloat(row, priceIdx)
				if hasDate {
					entry.DeliveryDate = getRowString(row, dateIdx)
				}
				if hasHour {
					entry.DeliveryHour = getRowInt(row, hourIdx)
				}
				if hasHE {
					entry.HourEnding = row[heIdx]
				}
				entries = append(entries, entry)
			}
			return entries, nil
		}
	}

	var objResp ercotDAMAPIResponse
	if err := json.Unmarshal(bodyBytes, &objResp); err == nil && len(objResp.Data) > 0 {
		return objResp.Data, nil
	}

	var directArray []ercotDAMEntry
	if err := json.Unmarshal(bodyBytes, &directArray); err == nil && len(directArray) > 0 {
		return directArray, nil
	}

	return nil, fmt.Errorf("failed to parse ercot dam api response: unrecognized json structure")
}

func parseDAMHour(raw any, deliveryHour int) (int, bool) {
	if deliveryHour >= 1 && deliveryHour <= 25 {
		return deliveryHour - 1, true
	}
	switch v := raw.(type) {
	case float64:
		he := int(v)
		if he >= 1 && he <= 25 {
			return he - 1, true
		}
	case int:
		if v >= 1 && v <= 25 {
			return v - 1, true
		}
	case string:
		part, _, _ := strings.Cut(v, ":")
		if he, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			if he >= 1 && he <= 25 {
				return he - 1, true
			}
		}
	}
	return 0, false
}

// getDAMPricesForDate returns 24 hourly Day-Ahead Market prices for a given date in Central Time,
// caching results in memory and in Google Cloud Firestore under unified ercot_<loadZone> ID.
func (c *baseERCOT) getDAMPricesForDate(ctx context.Context, date time.Time, loadZone string) ([]types.Price, error) {
	if loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	dateCT := date.In(ctLocation)
	dateStr := dateCT.Format("20060102")
	key := fmt.Sprintf("%s_%s", loadZone, dateStr)

	c.mu.Lock()
	if prices, ok := c.cachedDAMPrices[key]; ok {
		c.mu.Unlock()
		return copyPrices(prices), nil
	}
	c.mu.Unlock()

	// Singleflight per (loadZone, date) so callers get their own zone's prices
	sfKey := fmt.Sprintf("dam_%s_%s", loadZone, dateStr)
	res, err, _ := c.sfGroup.Do(sfKey, func() (any, error) {
		c.mu.Lock()
		if prices, ok := c.cachedDAMPrices[key]; ok {
			c.mu.Unlock()
			return prices, nil
		}
		c.mu.Unlock()

		// 1. Check Firestore database cache first (unified ercot_<loadZone> ID)
		if c.db != nil {
			start := truncateDay(dateCT)
			end := start.AddDate(0, 0, 1)
			expectedHours := hoursInDay(start)
			dbUtilityID := "ercot_" + strings.ToLower(loadZone)
			dbPrices, err := c.db.GetUtilityPrices(ctx, dbUtilityID, start, end)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to get ercot dam prices from database",
					slog.String("utilityID", dbUtilityID),
					slog.Any("error", err),
				)
			} else if len(dbPrices) >= expectedHours {
				var prices []types.Price
				for _, p := range dbPrices {
					prices = append(prices, p.Price)
				}
				c.mu.Lock()
				c.cachedDAMPrices[key] = prices
				c.mu.Unlock()
				return prices, nil
			}
		}

		// 2. Fetch all load zones in a single request from ERCOT DAM API using inner singleflight
		fetchKey := "fetch_dam_" + dateStr
		fetchRes, err, _ := c.sfGroup.Do(fetchKey, func() (any, error) {
			return c.fetchERCOTDAM(ctx, dateCT)
		})
		if err != nil {
			return nil, err
		}
		byZone := fetchRes.(map[string][]types.Price)

		c.mu.Lock()
		for z, prices := range byZone {
			c.cachedDAMPrices[z+"_"+dateStr] = prices
		}
		c.mu.Unlock()

		// 3. Persist unconfirmed DAM prices for each load zone to Firestore DB (unified ercot_<loadZone> ID)
		if c.db != nil {
			nowTime := c.now()
			expectedHours := hoursInDay(truncateDay(dateCT))
			for z, prices := range byZone {
				if len(prices) >= expectedHours {
					var toUpsert []types.PriceState
					for _, p := range prices {
						toUpsert = append(toUpsert, types.PriceState{
							Price:     p,
							Confirmed: false, // DAM prices are unconfirmed forward forecasts
							TSUpdated: nowTime,
						})
					}
					dbUtilityID := "ercot_" + strings.ToLower(z)
					if err := c.db.UpsertUtilityPrices(ctx, dbUtilityID, toUpsert, 0); err != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to upsert ercot dam prices to database",
							slog.String("zone", z),
							slog.Any("error", err),
						)
					}
				}
			}
		}

		prices, ok := byZone[loadZone]
		if !ok {
			return nil, fmt.Errorf("no ercot dam data available for %s in zone %s", dateStr, loadZone)
		}
		return prices, nil
	})
	if err != nil {
		return nil, err
	}
	prices := res.([]types.Price)
	return copyPrices(prices), nil
}

// fetchERCOTDAM fetches hourly Day-Ahead Market (DAM) Settlement Point Prices from ERCOT for all load zones in a single request.
//
// What is "np4-190-cd" and why it was chosen:
// In ERCOT's Market Information System (MIS) and Public Reports API:
//   - "NP": Nodal Protocol.
//   - "4": Section 4 of ERCOT Nodal Protocols, governing the Day-Ahead Market (DAM).
//   - "190": Specific report ID for "DAM Settlement Point Prices" (EMIL ID: NP4-190-CD, Report Type ID: 12331).
//   - "-CD": Certified/Disclosed public data product available through ERCOT's public API.
//
// NP4-190-CD was chosen because the Day-Ahead Market clears financially binding hourly energy prices
// for all Settlement Points (including electrical Load Zones LZ_HOUSTON, LZ_NORTH, LZ_SOUTH, LZ_WEST)
// 24 to 36 hours in advance.
//
// ERCOT DAM Market Timeline:
//   - 10:00 AM Central Time: Bids and offers close for the Day-Ahead Market.
//   - 13:30 (1:30 PM) Central Time: ERCOT completes DAM clearing and publishes Day-Ahead Settlement Point Prices
//     for all 24 hours (Hour Ending 1..24) of the next operating day.
//
// Real-Time Market (RTM, NP6-905-CD) publishes 15-minute intervals ex-post (after physical delivery), so RTM
// cannot provide forward prices for battery arbitrage scheduling. Therefore, GetFuturePrices queries DAM (NP4-190-CD).
// Prior to 13:30 CT, DAM provides forward visibility for the remainder of the current operating day;
// after 13:30 CT, DAM additionally provides all 24 hours of tomorrow, enabling optimal overnight battery scheduling.
func (c *baseERCOT) fetchERCOTDAM(ctx context.Context, date time.Time) (map[string][]types.Price, error) {
	dateCT := date.In(ctLocation)
	dateFormatted := dateCT.Format("2006-01-02")
	endpoint := fmt.Sprintf("%s/np4-190-cd/dam_stlmnt_pnt_prices?deliveryDateFrom=%s&deliveryDateTo=%s&size=50000",
		c.apiURL,
		url.QueryEscape(dateFormatted),
		url.QueryEscape(dateFormatted),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)
	}
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("dam prices not yet published for %s", dateFormatted)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if strings.Contains(bodyStr, "Access token") || strings.Contains(bodyStr, "token") {
			c.tokenMu.Lock()
			c.cachedToken = ""
			c.tokenExpiry = time.Time{}
			c.tokenMu.Unlock()
		}
		return nil, fmt.Errorf("ercot dam api unauthorized (check Ocp-Apim-Subscription-Key and ercot credentials/token): %s", bodyStr)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ercot dam api error status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	parsedData, err := parseERCOTDAMResponse(bodyBytes)
	if err != nil {
		return nil, err
	}

	hourlyByZone := make(map[string]map[int]float64)
	for _, entry := range parsedData {
		zone := entry.SettlementPoint
		if zone == "" {
			continue
		}
		h, ok := parseDAMHour(entry.HourEnding, entry.DeliveryHour)
		if !ok || h < 0 || h > 24 {
			continue
		}
		if _, ok := hourlyByZone[zone]; !ok {
			hourlyByZone[zone] = make(map[int]float64)
		}
		hourlyByZone[zone][h] = entry.SettlementPointPrice
	}

	if len(hourlyByZone) == 0 {
		return nil, fmt.Errorf("no ercot dam data available for %s", dateFormatted)
	}

	byZone := make(map[string][]types.Price)
	dayStart := truncateDay(dateCT)
	numHours := hoursInDay(dayStart)

	for zone, hourlyMap := range hourlyByZone {
		var lastWholesaleDollarsPerKWH float64
		for h := 0; h < numHours; h++ {
			if val, ok := hourlyMap[h]; ok {
				lastWholesaleDollarsPerKWH = val / 1000.0
				break
			}
		}

		var prices []types.Price
		for h := 0; h < numHours; h++ {
			start := dayStart.Add(time.Duration(h) * time.Hour)
			end := start.Add(time.Hour)

			if val, ok := hourlyMap[h]; ok {
				lastWholesaleDollarsPerKWH = val / 1000.0
			}

			prices = append(prices, types.Price{
				Provider:                      "ercot",
				TSStart:                       start,
				TSEnd:                         end,
				DollarsPerKWH:                 lastWholesaleDollarsPerKWH,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: lastWholesaleDollarsPerKWH,
			})
		}
		byZone[zone] = prices
	}

	return byZone, nil
}

// GetCurrentPrice returns the current hourly wholesale price. It first attempts to retrieve
// the Real-Time Market (RTM, NP6-905-CD) price for the current hour. If RTM data is not yet available for the
// current interval (e.g. early in the hour or during ERCOT publication latency), it seamlessly falls back to
// the Day-Ahead Market (DAM, NP4-190-CD) price for the current hour.
func (c *baseERCOT) GetCurrentPrice(ctx context.Context, loadZone string) (types.Price, error) {
	if loadZone == "" {
		return types.Price{}, fmt.Errorf("ercot load zone is required")
	}
	now := c.now().In(ctLocation)

	// 1. Try Real-Time Market (RTM) first
	prices, err := c.getPricesForDate(ctx, now, loadZone)
	if err == nil {
		for _, p := range prices {
			if p.Contains(now) {
				return p, nil
			}
		}
	} else {
		log.Ctx(ctx).WarnContext(ctx, "failed to get ercot rtm price for current date, falling back to dam",
			slog.String("loadZone", loadZone),
			slog.Any("error", err),
		)
	}

	// 2. Fall back to Day-Ahead Market (DAM)
	damPrices, damErr := c.getDAMPricesForDate(ctx, now, loadZone)
	if damErr == nil {
		for _, p := range damPrices {
			if p.Contains(now) {
				return p, nil
			}
		}
	} else {
		log.Ctx(ctx).WarnContext(ctx, "failed to get ercot dam price for current date",
			slog.String("loadZone", loadZone),
			slog.Any("error", damErr),
		)
	}

	if err != nil {
		return types.Price{}, fmt.Errorf("failed to get current ercot price for %s: %w", loadZone, err)
	}
	return types.Price{}, fmt.Errorf("no current ercot price found for zone %s", loadZone)
}

// GetFuturePrices returns the Day-Ahead Market (DAM) wholesale prices for the remainder of today and tomorrow.
//
// ERCOT DAM Market Timeline & Tomorrow's Availability:
// ERCOT completes the Day-Ahead Market clearing and publishes next-day Settlement Point Prices by
// 13:30 (1:30 PM) Central Time daily (NP4-190-CD).
//
// Prior to 13:30 CT: Tomorrow's DAM prices are not yet published. The system maintains at least 11 hours
// of forward prices for the remainder of today. If len(futurePrices) >= 11, lookups for tomorrow are skipped
// to prevent unnecessary/failed queries to ERCOT before data is published.
//
// After 13:30 CT: When len(futurePrices) < 11 (or tomorrow is not yet cached), tomorrow's 24 hours of DAM prices
// are fetched and cached, expanding forward visibility to 24–36+ hours. A 15-minute cooldown prevents rapid retries
// if ERCOT's 1:30 PM publication is slightly delayed.
func (c *baseERCOT) GetFuturePrices(ctx context.Context, loadZone string) ([]types.Price, error) {
	if loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	nowCT := c.now().In(ctLocation)
	today := truncateDay(nowCT)
	tomorrow := today.AddDate(0, 0, 1)

	// Check in-memory cache
	checkCache := func() ([]types.Price, time.Time) {
		c.mu.Lock()
		defer c.mu.Unlock()
		var future []types.Price
		todayKey := loadZone + "_" + today.Format("20060102")
		tomorrowKey := loadZone + "_" + tomorrow.Format("20060102")

		if todayPrices, ok := c.cachedDAMPrices[todayKey]; ok {
			for _, p := range todayPrices {
				if p.TSStart.After(nowCT) {
					future = append(future, p)
				}
			}
		}
		expectedTomorrowHours := hoursInDay(tomorrow)
		if tomorrowPrices, ok := c.cachedDAMPrices[tomorrowKey]; ok && len(tomorrowPrices) >= expectedTomorrowHours {
			future = append(future, tomorrowPrices...)
		}
		return future, c.lastFutureFetch
	}

	futurePrices, lastFetch := checkCache()

	// If we already have >= 11 hours of future prices, we have sufficient forward data
	// (either earlier in the day, or tomorrow's 24 hours are already loaded).
	if len(futurePrices) >= 11 {
		return futurePrices, nil
	}

	if !lastFetch.IsZero() && c.now().Sub(lastFetch) < 15*time.Minute && len(futurePrices) > 0 {
		return futurePrices, nil
	}

	// 1. Fetch today's DAM prices if not yet cached
	if len(futurePrices) == 0 {
		todayPrices, err := c.getDAMPricesForDate(ctx, today, loadZone)
		if err == nil {
			for _, p := range todayPrices {
				if p.TSStart.After(nowCT) {
					futurePrices = append(futurePrices, p)
				}
			}
		} else {
			log.Ctx(ctx).WarnContext(ctx, "failed to get ercot dam prices for today",
				slog.String("loadZone", loadZone),
				slog.String("date", today.Format("2006-01-02")),
				slog.Any("error", err),
			)
		}
	}

	// 2. ERCOT publishes tomorrow's DAM prices by 13:30 Central Time daily.
	// Only fetch tomorrow if at or past 13:30 CT (when len(futurePrices) < 11).
	isAfterDAMPost := nowCT.Hour() > 13 || (nowCT.Hour() == 13 && nowCT.Minute() >= 30)
	if isAfterDAMPost && (lastFetch.IsZero() || c.now().Sub(lastFetch) >= 15*time.Minute) {
		tomorrowPrices, err := c.getDAMPricesForDate(ctx, tomorrow, loadZone)
		c.mu.Lock()
		c.lastFutureFetch = c.now()
		c.mu.Unlock()
		if err == nil {
			futurePrices = append(futurePrices, tomorrowPrices...)
		} else {
			log.Ctx(ctx).WarnContext(ctx, "failed to get ercot dam prices for tomorrow",
				slog.String("loadZone", loadZone),
				slog.String("date", tomorrow.Format("2006-01-02")),
				slog.Any("error", err),
			)
		}
	}

	if len(futurePrices) == 0 {
		return nil, fmt.Errorf("no future ercot dam prices found for zone %s", loadZone)
	}

	return futurePrices, nil
}

// GetConfirmedPrices returns confirmed wholesale prices for the given date range.
// It retrieves Real-Time Market (RTM) prices, which are the only confirmed prices.
func (c *baseERCOT) GetConfirmedPrices(ctx context.Context, loadZone string, start, end time.Time) ([]types.Price, error) {
	if loadZone == "" {
		return nil, fmt.Errorf("ercot load zone is required")
	}
	var confirmed []types.Price
	curr := start.In(ctLocation).Truncate(time.Hour)
	endCT := end.In(ctLocation)

	for curr.Before(endCT) {
		dayDate := truncateDay(curr)
		prices, err := c.getPricesForDate(ctx, dayDate, loadZone)
		if err != nil {
			return nil, err
		}
		for _, p := range prices {
			if !p.TSStart.Before(curr) && p.TSStart.Before(endCT) {
				confirmed = append(confirmed, p)
			}
		}
		curr = dayDate.AddDate(0, 0, 1)
	}

	return confirmed, nil
}
