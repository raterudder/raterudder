package server

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/crypto/hkdf"
)

// Tunable notification thresholds for anomaly detection.
// These constants are grouped here for easy iteration and simulation against actual site history.
const (
	// defaultSummaryFlavor is the default notification flavor for morning and evening summaries.
	defaultSummaryFlavor = "home_planner"

	// Default duration to wait before verifying a grid outage isn't a temporary blip.
	defaultGridOutageDelay = 5 * time.Minute

	// Maximum age of a quiet-period suppressed grid restoration event to still alert upon wakeup.
	maxDeferredGridRestoredAge = 1 * time.Hour

	// Deduplication window for VPP dispatches (allows separate morning/evening events).
	vppDispatchDeduplicationWindow = 5 * time.Hour

	// Minimum battery SOC increase (%) required to mention a projected peak in the morning summary.
	morningSummaryMinPeakDeltaSOC = 5.0

	// Multiplier for price surge to be considered a significant change (at least 20% higher than previous alert).
	priceSpikeSignificantMultiplier = 1.20

	// Escalated percentile tiers required for re-alerting within 1-6 hours on significant change.
	// Within 1 to 6 hours of a previous alert, a new alert is only sent if the price is at least 20%
	// higher than the previous alerted peak price AND meets these higher percentile thresholds.
	priceSpikeEscalatedPercentileLow    = 0.98 // Top 2% of all hours
	priceSpikeEscalatedPercentileMedium = 0.95 // Top 5% of all hours
	priceSpikeEscalatedPercentileHigh   = 0.95 // Top 5% of all hours

	// Minimum absolute price ($/kWh) before any price spike alert can trigger.
	// Prevents alerts during extremely cheap periods (e.g., prices jumping from 2¢ to 4¢/kWh).
	priceSpikeAbsoluteFloorDollarsPerKWH = 0.18

	// Minimum delta ($/kWh) that upcoming price must exceed the reference price to be considered a spike.
	// Low sensitivity alerts only on large surges ($0.10/kWh above reference);
	// Medium alerts on moderate surges ($0.05/kWh); High alerts on smaller surges ($0.03/kWh).
	priceSpikeMinDeltaLow    = 0.10
	priceSpikeMinDeltaMedium = 0.05
	priceSpikeMinDeltaHigh   = 0.03

	// Percentile of historical prices required to qualify as a price spike:
	// - High sensitivity: Top 10% (0.90) of all-hours AND time-of-day relative (+/- 1 hour buffer) with lower delta ($0.03/kWh).
	// - Medium sensitivity: Top 10% (0.90) of all-hours AND time-of-day relative (+/- 1 hour buffer) with moderate delta ($0.05/kWh).
	// - Low sensitivity: Top 5% (0.95) of all-hours AND time-of-day relative (+/- 1 hour buffer) with large delta ($0.10/kWh).
	priceSpikePercentileLow    = 0.95
	priceSpikePercentileMedium = 0.90
	priceSpikePercentileHigh   = 0.90

	// Minimum weather forecast solar (kW) required before solar underproduction is evaluated.
	solarUnderproductionMinForecastKW = 3.0

	// Minimum absolute deficit (kW) between forecast and actual generation to trigger alert.
	solarUnderproductionMinDeficitKW = 2.5

	// Ratio of actual generation to forecasted generation below which an alert is triggered.
	solarUnderproductionRatioLow    = 0.50 // 50% of forecast (noticeable underproduction / dirty panels)
	solarUnderproductionRatioMedium = 0.30 // 30% of forecast (moderate underproduction / partial string failure)
	solarUnderproductionRatioHigh   = 0.15 // 15% of forecast (severe underproduction / inverter offline)

	// Maximum cloud cover percentage above which underproduction alerts are suppressed (clouds explain deficit).
	solarUnderproductionMaxCloudCoverPercent = 60.0

	// highHomeLoadMinPriceDropDollarsPerKWH is the minimum price difference ($/kWh) required
	// to advise the user to wait for an upcoming cheaper rate. Smaller price drops (e.g. 1-4¢)
	// do not warrant behavioral changes or deferred appliance usage.
	highHomeLoadMinPriceDropDollarsPerKWH = 0.05

	// highHomeLoadSolarCoverageToleranceKW is the tolerance (in kW) above which solar is considered to cover home load.
	highHomeLoadSolarCoverageToleranceKW = 1.0

	// highHomeLoadPeakToAverageFactor scales the Time-of-Day baseline to account for peak-to-average appliance duty cycles.
	highHomeLoadPeakToAverageFactor = 1.30

	// highHomeLoadMinUsableBatteryKWH is the minimum usable energy above reserve required to alert before depletion.
	highHomeLoadMinUsableBatteryKWH = 1.5

	// highHomeLoadMaxChronicReserveRatio is the maximum ratio of hours at reserve around the time of day (+/- 1 hour)
	// over prior days above which at-reserve alerts are suppressed.
	highHomeLoadMaxChronicReserveRatio = 0.35

	// Minimum absolute load (kW) required before high home load alert can trigger for each sensitivity level.
	// Since HomeKW represents instantaneous power rather than hourly energy, these floors prevent routine single-appliance
	// cycling (like central AC units pulling 2.5-3.5 kW) from triggering alerts.
	//
	// Option A (Active): High = 3.5 kW, Medium = 4.5 kW, Low = 6.0 kW.
	//   - September simulation across 34 active sites showed a 17% reduction in fleet noise on Medium (4.1 alerts/mo avg)
	//     and 30% reduction on Low (2.5 alerts/mo avg), completely eliminating spurious AC alerts on sites like chemwiz78 and darryldaviddixon.
	//
	// Option B (Alternative for even quieter operation): High = 4.0 kW, Medium = 5.0 kW, Low = 7.0 kW.
	//   - Would require a dedicated heavy heating load (electric dryer, water heater, EV charger) to trigger Medium (3.9 alerts/mo avg),
	//     and reduced Low sensitivity alerts by 43% (2.0 alerts/mo avg).
	highHomeLoadMinAbsoluteKWLow    = 6.0
	highHomeLoadMinAbsoluteKWMedium = 4.5
	highHomeLoadMinAbsoluteKWHigh   = 3.5

	// Percentiles for Time-of-Day (TOD) window [H-1, H, H+1].
	highHomeLoadTODPercentileLow    = 0.95
	highHomeLoadTODPercentileMedium = 0.90
	highHomeLoadTODPercentileHigh   = 0.80

	// Cooldown window between high home load alerts.
	highHomeLoadCooldownWindow = 6 * time.Hour
)

// decodeBase64Key decodes a base64 string using base64.RawURLEncoding (RFC 7515 / RFC 8291).
func decodeBase64Key(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
}

// parseVAPIDPrivateKey parses a base64-encoded 32-byte ECDSA P-256 private key scalar.
func parseVAPIDPrivateKey(privateKeyStr string) (*ecdsa.PrivateKey, string, error) {
	privBytes, err := decodeBase64Key(privateKeyStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode private key base64: %w", err)
	}

	if len(privBytes) != 32 {
		return nil, "", fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(privBytes))
	}

	ecdhKey, err := ecdh.P256().NewPrivateKey(privBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse private key scalar: %w", err)
	}

	pubBytes := ecdhKey.PublicKey().Bytes()
	publicKeyStr := base64.RawURLEncoding.EncodeToString(pubBytes)

	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(pubBytes[1:33]),
			Y:     new(big.Int).SetBytes(pubBytes[33:65]),
		},
		D: new(big.Int).SetBytes(privBytes),
	}

	return priv, publicKeyStr, nil
}

// generateMorningSummary generates copy for the morning summary report based on selected flavor.
func generateMorningSummary(
	ctx context.Context,
	flavor string,
	currentStatus types.SystemStatus,
	todayForecastKWH float64,
	yesterdayActualKWH float64,
	peakSolarKWH float64,
	hitCapacityAt time.Time,
	projectedPeakSOC float64,
	timeLoc *time.Location,
) (string, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeLoc == nil {
		timeLoc = time.UTC
	}
	if projectedPeakSOC <= 0 {
		projectedPeakSOC = currentStatus.BatterySOC
	}

	currentEnergyKWH := currentStatus.BatterySOC * currentStatus.BatteryCapacityKWH / 100.0

	deltaPct := 0.0
	hasYesterday := yesterdayActualKWH > 0.5
	if hasYesterday {
		deltaPct = ((todayForecastKWH - yesterdayActualKWH) / yesterdayActualKWH) * 100.0
	}

	var relWording string
	if !hasYesterday {
		relWording = fmt.Sprintf("%.1f kWh expected", todayForecastKWH)
	} else if deltaPct >= 10.0 {
		relWording = fmt.Sprintf("+%.0f%% vs yesterday", deltaPct)
	} else if deltaPct <= -10.0 {
		relWording = fmt.Sprintf("%.0f%% vs yesterday", deltaPct)
	} else {
		relWording = "similar to yesterday"
	}

	hitCapacityStr := ""
	if !hitCapacityAt.IsZero() {
		hitCapacityStr = hitCapacityAt.In(timeLoc).Format("3:04 PM")
	}

	solarRatio := 0.0
	if peakSolarKWH > 0 {
		solarRatio = todayForecastKWH / peakSolarKWH
	} else if todayForecastKWH > 0 {
		solarRatio = 1.0
	}

	var title, body string
	switch flavor {
	case "home_planner":
		if solarRatio >= 0.80 {
			if hitCapacityStr != "" {
				title = "☀️ Great Solar Day Ahead"
			} else {
				title = "☀️ Clear Skies Ahead"
			}
		} else if solarRatio >= 0.45 {
			title = "⛅ Moderate Solar Outlook"
		} else {
			title = "☁️ Low Solar Outlook"
		}

		if hitCapacityStr != "" {
			body = fmt.Sprintf("Battery at %.0f%%. Full battery expected by %s.", currentStatus.BatterySOC, hitCapacityStr)
		} else if solarRatio >= 0.80 {
			if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
				body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Strong solar today (%s) will help cover daytime home usage.", currentStatus.BatterySOC, projectedPeakSOC, relWording)
			} else {
				body = fmt.Sprintf("Battery at %.0f%%. Strong solar today (%s) will help cover daytime home usage.", currentStatus.BatterySOC, relWording)
			}
		} else if solarRatio >= 0.45 {
			if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
				body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Moderate solar expected today (%s).", currentStatus.BatterySOC, projectedPeakSOC, relWording)
			} else {
				body = fmt.Sprintf("Battery at %.0f%%. Moderate solar expected today (%s); solar will help cover baseline load.", currentStatus.BatterySOC, relWording)
			}
		} else {
			body = fmt.Sprintf("Battery at %.0f%%. Solar will be limited today (%s). Consider avoiding heavy loads.", currentStatus.BatterySOC, relWording)
		}

	case "executive":
		title = fmt.Sprintf("☀️ %.1f kWh Solar Expected • 🔋 %.0f%% SOC", todayForecastKWH, currentStatus.BatterySOC)
		if hitCapacityStr != "" {
			if solarRatio >= 0.70 {
				body = fmt.Sprintf("Great solar today; battery will fully top off by %s.", hitCapacityStr)
			} else {
				body = fmt.Sprintf("Battery will reach full charge by %s (%s).", hitCapacityStr, relWording)
			}
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Solar expected: %.1f kWh (%s). Battery projected to reach ~%.0f%%.", todayForecastKWH, relWording, projectedPeakSOC)
		} else {
			body = fmt.Sprintf("Solar limited today (%s); battery will supply home without charging.", relWording)
		}

	case "pilot":
		title = "🤖 RateRudder: Morning Outlook"
		if hitCapacityStr != "" {
			body = fmt.Sprintf("Battery at %.0f%%. Forecast shows %.1f kWh solar refilling battery by %s. Optimizing daytime self-consumption.", currentStatus.BatterySOC, todayForecastKWH, hitCapacityStr)
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Forecast shows %.1f kWh solar today. Optimizing self-consumption to defend peak hours.", currentStatus.BatterySOC, projectedPeakSOC, todayForecastKWH)
		} else {
			body = fmt.Sprintf("Battery at %.0f%%. Solar limited (%.1f kWh). Preserving battery reserve to defend peak pricing hours.", currentStatus.BatterySOC, todayForecastKWH)
		}

	case "metrics_heavy":
		fallthrough
	default:
		title = fmt.Sprintf("🔋 %.0f%% SOC (%.1f kWh) • ☀️ %.1f kWh Solar", currentStatus.BatterySOC, currentEnergyKWH, todayForecastKWH)
		if hitCapacityStr != "" {
			body = fmt.Sprintf("Forecast: %s. Full charge expected by %s.", relWording, hitCapacityStr)
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Forecast: %s. Battery projected to peak at ~%.0f%% today.", relWording, projectedPeakSOC)
		} else {
			body = fmt.Sprintf("Forecast: %s. Battery not projected to charge today (currently %.0f%%).", relWording, currentStatus.BatterySOC)
		}
	}

	log.Ctx(ctx).DebugContext(ctx, "generated morning summary",
		slog.String("flavor", flavor),
		slog.Float64("batterySOC", currentStatus.BatterySOC),
		slog.Float64("batteryCapacityKWH", currentStatus.BatteryCapacityKWH),
		slog.Float64("currentEnergyKWH", currentEnergyKWH),
		slog.Float64("todayForecastKWH", todayForecastKWH),
		slog.Float64("yesterdayActualKWH", yesterdayActualKWH),
		slog.Float64("peakSolarKWH", peakSolarKWH),
		slog.Float64("solarRatio", solarRatio),
		slog.Float64("deltaPct", deltaPct),
		slog.String("relWording", relWording),
		slog.String("hitCapacityAt", hitCapacityStr),
		slog.Float64("projectedPeakSOC", projectedPeakSOC),
		slog.String("title", title),
		slog.String("body", body),
	)

	return title, body
}

// generateEveningSummary generates copy for the evening summary report based on selected flavor.
func generateEveningSummary(
	ctx context.Context,
	flavor string,
	currentStatus types.SystemStatus,
	todayActualSolarKWH float64,
	todayHomeUsageKWH float64,
	todayGridExportKWH float64,
	todayGridImportKWH float64,
	minBatterySOC float64,
	hitDeficitAt time.Time,
	scheduledChargeAt time.Time,
	timeLoc *time.Location,
) (string, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeLoc == nil {
		timeLoc = time.UTC
	}
	currentEnergyKWH := currentStatus.BatterySOC * currentStatus.BatteryCapacityKWH / 100.0

	var deficitStr string
	if !hitDeficitAt.IsZero() {
		deficitStr = hitDeficitAt.In(timeLoc).Format("3:04 PM")
	}
	var chargeStr string
	if !scheduledChargeAt.IsZero() {
		chargeStr = scheduledChargeAt.In(timeLoc).Format("3:04 PM")
	}

	reserveThreshold := minBatterySOC
	if reserveThreshold <= 0 {
		reserveThreshold = 20.0
	}

	isLowReserve := currentStatus.BatterySOC <= reserveThreshold+2.0 ||
		(!hitDeficitAt.IsZero() && !currentStatus.Timestamp.IsZero() && hitDeficitAt.Before(currentStatus.Timestamp.In(timeLoc).Add(30*time.Minute)))

	var title, body string
	switch flavor {
	case "home_planner":
		title = "🌙 Evening Energy Wrap-up"
		if !scheduledChargeAt.IsZero() {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Scheduled to charge from the grid at ~%s.", currentStatus.BatterySOC, currentEnergyKWH, chargeStr)
		} else if isLowReserve {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Reserve is low; home will switch to grid power shortly.", currentStatus.BatterySOC, currentEnergyKWH)
		} else if hitDeficitAt.IsZero() {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Projected to power home through the night until tomorrow's solar.", currentStatus.BatterySOC, currentEnergyKWH)
		} else {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Projected to supply home until ~%s.", currentStatus.BatterySOC, currentEnergyKWH, deficitStr)
		}

	case "executive":
		title = fmt.Sprintf("🌙 %.1f kWh Solar Today • 🔋 %.0f%% SOC", todayActualSolarKWH, currentStatus.BatterySOC)
		if todayGridExportKWH > 0.5 {
			body = fmt.Sprintf("Solar generated %.1f kWh today with %.1f kWh exported to the grid. Battery entering night at %.0f%%.", todayActualSolarKWH, todayGridExportKWH, currentStatus.BatterySOC)
		} else if todayHomeUsageKWH > 0 && todayActualSolarKWH >= todayHomeUsageKWH {
			body = fmt.Sprintf("Solar generated %.1f kWh today, fully covering home needs. Battery entering night at %.0f%%.", todayActualSolarKWH, currentStatus.BatterySOC)
		} else if todayHomeUsageKWH > 0 && todayActualSolarKWH > 0.5 {
			coveragePct := (todayActualSolarKWH / todayHomeUsageKWH) * 100.0
			body = fmt.Sprintf("Solar generated %.1f kWh today (covered %.0f%% of home use). Battery entering night at %.0f%%.", todayActualSolarKWH, coveragePct, currentStatus.BatterySOC)
		} else {
			body = fmt.Sprintf("Home used %.1f kWh today with minimal solar. Battery entering night at %.0f%%.", todayHomeUsageKWH, currentStatus.BatterySOC)
		}

	case "pilot":
		title = "🤖 RateRudder: Evening Wrap-up"
		if !scheduledChargeAt.IsZero() {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Scheduled to charge from the grid at ~%s.", currentStatus.BatterySOC, currentEnergyKWH, chargeStr)
		} else if isLowReserve {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Reserve is low; home will switch to grid power shortly.", currentStatus.BatterySOC, currentEnergyKWH)
		} else if hitDeficitAt.IsZero() {
			if todayActualSolarKWH >= 2.0 {
				body = fmt.Sprintf("Automated battery managed %.1f kWh solar today. Stored %.1f kWh projected to power home through sunrise.", todayActualSolarKWH, currentEnergyKWH)
			} else {
				body = fmt.Sprintf("Battery maintained %.1f kWh reserve on a low solar day. Stored energy projected to power home through sunrise.", currentEnergyKWH)
			}
		} else {
			if todayActualSolarKWH >= 2.0 {
				body = fmt.Sprintf("Automated battery managed %.1f kWh solar today. Stored %.1f kWh will supply home until ~%s.", todayActualSolarKWH, currentEnergyKWH, deficitStr)
			} else {
				body = fmt.Sprintf("Stored %.1f kWh will supply home until ~%s.", currentEnergyKWH, deficitStr)
			}
		}

	case "metrics_heavy":
		fallthrough
	default:
		title = fmt.Sprintf("🌙 %.1f kWh Solar • 🔋 %.0f%% SOC (%.1f kWh)", todayActualSolarKWH, currentStatus.BatterySOC, currentEnergyKWH)
		var flowStr string
		if todayGridExportKWH > 0.0 {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home (%.1f kWh exported).", todayActualSolarKWH, todayHomeUsageKWH, todayGridExportKWH)
		} else if todayGridImportKWH > 0.0 {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home (%.1f kWh imported).", todayActualSolarKWH, todayHomeUsageKWH, todayGridImportKWH)
		} else {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home.", todayActualSolarKWH, todayHomeUsageKWH)
		}

		if !scheduledChargeAt.IsZero() {
			body = fmt.Sprintf("%s Battery: %.1f kWh (scheduled to charge from grid at ~%s).", flowStr, currentEnergyKWH, chargeStr)
		} else if isLowReserve {
			body = fmt.Sprintf("%s Battery: %.1f kWh (reserve is low; home will switch to grid power shortly).", flowStr, currentEnergyKWH)
		} else if hitDeficitAt.IsZero() {
			body = fmt.Sprintf("%s Battery: %.1f kWh powers home through sunrise.", flowStr, currentEnergyKWH)
		} else {
			body = fmt.Sprintf("%s Battery: %.1f kWh powers home until ~%s.", flowStr, currentEnergyKWH, deficitStr)
		}
	}

	log.Ctx(ctx).DebugContext(ctx, "generated evening summary",
		slog.String("flavor", flavor),
		slog.Float64("batterySOC", currentStatus.BatterySOC),
		slog.Float64("batteryCapacityKWH", currentStatus.BatteryCapacityKWH),
		slog.Float64("currentEnergyKWH", currentEnergyKWH),
		slog.Float64("todayActualSolarKWH", todayActualSolarKWH),
		slog.Float64("todayHomeUsageKWH", todayHomeUsageKWH),
		slog.Float64("todayGridExportKWH", todayGridExportKWH),
		slog.Float64("todayGridImportKWH", todayGridImportKWH),
		slog.Float64("minBatterySOC", minBatterySOC),
		slog.Float64("reserveThreshold", reserveThreshold),
		slog.String("hitDeficitAt", deficitStr),
		slog.String("title", title),
		slog.String("body", body),
	)

	return title, body
}

// notificationsEnabled returns true if VAPID keys are properly configured.
func (s *Server) notificationsEnabled() bool {
	return s.vapidKey != nil && s.vapidPublicKey != ""
}

// pushPayload defines the JSON payload structure sent inside the Web Push body.
type pushPayload struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Tag   string         `json:"tag,omitempty"`
	Icon  string         `json:"icon"`
	Badge string         `json:"badge,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// pushPayloadIcon returns the icon URL for the push notification.
// For installed app / WebAPK subscriptions, Android already displays the app launcher icon on the left;
// returning a transparent icon avoids displaying a redundant duplicate icon on the right
// while preventing Chrome on Android from falling back to generating a letter monogram avatar ("R").
func pushPayloadIcon(sub types.PushSubscription) string {
	if sub.AppType == types.PushSubscriptionAppTypeWebAPK {
		// TODO: remove this after https://issues.chromium.org/issues/568852330
		return "/transparent_192.png"
	}
	return "/logo_192.png"
}

// encryptWebPushPayload encrypts a plaintext message for a subscriber using RFC 8291 (aes128gcm).
// Peer public keys must be 65 bytes in uncompressed EC point format (ANSI X9.62 / SEC 1: 0x04 prefix byte followed by 32 bytes X and 32 bytes Y coordinate).
func encryptWebPushPayload(plaintext []byte, peerPublicKeyBase64, peerAuthBase64 string) ([]byte, string, error) {
	peerPubBytes, err := decodeBase64Key(peerPublicKeyBase64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode subscriber public key: %w", err)
	}
	// SEC 1 uncompressed point format: 1 byte header (0x04) + 32 bytes X + 32 bytes Y = 65 bytes
	if len(peerPubBytes) != 65 || peerPubBytes[0] != 0x04 {
		return nil, "", fmt.Errorf("invalid subscriber public key: expected 65-byte uncompressed point")
	}

	peerAuthBytes, err := decodeBase64Key(peerAuthBase64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode subscriber auth secret: %w", err)
	}

	peerPub, err := ecdh.P256().NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse subscriber public key: %w", err)
	}

	// 1. Generate local ephemeral ECDH keypair
	localPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate ephemeral keypair: %w", err)
	}
	localPubBytes := localPriv.PublicKey().Bytes()

	// 2. Compute shared secret
	sharedSecret, err := localPriv.ECDH(peerPub)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compute shared ECDH secret: %w", err)
	}

	// 3. Generate 16-byte random salt
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	// 4. Derive PRK using peerAuthBytes as salt and sharedSecret as IKM
	prkHKDF := hkdf.Extract(sha256.New, sharedSecret, peerAuthBytes)

	// Derive IKM
	var keyInfo bytes.Buffer
	keyInfo.WriteString("WebPush: info\x00")
	keyInfo.Write(peerPubBytes)
	keyInfo.Write(localPubBytes)

	ikmReader := hkdf.Expand(sha256.New, prkHKDF, keyInfo.Bytes())
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(ikmReader, ikm); err != nil {
		return nil, "", fmt.Errorf("failed to derive IKM: %w", err)
	}

	// 5. Derive Content Encryption Key (CEK) and Nonce
	prk := hkdf.Extract(sha256.New, ikm, salt)
	encoding := "aes128gcm"

	cekInfo := []byte("Content-Encoding: " + encoding + "\x00")
	cekReader := hkdf.Expand(sha256.New, prk, cekInfo)
	cek := make([]byte, 16)
	if _, err := io.ReadFull(cekReader, cek); err != nil {
		return nil, "", fmt.Errorf("failed to derive CEK: %w", err)
	}

	nonceInfo := []byte("Content-Encoding: nonce\x00")
	nonceReader := hkdf.Expand(sha256.New, prk, nonceInfo)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return nil, "", fmt.Errorf("failed to derive nonce: %w", err)
	}

	// 6. Encrypt with AES-GCM-128
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Payload plaintext padding: append delimiter byte 0x02
	paddedPlaintext := append(plaintext, 0x02)
	ciphertext := gcm.Seal(nil, nonce, paddedPlaintext, nil)

	// 7. Construct RFC 8188 header
	// Header format:
	// salt (16 bytes) || rs (4 bytes) || idlen (1 byte) || keyid (65 bytes) || ciphertext
	const recordSize uint32 = 4096
	var buf bytes.Buffer
	buf.Write(salt)
	// binary.Write to an in-memory bytes.Buffer never returns an error, so ignoring error is safe.
	_ = binary.Write(&buf, binary.BigEndian, recordSize)
	buf.WriteByte(byte(len(localPubBytes)))
	buf.Write(localPubBytes)
	buf.Write(ciphertext)

	return buf.Bytes(), "aes128gcm", nil
}

// createVAPIDToken generates a signed JWT token for VAPID authorization (RFC 8292).
// audience specifies the push service origin URI (e.g. "https://fcm.googleapis.com").
// subject specifies the contact URI (mailto: or https:) for the application server admin.
func createVAPIDToken(privKey *ecdsa.PrivateKey, audience string, subject string, expiration time.Time) (string, error) {
	header := map[string]string{
		"typ": "JWT",
		"alg": "ES256",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claims := map[string]any{
		"aud": audience,
		"exp": expiration.Unix(),
		"sub": subject,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	hash := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign VAPID token: %w", err)
	}

	// Format signature as IEEE P1363 (32 bytes R || 32 bytes S)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return signingInput + "." + sigB64, nil
}

// webPushTopic converts a notification tag into a compliant RFC 8030 Topic header value.
// RFC 8030 Section 5.4 restricts Topic to at most 32 characters using the base64url alphabet ([A-Za-z0-9_-]).
func webPushTopic(tag string) string {
	if tag == "" {
		return ""
	}
	var clean strings.Builder
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			clean.WriteRune(r)
		}
	}
	s := clean.String()
	if len(s) == 0 {
		return ""
	}
	if len(s) <= 32 {
		return s
	}
	// If longer than 32 characters, derive a deterministic 22-character base64url string.
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:16])
}

// sendWebPush sends an encrypted Web Push notification using RFC 8291 and RFC 8292.
func (s *Server) sendWebPush(
	ctx context.Context,
	sub types.PushSubscription,
	payload pushPayload,
	ttlSeconds int,
	urgency string,
) (int, error) {
	if !s.notificationsEnabled() {
		return 0, errors.New("notifications are not enabled on server")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal push payload: %w", err)
	}

	encryptedBody, encoding, err := encryptWebPushPayload(payloadBytes, sub.Keys.P256DH, sub.Keys.Auth)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt push payload: %w", err)
	}

	parsedEndpoint, err := url.Parse(sub.Endpoint)
	if err != nil {
		return 0, fmt.Errorf("invalid push subscription endpoint: %w", err)
	}
	audience := fmt.Sprintf("%s://%s", parsedEndpoint.Scheme, parsedEndpoint.Host)

	// RFC 8292 Section 2 mandates that VAPID expiration must not be more than 24 hours in the future.
	// 12 hours provides sufficient leeway for network retries while complying with RFC requirements.
	vapidToken, err := createVAPIDToken(s.vapidKey, audience, s.vapidSubject, s.now().Add(12*time.Hour))
	if err != nil {
		return 0, fmt.Errorf("failed to generate VAPID token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(encryptedBody))
	if err != nil {
		return 0, fmt.Errorf("failed to create push request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", encoding)
	req.Header.Set("TTL", fmt.Sprintf("%d", ttlSeconds))
	if urgency != "" {
		req.Header.Set("Urgency", urgency)
	}
	if topic := webPushTopic(payload.Tag); topic != "" {
		req.Header.Set("Topic", topic)
	}

	// Support both RFC 8292 'vapid' and modern 'WebPush' schemes
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", vapidToken, s.vapidPublicKey))
	req.Header.Set("Crypto-Key", fmt.Sprintf("p256ecdsa=%s", s.vapidPublicKey))

	client := common.HTTPClient(10 * time.Second)
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}

// generateNotificationLogID generates a unique ID for a notification dispatch encoding month and siteID.
// Format: <month>_<siteID>_<hash> (e.g., 2026-09_mysite_a1b2c3d4e5f67890).
func generateNotificationLogID(siteID, userID, endpoint string, ts time.Time) string {
	month := ts.UTC().Format("2006-01")
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%s:%d", siteID, userID, endpoint, ts.UnixNano()))
	return fmt.Sprintf("%s_%s_%s", month, siteID, hex.EncodeToString(h[:8]))
}

// parseNotificationLogID extracts month, siteID, and hash from a structured notification ID (<month>_<siteID>_<hash>).
func parseNotificationLogID(id string) (month, siteID, hash string, err error) {
	// Minimum length: 7 (YYYY-MM) + 1 (_) + 1 (siteID) + 1 (_) + 16 (hash) = 26
	if len(id) < 26 || id[7] != '_' {
		return "", "", "", fmt.Errorf("invalid notification ID structure: %s", id)
	}
	month = id[:7]
	if month[4] != '-' {
		return "", "", "", fmt.Errorf("invalid month in notification ID: %s", id)
	}
	lastUnderscore := strings.LastIndex(id, "_")
	if lastUnderscore <= 7 {
		return "", "", "", fmt.Errorf("invalid notification ID delimiters: %s", id)
	}
	siteID = id[8:lastUnderscore]
	hash = id[lastUnderscore+1:]
	if siteID == "" || hash == "" {
		return "", "", "", fmt.Errorf("missing siteID or hash in notification ID: %s", id)
	}
	return month, siteID, hash, nil
}

type siteRecentNotifications struct {
	logs []types.NotificationLog
}

func (n *siteRecentNotifications) hasSentToday(userID, notifType, dateStr string, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	for _, l := range n.logs {
		if l.UserID == userID && l.Type == notifType && (l.Success || l.Muted) {
			if l.TSCreated.In(loc).Format("2006-01-02") == dateStr {
				return true
			}
		}
	}
	return false
}

func (n *siteRecentNotifications) hasSentWithin(userID, notifType string, d time.Duration, now time.Time) bool {
	cutoff := now.Add(-d)
	for _, l := range n.logs {
		if l.UserID == userID && l.Type == notifType && (l.Success || l.Muted) {
			if l.TSCreated.After(cutoff) {
				return true
			}
		}
	}
	return false
}

// lastLog returns the most recent notification log matching the given criteria.
// If userID is non-empty, only logs for that user are returned.
// If onlyDelivered is true, non-delivered logs (!l.Success || l.Muted) are ignored.
// If onlyDelivered is false, valid event logs (delivered, muted, or internal audit suppression) are returned,
// while failed delivery attempts (!l.Success && !l.Muted && l.Error != "") are ignored.
// If notifTypes is provided, the log's type must match one of the specified types.
func (n *siteRecentNotifications) lastLog(userID string, onlyDelivered bool, notifTypes ...string) (types.NotificationLog, bool) {
	var latest time.Time
	var latestLog types.NotificationLog
	var found bool
	for _, l := range n.logs {
		if onlyDelivered {
			if !l.Success || l.Muted {
				continue
			}
		} else {
			if !l.Success && !l.Muted && l.Error != "" {
				continue
			}
		}
		if userID != "" && l.UserID != userID {
			continue
		}
		if len(notifTypes) > 0 && !slices.Contains(notifTypes, l.Type) {
			continue
		}
		if !found || l.TSCreated.After(latest) {
			latest = l.TSCreated
			latestLog = l
			found = true
		}
	}
	return latestLog, found
}

func extractHighestAlertedPrice(l *types.NotificationLog) float64 {
	if l == nil || len(l.Metadata) == 0 {
		return 0
	}
	var maxP float64
	if peakStr, ok := l.Metadata["peakPrice"]; ok && peakStr != "" {
		if p, err := strconv.ParseFloat(peakStr, 64); err == nil && p > maxP {
			maxP = p
		}
	}
	if priceStr, ok := l.Metadata["price"]; ok && priceStr != "" {
		if p, err := strconv.ParseFloat(priceStr, 64); err == nil && p > maxP {
			maxP = p
		}
	}
	return maxP
}

// priceDroppedBelowBetween checks if electricity prices dropped below spike threshold conditions
// at any point between the previous alert timestamp (start) and the start of the candidate spike (end).
//
// If prices dropped back to normal levels in between, the user is eligible for a re-alert
// after the 6-hour cooldown window. If prices remained continuously elevated without dropping below,
// repeated alerts for the same ongoing elevated price event are suppressed until the next day (24h).
func priceDroppedBelowBetween(
	histPrices []types.Price,
	start, end time.Time,
	rawCosts []float64,
	reqPercentile, minDelta float64,
	useTimeOfDayRelative bool,
	loc *time.Location,
) bool {
	if loc == nil {
		loc = time.UTC
	}
	pctVal := computePricePercentile(rawCosts, reqPercentile)
	medianCost := computePricePercentile(rawCosts, 0.50)
	foundAny := false
	for _, p := range histPrices {
		if p.TSStart.After(start) && p.TSStart.Before(end) {
			foundAny = true
			cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
			var ref float64
			if useTimeOfDayRelative {
				ref = computeTimeOfDayRefPrice(histPrices, p.TSStart, loc, medianCost)
			} else {
				ref = medianCost
			}
			// If at any hour the cost was below the absolute floor, below the percentile threshold,
			// or failed to exceed the reference price by the required delta, it is considered dropped below.
			if cost < priceSpikeAbsoluteFloorDollarsPerKWH || cost < pctVal || (cost-ref) < minDelta {
				return true
			}
		}
	}
	// If no prices were found in the window, default to true to allow alerting
	if !foundAny {
		return true
	}
	return false
}

// computePercentile returns the p-th percentile value (e.g. p=0.90 for 90th percentile)
// from a slice of values using nearest-rank selection over a sorted copy.
func computePercentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// computePricePercentile returns the p-th percentile value from a slice of prices.
func computePricePercentile(prices []float64, p float64) float64 {
	return computePercentile(prices, p)
}

// computeTimeOfDayRefPrice calculates the typical baseline price for a target time of day,
// using historical prices from previous days within a +/- 1 hour window.
//
// For example, if targetTime is 7:00 PM, this function inspects historical prices at 6:00 PM,
// 7:00 PM, and 8:00 PM across past days. The +/- 1 hour buffer accounts for slight shifts in peak
// hours, seasonal daylight changes, and Daylight Saving Time adjustments.
//
// To prevent adjacent lower shoulder hours (e.g. 4 PM before a 5 PM peak) from dragging down
// the peak baseline, we find the maximum price within that 3-hour window for each previous day,
// and then take the median (50th percentile) of those daily peaks:
//  1. Normal recurring daily peaks around this time form the baseline (so regular daily peaks don't alert).
//  2. An occasional past price spike on a single day does NOT inflate the baseline or poison
//     future alerts (unlike a single max, a spike day won't move the median of daily peaks).
//  3. If there are no historical prices in this window, it gracefully falls back to the provided fallback
//     (typically the all-hours median).
func computeTimeOfDayRefPrice(histPrices []types.Price, targetTime time.Time, loc *time.Location, fallback float64) float64 {
	if loc == nil {
		loc = time.UTC
	}
	targetLocal := targetTime.In(loc)
	targetHour := targetLocal.Hour()
	targetY, targetM, targetD := targetLocal.Date()

	// 3-hour window around targetHour (+/- 1 hour), with modulo 24 handling midnight wrap-around cleanly
	prevHour := (targetHour + 23) % 24
	nextHour := (targetHour + 1) % 24

	// Group prices in the 3-hour window by calendar date on previous days
	dailyWindowCosts := make(map[string][]float64)
	for _, p := range histPrices {
		pLocal := p.TSStart.In(loc)
		pY, pM, pD := pLocal.Date()

		// Only inspect previous days, never prices from the current target day
		if pY == targetY && pM == targetM && pD == targetD {
			continue
		}

		h := pLocal.Hour()
		if h == prevHour || h == targetHour || h == nextHour {
			cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
			dateKey := fmt.Sprintf("%04d-%02d-%02d", pY, pM, pD)
			dailyWindowCosts[dateKey] = append(dailyWindowCosts[dateKey], cost)
		}
	}

	if len(dailyWindowCosts) == 0 {
		return fallback
	}

	// For each previous day, find the highest price within the 3-hour window.
	// This captures the true daily peak for this time of day without dilution from adjacent shoulder hours.
	var dailyPeaks []float64
	for _, costs := range dailyWindowCosts {
		maxCost := costs[0]
		for _, c := range costs[1:] {
			if c > maxCost {
				maxCost = c
			}
		}
		dailyPeaks = append(dailyPeaks, maxCost)
	}

	// Use median of the daily peaks as the baseline for this time of day
	return computePricePercentile(dailyPeaks, 0.50)
}

// notificationTag returns the push notification tag for a given notification type and site.
// Solar underproduction, grid outages, grid restored, and daily summaries share the primary
// tag ("raterudder-" + siteID) so that alerts overwrite summaries on the user's device when
// conditions change. Price spike, VPP dispatch, and high home load alerts use distinct tags so
// they do not overwrite summaries or each other.
func notificationTag(notifType string, siteID string) string {
	switch notifType {
	case types.NotificationTypePriceSpike:
		return "raterudder-" + siteID + "-price-spike"
	case types.NotificationTypeVPPDispatch:
		return "raterudder-" + siteID + "-vpp"
	case types.NotificationTypeHighHomeLoad:
		return "raterudder-" + siteID + "-high-home-load"
	default:
		return "raterudder-" + siteID
	}
}

// dispatchPushToUser sends a push notification to all subscriptions of a user, handles dead subscription pruning, and logs.
func (s *Server) dispatchPushToUser(
	ctx context.Context,
	siteID string,
	userID string,
	notifType string,
	flavor string,
	title string,
	body string,
	urlPath string,
	metadata map[string]string,
	getUser userFetcher,
) {
	user, err := getUser(ctx, userID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get user for push notification",
			slog.String("userID", userID),
			slog.String("type", notifType),
			slog.Any("error", err),
		)
		return
	}
	if len(user.Subscriptions) == 0 {
		return
	}

	nowUTC := s.now().UTC()
	for _, sub := range user.Subscriptions {
		logID := generateNotificationLogID(siteID, user.ID, sub.Endpoint, nowUTC)
		payload := pushPayload{
			Title: title,
			Body:  body,
			Tag:   notificationTag(notifType, siteID),
			Icon:  pushPayloadIcon(sub),
			Badge: "/badge_96.png",
			Data: map[string]any{
				"url":      urlPath,
				"id":       logID,
				"logID":    logID,
				"metadata": metadata,
			},
		}

		statusCode, err := s.sendWebPush(ctx, sub, payload, 14400, "normal")
		success := err == nil && (statusCode >= 200 && statusCode < 300)

		var errStr string
		if err != nil {
			errStr = err.Error()
		}

		// Prune dead subscriptions on 404 or 410
		if statusCode == http.StatusNotFound || statusCode == http.StatusGone {
			log.Ctx(ctx).InfoContext(ctx, "pruning dead push subscription",
				slog.String("userID", user.ID),
				slog.String("endpoint", sub.Endpoint),
				slog.Int("statusCode", statusCode),
			)
			if rmErr := s.storage.RemoveUserPushSubscription(ctx, user.ID, sub.Endpoint); rmErr != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to remove dead push subscription", slog.Any("error", rmErr))
			}
		}

		log.Ctx(ctx).InfoContext(ctx, "sent push notification",
			slog.String("userID", user.ID),
			slog.String("logID", logID),
			slog.String("type", notifType),
			slog.String("flavor", flavor),
			slog.String("title", title),
			slog.Int("statusCode", statusCode),
		)

		// Append log to monthly document
		logEntry := types.NotificationLog{
			ID:         logID,
			TSCreated:  nowUTC,
			UserID:     user.ID,
			Type:       notifType,
			Flavor:     flavor,
			Title:      title,
			Body:       body,
			Success:    success,
			StatusCode: statusCode,
			Error:      errStr,
			Metadata:   metadata,
		}

		if appendErr := s.storage.AppendNotificationLog(ctx, siteID, logEntry); appendErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to append notification log",
				slog.String("userID", user.ID),
				slog.String("type", notifType),
				slog.Any("error", appendErr),
			)
		}
	}
}

// logMutedNotification records an audit log entry with Muted: true when an alert condition
// is triggered during a user's quiet period, without dispatching a WebPush notification.
func (s *Server) logMutedNotification(
	ctx context.Context,
	siteID string,
	userID string,
	notifType string,
	flavor string,
	title string,
	body string,
	metadata map[string]string,
) {
	nowUTC := s.now().UTC()
	logID := generateNotificationLogID(siteID, userID, "muted", nowUTC)
	logEntry := types.NotificationLog{
		ID:        logID,
		TSCreated: nowUTC,
		UserID:    userID,
		Type:      notifType,
		Flavor:    flavor,
		Title:     title,
		Body:      body,
		Success:   false,
		Muted:     true,
		Metadata:  metadata,
	}

	log.Ctx(ctx).InfoContext(ctx, "muted push notification due to quiet period",
		slog.String("userID", userID),
		slog.String("logID", logID),
		slog.String("type", notifType),
		slog.String("flavor", flavor),
		slog.String("title", title),
	)

	if appendErr := s.storage.AppendNotificationLog(ctx, siteID, logEntry); appendErr != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to append muted notification log",
			slog.String("userID", userID),
			slog.String("type", notifType),
			slog.Any("error", appendErr),
		)
	}
}

// getSiteRecentNotifications returns recent notification logs for the past 3 days wrapped in siteRecentNotifications.
func (s *Server) getSiteRecentNotifications(ctx context.Context, siteID string, nowLocal time.Time) *siteRecentNotifications {
	logs, err := s.storage.GetNotificationLogs(ctx, siteID, nowLocal.AddDate(0, 0, -3), nowLocal.Add(1*time.Hour))
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get recent notification logs", slog.Any("error", err))
	}
	return &siteRecentNotifications{logs: logs}
}

// newSiteRecentNotificationsFetcher returns a lazy fetcher that retrieves and caches recent notification logs on first call.
func (s *Server) newSiteRecentNotificationsFetcher(ctx context.Context, siteID string, nowLocal time.Time) func() *siteRecentNotifications {
	var (
		mu      sync.Mutex
		fetched bool
		state   *siteRecentNotifications
	)
	return func() *siteRecentNotifications {
		mu.Lock()
		defer mu.Unlock()
		if !fetched {
			state = s.getSiteRecentNotifications(ctx, siteID, nowLocal)
			fetched = true
		}
		return state
	}
}

// userFetcher retrieves a user by ID with thread-safe caching.
type userFetcher func(ctx context.Context, userID string) (types.User, error)

// newCachedUserFetcher returns a thread-safe helper that fetches and caches users by ID for the duration of a notification cycle.
func (s *Server) newCachedUserFetcher() userFetcher {
	var mu sync.RWMutex
	cache := make(map[string]types.User)
	return func(ctx context.Context, userID string) (types.User, error) {
		mu.RLock()
		u, found := cache[userID]
		mu.RUnlock()
		if found {
			return u, nil
		}

		mu.Lock()
		defer mu.Unlock()
		if u, found := cache[userID]; found {
			return u, nil
		}
		user, err := s.storage.GetUser(ctx, userID)
		if err != nil {
			return types.User{}, err
		}
		cache[userID] = user
		return user, nil
	}
}

type notificationPlanHelper struct {
	plan *types.Plan
}

func newNotificationPlanHelper(plan *types.Plan) notificationPlanHelper {
	return notificationPlanHelper{plan: plan}
}

// todaySolarForecastKWH sums forecasted solar generation for periods starting within [todayStart, todayEnd).
func (h notificationPlanHelper) todaySolarForecastKWH(todayStart, todayEnd time.Time) float64 {
	if h.plan == nil {
		return 0
	}
	var total float64
	for _, p := range h.plan.Periods {
		if !p.TSStart.Before(todayStart) && p.TSStart.Before(todayEnd) {
			total += p.SolarKWH
		}
	}
	return total
}

// todayPeakSOC returns the maximum projected battery SOC during today's periods.
func (h notificationPlanHelper) todayPeakSOC(todayStart, todayEnd time.Time, currentSOC float64) float64 {
	peak := currentSOC
	if h.plan == nil {
		return peak
	}
	for _, p := range h.plan.Periods {
		if !p.TSStart.Before(todayStart) && p.TSStart.Before(todayEnd) {
			if p.StartSOC > peak {
				peak = p.StartSOC
			}
			if p.EndSOC > peak {
				peak = p.EndSOC
			}
		}
	}
	return peak
}

// batteryCapacityETA finds the discrete end time of the first period reaching full capacity (>= 99%) after nowLocal.
func (h notificationPlanHelper) batteryCapacityETA(nowLocal, todayEnd time.Time) (time.Time, bool) {
	if h.plan == nil {
		return time.Time{}, false
	}
	for _, p := range h.plan.Periods {
		if p.TSEnd.After(nowLocal) && p.TSStart.Before(todayEnd) {
			if p.EndSOC >= 99.0 {
				return p.TSEnd, true
			}
		}
	}
	return time.Time{}, false
}

type overnightPlanOutcome struct {
	scheduledChargeAt time.Time
	reachesReserveAt  time.Time
	lastsUntilSunrise bool
	allStandby        bool
}

func (h notificationPlanHelper) overnightPlanOutcome(
	ctx context.Context,
	nowLocal time.Time,
	siteLoc *time.Location,
	currentSOC, minSOC float64,
) overnightPlanOutcome {
	var outcome overnightPlanOutcome
	if h.plan == nil || len(h.plan.Periods) == 0 {
		return outcome
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if siteLoc == nil {
		siteLoc = time.UTC
	}

	if currentSOC <= minSOC {
		outcome.reachesReserveAt = nowLocal
		log.Ctx(ctx).DebugContext(ctx, "battery already at or below reserve for overnight plan",
			slog.Time("nowLocal", nowLocal),
			slog.Float64("currentSOC", currentSOC),
			slog.Float64("minSOC", minSOC),
		)
		return outcome
	}

	tomorrowDay := nowLocal.AddDate(0, 0, 1).Day()
	var tomorrowSolarStart time.Time
	for _, p := range h.plan.Periods {
		if p.TSStart.In(siteLoc).Day() == tomorrowDay && p.SolarKWH > 0.2 {
			tomorrowSolarStart = p.TSStart
			break
		}
	}

	cutoff := tomorrowSolarStart
	if cutoff.IsZero() {
		cutoff = time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 12, 0, 0, 0, siteLoc).AddDate(0, 0, 1)
	}

	var overnightPeriods []types.PlanPeriod
	for _, p := range h.plan.Periods {
		if p.TSEnd.After(nowLocal) && p.TSStart.Before(cutoff) {
			overnightPeriods = append(overnightPeriods, p)
		}
	}

	if len(overnightPeriods) == 0 {
		log.Ctx(ctx).DebugContext(ctx, "no overnight plan periods found",
			slog.Time("nowLocal", nowLocal),
			slog.Time("cutoff", cutoff),
			slog.Time("tomorrowSolarStart", tomorrowSolarStart),
			slog.Int("totalPlanPeriods", len(h.plan.Periods)),
		)
		return outcome
	}

	// 1. Check for scheduled grid charging overnight
	for _, p := range overnightPeriods {
		if p.BatteryMode == types.BatteryModeChargeAny {
			outcome.scheduledChargeAt = p.TSStart
			log.Ctx(ctx).DebugContext(ctx, "scheduled overnight grid charge detected in plan",
				slog.Time("scheduledChargeAt", p.TSStart),
				slog.Float64("currentSOC", currentSOC),
				slog.Float64("minSOC", minSOC),
			)
			return outcome
		}
	}

	// 2. Check if all periods are Standby
	allStandby := true
	for _, p := range overnightPeriods {
		if p.BatteryMode != types.BatteryModeStandby {
			allStandby = false
			break
		}
	}
	outcome.allStandby = allStandby

	// 3. Check if battery reaches reserve before sunrise
	for _, p := range overnightPeriods {
		resSOC := p.ReserveSOC
		if resSOC <= 0 {
			resSOC = minSOC
		}
		if p.EndSOC <= resSOC {
			outcome.reachesReserveAt = p.TSEnd
			log.Ctx(ctx).DebugContext(ctx, "battery reaches reserve overnight",
				slog.Time("reachesReserveAt", p.TSEnd),
				slog.Float64("endSOC", p.EndSOC),
				slog.Float64("reserveSOC", resSOC),
				slog.Float64("currentSOC", currentSOC),
			)
			return outcome
		}
	}

	// 4. Lasts through sunrise
	outcome.lastsUntilSunrise = true
	var minOvernightSOC float64 = currentSOC
	var sunriseSOC float64 = currentSOC
	if len(overnightPeriods) > 0 {
		sunriseSOC = overnightPeriods[len(overnightPeriods)-1].EndSOC
		for _, p := range overnightPeriods {
			if p.EndSOC < minOvernightSOC {
				minOvernightSOC = p.EndSOC
			}
		}
	}
	log.Ctx(ctx).DebugContext(ctx, "battery projected to last through sunrise",
		slog.Time("nowLocal", nowLocal),
		slog.Time("cutoff", cutoff),
		slog.Time("tomorrowSolarStart", tomorrowSolarStart),
		slog.Float64("currentSOC", currentSOC),
		slog.Float64("minSOC", minSOC),
		slog.Float64("minOvernightSOC", minOvernightSOC),
		slog.Float64("sunriseSOC", sunriseSOC),
		slog.Bool("allStandby", allStandby),
		slog.Int("overnightPeriodsCount", len(overnightPeriods)),
	)
	return outcome
}

// currentSolarForecastKW returns the forecasted solar generation rate in kW for the interval covering nowLocal.
func (h notificationPlanHelper) currentSolarForecastKW(nowLocal time.Time) (float64, bool) {
	if h.plan == nil {
		return 0, false
	}
	for _, p := range h.plan.Periods {
		if !nowLocal.Before(p.TSStart) && nowLocal.Before(p.TSEnd) {
			if p.DurationHours > 0 {
				return p.SolarKWH / p.DurationHours, true
			}
			return p.SolarKWH, true
		}
	}
	return 0, false
}

type priceSpikePlanOutcome struct {
	solarCovers      bool
	isExporting      bool
	reachesReserveAt time.Time
	lastsEntireSpike bool
}

func (h notificationPlanHelper) priceSpikeGuidance(
	ctx context.Context,
	spikeStart, spikeEnd time.Time,
	currentSOC, reserveSOC float64,
) priceSpikePlanOutcome {
	var outcome priceSpikePlanOutcome
	if h.plan == nil || len(h.plan.Periods) == 0 {
		return outcome
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var spikePeriods []types.PlanPeriod
	for _, p := range h.plan.Periods {
		if p.TSEnd.After(spikeStart) && p.TSStart.Before(spikeEnd) {
			spikePeriods = append(spikePeriods, p)
		}
	}
	if len(spikePeriods) == 0 {
		log.Ctx(ctx).DebugContext(ctx, "no plan periods found overlapping price spike",
			slog.Time("spikeStart", spikeStart),
			slog.Time("spikeEnd", spikeEnd),
			slog.Int("totalPlanPeriods", len(h.plan.Periods)),
		)
		return outcome
	}

	// 1. Solar coverage check
	var totalSolar float64
	solarCoversAll := true
	for _, p := range spikePeriods {
		totalSolar += p.SolarKWH
		if p.SolarKWH < p.LoadKWH && p.GridImportKWH > 0.05 {
			solarCoversAll = false
		}
	}
	if totalSolar >= 0.5 && solarCoversAll {
		outcome.solarCovers = true
		log.Ctx(ctx).DebugContext(ctx, "solar covers home load during price spike",
			slog.Time("spikeStart", spikeStart),
			slog.Time("spikeEnd", spikeEnd),
			slog.Float64("totalSolarKWH", totalSolar),
			slog.Int("spikePeriodsCount", len(spikePeriods)),
		)
		return outcome
	}

	// 2. Export arbitrage check
	for _, p := range spikePeriods {
		if p.BatteryMode == types.BatteryModeExport {
			outcome.isExporting = true
			log.Ctx(ctx).DebugContext(ctx, "battery export arbitrage scheduled during price spike",
				slog.Time("spikeStart", spikeStart),
				slog.Time("spikeEnd", spikeEnd),
				slog.Float64("currentSOC", currentSOC),
			)
			return outcome
		}
	}

	// 3. Battery endurance
	for _, p := range spikePeriods {
		res := p.ReserveSOC
		if res <= 0 {
			res = reserveSOC
		}
		if p.EndSOC <= res {
			outcome.reachesReserveAt = p.TSEnd
			log.Ctx(ctx).DebugContext(ctx, "battery reaches reserve during price spike",
				slog.Time("spikeStart", spikeStart),
				slog.Time("spikeEnd", spikeEnd),
				slog.Time("reachesReserveAt", p.TSEnd),
				slog.Float64("endSOC", p.EndSOC),
				slog.Float64("reserveSOC", res),
				slog.Float64("currentSOC", currentSOC),
			)
			return outcome
		}
	}

	outcome.lastsEntireSpike = true
	var minSpikeSOC float64 = currentSOC
	var finalSOC float64 = currentSOC
	if len(spikePeriods) > 0 {
		finalSOC = spikePeriods[len(spikePeriods)-1].EndSOC
		for _, p := range spikePeriods {
			if p.EndSOC < minSpikeSOC {
				minSpikeSOC = p.EndSOC
			}
		}
	}
	log.Ctx(ctx).DebugContext(ctx, "battery projected to last through price spike",
		slog.Time("spikeStart", spikeStart),
		slog.Time("spikeEnd", spikeEnd),
		slog.Float64("currentSOC", currentSOC),
		slog.Float64("reserveSOC", reserveSOC),
		slog.Float64("minSpikeSOC", minSpikeSOC),
		slog.Float64("finalSOC", finalSOC),
		slog.Int("spikePeriodsCount", len(spikePeriods)),
	)
	return outcome
}

type dataForNotifications struct {
	settings       types.Settings
	essSystem      ess.System
	currentPrice   types.Price
	status         types.SystemStatus
	vppInfo        types.UtilityVPPInfo
	energyHistory  []types.DailyEnergyStats
	weatherHistory []types.Weather
	futurePrices   []types.Price
	plan           *types.Plan
}

// handleNotifications evaluates and dispatches any due notifications during the site update cycle.
func (s *Server) handleNotifications(
	ctx context.Context,
	siteID string,
	data *dataForNotifications,
) {
	if !s.notificationsEnabled() {
		return
	}

	if data == nil || len(data.settings.Notifications) == 0 {
		return
	}
	notifications := data.settings.Notifications

	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	nowLocal := s.now().In(siteLoc)

	wg := new(sync.WaitGroup)

	getNotifState := s.newSiteRecentNotificationsFetcher(ctx, siteID, nowLocal)
	getUser := s.newCachedUserFetcher()

	// Gating: notifications require active PlanMode (or staging).
	isStaging := strings.EqualFold(s.release, "staging")
	if !data.settings.PlanMode && !isStaging {
		return
	}

	// If a severe grid event or emergency occurs, the inverter is in off-grid backup
	// and forward optimization planning is skipped. Still evaluate hardware outage alerts.
	if data.status.GridUnavailable || data.status.EmergencyMode {
		s.handleGridOutageNotifications(ctx, siteID, notifications, data.status, data.essSystem, nowLocal, getNotifState, getUser)
		return
	}

	// All remaining forecast-based notifications require a valid generated Plan.
	if data.plan == nil || len(data.plan.Periods) == 0 {
		log.Ctx(ctx).ErrorContext(ctx, "skipping forecast notifications: missing plan",
			slog.String("siteID", siteID),
		)
		return
	}

	// 1. Morning Summary
	wg.Go(func() {
		s.handleMorningSummaryNotifications(ctx, siteID, notifications, data, nowLocal, getNotifState, getUser)
	})

	// 2. Evening Summary
	wg.Go(func() {
		s.handleEveningSummaryNotifications(ctx, siteID, notifications, data, nowLocal, getNotifState, getUser)
	})

	// 3. Grid Restoration & Outage
	wg.Go(func() {
		s.handleGridOutageNotifications(ctx, siteID, notifications, data.status, data.essSystem, nowLocal, getNotifState, getUser)
	})

	// 4. Real-Time Price Spike
	wg.Go(func() {
		s.handlePriceSpikeNotifications(ctx, siteID, notifications, data, nowLocal, getNotifState, getUser)
	})

	// 5. Unexpected Solar Underproduction
	wg.Go(func() {
		s.handleSolarUnderproductionNotifications(ctx, siteID, notifications, data, nowLocal, getNotifState, getUser)
	})

	// 6. Unplanned VPP Dispatch
	wg.Go(func() {
		s.handleVPPDispatchNotifications(ctx, siteID, notifications, data.status, data.vppInfo, nowLocal, getNotifState, getUser)
	})

	// 7. Large Unusual Home Load
	wg.Go(func() {
		s.handleHighHomeLoadNotifications(ctx, siteID, notifications, data, nowLocal, getNotifState, getUser)
	})

	wg.Wait()
}

// handleMorningSummaryNotifications evaluates and sends morning summary push notifications.
// It summarizes the day's solar production outlook, projected battery charging milestones (e.g.
// full battery ETA), and provides tailored advice based on the user's chosen flavor.
func (s *Server) handleMorningSummaryNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	todayDateStr := nowLocal.Format("2006-01-02")
	todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, siteLoc)
	todayEnd := todayStart.AddDate(0, 0, 1)

	// Calculate historical daily solar totals for days strictly before today.
	// Used to determine the historical peak solar generation day and yesterday's actual solar generation.
	dailySolarMap := make(map[string]float64)
	for _, day := range data.energyHistory {
		if !day.TSDayStart.IsZero() {
			dayStartLocal := day.TSDayStart.In(siteLoc)
			if dayStartLocal.Before(todayStart) {
				dayKey := dayStartLocal.Format("2006-01-02")
				if _, ok := dailySolarMap[dayKey]; !ok {
					dailySolarMap[dayKey] = 0
				}
			}
		}
		for _, h := range day.Hourly {
			tHour := h.TSHourStart.In(siteLoc)
			if tHour.Before(todayStart) {
				dayKey := tHour.Format("2006-01-02")
				dailySolarMap[dayKey] += h.SolarKWH
			}
		}
	}

	// Cold-start rule: if fewer than 3 days of historical data exist, do not generate morning summaries
	// because baseline solar ratios and yesterday comparisons cannot be reliably computed.
	if len(dailySolarMap) < 3 {
		return
	}

	var peakSolarKWH float64
	for _, kwh := range dailySolarMap {
		if kwh > peakSolarKWH {
			peakSolarKWH = kwh
		}
	}

	// Iterate through all users configured for this site and dispatch their morning summary
	for userID, notifConfig := range notifications {
		if !notifConfig.MorningSummaryEnabled || nowLocal.Hour() != notifConfig.MorningSummaryHour {
			continue
		}
		// Ensure only one morning summary is delivered per user per calendar day
		if getNotifState().hasSentToday(userID, types.NotificationTypeMorningSummary, todayDateStr, siteLoc) {
			continue
		}

		// Retrieve plan projection data for today
		planHelper := newNotificationPlanHelper(data.plan)
		todayForecastKWH := planHelper.todaySolarForecastKWH(todayStart, todayEnd)
		maxSimSOC := planHelper.todayPeakSOC(todayStart, todayEnd, data.status.BatterySOC)
		hitCapacityAt, hasHitCapacityAt := planHelper.batteryCapacityETA(nowLocal, todayEnd)

		yesterdayStart := todayStart.AddDate(0, 0, -1)
		yesterdayActualKWH := dailySolarMap[yesterdayStart.Format("2006-01-02")]

		solarRatio := 0.0
		if peakSolarKWH > 0 {
			solarRatio = todayForecastKWH / peakSolarKWH
		}

		// Populate structured metadata for client-side rendering and logging analysis
		metadata := map[string]string{
			"currentSOC":        fmt.Sprintf("%.1f", data.status.BatterySOC),
			"peakSOC":           fmt.Sprintf("%.1f", maxSimSOC),
			"solarRatio":        fmt.Sprintf("%.2f", solarRatio),
			"forecastSolarKWH":  fmt.Sprintf("%.2f", todayForecastKWH),
			"yesterdaySolarKWH": fmt.Sprintf("%.2f", yesterdayActualKWH),
		}
		if hasHitCapacityAt {
			metadata["hitCapacityAt"] = hitCapacityAt.In(siteLoc).Format(time.RFC3339)
		}

		title, body := generateMorningSummary(ctx, notifConfig.MorningSummaryFlavor, data.status, todayForecastKWH, yesterdayActualKWH, peakSolarKWH, hitCapacityAt, maxSimSOC, siteLoc)
		s.dispatchPushToUser(
			ctx,
			siteID,
			userID,
			types.NotificationTypeMorningSummary,
			notifConfig.MorningSummaryFlavor,
			title,
			body,
			"/forecast",
			metadata,
			getUser,
		)
	}
}

// handleEveningSummaryNotifications evaluates and sends evening summary push notifications.
// It summarizes today's actual performance (solar generation, home usage, grid import/export)
// and projects whether the battery will supply the home through the night or hit reserve.
func (s *Server) handleEveningSummaryNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	todayDateStr := nowLocal.Format("2006-01-02")

	for userID, notifConfig := range notifications {
		if !notifConfig.EveningSummaryEnabled || nowLocal.Hour() != notifConfig.EveningSummaryHour {
			continue
		}
		// Ensure only one evening summary is delivered per user per calendar day
		if getNotifState().hasSentToday(userID, types.NotificationTypeEveningSummary, todayDateStr, siteLoc) {
			continue
		}

		minSOC := data.settings.GetMinBatterySOC(ctx, nowLocal, siteLoc, data.currentPrice)

		planHelper := newNotificationPlanHelper(data.plan)
		outcome := planHelper.overnightPlanOutcome(ctx, nowLocal, siteLoc, data.status.BatterySOC, minSOC)
		hitDeficitAt := outcome.reachesReserveAt
		scheduledChargeAt := outcome.scheduledChargeAt

		// Aggregate today's energy metrics from midnight up to the current hour
		todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, siteLoc)
		todayEnd := todayStart.AddDate(0, 0, 1)
		var todayActualSolarKWH, todayHomeUsageKWH, todayGridImportKWH, todayGridExportKWH float64
		for _, day := range data.energyHistory {
			for _, h := range day.Hourly {
				tHour := h.TSHourStart.In(siteLoc)
				if !tHour.Before(todayStart) && tHour.Before(todayEnd) {
					todayActualSolarKWH += h.SolarKWH
					todayHomeUsageKWH += h.HomeKWH
					todayGridImportKWH += h.GridImportKWH
					todayGridExportKWH += h.GridExportKWH
				}
			}
		}

		// Populate structured metadata for evening summary
		metadata := map[string]string{
			"currentSOC":         fmt.Sprintf("%.1f", data.status.BatterySOC),
			"todaySolarKWH":      fmt.Sprintf("%.2f", todayActualSolarKWH),
			"todayHomeUsageKWH":  fmt.Sprintf("%.2f", todayHomeUsageKWH),
			"todayGridImportKWH": fmt.Sprintf("%.2f", todayGridImportKWH),
			"todayGridExportKWH": fmt.Sprintf("%.2f", todayGridExportKWH),
		}
		if !hitDeficitAt.IsZero() {
			metadata["hitDeficitAt"] = hitDeficitAt.In(siteLoc).Format(time.RFC3339)
		}
		if !scheduledChargeAt.IsZero() {
			metadata["scheduledChargeAt"] = scheduledChargeAt.In(siteLoc).Format(time.RFC3339)
		}

		title, body := generateEveningSummary(ctx, notifConfig.EveningSummaryFlavor, data.status, todayActualSolarKWH, todayHomeUsageKWH, todayGridExportKWH, todayGridImportKWH, minSOC, hitDeficitAt, scheduledChargeAt, siteLoc)
		s.dispatchPushToUser(
			ctx,
			siteID,
			userID,
			types.NotificationTypeEveningSummary,
			notifConfig.EveningSummaryFlavor,
			title,
			body,
			"/dashboard",
			metadata,
			getUser,
		)
	}
}

// handleGridOutageNotifications evaluates and sends grid outage and restoration notifications.
func (s *Server) handleGridOutageNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	status types.SystemStatus,
	essSystem ess.System,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	hasAnyGridOutageUser := false
	for _, notifConfig := range notifications {
		if notifConfig.RealTimeAlertEnabled(notifConfig.GridOutageAlert) {
			hasAnyGridOutageUser = true
			break
		}
	}
	if !hasAnyGridOutageUser {
		return
	}
	if nowLocal.IsZero() {
		nowLocal = s.now()
	}

	// TODO: When the grid is connected and operating normally, this evaluation queries recent notification
	// logs on every routine cycle to detect transitions from an active outage to power restoration.
	// Energy history (hourly import/export totals) cannot reliably indicate whether the grid was down,
	// because homes with solar and storage frequently operate at zero import and zero export while grid-tied.
	// To avoid querying notification logs on every normal-grid cycle, consider tracking recent grid state
	// transitions in-memory on the server or persisting a lightweight grid status / transition timestamp
	// on the site record, so getNotifState() is only called when an actual transition from unavailable
	// to available has occurred.
	if !status.GridUnavailable {
		for userID, notifConfig := range notifications {
			if !notifConfig.RealTimeAlertEnabled(notifConfig.GridOutageAlert) {
				continue
			}
			lastLog, ok := getNotifState().lastLog(userID, false, types.NotificationTypeGridOutage, types.NotificationTypeGridRestored)
			if !ok {
				continue
			}

			title := "✅ Grid Power Restored"
			body := "The electric grid is back online. Your system has safely resumed normal grid-tied operation."
			metadata := map[string]string{
				"currentSOC": fmt.Sprintf("%.1f", status.BatterySOC),
			}

			// Case 1: Grid just transitioned from an active outage to restored.
			if lastLog.Type == types.NotificationTypeGridOutage {
				if notifConfig.IsInQuietPeriod(nowLocal) {
					// Power returned during quiet hours; mute the alert and record an audit log.
					s.logMutedNotification(ctx, siteID, userID, types.NotificationTypeGridRestored, "", title, body, metadata)
					continue
				}

				log.Ctx(ctx).DebugContext(ctx, "sending grid restored notification",
					slog.String("userID", userID),
					slog.Float64("batterySOC", status.BatterySOC),
					slog.String("title", title),
					slog.String("body", body),
				)
				s.dispatchPushToUser(
					ctx,
					siteID,
					userID,
					types.NotificationTypeGridRestored,
					"",
					title,
					body,
					"/dashboard",
					metadata,
					getUser,
				)
				continue
			}

			// Case 2: Grid was restored during quiet hours while the user slept.
			// The immediate notification was muted, leaving a log entry with Type == GridRestored and Muted == true.
			// We check if it was muted last time because now that quiet hours have ended (or upon wakeup evaluation),
			// we need to know if there is an unacknowledged overnight power restoration to inform the user about.
			// If power returned recently (<= 1 hour ago), we deliver a deferred notification upon wakeup.
			// If power returned several hours ago (> 1 hour ago), the event is stale—the user already woke up
			// seeing lights and appliances on—so we suppress it without sending a push notification or cluttering
			// storage with fake unsuccessful logs.
			if lastLog.Type == types.NotificationTypeGridRestored && lastLog.Muted {
				if notifConfig.IsInQuietPeriod(nowLocal) {
					continue
				}
				restoredAge := nowLocal.Sub(lastLog.TSCreated)
				if restoredAge <= maxDeferredGridRestoredAge {
					log.Ctx(ctx).InfoContext(ctx, "sending deferred grid restored notification after quiet period",
						slog.String("userID", userID),
						slog.Duration("restoredAge", restoredAge),
					)
					s.dispatchPushToUser(
						ctx,
						siteID,
						userID,
						types.NotificationTypeGridRestored,
						"",
						title,
						body,
						"/dashboard",
						metadata,
						getUser,
					)
				} else {
					log.Ctx(ctx).InfoContext(ctx, "suppressing stale deferred grid restored notification (> 1h)",
						slog.String("userID", userID),
						slog.Duration("restoredAge", restoredAge),
					)
				}
			}
		}
		return
	}

	if status.GridUnavailable && essSystem != nil {
		var outageUserIDs []string
		var mutedOutageUserIDs []string
		for userID, notifConfig := range notifications {
			if !notifConfig.RealTimeAlertEnabled(notifConfig.GridOutageAlert) {
				continue
			}

			lastLog, ok := getNotifState().lastLog(userID, false, types.NotificationTypeGridOutage, types.NotificationTypeGridRestored)
			if ok && lastLog.Type == types.NotificationTypeGridOutage {
				if !lastLog.Muted {
					log.Ctx(ctx).DebugContext(ctx, "skipping grid outage check: already alerted for outage", slog.String("userID", userID))
					continue
				}
				if notifConfig.IsInQuietPeriod(nowLocal) {
					continue
				}
			}

			if notifConfig.IsInQuietPeriod(nowLocal) {
				mutedOutageUserIDs = append(mutedOutageUserIDs, userID)
				continue
			}

			user, err := getUser(ctx, userID)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get user for grid outage notification",
					slog.String("userID", userID),
					slog.Any("error", err),
				)
				continue
			}

			if len(user.Subscriptions) > 0 {
				outageUserIDs = append(outageUserIDs, userID)
			}
		}

		if len(outageUserIDs) == 0 && len(mutedOutageUserIDs) == 0 {
			return
		}

		if wg := common.CtxWaitGroup(ctx); wg != nil {
			wg.Add(1)
			delay := s.gridOutageDelay
			if delay == 0 {
				delay = defaultGridOutageDelay
			}
			log.Ctx(ctx).DebugContext(ctx, "grid unavailable detected, launching outage verification",
				slog.Duration("delay", delay),
			)
			go func(ctx context.Context) {
				defer wg.Done()
				asyncCtx, cancel := context.WithTimeout(ctx, delay+30*time.Second)
				defer cancel()

				select {
				case <-time.After(delay):
				case <-asyncCtx.Done():
					return
				}

				currStatus, err := essSystem.GetStatus(asyncCtx)
				if err != nil {
					log.Ctx(asyncCtx).WarnContext(asyncCtx, "failed to re-check grid status after outage delay",
						slog.Any("error", err),
					)
					return
				}

				log.Ctx(asyncCtx).DebugContext(asyncCtx, "grid outage verification result",
					slog.Bool("stillDown", currStatus.GridUnavailable),
					slog.Float64("batterySOC", currStatus.BatterySOC),
				)

				if currStatus.GridUnavailable {
					hrsRemainingStr := ""
					if currStatus.HomeKW > 0.1 && currStatus.BatteryCapacityKWH > 0 {
						availKWH := currStatus.BatteryCapacityKWH * (currStatus.BatterySOC / 100.0)
						hrs := availKWH / currStatus.HomeKW
						hrsRemainingStr = fmt.Sprintf(" (~%.1f hours remaining)", hrs)
					}
					title := "⚠️ Grid Outage Detected"
					body := fmt.Sprintf("Utility grid power is currently down. Battery reserve is at %.0f%%%s.", currStatus.BatterySOC, hrsRemainingStr)
					metadata := map[string]string{
						"currentSOC": fmt.Sprintf("%.1f", currStatus.BatterySOC),
						"homeKW":     fmt.Sprintf("%.2f", currStatus.HomeKW),
					}
					for _, uID := range outageUserIDs {
						log.Ctx(asyncCtx).DebugContext(asyncCtx, "sending grid outage notification",
							slog.String("userID", uID),
							slog.Float64("batterySOC", currStatus.BatterySOC),
							slog.Float64("batteryCapacityKWH", currStatus.BatteryCapacityKWH),
							slog.Float64("homeKW", currStatus.HomeKW),
							slog.String("title", title),
							slog.String("body", body),
						)
						s.dispatchPushToUser(
							asyncCtx,
							siteID,
							uID,
							types.NotificationTypeGridOutage,
							"",
							title,
							body,
							"/dashboard",
							metadata,
							getUser,
						)
					}
					for _, uID := range mutedOutageUserIDs {
						s.logMutedNotification(asyncCtx, siteID, uID, types.NotificationTypeGridOutage, "", title, body, metadata)
					}
				} else {
					log.Ctx(asyncCtx).InfoContext(asyncCtx, "grid outage was a temporary blip (<5m), suppressed notification")
				}
			}(ctx)
		}
	}
}

// handlePriceSpikeNotifications evaluates incoming current and forecasted electricity prices against
// historical baselines to send price spike push notifications to subscribed users.
//
// Key features:
// - Evaluates both active real-time prices and upcoming horizon forecasts.
// - Supports High, Medium, and Low sensitivity tiers:
//   - High: Triggers on top 10% of all hours without time-of-day restriction.
//   - Medium: Triggers on top 10% of all hours AND requires price to exceed the +/- 1h time-of-day baseline.
//   - Low: Triggers on top 5% of all hours AND requires price to exceed the +/- 1h time-of-day baseline.
//
// - Scans forward to detect contiguous duration and projected peak time/price.
// - Formats battery simulation outcome (solar coverage, battery capacity survival, reserve deficit ETA).
// - Enforces a tiered anti-flapping cooldown: 1-hour lockout, 1-6h surge threshold, and 6-24h drop-below check.
func (s *Server) handlePriceSpikeNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	if data == nil || (len(data.futurePrices) == 0 && data.currentPrice.TSStart.IsZero()) {
		return
	}

	// Calculate total unit electricity cost ($/kWh) including energy supply and grid delivery fees
	currentCost := data.currentPrice.DollarsPerKWH + data.currentPrice.GridUseDollarsPerKWH
	var nextPriceCost float64
	var hasFuturePrice bool
	if len(data.futurePrices) > 0 {
		hasFuturePrice = true
		nextPriceCost = data.futurePrices[0].DollarsPerKWH + data.futurePrices[0].GridUseDollarsPerKWH
	}

	// Fast rejection: If neither the current price nor the next future price meets the absolute price floor,
	// skip history fetching and heavy evaluations entirely.
	currentCouldSpike := !data.currentPrice.TSStart.IsZero() && currentCost >= priceSpikeAbsoluteFloorDollarsPerKWH
	futureCouldSpike := hasFuturePrice && nextPriceCost >= priceSpikeAbsoluteFloorDollarsPerKWH
	if !currentCouldSpike && !futureCouldSpike {
		return
	}

	// Determine site location / timezone for day-of-week and hour-of-day evaluations
	var siteLoc *time.Location
	if !data.currentPrice.TSStart.IsZero() {
		siteLoc = data.currentPrice.TSStart.Location()
	} else if len(data.futurePrices) > 0 && !data.futurePrices[0].TSStart.IsZero() {
		siteLoc = data.futurePrices[0].TSStart.Location()
	} else if !nowLocal.IsZero() {
		siteLoc = nowLocal.Location()
	}
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	// Filter users configured to receive price spike alerts for this site who are not in a quiet period.
	// Users in their quiet period are skipped early to avoid unnecessary history fetches, simulations,
	// user lookups, or writing useless muted logs to storage.
	var spikeUsers []struct {
		userID      string
		sensitivity string
	}
	for userID, notifConfig := range notifications {
		sensitivity := notifConfig.RealTimeAlertSensitivity(notifConfig.PriceSpikeAlert)
		if sensitivity == "" {
			continue
		}
		if notifConfig.IsInQuietPeriod(nowLocal) {
			continue
		}
		spikeUsers = append(spikeUsers, struct {
			userID      string
			sensitivity string
		}{userID: userID, sensitivity: sensitivity})
	}
	if len(spikeUsers) == 0 {
		return
	}

	// Fetch up to 5 days of recent price history to establish the baseline percentile distributions
	startHist := nowLocal.AddDate(0, 0, -5).UTC()
	endHist := nowLocal.UTC()
	histPrices, err := s.storage.GetPriceHistory(ctx, siteID, startHist, endHist)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get price history for price spike notification",
			slog.Any("error", err),
		)
		return
	}
	if len(histPrices) == 0 {
		return
	}

	// Compute raw total unit costs and overall median across all historical hours
	var rawCosts []float64
	for _, p := range histPrices {
		rawCosts = append(rawCosts, p.DollarsPerKWH+p.GridUseDollarsPerKWH)
	}
	medianCost := computePricePercentile(rawCosts, 0.50)

	for _, su := range spikeUsers {
		// Map user-selected sensitivity tier to spike detection parameters:
		// - "high": Top 10% (0.90) of all hours AND time-of-day relative (+/- 1 hour buffer).
		//   Alerts earlier on smaller rate increases ($0.03/kWh) above the time-of-day baseline.
		// - "medium": Top 10% (0.90) of all hours AND time-of-day relative (+/- 1 hour buffer).
		//   Requires the price to be in the top 10% of all hours AND significantly above the typical
		//   price for this time of day (+/- 1h) ($0.05/kWh), filtering out normal daily evening peaks.
		// - "low": Top 5% (0.95) of all hours AND time-of-day relative (+/- 1 hour buffer).
		//   Only alerts on severe surges in the top 5% that also exceed the time-of-day baseline ($0.10/kWh).
		var minDelta, reqPercentile, escalatedPercentile float64
		var useTimeOfDayRelative bool
		switch su.sensitivity {
		case "low":
			minDelta = priceSpikeMinDeltaLow
			reqPercentile = priceSpikePercentileLow
			escalatedPercentile = priceSpikeEscalatedPercentileLow
			useTimeOfDayRelative = true
		case "high":
			minDelta = priceSpikeMinDeltaHigh
			reqPercentile = priceSpikePercentileHigh
			escalatedPercentile = priceSpikeEscalatedPercentileHigh
			useTimeOfDayRelative = true
		case "medium":
			fallthrough
		default:
			minDelta = priceSpikeMinDeltaMedium
			reqPercentile = priceSpikePercentileMedium
			escalatedPercentile = priceSpikeEscalatedPercentileMedium
			useTimeOfDayRelative = true
		}

		pctVal := computePricePercentile(rawCosts, reqPercentile)

		// Candidate 1: Current price (active right now).
		// For Medium/Low, reference price is the time-of-day median (+/- 1 hour on previous days).
		// For High sensitivity, reference price is the overall all-hours median.
		var refPriceCurrent float64
		if useTimeOfDayRelative {
			refPriceCurrent = computeTimeOfDayRefPrice(histPrices, data.currentPrice.TSStart, siteLoc, medianCost)
		} else {
			refPriceCurrent = medianCost
		}
		isCurrentSpike := !data.currentPrice.TSStart.IsZero() &&
			currentCost >= priceSpikeAbsoluteFloorDollarsPerKWH &&
			currentCost >= pctVal &&
			(currentCost-refPriceCurrent) >= minDelta

		// Candidate 2: Future upcoming price (data.futurePrices[0]).
		var isFutureSpike bool
		var nextPrice types.Price
		var refPriceNext float64
		if len(data.futurePrices) > 0 {
			nextPrice = data.futurePrices[0]
			if useTimeOfDayRelative {
				refPriceNext = computeTimeOfDayRefPrice(histPrices, nextPrice.TSStart, siteLoc, medianCost)
			} else {
				refPriceNext = medianCost
			}
			isFutureSpike = nextPriceCost >= priceSpikeAbsoluteFloorDollarsPerKWH &&
				nextPriceCost >= pctVal &&
				(nextPriceCost-refPriceNext) >= minDelta
		}

		// If neither the active current price nor the next upcoming hour qualifies as a spike, skip.
		if !isCurrentSpike && !isFutureSpike {
			log.Ctx(ctx).DebugContext(ctx, "skipping price spike notification: price does not qualify as spike",
				slog.String("userID", su.userID),
				slog.String("sensitivity", su.sensitivity),
				slog.Float64("currentCost", currentCost),
				slog.Float64("nextPriceCost", nextPriceCost),
				slog.Float64("floor", priceSpikeAbsoluteFloorDollarsPerKWH),
				slog.Float64("pctVal", pctVal),
				slog.Float64("refPriceCurrent", refPriceCurrent),
				slog.Float64("refPriceNext", refPriceNext),
				slog.Float64("minDelta", minDelta),
				slog.Bool("useTimeOfDayRelative", useTimeOfDayRelative),
			)
			continue
		}

		var spikeStart, spikeEnd time.Time
		var activeSpikeCost, maxSpikeCost float64
		var maxSpikeTime time.Time
		var refPrice float64

		// Scan forward to determine the full contiguous duration of elevated prices and locate the peak price/time.
		if isCurrentSpike {
			spikeStart = data.currentPrice.TSStart
			spikeEnd = data.currentPrice.TSEnd
			if spikeEnd.IsZero() {
				spikeEnd = spikeStart.Add(time.Hour)
			}
			activeSpikeCost = currentCost
			maxSpikeCost = currentCost
			maxSpikeTime = spikeStart
			refPrice = refPriceCurrent

			// Scan forward across contiguous future prices that remain elevated
			for _, fp := range data.futurePrices {
				if !fp.TSStart.Before(spikeEnd) && (fp.TSStart.Equal(spikeEnd) || fp.TSStart.Sub(spikeEnd) <= 15*time.Minute) {
					fpCost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
					var fpRef float64
					if useTimeOfDayRelative {
						fpRef = computeTimeOfDayRefPrice(histPrices, fp.TSStart, siteLoc, medianCost)
					} else {
						fpRef = medianCost
					}
					if fpCost >= priceSpikeAbsoluteFloorDollarsPerKWH && fpCost >= pctVal && (fpCost-fpRef) >= minDelta {
						if !fp.TSEnd.IsZero() {
							spikeEnd = fp.TSEnd
						} else {
							spikeEnd = fp.TSStart.Add(time.Hour)
						}
						if fpCost > maxSpikeCost+0.005 {
							maxSpikeCost = fpCost
							maxSpikeTime = fp.TSStart
						}
					} else {
						break
					}
				}
			}
		} else {
			spikeStart = nextPrice.TSStart
			spikeEnd = nextPrice.TSEnd
			if spikeEnd.IsZero() {
				spikeEnd = spikeStart.Add(time.Hour)
			}
			activeSpikeCost = nextPriceCost
			maxSpikeCost = nextPriceCost
			maxSpikeTime = nextPrice.TSStart
			refPrice = refPriceNext

			// Scan forward across subsequent future prices
			for i := 1; i < len(data.futurePrices); i++ {
				fp := data.futurePrices[i]
				if !fp.TSStart.Before(spikeEnd) && (fp.TSStart.Equal(spikeEnd) || fp.TSStart.Sub(spikeEnd) <= 15*time.Minute) {
					fpCost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
					var fpRef float64
					if useTimeOfDayRelative {
						fpRef = computeTimeOfDayRefPrice(histPrices, fp.TSStart, siteLoc, medianCost)
					} else {
						fpRef = medianCost
					}
					if fpCost >= priceSpikeAbsoluteFloorDollarsPerKWH && fpCost >= pctVal && (fpCost-fpRef) >= minDelta {
						if !fp.TSEnd.IsZero() {
							spikeEnd = fp.TSEnd
						} else {
							spikeEnd = fp.TSStart.Add(time.Hour)
						}
						if fpCost > maxSpikeCost+0.005 {
							maxSpikeCost = fpCost
							maxSpikeTime = fp.TSStart
						}
					} else {
						break
					}
				}
			}
		}

		// Cooldown and Anti-Flapping Logic:
		// 1. Hard 1-hour lockout: Never re-alert within 1 hour of an alert.
		// 2. 1h to 6h window: Only re-alert if price is significantly higher (>= 20% surge above previously
		//    alerted/warned peak price) AND meets the escalated percentile threshold (e.g. top 5% or 2%).
		// 3. 6h to 24h window: Re-alert if prices dropped back below spike thresholds between alerts.
		//    If prices stayed continuously elevated for 6+ hours at the same high level, do not re-alert
		//    about the same price until the next day.
		// 4. 24h+: Standard alert thresholds apply.
		lastDeliveredLog, hasDelivered := getNotifState().lastLog(su.userID, true, types.NotificationTypePriceSpike)

		if hasDelivered {
			timeSince := s.now().Sub(lastDeliveredLog.TSCreated)
			if timeSince < 1*time.Hour {
				log.Ctx(ctx).DebugContext(ctx, "skipping price spike notification: within 1-hour lockout cooldown",
					slog.String("userID", su.userID),
					slog.Duration("timeSinceLastAlert", timeSince),
					slog.Time("lastAlertTime", lastDeliveredLog.TSCreated),
					slog.Float64("activeSpikeCost", activeSpikeCost),
					slog.Float64("maxSpikeCost", maxSpikeCost),
				)
				continue
			}

			prevPeakCost := extractHighestAlertedPrice(&lastDeliveredLog)
			escalatedPctVal := computePricePercentile(rawCosts, escalatedPercentile)
			checkCost := activeSpikeCost
			if maxSpikeCost > checkCost {
				checkCost = maxSpikeCost
			}
			var isSignificantSurge bool
			if prevPeakCost > 0 {
				isSignificantSurge = (checkCost >= prevPeakCost*priceSpikeSignificantMultiplier) && (checkCost >= escalatedPctVal)
			}

			if timeSince < 6*time.Hour {
				if !isSignificantSurge {
					log.Ctx(ctx).DebugContext(ctx, "skipping price spike notification: price surge not significant since last alert (1-6h window)",
						slog.String("userID", su.userID),
						slog.Duration("timeSinceLastAlert", timeSince),
						slog.Time("lastAlertTime", lastDeliveredLog.TSCreated),
						slog.Float64("prevPeakCost", prevPeakCost),
						slog.Float64("checkCost", checkCost),
						slog.Float64("requiredCost", prevPeakCost*priceSpikeSignificantMultiplier),
						slog.Float64("escalatedPctVal", escalatedPctVal),
					)
					continue
				}
			} else if timeSince < 24*time.Hour {
				droppedBelow := priceDroppedBelowBetween(
					histPrices,
					lastDeliveredLog.TSCreated,
					spikeStart,
					rawCosts,
					reqPercentile,
					minDelta,
					useTimeOfDayRelative,
					siteLoc,
				)
				if !droppedBelow && !isSignificantSurge {
					log.Ctx(ctx).DebugContext(ctx, "skipping price spike notification: price did not drop below threshold between alerts and not significant surge (6-24h window)",
						slog.String("userID", su.userID),
						slog.Duration("timeSinceLastAlert", timeSince),
						slog.Time("lastAlertTime", lastDeliveredLog.TSCreated),
						slog.Float64("prevPeakCost", prevPeakCost),
						slog.Float64("checkCost", checkCost),
						slog.Bool("droppedBelow", droppedBelow),
					)
					continue
				}
			}
		}

		// Construct Notification Title and Opening Sentence:
		// If the spike is active right now:
		// - If upcoming hours peak even higher (+2¢ or more), mention the current price and projected peak time/price.
		// - Otherwise, state the current price and duration.
		// If the spike begins in an upcoming hour:
		// - State when it begins, along with any projected higher peak time/price.
		var title, firstSentence string
		if isCurrentSpike {
			if maxSpikeCost >= activeSpikeCost+0.02 {
				title = fmt.Sprintf("🚨 Price Spike: $%.2f/kWh (peaking at $%.2f at %s)", activeSpikeCost, maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is $%.2f/kWh now and expected to rise to $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			} else {
				title = fmt.Sprintf("🚨 Price Spike: $%.2f/kWh", activeSpikeCost)
				firstSentence = fmt.Sprintf("Price is $%.2f/kWh now and anticipated to last until %s.", activeSpikeCost, spikeEnd.In(siteLoc).Format("3:04 PM"))
			}
		} else {
			if maxSpikeCost >= activeSpikeCost+0.02 {
				title = fmt.Sprintf("🚨 Price Spike Ahead: $%.2f/kWh at %s (peaking at $%.2f at %s)", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is projected to reach $%.2f/kWh at %s and peak at $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			} else {
				title = fmt.Sprintf("🚨 Price Spike Ahead: $%.2f/kWh at %s", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is projected to reach $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			}
		}

		// Second sentence explains the baseline comparison and what RateRudder will do.
		// Defaults to stating RateRudder will prioritize battery power.
		secondSentence := fmt.Sprintf("Electricity rates are surging significantly above recent prices ($%.2f/kWh). RateRudder will prioritize battery power to protect your home.", refPrice)

		// Inspect hourly simulation data across the spike slots to provide specific home battery outcome guidance:
		// 1. If solar generation covers entire home consumption: inform user solar is powering home without drawing battery.
		// 2. If battery is already at or below reserve: warn that the home will draw from grid during the spike.
		// 3. If battery will run out of energy before the spike ends: provide the estimated time battery reaches reserve.
		// 4. If battery lasts through the entire spike: reassure user battery powers home through the whole event.
		currentSOC := data.status.BatterySOC
		reserveSOC := data.settings.GetMinBatterySOC(ctx, spikeStart, siteLoc, types.Price{DollarsPerKWH: activeSpikeCost})
		if reserveSOC <= 0 {
			reserveSOC = data.settings.MinBatterySOC
		}

		planHelper := newNotificationPlanHelper(data.plan)
		spikeOutcome := planHelper.priceSpikeGuidance(ctx, spikeStart, spikeEnd, currentSOC, reserveSOC)

		if spikeOutcome.solarCovers {
			secondSentence = "Solar is projected to cover your home usage during the spike."
		} else if spikeOutcome.isExporting {
			secondSentence = "RateRudder is discharging battery to export power during the surge."
		} else if currentSOC <= reserveSOC {
			secondSentence = fmt.Sprintf("Battery is currently at %.0f%% reserve; your home will draw from the grid during the spike.", currentSOC)
		} else if !spikeOutcome.reachesReserveAt.IsZero() {
			secondSentence = fmt.Sprintf("Battery is at %.0f%% and will supply home until ~%s.", currentSOC, spikeOutcome.reachesReserveAt.In(siteLoc).Format("3:04 PM"))
		} else if spikeOutcome.lastsEntireSpike {
			secondSentence = fmt.Sprintf("Battery is at %.0f%% and projected to power your home through the spike.", currentSOC)
		}

		// Store structured metadata on the notification log for debugging and re-alert evaluations
		metadata := map[string]string{
			"price":      fmt.Sprintf("%.4f", activeSpikeCost),
			"peakPrice":  fmt.Sprintf("%.4f", maxSpikeCost),
			"refPrice":   fmt.Sprintf("%.4f", refPrice),
			"currentSOC": fmt.Sprintf("%.1f", currentSOC),
			"spikeStart": spikeStart.In(siteLoc).Format(time.RFC3339),
			"spikeEnd":   spikeEnd.In(siteLoc).Format(time.RFC3339),
		}

		body := fmt.Sprintf("%s %s", firstSentence, secondSentence)

		log.Ctx(ctx).DebugContext(ctx, "sending price spike notification",
			slog.String("userID", su.userID),
			slog.String("sensitivity", su.sensitivity),
			slog.Float64("activeSpikeCost", activeSpikeCost),
			slog.Float64("maxSpikeCost", maxSpikeCost),
			slog.Float64("refPrice", refPrice),
			slog.Time("spikeStart", spikeStart),
			slog.Time("spikeEnd", spikeEnd),
			slog.Float64("currentSOC", currentSOC),
			slog.String("title", title),
			slog.String("body", body),
		)
		s.dispatchPushToUser(
			ctx,
			siteID,
			su.userID,
			types.NotificationTypePriceSpike,
			su.sensitivity,
			title,
			body,
			"/forecast",
			metadata,
			getUser,
		)
	}
}

// handleSolarUnderproductionNotifications evaluates and sends alerts when actual solar production
// drops significantly below weather-forecasted generation during peak production hours.
//
// Key safeguards:
// 1. Time window: Only evaluated during peak midday hours (11:00 AM - 3:00 PM local time).
// 2. Weather overcast suppression: If cloud cover exceeds 60%, cloud cover explains the deficit, so no alert is sent.
// 3. Active alarms/storms: Suppressed during severe weather or ESS hardware alarms.
// 4. Sensitivity tiers:
//   - Low: Triggers if actual < 50% of forecast (noticeable underproduction / dirty panels).
//   - Medium: Triggers if actual < 30% of forecast (moderate underproduction / partial string failure).
//   - High: Triggers if actual < 15% of forecast (severe underproduction / inverter tripped).
//
// 5. Absolute deficit requirement: Requires at least 2.5 kW generation shortfall to prevent micro-alerts.
func (s *Server) handleSolarUnderproductionNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	hasAnySolarUser := false
	for _, notifConfig := range notifications {
		if s := notifConfig.RealTimeAlertSensitivity(notifConfig.SolarUnderproductionAlert); s != "" {
			hasAnySolarUser = true
			break
		}
	}
	// Restrict evaluation to peak solar hours (11 AM to 3 PM) and suppress during active storms or alarms
	if !hasAnySolarUser {
		return
	}
	if nowLocal.Hour() < 11 || nowLocal.Hour() > 15 {
		log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction check: outside peak solar hours (11am-3pm)",
			slog.Int("hour", nowLocal.Hour()),
		)
		return
	}
	if len(data.status.Storms) > 0 || len(data.status.Alarms) > 0 {
		log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction check: active storms or alarms present",
			slog.Int("storms", len(data.status.Storms)),
			slog.Int("alarms", len(data.status.Alarms)),
		)
		return
	}

	// Check whether recent weather forecasts indicate heavy cloud cover for this hour
	isOvercast := false
	var overcastCloudCover float64
	var forecastHourStart time.Time
	for _, w := range data.weatherHistory {
		for _, hw := range w.ForecastHours {
			hwLocal := hw.TSHourStart.In(nowLocal.Location())
			if hwLocal.Year() == nowLocal.Year() && hwLocal.Month() == nowLocal.Month() && hwLocal.Day() == nowLocal.Day() && hwLocal.Hour() == nowLocal.Hour() {
				if hw.CloudCoverPercent >= solarUnderproductionMaxCloudCoverPercent {
					isOvercast = true
					overcastCloudCover = hw.CloudCoverPercent
					forecastHourStart = hw.TSHourStart
				}
				break
			}
		}
		if isOvercast {
			break
		}
	}
	if isOvercast {
		log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction check: overcast weather forecast",
			slog.Float64("cloudCoverPercent", overcastCloudCover),
			slog.Float64("maxCloudCoverPercent", solarUnderproductionMaxCloudCoverPercent),
			slog.Time("forecastHour", forecastHourStart),
		)
		return
	}

	// Locate the forecasted solar generation for the current hour from plan data
	planHelper := newNotificationPlanHelper(data.plan)
	forecastKW, hasForecast := planHelper.currentSolarForecastKW(nowLocal)
	if !hasForecast || forecastKW < solarUnderproductionMinForecastKW {
		log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction check: forecasted solar too low or missing",
			slog.Float64("forecastKW", forecastKW),
			slog.Float64("minForecastKW", solarUnderproductionMinForecastKW),
		)
		return
	}

	todayDateStr := nowLocal.Format("2006-01-02")
	actualKW := data.status.SolarKW
	for userID, notifConfig := range notifications {
		sensitivity := notifConfig.RealTimeAlertSensitivity(notifConfig.SolarUnderproductionAlert)
		if sensitivity == "" {
			continue
		}
		var ratio float64
		switch sensitivity {
		case "low":
			ratio = solarUnderproductionRatioLow
		case "high":
			ratio = solarUnderproductionRatioHigh
		case "medium":
			fallthrough
		default:
			ratio = solarUnderproductionRatioMedium
		}

		// Check if actual generation is below the sensitivity ratio AND meets the minimum kW deficit
		if actualKW >= ratio*forecastKW || (forecastKW-actualKW) < solarUnderproductionMinDeficitKW {
			log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction notification: actual generation within tolerance",
				slog.String("userID", userID),
				slog.String("sensitivity", sensitivity),
				slog.Float64("actualKW", actualKW),
				slog.Float64("forecastKW", forecastKW),
				slog.Float64("ratioThreshold", ratio),
				slog.Float64("minDeficitKW", solarUnderproductionMinDeficitKW),
			)
			continue
		}

		if getNotifState().hasSentToday(userID, types.NotificationTypeSolarUnderproduction, todayDateStr, nowLocal.Location()) {
			log.Ctx(ctx).DebugContext(ctx, "skipping solar underproduction notification: already sent today",
				slog.String("userID", userID),
				slog.String("date", todayDateStr),
			)
			continue
		}
		title := "⚠️ Solar Underproduction Alert"
		body := fmt.Sprintf("Solar panels are generating %.1f kW, significantly below the %.1f kW forecast for this hour. Check your solar inverter or breakers.", actualKW, forecastKW)
		metadata := map[string]string{
			"currentSolarKW":  fmt.Sprintf("%.2f", actualKW),
			"forecastSolarKW": fmt.Sprintf("%.2f", forecastKW),
			"deficitKW":       fmt.Sprintf("%.2f", forecastKW-actualKW),
		}
		if notifConfig.IsInQuietPeriod(nowLocal) {
			s.logMutedNotification(ctx, siteID, userID, types.NotificationTypeSolarUnderproduction, sensitivity, title, body, metadata)
			continue
		}

		log.Ctx(ctx).DebugContext(ctx, "sending solar underproduction notification",
			slog.String("userID", userID),
			slog.String("sensitivity", sensitivity),
			slog.Float64("actualKW", actualKW),
			slog.Float64("forecastKW", forecastKW),
			slog.Float64("deficitKW", forecastKW-actualKW),
			slog.String("title", title),
			slog.String("body", body),
		)
		s.dispatchPushToUser(
			ctx,
			siteID,
			userID,
			types.NotificationTypeSolarUnderproduction,
			sensitivity,
			title,
			body,
			"/dashboard",
			metadata,
			getUser,
		)
	}
}

// handleVPPDispatchNotifications evaluates and sends alerts when an unexpected or unplanned Virtual Power Plant
// (VPP) grid support event is triggered on the user's battery system.
func (s *Server) handleVPPDispatchNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	status types.SystemStatus,
	vppInfo types.UtilityVPPInfo,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	if !status.VPPActive {
		return
	}

	hasAnyVPPUser := false
	for _, notifConfig := range notifications {
		if notifConfig.RealTimeAlertEnabled(notifConfig.VPPDispatchAlert) {
			hasAnyVPPUser = true
			break
		}
	}
	if !hasAnyVPPUser {
		return
	}

	siteLoc := status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	// Check if this VPP dispatch was scheduled or known in advance.
	// We only send alerts for unscheduled / unplanned VPP dispatches so users aren't spammed during normal scheduled programs.
	isPlanned := false
	for _, p := range vppInfo.Mandatory {
		if contains, _, _ := p.Contains(nowLocal); contains {
			isPlanned = true
			break
		}
	}
	if !isPlanned {
		for _, ev := range status.VPPEvents {
			if (nowLocal.Equal(ev.TSStart) || nowLocal.After(ev.TSStart)) && nowLocal.Before(ev.TSEnd) {
				isPlanned = true
				break
			}
		}
	}
	if isPlanned {
		log.Ctx(ctx).DebugContext(ctx, "skipping vpp dispatch notification: event was scheduled",
			slog.Time("nowLocal", nowLocal),
		)
		return
	}

	for userID, notifConfig := range notifications {
		if !notifConfig.RealTimeAlertEnabled(notifConfig.VPPDispatchAlert) {
			continue
		}
		if getNotifState().hasSentWithin(userID, types.NotificationTypeVPPDispatch, vppDispatchDeduplicationWindow, s.now()) {
			log.Ctx(ctx).DebugContext(ctx, "skipping vpp dispatch notification: within deduplication window",
				slog.String("userID", userID),
				slog.Duration("window", vppDispatchDeduplicationWindow),
			)
			continue
		}
		title := "⚡ Virtual Power Plant Active"
		body := "RateRudder detected an active VPP grid support event on your system."
		if status.BatteryKW > 0.1 {
			body = "Your battery is discharging to support the electric grid during an unscheduled VPP event."
		}
		metadata := map[string]string{
			"currentSOC": fmt.Sprintf("%.1f", status.BatterySOC),
			"batteryKW":  fmt.Sprintf("%.2f", status.BatteryKW),
		}
		if notifConfig.IsInQuietPeriod(nowLocal) {
			s.logMutedNotification(ctx, siteID, userID, types.NotificationTypeVPPDispatch, "", title, body, metadata)
			continue
		}

		log.Ctx(ctx).DebugContext(ctx, "sending vpp dispatch notification",
			slog.String("userID", userID),
			slog.Float64("batterySOC", status.BatterySOC),
			slog.Float64("batteryKW", status.BatteryKW),
			slog.String("title", title),
			slog.String("body", body),
		)
		s.dispatchPushToUser(
			ctx,
			siteID,
			userID,
			types.NotificationTypeVPPDispatch,
			"",
			title,
			body,
			"/dashboard",
			metadata,
			getUser,
		)
	}
}

// calculateTODHomeLoadBaseline extracts historical load readings for the Time-of-Day (TOD)
// window [H-1, H, H+1] across past days.
// Returns false if there is insufficient historical data (< 3 readings) to reliably establish a baseline.
func calculateTODHomeLoadBaseline(history []types.DailyEnergyStats, targetLocal time.Time, loc *time.Location) ([]float64, bool) {
	if loc == nil {
		loc = time.UTC
	}
	targetLocal = targetLocal.In(loc)
	todayStart := time.Date(targetLocal.Year(), targetLocal.Month(), targetLocal.Day(), 0, 0, 0, 0, loc)
	targetHour := targetLocal.Hour()
	prevHour := (targetHour + 23) % 24
	nextHour := (targetHour + 1) % 24

	hasPriorDays := false
	for _, day := range history {
		for _, h := range day.Hourly {
			if h.TSHourStart.In(loc).Before(todayStart) {
				hasPriorDays = true
				break
			}
		}
		if hasPriorDays {
			break
		}
	}
	if !hasPriorDays {
		return nil, false
	}

	var todLoads []float64
	for _, day := range history {
		for _, h := range day.Hourly {
			tHour := h.TSHourStart.In(loc)
			if !tHour.Before(todayStart) {
				continue
			}
			if h.HomeKWH <= 0 {
				continue
			}
			hr := tHour.Hour()
			if hr == prevHour || hr == targetHour || hr == nextHour {
				todLoads = append(todLoads, h.HomeKWH)
			}
		}
	}

	if len(todLoads) < 3 {
		return nil, false
	}
	return todLoads, true
}

// calculateTODReserveRatio computes the fraction of historical hours over the past 7 days
// around the same time of day (+/- 1 hour) where the battery was at or near the current reserve SOC level.
// It excludes the current day (today). Returns (ratio, true) if sufficient historical data exists (>= 3 samples),
// or (0, false) if there is insufficient historical data.
func calculateTODReserveRatio(history []types.DailyEnergyStats, targetLocal time.Time, loc *time.Location, currentSOC float64) (float64, bool) {
	if loc == nil {
		loc = time.UTC
	}
	targetLocal = targetLocal.In(loc)
	targetHour := targetLocal.Hour()
	prevHour := (targetHour + 23) % 24
	nextHour := (targetHour + 1) % 24

	todayStart := time.Date(targetLocal.Year(), targetLocal.Month(), targetLocal.Day(), 0, 0, 0, 0, loc)
	startWindow := todayStart.AddDate(0, 0, -7)

	totalHours := 0
	reserveHours := 0
	for _, day := range history {
		for _, h := range day.Hourly {
			tHour := h.TSHourStart.In(loc)
			if tHour.Before(startWindow) || !tHour.Before(todayStart) {
				continue
			}
			hr := tHour.Hour()
			if hr != prevHour && hr != targetHour && hr != nextHour {
				continue
			}
			if h.MaxBatterySOC <= 0 {
				continue
			}
			totalHours++
			if h.MaxBatterySOC <= currentSOC+2.0 || h.MinBatterySOC <= currentSOC+0.5 {
				reserveHours++
			}
		}
	}
	if totalHours < 3 {
		return 0, false
	}
	return float64(reserveHours) / float64(totalHours), true
}

// isCheapestRateOfDay checks if the current electricity cost is within $0.01/kWh
// of the minimum rate across the upcoming 24 hours, provided there is a meaningful price spread (>= $0.02/kWh)
// indicating distinct peak and off-peak periods. It also returns the minimum and maximum price across the 24-hour window.
func isCheapestRateOfDay(current types.Price, future []types.Price, loc *time.Location) (isCheapest bool, minPrice, maxPrice float64) {
	if current.TSStart.IsZero() {
		return false, 0, 0
	}
	if loc == nil {
		loc = time.UTC
	}
	currLocal := current.TSStart.In(loc)
	currentCost := current.DollarsPerKWH + current.GridUseDollarsPerKWH

	minPrice = currentCost
	maxPrice = currentCost
	for _, fp := range future {
		fpLocal := fp.TSStart.In(loc)
		if fpLocal.Before(currLocal) || fpLocal.After(currLocal.Add(24*time.Hour)) {
			continue
		}
		cost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
		if cost < minPrice {
			minPrice = cost
		}
		if cost > maxPrice {
			maxPrice = cost
		}
	}

	if len(future) == 0 || maxPrice-minPrice < 0.02 {
		return false, minPrice, maxPrice
	}
	return currentCost <= minPrice+0.01, minPrice, maxPrice
}

// formatHomeLoadPriceGuidance formats actionable electricity price guidance for high home load notifications.
// It checks whether current prices are at the day's peak, if rates will drop by >= $0.05/kWh in upcoming hours,
// or omits price entirely if current rates are already the day's cheapest.
func formatHomeLoadPriceGuidance(current types.Price, future []types.Price, loc *time.Location) string {
	if current.TSStart.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	currLocal := current.TSStart.In(loc)
	currentCost := current.DollarsPerKWH + current.GridUseDollarsPerKWH

	isCheapest, minPrice, maxPrice := isCheapestRateOfDay(current, future, loc)
	// 1. Already at Cheapest Rate:
	// If C_curr <= min(C_day) + 0.01: Do not mention price at all.
	if isCheapest || currentCost <= minPrice+0.01 {
		return ""
	}

	// 2. Peak Rates for the Day:
	// If C_curr >= max(C_day) - 0.01 and max(C_day) - min(C_day) >= 0.05:
	isPeak := currentCost >= maxPrice-0.01 && (maxPrice-minPrice) >= (highHomeLoadMinPriceDropDollarsPerKWH-1e-3)

	// 3. Significant Upcoming Price Drop (>= $0.05/kWh):
	// Find the earliest upcoming interval within the next 8 hours where C_drop <= C_curr - 0.05
	var hasDrop bool
	var dropTime time.Time
	var dropCost float64
	for _, fp := range future {
		fpLocal := fp.TSStart.In(loc)
		if fpLocal.Before(currLocal) {
			continue
		}
		if fpLocal.After(currLocal.Add(8 * time.Hour)) {
			break
		}
		cost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
		if currentCost-cost >= (highHomeLoadMinPriceDropDollarsPerKWH - 1e-3) {
			hasDrop = true
			dropTime = fpLocal
			dropCost = cost
			break
		}
	}

	var peakStr, dropStr string
	if isPeak {
		peakStr = fmt.Sprintf("Rates are currently at today's peak ($%.2f/kWh).", currentCost)
	}
	if hasDrop {
		timeStr := dropTime.Format("3 PM")
		if dropTime.Minute() != 0 {
			timeStr = dropTime.Format("3:04 PM")
		}
		dropStr = fmt.Sprintf("Consider waiting until %s when rates drop to $%.2f/kWh.", timeStr, dropCost)
	}

	if peakStr != "" && dropStr != "" {
		return peakStr + " " + dropStr
	} else if peakStr != "" {
		return peakStr
	} else if dropStr != "" {
		return dropStr
	}
	return ""
}

// handleHighHomeLoadNotifications evaluates and sends alerts when home electricity consumption surges abnormally.
// It warns whether the battery will run out (with estimated minutes until reserve) or has already run out
// (battery is at reserve and grid power is in use), with actionable electricity price guidance.
func (s *Server) handleHighHomeLoadNotifications(
	ctx context.Context,
	siteID string,
	notifications map[string]types.UserNotificationSettings,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
	getUser userFetcher,
) {
	hasAnyUser := false
	for _, notifConfig := range notifications {
		if s := notifConfig.RealTimeAlertSensitivity(notifConfig.HighHomeLoadAlert); s != "" {
			hasAnyUser = true
			break
		}
	}
	if !hasAnyUser {
		return
	}

	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	} else {
		nowLocal = nowLocal.In(siteLoc)
	}

	// TODO: Re-address suppressing high-home-load alerts specifically during detected EV charging
	// sessions so we do not ignore all nighttime loads when EVChargingStandby defaults to on.
	// Legacy EV charging suppression: if current time falls within any configured EV charging period, suppress alert
	for _, period := range data.settings.EVChargingPeriods {
		if inPeriod, _, err := period.Contains(nowLocal); err == nil && inPeriod {
			log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: within EV charging period",
				slog.String("periodName", period.Name),
				slog.Time("nowLocal", nowLocal),
			)
			return
		}
	}

	// Solar coverage suppression: if solar covers home load (within tolerance), suppress alert
	if data.status.SolarKW >= data.status.HomeKW-highHomeLoadSolarCoverageToleranceKW {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: solar covers load",
			slog.Float64("solarKW", data.status.SolarKW),
			slog.Float64("homeKW", data.status.HomeKW),
			slog.Float64("toleranceKW", highHomeLoadSolarCoverageToleranceKW),
		)
		return
	}

	// Floor check: home load must be at least the high sensitivity absolute minimum
	if data.status.HomeKW < highHomeLoadMinAbsoluteKWHigh {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: load below minimum threshold",
			slog.Float64("homeKW", data.status.HomeKW),
			slog.Float64("minLoad", highHomeLoadMinAbsoluteKWHigh),
		)
		return
	}

	todLoads, ok := calculateTODHomeLoadBaseline(data.energyHistory, nowLocal, siteLoc)
	if !ok {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: insufficient historical load data",
			slog.Int("todLoadsCount", len(todLoads)),
		)
		return
	}

	reserveSOC := data.settings.GetMinBatterySOC(ctx, nowLocal, siteLoc, data.currentPrice)
	if reserveSOC <= 0 {
		reserveSOC = 20.0
	}
	reserveBufferPct := data.settings.GetOptimizationParams().ReserveBufferPercent
	effectiveReserveSOC := reserveSOC + reserveBufferPct
	if effectiveReserveSOC > 100.0 {
		effectiveReserveSOC = 100.0
	}

	capKWH := data.status.BatteryCapacityKWH
	if capKWH <= 0 {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: missing battery capacity",
			slog.Any("status", data.status),
		)
		return
	}

	// Battery state determination
	isAtReserve := data.status.BatterySOC <= effectiveReserveSOC+1.0 && data.status.GridKW >= 1.0
	usableKWH := (data.status.BatterySOC - effectiveReserveSOC) * (capKWH / 100.0)
	var hoursRemaining float64
	var isWillRunOut bool
	if !isAtReserve && data.status.BatteryKW >= 0.5 && usableKWH >= highHomeLoadMinUsableBatteryKWH {
		simEnergy := data.status.BatterySOC * (capKWH / 100.0)
		for m := 1; m <= 60; m++ {
			tSim := nowLocal.Add(time.Duration(m) * time.Minute)
			simEnergy -= data.status.BatteryKW * (1.0 / 60.0)
			targetReserveSOC := effectiveReserveSOC
			if tSim.Hour() != nowLocal.Hour() {
				var nextPrice types.Price
				for _, fp := range data.futurePrices {
					fpLocal := fp.TSStart.In(siteLoc)
					if fpLocal.Hour() == tSim.Hour() && fpLocal.Day() == tSim.Day() {
						nextPrice = fp
						break
					}
				}
				if nextSOC := data.settings.GetMinBatterySOC(ctx, tSim, siteLoc, nextPrice); nextSOC > 0 {
					targetReserveSOC = nextSOC + reserveBufferPct
					if targetReserveSOC > 100.0 {
						targetReserveSOC = 100.0
					}
				}
			}
			targetReserveKWH := targetReserveSOC * (capKWH / 100.0)
			if simEnergy <= targetReserveKWH {
				hoursRemaining = float64(m) / 60.0
				isWillRunOut = true
				break
			}
		}
	}

	if !isAtReserve && !isWillRunOut {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: battery not running out and not at reserve",
			slog.Float64("batterySOC", data.status.BatterySOC),
			slog.Float64("reserveSOC", effectiveReserveSOC),
			slog.Float64("usableKWH", usableKWH),
			slog.Float64("batteryKW", data.status.BatteryKW),
			slog.Float64("gridKW", data.status.GridKW),
			slog.Float64("hoursRemaining", hoursRemaining),
		)
		return
	}

	// Chronic reserve filter: suppress alerts if the site is chronically at or near reserve around this time of day.
	// Applies both when already at reserve, and when projected to run out to reserve.
	if reserveRatio, ok := calculateTODReserveRatio(data.energyHistory, nowLocal, siteLoc, effectiveReserveSOC); ok && reserveRatio >= highHomeLoadMaxChronicReserveRatio {
		log.Ctx(ctx).DebugContext(ctx, "skipping high home load check: site chronically at reserve around this time of day",
			slog.Float64("reserveRatio", reserveRatio),
			slog.Float64("maxChronicReserveRatio", highHomeLoadMaxChronicReserveRatio),
			slog.Float64("currentSOC", data.status.BatterySOC),
			slog.Float64("reserveSOC", effectiveReserveSOC),
			slog.Bool("isAtReserve", isAtReserve),
			slog.Bool("isWillRunOut", isWillRunOut),
		)
		return
	}

	isCheapestRate, _, _ := isCheapestRateOfDay(data.currentPrice, data.futurePrices, siteLoc)
	priceGuidance := formatHomeLoadPriceGuidance(data.currentPrice, data.futurePrices, siteLoc)
	currentLoad := data.status.HomeKW

	for userID, notifConfig := range notifications {
		sensitivity := notifConfig.RealTimeAlertSensitivity(notifConfig.HighHomeLoadAlert)
		if sensitivity == "" {
			continue
		}

		// Cheapest rate suppression: users on low or medium sensitivity are not warned during
		// the day's cheapest rate period, as large loads are likely intentionally scheduled.
		// High sensitivity users continue to be notified.
		if isCheapestRate && sensitivity != "high" {
			log.Ctx(ctx).DebugContext(ctx, "skipping high home load notification: cheapest rate of the day on low/medium sensitivity",
				slog.String("userID", userID),
				slog.String("sensitivity", sensitivity),
			)
			continue
		}

		var minLoad, reqTODPercentile float64
		switch sensitivity {
		case "low":
			minLoad = highHomeLoadMinAbsoluteKWLow
			reqTODPercentile = highHomeLoadTODPercentileLow
		case "high":
			minLoad = highHomeLoadMinAbsoluteKWHigh
			reqTODPercentile = highHomeLoadTODPercentileHigh
		case "medium":
			fallthrough
		default:
			minLoad = highHomeLoadMinAbsoluteKWMedium
			reqTODPercentile = highHomeLoadTODPercentileMedium
		}

		if currentLoad < minLoad {
			log.Ctx(ctx).DebugContext(ctx, "skipping high home load notification: load below sensitivity minimum threshold",
				slog.String("userID", userID),
				slog.Float64("homeKW", currentLoad),
				slog.Float64("minLoad", minLoad),
			)
			continue
		}

		pTOD := computePercentile(todLoads, reqTODPercentile) * highHomeLoadPeakToAverageFactor
		if currentLoad < pTOD {
			log.Ctx(ctx).DebugContext(ctx, "skipping high home load notification: load below TOD baseline threshold",
				slog.String("userID", userID),
				slog.Float64("homeKW", currentLoad),
				slog.Float64("pTOD", pTOD),
				slog.Float64("percentile", reqTODPercentile),
				slog.Float64("factor", highHomeLoadPeakToAverageFactor),
			)
			continue
		}

		// Cooldown check: strict cooldown window (no escalation bypass, no state tracking)
		lastDeliveredLog, hasDelivered := getNotifState().lastLog(userID, true, types.NotificationTypeHighHomeLoad)
		if hasDelivered {
			timeSince := s.now().Sub(lastDeliveredLog.TSCreated)
			if timeSince < highHomeLoadCooldownWindow {
				log.Ctx(ctx).DebugContext(ctx, "skipping high home load notification: within cooldown window",
					slog.String("userID", userID),
					slog.Duration("timeSinceLastAlert", timeSince),
					slog.Duration("cooldownWindow", highHomeLoadCooldownWindow),
				)
				continue
			}
		}

		title := "⚠️ High Home Load Detected"
		var baseBody string
		metadata := map[string]string{
			"homeKW":     fmt.Sprintf("%.2f", currentLoad),
			"batterySOC": fmt.Sprintf("%.1f", data.status.BatterySOC),
		}

		if isAtReserve {
			baseBody = fmt.Sprintf("Large unusual home load detected (%.1f kW) and battery is at reserve (%.0f%% SOC). Grid will be used.", currentLoad, data.status.BatterySOC)
			metadata["gridKW"] = fmt.Sprintf("%.2f", data.status.GridKW)
		} else {
			minutes := math.Round(hoursRemaining * 60.0)
			if minutes < 1 {
				minutes = 1
			}
			baseBody = fmt.Sprintf("Large unusual home load detected (%.1f kW). Battery will run out in ~%.0f min (SOC %.0f%%). Grid will be used.", currentLoad, minutes, data.status.BatterySOC)
			metadata["minutesRemaining"] = fmt.Sprintf("%.0f", minutes)
		}

		body := baseBody
		if priceGuidance != "" {
			body = baseBody + " " + priceGuidance
		}

		if notifConfig.IsInQuietPeriod(nowLocal) {
			s.logMutedNotification(ctx, siteID, userID, types.NotificationTypeHighHomeLoad, sensitivity, title, body, metadata)
			continue
		}

		log.Ctx(ctx).DebugContext(ctx, "sending high home load notification",
			slog.String("userID", userID),
			slog.String("sensitivity", sensitivity),
			slog.Float64("homeKW", currentLoad),
			slog.Float64("batterySOC", data.status.BatterySOC),
			slog.String("title", title),
			slog.String("body", body),
		)
		s.dispatchPushToUser(
			ctx,
			siteID,
			userID,
			types.NotificationTypeHighHomeLoad,
			sensitivity,
			title,
			body,
			"/dashboard",
			metadata,
			getUser,
		)
	}
}

// handleGetVAPIDPublicKey returns the server's derived VAPID public key as an application/octet-stream binary.
func (s *Server) handleGetVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if !s.notificationsEnabled() {
		writeJSONError(w, "notifications not configured on server", http.StatusServiceUnavailable)
		return
	}

	pubBytes, err := decodeBase64Key(s.vapidPublicKey)
	if err != nil {
		writeJSONError(w, "failed to decode public key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(pubBytes); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// subscribeRequest payload for registering a push subscription.
type subscribeRequest struct {
	Subscription types.PushSubscription `json:"subscription"`
	SendTest     bool                   `json:"sendTest,omitempty"`
}

// handleSubscribe registers a new push subscription for the authenticated user.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.notificationsEnabled() {
		writeJSONError(w, "notifications not configured on server", http.StatusServiceUnavailable)
		return
	}

	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Subscription.Endpoint == "" || req.Subscription.Keys.P256DH == "" || req.Subscription.Keys.Auth == "" {
		writeJSONError(w, "subscription endpoint and keys are required", http.StatusBadRequest)
		return
	}

	// Generate SHA256 ID from endpoint if empty
	if req.Subscription.ID == "" {
		h := sha256.Sum256([]byte(req.Subscription.Endpoint))
		req.Subscription.ID = hex.EncodeToString(h[:])
	}
	req.Subscription.TSCreated = s.now().UTC()

	ctx := r.Context()
	if err := s.storage.AddUserPushSubscription(ctx, userID, req.Subscription); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save push subscription", slog.Any("error", err))
		writeJSONError(w, "failed to save subscription", http.StatusInternalServerError)
		return
	}

	// If sendTest is requested, dispatch an immediate test notification
	if req.SendTest {
		payload := pushPayload{
			Title: "RateRudder Notifications Enabled",
			Body:  "You will now receive energy updates and alerts for your system.",
			Tag:   "raterudder-test",
			Icon:  pushPayloadIcon(req.Subscription),
			Badge: "/badge_96.png",
			Data: map[string]any{
				"url": "/dashboard",
			},
		}
		if code, err := s.sendWebPush(ctx, req.Subscription, payload, 300, "high"); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to send test welcome push notification",
				slog.String("userID", userID),
				slog.Int("statusCode", code),
				slog.Any("error", err),
			)
		} else {
			log.Ctx(ctx).InfoContext(ctx, "sent test welcome push notification",
				slog.String("userID", userID),
				slog.Int("statusCode", code),
			)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// unsubscribeRequest payload for removing a push subscription.
type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// handleUnsubscribe removes a push subscription for the authenticated user.
func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" {
		writeJSONError(w, "endpoint is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := s.storage.RemoveUserPushSubscription(ctx, userID, req.Endpoint); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to remove push subscription", slog.Any("error", err))
		writeJSONError(w, "failed to remove subscription", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// replaceSubscriptionRequest payload for rotating or revoking a push subscription without user authentication.
type replaceSubscriptionRequest struct {
	PrevEndpoint string                  `json:"prevEndpoint"`
	PrevAuth     string                  `json:"prevAuth"`
	Subscription *types.PushSubscription `json:"subscription"`
}

// handleReplaceSubscription handles unauthenticated push subscription replacement or revocation.
func (s *Server) handleReplaceSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.notificationsEnabled() {
		writeJSONError(w, "notifications not configured on server", http.StatusServiceUnavailable)
		return
	}

	var req replaceSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PrevEndpoint == "" || req.PrevAuth == "" {
		writeJSONError(w, "prevEndpoint and prevAuth are required", http.StatusBadRequest)
		return
	}

	if req.Subscription != nil && req.Subscription.Endpoint != "" {
		if req.Subscription.Keys.P256DH == "" || req.Subscription.Keys.Auth == "" {
			writeJSONError(w, "subscription keys are required for replacement", http.StatusBadRequest)
			return
		}
		if req.Subscription.ID == "" {
			h := sha256.Sum256([]byte(req.Subscription.Endpoint))
			req.Subscription.ID = hex.EncodeToString(h[:])
		}
		req.Subscription.TSCreated = s.now().UTC()
	}

	ctx := r.Context()
	if err := s.storage.ReplaceUserPushSubscription(ctx, req.PrevEndpoint, req.PrevAuth, req.Subscription); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to replace push subscription", slog.Any("error", err), slog.String("prevEndpoint", req.PrevEndpoint))
		writeJSONError(w, "failed to replace subscription", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]bool{"replaced": req.Subscription != nil && req.Subscription.Endpoint != ""}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// updateNotificationSettingsRequest payload for updating user notification preferences on a site.
type updateNotificationSettingsRequest struct {
	SiteID   string                         `json:"siteID"`
	Settings types.UserNotificationSettings `json:"settings"`
}

// handleUpdateNotificationSettings updates user notification preferences on a site.
func (s *Server) handleUpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req updateNotificationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	siteID := s.getSiteID(r)
	if siteID == "" {
		writeJSONError(w, "siteID is required", http.StatusBadRequest)
		return
	}

	if req.Settings.MorningSummaryHour < 0 || req.Settings.MorningSummaryHour > 23 {
		writeJSONError(w, "morning summary hour must be between 0 and 23", http.StatusBadRequest)
		return
	}
	if req.Settings.MorningSummaryEnabled && req.Settings.MorningSummaryFlavor == "" {
		writeJSONError(w, "morning summary flavor is required", http.StatusBadRequest)
		return
	}
	if req.Settings.MorningSummaryFlavor == "" {
		req.Settings.MorningSummaryFlavor = defaultSummaryFlavor
	}
	if req.Settings.EveningSummaryHour < 0 || req.Settings.EveningSummaryHour > 23 {
		writeJSONError(w, "evening summary hour must be between 0 and 23", http.StatusBadRequest)
		return
	}
	if req.Settings.EveningSummaryEnabled && req.Settings.EveningSummaryFlavor == "" {
		writeJSONError(w, "evening summary flavor is required", http.StatusBadRequest)
		return
	}
	if req.Settings.EveningSummaryFlavor == "" {
		req.Settings.EveningSummaryFlavor = defaultSummaryFlavor
	}
	switch req.Settings.PriceSpikeAlert {
	case "", "disabled", "low", "medium", "high":
	default:
		writeJSONError(w, "invalid price spike alert sensitivity", http.StatusBadRequest)
		return
	}
	switch req.Settings.SolarUnderproductionAlert {
	case "", "disabled", "low", "medium", "high":
	default:
		writeJSONError(w, "invalid solar underproduction alert sensitivity", http.StatusBadRequest)
		return
	}
	switch req.Settings.HighHomeLoadAlert {
	case "", "disabled", "low", "medium", "high":
	default:
		writeJSONError(w, "invalid high home load alert sensitivity", http.StatusBadRequest)
		return
	}
	for _, period := range req.Settings.QuietPeriods {
		for _, hp := range period.Hours {
			if hp.HourStart < 0 || hp.HourStart > 23 || hp.HourEnd < 0 || hp.HourEnd > 23 || hp.HourStart == hp.HourEnd {
				writeJSONError(w, "quiet period start and end hours must be between 0 and 23, and start cannot equal end", http.StatusBadRequest)
				return
			}
			if hp.MinuteStart < 0 || hp.MinuteStart > 59 || hp.MinuteEnd < 0 || hp.MinuteEnd > 59 {
				writeJSONError(w, "quiet period start and end minutes must be between 0 and 59", http.StatusBadRequest)
				return
			}
		}
	}

	ctx := r.Context()
	if err := s.storage.UpdateSiteNotificationSettings(ctx, siteID, userID, req.Settings); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update notification settings", slog.Any("error", err))
		writeJSONError(w, "failed to update notification settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// handleNotificationClick records click telemetry from the Service Worker.
func (s *Server) handleNotificationClick(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var siteID, month, logID string

	if id == "" {
		writeJSONError(w, "missing notification id", http.StatusBadRequest)
		return
	}
	m, sID, _, err := parseNotificationLogID(id)
	if err != nil {
		writeJSONError(w, "invalid notification id", http.StatusBadRequest)
		return
	}
	month = m
	siteID = sID
	logID = id

	ctx := r.Context()
	if err := s.storage.RecordNotificationClick(ctx, siteID, month, logID, s.now().UTC()); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to record notification click", slog.Any("error", err), slog.String("id", logID))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"recorded": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

type notificationSubscriptionsResponse struct {
	Subscriptions        []types.PushSubscription `json:"subscriptions"`
	NotificationsEnabled bool                     `json:"notificationsEnabled"`
}

func (s *Server) handleGetNotificationSubscriptions(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	subs := user.Subscriptions
	if subs == nil {
		subs = []types.PushSubscription{}
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(notificationSubscriptionsResponse{
		Subscriptions:        subs,
		NotificationsEnabled: s.notificationsEnabled(),
	}); err != nil {
		panic(http.ErrAbortHandler)
	}
}
