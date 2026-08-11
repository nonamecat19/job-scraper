package jobscraper

import (
	"context"
	"errors"
	"sync"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// SourceResult is one source's contribution to a run.
type SourceResult struct {
	SourceKey string
	Jobs      []model.NormalizedJob
	Err       error
}

// Result is the outcome of a whole run: every posting found, plus the
// per-source detail needed to tell "nothing matched" apart from "half the
// sources were blocked".
type Result struct {
	Jobs    []model.NormalizedJob
	Sources []SourceResult
}

// Failed returns the sources that errored.
func (r Result) Failed() []SourceResult {
	var out []SourceResult
	for _, s := range r.Sources {
		if s.Err != nil {
			out = append(out, s)
		}
	}
	return out
}

// Err joins every source error, or nil when all succeeded. Use it when you want
// one error; read Sources when you want to know which source produced what.
func (r Result) Err() error {
	var errs []error
	for _, s := range r.Sources {
		if s.Err != nil {
			errs = append(errs, s.Err)
		}
	}
	return errors.Join(errs...)
}

// Verdict summarises the run the way an operator reads it: everything worked,
// some sources were lost, or nothing came back at all.
func (r Result) Verdict() ports.RunVerdict {
	failed := len(r.Failed())
	switch {
	case failed == 0:
		return ports.VerdictSuccess
	case failed < len(r.Sources):
		return ports.VerdictPartial
	default:
		return ports.VerdictBlocked
	}
}

// Search runs the query across every configured source and returns the postings
// they found.
//
// Sources run concurrently, bounded by the configured concurrency. A source
// that fails does not stop the others: its error is recorded against it and the
// run continues, because twenty working sources are worth more than one clean
// error. The returned error is non-nil only when every source failed — inspect
// Result.Sources for the per-source picture in every other case.
//
// query.Sources, when set, narrows the run to those keys.
func (c *Client) Search(ctx context.Context, query model.SearchQuery) (Result, error) {
	sources := c.registry.Select(query.Sources...)
	if len(sources) == 0 {
		return Result{}, nil
	}

	results := make([]SourceResult, len(sources))

	var wg sync.WaitGroup
	sem := c.semaphore()

	for i, src := range sources {
		wg.Add(1)
		go func(i int, src ports.JobSource) {
			defer wg.Done()

			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[i] = SourceResult{SourceKey: src.Key(), Err: ctx.Err()}
					return
				}
			}

			jobs, err := src.Search(ctx, query, c.configFor(ctx, src.Key()))
			results[i] = SourceResult{SourceKey: src.Key(), Jobs: jobs, Err: err}
		}(i, src)
	}
	wg.Wait()

	out := Result{Sources: results}
	for _, r := range results {
		out.Jobs = append(out.Jobs, r.Jobs...)
	}

	if out.Verdict() == ports.VerdictBlocked {
		return out, out.Err()
	}
	return out, nil
}

// HealthCheck probes every configured source and reports which are reachable.
// A source that errors is reported as unhealthy rather than aborting the sweep.
func (c *Client) HealthCheck(ctx context.Context) map[string]error {
	sources := c.registry.All()
	out := make(map[string]error, len(sources))

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := c.semaphore()

	for _, src := range sources {
		wg.Add(1)
		go func(src ports.JobSource) {
			defer wg.Done()

			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			healthy, err := src.HealthCheck(ctx, c.configFor(ctx, src.Key()))
			if err == nil && !healthy {
				err = errors.New("source reported itself unhealthy")
			}

			mu.Lock()
			out[src.Key()] = err
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	return out
}

// ReadPosting reads a single posting page into a job, using whichever source
// claims the URL.
//
// It returns an adapter.NoPostingReaderError when no source recognises the
// URL — the caller's cue to fall back to manual entry. A source that claims the
// host but rejects the URL's shape, such as a search page on a supported site,
// produces the same error: the site is supported, this page is just not a
// posting.
func (c *Client) ReadPosting(ctx context.Context, rawURL string) (model.NormalizedJob, error) {
	src, reader, err := c.registry.PostingReaderFor(rawURL)
	if err != nil {
		return model.NormalizedJob{}, err
	}
	return reader.ReadPosting(ctx, rawURL, c.configFor(ctx, src.Key()))
}

// EmployerReport returns the per-employer detail from the last run of an ATS
// vendor source. It reports false for a source that does not iterate employer
// boards.
func (c *Client) EmployerReport(sourceKey string) ([]ports.EmployerRunOutcome, bool) {
	src, err := c.registry.Get(sourceKey)
	if err != nil {
		return nil, false
	}
	reporter, ok := adapter.AsEmployerReporter(src)
	if !ok {
		return nil, false
	}
	return reporter.LastRunDetail(), true
}

// configFor loads a source's stored settings. A missing or failing config store
// yields a nil map, which every source must accept: running a source with no
// configuration is a supported state, not an error.
func (c *Client) configFor(ctx context.Context, sourceKey string) map[string]any {
	if c.configStore == nil {
		return nil
	}
	cfg, err := c.configStore.Config(ctx, sourceKey)
	if err != nil {
		c.log.Warn("jobscraper: could not load source config, continuing without it",
			"source", sourceKey, "error", err)
		return nil
	}
	return cfg
}

// semaphore returns the concurrency limiter, or nil when unlimited.
func (c *Client) semaphore() chan struct{} {
	if c.concurrency < 1 {
		return nil
	}
	return make(chan struct{}, c.concurrency)
}
