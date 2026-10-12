package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
)

func main() {
	dryRun := lflag.Bool("dry-run", false, "If true, simulate migration without writing 12-hour rollup documents or purging legacy ones")
	purgeLegacy := lflag.Bool("purge-legacy", false, "If true, ONLY delete legacy action_history documents without re-inserting into daily_actions")
	siteFlag := lflag.String("site", "", "Optional site ID to migrate")
	sinceFlag := lflag.String("since", "", "Optional RFC3339 timestamp floor for action history query")

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
			log.Ctx(ctx).ErrorContext(ctx, "failed to close storage", slog.Any("error", err))
		}
	}()

	var sinceTime time.Time
	if *sinceFlag != "" {
		t, err := time.Parse(time.RFC3339, *sinceFlag)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "invalid --since format (must be RFC3339)", slog.Any("error", err))
			os.Exit(1)
		}
		sinceTime = t
	}

	opts := storage.ActionMigrationOptions{
		DryRun:      *dryRun,
		PurgeLegacy: *purgeLegacy,
		SiteID:      *siteFlag,
		Since:       sinceTime,
	}

	log.Ctx(ctx).InfoContext(ctx, "starting action history migration",
		slog.Bool("dryRun", opts.DryRun),
		slog.Bool("purgeLegacy", opts.PurgeLegacy),
		slog.String("siteID", opts.SiteID),
		slog.Time("since", opts.Since),
	)

	fp, ok := storage.Unwrap(s).(*storage.FirestoreProvider)
	if !ok {
		log.Ctx(ctx).ErrorContext(ctx, "storage provider must be *FirestoreProvider")
		os.Exit(1)
	}

	stats, err := fp.MigrateLegacyActions(ctx, opts)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "action history migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Ctx(ctx).InfoContext(ctx, "action history migration completed successfully",
		slog.Int("sitesInspected", stats.SitesInspected),
		slog.Int("siteActionsMigrated", stats.SiteActionsMigrated),
		slog.Int("siteBucketsCreated", stats.SiteBucketsCreated),
		slog.Int("sitesPurged", stats.SitesPurged),
	)
}
