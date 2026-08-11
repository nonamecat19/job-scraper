package retrieval

import (
	"log/slog"
	"time"

	"github.com/nonamecat19/job-scraper/ports"
)

// Option configures the engine. Options are applied in order, so a later one
// overrides an earlier one, and every option has a sensible zero-config
// default — NewEngine(store) alone produces a working direct-only engine.
type Option func(*engineConfig)

type engineConfig struct {
	identity       *BrowserIdentity
	detector       ports.ChallengeDetector
	logger         *slog.Logger
	extraRungs     []Rung
	ladder         *Ladder
	browserEnabled bool
	flaresolverURL string

	cheapRungRetestInterval time.Duration
	coolingOffThreshold     int
	coolingOffBaseDuration  time.Duration
}

const (
	defaultRetestInterval      = 6 * time.Hour
	defaultCoolingOffThreshold = 3
	defaultCoolingOffBase      = 30 * time.Minute
)

func newEngineConfig(opts []Option) *engineConfig {
	cfg := &engineConfig{
		identity:                DefaultBrowserIdentity(),
		detector:                DefaultDetector,
		logger:                  slog.Default(),
		cheapRungRetestInterval: defaultRetestInterval,
		coolingOffThreshold:     defaultCoolingOffThreshold,
		coolingOffBaseDuration:  defaultCoolingOffBase,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithIdentity sets the browser identity the direct rung wears. A nil identity
// is ignored, keeping the built-in default.
func WithIdentity(id *BrowserIdentity) Option {
	return func(c *engineConfig) {
		if id != nil {
			c.identity = id
		}
	}
}

// WithChallengeDetector replaces the classification of fetched bodies into
// read, challenged and refused.
func WithChallengeDetector(d ports.ChallengeDetector) Option {
	return func(c *engineConfig) {
		if d != nil {
			c.detector = d
		}
	}
}

// WithLogger sets where the engine reports escalations and blocks.
func WithLogger(l *slog.Logger) Option {
	return func(c *engineConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithBrowser adds the headless-browser rung to the ladder. If the browser
// cannot be launched the engine logs it and continues without the rung rather
// than failing to build.
func WithBrowser(enabled bool) Option {
	return func(c *engineConfig) { c.browserEnabled = enabled }
}

// WithFlareSolverr adds the FlareSolverr rung, pointed at a running sidecar.
// An empty URL is ignored.
func WithFlareSolverr(baseURL string) Option {
	return func(c *engineConfig) { c.flaresolverURL = baseURL }
}

// WithRung appends a consumer-supplied rung above the built-in ones. Use it to
// plug in a proxy pool or a paid unblocking API without forking the engine.
// Ignored when WithLadder replaces the ladder outright.
func WithRung(r Rung) Option {
	return func(c *engineConfig) {
		if r != nil {
			c.extraRungs = append(c.extraRungs, r)
		}
	}
}

// WithLadder replaces the whole ladder, discarding the built-in rungs. This is
// the escape hatch for a consumer that wants full control over escalation —
// most callers want WithRung instead.
func WithLadder(l *Ladder) Option {
	return func(c *engineConfig) { c.ladder = l }
}

// WithCheapRungRetest sets how long a learned expensive rung is trusted before
// the engine drops back to the cheapest one to see whether the host has
// relaxed. Zero or negative disables re-testing.
func WithCheapRungRetest(d time.Duration) Option {
	return func(c *engineConfig) { c.cheapRungRetestInterval = d }
}

// WithCoolingOff sets how many consecutive blocks a host may accumulate before
// the engine stops fetching it, and the base pause length. Each further block
// past the threshold doubles the pause.
func WithCoolingOff(threshold int, base time.Duration) Option {
	return func(c *engineConfig) {
		c.coolingOffThreshold = threshold
		c.coolingOffBaseDuration = base
	}
}
