package retrieval

import (
	"context"
	"net/http"
	"time"
)

type HostState struct {
	Host               string
	IdentityVersion    string
	CurrentRung        string
	RungLastVerifiedAt *time.Time
	Cookies            []byte // plaintext JSON (map[string]string); app encrypts at rest
	ConsecutiveBlocks  int32
	CoolingOffUntil    *time.Time
	LastBlockAt        *time.Time
	LastBlockReason    *string
	CrawlDelaySeconds  *int32
}

type StateStorePort interface {
	// Get returns the stored state for host. Returning (nil, nil) for a host
	// the store has never seen is legal — the engine treats a nil state as
	// "no history" and proceeds from the cheapest rung.
	Get(ctx context.Context, host string) (*HostState, error)
	Upsert(ctx context.Context, host string, state *HostState) error
	FetchAndSetCrawlDelay(ctx context.Context, host string) error
	RecordBlock(ctx context.Context, host string, reason string) error
	RecordSuccess(ctx context.Context, host string, rung string) error
	ClearRung(ctx context.Context, host string) error
	ClearCookies(ctx context.Context, host string) error
	LoadCookies(ctx context.Context, host string) ([]*http.Cookie, error)
	SaveCookies(ctx context.Context, host string, cookies []*http.Cookie) error
}
