package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// PanicError wraps a panic that escaped a source, so a caller sees an ordinary
// error and the stack that produced it.
type PanicError struct {
	SourceKey string
	Value     any
	Stack     []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("source %q panicked: %v", e.SourceKey, e.Value)
}

// Recover turns a panic inside a source into an error.
//
// Sources parse HTML from sites that change without notice, so a nil map entry
// or a short slice is a routine hazard rather than an exotic one. One such
// source must not take down a run across twenty others, so this belongs
// outermost in the chain, where it contains everything below it.
func Recover(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next ports.JobSource) ports.JobSource {
		return &recovered{Base: Base{Next: next}, log: logger}
	}
}

type recovered struct {
	Base
	log *slog.Logger
}

func (r *recovered) Search(ctx context.Context, q model.SearchQuery, cfg map[string]any) (jobs []model.NormalizedJob, err error) {
	defer func() {
		if v := recover(); v != nil {
			stack := debug.Stack()
			r.log.Error("source panicked during search",
				"source", r.Key(), "panic", v, "stack", string(stack))
			err = &PanicError{SourceKey: r.Key(), Value: v, Stack: stack}
			// Whatever the source had appended before it panicked is left in
			// jobs: partial results are the contract, and a panic does not
			// make the postings already parsed any less valid.
		}
	}()
	return r.Next.Search(ctx, q, cfg)
}

func (r *recovered) HealthCheck(ctx context.Context, cfg map[string]any) (healthy bool, err error) {
	defer func() {
		if v := recover(); v != nil {
			stack := debug.Stack()
			r.log.Error("source panicked during health check",
				"source", r.Key(), "panic", v, "stack", string(stack))
			healthy = false
			err = &PanicError{SourceKey: r.Key(), Value: v, Stack: stack}
		}
	}()
	return r.Next.HealthCheck(ctx, cfg)
}
