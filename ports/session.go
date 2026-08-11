package ports

import (
	"context"

	"github.com/nonamecat19/jobscraper/model"
)

// SourceConfigStore is the per-source settings port. Credentialed sources use
// it to read their configuration and to write back a session cookie they just
// obtained, so a login survives process restarts. The consumer owns the storage
// and decides whether the persisted cookie is encrypted.
type SourceConfigStore interface {
	// Config returns the stored settings for the source, or an empty map when
	// the source has none.
	Config(ctx context.Context, key string) (map[string]any, error)

	// Update patches the source's settings. A nil enabled leaves the enabled
	// flag untouched; configPatch is merged over the existing config rather
	// than replacing it.
	Update(ctx context.Context, key string, enabled *bool, configPatch map[string]any) (*model.JobSourceDto, error)
}

// SessionProvider hands out an authenticated session cookie for a credentialed
// source. One implementation serves every such source — the differences between
// sites live in Authenticator, not here.
//
// Ensure is the hot path: it returns the cached cookie without any network I/O
// whenever one is stored, and only logs in when none is. Refresh is what a
// source calls after a request came back at the login page, forcing a new login
// and replacing the stored cookie. Both must be safe for concurrent use.
type SessionProvider interface {
	Ensure(ctx context.Context) (string, error)
	Refresh(ctx context.Context) (string, error)
}

// Authenticator performs the site-specific half of a login: fetch whatever CSRF
// token the form needs, post the credentials, and return the session cookie the
// site issued. It is the Strategy that SessionProvider composes, which is why
// adding a credentialed source needs no new session plumbing.
type Authenticator interface {
	// SourceKey identifies the source whose settings hold this login.
	SourceKey() string

	// Login exchanges credentials for a session cookie value.
	Login(ctx context.Context, email, password string) (cookie string, err error)
}
