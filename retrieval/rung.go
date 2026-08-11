package retrieval

import (
	"context"

	"github.com/nonamecat19/jobscraper/ports"
)

// Rung keys. A key is stored in HostState.CurrentRung, so renaming one
// invalidates every learned preference in the consumer's database.
const (
	KeyDirect       = "direct"
	KeyBrowser      = "browser"
	KeyFlareSolverr = "flaresolverr"
)

// Rung is one strategy for fetching a page. The bundled rungs run from cheapest
// to costliest — a TLS-fingerprinted client, a real browser, a FlareSolverr
// sidecar — and a consumer can add its own (a residential proxy pool, a paid
// unblocking API) by implementing this interface and passing WithRung.
//
// Fetch reports failure through the returned PageOutcome rather than an error:
// a challenge is an observation, not a fault, and the engine needs the body and
// the reason to decide whether escalating is worthwhile. The body is returned
// even for a non-read outcome, because a challenge page is what an operator
// needs to see when diagnosing a block.
type Rung interface {
	// Key identifies the rung. It must be stable across releases and unique
	// within a Ladder.
	Key() string

	// Fetch attempts the page and classifies the result.
	Fetch(ctx context.Context, req ports.FetchRequest) (ports.PageOutcome, string)

	// Available reports whether the rung can run right now — the browser
	// launched, the sidecar answered its health check. The engine skips
	// unavailable rungs instead of escalating into a guaranteed failure.
	Available(ctx context.Context) bool

	// Close releases whatever the rung holds. It must be safe to call on a
	// rung that never fetched anything, and safe to call twice.
	Close() error
}

// Ladder is the ordered chain of rungs the engine walks. Position in the slice
// is cost: index 0 is tried first, and escalation moves one step to the right.
//
// A Ladder is immutable once built, which is what makes it safe to share
// between the engine's goroutines without a lock.
type Ladder struct {
	rungs []Rung
}

// NewLadder builds a ladder from rungs in ascending cost order. Nil entries are
// dropped, so a caller can pass the result of a constructor that returns nil
// when its dependency is absent.
func NewLadder(rungs ...Rung) *Ladder {
	l := &Ladder{rungs: make([]Rung, 0, len(rungs))}
	for _, r := range rungs {
		if r != nil {
			l.rungs = append(l.rungs, r)
		}
	}
	return l
}

// Len reports how many rungs the ladder holds.
func (l *Ladder) Len() int { return len(l.rungs) }

// Rungs returns the rungs in cost order. The returned slice is a copy.
func (l *Ladder) Rungs() []Rung {
	out := make([]Rung, len(l.rungs))
	copy(out, l.rungs)
	return out
}

// First returns the cheapest rung, or nil for an empty ladder.
func (l *Ladder) First() Rung {
	if len(l.rungs) == 0 {
		return nil
	}
	return l.rungs[0]
}

// Find returns the rung with the given key. A key the ladder does not hold —
// a stored preference for a rung the process no longer configures — returns
// (nil, false), and the caller falls back to First.
func (l *Ladder) Find(key string) (Rung, bool) {
	for _, r := range l.rungs {
		if r.Key() == key {
			return r, true
		}
	}
	return nil, false
}

// Next returns the rung one step costlier than key. It reports false at the top
// of the ladder, which is the engine's signal that escalation is exhausted.
func (l *Ladder) Next(key string) (Rung, bool) {
	for i, r := range l.rungs {
		if r.Key() == key && i+1 < len(l.rungs) {
			return l.rungs[i+1], true
		}
	}
	return nil, false
}

// IsCheapest reports whether key is the ladder's first rung. The engine uses it
// to decide whether a stored preference is worth periodically re-testing:
// there is nothing cheaper to fall back to from the bottom.
func (l *Ladder) IsCheapest(key string) bool {
	return len(l.rungs) > 0 && l.rungs[0].Key() == key
}

// Close closes every rung, returning the first error while still closing the
// rest.
func (l *Ladder) Close() error {
	var firstErr error
	for _, r := range l.rungs {
		if err := r.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
