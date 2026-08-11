# jobscraper

A Go library that reads job postings from 22 sites behind one interface.

```go
client, err := jobscraper.New()
if err != nil {
	return err
}
defer client.Close()

res, err := client.Search(ctx, model.SearchQuery{Keywords: "golang"})
for _, job := range res.Jobs {
	fmt.Println(job.SourceKey, job.Title, job.URL)
}
```

That runs every source that works without configuration. Sources that need
something they were not given — an employer roster, an API key, a login — are
skipped with a logged reason rather than failing the run, so a partly configured
client is a working client.

## Layout

Each layer depends only on the interfaces in `ports`, never on the layer below.

```
jobscraper/            Client — the facade. Wires everything, runs the fan-out.
├── ports/             Every interface, and the values they exchange.
├── model/             Domain types: NormalizedJob, SearchQuery.
├── adapter/           Registry, Provider/Catalog factories, capability helpers.
│   └── middleware/    Decorators: recover, observe, timeout, retry, log.
├── adapters/          One package per site, each a ports.JobSource.
│   ├── atsboard/      Template shared by the five ATS vendors.
│   ├── all/           Imports every vendor, for side-effect registration.
│   └── greenhouse/ lever/ ashby/ workable/ smartrecruiters/
│       djinni/ dou/ workua/ robota/ indeed/ glassdoor/ jobgether/
│       jobleads/ wellfound/ remoteok/ remotive/ himalayas/
│       arbeitnow/ adzuna/ jooble/ jobspy/ manual/
├── retrieval/         The escalating fetch ladder: Rung strategies in a chain.
├── session/           Logged-in sessions, one implementation for every site.
├── store/memstore/    In-memory implementations of the persistence ports.
└── internal/          Helpers with no public contract.
```

## The design, and why

Every pattern here earns its place by removing something.

**Ports and adapters.** `ports` holds every interface and imports nothing but
`model`. Everything else depends on it rather than on a concrete type, which is
what makes the library's storage, transport and sources all replaceable from
outside. The library ships no database code at all: `ports.StateStore`,
`ports.Roster` and `ports.SourceConfigStore` are yours to implement, with
`store/memstore` as both the reference implementation and a working default.

**Abstract Factory + a self-registering catalog.** A source is described by an
`adapter.Provider` — its key, and how to build it from `adapter.Deps`. Vendor
packages register their Provider from `init`, so the set of available sources is
whatever the binary imported rather than a switch statement somebody has to
remember to extend. Import `adapters/all` for everything, or individual vendors
for a restricted build.

```go
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			return New(deps.Scraper)
		},
	}
}

func init() { adapter.Register(Provider()) }
```

**One dependency bag.** Every source is constructed from the same
`adapter.Deps`, taking only the ports it needs. Adding a dependency to one
source costs nothing anywhere else.

**Decorator chain.** Retries, timeouts, logging, panic containment and
observation are middleware wrapped around every source, so they exist once
instead of twenty-two times. Order is load-bearing: `Recover` outermost so
nothing escapes, `Timeout` above `Retry` so the budget covers the retries,
`Log` innermost so it records what actually happened on the wire.

Decorators stay transparent by embedding `middleware.Base`, which provides
`Unwrap`; capabilities are then queried through `adapter.As[T]`, which walks the
wrapper chain:

```go
if pr, ok := adapter.As[ports.PostingReader](src); ok {
	job, err := pr.ReadPosting(ctx, url, cfg)
}
```

**Strategy + Chain of Responsibility for retrieval.** Each way of fetching a
page is a `retrieval.Rung`: a TLS-fingerprinted client, a headless browser, a
FlareSolverr sidecar. The engine walks them cheapest-first, escalating when a
page comes back challenged, and remembers per host which one worked. Adding a
residential proxy pool means implementing one interface and passing
`retrieval.WithRung`.

**Template Method for the ATS vendors.** Greenhouse, Lever, Ashby, Workable and
SmartRecruiters differ only in a URL and a JSON shape. `atsboard.Source` owns
everything else — the roster walk, the per-employer outcome classification, the
runaway-board cap, the "did the whole vendor fail?" decision — and each vendor
is an `atsboard.Fetcher`:

```go
type Fetcher struct{}

func (Fetcher) Vendor() string { return Key }
func (Fetcher) FetchEmployer(ctx context.Context, e ports.EmployerBoard) (int, []model.NormalizedJob, error)
```

**One session implementation.** Credentialed sites differ in their login form's
CSRF field and their session cookie's name. `session.Provider` owns the caching,
the persistence and the concurrent-login lock; `session.FormLogin` is the
configurable strategy for the form itself.

**Functional options.** `jobscraper.New` and `retrieval.NewEngine` both take
options, so every knob has a working default and adding one breaks nobody.

## Configuring

```go
client, err := jobscraper.New(
	jobscraper.WithStateStore(myPostgresStateStore), // survives restarts
	jobscraper.WithRoster(myEmployerRoster),         // enables the ATS sources
	jobscraper.WithConfigStore(myConfigStore),       // API keys and sessions
	jobscraper.WithBrowser(true),                    // JS-rendered listings
	jobscraper.WithFlareSolverr("http://localhost:8191"),
	jobscraper.WithSources("greenhouse", "lever", "djinni"),
	jobscraper.WithConcurrency(8),
	jobscraper.WithSourceTimeout(2*time.Minute),
	jobscraper.WithObserver(myProgressReporter),
)
```

Plugging in a source of your own needs no changes here:

```go
jobscraper.New(jobscraper.WithSource(myCompanyBoard))
```

## Reading the result

`Search` never fails because one site did. Its error is non-nil only when every
source failed; everything else is in the result.

```go
res, err := client.Search(ctx, query)

switch res.Verdict() {
case ports.VerdictSuccess: // every source returned
case ports.VerdictPartial: // some sources lost; res.Jobs still has the rest
case ports.VerdictBlocked: // nothing came back; err is non-nil
}

for _, f := range res.Failed() {
	log.Printf("%s: %v", f.SourceKey, f.Err)
}
```

## Tests

```sh
go test ./...                        # unit tests, no network
go test -tags integration ./...      # against local mock servers
go test -tags live ./adapters/live/  # against the real sites; slow, flaky
```
