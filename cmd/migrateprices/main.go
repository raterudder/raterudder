package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/levenlabs/go-lflag"
	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
)

func main() {
	dryRun := lflag.Bool("dry-run", false, "If true, simulate migration without writing monthly documents or purging legacy ones")
	purgeLegacy := lflag.Bool("purge-legacy", false, "If true, delete legacy hourly_prices and price_history documents after migration")
	utilityFlag := lflag.String("utility", "", "Optional utility ID to migrate (e.g. comed, ameren)")
	siteFlag := lflag.String("site", "", "Optional site ID to migrate")

	s := storage.Configured()
	lflag.Configure()

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
		panic(fmt.Errorf("unknown log level: %s", llog.GetLevel().String()))
	}
	log.SetDefaultLogLevel(level)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	defer func() {
		if err := s.Close(); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to close storage", "error", err)
		}
	}()

	opts := storage.PricingMigrationOptions{
		DryRun:      *dryRun,
		PurgeLegacy: *purgeLegacy,
		UtilityID:   *utilityFlag,
		SiteID:      *siteFlag,
	}

	log.Ctx(ctx).InfoContext(ctx, "starting pricing storage migration",
		slog.Bool("dryRun", opts.DryRun),
		slog.Bool("purgeLegacy", opts.PurgeLegacy),
		slog.String("utilityID", opts.UtilityID),
		slog.String("siteID", opts.SiteID),
	)

	fp, ok := storage.Unwrap(s).(*storage.FirestoreProvider)
	if !ok {
		log.Ctx(ctx).ErrorContext(ctx, "storage provider must be *FirestoreProvider")
		os.Exit(1)
	}

	stats, err := fp.MigrateLegacyPricing(ctx, opts)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "pricing migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Ctx(ctx).InfoContext(ctx, "pricing migration completed successfully",
		slog.Int("utilitiesInspected", stats.UtilitiesInspected),
		slog.Int("utilityHoursMigrated", stats.UtilityHoursMigrated),
		slog.Int("utilityMonthsCreated", stats.UtilityMonthsCreated),
		slog.Int("sitesInspected", stats.SitesInspected),
		slog.Int("siteHoursMigrated", stats.SiteHoursMigrated),
		slog.Int("siteMonthsCreated", stats.SiteMonthsCreated),
	)
}
