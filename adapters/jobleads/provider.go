package jobleads

import (
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
	"github.com/nonamecat19/job-scraper/session"
)

// Key is this source's registry key.
const Key = "jobleads"

// BaseURL is JobLeads' site root. Tests override it to point the login at a
// fixture server.
const BaseURL = "https://www.jobleads.com"

// Authenticator describes JobLeads' login form. The CSRF token is optional
// here: JobLeads only sometimes renders one, and posting without it succeeds.
func Authenticator(baseURL string) ports.Authenticator {
	if baseURL == "" {
		baseURL = BaseURL
	}
	return session.FormLogin{
		Key:            Key,
		BaseURL:        baseURL,
		CookieName:     "session",
		CSRFField:      "csrf_token",
		CredentialHint: "JOBLEADS_EMAIL and JOBLEADS_PASSWORD",
	}
}

// NewSession builds the session provider JobLeads reads with.
func NewSession(config ports.SourceConfigStore, email, password string) (ports.SessionProvider, error) {
	return session.NewProvider(Authenticator(""), config, email, password)
}

// New builds the source. JobLeads shows almost nothing to anonymous visitors,
// so a session is required rather than optional.
func New(scraper ports.Scraper, sess ports.SessionProvider) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("jobleads: a Scraper is required")
	}
	if sess == nil {
		return nil, fmt.Errorf("jobleads: a SessionProvider is required — JobLeads serves listings only to signed-in users")
	}
	return Source{Scraping: scraper, Session: sess}, nil
}

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			if err := deps.Require(Key, deps.Config != nil, "Config"); err != nil {
				return nil, err
			}
			sess, err := session.NewProvider(Authenticator(""), deps.Config, "", "")
			if err != nil {
				return nil, err
			}
			return New(deps.Scraper, sess)
		},
	}
}

func init() { adapter.Register(Provider()) }
