package workable

import (
	"encoding/json"
	"testing"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/store/memstore"
)

func TestWorkableResponseJSON(t *testing.T) {
	raw := `{"jobs":[{"shortcode":"ABC123","title":"Go Engineer","description":"full text","url":"https://apply.workable.com/acme/j/ABC123","remote":true,"created_at":"2026-01-01","location":{"city":"Remote","country_name":""}}]}`
	var res workableResponse
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(res.Jobs))
	}
	if res.Jobs[0].Title != "Go Engineer" || res.Jobs[0].Description == "" {
		t.Errorf("unexpected fields: %+v", res.Jobs[0])
	}
}

// TestFetcherVendor pins the key the roster is queried by. Changing it orphans
// every board row a consumer has already stored under the old name.
func TestFetcherVendor(t *testing.T) {
	if got := (Fetcher{}).Vendor(); got != Key {
		t.Errorf("Vendor() = %q, want %q", got, Key)
	}
	if Key != "workable" {
		t.Errorf("Key = %q, want %q", Key, "workable")
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
