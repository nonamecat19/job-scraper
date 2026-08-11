package adapter

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/nonamecat19/jobscraper/ports"
)

// Deps is everything a source may be built from. One bag rather than a
// per-source signature is deliberate: adding a dependency to a single source
// then costs nothing anywhere else, and a Provider takes only what it needs.
//
// Every field is a port, so nothing here binds a source to a concrete
// implementation. Fields may be nil — a source whose dependency is missing
// should return an error from New rather than panicking later.
type Deps struct {
	// Retriever fetches pages through the escalating ladder. Sources reading
	// hosts that push back take this.
	Retriever ports.Retriever

	// Scraper fetches pages plainly, for cooperative hosts where the ladder
	// would be overhead.
	Scraper ports.Scraper

	// Roster lists the employer boards an ATS vendor source iterates.
	Roster ports.Roster

	// Config reads and writes per-source settings, including session cookies
	// a credentialed source persists.
	Config ports.SourceConfigStore

	// Logger is where sources report. Never nil once Deps has passed through
	// Normalize.
	Logger *slog.Logger
}

// Normalize fills in the dependencies that have a safe default, so a Provider
// can use them without a nil check. It does not invent a Retriever, Scraper,
// Roster or Config: those have no meaningful default, and a source that needs
// one should say so.
func (d Deps) Normalize() Deps {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return d
}

// Require reports a missing dependency as an error naming both the source and
// what it needed, which is the difference between a wiring mistake a caller can
// fix and a nil dereference three layers down.
//
//	if err := deps.Require("greenhouse", deps.Roster != nil, "Roster"); err != nil {
//		return nil, err
//	}
func (d Deps) Require(sourceKey string, present bool, name string) error {
	if present {
		return nil
	}
	return fmt.Errorf("adapter: source %q requires Deps.%s, which is nil", sourceKey, name)
}

// Provider is the factory for one source: its key, known before anything is
// built, and how to construct it.
//
// Keeping the key separate from the instance is what lets a caller decide which
// sources to build without building them all first — the expensive part of
// constructing a source is acquiring its dependencies, and there is no reason
// to do that for a source the caller has disabled.
type Provider interface {
	// Key is the source key this provider builds. It must equal the Key() of
	// the source New returns.
	Key() string

	// New builds the source. It errors when a required dependency is absent
	// rather than returning a source that will fail on first use.
	New(deps Deps) (ports.JobSource, error)
}

// ProviderFunc adapts a constructor function to Provider.
type ProviderFunc struct {
	SourceKey string
	Build     func(deps Deps) (ports.JobSource, error)
}

var _ Provider = ProviderFunc{}

func (p ProviderFunc) Key() string { return p.SourceKey }

func (p ProviderFunc) New(deps Deps) (ports.JobSource, error) {
	if p.Build == nil {
		return nil, fmt.Errorf("adapter: provider %q has no Build function", p.SourceKey)
	}
	return p.Build(deps)
}

// Simple builds a Provider from a constructor that cannot fail — the common
// case for a source with no required dependencies.
func Simple(key string, build func(deps Deps) ports.JobSource) Provider {
	return ProviderFunc{
		SourceKey: key,
		Build: func(deps Deps) (ports.JobSource, error) {
			return build(deps), nil
		},
	}
}

// Catalog is the set of sources a build can choose from. It is populated by the
// vendor packages themselves — each calls Register from its init — so the list
// of available sources is what the binary imported rather than a switch
// somebody has to remember to extend.
//
// Import github.com/nonamecat19/jobscraper/adapters/all to get every bundled
// source, or import only the vendor packages you want.
type Catalog struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string
}

// DefaultCatalog is where the bundled vendor packages register themselves.
var DefaultCatalog = NewCatalog()

func NewCatalog() *Catalog {
	return &Catalog{providers: map[string]Provider{}}
}

// Register adds p to the catalog. It panics on a duplicate key, because the
// only way to reach it is two packages claiming the same source at init time —
// a build-time mistake with no sensible runtime recovery.
func (c *Catalog) Register(p Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := p.Key()
	if _, exists := c.providers[key]; exists {
		panic(DuplicateKeyError{Key: key})
	}
	c.providers[key] = p
	c.order = append(c.order, key)
}

// Register adds p to DefaultCatalog. Vendor packages call it from init.
func Register(p Provider) { DefaultCatalog.Register(p) }

// Get returns the provider for key.
func (c *Catalog) Get(key string) (Provider, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.providers[key]
	if !ok {
		return nil, NotRegisteredError{Key: key}
	}
	return p, nil
}

// Keys returns every registered key, sorted, so a caller listing what is
// available gets a stable answer regardless of import order.
func (c *Catalog) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.order))
	copy(out, c.order)
	sort.Strings(out)
	return out
}

// Providers returns every registered provider, in key order.
func (c *Catalog) Providers() []Provider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, len(c.order))
	copy(keys, c.order)
	sort.Strings(keys)

	out := make([]Provider, 0, len(keys))
	for _, k := range keys {
		out = append(out, c.providers[k])
	}
	return out
}

// Build constructs the named sources from deps. An empty keys slice builds
// everything in the catalog.
//
// A source whose dependencies are missing is skipped rather than failing the
// whole build, and the reason is logged: a consumer that configured no
// FlareSolverr and no roster should still get the sources that do not need
// them. The returned error covers only failures with no such interpretation.
func (c *Catalog) Build(deps Deps, keys ...string) ([]ports.JobSource, error) {
	deps = deps.Normalize()

	providers := c.Providers()
	if len(keys) > 0 {
		providers = providers[:0]
		for _, k := range keys {
			p, err := c.Get(k)
			if err != nil {
				return nil, err
			}
			providers = append(providers, p)
		}
	}

	out := make([]ports.JobSource, 0, len(providers))
	for _, p := range providers {
		src, err := p.New(deps)
		if err != nil {
			deps.Logger.Warn("adapter: skipping source, cannot build it",
				"source", p.Key(), "error", err)
			continue
		}
		if src == nil {
			continue
		}
		out = append(out, src)
	}
	return out, nil
}
