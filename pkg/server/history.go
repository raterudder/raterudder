package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// HistoryEnergyRes represents the response for the history energy endpoint.
type HistoryEnergyRes struct {
	Energy  []types.EnergyStats `json:"energy"`
	Weather []WeatherRes        `json:"weather"`
}

func (s *Server) handleHistoryEnergy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// 1. Get Date from query
	dateStr := r.URL.Query().Get("date")
	var targetDate time.Time
	if dateStr == "" {
		targetDate = time.Now()
	} else {
		var err error
		targetDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			writeJSONError(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// 2. Get Settings and Location
	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	timeLoc := time.Local
	if settings.Location != nil && settings.Location.TimeZone != "" {
		var err error
		timeLoc, err = time.LoadLocation(settings.Location.TimeZone)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to load location", slog.Any("error", err), slog.String("timeZone", settings.Location.TimeZone))
			timeLoc = time.Local
		}
	}

	// 3. Calculate ranges
	// Target day in site timezone
	targetStart := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, timeLoc)
	targetEnd := targetStart.AddDate(0, 0, 1)

	// Calibration range: 3 days leading up to targetStart
	calibStart := targetStart.AddDate(0, 0, -3)

	// 4. Fetch Energy Stats
	// We need stats from calibStart to targetEnd for calibration + display
	allStats, err := s.storage.GetEnergyHistory(ctx, siteID, calibStart, targetEnd)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	// 5. Fetch Weather
	// We need weather from calibStart to targetEnd
	var weatherHistory []types.Weather
	if settings.Location != nil {
		weatherHistory, err = s.storage.GetWeather(ctx, siteID, calibStart, targetEnd)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch weather for history", slog.Any("error", err))
		}
	}

	// 6. Calculate Improved Solar
	var improvedSolarMap map[int64]improvedSolar
	if settings.Location != nil {
		improvedSolarMap = calculateImprovedSolar(ctx, allStats, weatherHistory, *settings.Location)
	}

	// 7. Filter results for the target day
	dayStats := make([]types.EnergyStats, 0)
	for _, s := range allStats {
		if !s.TSHourStart.Before(targetStart) && s.TSHourStart.Before(targetEnd) {
			dayStats = append(dayStats, s)
		}
	}

	dayWeather := make([]WeatherRes, 0)
	for _, w := range weatherHistory {
		for _, h := range w.ForecastHours {
			if !h.TSHourStart.Before(targetStart) && h.TSHourStart.Before(targetEnd) {
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
				dayWeather = append(dayWeather, wr)
			}
		}
	}

	res := HistoryEnergyRes{
		Energy:  dayStats,
		Weather: dayWeather,
	}

	w.Header().Set("Content-Type", "application/json")
	// Cache historical data for 24 hours if it's in the past
	if targetEnd.Before(time.Now()) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300")
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHistoryPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	prices, err := s.storage.GetPriceHistory(ctx, siteID, start, end)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get prices", slog.Any("error", err))
		writeJSONError(w, "failed to get prices", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control headers
	// If the range ends before today (midnight today), cache for 24 hours.
	// Otherwise, cache for 1 minute.
	today := truncateDay(time.Now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(prices); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHistoryActions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	actions, err := s.storage.GetActionHistory(ctx, siteID, start, end)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get actions", slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "failed to get actions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control headers
	// If the range ends before today (midnight today), cache for 24 hours.
	// Otherwise, cache for 1 minute.
	today := truncateDay(time.Now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(actions); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func parseTimeRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")

	if startStr == "" || endStr == "" {
		// Default to last 24 hours if not specified
		end := time.Now()
		start := end.Add(-24 * time.Hour)
		return start, end, nil
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
	}

	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time must be before end time")
	}

	if end.Sub(start) > 24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("time range cannot exceed 24 hours")
	}

	return start, end, nil
}
