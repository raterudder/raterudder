package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/raterudder/raterudder/pkg/weather"
)

var errESSRateLimited = errors.New("ESS rate limited")

const (
	maxInteractiveESSBackoff   = 15 * time.Minute
	essFailurePauseThreshold   = 40
	defaultSettingsSyncTimeout = 30 * time.Second
)

func (s *Server) getSettingsSyncTimeout() time.Duration {
	if s.settingsSyncTimeout > 0 {
		return s.settingsSyncTimeout
	}
	return defaultSettingsSyncTimeout
}

func getInteractiveESSBackoff(failures int) time.Duration {
	if failures <= 1 {
		return 0
	}
	shift := min(failures-2, 10)
	backoff := time.Duration(30*(1<<shift)) * time.Second
	return min(backoff, maxInteractiveESSBackoff)
}

func nextESSFailureCount(prev int, err error) int {
	if errors.Is(err, ess.ErrAccountLocked) {
		return max(prev+1, essFailurePauseThreshold)
	}
	if errors.Is(err, ess.ErrUnauthorized) {
		if prev == 0 {
			return 1
		}
		return max(prev+1, essFailurePauseThreshold)
	}
	return prev + 1
}

func checkInteractiveESSRateLimit(authStatus *types.ESSAuthStatus, now time.Time) time.Duration {
	if authStatus.ConsecutiveUserFailures == 0 {
		return 0
	}
	backoff := getInteractiveESSBackoff(authStatus.ConsecutiveUserFailures)
	resetWindow := max(2*backoff, maxInteractiveESSBackoff)
	elapsed := now.Sub(authStatus.LastUserAttempt)
	if !authStatus.LastUserAttempt.IsZero() && elapsed > resetWindow {
		authStatus.ConsecutiveUserFailures = 0
		return 0
	}
	if authStatus.ConsecutiveUserFailures > 1 {
		if timeLeft := backoff - elapsed; timeLeft > 0 {
			return max(timeLeft.Round(time.Second), time.Second)
		}
	}
	return 0
}

func clientLocationErrorMessage(err error) string {
	if errors.Is(err, weather.ErrInvalidLocation) {
		return err.Error()
	}
	if errors.Is(err, weather.ErrNoLocationFound) || (strings.Contains(strings.ToLower(err.Error()), "no location found") && !strings.Contains(err.Error(), "://")) {
		return fmt.Sprintf("failed to fetch location data: %v", err)
	}
	return "failed to fetch location data"
}

type settingsWithVersion struct {
	types.Settings
	version   int
	updatedAt time.Time
}

func (s *Server) migrateAndDecryptSettings(ctx context.Context, siteID string, settings types.Settings, version int, updatedAt time.Time) (settingsWithVersion, types.Credentials, error) {
	sv := settingsWithVersion{
		Settings:  settings,
		version:   version,
		updatedAt: updatedAt,
	}

	var migrated bool
	// Check for migration
	if version < types.CurrentSettingsVersion {
		newSettings, changed, err := types.MigrateSettings(settings, version, s.release)
		if err != nil {
			// Log error but return settings as is (best effort)
			log.Ctx(ctx).ErrorContext(ctx, "failed to migrate settings", slog.Int("currentVersion", version), slog.Any("error", err))
		} else if changed {
			log.Ctx(ctx).InfoContext(ctx, "migrating settings", slog.Int("oldVersion", version), slog.Int("newVersion", types.CurrentSettingsVersion))
			sv.Settings = newSettings
			sv.version = types.CurrentSettingsVersion
			migrated = true
		}
	}

	var creds types.Credentials
	var credsMigrated bool
	if len(sv.Settings.EncryptedCredentials) > 0 {
		var needsUpgrade bool
		var err error
		creds, needsUpgrade, err = s.decryptCredentials(ctx, siteID, sv.Settings.EncryptedCredentials)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
			return settingsWithVersion{}, types.Credentials{}, err
		}
		if needsUpgrade {
			encrypted, err := s.encryptCredentials(ctx, siteID, creds)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to re-encrypt credentials during migration", slog.Any("error", err))
			} else {
				log.Ctx(ctx).InfoContext(ctx, "migrating encrypted credentials to v2")
				sv.Settings.EncryptedCredentials = encrypted
				credsMigrated = true
			}
		}
	}

	if migrated || credsMigrated {
		savedSettings, savedVersion, newUpdatedAt, err := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
			next := current
			nextVer := curVersion
			var save bool

			if nextVer < types.CurrentSettingsVersion {
				migratedCur, changed, migErr := types.MigrateSettings(next, nextVer, s.release)
				if migErr != nil {
					return current, curVersion, false, migErr
				}
				if changed {
					next = migratedCur
					nextVer = types.CurrentSettingsVersion
					save = true
				}
			}

			if len(next.EncryptedCredentials) > 0 {
				curCreds, curNeedsUpgrade, decErr := s.decryptCredentials(ctx, siteID, next.EncryptedCredentials)
				if decErr == nil && curNeedsUpgrade {
					if encrypted, encErr := s.encryptCredentials(ctx, siteID, curCreds); encErr == nil {
						next.EncryptedCredentials = encrypted
						save = true
					}
				}
			}

			return next, nextVer, save, nil
		})
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to save migrated settings", slog.Any("error", err))
			// Return migrated settings even if save failed, so current request works with new defaults
		} else {
			log.Ctx(ctx).InfoContext(ctx, "saved migrated settings", slog.Int("oldVersion", version), slog.Int("newVersion", savedVersion), slog.Bool("credsMigrated", credsMigrated))
			sv.Settings = savedSettings
			sv.version = savedVersion
			sv.updatedAt = newUpdatedAt
			if len(savedSettings.EncryptedCredentials) > 0 && !bytes.Equal(savedSettings.EncryptedCredentials, settings.EncryptedCredentials) {
				if latestCreds, _, decErr := s.decryptCredentials(ctx, siteID, savedSettings.EncryptedCredentials); decErr == nil {
					creds = latestCreds
				}
			}
		}
	}

	return sv, creds, nil
}

func equalProviderCredentials(essName string, a, b types.Credentials) bool {
	switch essName {
	case "tesla":
		if (a.Tesla == nil) != (b.Tesla == nil) {
			return false
		}
		if a.Tesla == nil {
			return true
		}
		return a.Tesla.AuthCode == b.Tesla.AuthCode &&
			a.Tesla.Region == b.Tesla.Region &&
			a.Tesla.AccessToken == b.Tesla.AccessToken &&
			a.Tesla.RefreshToken == b.Tesla.RefreshToken &&
			a.Tesla.Expiry.Equal(b.Tesla.Expiry) &&
			a.Tesla.EnergySiteID == b.Tesla.EnergySiteID &&
			a.Tesla.SerialNumber == b.Tesla.SerialNumber
	case "franklin":
		if (a.Franklin == nil) != (b.Franklin == nil) {
			return false
		}
		if a.Franklin == nil {
			return true
		}
		return *a.Franklin == *b.Franklin
	case "enphase":
		if (a.Enphase == nil) != (b.Enphase == nil) {
			return false
		}
		if a.Enphase == nil {
			return true
		}
		return *a.Enphase == *b.Enphase
	case "mock":
		if (a.Mock == nil) != (b.Mock == nil) {
			return false
		}
		if a.Mock == nil {
			return true
		}
		return *a.Mock == *b.Mock
	default:
		return reflect.DeepEqual(a, b)
	}
}

func (s *Server) decryptCredentialsQuiet(ctx context.Context, siteID string, encrypted []byte) (types.Credentials, error) {
	if len(s.encryptionKey) != 32 {
		return types.Credentials{}, errors.New("encryption key not configured")
	}
	quietCtx := log.With(ctx, slog.New(slog.DiscardHandler))
	creds, _, err := s.decryptCredentials(quietCtx, siteID, encrypted)
	return creds, err
}

func (s *Server) areProviderCredentialsSuperseded(
	ctx context.Context,
	siteID string,
	targetESS string,
	origEncrypted []byte,
	currentEncrypted []byte,
	origCreds types.Credentials,
	newCreds types.Credentials,
) bool {
	if bytes.Equal(origEncrypted, currentEncrypted) || (len(origEncrypted) == 0 && len(currentEncrypted) == 0) {
		return false
	}
	if len(origEncrypted) > 0 && len(currentEncrypted) == 0 {
		return true
	}
	curCreds, err := s.decryptCredentialsQuiet(ctx, siteID, currentEncrypted)
	if err != nil {
		return false
	}
	if !equalProviderCredentials(targetESS, origCreds, curCreds) {
		return true
	}
	if targetESS == "tesla" && newCreds.Tesla != nil && curCreds.Tesla != nil && curCreds.Tesla.Expiry.After(newCreds.Tesla.Expiry) {
		return true
	}
	return false
}

func (s *Server) mergeProviderCredsOntoEncrypted(
	ctx context.Context,
	siteID string,
	currentEncrypted []byte,
	fallbackCreds types.Credentials,
	updatedCreds types.Credentials,
	essName string,
) ([]byte, error) {
	baseCreds := fallbackCreds
	if len(currentEncrypted) > 0 {
		dec, _, err := s.decryptCredentials(ctx, siteID, currentEncrypted)
		if err != nil {
			return nil, err
		}
		baseCreds = dec
	}
	savedCreds := mergeUpdatedProviderCredentials(baseCreds, updatedCreds, essName)
	return s.encryptCredentials(ctx, siteID, savedCreds)
}

func (s *Server) getSettingsWithMigration(ctx context.Context, siteID string) (settingsWithVersion, types.Credentials, error) {
	settings, version, updatedAt, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		return settingsWithVersion{}, types.Credentials{}, err
	}
	return s.migrateAndDecryptSettings(ctx, siteID, settings, version, updatedAt)
}

func (s *Server) getESSSystem(ctx context.Context, siteID string, settings settingsWithVersion, creds types.Credentials) (ess.System, settingsWithVersion, error) {
	failures := settings.ESSAuthStatus.ConsecutiveFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - s.now().Sub(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			// Round to seconds
			timeLeft = max(timeLeft.Round(time.Second), time.Second)
			return nil, settings, fmt.Errorf("%w, try again in %v", errESSRateLimited, timeLeft)
		}
	}

	essSystem, err := s.ess.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, settings, fmt.Errorf("failed to get ESS system: %w", err)
	}

	targetESS := settings.ESS
	origEncrypted := settings.EncryptedCredentials

	// and apply those settings to the ESS
	newCreds, updated, err := essSystem.Authenticate(ctx, creds)
	now := s.now().UTC()
	if err != nil {
		var enc []byte
		if updated {
			log.Ctx(ctx).DebugContext(ctx, "credentials updated by ess system before error")
			var encErr error
			if enc, encErr = s.encryptCredentials(ctx, siteID, newCreds); encErr != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", encErr))
			} else {
				settings.EncryptedCredentials = enc
			}
		}
		settings.ESSAuthStatus.ConsecutiveFailures = nextESSFailureCount(settings.ESSAuthStatus.ConsecutiveFailures, err)
		settings.ESSAuthStatus.LastAttempt = now

		saved, savedVer, newUpdatedAt, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
			if current.ESS != targetESS {
				log.Ctx(ctx).InfoContext(ctx, "aborting ess auth write-back because ess provider changed concurrently",
					slog.String("expectedESS", targetESS),
					slog.String("currentESS", current.ESS),
				)
				return current, curVersion, false, nil
			}
			if s.areProviderCredentialsSuperseded(ctx, siteID, targetESS, origEncrypted, current.EncryptedCredentials, creds, newCreds) {
				log.Ctx(ctx).InfoContext(ctx, "aborting ess auth write-back because credentials changed concurrently",
					slog.String("ess", targetESS),
				)
				return current, curVersion, false, nil
			}
			next := current
			next.ESSAuthStatus.ConsecutiveFailures = nextESSFailureCount(current.ESSAuthStatus.ConsecutiveFailures, err)
			next.ESSAuthStatus.LastAttempt = now
			if len(enc) > 0 {
				if bytes.Equal(origEncrypted, current.EncryptedCredentials) {
					next.EncryptedCredentials = enc
				} else if mergedEnc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, creds, newCreds, targetESS); encErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt merged credentials", slog.Any("error", encErr))
				} else {
					next.EncryptedCredentials = mergedEnc
				}
			}
			ver := curVersion
			if ver == 0 {
				ver = settings.version
			}
			return next, ver, true, nil
		})
		if dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		} else {
			settings.Settings = saved
			settings.version = savedVer
			settings.updatedAt = newUpdatedAt
		}
		return nil, settings, fmt.Errorf("failed to apply settings: %w", err)
	}

	authStatusChanged := settings.ESSAuthStatus.ConsecutiveFailures > 0
	if authStatusChanged {
		settings.ESSAuthStatus.ConsecutiveFailures = 0
		settings.ESSAuthStatus.LastAttempt = now
	}

	var enc []byte
	var encFailed bool
	if updated {
		log.Ctx(ctx).DebugContext(ctx, "credentials updated by ess system")
		var encErr error
		enc, encErr = s.encryptCredentials(ctx, siteID, newCreds)
		if encErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", encErr))
			encFailed = true
		} else {
			settings.EncryptedCredentials = enc
		}
	}

	if (updated && !encFailed) || authStatusChanged {
		saved, savedVer, newUpdatedAt, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
			if current.ESS != targetESS {
				log.Ctx(ctx).InfoContext(ctx, "aborting ess credential write-back because ess provider changed concurrently",
					slog.String("expectedESS", targetESS),
					slog.String("currentESS", current.ESS),
				)
				return current, curVersion, false, nil
			}
			skipCredWrite := false
			if !bytes.Equal(origEncrypted, current.EncryptedCredentials) {
				if len(origEncrypted) > 0 && len(current.EncryptedCredentials) == 0 {
					log.Ctx(ctx).InfoContext(ctx, "aborting ess credential write-back because credentials changed concurrently",
						slog.String("ess", targetESS),
					)
					return current, curVersion, false, nil
				}
				if curCreds, decErr := s.decryptCredentialsQuiet(ctx, siteID, current.EncryptedCredentials); decErr == nil {
					if !equalProviderCredentials(targetESS, creds, curCreds) {
						if updated && equalProviderCredentials(targetESS, newCreds, curCreds) {
							skipCredWrite = true
						} else {
							log.Ctx(ctx).InfoContext(ctx, "aborting ess credential write-back because credentials changed concurrently",
								slog.String("ess", targetESS),
							)
							return current, curVersion, false, nil
						}
					} else if targetESS == "tesla" && newCreds.Tesla != nil && curCreds.Tesla != nil && curCreds.Tesla.Expiry.After(newCreds.Tesla.Expiry) {
						log.Ctx(ctx).InfoContext(ctx, "aborting ess credential write-back because credentials changed concurrently",
							slog.String("ess", targetESS),
						)
						return current, curVersion, false, nil
					}
				}
			}
			next := current
			changed := false
			if next.ESSAuthStatus.ConsecutiveFailures > 0 {
				next.ESSAuthStatus.ConsecutiveFailures = 0
				next.ESSAuthStatus.LastAttempt = now
				changed = true
			}
			if len(enc) > 0 && !skipCredWrite {
				if bytes.Equal(origEncrypted, current.EncryptedCredentials) {
					next.EncryptedCredentials = enc
					changed = true
				} else if mergedEnc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, creds, newCreds, targetESS); encErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt merged credentials", slog.Any("error", encErr))
				} else {
					next.EncryptedCredentials = mergedEnc
					changed = true
				}
			}
			if !changed {
				return current, curVersion, false, nil
			}
			ver := curVersion
			if ver == 0 {
				ver = settings.version
			}
			return next, ver, true, nil
		})
		if dbErr != nil {
			if updated && !encFailed {
				log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", dbErr))
			} else {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
			}
		} else {
			settings.Settings = saved
			settings.version = savedVer
			settings.updatedAt = newUpdatedAt
		}
	}

	return essSystem, settings, nil
}

// SettingsRes is the response type for GetSettings
type SettingsRes struct {
	types.Settings
	HasCredentials map[string]bool `json:"hasCredentials"`
	Features       map[string]bool `json:"features"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	// remove encrypted credentials from response
	settings.EncryptedCredentials = nil

	resp := SettingsRes{
		Settings:       settings.Settings,
		HasCredentials: creds.Has(),
		Features:       feats.All(ctx),
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// Validate Authentication from Context (set by authMiddleware)
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	if !user.Admin {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized for settings update", slog.String("userID", user.ID), slog.String("email", user.Email))
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		types.Settings
		Credentials *types.Credentials `json:"credentials,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to decode settings", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	newSettings := req.Settings

	// if a utility switched from netMeteringCredits switch to a netMeteringScheme select
	// we need to make sure we are unsetting netMeteringCredits
	if newSettings.UtilityRateOptions.NetMeteringScheme != "" {
		newSettings.UtilityRateOptions.NetMeteringCredits = false
	}

	if newSettings.MinArbitrageDifferenceDollarsPerKWH < 0 {
		writeJSONError(w, "minimum arbitrage difference cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.MinBatteryExportDifferenceDollarsPerKWH < 0 {
		writeJSONError(w, "minimum battery export difference cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.MinBatterySOC < 0 || newSettings.MinBatterySOC > 100 {
		writeJSONError(w, "minimum battery SOC must be between 0 and 100", http.StatusBadRequest)
		return
	}

	if len(newSettings.MinBatterySOCPeriods) > 0 {
		minSOC, err := s.validateAndCalculateMinBatterySOCPeriods(ctx, siteID, newSettings)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		newSettings.MinBatterySOC = minSOC
	}

	if newSettings.SolarBellCurveMultiplier < 0 {
		writeJSONError(w, "solar bell curve multiplier cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SolarTrendRatioMax < 1 {
		writeJSONError(w, "solar trend ratio max must be at least 1", http.StatusBadRequest)
		return
	}
	if err := weather.ValidateLocationInput(newSettings.CountryCode, newSettings.PostalCode); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "invalid location settings", slog.Any("error", err))
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	// this should never really happen in practice but makes tests easier
	if newSettings.Release == "" {
		newSettings.Release = s.release
	}
	if newSettings.Release != s.release {
		log.Ctx(ctx).WarnContext(ctx,
			"settings release mismatch",
			slog.String("release", newSettings.Release),
		)
		writeJSONError(w, "settings release mismatch", http.StatusBadRequest)
		return
	}

	var wg sync.WaitGroup
	var updatedWeather, updatedEnergy bool
	// normally we would just do defer wg.Wait() but then we write out the response
	// before we actually finish which makes tests difficult and also could cause
	// cloud run to shutdown before we're done
	var finishedWaiting bool
	defer func() {
		if !finishedWaiting {
			wg.Wait()
		}
	}()

	// do this early in case the utility options are invalid
	var u utility.Utility
	if newSettings.UtilityProvider != "" {
		var err error
		u, err = s.utilities.Site(ctx, siteID, newSettings)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get utility provider", slog.String("utilityProvider", newSettings.UtilityProvider), slog.Any("error", err))
			writeJSONError(w, fmt.Sprintf("invalid utility provider settings: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Get existing credentials with migration to preserve other fields
	existingSV, existingCreds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	existing := existingSV.Settings

	// Preserve existing auth status, encrypted credentials, notifications, and update group by default
	newSettings.ESSAuthStatus = existing.ESSAuthStatus
	newSettings.EncryptedCredentials = existing.EncryptedCredentials
	newSettings.Notifications = existing.Notifications
	newSettings.UpdateGroup = existing.UpdateGroup

	// Update Location if zip/country changed
	var fetchedNewLocation bool
	if newSettings.PostalCode != "" && newSettings.CountryCode != "" {
		if existing.PostalCode != newSettings.PostalCode || existing.CountryCode != newSettings.CountryCode || existing.Location == nil {
			log.Ctx(ctx).InfoContext(
				ctx,
				"location changed, fetching new location data",
				slog.String("postalCode", newSettings.PostalCode),
				slog.String("countryCode", newSettings.CountryCode),
			)
			loc, err := s.weather.Location(ctx, newSettings.CountryCode, newSettings.PostalCode)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to fetch location data", slog.Any("error", err))
				writeJSONError(w, clientLocationErrorMessage(err), http.StatusBadRequest)
				return
			}
			newSettings.Location = &loc
			fetchedNewLocation = true

			updatedWeather = true
			wg.Go(func() {
				log.Ctx(ctx).InfoContext(ctx, "fetching initial weather for new location")
				ctx, done := context.WithTimeout(context.WithoutCancel(ctx), s.getSettingsSyncTimeout())
				defer done()
				if err := s.updateWeatherHistory(ctx, siteID, loc); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to sync weather history after settings update", slog.Any("error", err))
				}
			})
		} else {
			newSettings.Location = existing.Location
		}
	} else {
		newSettings.Location = nil
	}

	// If the existing settings already have custom grid settings and the ESS provider didn't change, preserve it
	if existing.CustomGridSettings && existing.ESS == newSettings.ESS {
		newSettings.CustomGridSettings = true
	}

	var credsChanged bool
	var changedESS bool
	var authCheckNow, authDoneNow time.Time
	// Handle credentials or ESS update
	if req.Credentials != nil || existing.ESS != newSettings.ESS {

		// check if ESS changed
		var shouldBackfillHistory bool
		if existing.ESS != newSettings.ESS {
			changedESS = true
			shouldBackfillHistory = true
		}
		origCreds := existingCreds
		candidateCreds, credChanged, credBackfill := mergeSettingsCredentials(existingCreds, newSettings.ESS, req.Credentials)
		if credChanged {
			changedESS = true
		}
		if credBackfill {
			shouldBackfillHistory = true
		}
		existingCreds = candidateCreds

		// if the ess credentials changed, we need to verify them and potentially backfill history
		log.Ctx(ctx).InfoContext(
			ctx,
			"ess credentials changed, verifying and potentially backfilling history",
			slog.Bool("changedESS", changedESS),
			slog.Bool("shouldBackfillHistory", shouldBackfillHistory),
		)
		if changedESS {
			// this is an expensive call since it will apply the settings too which means
			// it might log into the ess
			essSystem, err := s.ess.Site(ctx, siteID, newSettings)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to get ess system: %v", err), http.StatusInternalServerError)
				return
			}

			authCheckNow = s.now().UTC()
			if timeLeft := checkInteractiveESSRateLimit(&newSettings.ESSAuthStatus, authCheckNow); timeLeft > 0 {
				writeJSONError(w, fmt.Sprintf("ESS rate limited, try again in %v", timeLeft), http.StatusTooManyRequests)
				return
			}
			existing.ESSAuthStatus.ConsecutiveUserFailures = newSettings.ESSAuthStatus.ConsecutiveUserFailures

			// Verify and update credentials
			var credsUpdated bool
			existingCreds, credsUpdated, err = essSystem.Authenticate(ctx, existingCreds)
			authDoneNow = s.now().UTC()
			if err != nil {
				if errors.Is(err, ess.ErrNeedsNextStage) {
					log.Ctx(ctx).InfoContext(ctx, "ess credentials require next stage verification", slog.String("ess", newSettings.ESS))
					if credsUpdated {
						_, _, _, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
							enc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, origCreds, existingCreds, newSettings.ESS)
							if encErr != nil {
								log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt stage credentials", slog.Any("error", encErr))
								return current, curVersion, false, nil
							}
							next := current
							next.EncryptedCredentials = enc
							ver := curVersion
							if ver == 0 {
								ver = existingSV.version
							}
							return next, ver, true, nil
						})
						if dbErr != nil {
							log.Ctx(ctx).ErrorContext(ctx, "failed to save stage credentials", slog.Any("error", dbErr))
						}
					}
					writeJSONError(w, "A verification code was sent to your email. Please enter the code and try again.", http.StatusBadRequest)
					return
				}
				log.Ctx(ctx).WarnContext(ctx, "failed to verify ess credentials", slog.Any("error", err))

				// purposefully store the existing settings with the auth status updated
				// and NOT the new settings since the credentials were not verified, while
				// preserving preexisting credentials for another provider and saving any
				// rotated credentials for the target provider.
				var credsSaved bool
				_, _, _, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
					next := current
					credsSaved = false
					if credsUpdated {
						if enc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, origCreds, existingCreds, newSettings.ESS); encErr != nil {
							log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt rotated credentials", slog.Any("error", encErr))
						} else {
							next.EncryptedCredentials = enc
							credsSaved = true
						}
					}
					checkInteractiveESSRateLimit(&next.ESSAuthStatus, authCheckNow)
					next.ESSAuthStatus.ConsecutiveFailures++
					next.ESSAuthStatus.ConsecutiveUserFailures++
					next.ESSAuthStatus.LastAttempt = authDoneNow
					next.ESSAuthStatus.LastUserAttempt = authDoneNow
					ver := curVersion
					if ver == 0 {
						ver = existingSV.version
					}
					return next, ver, true, nil
				})
				if dbErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
					credsSaved = false
				}
				if credsSaved {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					if encErr := json.NewEncoder(w).Encode(struct {
						Error            string `json:"error"`
						CredentialsSaved bool   `json:"credentialsSaved,omitempty"`
					}{
						Error:            fmt.Sprintf("failed to verify ess credentials: %v", err),
						CredentialsSaved: true,
					}); encErr != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to write error response", slog.Any("error", encErr))
						panic(http.ErrAbortHandler)
					}
					return
				}
				writeJSONError(w, fmt.Sprintf("failed to verify ess credentials: %v", err), http.StatusBadRequest)
				return
			}

			// If grid settings have not been manually customized, pull them from the ESS
			if !newSettings.CustomGridSettings {
				if gridSettings, err := essSystem.GridSettings(ctx); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to get grid settings from ESS", slog.Any("error", err))
				} else {
					log.Ctx(ctx).InfoContext(ctx, "synced grid settings from ESS",
						slog.Bool("gridChargeBatteries", gridSettings.GridChargeBatteries),
						slog.Bool("gridExportSolar", gridSettings.GridExportSolar),
						slog.Bool("gridExportBatteries", gridSettings.GridExportBatteries),
					)
					newSettings.GridChargeBatteries = gridSettings.GridChargeBatteries
					newSettings.GridExportSolar = gridSettings.GridExportSolar
					newSettings.GridExportBatteries = gridSettings.GridExportBatteries
					if (newSettings.ESS == "tesla" || newSettings.ESS == "franklin" || (newSettings.ESS == "enphase" && ess.EnphaseExportSchedulesEnabled(ctx))) &&
						(gridSettings.GridExportSolar || gridSettings.GridExportBatteries) {
						newSettings.ManageTOUSchedules = true
					}
				}
			}

			// now backfill if we need to since the credentials were verified
			if shouldBackfillHistory {
				// this goroutine is okay because we use a waitgroup to block the return
				// of the call until it finishes
				updatedEnergy = true
				wg.Go(func() {
					log.Ctx(ctx).InfoContext(ctx, "backfilling energy history for new credentials")
					ctx, done := context.WithTimeout(context.WithoutCancel(ctx), s.getSettingsSyncTimeout())
					defer done()
					if err := s.updateEnergyHistory(ctx, siteID, essSystem); err != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to sync energy history after settings update", slog.Any("error", err))
					}
				})
			}
			credsChanged = true
		}
	}

	var encryptedCreds []byte
	if credsChanged {
		// store the existing credentials with the new ones updated in-place
		var err error
		encryptedCreds, err = s.encryptCredentials(ctx, siteID, existingCreds)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", err))
			writeJSONError(w, "failed to encrypt credentials", http.StatusInternalServerError)
			return
		}
	}

	// we want this to be low down at the bottom because it will re-fetch prices
	// and start storing data into firestore which we want to minimize if other
	// errors will happen earlier
	if newSettings.UtilityProvider != "" && (existing.UtilityProvider != newSettings.UtilityProvider ||
		existing.UtilityRate != newSettings.UtilityRate ||
		existing.UtilityRateOptions != newSettings.UtilityRateOptions ||
		!reflect.DeepEqual(existing.UtilityFeesPeriods, newSettings.UtilityFeesPeriods)) {
		wg.Go(func() {
			log.Ctx(ctx).InfoContext(
				ctx,
				"utility settings changed, re-fetching prices",
				slog.String("utilityProvider", newSettings.UtilityProvider),
				slog.String("utilityRate", newSettings.UtilityRate),
			)
			ctx, done := context.WithTimeout(context.WithoutCancel(ctx), s.getSettingsSyncTimeout())
			defer done()
			if err := s.updatePriceHistory(ctx, siteID, u, true); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to update price history after settings change", slog.Any("error", err))
			}
		})
	}

	defaultComEdGroup := rand.IntN(4) + 13
	defaultTOUGroup := rand.IntN(12) + 1
	var beforeSettings types.Settings

	savedSettings, _, updatedAt, err := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
		if curVersion > 0 && curVersion < types.CurrentSettingsVersion {
			if migratedCur, changed, migErr := types.MigrateSettings(current, curVersion, s.release); migErr == nil && changed {
				current = migratedCur
			}
		}
		beforeSettings = current
		merged := mergeAdminSettingsUpdate(
			current,
			newSettings,
			fetchedNewLocation,
			credsChanged,
			encryptedCreds,
			authCheckNow,
			authDoneNow,
			defaultComEdGroup,
			defaultTOUGroup,
		)
		return merged, types.CurrentSettingsVersion, true, nil
	})
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", err))
		writeJSONError(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).InfoContext(
		ctx,
		"settings updated",
		slog.Any("before", beforeSettings),
		slog.Any("after", savedSettings),
	)

	if beforeSettings.ESS == "" && savedSettings.ESS != "" {
		log.Ctx(ctx).InfoContext(ctx, "ess connected",
			slog.String("event", eventESSConnected),
			slog.String("siteID", siteID),
			slog.String("userID", user.ID),
			slog.String("ess", savedSettings.ESS),
		)
	}
	if beforeSettings.UtilityProvider == "" && savedSettings.UtilityProvider != "" {
		log.Ctx(ctx).InfoContext(ctx, "utility configured",
			slog.String("event", eventUtilityConfigured),
			slog.String("siteID", siteID),
			slog.String("userID", user.ID),
			slog.String("utilityProvider", savedSettings.UtilityProvider),
			slog.String("utilityRate", savedSettings.UtilityRate),
		)
	}
	if savedSettings.ESS != "" {
		if !beforeSettings.Pause && savedSettings.Pause {
			log.Ctx(ctx).InfoContext(ctx, "site automation paused",
				slog.String("event", eventSiteAutomationPaused),
				slog.String("siteID", siteID),
				slog.String("userID", user.ID),
			)
		} else if beforeSettings.Pause && !savedSettings.Pause {
			log.Ctx(ctx).InfoContext(ctx, "site automation resumed",
				slog.String("event", eventSiteAutomationResumed),
				slog.String("siteID", siteID),
				slog.String("userID", user.ID),
			)
		}
	}

	if !beforeSettings.Pause && savedSettings.Pause && savedSettings.ManageTOUSchedules {
		credsForPause := existingCreds
		if !credsChanged && len(savedSettings.EncryptedCredentials) > 0 && !bytes.Equal(savedSettings.EncryptedCredentials, existing.EncryptedCredentials) {
			if latestCreds, _, decErr := s.decryptCredentials(ctx, siteID, savedSettings.EncryptedCredentials); decErr == nil {
				credsForPause = latestCreds
			}
		}
		s.revertManagedTOUModeOnPause(ctx, siteID, settingsWithVersion{
			Settings:  savedSettings,
			version:   types.CurrentSettingsVersion,
			updatedAt: updatedAt,
		}, credsForPause)
	}

	if beforeSettings.UtilityProvider == "" && savedSettings.UtilityProvider != "" && user.Email != "" {
		if err := s.storage.DeleteInterest(ctx, user.Email); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to automatically clear interest submission", slog.String("email", user.Email), slog.Any("error", err))
		} else {
			log.Ctx(ctx).DebugContext(ctx, "automatically cleared interest submission", slog.String("email", user.Email))
		}
	}

	wg.Wait()
	finishedWaiting = true

	if updatedWeather || updatedEnergy {
		log.Ctx(ctx).InfoContext(ctx, "triggering history summary update after settings backfill")
		ctx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer done()
		if _, err := s.backfillHistorySummaries(ctx, siteID, s.now()); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to backfill history summaries after settings update", slog.Any("error", err))
		}
	}

	w.WriteHeader(http.StatusOK)
}

func mergeAdminSettingsUpdate(
	current types.Settings,
	edited types.Settings,
	fetchedNewLocation bool,
	credsChanged bool,
	encryptedCreds []byte,
	authCheckNow time.Time,
	authDoneNow time.Time,
	defaultComEdGroup int,
	defaultTOUGroup int,
) types.Settings {
	merged := edited

	if current.CustomGridSettings && current.ESS == edited.ESS {
		merged.CustomGridSettings = true
	}

	if edited.PostalCode == "" || edited.CountryCode == "" {
		merged.Location = nil
	} else if fetchedNewLocation {
		merged.Location = edited.Location
	} else if current.PostalCode == edited.PostalCode && current.CountryCode == edited.CountryCode && current.Location != nil {
		merged.Location = current.Location
	} else {
		merged.Location = edited.Location
	}

	merged.Notifications = current.Notifications
	merged.EncryptedCredentials = current.EncryptedCredentials
	merged.ESSAuthStatus = current.ESSAuthStatus
	merged.UpdateGroup = current.UpdateGroup

	if credsChanged {
		merged.EncryptedCredentials = encryptedCreds
		checkInteractiveESSRateLimit(&merged.ESSAuthStatus, authCheckNow)
		if merged.ESSAuthStatus.ConsecutiveFailures > 0 || merged.ESSAuthStatus.ConsecutiveSetFailures > 0 || merged.ESSAuthStatus.ConsecutiveUserFailures > 0 {
			if merged.ESS != "mock" || current.ESS == "" || current.ESS == "mock" {
				merged.ESSAuthStatus.ConsecutiveUserFailures = 0
				merged.ESSAuthStatus.LastUserAttempt = authDoneNow
			}
			merged.ESSAuthStatus.ConsecutiveFailures = 0
			merged.ESSAuthStatus.ConsecutiveSetFailures = 0
			merged.ESSAuthStatus.LastAttempt = authDoneNow
		}
	}

	// setting UpdateGroup means it's ready for updates which we only do if the
	// ess and utility are set and they're both validated
	eligible := merged.ESS != "" && merged.UtilityProvider != "" && (credsChanged || len(merged.EncryptedCredentials) > 0)
	if eligible {
		isComEd := isComEdHourly(merged.UtilityProvider, merged.UtilityRate)
		if isComEd && (merged.UpdateGroup < 13 || merged.UpdateGroup > 16) {
			merged.UpdateGroup = defaultComEdGroup
		} else if !isComEd && (merged.UpdateGroup < 1 || merged.UpdateGroup > 12) {
			merged.UpdateGroup = defaultTOUGroup
		}
	}

	return merged
}

func (s *Server) revertManagedTOUModeOnPause(ctx context.Context, siteID string, settings settingsWithVersion, creds types.Credentials) {
	if s.ess == nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to revert managed TOU mode on pause: ess map is nil")
		return
	}
	essSystem, settings, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) || errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).WarnContext(ctx, "failed to get ESS system to revert managed TOU mode on pause", slog.Any("error", err))
		} else {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get ESS system to revert managed TOU mode on pause", slog.Any("error", err))
		}
		return
	}

	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) {
			log.Ctx(ctx).WarnContext(ctx, "failed to get ESS status to revert managed TOU mode on pause", slog.Any("error", err))
		} else {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get ESS status to revert managed TOU mode on pause", slog.Any("error", err))
		}
		return
	}

	if !status.ManagedTOUMode {
		return
	}

	minSOC := int(math.Round(settings.MinBatterySOC))
	if _, err := s.setESSModes(ctx, siteID, essSystem, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{
		MinimumSOC: minSOC,
	}, settings); err != nil {
		if errors.Is(err, ess.ErrUnauthorized) || errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).WarnContext(ctx, "failed to set modes back to self-consumption on pause", slog.Any("error", err))
		} else {
			log.Ctx(ctx).ErrorContext(ctx, "failed to set modes back to self-consumption on pause", slog.Any("error", err))
		}
	}
}

func getESSBackoff(failures int) time.Duration {
	if failures <= 1 {
		return 0
	}
	if failures >= essFailurePauseThreshold {
		return 365 * 24 * time.Hour // stop trying
	}
	if failures >= 22 {
		return 12 * time.Hour
	}
	if failures >= 13 {
		return 2 * time.Hour
	}
	if failures >= 10 {
		return time.Hour
	}
	if failures >= 5 {
		return 45 * time.Minute
	}
	return 30 * time.Minute
}

func (s *Server) handleESSStage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	if !user.Admin {
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		ESS         string             `json:"ess"`
		Credentials *types.Credentials `json:"credentials,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to decode ess stage request", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ESS == "" {
		writeJSONError(w, "missing ess", http.StatusBadRequest)
		return
	}

	tempSettings := types.Settings{
		ESS: req.ESS,
	}

	existingRaw, version, updatedAt, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	now := s.now().UTC()
	authCheckNow := now
	rateLimitStatus := existingRaw.ESSAuthStatus
	if timeLeft := checkInteractiveESSRateLimit(&rateLimitStatus, authCheckNow); timeLeft > 0 {
		writeJSONError(w, fmt.Sprintf("ESS rate limited, try again in %v", timeLeft), http.StatusTooManyRequests)
		return
	}

	existingSV, existingCreds, err := s.migrateAndDecryptSettings(ctx, siteID, existingRaw, version, updatedAt)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
		writeJSONError(w, "failed to decrypt credentials", http.StatusInternalServerError)
		return
	}
	existing := existingSV.Settings

	origCreds := existingCreds
	switch req.ESS {
	case "enphase":
		if req.Credentials != nil && req.Credentials.Enphase != nil {
			existingCreds.Enphase = mergeEnphaseCredentials(existingCreds.Enphase, req.Credentials.Enphase)
			// Stage 0 requests (no OTP code and no password) explicitly request a fresh OTP email;
			// clear any previously cached session so expired OTP-only sessions don't fail with 401.
			if req.Credentials.Enphase.Code == "" && req.Credentials.Enphase.Password == "" && existingCreds.Enphase != nil {
				existingCreds.Enphase.SessionID = ""
				existingCreds.Enphase.ManagerToken = ""
			}
		}
		existingCreds.Franklin = nil
		existingCreds.Mock = nil
		existingCreds.Tesla = nil
	case "franklin":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for franklin")
		writeJSONError(w, "Franklin does not support stages", http.StatusBadRequest)
		return
	case "mock":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for mock")
		writeJSONError(w, "Mock does not support stages", http.StatusBadRequest)
		return
	case "tesla":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for tesla")
		writeJSONError(w, "Tesla does not support stages", http.StatusBadRequest)
		return
	default:
		log.Ctx(ctx).ErrorContext(ctx, "unknown ess", slog.String("ess", req.ESS))
		writeJSONError(w, "unknown ess", http.StatusBadRequest)
		return
	}

	essSystem, err := s.ess.Site(ctx, siteID, tempSettings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
		writeJSONError(w, fmt.Sprintf("failed to get ess system: %v", err), http.StatusInternalServerError)
		return
	}

	newCreds, updated, err := essSystem.Authenticate(ctx, existingCreds)
	now = s.now().UTC()

	// its expected that this returns needs next stage if there is one
	if err != nil && !errors.Is(err, ess.ErrNeedsNextStage) {
		_, _, _, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
			next := current
			if updated {
				if enc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, origCreds, newCreds, req.ESS); encErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt rotated stage credentials", slog.Any("error", encErr))
				} else {
					next.EncryptedCredentials = enc
				}
			}
			checkInteractiveESSRateLimit(&next.ESSAuthStatus, authCheckNow)
			next.ESSAuthStatus.ConsecutiveFailures++
			next.ESSAuthStatus.ConsecutiveUserFailures++
			next.ESSAuthStatus.LastAttempt = now
			next.ESSAuthStatus.LastUserAttempt = now
			ver := curVersion
			if ver == 0 {
				ver = existingSV.version
			}
			return next, ver, true, nil
		})
		if dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}

		log.Ctx(ctx).WarnContext(ctx, "ess stage authentication failed", slog.Any("error", err))
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Only reset failure counters when full stage authentication succeeds (err == nil),
	// NOT when ErrNeedsNextStage is returned on stage 0 before OTP/password verification.
	authReset := err == nil && (existing.ESSAuthStatus.ConsecutiveFailures > 0 || existing.ESSAuthStatus.ConsecutiveSetFailures > 0 || existing.ESSAuthStatus.ConsecutiveUserFailures > 0)
	if authReset || updated {
		_, _, _, dbErr := s.storage.UpdateSettings(ctx, siteID, func(current types.Settings, curVersion int) (types.Settings, int, bool, error) {
			next := current
			changed := false
			if updated {
				if enc, encErr := s.mergeProviderCredsOntoEncrypted(ctx, siteID, current.EncryptedCredentials, origCreds, newCreds, req.ESS); encErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt rotated stage credentials", slog.Any("error", encErr))
				} else {
					next.EncryptedCredentials = enc
					changed = true
				}
			}
			if err == nil {
				checkInteractiveESSRateLimit(&next.ESSAuthStatus, authCheckNow)
				if next.ESSAuthStatus.ConsecutiveFailures > 0 || next.ESSAuthStatus.ConsecutiveSetFailures > 0 || next.ESSAuthStatus.ConsecutiveUserFailures > 0 {
					next.ESSAuthStatus.ConsecutiveFailures = 0
					next.ESSAuthStatus.ConsecutiveSetFailures = 0
					next.ESSAuthStatus.ConsecutiveUserFailures = 0
					next.ESSAuthStatus.LastAttempt = now
					next.ESSAuthStatus.LastUserAttempt = now
					changed = true
				}
			}
			if !changed {
				return current, curVersion, false, nil
			}
			ver := curVersion
			if ver == 0 {
				ver = existingSV.version
			}
			return next, ver, true, nil
		})
		if dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func mergeSettingsCredentials(existingCreds types.Credentials, essName string, reqCreds *types.Credentials) (types.Credentials, bool, bool) {
	var changedESS bool
	var shouldBackfill bool

	switch essName {
	case "franklin":
		if reqCreds != nil && reqCreds.Franklin != nil {
			changedESS = true
			if existingCreds.Franklin == nil {
				shouldBackfill = true
			}
			cp := *reqCreds.Franklin
			existingCreds.Franklin = &cp
		}
		existingCreds.Mock = nil
		existingCreds.Tesla = nil
		existingCreds.Enphase = nil
	case "enphase":
		if reqCreds != nil && reqCreds.Enphase != nil {
			changedESS = true
			if existingCreds.Enphase == nil {
				shouldBackfill = true
			}
			existingCreds.Enphase = mergeEnphaseCredentials(existingCreds.Enphase, reqCreds.Enphase)
		}
		existingCreds.Franklin = nil
		existingCreds.Mock = nil
		existingCreds.Tesla = nil
	case "mock":
		if reqCreds != nil && reqCreds.Mock != nil {
			changedESS = true
			if existingCreds.Mock == nil {
				shouldBackfill = true
			}
			mockCopy := *reqCreds.Mock
			if existingCreds.Mock != nil {
				merged := *existingCreds.Mock
				if mockCopy.Strategy != "" {
					merged.Strategy = mockCopy.Strategy
				}
				if mockCopy.Location != "" {
					merged.Location = mockCopy.Location
				}
				mockCopy = merged
			}
			existingCreds.Mock = &mockCopy
		}
		existingCreds.Franklin = nil
		existingCreds.Tesla = nil
		existingCreds.Enphase = nil
	case "tesla":
		if reqCreds != nil && reqCreds.Tesla != nil {
			changedESS = true
			if existingCreds.Tesla == nil {
				shouldBackfill = true
			}
			existingCreds.Tesla = mergeTeslaCredentials(existingCreds.Tesla, reqCreds.Tesla)
		}
		existingCreds.Franklin = nil
		existingCreds.Mock = nil
		existingCreds.Enphase = nil
	}

	return existingCreds, changedESS, shouldBackfill
}

func mergeEnphaseCredentials(existing, incoming *types.EnphaseCredentials) *types.EnphaseCredentials {
	if existing == nil || (incoming.Username != "" && incoming.Username != existing.Username) {
		cp := *incoming
		return &cp
	}
	merged := *existing
	if incoming.Username != "" {
		merged.Username = incoming.Username
	}
	if incoming.Password != "" {
		if incoming.Password != existing.Password {
			merged.SessionID = ""
			merged.ManagerToken = ""
			merged.SystemID = 0
			merged.UserID = 0
		}
		merged.Password = incoming.Password
	}
	if incoming.Code != "" {
		merged.Code = incoming.Code
		merged.SessionID = ""
		merged.ManagerToken = ""
		merged.SystemID = 0
		merged.UserID = 0
	}
	if incoming.SessionID != "" {
		merged.SessionID = incoming.SessionID
	}
	if incoming.ManagerToken != "" {
		merged.ManagerToken = incoming.ManagerToken
	}
	if incoming.SystemID != 0 {
		merged.SystemID = incoming.SystemID
	}
	if incoming.UserID != 0 {
		merged.UserID = incoming.UserID
	}
	return &merged
}

func mergeTeslaCredentials(existing, incoming *types.TeslaCredentials) *types.TeslaCredentials {
	// A fresh AuthCode authorizes a potentially new Tesla account/battery, so start
	// from incoming credentials (clearing any previously saved EnergySiteID/tokens)
	// while preserving saved tokens and site selection on retries without AuthCode.
	if existing == nil || incoming.AuthCode != "" {
		cp := *incoming
		return &cp
	}
	merged := *existing
	if incoming.Region != "" {
		merged.Region = incoming.Region
	}
	if incoming.AccessToken != "" {
		merged.AccessToken = incoming.AccessToken
	}
	if incoming.RefreshToken != "" {
		merged.RefreshToken = incoming.RefreshToken
	}
	if !incoming.Expiry.IsZero() {
		merged.Expiry = incoming.Expiry
	}
	if incoming.SerialNumber != "" {
		if incoming.SerialNumber != existing.SerialNumber && incoming.EnergySiteID == 0 {
			merged.EnergySiteID = 0
		}
		merged.SerialNumber = incoming.SerialNumber
	}
	if incoming.EnergySiteID != 0 {
		merged.EnergySiteID = incoming.EnergySiteID
	}
	return &merged
}

func mergeUpdatedProviderCredentials(base, updated types.Credentials, essName string) types.Credentials {
	saved := base
	switch essName {
	case "franklin":
		saved.Franklin = updated.Franklin
	case "enphase":
		saved.Enphase = updated.Enphase
	case "mock":
		saved.Mock = updated.Mock
	case "tesla":
		saved.Tesla = updated.Tesla
	}
	return saved
}

func (s *Server) validateAndCalculateMinBatterySOCPeriods(ctx context.Context, siteID string, newSettings types.Settings) (float64, error) {
	if len(newSettings.MinBatterySOCPeriods) == 0 {
		return newSettings.MinBatterySOC, nil
	}

	minPeriodSOC := 100.0
	isNameBased := false
	for _, p := range newSettings.MinBatterySOCPeriods {
		if p.MinBatterySOC < 0 || p.MinBatterySOC > 100 {
			return 0, errors.New("minimum battery SOC period value must be between 0 and 100")
		}
		if p.MinBatterySOC < minPeriodSOC {
			minPeriodSOC = p.MinBatterySOC
		}
		if p.UtilityPeriodName != "" {
			isNameBased = true
		}
	}

	if isNameBased {
		for _, p := range newSettings.MinBatterySOCPeriods {
			if p.UtilityPeriodName == "" {
				return 0, errors.New("all reserve SOC periods must specify a utility period name for rate-based minimum SOC")
			}
		}
		if newSettings.UtilityProvider != "" && s.utilities != nil {
			u, err := s.utilities.PreviewSite(ctx, newSettings)
			if err != nil || u == nil {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
			periods, err := u.GetPeriods(ctx)
			if err != nil || len(periods) == 0 {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
			configuredNames := map[string]bool{}
			for _, p := range newSettings.MinBatterySOCPeriods {
				if p.UtilityPeriodName != "" {
					configuredNames[p.UtilityPeriodName] = true
				}
			}
			var namedPeriods []types.TimePeriod
			for _, p := range periods {
				if p.Name != "" {
					namedPeriods = append(namedPeriods, p)
					if !configuredNames[p.Name] {
						return 0, fmt.Errorf("missing minimum battery SOC for utility period name: %s", p.Name)
					}
				}
			}
			if len(namedPeriods) == 0 {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
		}
	} else {
		// Custom schedule 24-hour validation
		for _, p := range newSettings.MinBatterySOCPeriods {
			if len(p.Hours) == 0 {
				return 0, errors.New("reserve SOC period must specify hours")
			}
			for _, hp := range p.Hours {
				if hp.MinuteStart != 0 || hp.MinuteEnd != 0 {
					return 0, errors.New("reserve SOC period start and end minutes must be 0")
				}
				duration := hp.HourEnd - hp.HourStart
				if hp.HourEnd < hp.HourStart {
					duration += 24
				}
				if hp.HourStart == hp.HourEnd {
					duration = 24
				}
				if duration < 1 {
					return 0, errors.New("reserve SOC period minimum duration is 1 hour")
				}
			}
		}

		for h := 0; h < 24; h++ {
			count := 0
			for _, p := range newSettings.MinBatterySOCPeriods {
				for _, hp := range p.Hours {
					if hp.HourStart < hp.HourEnd {
						if h >= hp.HourStart && h < hp.HourEnd {
							count++
						}
					} else if hp.HourStart > hp.HourEnd {
						if h >= hp.HourStart || h < hp.HourEnd {
							count++
						}
					} else {
						// HourStart == HourEnd (covers 24h)
						count++
					}
				}
			}
			if count == 0 {
				return 0, fmt.Errorf("reserve SOC periods must cover all 24 hours of the day (hour %d is not covered)", h)
			}
			if count > 1 {
				return 0, fmt.Errorf("reserve SOC periods cannot overlap (hour %d has multiple periods defined)", h)
			}
		}
	}

	return minPeriodSOC, nil
}

func isComEdHourly(provider, rate string) bool {
	return provider == "comed" && rate != "comed_bes" && rate != "comed_best"
}
