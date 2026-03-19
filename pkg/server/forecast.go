package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
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
	TSHourStart             time.Time `json:"tsHourStart"`
	ForecastGHI             float64   `json:"forecastGHI"`
	ForecastGTI             float64   `json:"forecastGTI,omitempty"`
	TemperatureC            float64   `json:"temperatureC,omitempty"`
	TemperatureCellC        float64   `json:"temperatureCellC,omitempty"`
	SnowfallCM              float64   `json:"snowfallCM,omitempty"`
	ImprovedSolarGeneration float64   `json:"improvedSolarGeneration,omitempty"`
	SnowAccumulationCM      float64   `json:"snowAccumulationCM,omitempty"`
	TempFactor              float64   `json:"tempFactor,omitempty"`
	SnowFactor              float64   `json:"snowFactor,omitempty"`
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
	now := time.Now().In(status.Timestamp.Location())
	historyStart := now.AddDate(0, 0, -3).Truncate(time.Hour)
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, now)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
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
		improvedSolarMap = calculateImprovedSolar(ctx, energyHistory, weatherHistory)
	}

	weatherRes := make([]WeatherRes, 0, len(weatherHistory))
	for _, w := range weatherHistory {
		for _, h := range w.ForecastHours {
			wr := WeatherRes{
				TSHourStart:  h.TSHourStart,
				ForecastGHI:  h.GHI,
				ForecastGTI:  h.GTI,
				TemperatureC: h.TemperatureC,
				SnowfallCM:   h.SnowfallCM,
			}

			if improved, ok := improvedSolarMap[h.TSHourStart.Unix()]; ok {
				wr.ImprovedSolarGeneration = improved.ImprovedSolar
				wr.SnowAccumulationCM = improved.SnowAccumulation
				wr.TempFactor = improved.TempFactor
				wr.SnowFactor = improved.SnowFactor
				wr.TemperatureCellC = improved.TCell
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
	TSHourStart      int64
	ImprovedSolar    float64
	SnowAccumulation float64
	TempFactor       float64
	SnowFactor       float64
	TCell            float64
	GTI              float64
}

// calculateImprovedSolar estimates solar generation for each weather hour by:
//  1. Calibrating a robust efficiency factor from filtered historical actual solar vs. irradiance data.
//  2. Tracking snow accumulation and melt sequentially to derive a snow attenuation factor.
//  3. Applying NOCT-based cell temperature estimation to correct for temperature-dependent efficiency.
//  4. Projecting forward (and backward for history comparison) using the calibrated efficiency.
//
// Returns a map keyed by Unix timestamp (seconds) of each weather hour's computed improvedSolar.
func calculateImprovedSolar(ctx context.Context, history []types.EnergyStats, weather []types.Weather) map[int64]improvedSolar {
	const (
		noct      = 45.0   // Nominal Operating Cell Temperature
		tempCoeff = 0.0035 // typical power temperature coefficient
	)

	// 1. Index historical actual solar by hour timestamp for O(1) lookup.
	statsByHour := make(map[int64]types.EnergyStats, len(history))
	for _, h := range history {
		statsByHour[h.TSHourStart.Unix()] = h
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

	// Pre-calculate solar window for each day (hours with GTI > 50)
	// Key is the Unix timestamp of the start of the day.
	type solarWindow struct {
		start time.Time
		end   time.Time
	}
	solarWindows := make(map[int64]solarWindow)
	for _, ts := range timestamps {
		hw := weatherByHour[ts]
		gti := hw.GTI
		if gti <= 0 {
			gti = hw.GHI
		}
		if gti > 50 {
			dayStart := hw.TSHourStart.Truncate(24 * time.Hour).Unix()
			window := solarWindows[dayStart]
			if window.start.IsZero() || hw.TSHourStart.Before(window.start) {
				window.start = hw.TSHourStart
			}
			if window.end.IsZero() || hw.TSHourStart.After(window.end) {
				window.end = hw.TSHourStart
			}
			solarWindows[dayStart] = window
		}
	}

	var (
		efficiencies     []float64
		snowAccumulation float64
	)
	results := make(map[int64]improvedSolar, len(timestamps))

	for _, ts := range timestamps {
		hw := weatherByHour[ts]
		h := improvedSolar{TSHourStart: ts}

		// Use GTI when available (accounts for panel tilt/azimuth); fall back to GHI.
		gti := hw.GTI
		if gti <= 0 {
			gti = hw.GHI
		}
		h.GTI = gti

		// Estimated cell temperature via NOCT model:
		//   Tcell = Tamb + (GTI / 800) * (NOCT - 20)
		// At rated conditions (GTI=800 W/m²) the cell runs (NOCT-20) degrees above ambient.
		h.TCell = hw.TemperatureC + (gti/800.0)*(noct-20.0)

		// Add new snowfall first, then apply temperature-driven melt / slide-off.
		snowAccumulation += hw.SnowfallCM

		switch {
		case h.TCell > 5:
			// High melt: ~2 cm/hr. If residual drops below 1 cm it slides off.
			snowAccumulation -= 2
			if snowAccumulation < 1 {
				snowAccumulation = 0
			}
		case h.TCell > 2:
			// Moderate melt: ~1 cm/hr. If residual drops below 1 cm it slides off.
			snowAccumulation -= 1
			if snowAccumulation < 1 {
				snowAccumulation = 0
			}
		case h.TCell > 0:
			// Slow surface melt: ~0.5 cm/hr. Snow stays put at these temps.
			snowAccumulation -= 0.5
		}
		// Hard bounds: snow cannot be negative, and we cap at 10 cm (extreme event).
		snowAccumulation = min(max(0, snowAccumulation), 10)
		h.SnowAccumulation = snowAccumulation

		// Calculate factor based on cell difference compared to STC (25C cell temp).
		// Degrades by tempCoeff per C above 25 C; improves below 25 C.
		// Cell temp is physically bounded to the operating range [-40, 80] C.
		clampedTCell := min(max(h.TCell, -40), 80)
		h.TempFactor = 1.0 - (clampedTCell-25)*tempCoeff

		// > 5 cm: opaque layer, essentially zero generation.
		// > 0.2 cm: partial blockage, ~90 % reduction.
		// > 0 cm: light dusting, ~30 % reduction.
		h.SnowFactor = 1.0
		switch {
		case snowAccumulation > 5:
			h.SnowFactor = 0.0
		case snowAccumulation > 0.2:
			h.SnowFactor = 0.1
		case snowAccumulation > 0:
			h.SnowFactor = 0.70
		}

		// Collect historical efficiency ratios that pass several quality filters:
		// 1. Minimum Irradiance: Noise is high at dawn/dusk.
		// 2. Curtailment: Ignore hours if the battery was nearly full and no export occurred.
		// 3. Snow: Ignore hours with significant snow accumulation.
		// 4. Solar Window Edge: Avoid the first/last two hours of daily production range.
		if stats, ok := statsByHour[ts]; ok {
			dayStart := hw.TSHourStart.Truncate(24 * time.Hour).Unix()
			window := solarWindows[dayStart]

			isCurtailed := stats.GridExportKWH <= 0.1 && stats.MaxBatterySOC >= 98.0
			isSnowy := snowAccumulation > 0.2
			isEdge := hw.TSHourStart.Before(window.start.Add(2*time.Hour)) || hw.TSHourStart.After(window.end.Add(-2*time.Hour))
			hasSolar := stats.SolarKWH > 0.5

			if gti >= 50 && h.TempFactor > 0 && hasSolar && !isCurtailed && !isSnowy && !isEdge {
				eff := stats.SolarKWH / (gti * h.TempFactor * h.SnowFactor)
				efficiencies = append(efficiencies, eff)
			}
		}

		results[ts] = h
	}

	// 3. Determine robust efficiency (e.g., 90th percentile)
	var finalEff float64
	if len(efficiencies) > 0 {
		slices.Sort(efficiencies)
		index := int(0.9 * float64(len(efficiencies)))
		// If index is greater than or equal to the highest index, default to the
		// second highest or the highest if the dataset is small
		if index >= len(efficiencies)-1 {
			index = max(0, len(efficiencies)-2)
		}
		finalEff = efficiencies[index]
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calculated robust efficiency",
		slog.Float64("finalEfficiency", finalEff),
		slog.Int("validPoints", len(efficiencies)),
	)

	// 4. Project solar generation for every weather hour using the calibrated efficiency.
	if finalEff > 0 {
		for _, ts := range timestamps {
			h := results[ts]
			if h.GTI > 0 {
				h.ImprovedSolar = h.GTI * finalEff * h.TempFactor * h.SnowFactor
			}
			results[ts] = h
		}
	}

	return results
}
