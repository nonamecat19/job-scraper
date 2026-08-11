//go:build integration

package atsboard_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/adapters/ashby"
	"github.com/nonamecat19/jobscraper/adapters/atsboard"
	"github.com/nonamecat19/jobscraper/adapters/greenhouse"
	"github.com/nonamecat19/jobscraper/adapters/lever"
	"github.com/nonamecat19/jobscraper/adapters/smartrecruiters"
	"github.com/nonamecat19/jobscraper/adapters/workable"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

// Every vendor's mock board serves the same four shapes, so one table can
// assert the same behaviour across all five: a board with a posting, an empty
// board, a board that 404s, and a board returning unparseable JSON.
type vendorCase struct {
	name       string
	fetcher    atsboard.Fetcher
	prefix     string // employer-identifier prefix used by this vendor's fixtures
	company    string // DisplayName of the board that has a posting
	title      string // title that posting should carry
	wantRemote bool
}

var vendorCases = []vendorCase{
	{"greenhouse", greenhouse.Fetcher{}, "gh", "GH Test Corp", "Go Engineer", true},
	{"lever", lever.Fetcher{}, "lv", "LV Test Corp", "Backend Engineer", false},
	{"ashby", ashby.Fetcher{}, "as", "AS Test Corp", "Senior Go Developer", true},
	{"workable", workable.Fetcher{}, "wk", "WK Test Corp", "Platform Engineer", true},
	{"smartrecruiters", smartrecruiters.Fetcher{}, "sr", "SR Test Corp", "Cloud Architect", true},
}

func atsBoardMux() *http.ServeMux {
	mux := http.NewServeMux()

	// serve routes one vendor's path prefix, extracting the employer
	// identifier and answering with whichever fixture its suffix selects.
	serve := func(pathPrefix string, empty, one string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimPrefix(r.URL.Path, pathPrefix)
			id, _, _ = strings.Cut(id, "/")
			id, _, _ = strings.Cut(id, "?")

			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.HasSuffix(id, "-test"):
				_, _ = w.Write([]byte(one))
			case strings.HasSuffix(id, "-404"):
				w.WriteHeader(http.StatusNotFound)
			case strings.HasSuffix(id, "-malformed"):
				_, _ = w.Write([]byte(`not json`))
			default:
				_, _ = w.Write([]byte(empty))
			}
		}
	}

	mux.HandleFunc("/v1/boards/", serve("/v1/boards/", `{"jobs":[]}`,
		`{"jobs":[{"id":1,"title":"Go Engineer","absolute_url":"https://boards.greenhouse.io/gh-test/jobs/1","content":"<p>Greenhouse full job description</p>","updated_at":"2026-01-01T00:00:00Z","location":{"name":"Remote, US"}}]}`))

	mux.HandleFunc("/v0/postings/", serve("/v0/postings/", `[]`,
		`[{"id":"abc123","text":"Backend Engineer","hostedUrl":"https://jobs.lever.co/lv-test/abc123","createdAt":1700000000000,"categories":{"location":"New York, NY","commitment":"Full-time"},"descriptionPlain":"Lever full job description"}]`))

	mux.HandleFunc("/posting-api/job-board/", serve("/posting-api/job-board/", `{"jobs":[]}`,
		`{"jobs":[{"id":"j1","title":"Senior Go Developer","location":"Remote","isRemote":true,"jobUrl":"https://jobs.ashbyhq.com/as-test/j1","descriptionHtml":"<p>Ashby full job description</p>","publishedAt":"2026-01-01T00:00:00Z"}]}`))

	mux.HandleFunc("/api/v1/widget/accounts/", serve("/api/v1/widget/accounts/", `{"jobs":[]}`,
		`{"jobs":[{"shortcode":"ABC123","title":"Platform Engineer","description":"Workable full job description","url":"https://apply.workable.com/wk-test/j/ABC123","remote":true,"created_at":"2026-01-01","location":{"city":"San Francisco","country_name":"US"}}]}`))

	mux.HandleFunc("/v1/companies/", serve("/v1/companies/", `{"content":[]}`,
		`{"content":[{"id":"p1","name":"Cloud Architect","releasedDate":"2026-01-01","ref":"https://jobs.smartrecruiters.com/SRTest/p1","location":{"city":"Chicago","country":"US","remote":true},"jobAd":{"sections":{"jobDescription":{"text":"SmartRecruiters full job description"}}}}]}`))

	return mux
}

// rewriteTransport points every outbound request at the test server, whatever
// host the source asked for. It is how real vendor URLs are exercised without
// touching the network.
type rewriteTransport struct {
	base *url.URL
	rt   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.base.Scheme
	u.Host = t.base.Host
	clone.URL = &u
	clone.Host = ""
	return t.rt.RoundTrip(clone)
}

// setup starts the mock ATS, redirects the shared JSON client at it for the
// duration of the test, and seeds a roster with this vendor's four boards.
func setup(t *testing.T, vc vendorCase) (context.Context, *memstore.Roster, ports.JobSource) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	ts := httptest.NewServer(atsBoardMux())
	t.Cleanup(ts.Close)

	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	orig := httpjson.DefaultClient()
	httpjson.SetDefaultClient(&http.Client{
		Transport: rewriteTransport{base: base, rt: ts.Client().Transport},
		Timeout:   30 * time.Second,
	})
	t.Cleanup(func() { httpjson.SetDefaultClient(orig) })

	roster := memstore.NewRoster()
	vendor := vc.fetcher.Vendor()

	// DisplayName is what the assertions match on, so the healthy board carries
	// the case's expected company name and the three failing boards carry names
	// the assertions can recognise as "should contribute nothing".
	names := map[string]string{
		"empty":     strings.ToUpper(vc.prefix) + " Empty Corp",
		"404":       strings.ToUpper(vc.prefix) + " 404 Corp",
		"malformed": strings.ToUpper(vc.prefix) + " Malformed Corp",
		"test":      vc.company,
	}
	for suffix, name := range names {
		roster.Seed(ports.EmployerBoard{
			Vendor:             vendor,
			EmployerIdentifier: vc.prefix + "-" + suffix,
			DisplayName:        name,
			AddedVia:           "test",
			Enabled:            true,
		})
	}

	src, err := atsboard.New(vc.fetcher, roster)
	if err != nil {
		t.Fatalf("atsboard.New(%s): %v", vendor, err)
	}
	return ctx, roster, src
}

// TestSearchReadsBoards runs the same expectations against every vendor: the
// posting from the healthy board arrives complete, and none of the empty, 404
// or malformed boards contributes anything.
func TestSearchReadsBoards(t *testing.T) {
	for _, vc := range vendorCases {
		t.Run(vc.name, func(t *testing.T) {
			ctx, _, src := setup(t, vc)

			jobs, err := src.Search(ctx, model.SearchQuery{}, nil)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			var found *model.NormalizedJob
			for i, j := range jobs {
				if j.Company == vc.company {
					found = &jobs[i]
				}
				if strings.Contains(j.Company, "Empty") || strings.Contains(j.Company, "404") || strings.Contains(j.Company, "Malformed") {
					t.Errorf("board %q contributed a posting, want none", j.Company)
				}
			}
			if found == nil {
				t.Fatalf("no posting from %s; got %d jobs", vc.company, len(jobs))
			}

			if found.Title != vc.title {
				t.Errorf("Title = %q, want %q", found.Title, vc.title)
			}
			if found.URL == "" {
				t.Error("URL is empty")
			}
			// An ATS API inlines the description, which is the whole reason
			// these sources skip a detail pass.
			if found.Description == "" {
				t.Error("Description is empty, want the inlined posting body")
			}
			if found.SourceKey != vc.fetcher.Vendor() {
				t.Errorf("SourceKey = %q, want %q", found.SourceKey, vc.fetcher.Vendor())
			}
			if found.Remote != vc.wantRemote {
				t.Errorf("Remote = %v, want %v", found.Remote, vc.wantRemote)
			}
		})
	}
}

// TestSearchIsRepeatable confirms a second run returns the same postings rather
// than accumulating or deduplicating them: sources are stateless readers, and
// deduplication is the consumer's job.
func TestSearchIsRepeatable(t *testing.T) {
	ctx, _, src := setup(t, vendorCases[0])

	count := func() int {
		jobs, err := src.Search(ctx, model.SearchQuery{}, nil)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		n := 0
		for _, j := range jobs {
			if j.Company == vendorCases[0].company {
				n++
			}
		}
		return n
	}

	if first, second := count(), count(); first != second || first != 1 {
		t.Errorf("posting counts across runs = %d, %d; want 1, 1", first, second)
	}
}

// TestLastRunDetailClassifiesEveryBoard checks the operator-facing report: each
// of the four board shapes must be classified distinctly, because that is what
// tells "no openings" apart from "this board is gone".
func TestLastRunDetailClassifiesEveryBoard(t *testing.T) {
	ctx, _, src := setup(t, vendorCases[0])

	if _, err := src.Search(ctx, model.SearchQuery{}, nil); err != nil {
		t.Fatalf("Search: %v", err)
	}

	reporter, ok := adapter.AsEmployerReporter(src)
	if !ok {
		t.Fatal("ATS source does not implement EmployerReporter")
	}

	got := map[string]ports.EmployerOutcome{}
	for _, d := range reporter.LastRunDetail() {
		got[d.EmployerIdentifier] = d.Outcome
	}

	want := map[string]ports.EmployerOutcome{
		"gh-test":      ports.EmployerOutcomeRead,
		"gh-empty":     ports.EmployerOutcomeNoPostings,
		"gh-404":       ports.EmployerOutcomeNotFound,
		"gh-malformed": ports.EmployerOutcomeUnreadable,
	}
	for id, wantOutcome := range want {
		if got[id] != wantOutcome {
			t.Errorf("outcome for %s = %q, want %q", id, got[id], wantOutcome)
		}
	}
}

// TestHealthCheckerProbesUnrosteredBoard covers the pre-add validation path: a
// consumer checks an employer identifier before storing it, without the board
// being on the roster yet.
func TestHealthCheckerProbesUnrosteredBoard(t *testing.T) {
	ctx, _, _ := setup(t, vendorCases[0])

	count, err := greenhouse.HealthChecker()(ctx, "gh-test")
	if err != nil {
		t.Fatalf("HealthChecker: %v", err)
	}
	if count != 1 {
		t.Errorf("posting count = %d, want 1", count)
	}
}
