package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
)

const (
	featsCacheTTL            = 5 * time.Minute
	featsFetchTimeout        = 5 * time.Second
	defaultFeatsRetryBackoff = time.Minute
)

func (s *Server) getFeatsRetryBackoff() time.Duration {
	if s.featsRetryBackoff > 0 {
		return s.featsRetryBackoff
	}
	return defaultFeatsRetryBackoff
}

func (s *Server) hasFeatsEvaluator() bool {
	s.featsMu.RLock()
	defer s.featsMu.RUnlock()
	return s.featsEvaluator != nil
}

func (s *Server) featsRelease() string {
	if s.featuresEnvironment != "" {
		return s.featuresEnvironment
	}
	if s.release != "" {
		return s.release
	}
	return "production"
}

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

	release := s.featsRelease()

	fetchCtx, cancel := context.WithTimeout(ctx, featsFetchTimeout)
	defer cancel()

	cfg, updatedAt, err := s.storage.GetFeatureConfig(fetchCtx, release)
	now = s.now()
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to fetch feature flag config", slog.String("release", release), slog.Any("error", err))
		s.featsMu.Lock()
		// if we errored try to refresh after retry backoff (default 1 minute)
		s.featsFetchedAt = now.Add(-featsCacheTTL + s.getFeatsRetryBackoff())
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

func (s *Server) featsRetryDelay() (time.Duration, bool) {
	s.featsMu.RLock()
	defer s.featsMu.RUnlock()
	if s.featsEvaluator != nil {
		return 0, false
	}
	if s.featsFetchedAt.IsZero() {
		return 0, true
	}
	wait := s.featsFetchedAt.Add(featsCacheTTL).Sub(s.now())
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

func (s *Server) retryFeatsUntilLoaded(ctx context.Context) {
	if s.storage == nil {
		return
	}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		wait, shouldRetry := s.featsRetryDelay()
		if !shouldRetry {
			return
		}
		if wait > 0 {
			if timer == nil {
				timer = time.NewTimer(wait)
			} else {
				timer.Reset(wait)
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		if s.getFeatsEvaluator(ctx) != nil {
			return
		}
	}
}

func (s *Server) initFeats(ctx context.Context, siteID string) context.Context {
	evalSiteID := siteID
	if evalSiteID == SiteIDAll {
		evalSiteID = ""
	}
	return s.getFeatsEvaluator(ctx).WithSite(ctx, evalSiteID)
}
