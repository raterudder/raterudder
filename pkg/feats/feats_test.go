package feats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetRegistryForTest(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	orig := definedFlags
	definedFlags = make(map[string]BoolFlag)
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		definedFlags = orig
		registryMu.Unlock()
	})
}

func TestFeats(t *testing.T) {
	t.Run("RegistrationAndDefaults", func(t *testing.T) {
		resetRegistryForTest(t)

		ffAlpha := Bool("alpha", true)
		ffBeta := Bool("beta", false)

		assert.Equal(t, "alpha", ffAlpha.Name())
		assert.True(t, ffAlpha.Default())
		assert.Equal(t, "beta", ffBeta.Name())
		assert.False(t, ffBeta.Default())

		// Re-registering with the same default value succeeds and returns identical flag
		ffAlphaDup := Bool("alpha", true)
		assert.Equal(t, ffAlpha, ffAlphaDup)

		// Re-registering with a different default value panics
		assert.PanicsWithValue(t, `feats: flag "alpha" already registered with different default value (true != false)`, func() {
			Bool("alpha", false)
		})

		// Empty flag name panics
		assert.PanicsWithValue(t, "feats: flag name cannot be empty", func() {
			Bool("", false)
		})

		// Uninitialized context falls back to defaults
		ctx := context.Background()
		assert.True(t, ffAlpha.Enabled(ctx))
		assert.False(t, ffBeta.Enabled(ctx))
		assert.Equal(t, map[string]bool{
			"alpha": true,
			"beta":  false,
		}, All(ctx))
	})

	t.Run("CriteriaEvaluation", func(t *testing.T) {
		resetRegistryForTest(t)

		ffRollout := Bool("rollout", false)
		ffDefaultOn := Bool("defaultOn", true)

		// Find two siteIDs with known buckets for deterministic testing
		var lowBucketSite, highBucketSite string
		for i := 0; i < 1000; i++ {
			candidate := fmt.Sprintf("site-%d", i)
			b := siteBucket(candidate)
			if b < 10 && lowBucketSite == "" {
				lowBucketSite = candidate
			}
			if b >= 80 && highBucketSite == "" {
				highBucketSite = candidate
			}
			if lowBucketSite != "" && highBucketSite != "" {
				break
			}
		}
		require.NotEmpty(t, lowBucketSite)
		require.NotEmpty(t, highBucketSite)

		cfg := Config{
			Features: map[string][]Criteria{
				"rollout": {
					// Rule 1: Explicitly block lowBucketSite even though it falls in the 25% cohort
					{
						SiteIDs: []string{lowBucketSite},
						Value:   false,
					},
					// Rule 2: Enable for explicit allowlist site or 25% of sites
					{
						SiteIDs:     []string{highBucketSite},
						SitePercent: 25,
						Value:       true,
					},
				},
				"defaultOn": {
					// Disable for highBucketSite only
					{
						SiteIDs: []string{highBucketSite},
						Value:   false,
					},
				},
			},
		}

		eval := NewEvaluator(cfg)

		// lowBucketSite matches Rule 1 (blocked -> false)
		ctxLow := eval.WithSite(context.Background(), lowBucketSite)
		assert.False(t, ffRollout.Enabled(ctxLow))
		assert.True(t, ffDefaultOn.Enabled(ctxLow))

		// highBucketSite matches Rule 2 via explicit SiteIDs allowlist (-> true), and defaultOn is false
		ctxHigh := eval.WithSite(context.Background(), highBucketSite)
		assert.True(t, ffRollout.Enabled(ctxHigh))
		assert.False(t, ffDefaultOn.Enabled(ctxHigh))

		// Another site in <25% bucket not in blocklist matches Rule 2 via percentage
		var anotherLowSite string
		for i := 0; i < 1000; i++ {
			candidate := fmt.Sprintf("other-site-%d", i)
			if siteBucket(candidate) < 25 {
				anotherLowSite = candidate
				break
			}
		}
		require.NotEmpty(t, anotherLowSite)
		ctxAnotherLow := eval.WithSite(context.Background(), anotherLowSite)
		assert.True(t, ffRollout.Enabled(ctxAnotherLow))

		// Another site in >=25% bucket not in allowlist falls back to default (false)
		var anotherHighSite string
		for i := 0; i < 1000; i++ {
			candidate := fmt.Sprintf("other-site-%d", i)
			if siteBucket(candidate) >= 25 {
				anotherHighSite = candidate
				break
			}
		}
		require.NotEmpty(t, anotherHighSite)
		ctxAnotherHigh := eval.WithSite(context.Background(), anotherHighSite)
		assert.False(t, ffRollout.Enabled(ctxAnotherHigh))

		// Empty siteID does not match partial percentage or siteIDs
		ctxEmpty := eval.WithSite(context.Background(), "")
		assert.False(t, ffRollout.Enabled(ctxEmpty))
		assert.True(t, ffDefaultOn.Enabled(ctxEmpty))
	})

	t.Run("FullRolloutMatchesEmptySiteID", func(t *testing.T) {
		resetRegistryForTest(t)

		ffGlobal := Bool("global", false)
		eval := NewEvaluator(Config{
			Features: map[string][]Criteria{
				"global": {
					{
						SitePercent: 100,
						Value:       true,
					},
				},
			},
		})

		ctxEmpty := eval.WithSite(context.Background(), "")
		assert.True(t, ffGlobal.Enabled(ctxEmpty))

		ctxSite := eval.WithSite(context.Background(), "any-site")
		assert.True(t, ffGlobal.Enabled(ctxSite))
	})

	t.Run("ContextOverrideDoesNotMutateParent", func(t *testing.T) {
		resetRegistryForTest(t)

		ffOne := Bool("one", false)
		ffTwo := Bool("two", true)

		baseCtx := (*Evaluator)(nil).WithSite(context.Background(), "site-1")
		assert.False(t, ffOne.Enabled(baseCtx))
		assert.True(t, ffTwo.Enabled(baseCtx))

		childCtx := ffOne.With(baseCtx, true)
		assert.True(t, ffOne.Enabled(childCtx))
		assert.True(t, ffTwo.Enabled(childCtx))

		// Parent context remains unchanged
		assert.False(t, ffOne.Enabled(baseCtx))

		// Override on empty context also works and preserves defaults for other flags
		overrideFromEmpty := ffTwo.With(context.Background(), false)
		assert.False(t, ffOne.Enabled(overrideFromEmpty))
		assert.False(t, ffTwo.Enabled(overrideFromEmpty))
		assert.Equal(t, map[string]bool{
			"one": false,
			"two": false,
		}, All(overrideFromEmpty))
	})

	t.Run("WithSiteAttachesFeatsLogGroup", func(t *testing.T) {
		resetRegistryForTest(t)

		Bool("solarV2", true)
		Bool("planMode", false)

		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		ctx := log.With(context.Background(), logger)

		eval := NewEvaluator(Config{
			Features: map[string][]Criteria{
				"planMode": {
					{SiteIDs: []string{"site-abc"}, Value: true},
				},
			},
		})

		ctx = eval.WithSite(ctx, "site-abc")
		log.Ctx(ctx).InfoContext(ctx, "test log")

		var logEntry map[string]any
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		featsGroup, ok := logEntry["feats"].(map[string]any)
		if assert.True(t, ok, "expected feats group in log output") {
			assert.Equal(t, true, featsGroup["solarV2"])
			assert.Equal(t, true, featsGroup["planMode"])
		}
	})

	t.Run("DefinedAndNormalizeCriteria", func(t *testing.T) {
		resetRegistryForTest(t)

		ffA := Bool("flagA", true)
		ffB := Bool("flagB", false)

		defs := Defined()
		if assert.Len(t, defs, 2) {
			assert.Equal(t, ffA, defs["flagA"])
			assert.Equal(t, ffB, defs["flagB"])
		}

		// Combines SiteIDs with same Value, deduplicates (first wins), splits mixed criteria,
		// and places SiteIDs rules before SitePercent.
		normalized, err := NormalizeCriteria([]Criteria{
			{SitePercent: 30, Value: true},
			{SiteIDs: []string{"site-1", "", "site-2"}, Value: true},
			{SiteIDs: []string{"site-3", "site-1"}, Value: false},
			{SiteIDs: []string{"site-4"}, Value: true},
			{SiteIDs: []string{"site-5"}, Value: false},
			{}, // empty criterion dropped
		})
		require.NoError(t, err)
		assert.Equal(t, []Criteria{
			{SiteIDs: []string{"site-1", "site-2", "site-4"}, Value: true},
			{SiteIDs: []string{"site-3", "site-5"}, Value: false},
			{SitePercent: 30, Value: true},
		}, normalized)

		// Rejects multiple SitePercent > 0 criteria
		_, err = NormalizeCriteria([]Criteria{
			{SitePercent: 20, Value: true},
			{SitePercent: 50, Value: false},
		})
		assert.ErrorContains(t, err, "at most one sitePercent criterion is allowed")

		// Rejects out-of-range SitePercent
		_, err = NormalizeCriteria([]Criteria{
			{SitePercent: -1, Value: true},
		})
		assert.ErrorContains(t, err, "sitePercent must be between 0 and 100")

		_, err = NormalizeCriteria([]Criteria{
			{SitePercent: 101, Value: true},
		})
		assert.ErrorContains(t, err, "sitePercent must be between 0 and 100")
	})
}
