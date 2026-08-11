package retrieval

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/nonamecat19/job-scraper/ports"
)

// Engine is the bundled ports.Retriever: it walks a Ladder of strategies from
// cheapest to costliest, remembers per host which one worked, and pauses hosts
// that keep blocking it.
//
// The escalation itself is a chain of responsibility — each rung either
// produces a page or hands the request up — with the per-host memory in
// ports.StateStore deciding where the chain starts rather than always starting
// at the bottom.
type Engine struct {
	ladder   *Ladder
	store    ports.StateStore
	detector ports.ChallengeDetector
	log      *slog.Logger

	retestInterval      time.Duration
	coolingOffThreshold int
	coolingOffBase      time.Duration
}

var _ ports.Retriever = (*Engine)(nil)

// NewEngine builds the retrieval engine. With no options it produces a
// direct-only engine, which is the zero-config path: a consumer with an
// in-memory store and nothing else installed still gets working retrieval.
//
// Rungs whose dependency is missing are dropped from the ladder with a warning
// rather than failing construction, so a machine without Chrome degrades to
// direct-only instead of refusing to start.
func NewEngine(store ports.StateStore, opts ...Option) *Engine {
	cfg := newEngineConfig(opts)

	ladder := cfg.ladder
	if ladder == nil {
		ladder = buildLadder(cfg, store)
	}

	return &Engine{
		ladder:              ladder,
		store:               store,
		detector:            cfg.detector,
		log:                 cfg.logger,
		retestInterval:      cfg.cheapRungRetestInterval,
		coolingOffThreshold: cfg.coolingOffThreshold,
		coolingOffBase:      cfg.coolingOffBaseDuration,
	}
}

func buildLadder(cfg *engineConfig, store ports.StateStore) *Ladder {
	rungs := []Rung{NewDirectRung(cfg.identity, store, cfg.detector)}

	if cfg.browserEnabled {
		browser, err := NewBrowserRung(cfg.detector)
		if err != nil {
			cfg.logger.Warn("retrieval: browser rung unavailable, will skip", "error", err)
		} else {
			rungs = append(rungs, browser)
		}
	}

	if cfg.flaresolverURL != "" {
		rungs = append(rungs, NewFlareSolverrRung(cfg.flaresolverURL, cfg.detector))
		cfg.logger.Info("retrieval: flaresolverr rung configured", "url", cfg.flaresolverURL)
	}

	rungs = append(rungs, cfg.extraRungs...)
	return NewLadder(rungs...)
}

// Ladder exposes the configured strategies, in cost order. Useful for
// operator-facing diagnostics: it answers "what could this engine even try?".
func (e *Engine) Ladder() *Ladder { return e.ladder }

// Close releases every rung.
func (e *Engine) Close() error { return e.ladder.Close() }

func (e *Engine) Fetch(ctx context.Context, req ports.FetchRequest) (ports.FetchResult, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return ports.FetchResult{}, fmt.Errorf("retrieval: parse url %q: %w", req.URL, err)
	}
	host := u.Host

	state, err := e.store.Get(ctx, host)
	if err != nil {
		e.log.Warn("retrieval: get state failed, proceeding from cheapest rung", "host", host, "error", err)
	}

	// A host whose crawl-delay was never resolved gets it looked up in the
	// background; this run proceeds at the default rate.
	if state != nil && state.CrawlDelaySeconds == nil {
		go func(h string) { _ = e.store.FetchAndSetCrawlDelay(context.Background(), h) }(host)
	}

	if deferred, res := e.checkCoolingOff(state, req); deferred {
		return res, nil
	}

	rung := e.startRung(state, host)
	if rung == nil {
		return ports.FetchResult{
			Outcome: outcome(ports.PageDeferred, "none", "no retrieval rungs configured", req.URL),
		}, nil
	}

	return e.climb(ctx, rung, host, req), nil
}

// climb runs the chain: try a rung, and on failure hand the request to the next
// one up until a rung reads the page or the ladder is exhausted.
func (e *Engine) climb(ctx context.Context, rung Rung, host string, req ports.FetchRequest) ports.FetchResult {
	for {
		pageOutcome, body := rung.Fetch(ctx, req)

		if pageOutcome.Status == ports.PageRead {
			_ = e.store.RecordSuccess(ctx, host, rung.Key())
			e.log.Info("retrieval: success", "host", host, "rung", rung.Key())
			return ports.FetchResult{Outcome: pageOutcome, Body: body}
		}

		reason := string(pageOutcome.Status) + " via " + rung.Key()

		// A credentialed request is pinned: replaying an authenticated session
		// through a different transport invalidates it and risks the account.
		if req.UsesUserAccount {
			_ = e.recordBlock(ctx, host, reason)
			e.log.Warn("retrieval: credentialed source blocked, not escalating",
				"host", host, "rung", rung.Key(), "reason", pageOutcome.Reason)
			return ports.FetchResult{Outcome: pageOutcome, Body: body}
		}

		next, hasNext := e.ladder.Next(rung.Key())
		if !hasNext {
			_ = e.recordBlock(ctx, host, reason)
			e.log.Warn("retrieval: all rungs exhausted",
				"host", host, "lastRung", rung.Key(), "reason", pageOutcome.Reason)
			return ports.FetchResult{Outcome: pageOutcome, Body: body}
		}

		if !next.Available(ctx) {
			_ = e.recordBlock(ctx, host, reason)
			e.log.Warn("retrieval: next rung unavailable, stopping escalation",
				"host", host, "from", rung.Key(), "next", next.Key())
			return ports.FetchResult{Outcome: pageOutcome, Body: body}
		}

		e.log.Info("retrieval: escalating rung",
			"host", host, "from", rung.Key(), "to", next.Key(), "reason", pageOutcome.Reason)
		rung = next
	}
}

// checkCoolingOff reports whether host is currently paused, and if so the
// deferral to return instead of fetching.
func (e *Engine) checkCoolingOff(state *ports.HostState, req ports.FetchRequest) (bool, ports.FetchResult) {
	if state == nil || state.CoolingOffUntil == nil {
		return false, ports.FetchResult{}
	}
	coolOff := *state.CoolingOffUntil
	if !time.Now().Before(coolOff) {
		return false, ports.FetchResult{}
	}
	remaining := time.Until(coolOff).Round(time.Second)
	reason := fmt.Sprintf("cooling off until %s (%s remaining)", coolOff.Format(time.RFC3339), remaining)
	return true, ports.FetchResult{
		Outcome: outcome(ports.PageDeferred, "none", reason, req.URL),
	}
}

// startRung picks where the chain begins: the rung this host last needed, or
// the cheapest one when there is no history or the learned preference is due
// for a re-test.
func (e *Engine) startRung(state *ports.HostState, host string) Rung {
	if state == nil {
		return e.ladder.First()
	}
	rung, ok := e.ladder.Find(state.CurrentRung)
	if !ok {
		return e.ladder.First()
	}
	if e.ladder.IsCheapest(rung.Key()) {
		return rung
	}
	if e.retestInterval > 0 && state.RungLastVerifiedAt != nil {
		if time.Now().After(state.RungLastVerifiedAt.Add(e.retestInterval)) {
			e.log.Info("retrieval: re-testing cheap rung", "host", host, "currentRung", rung.Key())
			return e.ladder.First()
		}
	}
	return rung
}

// recordBlock stores the block and, once a host has crossed the threshold,
// pauses it. The pause doubles for each block past the threshold, so a host
// that keeps refusing is backed off from exponentially rather than hammered.
func (e *Engine) recordBlock(ctx context.Context, host string, reason string) error {
	if err := e.store.RecordBlock(ctx, host, reason); err != nil {
		return err
	}
	state, err := e.store.Get(ctx, host)
	if err != nil {
		return err
	}
	if state == nil || state.ConsecutiveBlocks < int32(e.coolingOffThreshold) {
		return nil
	}

	excess := state.ConsecutiveBlocks - int32(e.coolingOffThreshold)
	duration := e.coolingOffBase
	if excess > 0 {
		duration *= time.Duration(1 << excess)
	}
	coolOff := time.Now().Add(duration)
	state.CoolingOffUntil = &coolOff
	_ = e.store.Upsert(ctx, host, state)
	e.log.Warn("retrieval: cooling off activated",
		"host", host, "duration", duration, "until", coolOff.Format(time.RFC3339))
	return nil
}

func (e *Engine) HostStatus(ctx context.Context, host string) (ports.HostStatus, error) {
	state, err := e.store.Get(ctx, host)
	if err != nil {
		return ports.HostStatus{}, err
	}

	status := ports.HostStatus{Host: host}
	if state != nil {
		status.IdentityVersion = state.IdentityVersion
		status.CurrentRung = state.CurrentRung
		if state.CrawlDelaySeconds != nil {
			v := int(*state.CrawlDelaySeconds)
			status.CrawlDelaySeconds = &v
		}
		if state.LastBlockAt != nil {
			t := *state.LastBlockAt
			status.LastBlockAt = &t
		}
		if state.LastBlockReason != nil {
			status.LastBlockReason = *state.LastBlockReason
		}
		if state.CoolingOffUntil != nil {
			t := *state.CoolingOffUntil
			status.CoolingOffUntil = &t
		}
	}

	rps, source := DefaultTransport.RateFor(host)
	status.Pacing = ports.HostPacing{
		RequestsPerSecond: rps,
		IntervalSeconds:   1 / rps,
		Source:            source,
	}
	return status, nil
}

func (e *Engine) ClearRungPreference(ctx context.Context, host string) error {
	return e.store.ClearRung(ctx, host)
}

func (e *Engine) ClearCookies(ctx context.Context, host string) error {
	return e.store.ClearCookies(ctx, host)
}

func (e *Engine) OverrideCoolingOff(ctx context.Context, host string) (time.Duration, error) {
	state, err := e.store.Get(ctx, host)
	if err != nil {
		return 0, err
	}
	if state == nil || state.CoolingOffUntil == nil {
		return 0, nil
	}
	remaining := time.Until(*state.CoolingOffUntil)
	state.CoolingOffUntil = nil
	state.ConsecutiveBlocks = 0
	if err := e.store.Upsert(ctx, host, state); err != nil {
		return 0, err
	}
	if remaining < 0 {
		return 0, nil
	}
	return remaining.Round(time.Second), nil
}
