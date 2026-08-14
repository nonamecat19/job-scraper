package euremotejobs

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/job-scraper/model"
)

// euremotejobsDetailFixtureHTML mirrors the schema.org JobPosting block a
// posting page embeds — captured (and trimmed) from a live page on
// 2026-08-14. The description carries doubly-escaped entities the way the
// site itself emits them: the JSON string holds HTML-escaped HTML.
const euremotejobsDetailFixtureHTML = `
<html><head>
<meta name="description" content="Sr Account Manager, AMER at Customer.io">
<meta property="og:image" content="https://euremotejobs.com/og.png">
<script type="application/ld+json">{"@context":"http://schema.org/","@type":"BreadcrumbList","itemListElement":[]}</script>
<script type="application/ld+json">
{
  "@context": "http://schema.org/",
  "@type": "JobPosting",
  "datePosted": "2026-08-14T12:19:05+02:00",
  "validThrough": "2026-09-14T12:19:05+02:00",
  "title": "Sr Account Manager, AMER",
  "description": "&lt;p&gt;About Customer.io. We need 3+ years of experience. English level B2 required.&lt;/p&gt;",
  "employmentType": ["FULL_TIME"],
  "hiringOrganization": {"@type": "Organization", "name": "Customer.io", "url": "https://customer.io", "logo": "https://euremotejobs.com/wp-content/uploads/logo.png"},
  "jobLocation": {"@type": "Place", "address": "US"},
  "directApply": true,
  "baseSalary": {"@type": "MonetaryAmount", "currency": "EUR", "value": {"@type": "QuantitativeValue", "value": "$151,700 - $212,000 USD OTE", "unitText": "YEAR"}},
  "industry": "Sales"
}
</script>
</head>
<body>
<div class="page-header">
	<h1 class="page-title">Sr Account Manager, AMER</h1>
</div>
</body></html>
`

func TestParseJobDetail(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(euremotejobsDetailFixtureHTML))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	detail := ParseJobDetail(doc, "https://euremotejobs.com/job/sr-account-manager-amer/")

	if detail.SourceKey != Key {
		t.Errorf("SourceKey: got %q", detail.SourceKey)
	}
	if detail.ExternalID != "sr-account-manager-amer" {
		t.Errorf("ExternalID: got %q", detail.ExternalID)
	}
	if detail.Title != "Sr Account Manager, AMER" {
		t.Errorf("Title: got %q", detail.Title)
	}
	if detail.Company.Name != "Customer.io" {
		t.Errorf("Company.Name: got %q", detail.Company.Name)
	}
	if detail.Company.URL != "https://customer.io" {
		t.Errorf("Company.URL: got %q", detail.Company.URL)
	}
	if detail.Category != "Sales" {
		t.Errorf("Category: got %q", detail.Category)
	}
	if detail.Employment != "FULL_TIME" {
		t.Errorf("Employment: got %q", detail.Employment)
	}
	if !detail.DirectApply {
		t.Errorf("DirectApply: got false, want true")
	}
	if detail.Location.Scope != model.LocationCountries || len(detail.Location.Countries) != 1 || detail.Location.Countries[0] != "US" {
		t.Errorf("Location: got scope=%v countries=%v", detail.Location.Scope, detail.Location.Countries)
	}
	if !detail.Remote() {
		t.Errorf("Remote: got false, want true — the whole board is remote-only")
	}
	if detail.Salary.Raw != "$151,700 - $212,000 USD OTE" {
		t.Errorf("Salary.Raw: got %q", detail.Salary.Raw)
	}
	if detail.Salary.Currency != "EUR" {
		t.Errorf("Salary.Currency: got %q", detail.Salary.Currency)
	}
	if detail.Timeline.PostedAt == 0 {
		t.Errorf("Timeline.PostedAt: got 0")
	}
	if detail.Timeline.ValidThru == 0 {
		t.Errorf("Timeline.ValidThru: got 0")
	}
	if detail.Timeline.ScrapedAt == 0 {
		t.Errorf("Timeline.ScrapedAt: got 0")
	}
	// The description must be unescaped exactly once: the site's JSON string
	// holds HTML-escaped HTML, not double-escaped HTML.
	if !strings.Contains(detail.Content.DescriptionHTML, "<p>About Customer.io") {
		t.Errorf("DescriptionHTML not unescaped correctly: %q", detail.Content.DescriptionHTML)
	}
	if strings.Contains(detail.Content.Description, "<p>") {
		t.Errorf("Description should be plain text, got tags: %q", detail.Content.Description)
	}
	if detail.Requirements.ExperienceYears != 3 {
		t.Errorf("Requirements.ExperienceYears: got %v", detail.Requirements.ExperienceYears)
	}
	if lang, ok := detail.Requirements.Language("English"); !ok || lang.Level != model.LevelB2 {
		t.Errorf("English requirement: got %v, ok=%v", lang, ok)
	}
}

func TestMatchesPostingURL(t *testing.T) {
	src := Source{}
	tests := []struct {
		url  string
		want bool
	}{
		{"https://euremotejobs.com/job/sr-account-manager-amer/", true},
		{"https://www.euremotejobs.com/job/sr-account-manager-amer/", true},
		{"https://euremotejobs.com/job/sr-account-manager-amer", true},
		{"https://euremotejobs.com/job-listings/", false},
		{"https://euremotejobs.com/job-category/sales/", false},
		{"https://euremotejobs.com/", false},
		{"https://otherboard.com/job/foo/", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := src.MatchesPostingURL(tt.url); got != tt.want {
			t.Errorf("MatchesPostingURL(%q): got %v, want %v", tt.url, got, tt.want)
		}
	}
}
