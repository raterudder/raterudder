package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const forecastHistoryDays = 35

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

// WeatherRes represents the solar forecast data for a specific hour in a response.
type WeatherRes struct {
	TSHourStart              time.Time `json:"tsHourStart"`
	ImprovedSolarGeneration  float64   `json:"improvedSolarGeneration,omitempty"`
	UnclippedSolarGeneration float64   `json:"unclippedSolarGeneration,omitempty"`
	ImprovedHomeLoad         float64   `json:"improvedHomeLoad,omitempty"`
	SnowDepthCM              float64   `json:"snowDepthCM,omitempty"`
	TempFactor               float64   `json:"tempFactor,omitempty"`
	SnowFactor               float64   `json:"snowFactor,omitempty"`
	TemperatureC             float64   `json:"temperatureC,omitempty"`
	TemperatureCellC         float64   `json:"temperatureCellC,omitempty"`
	Irradiance               float64   `json:"irradiance,omitempty"`
	SnowfallCM               float64   `json:"snowfallCM,omitempty"`
}

// ForecastRes represents the complete response for the forecast endpoint, including histories.
type ForecastRes struct {
	Simulation      []controller.SimHour `json:"simulation"`
	EnergyHistory   []EnergyHistoryRes   `json:"energyHistory"`
	PriceHistory    []PriceHistoryRes    `json:"priceHistory"`
	Solar1hForecast []WeatherRes         `json:"solar1hForecast,omitempty"`
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
		if errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).DebugContext(ctx, "failed to get ess system: ESS rate limited", slog.Any("error", err))
			writeJSONError(w, err.Error(), http.StatusTooManyRequests)
			return
		}
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

	// 5. Get History (Last x days from monthly summaries + today's/tomorrow's unsummarized data)
	now := status.Timestamp
	historyStart := now.AddDate(0, 0, -forecastHistoryDays).Truncate(time.Hour)
	energyHistory, weatherHistory, err := s.getCombinedHistory(ctx, siteID, settings, historyStart, now, nil)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get combined history for forecast", slog.Any("error", err))
		writeJSONError(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	flatEnergyHistory := flattenDailyEnergyStats(energyHistory)

	// 7. Run Simulation
	simHours := s.controller.SimulateState(ctx, now, status, currentPrice, futurePrices, flatEnergyHistory, weatherHistory, settings.Settings)

	// Fetch data for the previous day for history display
	histStart24 := now.AddDate(0, 0, -1).Truncate(time.Hour)
	energyHistory24 := make([]types.EnergyStats, 0, 24)
	for _, h := range flatEnergyHistory {
		if !h.TSHourStart.Before(histStart24) && h.TSHourStart.Before(now) {
			energyHistory24 = append(energyHistory24, h)
		}
	}

	// 8. Get Price History
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

	var solar1hRes []WeatherRes

	if settings.Location != nil && len(weatherHistory) > 0 {
		timeLoc, err := time.LoadLocation(settings.Location.TimeZone)
		if err == nil {
			todayMidnight := time.Date(now.In(timeLoc).Year(), now.In(timeLoc).Month(), now.In(timeLoc).Day(), 0, 0, 0, 0, timeLoc)
			tomorrowEnd := todayMidnight.AddDate(0, 0, 2)

			solar1hMap := controller.CalculateWeatherSolar1h(ctx, now, flatEnergyHistory, weatherHistory, *settings.Location)

			// Find matching forecast hours
			var allForecastHours []types.HourlyWeather
			for _, w := range weatherHistory {
				allForecastHours = append(allForecastHours, w.ForecastHours...)
			}
			// Sort them chronologically
			slices.SortFunc(allForecastHours, func(a, b types.HourlyWeather) int {
				return a.TSHourStart.Compare(b.TSHourStart)
			})

			for _, hw := range allForecastHours {
				if !hw.TSHourStart.Before(todayMidnight) && hw.TSHourStart.Before(tomorrowEnd) {
					ts := hw.TSHourStart.Unix()
					if ws, ok := solar1hMap[ts]; ok {
						solar1hRes = append(solar1hRes, WeatherRes{
							TSHourStart:              hw.TSHourStart,
							ImprovedSolarGeneration:  ws.ImprovedSolar,
							UnclippedSolarGeneration: ws.UnclippedSolar,
							SnowDepthCM:              ws.SnowDepth,
							TempFactor:               ws.TempFactor,
							SnowFactor:               ws.SnowFactor,
							TemperatureC:             hw.TemperatureC,
							TemperatureCellC:         ws.TCell,
							Irradiance:               ws.Irradiance,
							SnowfallCM:               hw.SnowfallCM,
						})
					}
				}
			}
		}
	}

	res := ForecastRes{
		Simulation:      simHours,
		EnergyHistory:   energyRes,
		PriceHistory:    priceRes,
		Solar1hForecast: solar1hRes,
	}

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(http.ErrAbortHandler)
	}
}
