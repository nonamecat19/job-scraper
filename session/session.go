// Package session manages the logged-in sessions credentialed sources need.
//
// Sites that require an account differ only in the shape of their login form
// and the name of the cookie they hand back. Everything around that — caching
// the cookie, persisting it so a restart does not force a fresh login,
// serialising concurrent logins, invalidating on a session that has expired —
// is identical, and lives here once.
//
// Provider is the shared half; ports.Authenticator is the site-specific half.
// FormLogin covers the common case of an HTML form with a CSRF token, so most
// sources need no code here at all:
//
//	auth := session.FormLogin{
//		Key:        "example",
//		BaseURL:    "https://example.com",
//		CookieName: "sessionid",
//		CSRFField:  "csrf_token",
//	}
//	provider := session.NewProvider(auth, configStore, email, password)
package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/ports"
)

// configKey is the field a session cookie is persisted under in the source's
// stored configuration.
const configKey = "sessionCookie"

// Provider caches one site's session cookie, persisting it through a
// SourceConfigStore so a login survives a restart.
//
// Ensure is the hot path and does no network I/O when a cookie is stored.
// Refresh forces a new login and is what a source calls after a request came
// back at the login page.
type Provider struct {
	auth   ports.Authenticator
	config ports.SourceConfigStore

	email    string
	password string

	mu sync.Mutex
}

var _ ports.SessionProvider = (*Provider)(nil)

// NewProvider builds a session provider for one credentialed source.
//
// Empty credentials are allowed: Ensure then returns an empty cookie rather
// than an error, and the source falls back to whatever it can read logged out.
// That is deliberate — a consumer who has not configured Djinni credentials
// should still get Djinni's public listings rather than a failed run.
func NewProvider(auth ports.Authenticator, config ports.SourceConfigStore, email, password string) (*Provider, error) {
	if auth == nil {
		return nil, fmt.Errorf("session: an Authenticator is required")
	}
	if config == nil {
		return nil, fmt.Errorf("session: source %q requires a config store to persist its session cookie", auth.SourceKey())
	}
	return &Provider{auth: auth, config: config, email: email, password: password}, nil
}

// HasCredentials reports whether a login is even possible. A source uses it to
// decide whether to advertise itself as credentialed.
func (p *Provider) HasCredentials() bool { return p.email != "" && p.password != "" }

// Ensure returns a usable session cookie, logging in only if none is stored.
//
// It returns ("", nil) when no cookie is stored and no credentials were
// configured: that is not a failure, it is the logged-out path.
func (p *Provider) Ensure(ctx context.Context) (string, error) {
	cfg, err := p.config.Config(ctx, p.auth.SourceKey())
	if err != nil {
		return "", err
	}
	if cookie := strutil.StringOr(cfg[configKey], ""); cookie != "" {
		return cookie, nil
	}
	if !p.HasCredentials() {
		return "", nil
	}
	return p.Refresh(ctx)
}

// Refresh logs in again and replaces the stored cookie.
//
// The lock serialises concurrent refreshes: several sources hitting an expired
// session at once would otherwise each perform a login, and sites treat a burst
// of logins far less kindly than one.
func (p *Provider) Refresh(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cookie, err := p.auth.Login(ctx, p.email, p.password)
	if err != nil {
		return "", err
	}
	if _, err := p.config.Update(ctx, p.auth.SourceKey(), nil, map[string]any{configKey: cookie}); err != nil {
		return "", fmt.Errorf("session: %s: persist session cookie: %w", p.auth.SourceKey(), err)
	}
	return cookie, nil
}
