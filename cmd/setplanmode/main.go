package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/levenlabs/go-lflag"
	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
)

// PlanModeOptions defines parameters for updating site planMode.
type PlanModeOptions struct {
	SiteID  string
	Enabled bool
	DryRun  bool
}

// setSitePlanMode fetches the site's current settings and enables or disables PlanMode.
func setSitePlanMode(ctx context.Context, db storage.Database, opts PlanModeOptions) error {
	siteID := strings.TrimSpace(opts.SiteID)
	if siteID == "" {
		return errors.New("site ID cannot be empty")
	}

	settings, version, updatedTime, err := db.GetSettings(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to fetch settings for site %q: %w", siteID, err)
	}

	log.Ctx(ctx).InfoContext(ctx, "site settings fetched",
		slog.String("siteID", siteID),
		slog.Bool("currentPlanMode", settings.PlanMode),
		slog.Bool("targetPlanMode", opts.Enabled),
		slog.String("release", settings.Release),
		slog.Int("version", version),
	)

	if settings.PlanMode == opts.Enabled {
		log.Ctx(ctx).InfoContext(ctx, "site planMode already matches desired state, no changes needed",
			slog.String("siteID", siteID),
			slog.Bool("planMode", opts.Enabled),
		)
		return nil
	}

	settings.PlanMode = opts.Enabled

	if opts.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "[DRY RUN] would update site planMode",
			slog.String("siteID", siteID),
			slog.Bool("planMode", opts.Enabled),
		)
		return nil
	}

	newUpdatedTime, err := db.SetSettings(ctx, siteID, settings, version, updatedTime)
	if err != nil {
		return fmt.Errorf("failed to update settings for site %q: %w", siteID, err)
	}

	log.Ctx(ctx).InfoContext(ctx, "successfully updated site planMode",
		slog.String("siteID", siteID),
		slog.Bool("planMode", opts.Enabled),
		slog.Time("updatedTime", newUpdatedTime),
	)
	return nil
}

func main() {
	siteFlag := lflag.String("site", "", "Site ID to update planMode for (required)")
	sitesFlag := lflag.String("sites", "", "Comma-separated list of site IDs to update planMode for")
	enabledFlag := lflag.Bool("enabled", true, "Whether to enable (true) or disable (false) planMode on the site")
	dryRunFlag := lflag.Bool("dry-run", false, "If true, simulate without modifying Firestore")

	db := storage.Configured()
	lflag.Configure()

	var siteArg string
	var sitesArg string
	var isEnabled bool
	var isDryRun bool

	lflag.Do(func() {
		siteArg = *siteFlag
		sitesArg = *sitesFlag
		isEnabled = *enabledFlag
		isDryRun = *dryRunFlag
	})

	var level slog.Level
	switch llog.GetLevel() {
	case llog.DebugLevel:
		level = slog.LevelDebug
	case llog.InfoLevel:
		level = slog.LevelInfo
	case llog.WarnLevel:
		level = slog.LevelWarn
	case llog.ErrorLevel:
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	log.SetDefaultLogLevel(level)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	defer func() {
		if err := db.Close(); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to close storage", slog.Any("error", err))
		}
	}()

	var targetSites []string
	addSites := func(val string) {
		for _, s := range strings.Split(val, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				targetSites = append(targetSites, trimmed)
			}
		}
	}

	if siteArg != "" {
		addSites(siteArg)
	}
	if sitesArg != "" {
		addSites(sitesArg)
	}
	if len(targetSites) == 0 {
		for _, arg := range flag.Args() {
			if trimmed := strings.TrimSpace(arg); trimmed != "" && !strings.HasPrefix(trimmed, "-") {
				targetSites = append(targetSites, trimmed)
			}
		}
	}

	if len(targetSites) == 0 {
		log.Ctx(ctx).ErrorContext(ctx, "missing required --site flag")
		fmt.Fprintf(os.Stderr, "Usage: %s --site <siteID> [--enabled=true|false] [--dry-run]\n", os.Args[0])
		os.Exit(1)
	}

	var hasError bool
	for _, sID := range targetSites {
		opts := PlanModeOptions{
			SiteID:  sID,
			Enabled: isEnabled,
			DryRun:  isDryRun,
		}
		if err := setSitePlanMode(ctx, db, opts); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update planMode",
				slog.String("siteID", sID),
				slog.Any("error", err),
			)
			hasError = true
		}
	}

	if hasError {
		os.Exit(1)
	}
}
