package retrieval_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/nonamecat19/jobscraper/adapters/remotive"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/retrieval"
)

// recordingBase stands in for the network underneath DefaultTransport: it
// records the host of every request that reached it and answers with an empty
// JSON body. That lets a case name a source's real host — the whole point is
// that pacing keys on the host the source actually asks for — without any of
// them being contacted.
type recordingBase struct {
	mu    sync.Mutex
	hosts []string
}

func (r *recordingBase) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, req.URL.Host)
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"jobs":[]}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func (r *recordingBase) sawHost(host string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.hosts {
		if h == host {
			return true
		}
	}
	return false
}

// TestJSONHelperUsesPacedTransport pins the wiring the API sources depend on.
// They all call httpjson with a nil client, so the default client is the only
// thing standing between them and an unpaced request.
func TestJSONHelperUsesPacedTransport(t *testing.T) {
	if httpjson.DefaultClient().Transport != http.RoundTripper(retrieval.DefaultTransport) {
		t.Fatal("the JSON helper's default client does not route through retrieval.DefaultTransport")
	}
}

// TestJSONRequestsArePaced drives the JSON path and asserts each request both
// reached the transport and was rate-resolved for its own host. Every case uses
// a distinct host because the transport caches a limiter per host for five
// minutes, so a shared host would answer with the previous case's rate.
func TestJSONRequestsArePaced(t *testing.T) {
	cases := []struct {
		name       string
		host       string
		fetch      func(context.Context) error
		rps        float64
		source     string
		resolved   bool
		wantRPS    float64
		wantSource string
	}{
		{
			// A real source, called the way the fan-out calls it, to prove the
			// paced client is reached through the adapter and not only when a
			// test drives httpjson directly.
			name: "a JSON source's request is paced at the site-requested rate",
			host: "remotive.com",
			fetch: func(ctx context.Context) error {
				_, err := remotive.Source{}.Search(ctx, model.SearchQuery{Keywords: "golang"}, nil)
				return err
			},
			rps:        0.25,
			source:     "site-requested",
			resolved:   true,
			wantRPS:    0.25,
			wantSource: "site-requested",
		},
		{
			name:       "an operator override is paced at the override rate",
			host:       "api.override.test",
			fetch:      getJSON("https://api.override.test/jobs"),
			rps:        0.1,
			source:     "override",
			resolved:   true,
			wantRPS:    0.1,
			wantSource: "override",
		},
		{
			name:       "a host the resolver knows nothing about falls back to the default rate",
			host:       "api.unknown.test",
			fetch:      getJSON("https://api.unknown.test/jobs"),
			resolved:   false,
			wantRPS:    retrieval.DefaultRPS,
			wantSource: "default",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &recordingBase{}
			swapTransport(t, base, func(string) (float64, string, bool) {
				return tc.rps, tc.source, tc.resolved
			})

			if err := tc.fetch(context.Background()); err != nil {
				t.Fatalf("fetch through the paced client: %v", err)
			}

			if !base.sawHost(tc.host) {
				t.Errorf("request for %s never reached the paced transport; hosts seen: %v", tc.host, base.hosts)
			}

			rps, source := retrieval.DefaultTransport.RateFor(tc.host)
			if rps != tc.wantRPS {
				t.Errorf("RateFor(%s) rps = %v, want %v", tc.host, rps, tc.wantRPS)
			}
			if source != tc.wantSource {
				t.Errorf("RateFor(%s) source = %q, want %q", tc.host, source, tc.wantSource)
			}
		})
	}
}

func getJSON(rawURL string) func(context.Context) error {
	return func(ctx context.Context) error {
		var out map[string]any
		return httpjson.GetJSON(ctx, nil, rawURL, url.Values{}, &out)
	}
}

// swapTransport points DefaultTransport at a stub network and resolver for the
// duration of one case, restoring both afterwards so the package-level
// transport the rest of the suite shares is left as it was found.
func swapTransport(t *testing.T, base http.RoundTripper, resolver func(string) (float64, string, bool)) {
	t.Helper()
	origBase, origResolver := retrieval.DefaultTransport.Base, retrieval.DefaultTransport.RateResolver
	retrieval.DefaultTransport.Base = base
	retrieval.DefaultTransport.RateResolver = resolver
	t.Cleanup(func() {
		retrieval.DefaultTransport.Base = origBase
		retrieval.DefaultTransport.RateResolver = origResolver
	})
}
