// Package atsboard is the shared implementation behind every applicant
// tracking system the library reads — Greenhouse, Lever, Ashby, Workable,
// SmartRecruiters.
//
// Those five differ only in the URL they call and the JSON they get back.
// Everything else — walking the roster, capping a runaway board, classifying
// each employer's outcome, recording the run, deciding whether the vendor as a
// whole failed — is identical, and lives here once.
//
// Source is the template; Fetcher is the one step each vendor fills in. A new
// ATS vendor is therefore a Fetcher and nothing else:
//
//	type Fetcher struct{}
//
//	func (Fetcher) Vendor() string { return "acme-ats" }
//	func (Fetcher) FetchEmployer(ctx context.Context, e ports.EmployerBoard) (int, []model.NormalizedJob, error) {
//		// one HTTP call, one loop mapping the response into NormalizedJob
//	}
package atsboard

import (
	"context"
	"fmt"
	"sync"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// MaxPostingsPerEmployer caps how many postings a single employer board can
// contribute to one run, so a misconfigured board cannot flood a search.
const MaxPostingsPerEmployer = 500

// Fetcher is the vendor-specific step of the template: read one employer's
// board and return its postings.
//
// The status code is returned separately from the error because it is what
// distinguishes a board that has been deleted (404) from one that is refusing
// this client (403) from one that is broken (500) — three situations an
// operator responds to differently, and which an error string alone conflates.
// Return the status even when returning an error; return 0 when the request
// never reached the server.
type Fetcher interface {
	// Vendor is the ATS vendor key. It is both the source key and the roster
	// column the boards are looked up by, so the two can never drift apart.
	Vendor() string

	// FetchEmployer reads one board. Postings must arrive complete: an ATS API
	// serves the full description inline, so no detail pass follows.
	FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (statusCode int, jobs []model.NormalizedJob, err error)
}

// Source is the ports.JobSource every ATS vendor is built from. It holds the
// roster walk and the outcome bookkeeping; the vendor supplies only a Fetcher.
type Source struct {
	fetcher Fetcher
	roster  ports.Roster

	mu     sync.Mutex
	detail []ports.EmployerRunOutcome
}

var (
	_ ports.JobSource        = (*Source)(nil)
	_ ports.EmployerReporter = (*Source)(nil)
)

// New builds the source for one ATS vendor. A nil roster is an error rather
// than an empty run: a vendor source with no roster has nothing to crawl, and
// silently returning zero postings would look like a working source finding
// nothing.
func New(fetcher Fetcher, roster ports.Roster) (*Source, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("atsboard: a Fetcher is required")
	}
	if roster == nil {
		return nil, fmt.Errorf("atsboard: source %q requires a roster of employer boards", fetcher.Vendor())
	}
	return &Source{fetcher: fetcher, roster: roster}, nil
}

// Provider returns the adapter.Provider that builds this vendor from Deps. A
// vendor package registers it from init, so importing the package is all it
// takes to make the source available.
func Provider(fetcher Fetcher) adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: fetcher.Vendor(),
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			return New(fetcher, deps.Roster)
		},
	}
}

func (s *Source) Key() string { return s.fetcher.Vendor() }

// Kind is always API: every supported ATS serves JSON over HTTP.
func (s *Source) Kind() model.SourceKind { return model.SourceKindAPI }

// Search walks every enabled board for this vendor and concatenates their
// postings. The query is ignored: an ATS board is a fixed list of that
// employer's openings, with no search endpoint to pass keywords to. Filtering
// happens downstream, on the postings this returns.
//
// A board that fails is recorded and skipped rather than failing the run — one
// employer's outage is not the vendor's outage. Only when every board failed
// does the whole run error, because at that point the vendor really is down (or
// the roster is wrong) and reporting success with zero postings would hide it.
func (s *Source) Search(ctx context.Context, _ model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	vendor := s.fetcher.Vendor()

	employers, err := s.roster.ListForRun(ctx, vendor)
	if err != nil {
		return nil, fmt.Errorf("atsboard: %s: list roster: %w", vendor, err)
	}

	var jobs []model.NormalizedJob
	detail := make([]ports.EmployerRunOutcome, 0, len(employers))
	successCount := 0

	for _, e := range employers {
		status, found, ferr := s.fetcher.FetchEmployer(ctx, e)

		outcome := ClassifyOutcome(status, found, ferr)
		if outcome == ports.EmployerOutcomeRead || outcome == ports.EmployerOutcomeNoPostings {
			successCount++
		}
		if len(found) > MaxPostingsPerEmployer {
			found = found[:MaxPostingsPerEmployer]
		}
		if ferr == nil {
			jobs = append(jobs, found...)
		}

		detail = append(detail, ports.EmployerRunOutcome{
			EmployerIdentifier: e.EmployerIdentifier,
			Outcome:            outcome,
			PostingsFound:      len(found),
		})

		// A roster that cannot record the outcome does not invalidate the
		// postings already read, so the failure is swallowed and the walk
		// continues.
		_ = s.roster.RecordRunOutcome(ctx, e.ID, len(found))
	}

	s.setDetail(detail)

	if len(employers) > 0 && successCount == 0 {
		return jobs, fmt.Errorf("atsboard: %s: 0 of %d employers read successfully", vendor, len(employers))
	}
	return jobs, nil
}

// HealthCheck probes the first board on the roster. An empty roster is healthy:
// there is nothing configured to be broken, and reporting otherwise would have
// every unused vendor show up as failing.
func (s *Source) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	vendor := s.fetcher.Vendor()

	employers, err := s.roster.ListForRun(ctx, vendor)
	if err != nil {
		return false, err
	}
	if len(employers) == 0 {
		return true, nil
	}

	status, _, err := s.fetcher.FetchEmployer(ctx, employers[0])
	if err != nil {
		return false, fmt.Errorf("atsboard: %s: health check on %s failed (status %d): %w",
			vendor, employers[0].EmployerIdentifier, status, err)
	}
	return true, nil
}

// LastRunDetail reports how each board fared in the most recent Search.
func (s *Source) LastRunDetail() []ports.EmployerRunOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ports.EmployerRunOutcome, len(s.detail))
	copy(out, s.detail)
	return out
}

func (s *Source) setDetail(d []ports.EmployerRunOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detail = d
}

// EmployerHealthChecker returns a probe for a single board that is not on the
// roster yet. A consumer calls it before adding a board, to tell a valid
// employer identifier from a typo.
func (s *Source) EmployerHealthChecker() ports.EmployerHealthChecker {
	return HealthChecker(s.fetcher)
}

// HealthChecker builds a single-board probe directly from a Fetcher, for
// callers validating an identifier without a configured Source.
func HealthChecker(fetcher Fetcher) ports.EmployerHealthChecker {
	return func(ctx context.Context, employerIdentifier string) (int, error) {
		_, jobs, err := fetcher.FetchEmployer(ctx, ports.EmployerBoard{
			EmployerIdentifier: employerIdentifier,
			DisplayName:        employerIdentifier,
		})
		if err != nil {
			return 0, err
		}
		return len(jobs), nil
	}
}

// ClassifyOutcome turns one board's result into the outcome an operator reads.
//
// The distinction that matters is between "read it, there was nothing there"
// and "could not read it": the first is a normal state for a company with no
// openings, the second needs attention. Everything else splits the failures by
// what the caller should do — remove a 404 board, back off a 403, retry a 500.
func ClassifyOutcome(status int, jobs []model.NormalizedJob, err error) ports.EmployerOutcome {
	switch {
	case err == nil:
		if len(jobs) == 0 {
			return ports.EmployerOutcomeNoPostings
		}
		return ports.EmployerOutcomeRead
	case status == 404:
		return ports.EmployerOutcomeNotFound
	case status == 401 || status == 403 || status == 429:
		return ports.EmployerOutcomeRefused
	default:
		return ports.EmployerOutcomeUnreadable
	}
}
