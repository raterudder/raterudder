package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
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
	TSHourStart             time.Time `json:"tsHourStart"`
	ImprovedSolarGeneration float64   `json:"improvedSolarGeneration,omitempty"`
	ImprovedHomeLoad        float64   `json:"improvedHomeLoad,omitempty"`
	SnowDepthCM             float64   `json:"snowDepthCM,omitempty"`
	TempFactor              float64   `json:"tempFactor,omitempty"`
	SnowFactor              float64   `json:"snowFactor,omitempty"`
	TemperatureC            float64   `json:"temperatureC,omitempty"`
	Irradiance              float64   `json:"irradiance,omitempty"`
	SnowfallCM              float64   `json:"snowfallCM,omitempty"`
}

// ForecastRes represents the complete response for the forecast endpoint, including histories.
type ForecastRes struct {
	Plan          *types.Plan          `json:"plan,omitempty"`
	LatestAction  *types.Action        `json:"latestAction,omitempty"`
	Simulation    []controller.SimHour `json:"simulation,omitempty"`
	EnergyHistory []EnergyHistoryRes   `json:"energyHistory"`
	PriceHistory  []PriceHistoryRes    `json:"priceHistory"`
	Updated       time.Time            `json:"updated"`
}

func buildEnergyHistoryRes(stats []types.EnergyStats, start, end time.Time) []EnergyHistoryRes {
	var res []EnergyHistoryRes
	for _, h := range stats {
		if !h.TSHourStart.Before(start) && h.TSHourStart.Before(end) {
			avgSoc := (h.MinBatterySOC + h.MaxBatterySOC) / 2
			res = append(res, EnergyHistoryRes{
				TSHourStart:   h.TSHourStart,
				AvgBatterySOC: avgSoc,
				SolarKWH:      max(0, h.SolarKWH),
				HomeLoadKWH:   h.HomeKWH,
			})
		}
	}
	return res
}

func buildPriceHistoryRes(prices []types.Price) []PriceHistoryRes {
	res := make([]PriceHistoryRes, 0, len(prices))
	for _, p := range prices {
		res = append(res, PriceHistoryRes{
			TSHourStart:          p.TSStart,
			DollarsPerKWH:        p.DollarsPerKWH,
			GridUseDollarsPerKWH: p.GridUseDollarsPerKWH,
		})
	}
	return res
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

	if overrideStrategy := r.URL.Query().Get("overrideHomeLoadPredictionStrategy"); overrideStrategy != "" {
		settings.HomeLoadPredictionStrategy = overrideStrategy
	}

	if settings.ESS == "" {
		writeJSONError(w, "no ESS configured", http.StatusBadRequest)
		return
	}

	latestAction, err := s.storage.GetLatestAction(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get latest action", slog.Any("error", err))
	} else if latestAction != nil {
		if latestAction.SystemStatus.TimeLocation != "" {
			if loc, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
				latestAction.Timestamp = latestAction.Timestamp.In(loc)
				latestAction.SystemTimestamp = latestAction.SystemTimestamp.In(loc)
				latestAction.SystemStatus.Timestamp = latestAction.SystemStatus.Timestamp.In(loc)
			}
		}
	}

	now := s.now()
	if latestAction != nil && !latestAction.Timestamp.IsZero() && latestAction.Timestamp.Location() != nil {
		now = now.In(latestAction.Timestamp.Location())
	}

	// FAST-PATH: If we have a fresh plan generated within the last hour, bypass
	// the 35-day historical query, ESS status fetch, and controller simulation.
	isFreshPlan := latestAction != nil &&
		latestAction.Plan != nil &&
		len(latestAction.Plan.Periods) > 0 &&
		now.Sub(latestAction.Timestamp) >= 0 &&
		now.Sub(latestAction.Timestamp) <= time.Hour

	if isFreshPlan {
		histStart24 := now.AddDate(0, 0, -1).Truncate(time.Hour)
		energyHistory24, _, err := s.getCombinedHistory(ctx, siteID, settings, histStart24, now, nil)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get combined history for 24h forecast", slog.Any("error", err))
		}
		flatEnergy24 := flattenDailyEnergyStats(energyHistory24)
		energyRes := buildEnergyHistoryRes(flatEnergy24, histStart24, now)

		priceHistory24, err := s.storage.GetPriceHistory(ctx, siteID, histStart24, now)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch price history for forecast", slog.Any("error", err))
		}
		if latestAction.CurrentPrice != nil && latestAction.CurrentPrice.Contains(now) {
			var foundCurrentPrice bool
			for _, p := range priceHistory24 {
				if p.Contains(now) {
					foundCurrentPrice = true
					break
				}
			}
			if !foundCurrentPrice {
				priceHistory24 = append(priceHistory24, *latestAction.CurrentPrice)
			}
		}
		priceRes := buildPriceHistoryRes(priceHistory24)

		res := ForecastRes{
			Plan:          latestAction.Plan,
			LatestAction:  latestAction,
			EnergyHistory: energyRes,
			PriceHistory:  priceRes,
			Updated:       latestAction.Timestamp,
		}

		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(res); err != nil {
			panic(http.ErrAbortHandler)
		}
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
	var status types.SystemStatus
	var updatedTime time.Time
	useRealtime := true

	if latestAction != nil {
		since := s.now().Sub(latestAction.Timestamp)
		if since >= 0 && since <= time.Hour {
			status = latestAction.SystemStatus
			updatedTime = latestAction.Timestamp
			useRealtime = false
		}
	}

	if useRealtime {
		// don't bother warning if we didn't find an action
		if latestAction != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get action in the last hour, fetching realtime status", slog.Any("latestAction", latestAction))
		}
		realtimeStatus, err := essSystem.GetStatus(ctx)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get ess status", slog.Any("error", err))
			writeJSONError(w, "failed to get ess status", http.StatusInternalServerError)
			return
		}
		status = realtimeStatus
		updatedTime = status.Timestamp
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

	// merge utility mandatory VPP events and finalize VPP pricing
	vppInfo, err := utility.GetVPPInfo(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get utility VPP info", slog.Any("error", err))
	}
	status = s.mergeUtilityVPPEvents(ctx, status, vppInfo)

	// 5. Get History (Last x days from monthly summaries + today's/tomorrow's unsummarized data)
	if !status.Timestamp.IsZero() && status.Timestamp.Location() != nil {
		now = now.In(status.Timestamp.Location())
	}
	// Fall back to status.Timestamp if it is far from s.now() (e.g. tests with mocked static timestamps)
	if s.nowFunc == nil && !status.Timestamp.IsZero() && (status.Timestamp.Before(now.Add(-2*time.Hour)) || status.Timestamp.After(now.Add(2*time.Hour))) {
		now = status.Timestamp
	}
	historyStart := now.AddDate(0, 0, -forecastHistoryDays).Truncate(time.Hour)
	energyHistory, weatherHistory, err := s.getCombinedHistory(ctx, siteID, settings, historyStart, now, nil)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get combined history for forecast", slog.Any("error", err))
		writeJSONError(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	flatEnergyHistory := flattenDailyEnergyStats(energyHistory)

	histStart24 := now.AddDate(0, 0, -1).Truncate(time.Hour)
	energyRes := buildEnergyHistoryRes(flatEnergyHistory, histStart24, now)

	priceHistory24, err := s.storage.GetPriceHistory(ctx, siteID, histStart24, now)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to fetch price history for forecast", slog.Any("error", err))
	}

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
	priceRes := buildPriceHistoryRes(priceHistory24)

	// If on staging, try real-time Plan fallback
	if strings.EqualFold(s.release, "staging") {
		planDecision, freshPlan, planErr := s.controller.Plan(
			ctx, status, currentPrice, futurePrices, flatEnergyHistory, weatherHistory, settings.Settings, latestAction,
		)
		if planErr == nil {
			res := ForecastRes{
				Plan:          &freshPlan,
				LatestAction:  &planDecision.Action,
				EnergyHistory: energyRes,
				PriceHistory:  priceRes,
				Updated:       now,
			}
			w.Header().Set("Cache-Control", "private, max-age=300")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(res); err != nil {
				panic(http.ErrAbortHandler)
			}
			return
		}
		log.Ctx(ctx).WarnContext(ctx, "real-time plan fallback failed, falling back to simulation", slog.Any("error", planErr))
	}

	// 7. Run Simulation
	simHours, _ := s.controller.SimulateState(ctx, now, status, currentPrice, futurePrices, flatEnergyHistory, weatherHistory, settings.Settings)

	res := ForecastRes{
		Simulation:    simHours,
		EnergyHistory: energyRes,
		PriceHistory:  priceRes,
		Updated:       updatedTime,
	}

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(http.ErrAbortHandler)
	}
}
