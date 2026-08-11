package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/jobscraper/ports"
	"github.com/nonamecat19/jobscraper/retrieval"
)

// FormLogin authenticates against an ordinary HTML login form: fetch the page,
// read its CSRF token out of a hidden input, post the credentials, and pick the
// session cookie out of the jar.
//
// It covers every credentialed source the library ships. A site that needs
// something else — an OAuth dance, a JSON endpoint, a second factor —
// implements ports.Authenticator directly instead.
type FormLogin struct {
	// Key is the source key. Required.
	Key string

	// BaseURL is the site root, without a trailing slash. Required, and
	// overridable so tests can point at a fixture server.
	BaseURL string

	// LoginPath is appended to BaseURL. Defaults to "/login".
	LoginPath string

	// CookieName is the cookie the site issues on a successful login, and the
	// value this returns. Required — without it there is no way to tell the
	// session cookie apart from the analytics ones.
	CookieName string

	// CSRFField is the name of the hidden input holding the CSRF token, and the
	// form field it is posted back as. Empty means the form has no CSRF token.
	CSRFField string

	// CSRFRequired makes a missing token an error. Leave it false for sites
	// that only sometimes issue one: posting without it still succeeds there,
	// and failing early would break a login that works.
	CSRFRequired bool

	// UserAgent is sent on both requests. Defaults to the library's standard
	// browser identity, which is what the rest of the fetching wears.
	UserAgent string

	// CredentialHint names the settings a consumer must fill in, and is quoted
	// back in the error when they are missing. Without it the error says only
	// that credentials are required, leaving the reader to guess where they go.
	CredentialHint string

	// Timeout bounds each of the two requests. Defaults to 20s.
	Timeout time.Duration
}

var _ ports.Authenticator = FormLogin{}

func (f FormLogin) SourceKey() string { return f.Key }

// Login performs the two-request form dance and returns the session cookie.
func (f FormLogin) Login(ctx context.Context, email, password string) (string, error) {
	if email == "" || password == "" {
		return "", fmt.Errorf("%s login requires credentials%s", f.Key, f.hint())
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: f.timeout(), Jar: jar}
	loginURL := strings.TrimRight(f.BaseURL, "/") + f.loginPath()

	token, err := f.csrfToken(ctx, client, loginURL)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("email", email)
	form.Set("password", password)
	if f.CSRFField != "" && token != "" {
		form.Set(f.CSRFField, token)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", loginURL)
	req.Header.Set("User-Agent", f.userAgent())

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s login POST failed: %w", f.Key, err)
	}
	defer res.Body.Close()

	// The cookie is read from the jar rather than the response, because sites
	// routinely set it on a redirect hop rather than the final response.
	base, err := url.Parse(f.BaseURL)
	if err != nil {
		return "", err
	}
	for _, c := range jar.Cookies(base) {
		if c.Name == f.CookieName && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("%s login failed: no %q cookie returned%s",
		f.Key, f.CookieName, f.hint())
}

// csrfToken fetches the login page and extracts the hidden CSRF input.
func (f FormLogin) csrfToken(ctx context.Context, client *http.Client, loginURL string) (string, error) {
	if f.CSRFField == "" {
		return "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", f.userAgent())

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s login GET failed: %w", f.Key, err)
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", err
	}

	token, _ := doc.Find(fmt.Sprintf(`input[name=%q]`, f.CSRFField)).First().Attr("value")
	if token == "" && f.CSRFRequired {
		return "", fmt.Errorf("%s login: %s not found on login page", f.Key, f.CSRFField)
	}
	return token, nil
}

func (f FormLogin) loginPath() string {
	if f.LoginPath != "" {
		return f.LoginPath
	}
	return "/login"
}

func (f FormLogin) userAgent() string {
	if f.UserAgent != "" {
		return f.UserAgent
	}
	return retrieval.Chrome126UserAgent
}

func (f FormLogin) timeout() time.Duration {
	if f.Timeout > 0 {
		return f.Timeout
	}
	return 20 * time.Second
}

func (f FormLogin) hint() string {
	if f.CredentialHint == "" {
		return ""
	}
	return " (check " + f.CredentialHint + ")"
}
