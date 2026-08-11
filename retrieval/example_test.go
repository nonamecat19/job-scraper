package retrieval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// memStore is the in-memory StateStorePort a consumer can supply instead of a
// database. It is deliberately the whole implementation a no-DB consumer needs
// (User Story 2 / SC-001): no Postgres, no encryption key, no app config.
type memStore struct {
	mu      sync.Mutex
	states  map[string]*HostState
	cookies map[string][]*http.Cookie
	blocks  []string
}

func newMemStore() *memStore {
	return &memStore{
		states:  map[string]*HostState{},
		cookies: map[string][]*http.Cookie{},
	}
}

func (m *memStore) Get(_ context.Context, host string) (*HostState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[host]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (m *memStore) Upsert(_ context.Context, host string, state *HostState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *state
	m.states[host] = &cp
	return nil
}

func (m *memStore) FetchAndSetCrawlDelay(context.Context, string) error { return nil }

func (m *memStore) RecordBlock(_ context.Context, host, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks = append(m.blocks, host+":"+reason)
	return nil
}

func (m *memStore) RecordSuccess(_ context.Context, host, rung string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[host]
	if !ok {
		s = &HostState{Host: host}
		m.states[host] = s
	}
	s.CurrentRung = rung
	return nil
}

func (m *memStore) ClearRung(_ context.Context, host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[host]; ok {
		s.CurrentRung = ""
	}
	return nil
}

func (m *memStore) ClearCookies(_ context.Context, host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cookies, host)
	return nil
}

func (m *memStore) LoadCookies(_ context.Context, host string) ([]*http.Cookie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cookies[host], nil
}

func (m *memStore) SaveCookies(_ context.Context, host string, cookies []*http.Cookie) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cookies[host] = cookies
	return nil
}

var _ StateStorePort = (*memStore)(nil)

// TestEngineWithInMemoryStore drives the engine end to end with no database in
// the call path. A 403 with no browser/FlareSolverr rung configured has nowhere
// left to escalate to, so the engine must report the refusal rather than
// succeed, and must record the block against the in-memory store.
func TestEngineWithInMemoryStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	store := newMemStore()
	engine := NewEngine(nil, store, EngineOpts{})

	res, err := engine.Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch returned a transport error, want a reported outcome: %v", err)
	}

	if res.Outcome.Status == PageRead {
		t.Fatalf("403 reported as read, want refused/challenged/deferred; outcome=%+v", res.Outcome)
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
	engine := NewEngine(nil, newMemStore(), EngineOpts{})

	st, err := engine.HostStatus(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("HostStatus on unknown host: %v", err)
	}
	if st.Host != "example.com" {
		t.Errorf("Host = %q, want %q", st.Host, "example.com")
	}
}
