package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nonamecat19/jobscraper/ports"
)

// FlareSolverrRung delegates the fetch to a FlareSolverr sidecar, which solves
// the challenge in its own managed browser and returns the page behind it. It
// is the costliest strategy — a network hop plus a full browser session on
// another host — so it sits at the top of the ladder.
type FlareSolverrRung struct {
	baseURL  string
	client   ports.HTTPDoer
	detector ports.ChallengeDetector

	// MaxTimeout is how long the sidecar may spend solving one challenge.
	// Zero selects the default.
	MaxTimeout time.Duration
	// HealthProbeURL is the page fetched to decide Available. Zero selects the
	// default.
	HealthProbeURL string
}

var _ Rung = (*FlareSolverrRung)(nil)

const (
	defaultFlareTimeout    = 60 * time.Second
	defaultFlareHTTPBudget = 90 * time.Second
	defaultHealthProbeURL  = "https://httpbin.org/get"
)

// NewFlareSolverrRung points the rung at a running sidecar. An empty baseURL
// yields a rung that is never Available, so the caller may build it
// unconditionally and let the ladder skip it.
func NewFlareSolverrRung(baseURL string, detector ports.ChallengeDetector) *FlareSolverrRung {
	if detector == nil {
		detector = DefaultDetector
	}
	return &FlareSolverrRung{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: defaultFlareHTTPBudget},
		detector: detector,
	}
}

func (f *FlareSolverrRung) Key() string { return KeyFlareSolverr }

func (f *FlareSolverrRung) Close() error { return nil }

type flsRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type flsResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL      string `json:"url"`
		Status   int    `json:"status"`
		Response string `json:"response"`
		Headers  map[string]string
	} `json:"solution"`
}

func (f *FlareSolverrRung) Fetch(ctx context.Context, req ports.FetchRequest) (ports.PageOutcome, string) {
	body, statusCode, err := f.solve(ctx, req.URL)
	if err != nil {
		reason := err.Error()
		if ctx.Err() != nil {
			reason = "context cancelled"
		}
		return outcome(ports.PageChallenged, KeyFlareSolverr, reason, req.URL), body
	}
	if statusCode >= 500 {
		return outcome(ports.PageChallenged, KeyFlareSolverr, fmt.Sprintf("server error (status %d)", statusCode), req.URL), body
	}
	if f.detector.IsChallenged(body, statusCode) {
		return outcome(ports.PageChallenged, KeyFlareSolverr, "challenge detected", req.URL), body
	}
	if f.detector.IsRefused(body, statusCode) {
		return outcome(ports.PageRefused, KeyFlareSolverr, fmt.Sprintf("refused (status %d)", statusCode), req.URL), body
	}
	return outcome(ports.PageRead, KeyFlareSolverr, "", req.URL), body
}

func (f *FlareSolverrRung) solve(ctx context.Context, url string) (string, int, error) {
	reqBody, err := json.Marshal(flsRequest{
		Cmd:        "request.get",
		URL:        url,
		MaxTimeout: int(orDuration(f.MaxTimeout, defaultFlareTimeout).Milliseconds()),
	})
	if err != nil {
		return "", 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/v1", bytes.NewReader(reqBody))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := f.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("flaresolverr: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", 0, err
	}

	var parsed flsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, fmt.Errorf("flaresolverr: parse response: %w", err)
	}
	if parsed.Status != "ok" {
		return "", 0, fmt.Errorf("flaresolverr: %s: %s", parsed.Status, parsed.Message)
	}

	return parsed.Solution.Response, parsed.Solution.Status, nil
}

// Available probes the sidecar with a cheap fetch. It is called before every
// escalation into this rung, so a sidecar that has gone away costs one short
// timeout rather than a hung run.
func (f *FlareSolverrRung) Available(ctx context.Context) bool {
	if f == nil || f.baseURL == "" {
		return false
	}
	probeURL := f.HealthProbeURL
	if probeURL == "" {
		probeURL = defaultHealthProbeURL
	}
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, _, err := f.solve(healthCtx, probeURL)
	if err != nil {
		slog.Warn("retrieval: flaresolverr health check failed", "url", f.baseURL, "error", err)
		return false
	}
	return body != ""
}
