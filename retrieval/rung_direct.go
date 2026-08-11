package retrieval

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"

	"github.com/nonamecat19/jobscraper/ports"
)

// maxBodyBytes caps how much of a response the direct rung will read, so a
// misbehaving host cannot exhaust memory.
const maxBodyBytes = 10 * 1024 * 1024

// DirectRung is the cheapest strategy: one request from a TLS-fingerprinted
// client wearing the configured browser identity. It carries the session
// forward by loading the host's stored cookies before the request and saving
// whatever the host set afterwards.
type DirectRung struct {
	identity *BrowserIdentity
	store    ports.StateStore
	detector ports.ChallengeDetector
}

var _ Rung = (*DirectRung)(nil)

// NewDirectRung builds the direct rung. A nil store disables cookie
// persistence, which is a supported configuration: the rung still fetches, it
// just starts every request logged out.
func NewDirectRung(identity *BrowserIdentity, store ports.StateStore, detector ports.ChallengeDetector) *DirectRung {
	if identity == nil {
		identity = DefaultBrowserIdentity()
	}
	if detector == nil {
		detector = DefaultDetector
	}
	return &DirectRung{identity: identity, store: store, detector: detector}
}

func (d *DirectRung) Key() string { return KeyDirect }

// Available is always true: the direct rung needs nothing but the network.
func (d *DirectRung) Available(context.Context) bool { return true }

func (d *DirectRung) Close() error { return nil }

func (d *DirectRung) Fetch(ctx context.Context, req ports.FetchRequest) (ports.PageOutcome, string) {
	body, statusCode, err := d.do(ctx, req)
	if err != nil {
		return outcome(ports.PageChallenged, KeyDirect, err.Error(), req.URL), body
	}
	if d.detector.IsChallenged(body, statusCode) {
		return outcome(ports.PageChallenged, KeyDirect, fmt.Sprintf("challenge detected (status %d)", statusCode), req.URL), body
	}
	if d.detector.IsRefused(body, statusCode) {
		return outcome(ports.PageRefused, KeyDirect, fmt.Sprintf("refused (status %d)", statusCode), req.URL), body
	}
	return outcome(ports.PageRead, KeyDirect, "", req.URL), body
}

func (d *DirectRung) do(ctx context.Context, freq ports.FetchRequest) (string, int, error) {
	u, err := url.Parse(freq.URL)
	if err != nil {
		return "", 0, fmt.Errorf("direct: parse url: %w", err)
	}
	host := u.Host

	cli, err := d.clientFor(ctx, host)
	if err != nil {
		return "", 0, fmt.Errorf("direct: init client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", freq.URL, nil)
	if err != nil {
		return "", 0, err
	}

	// Identity headers first, then caller overrides, so a source can replace
	// any of them. The three Sec-Fetch headers are set last and unconditionally
	// because they describe this request, not the identity.
	for _, h := range d.identity.Headers {
		if isSecFetchHeader(h[0]) {
			continue
		}
		if !headerExists(freq.Headers, h[0]) {
			req.Header.Set(h[0], h[1])
		}
	}
	for k, v := range freq.Headers {
		req.Header.Set(k, v)
	}
	if freq.RefererPage != "" {
		req.Header.Set("Referer", freq.RefererPage)
	}
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")

	res, err := cli.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("direct: fetch %s: %w", freq.URL, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return "", res.StatusCode, err
	}

	d.persistCookies(ctx, cli, res, host)

	return string(body), res.StatusCode, nil
}

func (d *DirectRung) persistCookies(ctx context.Context, cli tlsclient.HttpClient, res *http.Response, host string) {
	if d.store == nil || res.StatusCode >= 400 {
		return
	}
	cookies := cli.GetCookies(res.Request.URL)
	if len(cookies) == 0 {
		return
	}
	stdCookies := make([]*stdhttp.Cookie, len(cookies))
	for i, c := range cookies {
		stdCookies[i] = &stdhttp.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  c.Expires,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		}
	}
	_ = d.store.SaveCookies(ctx, host, stdCookies)
}

func (d *DirectRung) clientFor(ctx context.Context, host string) (tlsclient.HttpClient, error) {
	profile, err := tlsProfileFor(d.identity.TLSProfileID)
	if err != nil {
		return nil, err
	}

	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeout(30),
		tlsclient.WithClientProfile(profile),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithNotFollowRedirects(),
	}

	if d.store != nil {
		cookies, err := d.store.LoadCookies(ctx, host)
		if err == nil && len(cookies) > 0 {
			jar := tlsclient.NewCookieJar()
			fCookies := make([]*http.Cookie, len(cookies))
			for i, c := range cookies {
				fCookies[i] = &http.Cookie{
					Name:     c.Name,
					Value:    c.Value,
					Path:     c.Path,
					Domain:   c.Domain,
					Expires:  c.Expires,
					Secure:   c.Secure,
					HttpOnly: c.HttpOnly,
				}
			}
			base := &url.URL{Scheme: "https", Host: host, Path: "/"}
			jar.SetCookies(base, fCookies)
			options = append(options, tlsclient.WithCookieJar(jar))
		}
	}

	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
}

func tlsProfileFor(id string) (profiles.ClientProfile, error) {
	switch id {
	case "chrome_124":
		return profiles.Chrome_124, nil
	default:
		return profiles.ClientProfile{}, fmt.Errorf("retrieval: unknown TLS profile %q", id)
	}
}

func isSecFetchHeader(key string) bool {
	return key == "Sec-Fetch-Site" || key == "Sec-Fetch-Mode" || key == "Sec-Fetch-Dest"
}

func headerExists(headers map[string]string, key string) bool {
	if headers == nil {
		return false
	}
	_, ok := headers[key]
	return ok
}

func outcome(status ports.PageStatus, method, reason, url string) ports.PageOutcome {
	return ports.PageOutcome{Status: status, Method: method, Reason: reason, URL: url}
}
