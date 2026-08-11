package adapters

import (
	"context"
	"fmt"
	"sync"

	"github.com/job-finder/jobscraper/adapter"
	"github.com/job-finder/jobscraper/model"
	"github.com/job-finder/jobscraper/rosterport"
)

// MaxPostingsPerEmployer caps how many postings a single employer board can
// contribute to one run, so a misconfigured board cannot flood a search.
const MaxPostingsPerEmployer = 500

type employerFetcher func(ctx context.Context, employer rosterport.EmployerBoard) (statusCode int, jobs []model.NormalizedJob, err error)

type boardRunState struct {
	mu     sync.Mutex
	detail []adapter.EmployerRunOutcome
}

func (b *boardRunState) LastRunDetail() []adapter.EmployerRunOutcome {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]adapter.EmployerRunOutcome, len(b.detail))
	copy(out, b.detail)
	return out
}

func (b *boardRunState) setDetail(d []adapter.EmployerRunOutcome) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.detail = d
}

func runBoardVendor(ctx context.Context, rosterSvc rosterport.RosterPort, state *boardRunState, vendor string, fetch employerFetcher) ([]model.NormalizedJob, error) {
	employers, err := rosterSvc.ListForRun(ctx, vendor)
	if err != nil {
		return nil, fmt.Errorf("atsboard: %s: list roster: %w", vendor, err)
	}

	var jobs []model.NormalizedJob
	detail := make([]adapter.EmployerRunOutcome, 0, len(employers))
	successCount := 0

	for _, e := range employers {
		status, found, ferr := fetch(ctx, e)
		outcome := classifyOutcome(status, found, ferr)
		if outcome == adapter.EmployerOutcomeRead || outcome == adapter.EmployerOutcomeNoPostings {
			successCount++
		}
		if len(found) > MaxPostingsPerEmployer {
			found = found[:MaxPostingsPerEmployer]
		}
		if ferr == nil {
			jobs = append(jobs, found...)
		}
		detail = append(detail, adapter.EmployerRunOutcome{
			EmployerIdentifier: e.EmployerIdentifier,
			Outcome:            outcome,
			PostingsFound:      len(found),
		})
		if err := rosterSvc.RecordRunOutcome(ctx, e.ID, len(found)); err != nil {
			continue
		}
	}

	state.setDetail(detail)

	if len(employers) > 0 && successCount == 0 {
		return jobs, fmt.Errorf("atsboard: %s: 0 of %d employers read successfully", vendor, len(employers))
	}
	return jobs, nil
}

func classifyOutcome(status int, jobs []model.NormalizedJob, err error) adapter.EmployerOutcome {
	switch {
	case err == nil:
		if len(jobs) == 0 {
			return adapter.EmployerOutcomeNoPostings
		}
		return adapter.EmployerOutcomeRead
	case status == 404:
		return adapter.EmployerOutcomeNotFound
	case status == 401 || status == 403 || status == 429:
		return adapter.EmployerOutcomeRefused
	default:
		return adapter.EmployerOutcomeUnreadable
	}
}

func healthCheckEmployer(fetch employerFetcher) rosterport.EmployerHealthChecker {
	return func(ctx context.Context, employerIdentifier string) (int, error) {
		_, jobs, err := fetch(ctx, rosterport.EmployerBoard{EmployerIdentifier: employerIdentifier, DisplayName: employerIdentifier})
		if err != nil {
			return 0, err
		}
		return len(jobs), nil
	}
}

func vendorHealthCheck(ctx context.Context, rosterSvc rosterport.RosterPort, vendor string, fetch employerFetcher) (bool, error) {
	employers, err := rosterSvc.ListForRun(ctx, vendor)
	if err != nil {
		return false, err
	}
	if len(employers) == 0 {
		return true, nil
	}
	status, _, err := fetch(ctx, employers[0])
	if err != nil {
		return false, fmt.Errorf("atsboard: %s: health check on %s failed (status %d): %w", vendor, employers[0].EmployerIdentifier, status, err)
	}
	return true, nil
}

func NewBoardAdapters() (
	gh *GreenhouseAdapter,
	lv *LeverAdapter,
	as *AshbyAdapter,
	wk *WorkableAdapter,
	sr *SmartRecruitersAdapter,
	checkers map[string]rosterport.EmployerHealthChecker,
) {
	gh = &GreenhouseAdapter{}
	lv = &LeverAdapter{}
	as = &AshbyAdapter{}
	wk = &WorkableAdapter{}
	sr = &SmartRecruitersAdapter{}
	checkers = map[string]rosterport.EmployerHealthChecker{
		"greenhouse":      healthCheckEmployer(gh.fetchEmployer),
		"lever":           healthCheckEmployer(lv.fetchEmployer),
		"ashby":           healthCheckEmployer(as.fetchEmployer),
		"workable":        healthCheckEmployer(wk.fetchEmployer),
		"smartrecruiters": healthCheckEmployer(sr.fetchEmployer),
	}
	return gh, lv, as, wk, sr, checkers
}
