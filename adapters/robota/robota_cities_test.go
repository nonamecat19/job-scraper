package robota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCityNamesIn(t *testing.T) {
	kharkiv := cityNames{ID: 21, UA: "Харків", RU: "Харьков", EN: "Kharkiv"}
	tests := map[string]string{"ua": "Харків", "ru": "Харьков", "en": "Kharkiv", "": "Харьков"}
	for lang, want := range tests {
		if got := kharkiv.in(lang); got != want {
			t.Errorf("in(%q) = %q, want %q", lang, got, want)
		}
	}
}

func TestCityDictionaryLookup(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`[{"id":21,"ua":"Харків","ru":"Харьков","en":"Kharkiv"},
		                        {"id":1,"ua":"Київ","ru":"Киев","en":"Kyiv"}]`))
	}))
	defer srv.Close()

	dict := &cityDictionary{}
	dict.loadFrom(context.Background(), srv.URL+"/dictionary/city")

	if got := dict.names[21].UA; got != "Харків" {
		t.Errorf("names[21].UA = %q, want Харків", got)
	}
	if got := dict.names[1].UA; got != "Київ" {
		t.Errorf("names[1].UA = %q, want Київ", got)
	}
	if hits != 1 {
		t.Errorf("fetched %d times, want the reference data read once", hits)
	}
	// An id the dictionary does not know reports nothing rather than guessing.
	if got := dict.names[999].UA; got != "" {
		t.Errorf("names[unknown] = %q, want empty", got)
	}
}

// A dictionary that cannot be read must not fail the vacancy: the caller falls
// back to the name the payload itself carried.
func TestCityNameFallsBackWhenDictionaryUnavailable(t *testing.T) {
	dict := &cityDictionary{}
	// Consume the once with the unreachable endpoint, so name() cannot fall
	// through to the real dictionary.
	dict.once.Do(func() { dict.loadFrom(context.Background(), "http://127.0.0.1:1/dictionary/city") })

	if got := dict.name(context.Background(), 21, "ua"); got != "" {
		t.Errorf("name = %q, want empty when the dictionary is unreadable", got)
	}
	if got := cityName(context.Background(), 0, " Харьков "); got != "Харьков" {
		t.Errorf("cityName(no id) = %q, want the payload's own name trimmed", got)
	}
}
