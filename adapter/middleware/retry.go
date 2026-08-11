package middleware

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// RetryConfig tunes the retry decorator. The zero value is usable: it retries
// twice with a 1s base delay, doubling, capped at 30s.
type RetryConfig struct {
	// Attempts is the total number of tries, not the number of retries. Values
	// below 1 are treated as 1 — no retrying.
	Attempts int

	// BaseDelay is how long to wait before the second attempt. Each subsequent
	// wait doubles it.
	BaseDelay time.Duration

	// MaxDelay caps the backoff.
	MaxDelay time.Duration

	// Jitter spreads retries out as a fraction of the delay, so twenty sources
	// failing at once do not all come back at the same instant. 0.2 means each
	// wait lands within ±20% of nominal. Zero disables it.
	Jitter float64

	// Retryable decides whether an error is worth another try. The default
	// retries everything except a cancelled context — a source that failed
	// because its credentials are wrong will keep failing, but the library
	// cannot tell that apart from a transient fault without help, so a
	// consumer that can tell should supply this.
	Retryable func(error) bool
}

const (
	defaultAttempts  = 2
	defaultBaseDelay = time.Second
	defaultMaxDelay  = 30 * time.Second
)

func (c RetryConfig) withDefaults() RetryConfig {
	if c.Attempts < 1 {
		c.Attempts = defaultAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = defaultBaseDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = defaultMaxDelay
	}
	if c.Retryable == nil {
		c.Retryable = DefaultRetryable
	}
	return c
}

// DefaultRetryable retries anything that is not a cancellation. A cancelled or
// expired context means the caller has stopped waiting, and retrying then is
// work nobody will read.
func DefaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// Retry re-runs a failed Search with exponential backoff.
//
// It only retries a total failure. A Search that returned postings alongside an
// error read something, and running it again would duplicate that work and
// those postings — partial results are kept and the error is returned with
// them.
//
// HealthCheck is never retried: it exists to report the current state quickly,
// and a check that retries for half a minute reports a state that has passed.
func Retry(cfg RetryConfig) Middleware {
	cfg = cfg.withDefaults()
	return func(next ports.JobSource) ports.JobSource {
		return &retrying{Base: Base{Next: next}, cfg: cfg}
	}
}

type retrying struct {
	Base
	cfg RetryConfig
}

func (r *retrying) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) ([]model.NormalizedJob, error) {
	var lastErr error

	for attempt := 1; attempt <= r.cfg.Attempts; attempt++ {
		jobs, err := r.Next.Search(ctx, q, cfg)
		if err == nil || len(jobs) > 0 {
			return jobs, err
		}
		lastErr = err

		if attempt == r.cfg.Attempts || !r.cfg.Retryable(err) {
			break
		}
		if err := sleepCtx(ctx, r.backoff(attempt)); err != nil {
			return nil, errors.Join(lastErr, err)
		}
	}
	return nil, lastErr
}

// backoff returns the wait before the attempt after this one: BaseDelay
// doubled once per elapsed attempt, capped, then jittered.
func (r *retrying) backoff(attempt int) time.Duration {
	delay := r.cfg.BaseDelay << (attempt - 1)
	if delay > r.cfg.MaxDelay || delay <= 0 {
		delay = r.cfg.MaxDelay
	}
	if r.cfg.Jitter > 0 {
		spread := float64(delay) * r.cfg.Jitter
		delay = time.Duration(float64(delay) - spread + 2*spread*rand.Float64())
	}
	return delay
}

// sleepCtx waits for d, or returns early with the context's error if the caller
// gives up first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
