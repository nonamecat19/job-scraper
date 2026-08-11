// Package memstore provides in-memory implementations of the persistence
// ports. They exist so the library is usable with no database at all: a
// consumer that just wants to run a search wires memstore.NewStateStore() and
// is done, and a test that wants to assert on what was persisted reads the
// store directly.
//
// Nothing here survives a restart. A long-running consumer should implement the
// ports against its own database, using these as the reference for the
// contract.
package memstore

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/nonamecat19/jobscraper/ports"
)

// StateStore is an in-memory ports.StateStore. The zero value is not usable;
// call NewStateStore. It is safe for concurrent use.
type StateStore struct {
	mu      sync.Mutex
	states  map[string]*ports.HostState
	cookies map[string][]*http.Cookie
	blocks  []string
}

var _ ports.StateStore = (*StateStore)(nil)

func NewStateStore() *StateStore {
	return &StateStore{
		states:  map[string]*ports.HostState{},
		cookies: map[string][]*http.Cookie{},
	}
}

// Blocks returns every block recorded so far, as "host:reason". Tests assert on
// it; operators reading a live process get the same history the engine saw.
func (m *StateStore) Blocks() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.blocks))
	copy(out, m.blocks)
	return out
}

// Get returns a copy, so a caller mutating the result cannot corrupt the store
// behind Upsert's back.
func (m *StateStore) Get(_ context.Context, host string) (*ports.HostState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[host]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (m *StateStore) Upsert(_ context.Context, host string, state *ports.HostState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *state
	m.states[host] = &cp
	return nil
}

// FetchAndSetCrawlDelay is a no-op: reading robots.txt is a network concern the
// consumer's real store owns.
func (m *StateStore) FetchAndSetCrawlDelay(context.Context, string) error { return nil }

func (m *StateStore) RecordBlock(_ context.Context, host, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks = append(m.blocks, host+":"+reason)
	s := m.stateLocked(host)
	s.ConsecutiveBlocks++
	now := time.Now()
	s.LastBlockAt = &now
	s.LastBlockReason = &reason
	return nil
}

func (m *StateStore) RecordSuccess(_ context.Context, host, rung string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.stateLocked(host)
	s.CurrentRung = rung
	now := time.Now()
	s.RungLastVerifiedAt = &now
	s.ConsecutiveBlocks = 0
	return nil
}

func (m *StateStore) ClearRung(_ context.Context, host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[host]; ok {
		s.CurrentRung = ""
		s.RungLastVerifiedAt = nil
	}
	return nil
}

func (m *StateStore) ClearCookies(_ context.Context, host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cookies, host)
	return nil
}

func (m *StateStore) LoadCookies(_ context.Context, host string) ([]*http.Cookie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cookies[host], nil
}

func (m *StateStore) SaveCookies(_ context.Context, host string, cookies []*http.Cookie) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cookies[host] = cookies
	return nil
}

// stateLocked returns the state for host, creating it if absent. Callers must
// hold m.mu.
func (m *StateStore) stateLocked(host string) *ports.HostState {
	s, ok := m.states[host]
	if !ok {
		s = &ports.HostState{Host: host}
		m.states[host] = s
	}
	return s
}
