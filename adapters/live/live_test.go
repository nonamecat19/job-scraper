//go:build live

// Package live holds the smoke tests that hit the real sites. They are behind
// the `live` build tag because they need the network, are slow, and fail for
// reasons outside this repository — a site redesign, a rate limit, an outage.
//
//	go test -tags live ./adapters/live/
package live

import (
	"context"
	"testing"
	"time"

	"github.com/nonamecat19/job-scraper/adapters/adzuna"
	"github.com/nonamecat19/job-scraper/adapters/arbeitnow"
	"github.com/nonamecat19/job-scraper/adapters/remotive"
	"github.com/nonamecat19/job-scraper/adapters/workua"
	"github.com/nonamecat19/job-scraper/internal/scraping"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

func TestLive_Remotive(t *testing.T) {
	jobs := searchLive(t, remotive.New(), "golang", 20*time.Second)
	t.Logf("remotive returned %d jobs", len(jobs))
	if len(jobs) > 0 {
		t.Logf("sample: %+v", jobs[0])
	}
}

func TestLive_Arbeitnow(t *testing.T) {
	jobs := searchLive(t, arbeitnow.New(), "engineer", 20*time.Second)
	t.Logf("arbeitnow returned %d jobs", len(jobs))
}

func TestLive_WorkUa(t *testing.T) {
	src, err := workua.New(scraping.New())
	if err != nil {
		t.Fatalf("workua.New: %v", err)
	}

	jobs := searchLive(t, src, "php", 30*time.Second)
	t.Logf("workua returned %d jobs", len(jobs))
	if len(jobs) == 0 {
		t.Error("expected at least 1 job from live search")
		return
	}
	t.Logf("sample: %+v", jobs[0])
}

// TestLive_AdzunaNoCreds needs no network: it pins the contract that a source
// missing its credentials fails loudly rather than reporting zero results,
// which would look identical to a search that legitimately found nothing.
func TestLive_AdzunaNoCreds(t *testing.T) {
	_, err := adzuna.New("", "", "").Search(context.Background(), model.SearchQuery{Keywords: "engineer"}, nil)
	if err == nil {
		t.Fatal("expected an error without credentials, got a successful search")
	}
}

func searchLive(t *testing.T, src ports.JobSource, keywords string, budget time.Duration) []model.NormalizedJob {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	jobs, err := src.Search(ctx, model.SearchQuery{Keywords: keywords}, nil)
	if err != nil {
		t.Fatalf("%s search failed: %v", src.Key(), err)
	}
	return jobs
}
