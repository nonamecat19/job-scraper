package robota

import (
	"context"
	"testing"

	"github.com/nonamecat19/job-scraper/model"
)

func TestRobotaKey(t *testing.T) {
	a := Source{}
	if a.Key() != "robota" {
		t.Errorf("expected key 'robota', got %q", a.Key())
	}
}

func TestRobotaKind(t *testing.T) {
	a := Source{}
	if a.Kind() != model.SourceKindAPI {
		t.Errorf("expected kind %q, got %q", model.SourceKindAPI, a.Kind())
	}
}

func TestRobotaSearchRequiresKeywords(t *testing.T) {
	a := Source{}
	_, err := a.Search(context.Background(), model.SearchQuery{}, nil)
	if err == nil {
		t.Error("expected error for empty keywords")
	}
}

func TestRobotaHealthCheck(t *testing.T) {
	a := Source{}
	healthy, err := a.HealthCheck(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !healthy {
		t.Error("expected healthy to be true")
	}
}
