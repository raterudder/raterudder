package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
)

func main() {
	dryRun := lflag.Bool("dry-run", false, "If true, simulate migration without modifying Firestore")
	sitesFlag := lflag.String("sites", "hartero,fastest963", "Comma-separated list of site IDs to migrate")
	sinceFlag := lflag.String("since", "2026-09-29T00:00:00Z", "Timestamp floor for action history query (RFC3339). Set to empty string or pass --all to inspect all history")
	allFlag := lflag.Bool("all", false, "If true, scan all action history ignoring --since")

	s := storage.Configured()
	lflag.Configure()

	var isDryRun bool
	var targetSites string
	var targetSince string
	var isAll bool

	lflag.Do(func() {
		isDryRun = *dryRun
		targetSites = *sitesFlag
		targetSince = *sinceFlag
		isAll = *allFlag
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
		if err := s.Close(); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to close storage", slog.Any("error", err))
		}
	}()

	var sites []string
	if targetSites != "" {
		for _, site := range strings.Split(targetSites, ",") {
			trimmed := strings.TrimSpace(site)
			if trimmed != "" {
				sites = append(sites, trimmed)
			}
		}
	}

	var sinceTime time.Time
	if !isAll && targetSince != "" {
		t, err := time.Parse(time.RFC3339, targetSince)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "invalid --since format (must be RFC3339, e.g. 2026-09-29T00:00:00Z)", slog.Any("error", err))
			os.Exit(1)
		}
		sinceTime = t
	}

	opts := storage.PlanMigrationOptions{
		DryRun: isDryRun,
		Sites:  sites,
		Since:  sinceTime,
	}

	log.Ctx(ctx).InfoContext(ctx, "starting action plan migration",
		slog.Bool("dryRun", opts.DryRun),
		slog.Any("sites", opts.Sites),
		slog.Time("since", opts.Since),
	)

	fp, ok := storage.Unwrap(s).(*storage.FirestoreProvider)
	if !ok {
		log.Ctx(ctx).ErrorContext(ctx, "storage provider must be *FirestoreProvider")
		os.Exit(1)
	}

	stats, err := fp.MigrateActionPlans(ctx, opts)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "action plan migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Ctx(ctx).InfoContext(ctx, "action plan migration completed successfully",
		slog.Bool("dryRun", opts.DryRun),
		slog.Int("sitesInspected", stats.SitesInspected),
		slog.Int("actionsInspected", stats.ActionsInspected),
		slog.Int("plansMigrated", stats.PlansMigrated),
		slog.Int("plansSkipped", stats.PlansSkipped),
		slog.Int("bytesSaved", stats.BytesSaved),
	)
}
