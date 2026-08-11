package adapter

import (
	"context"

	"github.com/nonamecat19/job-scraper/ports"
)

// Registry holds the configured sources and preserves the order they were
// registered in. Order is not cosmetic: MatchesPostingURL is consulted in
// registry order and the first source to claim a URL wins, so registering a
// specialised source before a broad one is how a caller resolves overlap.
//
// A Registry is not safe for concurrent registration. Build it during startup,
// then read it from as many goroutines as you like.
type Registry struct {
	byKey map[string]ports.JobSource
	order []string
}

// NewRegistry builds a registry from sources in priority order. It errors on a
// duplicate key rather than letting one source silently shadow another.
func NewRegistry(sources ...ports.JobSource) (*Registry, error) {
	r := &Registry{byKey: make(map[string]ports.JobSource, len(sources))}
	for _, s := range sources {
		if err := r.Register(s); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// MustRegistry is NewRegistry for wiring that is known good at compile time —
// a fixed list in a test or an example. It panics on a duplicate key.
func MustRegistry(sources ...ports.JobSource) *Registry {
	r, err := NewRegistry(sources...)
	if err != nil {
		panic(err)
	}
	return r
}

// Register appends a source. A nil source is ignored, so a caller may pass the
// result of a constructor that returns nil when its dependency is absent.
func (r *Registry) Register(s ports.JobSource) error {
	if s == nil {
		return nil
	}
	if r.byKey == nil {
		r.byKey = map[string]ports.JobSource{}
	}
	key := s.Key()
	if _, exists := r.byKey[key]; exists {
		return DuplicateKeyError{Key: key}
	}
	r.byKey[key] = s
	r.order = append(r.order, key)
	return nil
}

// Get returns the source registered under key.
func (r *Registry) Get(key string) (ports.JobSource, error) {
	s, ok := r.byKey[key]
	if !ok {
		return nil, NotRegisteredError{Key: key}
	}
	return s, nil
}

// Has reports whether key is registered, without allocating an error for the
// common "is this configured?" check.
func (r *Registry) Has(key string) bool {
	_, ok := r.byKey[key]
	return ok
}

// All returns every source in registration order.
func (r *Registry) All() []ports.JobSource {
	out := make([]ports.JobSource, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKey[k])
	}
	return out
}

// Keys returns every registered key in registration order.
func (r *Registry) Keys() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len reports how many sources are registered.
func (r *Registry) Len() int { return len(r.order) }

// Select returns the sources named by keys, in the order given, skipping any
// key that is not registered. An empty keys slice selects everything — the
// convention a search query uses to mean "all sources".
func (r *Registry) Select(keys ...string) []ports.JobSource {
	if len(keys) == 0 {
		return r.All()
	}
	out := make([]ports.JobSource, 0, len(keys))
	for _, k := range keys {
		if s, ok := r.byKey[k]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Filter returns the sources satisfying keep, in registration order. Combine it
// with the capability helpers to answer questions like "which sources need a
// detail pass?".
func (r *Registry) Filter(keep func(ports.JobSource) bool) []ports.JobSource {
	var out []ports.JobSource
	for _, s := range r.All() {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// PostingReaderFor returns the first source that claims rawURL as one of its
// posting pages, consulted in registration order.
//
// A source that claims the host but rejects the URL's shape — a search page on
// a site the library otherwise reads — does not match, and the caller reports
// that the URL is not a posting rather than that the site is unsupported.
func (r *Registry) PostingReaderFor(rawURL string) (ports.JobSource, ports.PostingReader, error) {
	for _, s := range r.All() {
		pr, ok := AsPostingReader(s)
		if !ok {
			continue
		}
		if pr.MatchesPostingURL(rawURL) {
			return s, pr, nil
		}
	}
	return nil, nil, NoPostingReaderError{URL: rawURL}
}

// JobDetailReaderFor returns the first source that claims rawURL and can read
// it into a full JobDetail, consulted in registration order.
//
// A source claiming the URL with only a PostingReader is not a match: the
// caller falls back to projecting a JobDetail from the posting, which is the
// client's job rather than the registry's.
func (r *Registry) JobDetailReaderFor(rawURL string) (ports.JobSource, ports.JobDetailReader, error) {
	for _, s := range r.All() {
		dr, ok := AsJobDetailReader(s)
		if !ok {
			continue
		}
		if dr.MatchesPostingURL(rawURL) {
			return s, dr, nil
		}
	}
	return nil, nil, NoPostingReaderError{URL: rawURL}
}

// Decorate wraps every registered source with mw, in place. Call it once after
// registration and before the registry is shared.
func (r *Registry) Decorate(mw func(ports.JobSource) ports.JobSource) {
	if mw == nil {
		return
	}
	for _, k := range r.order {
		r.byKey[k] = mw(r.byKey[k])
	}
}

// Close releases every source that holds resources, returning the first error
// while still closing the rest. Sources without a Close are skipped.
func (r *Registry) Close(_ context.Context) error {
	var firstErr error
	for _, s := range r.All() {
		c, ok := As[ports.Closer](s)
		if !ok {
			continue
		}
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
