package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// Log records the start and end of every Search and HealthCheck, with how long
// it took and how many postings came back.
//
// Put it innermost, below Retry, so each attempt is logged separately — a
// source that succeeded on its third try is worth seeing as three lines, not
// one.
func Log(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next ports.JobSource) ports.JobSource {
		return &logged{Base: Base{Next: next}, log: logger}
	}
}

type logged struct {
	Base
	log *slog.Logger
}

func (l *logged) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) ([]model.NormalizedJob, error) {
	log := l.log.With("source", l.Key(), "keywords", q.Keywords)
	log.Debug("source search starting")

	start := time.Now()
	jobs, err := l.Next.Search(ctx, q, cfg)
	elapsed := time.Since(start)

	if err != nil {
		// Postings alongside an error is the partial-result case, and how many
		// survived is the first thing anyone diagnosing it wants to know.
		log.Error("source search failed", "elapsed", elapsed, "jobs", len(jobs), "error", err)
		return jobs, err
	}
	log.Info("source search finished", "elapsed", elapsed, "jobs", len(jobs))
	return jobs, nil
}

func (l *logged) HealthCheck(ctx context.Context, cfg map[string]any) (bool, error) {
	healthy, err := l.Next.HealthCheck(ctx, cfg)
	if err != nil {
		l.log.Warn("source health check failed", "source", l.Key(), "error", err)
		return healthy, err
	}
	l.log.Debug("source health check", "source", l.Key(), "healthy", healthy)
	return healthy, nil
}
