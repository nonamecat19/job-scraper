// Package jobscraper reads job postings from many sites behind one interface.
//
// # Getting started
//
// The whole library is reachable through Client:
//
//	client, err := jobscraper.New()
//	if err != nil {
//		return err
//	}
//	defer client.Close()
//
//	jobs, err := client.Search(ctx, model.SearchQuery{Keywords: "golang"})
//
// That runs every source that works without configuration. Sources needing
// something they were not given — an employer roster, an API key, a session —
// are skipped with a logged reason rather than failing the run, so a partly
// configured client is a working client.
//
// # How the pieces fit
//
// The library is layered, and each layer depends only on the interfaces in
// ports, never on the layer below it:
//
//	jobscraper      Client — the facade. Wires everything, runs the fan-out.
//	  ports         Every interface, and the values they exchange. Depends on
//	                nothing but model.
//	  adapter       Registry, Provider/Catalog factories, capability helpers.
//	    middleware  Decorators: recover, timeout, retry, log, observe.
//	  adapters/*    One package per site. Each is a ports.JobSource.
//	    atsboard    Template shared by the five ATS vendors.
//	  retrieval     The escalating fetch ladder: Rung strategies in a chain.
//	  session       Logged-in sessions, one implementation for every site.
//	  store/*       In-memory implementations of the persistence ports.
//	  internal/*    Helpers with no public contract.
//
// The patterns are load-bearing rather than decorative. Sources are built by
// Providers held in a Catalog they register themselves with, so the available
// set is decided by what the binary imported. Every source is wrapped in a
// chain of decorators, so retries and timeouts exist in one place instead of
// twenty-two. Retrieval escalates through a chain of Rung strategies, so adding
// a proxy pool means implementing one interface. And the five ATS vendors share
// a single template, so Greenhouse and Lever differ by a URL and a struct tag.
//
// # Replacing pieces
//
// Everything the library talks to is a port. Supply your own database-backed
// ports.StateStore and the retrieval ladder remembers hosts across restarts;
// supply your own ports.Roster and the ATS sources read your employer list.
// Add a source of your own with WithSource, or a decorator of your own with
// WithMiddleware, without touching this package.
package jobscraper

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/adapter/middleware"
	_ "github.com/nonamecat19/jobscraper/adapters/all" // registers every bundled source
	"github.com/nonamecat19/jobscraper/internal/scraping"
	"github.com/nonamecat19/jobscraper/ports"
	"github.com/nonamecat19/jobscraper/retrieval"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

// Client is the facade over the whole library: it owns the configured sources,
// the retrieval ladder, and the fan-out that runs a search across all of them.
//
// A Client is safe for concurrent use. Close it when done — it may own a
// browser process and pooled connections.
type Client struct {
	registry    *adapter.Registry
	configStore ports.SourceConfigStore
	log         *slog.Logger

	concurrency int

	// owned resources are closed by Close; ones the caller supplied are not,
	// because the caller may still be using them.
	ownedRetriever ports.Retriever
	ownedScraper   ports.Scraper

	closeOnce sync.Once
}

// New builds a client. With no options it uses in-memory state, no roster and
// no credentials, which is enough to read every source that needs none.
//
// It errors only on wiring mistakes the caller can fix — an unknown source key,
// a duplicate key. A source that cannot be built from what it was given is
// skipped with a warning, not an error.
func New(opts ...Option) (*Client, error) {
	cfg := newConfig(opts)

	deps, err := buildDeps(cfg)
	if err != nil {
		return nil, err
	}

	sources, err := cfg.catalog.Build(deps, cfg.sources...)
	if err != nil {
		return nil, err
	}
	sources = append(sources, cfg.extra...)

	registry, err := adapter.NewRegistry(sources...)
	if err != nil {
		return nil, err
	}
	registry.Decorate(buildMiddleware(cfg))

	c := &Client{
		registry:    registry,
		configStore: deps.Config,
		log:         cfg.logger,
		concurrency: cfg.concurrency,
	}
	if cfg.ownsRetriever {
		c.ownedRetriever = deps.Retriever
	}
	if cfg.ownsScraper {
		c.ownedScraper = deps.Scraper
	}
	return c, nil
}

// buildDeps assembles the ports every source is constructed from, filling in
// the ones the caller did not supply.
func buildDeps(cfg *config) (adapter.Deps, error) {
	deps := cfg.deps
	deps.Logger = cfg.logger

	if deps.Scraper == nil {
		deps.Scraper = scraping.New()
		cfg.ownsScraper = true
	}

	deps.Retriever = cfg.retriever
	if deps.Retriever == nil {
		store := cfg.stateStore
		if store == nil {
			store = memstore.NewStateStore()
		}
		deps.Retriever = retrieval.NewEngine(store,
			retrieval.WithBrowser(cfg.browserEnabled),
			retrieval.WithFlareSolverr(cfg.flareSolverURL),
			retrieval.WithLogger(cfg.logger),
		)
		cfg.ownsRetriever = true
	}

	return deps.Normalize(), nil
}

// buildMiddleware assembles the decorator chain wrapped around every source.
//
// The order is deliberate. Recover is outermost so a panic anywhere below it
// becomes an ordinary error. Timeout sits above Retry so the budget covers the
// retries rather than resetting for each. Log is innermost so it records what
// actually happened on the wire, one line per attempt. Caller middleware goes
// outside the built-ins, where it sees the finished result.
func buildMiddleware(cfg *config) middleware.Middleware {
	chain := make([]middleware.Middleware, 0, len(cfg.middleware)+5)
	chain = append(chain, cfg.middleware...)
	chain = append(chain, middleware.Recover(cfg.logger))

	// An empty Observers slice is a non-nil Observer, so the wrapper has to be
	// skipped explicitly rather than left to Observe's nil check.
	if len(cfg.observers) > 0 {
		chain = append(chain, middleware.Observe(cfg.observers))
	}

	chain = append(chain,
		middleware.Timeout(cfg.sourceTimeout),
		middleware.Retry(cfg.retry),
		middleware.Log(cfg.logger),
	)
	return middleware.Chain(chain...)
}

// Sources returns the configured source keys, in registration order.
func (c *Client) Sources() []string { return c.registry.Keys() }

// Registry exposes the underlying registry, for callers that need to reach a
// single source or inspect its capabilities directly.
func (c *Client) Registry() *adapter.Registry { return c.registry }

// Close releases everything the client owns — the browser, the retrieval
// ladder, any source holding a connection. Ports the caller supplied are left
// alone, since the caller may still be using them.
//
// Close is idempotent.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		errs := []error{c.registry.Close(context.Background())}

		if closer, ok := c.ownedRetriever.(interface{ Close() error }); ok {
			errs = append(errs, closer.Close())
		}
		if c.ownedScraper != nil {
			c.ownedScraper.Close()
		}
		err = errors.Join(errs...)
	})
	return err
}
