package controller

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// batteryModelMinSamples is the minimum number of clean multi-hour discharge or charge
	// blocks required before overriding the static baseline efficiency parameters.
	batteryModelMinSamples = 10

	// batteryModelMinFitR2 is the minimum ordinary-least-squares R² required on the
	// MAD-trimmed blocks before accepting learned discharge or charge parameters.
	batteryModelMinFitR2 = 0.80

	// batteryModelMinEffBound and batteryModelMaxEffBound bound learned one-way AC/DC
	// efficiencies. Values slightly above 1.0 (up to 1.06) are physically valid when a
	// battery's true usable capacity per 100% SOC exceeds its nominal nameplate capacity
	// (e.g., Enphase IQ 5P or Tesla PW3 with reserve headroom).
	batteryModelMinEffBound = 0.80
	batteryModelMaxEffBound = 1.06

	// batteryModelRawMinEff and batteryModelRawMaxEff reject implausible raw OLS slopes
	// before Bayesian shrinkage toward the baseline prior.
	batteryModelRawMinEff = 0.70
	batteryModelRawMaxEff = 1.15

	// batteryModelMaxBlockHours caps contiguous block length when aggregating hourly
	// telemetry into multi-hour OLS samples to keep both sample count and SOC delta resolution high.
	batteryModelMaxBlockHours = 6

	// batteryModelMaxFixedOverheadFraction caps learned fixed discharge inverter/BMS overhead
	// as a fraction of battery capacity per hour (1.5% cap/hr = 450 W on a 30 kWh system).
	batteryModelMaxFixedOverheadFraction = 0.015

	// batteryModelMinNegInterceptFraction rejects discharge OLS fits whose raw or shrunk
	// intercept is more negative than -1.0% cap/hr (a sign of corrupted SOC calibration).
	batteryModelMinNegInterceptFraction = -0.01

	// Clean hourly block-selection thresholds.
	batteryModelMinReserveFloorSOC  = 2.0  // Floor (%) below which reserve-edge SOC readings are excluded
	batteryModelReserveMarginSOC    = 1.0  // Buffer (%) above reserve floor to avoid reserve-clamping bias
	batteryModelMaxDischargeSOC     = 99.5 // Exclude top-of-charge saturation during discharge blocks
	batteryModelMaxChargeSOC        = 99.0 // Exclude CV taper/saturation near 100% during charge blocks
	batteryModelMaxOppositeFlowKWH  = 0.02 // Max opposite-direction energy (kWh/hr) for a pure charge/discharge hour
	batteryModelMinDischargeHourKWH = 0.25 // Minimum metered discharge energy (kWh/hr) per hour and block average
	batteryModelMinChargeHourKWH    = 0.50 // Minimum metered charge energy (kWh/hr) per hour
	batteryModelMinChargeBlockAvgKW = 0.40 // Minimum block-average charge rate (kW)
	batteryModelMinBlockSOCDeltaKW  = 0.05 // Minimum implied SOC power delta (kW) to filter quantization noise

	// MAD outlier trimming parameters.
	batteryModelMADSigmaMultiplier    = 3.5    // Reject blocks beyond 3.5 robust standard deviations
	batteryModelMADToStdDevScale      = 1.4826 // Consistency factor converting MAD to Gaussian sigma
	batteryModelDischargeMADFloorFrac = 0.015  // Minimum discharge residual MAD floor as fraction of cap/hr
	batteryModelChargeMADFloorRatio   = 0.06   // Minimum charge ratio (y/x) MAD floor

	// Ridge / Bayesian shrinkage priors toward baseline parameters.
	batteryModelDischargePriorSamples      = 12.0 // Pseudo-observations anchoring discharge slope to 1/baselineEff
	batteryModelDischargePriorRateFraction = 0.04 // Characteristic C-rate (4%/hr) scaling the discharge slope prior
	batteryModelDischargeInterceptPriorWt  = 3.0  // Pseudo-observations shrinking fixed discharge overhead toward 0 kW
	batteryModelChargePriorSamples         = 10.0 // Pseudo-observations anchoring charge efficiency to baselineEff
	batteryModelChargePriorRateFraction    = 0.15 // Characteristic C-rate (15%/hr) scaling the charge slope prior
)

// BatteryModel holds physical battery conversion parameters for SOC simulation.
// When learned from site history, DischargeEff and ChargeEff absorb any systematic offset
// between nominal nameplate capacity and true usable energy per 100% SOC, while
// DischargeFixedKW captures constant inverter/BMS parasitic draw during active discharge.
type BatteryModel struct {
	EffectiveCapacityKWH float64
	DischargeEff         float64
	DischargeFixedKW     float64
	ChargeEff            float64

	Source           string
	DischargeSamples int
	ChargeSamples    int
}

// BaselineBatteryModel returns the static physics fallback matching resolveRoundTripEfficiency.
func BaselineBatteryModel(roundTripEff float64, currentStatus types.SystemStatus) BatteryModel {
	effRT := resolveRoundTripEfficiency(roundTripEff, currentStatus)
	oneWayEff := math.Sqrt(effRT)
	return BatteryModel{
		EffectiveCapacityKWH: currentStatus.BatteryCapacityKWH,
		DischargeEff:         oneWayEff,
		ChargeEff:            oneWayEff,
		Source:               "baseline",
	}
}

// resolveEfficiencies returns the effective one-way charge efficiency, discharge efficiency,
// and fixed discharge overhead (kW) for stepPhysics. A zero-value BatteryModel falls back to
// symmetric sqrt(roundTripEff) with zero fixed overhead so direct callers of stepPhysics remain
// backwards-compatible.
func (m BatteryModel) resolveEfficiencies(roundTripEff float64) (chargeEff, dischargeEff, dischargeFixedKW float64) {
	if m.ChargeEff > 0 && m.DischargeEff > 0 {
		return m.ChargeEff, m.DischargeEff, max(0.0, m.DischargeFixedKW)
	}
	oneWayEff := math.Sqrt(sanitizeRoundTripEfficiency(roundTripEff))
	return oneWayEff, oneWayEff, 0.0
}

// LearnBatteryModel fits per-site discharge efficiency, fixed discharge inverter/BMS overhead,
// and charge efficiency from completed hourly energy history using MAD-trimmed OLS with
// ridge/Bayesian shrinkage toward the static baseline prior.
//
// It requires only hourly EnergyStats (no 20-minute action polling or standby drift estimation)
// and safely falls back to BaselineBatteryModel whenever history is sparse, noisy (R² < 0.80),
// or out of physical bounds.
func LearnBatteryModel(
	ctx context.Context,
	history []types.EnergyStats,
	currentStatus types.SystemStatus,
	settings types.Settings,
) BatteryModel {
	model := BaselineBatteryModel(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
	capKWH := currentStatus.BatteryCapacityKWH
	if capKWH <= 0 || math.IsNaN(capKWH) || len(history) == 0 {
		return model
	}

	sorted := slices.Clone(history)
	slices.SortFunc(sorted, func(a, b types.EnergyStats) int {
		return a.TSHourStart.Compare(b.TSHourStart)
	})

	// Collect pure discharge hours away from reserve and 100% SOC edges where BMS clamping
	// or cell-balancing plateaus distort the linear relationship between SOC delta and kWh.
	minReserveSOC := max(settings.MinBatterySOC, batteryModelMinReserveFloorSOC)
	disX, disY := collectBatteryModelBlocks(sorted, capKWH, batteryModelMinDischargeHourKWH, false, func(h types.EnergyStats) bool {
		return h.BatteryChargedKWH < batteryModelMaxOppositeFlowKWH &&
			h.SolarToBatteryKWH < batteryModelMaxOppositeFlowKWH &&
			h.BatteryUsedKWH >= batteryModelMinDischargeHourKWH &&
			h.MinBatterySOC > minReserveSOC+batteryModelReserveMarginSOC &&
			h.MaxBatterySOC < batteryModelMaxDischargeSOC
	})
	fitBatteryDischarge(disX, disY, &model)

	// Collect pure charge hours below the 99% CV taper region (charge rate is typically
	// high enough that fixed overhead is negligible, so fitBatteryCharge uses zero intercept).
	chgX, chgY := collectBatteryModelBlocks(sorted, capKWH, batteryModelMinChargeBlockAvgKW, true, func(h types.EnergyStats) bool {
		return h.BatteryUsedKWH < batteryModelMaxOppositeFlowKWH &&
			h.BatteryChargedKWH >= batteryModelMinChargeHourKWH &&
			h.MaxBatterySOC < batteryModelMaxChargeSOC
	})
	fitBatteryCharge(chgX, chgY, &model)

	// Track whether both directions were learned, only one direction fell back to baseline,
	// or neither had enough clean data.
	if model.DischargeSamples > 0 && model.ChargeSamples > 0 {
		model.Source = "learned"
	} else if model.DischargeSamples+model.ChargeSamples > 0 {
		model.Source = "blended"
	}

	if model.Source != "baseline" {
		log.Ctx(ctx).DebugContext(ctx, "learned per-site battery model",
			slog.String("source", model.Source),
			slog.Float64("dischargeEff", model.DischargeEff),
			slog.Float64("dischargeFixedKW", model.DischargeFixedKW),
			slog.Float64("chargeEff", model.ChargeEff),
			slog.Int("dischargeSamples", model.DischargeSamples),
			slog.Int("chargeSamples", model.ChargeSamples),
		)
	}

	return model
}

// collectBatteryModelBlocks aggregates contiguous clean hourly stats into 2..6 hour blocks
// and returns per-hour average metered energy (xs, kW) and nominal SOC energy delta (ys, kW).
//
// Aggregating across 2..6 consecutive hours dramatically reduces 1% integer SOC quantization
// noise (which is 0.3 kWh on a 30 kWh battery) while keeping enough independent blocks for OLS.
func collectBatteryModelBlocks(
	sorted []types.EnergyStats,
	capKWH, minAvgKW float64,
	isCharge bool,
	match func(types.EnergyStats) bool,
) (xs, ys []float64) {
	for i, j := 0, 0; i < len(sorted); i = max(j, i+1) {
		// Extend [i, j) across contiguous 1-hour records free of heating, outages,
		// emergency backup, or VPP events that distort normal SOC-to-energy ratios.
		for j = i; j < len(sorted) &&
			!sorted[j].HadBatteryHeating &&
			!sorted[j].HadGridUnavailable &&
			!sorted[j].HadEmergencyMode &&
			!sorted[j].HadVPPActive &&
			match(sorted[j]) &&
			(j == i || sorted[j].TSHourStart.Sub(sorted[j-1].TSHourStart) == time.Hour); j++ {
		}
		// Slice the clean run into non-overlapping 2..6 hour segments (skipping 1-hour runs
		// where single-hour SOC quantization error dominates).
		for p := i; p+1 < j; {
			k := min(batteryModelMaxBlockHours, j-p)
			seg := sorted[p : p+k]
			p += k

			// Because the run is strictly monotonic (pure discharge or pure charge),
			// MaxBatterySOC of the first hour and MinBatterySOC of the last hour give the
			// full end-to-end SOC sweep across the k-hour block (and vice versa for charging).
			sumEnergyKWH := 0.0
			socDeltaPct := seg[0].MaxBatterySOC - seg[k-1].MinBatterySOC
			if isCharge {
				socDeltaPct = seg[k-1].MaxBatterySOC - seg[0].MinBatterySOC
			}
			for _, h := range seg {
				if isCharge {
					sumEnergyKWH += h.BatteryChargedKWH
				} else {
					sumEnergyKWH += h.BatteryUsedKWH
				}
			}
			avgMeteredKW := sumEnergyKWH / float64(k)
			avgSOCDeltaKW := (socDeltaPct / 100.0) * capKWH / float64(k)
			if avgMeteredKW >= minAvgKW && avgSOCDeltaKW > batteryModelMinBlockSOCDeltaKW {
				xs = append(xs, avgMeteredKW)
				ys = append(ys, avgSOCDeltaKW)
			}
		}
	}
	return xs, ys
}

// fitBatteryDischarge fits y = DischargeFixedKW + (1 / DischargeEff) * x on MAD-trimmed
// discharge blocks with ridge shrinkage toward (a=0, b=1/baselineDischargeEff).
//
// Here x is metered AC discharge power (kW) and y is nominal battery SOC drain rate (kW).
// Fitting an intercept captures constant inverter/BMS self-consumption that disproportionately
// affects low-load peak windows, while the slope 1/DischargeEff absorbs both proportional
// conversion loss and any systematic offset between nameplate and true usable capacity.
func fitBatteryDischarge(xs, ys []float64, model *BatteryModel) {
	capKWH := model.EffectiveCapacityKWH
	// Pass 1: initial OLS fit to compute per-block residuals for MAD outlier rejection.
	_, _, _, _, a0, b0, ok := batteryOLSStats(xs, ys)
	if !ok {
		return
	}
	tx, ty := filterBatterySamplesByMAD(xs, ys, batteryModelDischargeMADFloorFrac*capKWH, func(x, y float64) float64 {
		return y - (a0 + b0*x)
	})
	// Pass 2: verify trimmed data still has enough samples, positive slope, plausible
	// intercept, and strong linear fit (R² >= 0.80) before overriding baseline.
	sx, sy, sxx, sxy, a0, b0, ok := batteryOLSStats(tx, ty)
	if !ok || b0 <= 0 || a0 < batteryModelMinNegInterceptFraction*capKWH || !batteryFitR2AtLeast(tx, ty, sy, a0, b0) {
		return
	}

	// Solve the ridge-regularized 2x2 normal equations minimizing:
	//   Σ(y - a - b*x)² + w_a*(a - 0)² + w_b*(b - priorSlope)²
	// This prevents overfitting on sites with narrow load variance while letting strong,
	// well-separated multi-week data dominate.
	priorRateKW := batteryModelDischargePriorRateFraction * capKWH
	priorSlopeWeight := batteryModelDischargePriorSamples * priorRateKW * priorRateKW
	priorSlope := 1.0 / model.DischargeEff
	a00 := float64(len(tx)) + batteryModelDischargeInterceptPriorWt
	a11 := sxx + priorSlopeWeight
	r1 := sxy + priorSlopeWeight*priorSlope
	det := a00*a11 - sx*sx
	if det <= 1e-12 {
		return
	}
	fixedKW := (sy*a11 - sx*r1) / det
	slope := (a00*r1 - sy*sx) / det
	if fixedKW < batteryModelMinNegInterceptFraction*capKWH {
		return
	}

	// If the shrunk intercept hits the [0, 1.5% cap/hr] bounds, re-solve the 1D slope
	// conditioned on the clamped fixedKW so the total predicted drain stays unbiased.
	clampedFixedKW := min(max(fixedKW, 0.0), batteryModelMaxFixedOverheadFraction*capKWH)
	if clampedFixedKW != fixedKW {
		fixedKW = clampedFixedKW
		slope = (sxy - fixedKW*sx + priorSlopeWeight*priorSlope) / (sxx + priorSlopeWeight)
	}
	if slope <= 0 {
		return
	}
	rawEff := 1.0 / slope
	if rawEff < batteryModelRawMinEff || rawEff > batteryModelRawMaxEff {
		return
	}
	model.DischargeEff = min(max(rawEff, batteryModelMinEffBound), batteryModelMaxEffBound)
	model.DischargeFixedKW = fixedKW
	model.DischargeSamples = len(tx)
}

// fitBatteryCharge fits zero-intercept y = ChargeEff * x on MAD-trimmed charge blocks
// with ridge shrinkage toward the baseline charge efficiency prior.
//
// Charging typically occurs at high power (grid charge or strong solar), where SOC delta
// scales nearly linearly through the origin and adding a free intercept is poorly identified.
func fitBatteryCharge(xs, ys []float64, model *BatteryModel) {
	// Trim outlier blocks using the per-block efficiency ratio (y / x) via MAD.
	tx, ty := filterBatterySamplesByMAD(xs, ys, batteryModelChargeMADFloorRatio, func(x, y float64) float64 {
		return y / x
	})
	if len(tx) < batteryModelMinSamples {
		return
	}
	_, sy, sxx, sxy, _, _, _ := batteryOLSStats(tx, ty)
	if sxx <= 1e-9 {
		return
	}
	// Check raw zero-intercept OLS slope and R² before applying Bayesian shrinkage.
	rawEff := sxy / sxx
	if rawEff < batteryModelRawMinEff || rawEff > batteryModelRawMaxEff || !batteryFitR2AtLeast(tx, ty, sy, 0.0, rawEff) {
		return
	}
	// Shrink slope toward baseline ChargeEff: (Σxy + w*prior) / (Σx² + w).
	priorRateKW := batteryModelChargePriorRateFraction * model.EffectiveCapacityKWH
	priorWeight := batteryModelChargePriorSamples * priorRateKW * priorRateKW
	shrunkEff := (sxy + priorWeight*model.ChargeEff) / (sxx + priorWeight)
	model.ChargeEff = min(max(shrunkEff, batteryModelMinEffBound), batteryModelMaxEffBound)
	model.ChargeSamples = len(tx)
}

// batteryOLSStats accumulates sufficient statistics (Σx, Σy, Σx², Σxy) and computes the
// ordinary least squares intercept and slope for y = intercept + slope*x.
// Returns ok=false if fewer than batteryModelMinSamples points exist or x has zero variance.
func batteryOLSStats(xs, ys []float64) (sx, sy, sxx, sxy, intercept, slope float64, ok bool) {
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	n := float64(len(xs))
	if len(xs) >= batteryModelMinSamples && n*sxx-sx*sx > 1e-9 {
		slope = (n*sxy - sx*sy) / (n*sxx - sx*sx)
		intercept = (sy - slope*sx) / n
		ok = true
	}
	return
}

// batteryFitR2AtLeast reports whether the linear model y = intercept + slope*x achieves
// coefficient of determination R² = 1 - SSE/SST >= batteryModelMinFitR2.
func batteryFitR2AtLeast(xs, ys []float64, sumY, intercept, slope float64) bool {
	meanY := sumY / float64(len(ys))
	sse, sst := 0.0, 0.0
	for i := range xs {
		resid := ys[i] - (intercept + slope*xs[i])
		diff := ys[i] - meanY
		sse += resid * resid
		sst += diff * diff
	}
	return sst > 1e-9 && 1.0-sse/sst >= batteryModelMinFitR2
}

// filterBatterySamplesByMAD removes outlier blocks whose score (e.g. OLS residual or charge
// ratio) deviates from the median by more than 3.5 robust standard deviations (1.4826 * MAD),
// floored at minFloor so tight, low-noise sites do not over-trim valid points.
func filterBatterySamplesByMAD(
	xs, ys []float64,
	minFloor float64,
	score func(x, y float64) float64,
) (tx, ty []float64) {
	if len(xs) < batteryModelMinSamples {
		return nil, nil
	}
	scores := make([]float64, len(xs))
	for i := range xs {
		scores[i] = score(xs[i], ys[i])
	}
	med := medianSortedOrCopy(scores)
	for i, v := range scores {
		scores[i] = math.Abs(v - med)
	}
	threshold := max(batteryModelMADSigmaMultiplier*batteryModelMADToStdDevScale*medianSortedOrCopy(scores), minFloor)
	for i, dev := range scores {
		if dev <= threshold {
			tx = append(tx, xs[i])
			ty = append(ty, ys[i])
		}
	}
	return tx, ty
}
