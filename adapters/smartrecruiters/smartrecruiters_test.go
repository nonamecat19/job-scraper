package smartrecruiters

import (
	"encoding/json"
	"testing"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/store/memstore"
)

func TestSmartRecruitersResponseJSON(t *testing.T) {
	raw := `{"content":[{"id":"p1","name":"Go Engineer","releasedDate":"2026-01-01","ref":"https://jobs.smartrecruiters.com/Acme/p1","location":{"city":"Remote","country":"","remote":true},"jobAd":{"sections":{"jobDescription":{"text":"full text"}}}}]}`
	var res smartRecruitersResponse
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 posting, got %d", len(res.Content))
	}
	if res.Content[0].Name != "Go Engineer" || res.Content[0].JobAd.Sections.JobDescription.Text == "" {
		t.Errorf("unexpected fields: %+v", res.Content[0])
	}
}

// TestFetcherVendor pins the key the roster is queried by. Changing it orphans
// every board row a consumer has already stored under the old name.
func TestFetcherVendor(t *testing.T) {
	if got := (Fetcher{}).Vendor(); got != Key {
		t.Errorf("Vendor() = %q, want %q", got, Key)
	}
	if Key != "smartrecruiters" {
		t.Errorf("Key = %q, want %q", Key, "smartrecruiters")
	}
}

// TestSourceDoesNotNeedDetail guards the promise that this vendor's postings
// arrive complete: the API inlines each description, so no detail pass follows,
// and advertising DetailNeeder would make the caller run one for nothing.
func TestSourceDoesNotNeedDetail(t *testing.T) {
	src, err := New(memstore.NewRoster())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if adapter.NeedsDetail(src) {
		t.Error("source reports NeedsDetail, but Search returns full descriptions")
	}
}

// TestNewRequiresRoster confirms a missing roster is refused at construction.
// A vendor source with no boards to walk would otherwise look like a healthy
// source that simply found nothing.
func TestNewRequiresRoster(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("New(nil) succeeded, want an error naming the missing roster")
	}
}
