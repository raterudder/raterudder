package feats

import (
	"context"
	"fmt"
	"hash/crc64"
	"log/slog"
	"maps"
	"sync"

	"github.com/raterudder/raterudder/pkg/log"
)

type contextKey int

const valuesContextKey contextKey = 1

var (
	registryMu   sync.RWMutex
	definedFlags = make(map[string]BoolFlag)
	crc64Table   = crc64.MakeTable(crc64.ECMA)
)

// BoolFlag represents a registered boolean feature flag.
type BoolFlag struct {
	name         string
	defaultValue bool
}

// Bool defines and registers a new boolean feature flag with a default value.
// It panics if the flag name is empty or already registered with a different default value.
func Bool(name string, defaultValue bool) BoolFlag {
	if name == "" {
		panic("feats: flag name cannot be empty")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if existing, exists := definedFlags[name]; exists {
		if existing.defaultValue != defaultValue {
			panic(fmt.Sprintf("feats: flag %q already registered with different default value (%v != %v)", name, existing.defaultValue, defaultValue))
		}
		return existing
	}
	f := BoolFlag{
		name:         name,
		defaultValue: defaultValue,
	}
	definedFlags[name] = f
	return f
}

// Name returns the unique identifier of the feature flag.
func (f BoolFlag) Name() string {
	return f.name
}

// Default returns the default boolean value of the feature flag.
func (f BoolFlag) Default() bool {
	return f.defaultValue
}

// Enabled evaluates whether the feature flag is enabled in ctx,
// falling back to the flag's default value if not set in ctx.
func (f BoolFlag) Enabled(ctx context.Context) bool {
	if vals, ok := ctx.Value(valuesContextKey).(map[string]bool); ok {
		if v, exists := vals[f.name]; exists {
			return v
		}
	}
	return f.defaultValue
}

// With returns a new context with this feature flag overridden to val.
func (f BoolFlag) With(ctx context.Context, val bool) context.Context {
	var next map[string]bool
	if existing, ok := ctx.Value(valuesContextKey).(map[string]bool); ok {
		next = maps.Clone(existing)
	} else {
		next = make(map[string]bool, 1)
	}
	next[f.name] = val
	return context.WithValue(ctx, valuesContextKey, next)
}

// All returns the evaluated boolean values for all registered feature flags in ctx.
func All(ctx context.Context) map[string]bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	res := make(map[string]bool, len(definedFlags))
	for name, flag := range definedFlags {
		res[name] = flag.defaultValue
	}
	if vals, ok := ctx.Value(valuesContextKey).(map[string]bool); ok {
		for k, v := range vals {
			if _, defined := res[k]; defined {
				res[k] = v
			}
		}
	}
	return res
}

// Criteria defines a single evaluation rule for a feature flag.
// A criterion matches a siteID if the siteID is in SiteIDs OR falls within SitePercent (0-100).
type Criteria struct {
	SiteIDs     []string `json:"siteIDs,omitempty"`
	SitePercent int      `json:"sitePercent,omitempty"`
	Value       bool     `json:"value"`
}

// Config is the serializable configuration of all feature flags for an environment.
type Config struct {
	Features map[string][]Criteria `json:"features,omitempty"`
}

type compiledCriteria struct {
	siteIDs     map[string]struct{}
	sitePercent int
	value       bool
}

// Evaluator holds precompiled feature flag rules for fast per-site evaluation.
type Evaluator struct {
	rules map[string][]compiledCriteria
}

// NewEvaluator compiles a Config into an Evaluator.
func NewEvaluator(cfg Config) *Evaluator {
	rules := make(map[string][]compiledCriteria, len(cfg.Features))
	for name, criteriaList := range cfg.Features {
		compiled := make([]compiledCriteria, 0, len(criteriaList))
		for _, c := range criteriaList {
			var siteSet map[string]struct{}
			if len(c.SiteIDs) > 0 {
				siteSet = make(map[string]struct{}, len(c.SiteIDs))
				for _, id := range c.SiteIDs {
					if id != "" {
						siteSet[id] = struct{}{}
					}
				}
			}
			compiled = append(compiled, compiledCriteria{
				siteIDs:     siteSet,
				sitePercent: c.SitePercent,
				value:       c.Value,
			})
		}
		rules[name] = compiled
	}
	return &Evaluator{rules: rules}
}

// Evaluate computes the values of all registered feature flags for the given siteID.
func (e *Evaluator) Evaluate(siteID string) map[string]bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	vals := make(map[string]bool, len(definedFlags))
	for name, flag := range definedFlags {
		val := flag.defaultValue
		if e != nil {
			if rules, ok := e.rules[name]; ok {
				for _, r := range rules {
					if r.matches(siteID) {
						val = r.value
						break
					}
				}
			}
		}
		vals[name] = val
	}
	return vals
}

// WithSite evaluates all registered flags for siteID, stores them in ctx,
// and attaches a "feats" slog.Group with all flag values to log.Ctx(ctx).
func (e *Evaluator) WithSite(ctx context.Context, siteID string) context.Context {
	vals := e.Evaluate(siteID)
	ctx = context.WithValue(ctx, valuesContextKey, vals)
	if len(vals) > 0 {
		attrs := make([]any, 0, len(vals))
		for k, v := range vals {
			attrs = append(attrs, slog.Bool(k, v))
		}
		ctx = log.With(ctx, log.Ctx(ctx).With(slog.Group("feats", attrs...)))
	}
	return ctx
}

func (c compiledCriteria) matches(siteID string) bool {
	if c.sitePercent >= 100 {
		return true
	}
	if siteID == "" {
		return false
	}
	if _, ok := c.siteIDs[siteID]; ok {
		return true
	}
	if c.sitePercent > 0 {
		return siteBucket(siteID) < c.sitePercent
	}
	return false
}

// siteBucket returns a deterministic integer in [0, 99] based on CRC-64 of siteID.
func siteBucket(siteID string) int {
	return int(crc64.Checksum([]byte(siteID), crc64Table) % 100)
}
