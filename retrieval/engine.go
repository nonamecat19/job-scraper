package retrieval

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"
)

var _ Service = (*engineImpl)(nil)

// EngineOpts is the plain-struct configuration the engine needs; the app maps
// its own config onto it so the library stays free of app dependencies.
type EngineOpts struct {
	BrowserEnabled          bool
	FlaresolverrURL         string
	CheapRungRetestInterval time.Duration
	CoolingOffThreshold     int
	CoolingOffBaseDuration  time.Duration
}

type engineImpl struct {
	identity     *BrowserIdentity
	store        StateStorePort
	opts         EngineOpts
	direct       *directRung
	browser      *browserRung
	flaresolverr *flareSolverrRung
}

// NewEngine builds the retrieval engine. A nil identity selects the built-in
// default, so a consumer with no identity of its own can construct the engine
// with NewEngine(nil, store, EngineOpts{}) and still climb the ladder.
func NewEngine(identity *BrowserIdentity, store StateStorePort, opts EngineOpts) Service {
	if identity == nil {
		identity = DefaultBrowserIdentity()
	}
	svc := &engineImpl{
		identity: identity,
		store:    store,
		opts:     opts,
		direct:   newDirectRung(identity, store),
	}

	if opts.BrowserEnabled {
		browser, err := newBrowserRung()
		if err != nil {
			slog.Warn("retrieval: browser rung unavailable, will skip", "error", err)
		} else {
			svc.browser = browser
		}
	}

	if opts.FlaresolverrURL != "" {
		svc.flaresolverr = newFlareSolverrRung(opts.FlaresolverrURL)
		slog.Info("retrieval: flaresolverr rung configured", "url", opts.FlaresolverrURL)
	}

	return svc
}

func (s *engineImpl) Close() {
	if s.browser != nil {
		s.browser.Close()
	}
	if s.flaresolverr != nil {
		s.flaresolverr.Close()
	}
}

func (s *engineImpl) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return FetchResult{}, fmt.Errorf("retrieval: parse url %q: %w", req.URL, err)
	}
	host := u.Host

	state, err := s.store.Get(ctx, host)
	if err != nil {
		slog.Warn("retrieval: get state failed, proceeding with direct", "host", host, "error", err)
	}

	if state != nil && state.CrawlDelaySeconds == nil {
		go func(h string) { _ = s.store.FetchAndSetCrawlDelay(context.Background(), h) }(host)
	}

	if state != nil && state.CoolingOffUntil != nil {
		coolOff := *state.CoolingOffUntil
		if time.Now().Before(coolOff) {
			remaining := time.Until(coolOff).Round(time.Second)
			return FetchResult{
				Outcome: PageOutcome{
					Status: PageDeferred,
					Method: "none",
					Reason: fmt.Sprintf("cooling off until %s (%s remaining)", coolOff.Format(time.RFC3339), remaining),
					URL:    req.URL,
				},
			}, nil
		}
	}

	startRung := RungDirect
	if state != nil {
		if r, ok := RungForKey(state.CurrentRung); ok {
			startRung = r
		}
		if startRung.Order > 0 && state.RungLastVerifiedAt != nil {
			retestAfter := state.RungLastVerifiedAt.Add(s.opts.CheapRungRetestInterval)
			if time.Now().After(retestAfter) {
				slog.Info("retrieval: re-testing cheap rung", "host", host, "currentRung", startRung.Key)
				startRung = RungDirect
			}
		}
	}

	currentRung := startRung
	for {
		rung := currentRung
		outcome, body := s.tryRung(ctx, rung, req)

		if outcome.Status == PageRead {
			_ = s.store.RecordSuccess(ctx, host, rung.Key)
			slog.Info("retrieval: success", "host", host, "rung", rung.Key)
			return FetchResult{Outcome: outcome, Body: body}, nil
		}

		if req.UsesUserAccount {
			_ = s.recordBlock(ctx, host, string(outcome.Status)+" via "+rung.Key)
			slog.Warn("retrieval: credential adapter blocked, not escalating", "host", host, "rung", rung.Key, "reason", outcome.Reason)
			return FetchResult{Outcome: outcome, Body: body}, nil
		}

		next, hasNext := rung.Next()
		if !hasNext {
			_ = s.recordBlock(ctx, host, string(outcome.Status)+" via "+rung.Key)
			slog.Warn("retrieval: all rungs exhausted", "host", host, "lastRung", rung.Key, "reason", outcome.Reason)
			return FetchResult{Outcome: outcome, Body: body}, nil
		}

		if !s.rungAvailable(ctx, next) {
			slog.Warn("retrieval: next rung unavailable, stopping escalation", "host", host, "from", rung.Key, "next", next.Key)
			_ = s.recordBlock(ctx, host, string(outcome.Status)+" via "+rung.Key)
			return FetchResult{Outcome: outcome, Body: body}, nil
		}

		slog.Info("retrieval: escalating rung", "host", host, "from", rung.Key, "to", next.Key, "reason", outcome.Reason)
		currentRung = next
	}
}

func (s *engineImpl) tryRung(ctx context.Context, rung RetrievalMethod, req FetchRequest) (PageOutcome, string) {
	switch rung.Key {
	case RungDirect.Key:
		return s.tryDirect(ctx, req)
	case RungBrowser.Key:
		return s.tryBrowser(ctx, req)
	case RungFlareSolverr.Key:
		return s.tryFlareSolverr(ctx, req)
	default:
		return PageOutcome{Status: PageChallenged, Method: rung.Key, Reason: "unknown rung", URL: req.URL}, ""
	}
}

func (s *engineImpl) tryDirect(ctx context.Context, req FetchRequest) (PageOutcome, string) {
	body, statusCode, err := s.direct.Fetch(ctx, req.URL, req.Headers)
	if err != nil {
		return PageOutcome{Status: PageChallenged, Method: "direct", Reason: err.Error(), URL: req.URL}, body
	}
	if IsChallenged(body, statusCode) {
		return PageOutcome{Status: PageChallenged, Method: "direct", Reason: fmt.Sprintf("challenge detected (status %d)", statusCode), URL: req.URL}, body
	}
	if IsRefused(body, statusCode) {
		return PageOutcome{Status: PageRefused, Method: "direct", Reason: fmt.Sprintf("refused (status %d)", statusCode), URL: req.URL}, body
	}
	return PageOutcome{Status: PageRead, Method: "direct", URL: req.URL}, body
}

func (s *engineImpl) tryBrowser(ctx context.Context, req FetchRequest) (PageOutcome, string) {
	if s.browser == nil {
		return PageOutcome{Status: PageChallenged, Method: "browser", Reason: "browser not available", URL: req.URL}, ""
	}
	body, err := s.browser.Fetch(ctx, req.URL)
	if err != nil {
		if ctx.Err() != nil {
			return PageOutcome{Status: PageChallenged, Method: "browser", Reason: "context cancelled", URL: req.URL}, body
		}
		return PageOutcome{Status: PageChallenged, Method: "browser", Reason: err.Error(), URL: req.URL}, body
	}
	if IsChallenged(body, 200) {
		return PageOutcome{Status: PageChallenged, Method: "browser", Reason: "challenge detected", URL: req.URL}, body
	}
	return PageOutcome{Status: PageRead, Method: "browser", URL: req.URL}, body
}

func (s *engineImpl) tryFlareSolverr(ctx context.Context, req FetchRequest) (PageOutcome, string) {
	if s.flaresolverr == nil {
		return PageOutcome{Status: PageChallenged, Method: "flaresolverr", Reason: "flaresolverr not configured", URL: req.URL}, ""
	}
	body, statusCode, err := s.flaresolverr.Fetch(ctx, req.URL)
	if err != nil {
		if ctx.Err() != nil {
			return PageOutcome{Status: PageChallenged, Method: "flaresolverr", Reason: "context cancelled", URL: req.URL}, body
		}
		return PageOutcome{Status: PageChallenged, Method: "flaresolverr", Reason: err.Error(), URL: req.URL}, body
	}
	if statusCode >= 500 {
		return PageOutcome{Status: PageChallenged, Method: "flaresolverr", Reason: fmt.Sprintf("server error (status %d)", statusCode), URL: req.URL}, body
	}
	if IsChallenged(body, statusCode) {
		return PageOutcome{Status: PageChallenged, Method: "flaresolverr", Reason: "challenge detected", URL: req.URL}, body
	}
	if IsRefused(body, statusCode) {
		return PageOutcome{Status: PageRefused, Method: "flaresolverr", Reason: fmt.Sprintf("refused (status %d)", statusCode), URL: req.URL}, body
	}
	return PageOutcome{Status: PageRead, Method: "flaresolverr", URL: req.URL}, body
}

func (s *engineImpl) rungAvailable(ctx context.Context, rung RetrievalMethod) bool {
	switch rung.Key {
	case RungBrowser.Key:
		return s.browser != nil
	case RungFlareSolverr.Key:
		return s.flaresolverr != nil && s.flaresolverr.Available(ctx)
	default:
		return true
	}
}

func (s *engineImpl) recordBlock(ctx context.Context, host string, reason string) error {
	if err := s.store.RecordBlock(ctx, host, reason); err != nil {
		return err
	}
	state, err := s.store.Get(ctx, host)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	if state.ConsecutiveBlocks >= int32(s.opts.CoolingOffThreshold) {
		excess := state.ConsecutiveBlocks - int32(s.opts.CoolingOffThreshold)
		duration := s.opts.CoolingOffBaseDuration
		if excess > 0 {
			duration = duration * time.Duration(1<<excess)
		}
		coolOff := time.Now().Add(duration)
		state.CoolingOffUntil = &coolOff
		_ = s.store.Upsert(ctx, host, state)
		slog.Warn("retrieval: cooling off activated", "host", host, "duration", duration, "until", coolOff.Format(time.RFC3339))
	}
	return nil
}

func (s *engineImpl) HostStatus(ctx context.Context, host string) (HostStatus, error) {
	state, err := s.store.Get(ctx, host)
	if err != nil {
		return HostStatus{}, err
	}
	status := HostStatus{Host: host}
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
	status.Pacing = HostPacing{
		RequestsPerSecond: rps,
		IntervalSeconds:   1 / rps,
		Source:            source,
	}
	return status, nil
}

func (s *engineImpl) ClearRungPreference(ctx context.Context, host string) error {
	return s.store.ClearRung(ctx, host)
}

func (s *engineImpl) ClearCookies(ctx context.Context, host string) error {
	return s.store.ClearCookies(ctx, host)
}

func (s *engineImpl) OverrideCoolingOff(ctx context.Context, host string) (time.Duration, error) {
	state, err := s.store.Get(ctx, host)
	if err != nil {
		return 0, err
	}
	if state == nil || state.CoolingOffUntil == nil {
		return 0, nil
	}
	remaining := time.Until(*state.CoolingOffUntil)
	state.CoolingOffUntil = nil
	state.ConsecutiveBlocks = 0
	if err := s.store.Upsert(ctx, host, state); err != nil {
		return 0, err
	}
	if remaining < 0 {
		return 0, nil
	}
	return remaining.Round(time.Second), nil
}
