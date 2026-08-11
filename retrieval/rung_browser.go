package retrieval

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nonamecat19/job-scraper/ports"
)

// BrowserRung drives a real headless Chrome. It is the middle strategy: it
// costs a process and several seconds per page, but it executes the JavaScript
// that many interstitials require before they hand over the real page.
type BrowserRung struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	detector    ports.ChallengeDetector

	// PageTimeout bounds one page load. Zero selects the default.
	PageTimeout time.Duration
	// SettleDelay is how long to wait after the document is ready, giving
	// client-side rendering time to fill the page in. Zero selects the default.
	SettleDelay time.Duration
}

var _ Rung = (*BrowserRung)(nil)

const (
	defaultPageTimeout = 45 * time.Second
	defaultSettleDelay = 2 * time.Second
)

// NewBrowserRung launches the browser and returns the rung. It errors when no
// browser could be started — a machine without Chrome installed, a container
// without the shared memory to run it — and the caller drops the rung from the
// ladder rather than failing the whole engine.
func NewBrowserRung(detector ports.ChallengeDetector) (*BrowserRung, error) {
	if detector == nil {
		detector = DefaultDetector
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.UserDataDir("/tmp/retrieval-browser"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, fmt.Errorf("retrieval: launch browser rung: %w", err)
	}

	return &BrowserRung{allocCtx: allocCtx, allocCancel: allocCancel, detector: detector}, nil
}

func (b *BrowserRung) Key() string { return KeyBrowser }

func (b *BrowserRung) Available(context.Context) bool { return b != nil && b.allocCtx != nil }

func (b *BrowserRung) Fetch(ctx context.Context, req ports.FetchRequest) (ports.PageOutcome, string) {
	body, err := b.load(ctx, req.URL)
	if err != nil {
		reason := err.Error()
		if ctx.Err() != nil {
			reason = "context cancelled"
		}
		return outcome(ports.PageChallenged, KeyBrowser, reason, req.URL), body
	}
	// A browser load has no status code to inspect, so 200 stands in and only
	// the body markers decide.
	if b.detector.IsChallenged(body, 200) {
		return outcome(ports.PageChallenged, KeyBrowser, "challenge detected", req.URL), body
	}
	return outcome(ports.PageRead, KeyBrowser, "", req.URL), body
}

func (b *BrowserRung) load(ctx context.Context, url string) (string, error) {
	tabCtx, tabCancel := chromedp.NewContext(b.allocCtx)
	defer tabCancel()

	deadlineCtx, cancel := context.WithTimeout(tabCtx, orDuration(b.PageTimeout, defaultPageTimeout))
	defer cancel()

	var html string
	err := chromedp.Run(deadlineCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Sleep(orDuration(b.SettleDelay, defaultSettleDelay)),
		chromedp.OuterHTML("html", &html),
	)
	if err != nil {
		return "", fmt.Errorf("browser: fetch %s: %w", url, err)
	}
	return html, nil
}

func (b *BrowserRung) Close() error {
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
		b.allocCtx = nil
	}
	return nil
}

func orDuration(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
