package middleware

import (
	"context"
	"time"

	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// Timeout bounds how long a source may run.
//
// Without it one unresponsive host stalls a whole run: sources are read
// concurrently, but the run is not done until the slowest one is. A
// non-positive duration returns a no-op middleware, so a caller can pass a
// configured value straight through without checking it.
//
// Place it above Retry to bound the whole operation including its retries, or
// below Retry to bound each attempt separately. Above is usually what you want.
func Timeout(d time.Duration) Middleware {
	if d <= 0 {
		return func(next ports.JobSource) ports.JobSource { return next }
	}
	return func(next ports.JobSource) ports.JobSource {
		return &timedOut{Base: Base{Next: next}, budget: d}
	}
}

type timedOut struct {
	Base
	budget time.Duration
}

func (t *timedOut) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) ([]model.NormalizedJob, error) {
	ctx, cancel := context.WithTimeout(ctx, t.budget)
	defer cancel()
	return t.Next.Search(ctx, q, cfg)
}

func (t *timedOut) HealthCheck(ctx context.Context, cfg map[string]any) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, t.budget)
	defer cancel()
	return t.Next.HealthCheck(ctx, cfg)
}
