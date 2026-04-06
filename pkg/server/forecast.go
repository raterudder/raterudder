package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// EnergyHistoryRes represents a simplified historical energy stat returned in the forecast.
type EnergyHistoryRes struct {
	TSHourStart   time.Time `json:"tsHourStart"`
	AvgBatterySOC float64   `json:"avgBatterySOC"`
	SolarKWH      float64   `json:"solarKWH"`
	HomeLoadKWH   float64   `json:"homeLoadKWH"`
}

// PriceHistoryRes represents historical pricing returned in the forecast.
type PriceHistoryRes struct {
	TSHourStart          time.Time `json:"tsHourStart"`
	DollarsPerKWH        float64   `json:"dollarsPerKWH"`
	GridUseDollarsPerKWH float64   `json:"gridUseDollarsPerKWH"`
}

// WeatherRes represents a simplified historical weather and forecast stat.
type WeatherRes struct {
	TSHourStart              time.Time `json:"tsHourStart"`
	Irradiance               float64   `json:"irradiance"`
	TemperatureC             float64   `json:"temperatureC,omitempty"`
	TemperatureCellC         float64   `json:"temperatureCellC,omitempty"`
	SnowfallCM               float64   `json:"snowfallCM,omitempty"`
	EstimatedSolarKWH        float64   `json:"estimatedSolarKWH,omitempty"`
	ImprovedSolarGeneration  float64   `json:"improvedSolarGeneration,omitempty"`
	UnclippedSolarGeneration float64   `json:"unclippedSolarGeneration,omitempty"`
	SnowDepthCM              float64   `json:"snowDepthCM,omitempty"`
	TempFactor               float64   `json:"tempFactor,omitempty"`
	SnowFactor               float64   `json:"snowFactor,omitempty"`
}

// ForecastRes represents the complete response for the forecast endpoint, including histories.
type ForecastRes struct {
	Simulation    []controller.SimHour `json:"simulation"`
	EnergyHistory []EnergyHistoryRes   `json:"energyHistory"`
	PriceHistory  []PriceHistoryRes    `json:"priceHistory"`
	Weather       []WeatherRes         `json:"weather"`
}

func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// 1. Get Settings
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	if settings.ESS == "" {
		writeJSONError(w, "no ESS configured", http.StatusBadRequest)
		return
	}

	essSystem, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
		writeJSONError(w, "failed to get ess system", http.StatusInternalServerError)
		return
	}

	// 2. Fetch current ESS status
	// TODO: skip fetching this and use the latest action instead
	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess status", slog.Any("error", err))
		writeJSONError(w, "failed to get ess status", http.StatusInternalServerError)
		return
	}

	// get utility
	utility, err := s.utilities.Site(ctx, siteID, settings.Settings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility system", slog.String("utility", settings.UtilityProvider))
		writeJSONError(w, "failed to get utility system", http.StatusInternalServerError)
		return
	}

	// 3. Get Current Price
	currentPrice, err := utility.GetCurrentPrice(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get price", slog.Any("error", err))
		writeJSONError(w, "failed to get current price", http.StatusInternalServerError)
		return
	}

	// 4. Get Future Prices
	futurePrices, err := utility.GetFuturePrices(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
		// Continue with empty future prices
	}

	// 5. Get History (Last 3 days from Storage) - no backfill
	now := status.Timestamp.Truncate(time.Hour)
	historyStart := now.AddDate(0, 0, -3).Truncate(time.Hour)
	energyHistoryDaily, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, now)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	var energyHistory []types.EnergyStats
	for _, day := range energyHistoryDaily {
		energyHistory = append(energyHistory, day.Hourly...)
	}

	// 6. Run Simulation
	simHours := s.controller.SimulateState(ctx, now, status, currentPrice, futurePrices, energyHistory, settings.Settings)

	// Fetch data for the previous day
	histStart24 := now.AddDate(0, 0, -1).Truncate(time.Hour)

	// Reuse energyHistory already fetched from db
	// Preallocate with known capacity to minimize memory reallocations
	energyHistory24 := make([]types.EnergyStats, 0, 24)
	for _, h := range energyHistory {
		if !h.TSHourStart.Before(histStart24.Truncate(time.Hour)) && h.TSHourStart.Before(now.Truncate(time.Hour)) {
			energyHistory24 = append(energyHistory24, h)
		}
	}

	// 7. Get Price History
	priceHistory24, err := s.storage.GetPriceHistory(ctx, siteID, histStart24, now)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to fetch price history for forecast", slog.Any("error", err))
	}

	// if we don't have the current hour in the price history, add it
	var foundCurrentPrice bool
	for _, p := range priceHistory24 {
		if p.Contains(now) {
			foundCurrentPrice = true
			break
		}
	}
	if !foundCurrentPrice && currentPrice.Contains(now) {
		priceHistory24 = append(priceHistory24, currentPrice)
	}

	var weatherHistory []types.Weather

	// 8. Get Weather History if we have a location set
	if settings.Location != nil {
		if timeLoc, err := time.LoadLocation(settings.Location.TimeZone); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to load location", slog.Any("error", err), slog.String("timeZone", settings.Location.TimeZone))
		} else {
			start := time.Date(historyStart.Year(), historyStart.Month(), historyStart.Day(), 0, 0, 0, 0, timeLoc)
			// we will simulate at most 24 hours so we only need weather for the next day
			end := now.In(timeLoc).AddDate(0, 0, 1)
			weatherHistory, err = s.storage.GetWeather(ctx, siteID, start, end)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to fetch weather for forecast", slog.Any("error", err))
			}
		}
	}

	energyRes := make([]EnergyHistoryRes, 0, len(energyHistory24))
	for _, h := range energyHistory24 {
		avgSoc := (h.MinBatterySOC + h.MaxBatterySOC) / 2
		energyRes = append(energyRes, EnergyHistoryRes{
			TSHourStart:   h.TSHourStart,
			AvgBatterySOC: avgSoc,
			SolarKWH:      h.SolarKWH,
			HomeLoadKWH:   h.HomeKWH,
		})
	}

	priceRes := make([]PriceHistoryRes, 0, len(priceHistory24))
	for _, p := range priceHistory24 {
		priceRes = append(priceRes, PriceHistoryRes{
			TSHourStart:          p.TSStart,
			DollarsPerKWH:        p.DollarsPerKWH,
			GridUseDollarsPerKWH: p.GridUseDollarsPerKWH,
		})
	}

	// Find improved solar for calculations
	var improvedSolarMap map[int64]improvedSolar
	if settings.Location != nil {
		improvedSolarMap = calculateImprovedSolar(ctx, energyHistory, weatherHistory, *settings.Location)
	}

	weatherRes := make([]WeatherRes, 0, len(weatherHistory))
	for _, w := range weatherHistory {
		for _, h := range w.ForecastHours {
			wr := WeatherRes{
				TSHourStart:  h.TSHourStart,
				TemperatureC: h.TemperatureC,
				SnowfallCM:   h.SnowfallCM,
			}

			if improved, ok := improvedSolarMap[h.TSHourStart.Unix()]; ok {
				wr.ImprovedSolarGeneration = improved.ImprovedSolar
				wr.UnclippedSolarGeneration = improved.UnclippedSolar
				wr.SnowDepthCM = improved.SnowDepth
				wr.TempFactor = improved.TempFactor
				wr.SnowFactor = improved.SnowFactor
				wr.TemperatureCellC = improved.TCell
				wr.Irradiance = improved.Irradiance
			}
			weatherRes = append(weatherRes, wr)
		}
	}

	res := ForecastRes{
		Simulation:    simHours,
		EnergyHistory: energyRes,
		PriceHistory:  priceRes,
		Weather:       weatherRes,
	}

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(http.ErrAbortHandler)
	}
}

type improvedSolar struct {
	TSHourStart    int64
	ImprovedSolar  float64
	UnclippedSolar float64
	SnowDepth      float64
	TempFactor     float64
	SnowFactor     float64
	TCell          float64
	Irradiance     float64
}

// calculateImprovedSolar estimates solar generation for each weather hour by:
//  1. Calibrating a robust efficiency factor from filtered historical actual solar vs. irradiance data.
//  2. Tracking snow accumulation and melt sequentially to derive a snow attenuation factor.
//  3. Applying NOCT-based cell temperature estimation to correct for temperature-dependent efficiency.
//  4. Projecting forward (and backward for history comparison) using the calibrated efficiency.
//
// Returns a map keyed by Unix timestamp (seconds) of each weather hour's computed improvedSolar.
func calculateImprovedSolar(ctx context.Context, history []types.EnergyStats, weather []types.Weather, locInfo types.SiteLocation) map[int64]improvedSolar {
	const (
		noct        = 45.0   // Nominal Operating Cell Temperature
		tempCoeff   = 0.0035 // typical power temperature coefficient
		clippingEps = 0.05   // kWh epsilon for detecting a plateau
	)

	// 1. Index historical actual solar by hour timestamp for O(1) lookup.
	statsByHour := make(map[int64]types.EnergyStats, len(history))
	maxSolarKWH := 0.0
	for _, h := range history {
		statsByHour[h.TSHourStart.Unix()] = h
		if h.SolarKWH > maxSolarKWH {
			maxSolarKWH = h.SolarKWH
		}
	}

	// Learning the Clipping Cap:
	// Identify days where production plateaus at the peak.
	var clippingCap float64
	if maxSolarKWH > 1.0 { // only consider clipping if production is significant
		usageCounts := make(map[int]int)
		for _, s := range history {
			if s.SolarKWH > maxSolarKWH*0.9 {
				// Round to 1 decimal place to group similar peak values
				val := int(math.Round(s.SolarKWH * 10))
				usageCounts[val]++
			}
		}

		// If any value in the top 30% of max occurs frequently, it might be a cap.
		mostFreqVal := -1
		mostFreqCount := 0
		for val, count := range usageCounts {
			if count > mostFreqCount {
				mostFreqVal = val
				mostFreqCount = count
			}
		}

		// only if we've seen this for at least 6 hours
		if mostFreqVal > 0 && mostFreqCount >= 6 {
			clippingCap = float64(mostFreqVal) / 10.0
			log.Ctx(ctx).DebugContext(
				ctx,
				"learned inverter clipping cap",
				slog.Float64("capKWH", clippingCap),
				slog.Int("occurrences", mostFreqCount),
			)
		} else {
			log.Ctx(ctx).DebugContext(
				ctx,
				"found no inverter clipping cap",
				slog.Float64("maxSolarKWH", maxSolarKWH),
				slog.Float64("frequentKWH", float64(mostFreqVal)/10.0),
				slog.Int("occurrences", mostFreqCount),
			)
		}
	}

	// Index weather by timestamp; later hours overwrite earlier for the same slot (dedup).
	weatherByHour := make(map[int64]types.HourlyWeather)
	for _, w := range weather {
		for _, hw := range w.ForecastHours {
			weatherByHour[hw.TSHourStart.Unix()] = hw
		}
	}

	// 2. Process hours chronologically so snow state carries forward correctly.
	timestamps := make([]int64, 0, len(weatherByHour))
	for ts := range weatherByHour {
		timestamps = append(timestamps, ts)
	}
	slices.Sort(timestamps)

	// Identify the best irradiance source (GTI if available, fallback to GHI)
	var anyGTI bool
	var anyGHI bool
	for _, hw := range weatherByHour {
		if hw.GTI > 0 {
			anyGTI = true
		}
		if hw.GHI > 0 {
			anyGHI = true
		}
	}
	useGTI := anyGTI || !anyGHI // if no GHI either, doesn't matter, but if we have GTI use it.

	var minClippedIrradiance float64
	if clippingCap > 0 {
		for ts, stats := range statsByHour {
			if stats.SolarKWH >= clippingCap-clippingEps {
				if hw, ok := weatherByHour[ts]; ok {
					var irr float64
					if useGTI {
						irr = hw.GTI
					} else {
						irr = hw.GHI
					}
					if irr > 0 && (irr < minClippedIrradiance || minClippedIrradiance == 0) {
						minClippedIrradiance = irr
					}
				}
			}
		}
		if minClippedIrradiance > 0 {
			log.Ctx(ctx).DebugContext(
				ctx,
				"learned min clipped irradiance",
				slog.Float64("minClippedIrradiance", minClippedIrradiance),
			)
		}
	}

	type dailyAcc struct {
		solarKWH         float64
		theoreticalIrrad float64
		count            int
	}
	dailyData := make(map[string]*dailyAcc)

	timeLoc, err := time.LoadLocation(locInfo.TimeZone)
	if err != nil {
		timeLoc = time.UTC
	}

	results := make(map[int64]improvedSolar, len(timestamps))

	for _, ts := range timestamps {
		hw := weatherByHour[ts]
		h := improvedSolar{TSHourStart: ts}

		// Use GTI from Open-Meteo when available; fallback to GHI.
		if useGTI {
			h.Irradiance = hw.GTI
		} else {
			h.Irradiance = hw.GHI
		}

		// Estimated cell temperature via NOCT model:
		//   Tcell = Tamb + (Irradiance / 800) * (NOCT - 20)
		h.TCell = hw.TemperatureC + (h.Irradiance/800.0)*(noct-20.0)

		// Use direct snow depth from API which is an average from this hour to the next
		h.SnowDepth = hw.SnowDepthCM

		// Calculate factor based on cell difference compared to STC (25C cell temp).
		h.TCell = min(max(h.TCell, -40), 80)
		h.TempFactor = 1.0 - (h.TCell-25)*tempCoeff

		// > 5 cm: opaque layer, essentially zero generation.
		// > 0.2 cm: partial blockage, ~90 % reduction.
		// > 0 cm: light dusting, ~30 % reduction.
		h.SnowFactor = 1.0
		switch {
		case h.SnowDepth > 5:
			h.SnowFactor = 0.0
		case h.SnowDepth > 0.2:
			h.SnowFactor = 0.1
		case h.SnowDepth > 0:
			h.SnowFactor = 0.70
		}

		// Collect historical efficiency ratios that pass several quality filters:
		// 1. Minimum Irradiance: Ignore low light / noise levels.
		// 2. Curtailment: Ignore hours if the battery was nearly full and no export occurred.
		// 3. Snow: Ignore hours with significant snow accumulation.
		// 4. Clipping: If clipped, clamp irradiance to minClippedIrradiance.
		if stats, ok := statsByHour[ts]; ok {
			isCurtailed := stats.GridExportKWH <= 0.1 && stats.MaxBatterySOC >= 98.0
			isSnowy := h.SnowDepth > 0.2
			hasSolar := stats.SolarKWH > 0.5
			isClipped := clippingCap > 0 && stats.SolarKWH >= clippingCap-clippingEps

			// Filters: Irradiance < 25. Ignore low light.
			if h.Irradiance >= 25 && h.TempFactor > 0 && hasSolar && !isCurtailed && !isSnowy {
				effectiveIrradiance := h.Irradiance
				if isClipped && minClippedIrradiance > 0 {
					effectiveIrradiance = math.Min(h.Irradiance, minClippedIrradiance)
				}

				dayStr := time.Unix(ts, 0).In(timeLoc).Format("2006-01-02")
				if dailyData[dayStr] == nil {
					dailyData[dayStr] = &dailyAcc{}
				}
				dailyData[dayStr].solarKWH += stats.SolarKWH
				dailyData[dayStr].theoreticalIrrad += effectiveIrradiance * h.TempFactor * h.SnowFactor
				dailyData[dayStr].count++
			}
		}

		results[ts] = h
	}

	// 3. Determine robust efficiency by analyzing daily points
	var dailyEfficiencies []float64
	for _, acc := range dailyData {
		if acc.theoreticalIrrad > 0 && acc.count > 0 {
			dailyEfficiencies = append(dailyEfficiencies, acc.solarKWH/acc.theoreticalIrrad)
		}
	}

	var finalEff float64

	if len(dailyEfficiencies) > 0 {
		sort.Float64s(dailyEfficiencies)

		// simple averaging
		var sum float64
		for _, e := range dailyEfficiencies {
			sum += e
		}
		finalEff = sum / float64(len(dailyEfficiencies))
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calculated gti scale factor",
		slog.Float64("finalEfficiency", finalEff),
		slog.Any("dailyEfficiencies", dailyEfficiencies),
	)

	// 4. Project solar generation for every weather hour using the calibrated
	// efficiency unless we don't have any efficiency data.
	if finalEff > 0 {
		for _, ts := range timestamps {
			h := results[ts]
			if h.Irradiance > 0 {
				h.UnclippedSolar = h.Irradiance * finalEff * h.TempFactor * h.SnowFactor
				h.ImprovedSolar = h.UnclippedSolar
				if clippingCap > 0 && h.ImprovedSolar > clippingCap {
					h.ImprovedSolar = clippingCap
				}
			}
			results[ts] = h
		}
	}
	// TODO: if we dont have any efficiency data then we should just average the
	// last few days and assume it stays the same

	return results
}
