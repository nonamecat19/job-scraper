package djinni

import (
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
	"github.com/nonamecat19/job-scraper/session"
)

// Key is this source's registry key.
const Key = "djinni"

// BaseURL is Djinni's site root. Tests override it to point the login at a
// fixture server.
const BaseURL = "https://djinni.co"

// Authenticator describes Djinni's login form. Djinni runs Django, hence the
// csrfmiddlewaretoken field and the sessionid cookie, and the token is always
// present — a login page without one means the page itself changed.
func Authenticator(baseURL string) ports.Authenticator {
	if baseURL == "" {
		baseURL = BaseURL
	}
	return session.FormLogin{
		Key:            Key,
		BaseURL:        baseURL,
		CookieName:     "sessionid",
		CSRFField:      "csrfmiddlewaretoken",
		CSRFRequired:   true,
		CredentialHint: "DJINNI_EMAIL and DJINNI_PASSWORD",
	}
}

// NewSession builds the session provider Djinni reads with. Empty credentials
// are allowed: the source then reads only what Djinni serves logged out.
func NewSession(config ports.SourceConfigStore, email, password string) (ports.SessionProvider, error) {
	return session.NewProvider(Authenticator(""), config, email, password)
}

// New builds the source. A nil session is allowed and means logged-out reading.
func New(scraper ports.Scraper, sess ports.SessionProvider) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("djinni: a Scraper is required")
	}
	return Source{Scraping: scraper, Session: sess}, nil
}

// Provider is the factory entry for this source. It builds the logged-out form
// unless a config store is available to hold a session; credentials themselves
// come from that store.
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			var sess ports.SessionProvider
			if deps.Config != nil {
				s, err := session.NewProvider(Authenticator(""), deps.Config, "", "")
				if err != nil {
					return nil, err
				}
				sess = s
			}
			return New(deps.Scraper, sess)
		},
	}
}

func init() { adapter.Register(Provider()) }
