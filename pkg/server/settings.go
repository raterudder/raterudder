package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

type settingsWithVersion struct {
	types.Settings
	version int
}

func (s *Server) getSettingsWithMigration(ctx context.Context, siteID string) (settingsWithVersion, types.Credentials, error) {
	settings, version, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		return settingsWithVersion{}, types.Credentials{}, err
	}
	sv := settingsWithVersion{
		Settings: settings,
		version:  version,
	}

	// Check for migration
	if version < types.CurrentSettingsVersion {
		log.Ctx(ctx).InfoContext(ctx, "migrating settings", slog.Int("oldVersion", version), slog.Int("newVersion", types.CurrentSettingsVersion))
		newSettings, changed, err := types.MigrateSettings(settings, version)
		if err != nil {
			// Log error but return settings as is (best effort)
			log.Ctx(ctx).ErrorContext(ctx, "failed to migrate settings", slog.Int("currentVersion", version), slog.Any("error", err))
		} else if changed {
			sv.Settings = newSettings
			sv.version = types.CurrentSettingsVersion
			if err := s.storage.SetSettings(ctx, siteID, newSettings, types.CurrentSettingsVersion); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to save migrated settings", slog.Any("error", err))
				// Return migrated settings even if save failed, so current request works with new defaults
			} else {
				log.Ctx(ctx).InfoContext(ctx, "saved migrated settings", slog.Int("oldVersion", version), slog.Int("newVersion", types.CurrentSettingsVersion))
			}
			sv.Settings = newSettings
		}
	}

	var creds types.Credentials
	if len(settings.EncryptedCredentials) > 0 {
		creds, err = s.decryptCredentials(ctx, settings.EncryptedCredentials)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
			return settingsWithVersion{}, types.Credentials{}, err
		}
	}

	return sv, creds, nil
}

func (s *Server) getESSSystem(ctx context.Context, siteID string, settings settingsWithVersion, creds types.Credentials) (ess.System, error) {
	if settings.ESSAuthStatus.ConsecutiveFailures > 1 {
		backoff := getESSBackoff(settings.ESSAuthStatus.ConsecutiveFailures)
		timeLeft := backoff - time.Since(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			// Round to seconds
			timeLeft = timeLeft.Round(time.Second)
			return nil, fmt.Errorf("ESS authentication rate limited, try again in %v", timeLeft)
		}
	}

	essSystem, err := s.ess.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, fmt.Errorf("failed to get ESS system: %w", err)
	}

	// and apply those settings to the ESS
	newCreds, updated, err := essSystem.Authenticate(ctx, creds)

	now := time.Now().UTC()
	if err != nil {
		settings.ESSAuthStatus.ConsecutiveFailures++
		settings.ESSAuthStatus.LastAttempt = now
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
		return nil, fmt.Errorf("failed to apply settings: %w", err)
	}

	authStatusChanged := false
	if settings.ESSAuthStatus.ConsecutiveFailures > 0 {
		settings.ESSAuthStatus.ConsecutiveFailures = 0
		settings.ESSAuthStatus.LastAttempt = now
		authStatusChanged = true
	}

	if updated {
		log.Ctx(ctx).DebugContext(ctx, "credentials updated by ess system")
		settings.EncryptedCredentials, err = s.encryptCredentials(ctx, newCreds)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", err))
		} else {
			if err := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", err))
			}
		}
	} else if authStatusChanged {
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
	}

	return essSystem, nil
}

// SettingsRes is the response type for GetSettings
type SettingsRes struct {
	types.Settings
	HasCredentials map[string]bool `json:"hasCredentials"`
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

	if newSettings.MinArbitrageDifferenceDollarsPerKWH < 0 {
		writeJSONError(w, "minimum arbitrage difference cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.MinBatterySOC < 0 || newSettings.MinBatterySOC > 100 {
		writeJSONError(w, "minimum battery SOC must be between 0 and 100", http.StatusBadRequest)
		return
	}
	if newSettings.IgnoreHourUsageOverMultiple < 1 {
		writeJSONError(w, "ignore hour usage over multiple must be at least 1", http.StatusBadRequest)
		return
	}
	if newSettings.SolarBellCurveMultiplier < 0 {
		writeJSONError(w, "solar bell curve multiplier cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SolarTrendRatioMax < 1 {
		writeJSONError(w, "solar trend ratio max must be at least 1", http.StatusBadRequest)
		return
	}
	if newSettings.Release != s.release {
		writeJSONError(w, "settings release mismatch", http.StatusBadRequest)
		return
	}

	_, err := s.utilities.Site(ctx, siteID, newSettings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility provider", slog.String("utilityProvider", newSettings.UtilityProvider), slog.Any("error", err))
		writeJSONError(w, fmt.Sprintf("invalid utility provider settings: %v", err), http.StatusBadRequest)
		return
	}
	// Get existing credentials to preserve other fields
	existing, _, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	newSettings.ESSAuthStatus = existing.ESSAuthStatus

	// Update Location if zip/country changed
	if newSettings.PostalCode != "" && newSettings.CountryCode != "" {
		if existing.PostalCode != newSettings.PostalCode || existing.CountryCode != newSettings.CountryCode || existing.Location == nil {
			loc, err := s.weather.GetLocationData(ctx, newSettings.CountryCode, newSettings.PostalCode)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to fetch location data", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to fetch location data: %v", err), http.StatusBadRequest)
				return
			}
			newSettings.Location = loc

			go func() {
				log.Ctx(context.Background()).InfoContext(context.Background(), "fetching initial weather for new location")
				if err := s.updateWeatherHistory(context.Background(), siteID, *loc, nil); err != nil {
					log.Ctx(context.Background()).ErrorContext(context.Background(), "failed to sync weather history after settings update", slog.Any("error", err))
				}
			}()

		} else {
			newSettings.Location = existing.Location
		}
	} else {
		newSettings.Location = nil
	}

	var wg sync.WaitGroup
	// Handle credentials update
	if req.Credentials != nil {
		var existingCreds types.Credentials
		if len(existing.EncryptedCredentials) > 0 {
			existingCreds, err = s.decryptCredentials(ctx, existing.EncryptedCredentials)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
				writeJSONError(w, "failed to decrypt credentials", http.StatusInternalServerError)
				return
			}
		}

		// check which credentials changed
		var changedESS bool
		var shouldBackfillHistory bool
		switch newSettings.ESS {
		case "franklin":
			if req.Credentials.Franklin != nil {
				changedESS = true
				if existingCreds.Franklin == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Franklin = req.Credentials.Franklin
			}
		case "mock":
			if req.Credentials.Mock != nil {
				changedESS = true
				if existingCreds.Mock == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Mock = req.Credentials.Mock
			}
		}

		// if the ess credentials changed, we need to verify them and potentially backfill history
		if changedESS {
			essSystem, err := s.ess.Site(ctx, siteID, newSettings)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to get ess system: %v", err), http.StatusInternalServerError)
				return
			}

			if newSettings.ESSAuthStatus.ConsecutiveFailures > 1 {
				backoff := getESSBackoff(newSettings.ESSAuthStatus.ConsecutiveFailures)
				timeLeft := backoff - time.Since(newSettings.ESSAuthStatus.LastAttempt)
				if timeLeft > 0 {
					timeLeft = timeLeft.Round(time.Second)
					writeJSONError(w, fmt.Sprintf("ESS authentication rate limited, try again in %v", timeLeft), http.StatusTooManyRequests)
					return
				}
			}

			// Verify and update credentials
			existingCreds, _, err = essSystem.Authenticate(ctx, existingCreds)
			now := time.Now().UTC()
			if err != nil {
				newSettings.ESSAuthStatus.ConsecutiveFailures++
				newSettings.ESSAuthStatus.LastAttempt = now
				if dbErr := s.storage.SetSettings(ctx, siteID, newSettings, types.CurrentSettingsVersion); dbErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
				}
				log.Ctx(ctx).WarnContext(ctx, "failed to verify ess credentials", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to verify ess credentials: %v", err), http.StatusBadRequest)
				return
			}

			if newSettings.ESSAuthStatus.ConsecutiveFailures > 0 {
				newSettings.ESSAuthStatus.ConsecutiveFailures = 0
				newSettings.ESSAuthStatus.LastAttempt = now
			}

			// now backfill if we need to since the credentials were verified
			if shouldBackfillHistory {
				wg.Add(1)
				go func() {
					defer wg.Done()
					log.Ctx(ctx).InfoContext(ctx, "backfilling energy history for new credentials")
					if err := s.updateEnergyHistory(ctx, siteID, essSystem); err != nil {
						log.Ctx(ctx).ErrorContext(ctx, "failed to sync energy history after settings update", slog.Any("error", err))
					}
				}()
			}
		}

		// store the existing credentials with the new ones updated in-place
		encrypted, err := s.encryptCredentials(ctx, existingCreds)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", err))
			writeJSONError(w, "failed to encrypt credentials", http.StatusInternalServerError)
			return
		}
		newSettings.EncryptedCredentials = encrypted
	} else {
		// Preserve existing encrypted credentials if not updating
		newSettings.EncryptedCredentials = existing.EncryptedCredentials
	}

	if err := s.storage.SetSettings(ctx, siteID, newSettings, types.CurrentSettingsVersion); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", err))
		writeJSONError(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	wg.Wait()
	log.Ctx(ctx).InfoContext(ctx, "settings updated")

	w.WriteHeader(http.StatusOK)
}

func getESSBackoff(failures int) time.Duration {
	if failures <= 1 {
		return 0
	}

	// Failures = 2 -> 30s
	// Failures = 3 -> 60s
	// Failures = 4 -> 120s
	// Failures = 5 -> 240s
	if failures > 7 {
		failures = 7 // max 15 minutes, prevent integer overflow
	}
	seconds := 30 * (1 << (failures - 2))
	if seconds > 900 {
		seconds = 900 // max 15 minutes
	}

	return time.Duration(seconds) * time.Second
}
