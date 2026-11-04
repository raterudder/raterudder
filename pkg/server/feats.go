package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
)

const featsCacheTTL = 5 * time.Minute

func (s *Server) getFeatsEvaluator(ctx context.Context) *feats.Evaluator {
	now := s.now()
	s.featsMu.RLock()
	if !s.featsFetchedAt.IsZero() && now.Sub(s.featsFetchedAt) < featsCacheTTL {
		eval := s.featsEvaluator
		s.featsMu.RUnlock()
		return eval
	}
	s.featsMu.RUnlock()

	s.featsFetchMu.Lock()
	defer s.featsFetchMu.Unlock()

	// check again in case we raced to fetch
	now = s.now()
	s.featsMu.RLock()
	if !s.featsFetchedAt.IsZero() && now.Sub(s.featsFetchedAt) < featsCacheTTL {
		eval := s.featsEvaluator
		s.featsMu.RUnlock()
		return eval
	}
	prevEval := s.featsEvaluator
	prevUpdatedAt := s.featsUpdatedAt
	s.featsMu.RUnlock()

	if s.storage == nil {
		return prevEval
	}

	release := s.release
	if release == "" {
		release = "production"
	}

	cfg, updatedAt, err := s.storage.GetFeatureConfig(ctx, release)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to fetch feature flag config", slog.String("release", release), slog.Any("error", err))
		s.featsMu.Lock()
		// if we errored try to refresh every minute
		s.featsFetchedAt = now.Add(-featsCacheTTL + time.Minute)
		eval := s.featsEvaluator
		s.featsMu.Unlock()
		return eval
	}

	needRebuild := prevEval == nil || !updatedAt.Equal(prevUpdatedAt) || (updatedAt.IsZero() && len(cfg.Features) > 0)
	var nextEval *feats.Evaluator
	if needRebuild {
		nextEval = feats.NewEvaluator(cfg)
	} else {
		nextEval = prevEval
	}

	s.featsMu.Lock()
	s.featsEvaluator = nextEval
	s.featsUpdatedAt = updatedAt
	// align to the 5-minute cadence so multiple instances refresh roughly in sync
	s.featsFetchedAt = now.Truncate(featsCacheTTL)
	s.featsMu.Unlock()

	return nextEval
}

func (s *Server) initFeats(ctx context.Context, siteID string) context.Context {
	evalSiteID := siteID
	if evalSiteID == SiteIDAll {
		evalSiteID = ""
	}
	return s.getFeatsEvaluator(ctx).WithSite(ctx, evalSiteID)
}
