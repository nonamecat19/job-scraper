package middleware

import (
	"context"
	"time"

	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// Observe notifies obs when a source starts and finishes a search.
//
// It is the hook for anything that wants to watch a run without being part of
// it — a progress bar, a metrics exporter, a per-source SLA tracker. Notifying
// happens on the source's own goroutine, so an observer that blocks slows the
// run; keep implementations quick or hand off to a channel.
//
// A nil observer returns a no-op middleware.
func Observe(obs ports.Observer) Middleware {
	if obs == nil {
		return func(next ports.JobSource) ports.JobSource { return next }
	}
	return func(next ports.JobSource) ports.JobSource {
		return &observed{Base: Base{Next: next}, obs: obs}
	}
}

type observed struct {
	Base
	obs ports.Observer
}

func (o *observed) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) ([]model.NormalizedJob, error) {
	o.obs.OnSourceStart(ports.RunEvent{SourceKey: o.Key(), Query: q})

	start := time.Now()
	jobs, err := o.Next.Search(ctx, q, cfg)

	o.obs.OnSourceFinish(ports.RunEvent{
		SourceKey: o.Key(),
		Query:     q,
		Jobs:      len(jobs),
		Duration:  time.Since(start),
		Err:       err,
	})
	return jobs, err
}
