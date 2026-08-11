package ports

import (
	"context"
	"net/http"
	"time"
)

// HostState is everything the retriever remembers between runs about one host:
// which strategy worked, what session it holds, and how badly it has been
// blocked lately.
type HostState struct {
	Host               string
	IdentityVersion    string
	CurrentRung        string
	RungLastVerifiedAt *time.Time
	// Cookies is plaintext JSON (map[string]string). The store is responsible
	// for encrypting it at rest — the library never does.
	Cookies           []byte
	ConsecutiveBlocks int32
	CoolingOffUntil   *time.Time
	LastBlockAt       *time.Time
	LastBlockReason   *string
	CrawlDelaySeconds *int32
}

// StateStore persists HostState. The library ships no implementation: the
// consumer owns the database, so the consumer owns this port.
type StateStore interface {
	// Get returns the stored state for host. Returning (nil, nil) for a host
	// the store has never seen is legal — the engine treats a nil state as
	// "no history" and proceeds from the cheapest rung.
	Get(ctx context.Context, host string) (*HostState, error)
	Upsert(ctx context.Context, host string, state *HostState) error

	// FetchAndSetCrawlDelay reads the host's robots.txt and records any
	// crawl-delay it declares. The engine calls it in the background.
	FetchAndSetCrawlDelay(ctx context.Context, host string) error

	RecordBlock(ctx context.Context, host string, reason string) error
	RecordSuccess(ctx context.Context, host string, rung string) error
	ClearRung(ctx context.Context, host string) error
	ClearCookies(ctx context.Context, host string) error
	LoadCookies(ctx context.Context, host string) ([]*http.Cookie, error)
	SaveCookies(ctx context.Context, host string, cookies []*http.Cookie) error
}
