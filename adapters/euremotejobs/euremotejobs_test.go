package euremotejobs

import (
	"testing"

	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
)

// euremotejobsListingFixtureHTML mirrors the <li class="job_listing"> markup
// the theme's job_manager_get_listings AJAX action renders — captured from a
// live response on 2026-08-14.
const euremotejobsListingFixtureHTML = `
<li id="job_listing-47831" class="job_listing job-type-full-time job_position_featured post-47831 job_listing_region-us">
	<a class="job_listing-clickbox" href="https://euremotejobs.com/job/sr-account-manager-amer/?utm_source=feed"></a>
	<div class="job_listing-logo"><img class="company_logo" src="https://euremotejobs.com/logo.png"></div>
	<div class="job_listing-about">
		<div class="job_listing-position job_listing__column">
			<h3 class="job_listing-title">Sr Account Manager, AMER</h3>
			<div class="job_listing-company">
				<strong>Customer.io</strong>
				<span class="job_listing-company-tagline">Automated messaging platform</span>
			</div>
		</div>
		<div class="job_listing-location job_listing__column">
			<a class="google_map_link">US</a>
		</div>
		<ul class="job_listing-meta job_listing__column">
			<li class="job_listing-type job-type full-time">Full Time</li>
			<li class="job_listing-type job-type high-salary">high salary</li>
			<li class="job_listing-date">Posted 2 hours ago</li>
		</ul>
	</div>
</li>
<li id="job_listing-47700" class="job_listing job-type-contract">
	<a class="job_listing-clickbox" href="/job/senior-data-engineer/"></a>
	<div class="job_listing-about">
		<div class="job_listing-position job_listing__column">
			<h3 class="job_listing-title">Senior Data Engineer</h3>
			<div class="job_listing-company"><strong>Lemon.io</strong></div>
		</div>
		<div class="job_listing-location job_listing__column">
			<a class="google_map_link">Europe, Canada, US</a>
		</div>
		<ul class="job_listing-meta job_listing__column">
			<li class="job_listing-type job-type contract">Contract</li>
			<li class="job_listing-date">Posted 51 minutes ago</li>
		</ul>
	</div>
</li>
`

func TestParseListingsHTML(t *testing.T) {
	jobs := parseListingsHTML(euremotejobsListingFixtureHTML)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	first := jobs[0]
	if first.SourceKey != Key {
		t.Errorf("SourceKey: got %q", first.SourceKey)
	}
	if first.ExternalID == nil || *first.ExternalID != "47831" {
		t.Errorf("ExternalID: got %v", first.ExternalID)
	}
	if first.Title != "Sr Account Manager, AMER" {
		t.Errorf("Title: got %q", first.Title)
	}
	if first.Company != "Customer.io" {
		t.Errorf("Company: got %q", first.Company)
	}
	if first.Location == nil || *first.Location != "US" {
		t.Errorf("Location: got %v", first.Location)
	}
	if !first.Remote {
		t.Errorf("Remote: got false, want true (whole board is remote)")
	}
	// The tracking query string must not survive into the stored URL.
	if first.URL != "https://euremotejobs.com/job/sr-account-manager-amer/" {
		t.Errorf("URL: got %q", first.URL)
	}
	if first.PostedAt == nil {
		t.Errorf("PostedAt: got nil, want a value parsed from 'Posted 2 hours ago'")
	}

	second := jobs[1]
	if second.URL != "https://euremotejobs.com/job/senior-data-engineer/" {
		t.Errorf("second URL not resolved against the site: got %q", second.URL)
	}
	if second.Company != "Lemon.io" {
		t.Errorf("second Company: got %q", second.Company)
	}
}

func TestEuremotejobsKey(t *testing.T) {
	if (Source{}).Key() != "euremotejobs" {
		t.Errorf("expected key 'euremotejobs', got %q", (Source{}).Key())
	}
}

func TestEuremotejobsKind(t *testing.T) {
	if (Source{}).Kind() != model.SourceKindScrape {
		t.Errorf("expected kind %q, got %q", model.SourceKindScrape, (Source{}).Kind())
	}
}

func TestEuremotejobsNeedsDetail(t *testing.T) {
	if !(Source{}).NeedsDetail() {
		t.Errorf("expected NeedsDetail true — listing cards carry no description")
	}
}

func TestRelativePostedAt(t *testing.T) {
	tests := []string{
		"Posted 51 minutes ago",
		"Posted 2 hours ago",
		"Posted 3 weeks ago",
		"Posted 6 months ago",
	}
	for _, text := range tests {
		if got := relativePostedAt(text); got == nil {
			t.Errorf("relativePostedAt(%q): got nil", text)
		}
	}
	if got := relativePostedAt("Featured"); got != nil {
		t.Errorf("relativePostedAt(%q): got %v, want nil", "Featured", *got)
	}
}

func TestRegionID(t *testing.T) {
	tests := []struct {
		location string
		want     string
	}{
		{"Poland", "101"},
		{"poland", "101"},
		{"  Europe  ", "90"},
		{"US", "111"},
		{"United States", "111"},
		{"Nowhereland", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := regionID(tt.location); got != tt.want {
			t.Errorf("regionID(%q): got %q, want %q", tt.location, got, tt.want)
		}
	}
}

func TestEuremotejobsMaxPagesCapped(t *testing.T) {
	if euremotejobsMaxPages != 5 {
		t.Errorf("euremotejobsMaxPages: got %d, want 5", euremotejobsMaxPages)
	}
}

func TestFilterByLocation(t *testing.T) {
	jobs := []model.NormalizedJob{
		{Title: "only UK", Location: strutil.Ptr("UK")},
		{Title: "Europe, UK", Location: strutil.Ptr("Europe, UK")},
		{Title: "Worldwide, Europe, Ukraine", Location: strutil.Ptr("Worldwide, Europe, Ukraine")},
		{Title: "no location", Location: nil},
		{Title: "case differs", Location: strutil.Ptr("europe")},
	}

	got := filterByLocation(jobs, "Europe")
	if len(got) != 3 {
		t.Fatalf("expected 3 jobs naming Europe itself, got %d: %+v", len(got), got)
	}
	for _, j := range got {
		if j.Title == "only UK" {
			t.Errorf("kept a job matched only via the taxonomy's child term: %v", j)
		}
	}
}
