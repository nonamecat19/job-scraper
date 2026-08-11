package jobscraper_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nonamecat19/jobscraper"
	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/adapter/middleware"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

// fakeSource is a JobSource with scripted behaviour, so the facade's fan-out
// can be tested without any network.
type fakeSource struct {
	key   string
	jobs  []model.NormalizedJob
	err   error
	panic bool

	mu    sync.Mutex
	calls int
}

func (f *fakeSource) Key() string            { return f.key }
func (f *fakeSource) Kind() model.SourceKind { return model.SourceKindAPI }

func (f *fakeSource) Search(context.Context, model.SearchQuery, map[string]any) ([]model.NormalizedJob, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.panic {
		panic("source exploded")
	}
	return f.jobs, f.err
}

func (f *fakeSource) HealthCheck(context.Context, map[string]any) (bool, error) {
	return f.err == nil, f.err
}

func (f *fakeSource) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func job(source, title string) model.NormalizedJob {
	return model.NormalizedJob{SourceKey: source, Title: title}
}

// newClient builds a client over only the given sources, with retries disabled
// so call counts are predictable.
func newClient(t *testing.T, sources ...ports.JobSource) *jobscraper.Client {
	t.Helper()

	opts := []jobscraper.Option{
		jobscraper.WithCatalog(adapter.NewCatalog()), // empty: only the explicit sources
		jobscraper.WithRetry(middleware.RetryConfig{Attempts: 1}),
		jobscraper.WithStateStore(memstore.NewStateStore()),
	}
	for _, s := range sources {
		opts = append(opts, jobscraper.WithSource(s))
	}

	client, err := jobscraper.New(opts...)
	if err != nil {
		t.Fatalf("jobscraper.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestSearchCollectsFromEverySource is the basic fan-out: every source runs and
// its postings all land in the result.
func TestSearchCollectsFromEverySource(t *testing.T) {
	client := newClient(t,
		&fakeSource{key: "alpha", jobs: []model.NormalizedJob{job("alpha", "Go Engineer")}},
		&fakeSource{key: "beta", jobs: []model.NormalizedJob{job("beta", "Rust Engineer"), job("beta", "SRE")}},
	)

	res, err := client.Search(context.Background(), model.SearchQuery{Keywords: "engineer"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Jobs) != 3 {
		t.Errorf("got %d jobs, want 3", len(res.Jobs))
	}
	if res.Verdict() != ports.VerdictSuccess {
		t.Errorf("Verdict = %q, want %q", res.Verdict(), ports.VerdictSuccess)
	}
}

// TestSearchSurvivesOneFailingSource is the property that matters most in a
// run across twenty sites: one site being down must not cost the other
// nineteen their results.
func TestSearchSurvivesOneFailingSource(t *testing.T) {
	client := newClient(t,
		&fakeSource{key: "good", jobs: []model.NormalizedJob{job("good", "Go Engineer")}},
		&fakeSource{key: "bad", err: errors.New("host refused")},
	)

	res, err := client.Search(context.Background(), model.SearchQuery{})
	if err != nil {
		t.Fatalf("Search errored on a partial run, want the postings that did arrive: %v", err)
	}
	if len(res.Jobs) != 1 {
		t.Errorf("got %d jobs, want the 1 from the working source", len(res.Jobs))
	}
	if res.Verdict() != ports.VerdictPartial {
		t.Errorf("Verdict = %q, want %q", res.Verdict(), ports.VerdictPartial)
	}
	if failed := res.Failed(); len(failed) != 1 || failed[0].SourceKey != "bad" {
		t.Errorf("Failed() = %+v, want just the failing source", failed)
	}
}

// TestSearchReportsTotalFailure confirms that losing every source is escalated
// to an error, rather than being reported as a successful empty search.
func TestSearchReportsTotalFailure(t *testing.T) {
	client := newClient(t,
		&fakeSource{key: "a", err: errors.New("boom")},
		&fakeSource{key: "b", err: errors.New("bang")},
	)

	res, err := client.Search(context.Background(), model.SearchQuery{})
	if err == nil {
		t.Fatal("Search succeeded with every source failing, want an error")
	}
	if res.Verdict() != ports.VerdictBlocked {
		t.Errorf("Verdict = %q, want %q", res.Verdict(), ports.VerdictBlocked)
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "bang") {
		t.Errorf("error %q does not name both failures", err)
	}
}

// TestSearchContainsPanics checks the Recover decorator end to end: a source
// that panics becomes one failed source, not a dead process.
func TestSearchContainsPanics(t *testing.T) {
	client := newClient(t,
		&fakeSource{key: "exploder", panic: true},
		&fakeSource{key: "fine", jobs: []model.NormalizedJob{job("fine", "Go Engineer")}},
	)

	res, err := client.Search(context.Background(), model.SearchQuery{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Jobs) != 1 {
		t.Errorf("got %d jobs, want the 1 from the source that did not panic", len(res.Jobs))
	}

	var panicErr *middleware.PanicError
	if failed := res.Failed(); len(failed) != 1 || !errors.As(failed[0].Err, &panicErr) {
		t.Errorf("panic was not reported as a PanicError; failed = %+v", res.Failed())
	}
}

// TestSearchQuerySourcesNarrowsTheRun confirms query.Sources selects a subset
// without the untouched sources being called at all.
func TestSearchQuerySourcesNarrowsTheRun(t *testing.T) {
	wanted := &fakeSource{key: "wanted", jobs: []model.NormalizedJob{job("wanted", "Go Engineer")}}
	skipped := &fakeSource{key: "skipped", jobs: []model.NormalizedJob{job("skipped", "Rust Engineer")}}

	client := newClient(t, wanted, skipped)

	res, err := client.Search(context.Background(), model.SearchQuery{Sources: []string{"wanted"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Jobs) != 1 || res.Jobs[0].SourceKey != "wanted" {
		t.Errorf("got %+v, want only the wanted source's posting", res.Jobs)
	}
	if skipped.Calls() != 0 {
		t.Errorf("skipped source was called %d times, want 0", skipped.Calls())
	}
}

// TestRetryRerunsAFailedSource exercises the retry decorator through the
// facade, since retry configuration is a client-level knob.
func TestRetryRerunsAFailedSource(t *testing.T) {
	flaky := &fakeSource{key: "flaky", err: errors.New("transient")}

	client, err := jobscraper.New(
		jobscraper.WithCatalog(adapter.NewCatalog()),
		jobscraper.WithSource(flaky),
		jobscraper.WithRetry(middleware.RetryConfig{Attempts: 3, BaseDelay: time.Millisecond}),
		jobscraper.WithStateStore(memstore.NewStateStore()),
	)
	if err != nil {
		t.Fatalf("jobscraper.New: %v", err)
	}
	defer client.Close()

	if _, err := client.Search(context.Background(), model.SearchQuery{}); err == nil {
		t.Fatal("Search succeeded, want the persistent failure reported")
	}
	if got := flaky.Calls(); got != 3 {
		t.Errorf("source called %d times, want 3 attempts", got)
	}
}

// TestReadPostingWithoutAClaimantIsReported confirms an unrecognised URL comes
// back as the specific "no source reads this" error, which is the caller's cue
// to fall back to manual entry.
func TestReadPostingWithoutAClaimantIsReported(t *testing.T) {
	client := newClient(t, &fakeSource{key: "alpha"})

	_, err := client.ReadPosting(context.Background(), "https://nowhere.example/jobs/1")

	var noReader adapter.NoPostingReaderError
	if !errors.As(err, &noReader) {
		t.Fatalf("err = %v, want adapter.NoPostingReaderError", err)
	}
}

// TestObserverSeesEverySource checks the observer port is notified once per
// source, at both ends of the run.
func TestObserverSeesEverySource(t *testing.T) {
	var mu sync.Mutex
	starts, finishes := 0, 0

	obs := recordingObserver{
		start:  func() { mu.Lock(); starts++; mu.Unlock() },
		finish: func() { mu.Lock(); finishes++; mu.Unlock() },
	}

	client, err := jobscraper.New(
		jobscraper.WithCatalog(adapter.NewCatalog()),
		jobscraper.WithSource(&fakeSource{key: "a"}),
		jobscraper.WithSource(&fakeSource{key: "b"}),
		jobscraper.WithObserver(obs),
		jobscraper.WithStateStore(memstore.NewStateStore()),
	)
	if err != nil {
		t.Fatalf("jobscraper.New: %v", err)
	}
	defer client.Close()

	if _, err := client.Search(context.Background(), model.SearchQuery{}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if starts != 2 || finishes != 2 {
		t.Errorf("observer saw %d starts and %d finishes, want 2 and 2", starts, finishes)
	}
}

type recordingObserver struct {
	start  func()
	finish func()
}

func (r recordingObserver) OnSourceStart(ports.RunEvent)  { r.start() }
func (r recordingObserver) OnSourceFinish(ports.RunEvent) { r.finish() }

// TestUnknownSourceKeyIsAWiringError confirms a typo in WithSources fails
// construction rather than silently running with fewer sources.
func TestUnknownSourceKeyIsAWiringError(t *testing.T) {
	_, err := jobscraper.New(jobscraper.WithSources("greenhosue"))
	if err == nil {
		t.Fatal("New accepted an unknown source key, want an error")
	}
}

// TestDefaultClientBuildsBundledSources is the zero-config path: no options at
// all must still produce a client with sources in it.
func TestDefaultClientBuildsBundledSources(t *testing.T) {
	client, err := jobscraper.New()
	if err != nil {
		t.Fatalf("jobscraper.New: %v", err)
	}
	defer client.Close()

	if len(client.Sources()) == 0 {
		t.Error("default client has no sources")
	}
}
