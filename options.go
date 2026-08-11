package jobscraper

import (
	"log/slog"
	"time"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/adapter/middleware"
	"github.com/nonamecat19/job-scraper/ports"
)

// Option configures a Client. Options are applied in order, so a later one
// overrides an earlier one, and every option has a working default:
// jobscraper.New() with no options at all produces a usable client.
type Option func(*config)

type config struct {
	catalog *adapter.Catalog
	deps    adapter.Deps
	sources []string
	extra   []ports.JobSource

	stateStore ports.StateStore
	retriever  ports.Retriever

	observers  ports.Observers
	middleware []middleware.Middleware

	logger         *slog.Logger
	sourceTimeout  time.Duration
	retry          middleware.RetryConfig
	concurrency    int
	browserEnabled bool
	flareSolverURL string
	ownsRetriever  bool
	ownsScraper    bool
}

const (
	defaultSourceTimeout = 3 * time.Minute
	defaultConcurrency   = 6
)

func newConfig(opts []Option) *config {
	cfg := &config{
		catalog:       adapter.DefaultCatalog,
		logger:        slog.Default(),
		sourceTimeout: defaultSourceTimeout,
		concurrency:   defaultConcurrency,
		retry:         middleware.RetryConfig{Attempts: 2},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithSources restricts the client to the named sources. The default is every
// source registered in the catalog — which, since the root package imports
// adapters/all, means every source the library ships.
//
// Naming a source that is not in the catalog is an error from New, because it
// is almost always a typo rather than an intent to run with fewer sources.
func WithSources(keys ...string) Option {
	return func(c *config) { c.sources = keys }
}

// WithCatalog replaces the catalog sources are built from. Use it to run
// against a catalog assembled by hand rather than by import side effects —
// mostly useful in tests.
func WithCatalog(cat *adapter.Catalog) Option {
	return func(c *config) {
		if cat != nil {
			c.catalog = cat
		}
	}
}

// WithSource adds an already-built source, bypassing the catalog. This is how a
// consumer plugs in a job source of its own without registering it globally.
func WithSource(src ports.JobSource) Option {
	return func(c *config) {
		if src != nil {
			c.extra = append(c.extra, src)
		}
	}
}

// WithStateStore supplies the per-host memory the retriever keeps: which
// strategy worked, what cookies it holds, how badly a host has been blocking.
// The default is an in-memory store, which is fine for a one-shot run and wrong
// for a long-lived service — there, pass a store backed by your database so the
// knowledge survives restarts.
func WithStateStore(s ports.StateStore) Option {
	return func(c *config) { c.stateStore = s }
}

// WithRoster supplies the employer boards the ATS sources iterate. Without one,
// those sources are skipped: they have nothing to crawl.
func WithRoster(r ports.Roster) Option {
	return func(c *config) { c.deps.Roster = r }
}

// WithConfigStore supplies per-source settings — API keys, credentials — and
// gives credentialed sources somewhere to persist a session cookie.
func WithConfigStore(s ports.SourceConfigStore) Option {
	return func(c *config) { c.deps.Config = s }
}

// WithRetriever replaces the page-fetching ladder outright. The client then
// does not own it and will not close it.
func WithRetriever(r ports.Retriever) Option {
	return func(c *config) { c.retriever = r }
}

// WithScraper replaces the plain page fetcher. The client then does not own it
// and will not close it.
func WithScraper(s ports.Scraper) Option {
	return func(c *config) { c.deps.Scraper = s }
}

// WithBrowser adds a headless browser to the retrieval ladder, for hosts that
// only serve their listings after JavaScript has run. It costs a Chrome process
// per client, so it is off by default. Ignored when WithRetriever is used.
func WithBrowser(enabled bool) Option {
	return func(c *config) { c.browserEnabled = enabled }
}

// WithFlareSolverr adds a FlareSolverr sidecar as the ladder's last resort.
// Ignored when WithRetriever is used.
func WithFlareSolverr(baseURL string) Option {
	return func(c *config) { c.flareSolverURL = baseURL }
}

// WithLogger sets where the client and its sources report.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithObserver adds a watcher notified as each source starts and finishes.
// Several observers may be added; they are called in the order given.
func WithObserver(obs ports.Observer) Option {
	return func(c *config) {
		if obs != nil {
			c.observers = append(c.observers, obs)
		}
	}
}

// WithSourceTimeout bounds how long any one source may take, retries included.
// Sources run concurrently but a run is not finished until its slowest source
// is, so this is what stops one unresponsive host from holding up everything.
// Zero or negative removes the bound.
func WithSourceTimeout(d time.Duration) Option {
	return func(c *config) { c.sourceTimeout = d }
}

// WithRetry configures how a failed source is retried. Only total failures are
// retried: a source that returned some postings alongside an error read
// something, and running it again would duplicate that work.
func WithRetry(cfg middleware.RetryConfig) Option {
	return func(c *config) { c.retry = cfg }
}

// WithConcurrency caps how many sources are read at once. The default is 6.
// Values below 1 mean unlimited, which is rarely what you want against a
// couple of dozen sources.
func WithConcurrency(n int) Option {
	return func(c *config) { c.concurrency = n }
}

// WithMiddleware adds a decorator applied to every source, outside the built-in
// ones. Use it for behaviour the library does not provide — a circuit breaker, a
// cache, a per-source metrics exporter.
func WithMiddleware(mw ...middleware.Middleware) Option {
	return func(c *config) { c.middleware = append(c.middleware, mw...) }
}
