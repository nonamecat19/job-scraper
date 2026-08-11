package retrieval_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/nonamecat19/jobscraper/ports"
	"github.com/nonamecat19/jobscraper/retrieval"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

// TestEngineWithInMemoryStore drives the engine end to end with no database in
// the call path. A 403 with no browser or FlareSolverr rung configured has
// nowhere left to escalate to, so the engine must report the refusal rather
// than succeed, and must record the block against the in-memory store.
func TestEngineWithInMemoryStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	store := memstore.NewStateStore()
	engine := retrieval.NewEngine(store)

	res, err := engine.Fetch(context.Background(), ports.FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch returned a transport error, want a reported outcome: %v", err)
	}
	if res.Outcome.Status == ports.PageRead {
		t.Fatalf("403 reported as read, want refused/challenged/deferred; outcome=%+v", res.Outcome)
	}
	if len(store.Blocks()) == 0 {
		t.Error("engine exhausted the ladder without recording a block")
	}

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	if _, err := engine.HostStatus(context.Background(), u.Hostname()); err != nil {
		t.Fatalf("HostStatus against in-memory store: %v", err)
	}
}

// TestEngineHostStatusNoDB confirms the read-only surface works against a store
// that has never seen the host — the zero-config consumer path.
func TestEngineHostStatusNoDB(t *testing.T) {
	engine := retrieval.NewEngine(memstore.NewStateStore())

	st, err := engine.HostStatus(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("HostStatus on unknown host: %v", err)
	}
	if st.Host != "example.com" {
		t.Errorf("Host = %q, want %q", st.Host, "example.com")
	}
}

// stubRung is a Rung that reports a fixed outcome, so a test can assert on how
// the engine walks the ladder without any network.
type stubRung struct {
	key       string
	status    ports.PageStatus
	body      string
	available bool
	calls     *[]string
}

func (s stubRung) Key() string                    { return s.key }
func (s stubRung) Available(context.Context) bool { return s.available }
func (s stubRung) Close() error                   { return nil }

func (s stubRung) Fetch(_ context.Context, req ports.FetchRequest) (ports.PageOutcome, string) {
	*s.calls = append(*s.calls, s.key)
	return ports.PageOutcome{Status: s.status, Method: s.key, URL: req.URL}, s.body
}

// TestEngineEscalatesThroughLadder confirms the chain hands a blocked request
// up one rung at a time and stops at the first one that reads the page.
func TestEngineEscalatesThroughLadder(t *testing.T) {
	var calls []string
	ladder := retrieval.NewLadder(
		stubRung{key: "cheap", status: ports.PageChallenged, available: true, calls: &calls},
		stubRung{key: "mid", status: ports.PageChallenged, available: true, calls: &calls},
		stubRung{key: "expensive", status: ports.PageRead, body: "<html>ok</html>", available: true, calls: &calls},
	)

	engine := retrieval.NewEngine(memstore.NewStateStore(), retrieval.WithLadder(ladder))

	res, err := engine.Fetch(context.Background(), ports.FetchRequest{URL: "https://example.com/jobs"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Outcome.Status != ports.PageRead {
		t.Fatalf("Status = %q, want %q", res.Outcome.Status, ports.PageRead)
	}
	if got, want := len(calls), 3; got != want {
		t.Fatalf("tried %d rungs (%v), want %d", got, calls, want)
	}
	if res.Outcome.Method != "expensive" {
		t.Errorf("Method = %q, want the rung that succeeded", res.Outcome.Method)
	}
}

// TestEngineDoesNotEscalateCredentialedRequests pins an authenticated request
// to the cheapest rung: swapping transport under a live session invalidates it.
func TestEngineDoesNotEscalateCredentialedRequests(t *testing.T) {
	var calls []string
	ladder := retrieval.NewLadder(
		stubRung{key: "cheap", status: ports.PageChallenged, available: true, calls: &calls},
		stubRung{key: "expensive", status: ports.PageRead, available: true, calls: &calls},
	)

	engine := retrieval.NewEngine(memstore.NewStateStore(), retrieval.WithLadder(ladder))

	res, err := engine.Fetch(context.Background(), ports.FetchRequest{
		URL:             "https://example.com/jobs",
		UsesUserAccount: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Outcome.Status == ports.PageRead {
		t.Error("credentialed request escalated and succeeded, want the block reported instead")
	}
	if len(calls) != 1 || calls[0] != "cheap" {
		t.Errorf("rungs tried = %v, want only [cheap]", calls)
	}
}

// TestEngineStopsAtUnavailableRung confirms an unreachable next rung ends
// escalation rather than being tried into a guaranteed failure.
func TestEngineStopsAtUnavailableRung(t *testing.T) {
	var calls []string
	ladder := retrieval.NewLadder(
		stubRung{key: "cheap", status: ports.PageChallenged, available: true, calls: &calls},
		stubRung{key: "offline", status: ports.PageRead, available: false, calls: &calls},
	)

	engine := retrieval.NewEngine(memstore.NewStateStore(), retrieval.WithLadder(ladder))

	if _, err := engine.Fetch(context.Background(), ports.FetchRequest{URL: "https://example.com/jobs"}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("rungs tried = %v, want only the available one", calls)
	}
}
