package manual

import (
	"context"
	"testing"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/model"
)

func TestManualAdapterHasNoPostingReader(t *testing.T) {
	if _, ok := adapter.AsPostingReader(Source{}); ok {
		t.Fatal("the manual source reads nothing — it must not claim any host")
	}
}

func TestManualAdapterSearchFailsLoudly(t *testing.T) {
	jobs, err := (Source{}).Search(context.Background(), model.SearchQuery{}, nil)
	if err == nil {
		t.Fatal("expected the manual source to refuse being crawled rather than return an empty result")
	}
	if jobs != nil {
		t.Errorf("expected no jobs, got %d", len(jobs))
	}
	healthy, err := (Source{}).HealthCheck(context.Background(), nil)
	if err != nil || !healthy {
		t.Errorf("expected the manual source to report healthy, got healthy=%v err=%v", healthy, err)
	}
}
