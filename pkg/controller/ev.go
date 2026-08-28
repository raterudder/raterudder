package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// EVMinThresholdKW is the minimum household load required to consider EV charging active.
	EVMinThresholdKW = 4.8
	// EVMinStepKW is the minimum step increase above baseline load required to confirm EV charging.
	EVMinStepKW = 3.5
)

// detectEVCharging checks if the current instantaneous household load reflects active EV charging
// by evaluating whether the load is >= 4.8 kW and represents a step increase of >= 3.5 kW above
// recent baseline household load.
func detectEVCharging(ctx context.Context, currentLoad float64, history []types.EnergyStats) (bool, float64) {
	if currentLoad < EVMinThresholdKW {
		return false, 0
	}

	baselineKW := calculateRecentBaseline(history)
	stepKW := currentLoad - baselineKW
	isEV := stepKW >= EVMinStepKW

	log.Ctx(ctx).DebugContext(ctx, "evaluating ev charging detection",
		slog.Float64("currentLoad", currentLoad),
		slog.Float64("baselineKW", baselineKW),
		slog.Float64("stepKW", stepKW),
		slog.Float64("minStepKW", EVMinStepKW),
		slog.Bool("isEV", isEV),
	)

	return isEV, stepKW
}

// calculateRecentBaseline computes the non-EV baseline from recent hourly history.
// It inspects the preceding 1-6 hours, filtering out elevated hours to find the true household base load.
func calculateRecentBaseline(history []types.EnergyStats) float64 {
	if len(history) == 0 {
		return 1.0 // Default baseline assumption when no history is present
	}

	// Sort history descending by timestamp (most recent first)
	hCopy := make([]types.EnergyStats, len(history))
	copy(hCopy, history)
	sort.Slice(hCopy, func(i, j int) bool {
		return hCopy[i].TSHourStart.After(hCopy[j].TSHourStart)
	})

	var baselines []float64
	for i := 0; i < len(hCopy) && i < 6; i++ {
		// If an hour was below the EV threshold, it's a candidate baseline
		if hCopy[i].HomeKWH < EVMinThresholdKW && hCopy[i].HomeKWH > 0.05 {
			baselines = append(baselines, hCopy[i].HomeKWH)
		}
	}

	if len(baselines) == 0 {
		// If all recent 6 hours were elevated, look further back (up to 24 hours) for the first hour < EVMinThresholdKW
		for i := 6; i < len(hCopy) && i < 24; i++ {
			if hCopy[i].HomeKWH < EVMinThresholdKW && hCopy[i].HomeKWH > 0.05 {
				return hCopy[i].HomeKWH
			}
		}
		// If still none, look for the minimum load in recent history
		minVal := hCopy[0].HomeKWH
		for i := 1; i < len(hCopy) && i < 24; i++ {
			if hCopy[i].HomeKWH < minVal {
				minVal = hCopy[i].HomeKWH
			}
		}
		return minVal
	}

	sort.Float64s(baselines)
	// Return median baseline
	return baselines[len(baselines)/2]
}

// EstimateEVCharging analyzes daily energy history over up to 30 days to detect
// recurring nighttime EV charging sessions and determine recommended window hours and charging rate.
func EstimateEVCharging(dailyStats []types.DailyEnergyStats, loc *time.Location) types.EVDetectionResult {
	if len(dailyStats) == 0 {
		return types.EVDetectionResult{
			Detected: false,
			Message:  "No energy history available to analyze.",
		}
	}

	if loc == nil {
		loc = time.Local
	}

	// Extract and sort all hourly stats chronologically
	var allHours []types.EnergyStats
	for _, day := range dailyStats {
		for _, h := range day.Hourly {
			hInLoc := h
			hInLoc.TSHourStart = h.TSHourStart.In(loc)
			allHours = append(allHours, hInLoc)
		}
	}

	if len(allHours) == 0 {
		return types.EVDetectionResult{
			Detected: false,
			Message:  "No hourly energy stats found.",
		}
	}

	sort.Slice(allHours, func(i, j int) bool {
		return allHours[i].TSHourStart.Before(allHours[j].TSHourStart)
	})

	// Detect charging sessions across history
	var sessions []types.EVSession
	var rates []float64
	startHourCounts := make(map[int]int)
	endHourCounts := make(map[int]int)

	for i := 0; i < len(allHours); i++ {
		h := allHours[i]
		hr := h.TSHourStart.Hour()

		// Focus on nighttime / early morning charging (20:00 to 08:00)
		isNight := (hr >= 20 || hr <= 8)

		if isNight && h.HomeKWH >= EVMinThresholdKW {
			startIdx := i
			endIdx := i
			peak := h.HomeKWH
			totalKWH := h.HomeKWH

			for endIdx+1 < len(allHours) {
				nextH := allHours[endIdx+1]
				nextHr := nextH.TSHourStart.Hour()
				nextIsNight := (nextHr >= 20 || nextHr <= 8)

				if nextIsNight && nextH.HomeKWH >= EVMinThresholdKW {
					endIdx++
					totalKWH += nextH.HomeKWH
					if nextH.HomeKWH > peak {
						peak = nextH.HomeKWH
					}
				} else {
					break
				}
			}

			dur := endIdx - startIdx + 1
			preLoad := 1.0
			for prev := startIdx - 1; prev >= 0 && prev >= startIdx-12; prev-- {
				if allHours[prev].HomeKWH < EVMinThresholdKW && allHours[prev].HomeKWH > 0.05 {
					preLoad = allHours[prev].HomeKWH
					break
				}
			}

			step := peak - preLoad

			// Confirm session strictly if step >= 3.5 kW above pre-session baseline
			if step >= EVMinStepKW {
				avgKW := totalKWH / float64(dur)
				sessionEndHr := (allHours[endIdx].TSHourStart.Hour() + 1) % 24

				sess := types.EVSession{
					TSStartHour: h.TSHourStart,
					TSEndHour:   allHours[endIdx].TSHourStart.Add(time.Hour),
					DurationHr:  dur,
					PeakKW:      peak,
					AvgKW:       avgKW,
					TotalKWH:    totalKWH,
					NetStepKW:   step,
				}
				sessions = append(sessions, sess)
				rates = append(rates, peak)
				startHourCounts[hr]++
				endHourCounts[sessionEndHr]++
			}

			i = endIdx
		}
	}

	if len(sessions) == 0 {
		return types.EVDetectionResult{
			Detected: false,
			Message:  "No consistent nighttime EV charging detected in recent history.",
		}
	}

	// Calculate modal start and end hours
	modalStart := getModalHour(startHourCounts, 23)
	modalEnd := getModalHour(endHourCounts, 6)

	// Ensure window covers at least 4 hours
	durHours := (modalEnd - modalStart + 24) % 24
	if durHours < 4 {
		modalEnd = (modalStart + 6) % 24
	}

	// Calculate estimated rate (median peak rate across sessions)
	sort.Float64s(rates)
	estRate := rates[len(rates)/2]
	estRate = math.Round(estRate*10) / 10

	recommendedPeriod := types.TimePeriod{
		Name: "Nighttime EV Charging",
		Hours: []types.UtilityHourPeriod{
			{
				HourStart:   modalStart,
				MinuteStart: 0,
				HourEnd:     modalEnd,
				MinuteEnd:   0,
			},
		},
	}

	return types.EVDetectionResult{
		Detected:           true,
		RecommendedPeriod:  recommendedPeriod,
		AllDetectedPeriods: []types.TimePeriod{recommendedPeriod},
		EstimatedRateKW:    estRate,
		SessionsCount:      len(sessions),
		Sessions:           sessions,
		Message:            fmt.Sprintf("Detected %d charging sessions with ~%.1fkW charge rate.", len(sessions), estRate),
	}
}

func getModalHour(counts map[int]int, fallback int) int {
	maxCount := 0
	bestHour := fallback
	for hr, count := range counts {
		if count > maxCount {
			maxCount = count
			bestHour = hr
		}
	}
	return bestHour
}
