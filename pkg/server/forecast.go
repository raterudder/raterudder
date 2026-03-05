package server

import (
	"github.com/raterudder/raterudder/pkg/controller"

	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// EnergyHistoryRes represents a simplified historical energy stat returned in the forecast.
type EnergyHistoryRes struct {
	TSHourStart   time.Time `json:"tsHourStart"`
	AvgBatterySOC float64   `json:"avgBatterySOC"`
	SolarKWH      float64   `json:"solarKWH"`
}

// PriceHistoryRes represents historical pricing returned in the forecast.
type PriceHistoryRes struct {
	TSHourStart          time.Time `json:"tsHourStart"`
	DollarsPerKWH        float64   `json:"dollarsPerKWH"`
	GridUseDollarsPerKWH float64   `json:"gridUseDollarsPerKWH"`
}

// WeatherRes represents a simplified historical weather and forecast stat.
type WeatherRes struct {
	TSHourStart time.Time `json:"tsHourStart"`
	ActualGHI   float64   `json:"actualGHI,omitempty"`
	ForecastGHI float64   `json:"forecastGHI,omitempty"`
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

	// 5. Get History (Last 72 hours from Storage) - no backfill
	historyStart := time.Now().Add(-72 * time.Hour)
	historyEnd := time.Now()
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, historyEnd)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	// 6. Run Simulation
	now := time.Now().In(status.Timestamp.Location())
	simHours := s.controller.SimulateState(ctx, now, status, currentPrice, futurePrices, energyHistory, settings.Settings)

	// Fetch data for the previous 24 hours
	histStart24 := now.Add(-24 * time.Hour)
	histEnd24 := now

	energyHistory24, err := s.storage.GetEnergyHistory(ctx, siteID, histStart24, histEnd24)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to fetch energy history for forecast", slog.Any("error", err), slog.String("siteID", siteID))
	}

	priceHistory24, err := s.storage.GetPriceHistory(ctx, siteID, histStart24, histEnd24)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to fetch price history for forecast", slog.Any("error", err), slog.String("siteID", siteID))
	}

	var weatherHistory24 []types.Weather

	if settings.Location != nil {
		if timeLoc, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			startMidnight := time.Date(histStart24.Year(), histStart24.Month(), histStart24.Day(), 0, 0, 0, 0, timeLoc)
			endMidnight := time.Date(histEnd24.Year(), histEnd24.Month(), histEnd24.Day(), 0, 0, 0, 0, timeLoc).Add(24 * time.Hour)
			weatherHistory24, err = s.storage.GetWeather(ctx, siteID, startMidnight, endMidnight)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to fetch weather for forecast", slog.Any("error", err), slog.String("siteID", siteID))
			}
		}
	}

	var energyRes []EnergyHistoryRes
	for _, h := range energyHistory24 {
		avgSoc := (h.MinBatterySOC + h.MaxBatterySOC) / 2
		energyRes = append(energyRes, EnergyHistoryRes{
			TSHourStart:   h.TSHourStart,
			AvgBatterySOC: avgSoc,
			SolarKWH:      h.SolarKWH,
		})
	}

	var priceRes []PriceHistoryRes
	for _, p := range priceHistory24 {
		priceRes = append(priceRes, PriceHistoryRes{
			TSHourStart:          p.TSStart,
			DollarsPerKWH:        p.DollarsPerKWH,
			GridUseDollarsPerKWH: p.GridUseDollarsPerKWH,
		})
	}

	weatherMap := make(map[time.Time]WeatherRes)
	for _, w := range weatherHistory24 {
		for _, h := range w.ActualHours {
			if !h.TSHourStart.Before(histStart24.Truncate(time.Hour)) && !h.TSHourStart.After(histEnd24.Truncate(time.Hour).Add(48*time.Hour)) {
				wr := weatherMap[h.TSHourStart]
				wr.TSHourStart = h.TSHourStart
				wr.ActualGHI = h.GHI
				weatherMap[h.TSHourStart] = wr
			}
		}
		for _, h := range w.ForecastHours {
			if !h.TSHourStart.Before(histStart24.Truncate(time.Hour)) && !h.TSHourStart.After(histEnd24.Truncate(time.Hour).Add(48*time.Hour)) {
				wr := weatherMap[h.TSHourStart]
				wr.TSHourStart = h.TSHourStart
				wr.ForecastGHI = h.GHI
				weatherMap[h.TSHourStart] = wr
			}
		}
	}

	var weatherRes []WeatherRes
	for _, wr := range weatherMap {
		weatherRes = append(weatherRes, wr)
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
