package ports

import (
	"context"

	"github.com/nonamecat19/job-scraper/model"
)

// JobSource is the core port: one place jobs can be read from. Every adapter in
// adapters/ implements it, and every decorator in adapter/middleware wraps it.
//
// Config carries the per-source settings the caller stored for this source
// (API keys, base URLs, credentials). A source that needs nothing from it must
// still accept a nil map.
type JobSource interface {
	// Key is the stable identifier of this source. It must be unique within a
	// Registry, constant for the lifetime of the process, and is copied into
	// every NormalizedJob the source returns.
	Key() string

	// Kind classifies how the source is read. It drives pacing and operator
	// reporting, not dispatch.
	Kind() model.SourceKind

	// Search returns the postings matching query. It returns a partial slice
	// with a nil error when some pages were readable and others were not; it
	// errors only when nothing could be read.
	Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error)

	// HealthCheck reports whether the source is currently reachable and
	// correctly configured. It must be cheap enough to run on a schedule.
	HealthCheck(ctx context.Context, config map[string]any) (bool, error)
}

// Closer is implemented by sources holding resources — a browser, a pooled
// client, a login session — that must be released. Registry.Close calls it on
// every source that has it.
type Closer interface {
	Close() error
}

// DetailNeeder is implemented by sources whose Search returns postings with an
// incomplete Description, so the caller knows a follow-up detail pass is
// required before the posting is usable.
type DetailNeeder interface {
	NeedsDetail() bool
}

// Credentialed is implemented by sources that read pages while logged in as the
// end user. A credentialed source is never escalated up the retrieval ladder:
// swapping transport under an authenticated session invalidates it and risks
// the user's account.
type Credentialed interface {
	UsesUserAccount() bool
}

// PostingReader is implemented by sources that can read a single posting page
// into a complete NormalizedJob. Optional: a source without it cannot serve
// manual add, and its hosts degrade to the fill-in path.
//
// Implementors must honour six rules:
//
//  1. MatchesPostingURL does no I/O and never panics on malformed input — it is
//     called for every registered source on every add, in registry order, and
//     the first claim wins.
//  2. It returns false for search URLs on its own host. A Djinni preset-search
//     URL is a Djinni URL that is not a posting, and the caller reports
//     not_a_posting precisely because the host was claimed but the shape was
//     rejected.
//  3. ReadPosting returns partial results rather than erroring when the page
//     loads but some fields are absent — that partial result is what feeds the
//     fill-in draft. It errors only when the page could not be read at all.
//  4. It honours the context deadline and returns context.DeadlineExceeded
//     wrapped no more deeply than errors.Is can see.
//  5. It sets SourceKey to its own Key() and resolves URL to an absolute,
//     canonical form.
//  6. It uses the same retrieval path as the source's other methods, so pacing
//     and ladder escalation apply unchanged.
type PostingReader interface {
	// MatchesPostingURL reports whether rawURL is a single posting on this
	// source's host. False for search pages, listings, and other hosts.
	// Must not perform I/O.
	MatchesPostingURL(rawURL string) bool

	// ReadPosting reads one posting page into a NormalizedJob. Title, Company,
	// URL and Description are filled where the page provides them; the caller
	// decides whether the result is complete enough to store.
	ReadPosting(ctx context.Context, rawURL string, config map[string]any) (model.NormalizedJob, error)
}

// EmployerOutcome is how one employer board fared in a single run.
type EmployerOutcome string

const (
	EmployerOutcomeRead       EmployerOutcome = "read"
	EmployerOutcomeNotFound   EmployerOutcome = "not_found"
	EmployerOutcomeUnreadable EmployerOutcome = "unreadable"
	EmployerOutcomeRefused    EmployerOutcome = "refused"
	EmployerOutcomeNoPostings EmployerOutcome = "no_postings"
)

// EmployerRunOutcome is the per-employer detail an ATS vendor source records so
// an operator can see which boards were read and which were not.
type EmployerRunOutcome struct {
	EmployerIdentifier string          `json:"employerIdentifier"`
	Outcome            EmployerOutcome `json:"outcome"`
	PostingsFound      int             `json:"postingsFound"`
}

// EmployerReporter is implemented by sources that iterate a roster of employer
// boards and can report how each one fared in the last run.
type EmployerReporter interface {
	LastRunDetail() []EmployerRunOutcome
}
