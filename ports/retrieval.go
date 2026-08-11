package ports

import (
	"context"
	"time"
)

// PageStatus is how a single page fetch ended.
type PageStatus string

const (
	PageRead        PageStatus = "read"
	PageChallenged  PageStatus = "challenged"
	PageRefused     PageStatus = "refused"
	PageUnparseable PageStatus = "unparseable"
	PageDeferred    PageStatus = "deferred"
)

// PageOutcome describes one fetch attempt in enough detail for an operator to
// tell a bot-check apart from a hard refusal apart from a paused host.
type PageOutcome struct {
	Status PageStatus `json:"status"`
	Method string     `json:"method"`
	Reason string     `json:"reason,omitempty"`
	URL    string     `json:"url"`
}

// RunVerdict summarises a whole source run.
type RunVerdict string

const (
	VerdictSuccess RunVerdict = "success"
	VerdictPartial RunVerdict = "partial"
	VerdictBlocked RunVerdict = "blocked"
)

// FetchRequest is one page to retrieve.
type FetchRequest struct {
	// URL is the absolute page URL.
	URL string
	// Headers override or extend the browser identity's own headers.
	Headers map[string]string
	// UsesUserAccount pins the request to the cheapest rung: an authenticated
	// session must not be replayed through a different transport.
	UsesUserAccount bool
	// RefererPage, when set, is sent as the Referer so the request looks like
	// in-site navigation rather than a cold hit.
	RefererPage string
}

// FetchResult pairs the page body with the outcome that produced it. Body may
// be non-empty even when Outcome.Status is not PageRead — a challenge page is
// still a page, and callers log it.
type FetchResult struct {
	Outcome PageOutcome
	Body    string
}

// HostPacing is the request rate currently applied to a host and where that
// rate came from (robots.txt crawl-delay, a per-host override, the default).
type HostPacing struct {
	RequestsPerSecond float64
	IntervalSeconds   float64
	Source            string
}

// HostStatus is the operator-facing view of everything the library remembers
// about one host.
type HostStatus struct {
	Host              string
	IdentityVersion   string
	CurrentRung       string
	LastBlockAt       *time.Time
	LastBlockReason   string
	CoolingOffUntil   *time.Time
	CrawlDelaySeconds *int
	Pacing            HostPacing
}

// Retriever is the page-fetching port. The bundled implementation walks a
// ladder of increasingly expensive strategies, but a consumer may supply any
// implementation — a fixture server in tests, a shared proxy pool in
// production — as long as it honours the contract.
//
// Fetch must not return an error for a blocked page: a challenge, a refusal and
// a cooling-off deferral are all successful observations, reported through
// FetchResult.Outcome. It errors only when the request could not be formed or
// attempted at all.
type Retriever interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)

	// HostStatus reports what the retriever remembers about host.
	HostStatus(ctx context.Context, host string) (HostStatus, error)

	// ClearRungPreference forgets the learned strategy for host, so the next
	// fetch starts from the cheapest one again.
	ClearRungPreference(ctx context.Context, host string) error

	// ClearCookies drops the stored session for host.
	ClearCookies(ctx context.Context, host string) error

	// OverrideCoolingOff lifts an active cooling-off period early and returns
	// how much of it was skipped.
	OverrideCoolingOff(ctx context.Context, host string) (time.Duration, error)
}

// ChallengeDetector decides whether a fetched body is a real page or a bot
// check. It is a port so a consumer facing a host with unusual challenge
// markup can substitute its own detection without forking the engine.
type ChallengeDetector interface {
	// IsChallenged reports a soft block: a CAPTCHA, an interstitial, a
	// "checking your browser" page. Escalating to a costlier rung may help.
	IsChallenged(body string, statusCode int) bool

	// IsRefused reports a hard block: the host has decided not to serve this
	// client. Escalating is unlikely to help and backing off is correct.
	IsRefused(body string, statusCode int) bool
}
