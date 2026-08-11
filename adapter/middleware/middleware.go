// Package middleware decorates job sources with cross-cutting behaviour —
// logging, retries, timeouts, panic containment, observation — without any of
// it leaking into the sources themselves.
//
// Every decorator here is a ports.JobSource wrapping a ports.JobSource, so a
// caller composes them freely and the sources stay ignorant of the whole
// arrangement. Chain applies them outermost-first:
//
//	wrapped := middleware.Chain(
//		middleware.Recover(logger),   // outermost: contains panics from all below
//		middleware.Timeout(2*time.Minute),
//		middleware.Retry(middleware.RetryConfig{Attempts: 3}),
//		middleware.Log(logger),       // innermost: sees each individual attempt
//	)(src)
//
// The order above is the one to copy. Recover outermost means nothing escapes;
// Timeout above Retry bounds the whole operation rather than each attempt; Log
// innermost means the log records what actually happened on the wire rather
// than the retry loop's summary.
//
// # Transparency
//
// Sources carry optional capabilities — ports.PostingReader,
// ports.DetailNeeder — and a naive wrapper would hide them behind a type that
// implements only ports.JobSource. Every decorator here therefore embeds Base,
// which provides Unwrap, and callers ask about capabilities through
// adapter.As rather than a direct type assertion.
package middleware

import (
	"context"

	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// Middleware wraps a source in additional behaviour.
type Middleware func(ports.JobSource) ports.JobSource

// Chain composes middlewares into one. The first argument ends up outermost, so
// the list reads in the order a call travels through it.
//
// Chaining nothing returns the source unchanged, which makes Chain safe to call
// with a slice that may be empty.
func Chain(mws ...Middleware) Middleware {
	return func(src ports.JobSource) ports.JobSource {
		for i := len(mws) - 1; i >= 0; i-- {
			if mws[i] != nil {
				src = mws[i](src)
			}
		}
		return src
	}
}

// Base is embedded by every decorator. It forwards the ports.JobSource methods
// to the wrapped source and provides Unwrap, which is what lets adapter.As see
// the capabilities of what is underneath.
//
// A decorator overrides only the methods it actually changes.
type Base struct {
	Next ports.JobSource
}

var _ ports.JobSource = Base{}

func (b Base) Key() string { return b.Next.Key() }

func (b Base) Kind() model.SourceKind { return b.Next.Kind() }

func (b Base) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) ([]model.NormalizedJob, error) {
	return b.Next.Search(ctx, q, cfg)
}

func (b Base) HealthCheck(ctx context.Context, cfg map[string]any) (bool, error) {
	return b.Next.HealthCheck(ctx, cfg)
}

// Unwrap returns the wrapped source.
func (b Base) Unwrap() ports.JobSource { return b.Next }
