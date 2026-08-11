package ports

import (
	"context"
	"net/http"
)

// HTTPDoer is the narrowest transport port — the one method *http.Client
// already satisfies. Take this rather than *http.Client wherever only a single
// round trip is needed, so tests can substitute a stub without a server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Scraper is the plain page-fetching port used by sources that do not need the
// retrieval ladder: an ordinary client, an optional real browser, and a way to
// release both.
//
// Prefer Retriever for hosts that push back. Scraper is for the cooperative
// ones, where a ladder would be overhead.
type Scraper interface {
	// HTTPClient returns the shared client, so a source can issue requests the
	// three convenience methods do not cover.
	HTTPClient() *http.Client

	// FetchHTML performs a GET and returns the body as a string.
	FetchHTML(ctx context.Context, url string, headers map[string]string) (string, error)

	// BrowserContext returns a live browser context, launching one on first
	// use. Sources that need JavaScript execution use it; the rest never call
	// it, and no browser is started on their behalf.
	BrowserContext(ctx context.Context) (context.Context, error)

	// Close releases the browser and any pooled connections.
	Close()
}
