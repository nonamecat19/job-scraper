package lever

import (
	"encoding/json"
	"testing"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

func TestLeverResponseJSON(t *testing.T) {
	raw := `[{"id":"abc123","text":"Go Engineer","hostedUrl":"https://jobs.lever.co/acme/abc123","createdAt":1700000000000,"categories":{"location":"Remote","commitment":"Full-time"},"descriptionPlain":"full text"}]`
	var res []leverPosting
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 posting, got %d", len(res))
	}
	if res[0].Text != "Go Engineer" || res[0].DescriptionPlain == "" {
		t.Errorf("unexpected fields: %+v", res[0])
	}
}

// TestFetcherVendor pins the key the roster is queried by. Changing it orphans
// every board row a consumer has already stored under the old name.
func TestFetcherVendor(t *testing.T) {
	if got := (Fetcher{}).Vendor(); got != Key {
		t.Errorf("Vendor() = %q, want %q", got, Key)
	}
	if Key != "lever" {
		t.Errorf("Key = %q, want %q", Key, "lever")
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
