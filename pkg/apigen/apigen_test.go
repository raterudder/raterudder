package apigen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractInterfaceBlock(src, name string) string {
	header := "export interface " + name + " {\n"
	start := strings.Index(src, header)
	if start == -1 {
		return ""
	}
	end := strings.Index(src[start:], "\n}\n")
	if end == -1 {
		return ""
	}
	return src[start : start+end+3]
}

func TestGenerate(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	t.Run("ClosedModelsAndHiddenFields", func(t *testing.T) {
		out, err := Generate(repoRoot)
		require.NoError(t, err)
		src := string(out)

		assert.NotContains(t, src, "[key: string]: any")
		assert.NotContains(t, src, ": any")
		assert.NotContains(t, src, "FranklinCredentials")
		assert.NotContains(t, src, "NotificationLog")
		assert.NotContains(t, src, "MonthlyNotificationLogs")

		sysBlock := extractInterfaceBlock(src, "SystemStatus")
		require.NotEmpty(t, sysBlock)
		assert.Contains(t, sysBlock, "timestamp: string;")
		assert.Contains(t, sysBlock, "timeLocation?: string;")
		assert.Contains(t, sysBlock, "batterySOC: number;")
		assert.Contains(t, sysBlock, "batteryKW: number;")
		assert.Contains(t, sysBlock, "batteryCapacityKWH: number;")
		assert.Contains(t, sysBlock, "maxBatteryChargeKW: number;")
		assert.Contains(t, sysBlock, "maxBatteryDischargeKW: number;")
		assert.Contains(t, sysBlock, "solarKW: number;")
		assert.Contains(t, sysBlock, "gridKW: number;")
		assert.Contains(t, sysBlock, "homeKW: number;")
		assert.Contains(t, sysBlock, "elevatedMinBatterySOC: boolean;")
		assert.Contains(t, sysBlock, "batteryAboveMinSOC: boolean;")
		assert.Contains(t, sysBlock, "vppEvents?: VPPEvent[];")
		assert.Contains(t, sysBlock, "managedTouMode?: boolean;")
		assert.Contains(t, sysBlock, "batteryHeating?: boolean;")
		assert.Contains(t, sysBlock, "dcCoupled?: boolean;")

		uroBlock := extractInterfaceBlock(src, "UtilityRateOptions")
		require.NotEmpty(t, uroBlock)
		assert.Contains(t, uroBlock, "rateClass?: string;")
		assert.Contains(t, uroBlock, "variableDeliveryRate?: boolean;")
		assert.Contains(t, uroBlock, "netMeteringCredits?: boolean;")
		assert.Contains(t, uroBlock, "netMeteringScheme?: string;")
		assert.Contains(t, uroBlock, "peakPeriodOption?: string;")
		assert.Contains(t, uroBlock, "generationRate?: string;")
		assert.Contains(t, uroBlock, "location?: string;")
		assert.Contains(t, uroBlock, "evCredit?: boolean;")
		assert.Contains(t, uroBlock, "vppProgram?: string;")
		assert.Contains(t, uroBlock, "applicationYear?: string;")
	})

	t.Run("MissingFieldsAndFlattenedPayloads", func(t *testing.T) {
		out, err := Generate(repoRoot)
		require.NoError(t, err)
		src := string(out)

		assert.Contains(t, extractInterfaceBlock(src, "PriceInfo"), "provider: string;")
		assert.Contains(t, extractInterfaceBlock(src, "PriceInfo"), "periodName?: string;")
		assert.Contains(t, extractInterfaceBlock(src, "SimulationParams"), "detectedShift?: string;")
		assert.Contains(t, extractInterfaceBlock(src, "Action"), "strategyBenefitDollars?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "Action"), "failed?: boolean;")
		assert.Contains(t, extractInterfaceBlock(src, "Action"), "error?: string;")
		assert.Contains(t, extractInterfaceBlock(src, "Plan"), "shortPeriods?: ShortPlanPeriod[];")
		assert.Contains(t, extractInterfaceBlock(src, "SavingsStats"), "hourlyDebugging: HourlySavingsStatsDebugging[];")
		assert.Contains(t, extractInterfaceBlock(src, "TimePeriod"), "hoursNot?: boolean;")
		assert.Contains(t, extractInterfaceBlock(src, "TimePeriod"), "specificDatesNot?: boolean;")
		assert.Contains(t, extractInterfaceBlock(src, "Site"), "inviteCode: string;")
		assert.Contains(t, extractInterfaceBlock(src, "Site"), "permissions: SitePermissions[];")
		assert.Contains(t, extractInterfaceBlock(src, "AdminSite"), "inviteCode: string;")
		assert.Contains(t, extractInterfaceBlock(src, "AdminSite"), "permissions: SitePermissions[];")
		assert.Contains(t, extractInterfaceBlock(src, "EnergyStats"), "hadBatteryHeating?: boolean;")
		assert.Contains(t, extractInterfaceBlock(src, "EnergyStats"), "hadSolarNoExport?: boolean;")

		settingsBlock := extractInterfaceBlock(src, "Settings")
		require.NotEmpty(t, settingsBlock)
		assert.Contains(t, settingsBlock, "utilityFeesPeriods?: UtilityFeesPeriod[];")
		assert.Contains(t, settingsBlock, "updateGroup: number;")
		assert.Contains(t, settingsBlock, "hasCredentials: Record<string, boolean>;")
		assert.Contains(t, settingsBlock, "features: Record<string, boolean>;")
		assert.NotContains(t, settingsBlock, "encryptedCredentials")

		updateBlock := extractInterfaceBlock(src, "SettingsUpdateRequest")
		require.NotEmpty(t, updateBlock)
		assert.Contains(t, updateBlock, "dryRun: boolean;")
		assert.Contains(t, updateBlock, "credentials?: CredentialsPayload;")
		assert.Contains(t, updateBlock, "siteID?: string;")
		assert.NotContains(t, updateBlock, "hasCredentials")
		assert.NotContains(t, updateBlock, "features")
		assert.NotContains(t, updateBlock, "encryptedCredentials")
	})

	t.Run("OmitemptyAndNoPhantomForecastFields", func(t *testing.T) {
		out, err := Generate(repoRoot)
		require.NoError(t, err)
		src := string(out)

		assert.Contains(t, extractInterfaceBlock(src, "PriceInfo"), "gridUseDollarsPerKWH?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "PlanPeriod"), "gridImportKWH?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "PlanPeriod"), "gridExportKWH?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "PlanPeriod"), "costDollars?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "Plan"), "totalProjectedCost?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "Plan"), "totalExportCredits?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "Plan"), "netEconomicBenefit?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "Plan"), "periods?: PlanPeriod[];")
		assert.Contains(t, extractInterfaceBlock(src, "ESSAuthStatus"), "consecutiveFailures?: number;")
		assert.Contains(t, extractInterfaceBlock(src, "ESSAuthStatus"), "lastAttempt?: string;")
		assert.Contains(t, extractInterfaceBlock(src, "WeatherRes"), "irradiance?: number;")

		weatherBlock := extractInterfaceBlock(src, "WeatherRes")
		require.NotEmpty(t, weatherBlock)
		assert.NotContains(t, weatherBlock, "temperatureCellC")
		assert.NotContains(t, weatherBlock, "unclippedSolarGeneration")

		forecastBlock := extractInterfaceBlock(src, "ForecastResponse")
		require.NotEmpty(t, forecastBlock)
		assert.Contains(t, forecastBlock, "energyHistory: EnergyHistoryRes[];")
		assert.Contains(t, forecastBlock, "priceHistory: PriceHistoryRes[];")
		assert.Contains(t, forecastBlock, "updated: string;")
		assert.NotContains(t, forecastBlock, "weather")
	})

	t.Run("AlertThresholdsAndNotificationUnions", func(t *testing.T) {
		out, err := Generate(repoRoot)
		require.NoError(t, err)
		src := string(out)

		assert.Contains(t, src, "export const HIGH_HOME_LOAD_MIN_KW_LOW = 6.0;")
		assert.Contains(t, src, "export const HIGH_HOME_LOAD_MIN_KW_MEDIUM = 4.5;")
		assert.Contains(t, src, "export const HIGH_HOME_LOAD_MIN_KW_HIGH = 3.5;")
		assert.Contains(t, src, "export type MorningSummaryFlavor = 'home_planner' | 'executive' | 'pilot' | 'metrics_heavy';")
		assert.Contains(t, src, "export type AlertSensitivity = 'low' | 'medium' | 'high' | 'disabled';")
		assert.Contains(t, src, "export type AnomalyAlertSensitivity = '' | AlertSensitivity;")
	})

	t.Run("MissingRepoRootReturnsError", func(t *testing.T) {
		_, err := Generate(filepath.Join(t.TempDir(), "nonexistent"))
		require.Error(t, err)
	})
}

func TestWriteOutput(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	t.Run("WritesGeneratedFile", func(t *testing.T) {
		expected, err := Generate(repoRoot)
		require.NoError(t, err)

		tmpRoot := t.TempDir()
		for _, pkgDir := range []string{"pkg/types", "pkg/server", "pkg/feats"} {
			srcDir := filepath.Join(repoRoot, filepath.FromSlash(pkgDir))
			dstDir := filepath.Join(tmpRoot, filepath.FromSlash(pkgDir))
			require.NoError(t, os.MkdirAll(filepath.Dir(dstDir), 0o755))
			absSrc, err := filepath.Abs(srcDir)
			require.NoError(t, err)
			require.NoError(t, os.Symlink(absSrc, dstDir))
		}

		require.NoError(t, WriteOutput(tmpRoot))
		written, err := os.ReadFile(filepath.Join(tmpRoot, filepath.FromSlash(OutputRelPath)))
		require.NoError(t, err)
		assert.Equal(t, expected, written)
	})
}

func TestCheckFresh(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	t.Run("MatchesCommittedFile", func(t *testing.T) {
		targetPath := filepath.Join(repoRoot, filepath.FromSlash(OutputRelPath))
		err := CheckFresh(repoRoot, targetPath)
		require.NoError(t, err)
	})

	t.Run("RejectsStaleContract", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "apiTypes.gen.ts")
		err := os.WriteFile(tmpFile, []byte("// stale contract\nexport interface ForecastResponse {\n    weather?: WeatherRes[];\n}\n"), 0o644)
		require.NoError(t, err)

		err = CheckFresh(repoRoot, tmpFile)
		require.Error(t, err)
		assert.ErrorContains(t, err, "out of date")
	})

	t.Run("MissingTargetFileReturnsError", func(t *testing.T) {
		err := CheckFresh(repoRoot, filepath.Join(t.TempDir(), "missing.ts"))
		require.Error(t, err)
	})
}
