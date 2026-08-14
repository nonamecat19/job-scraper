package euremotejobs

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/job-scraper/internal/htmlutil"
	"github.com/nonamecat19/job-scraper/internal/jobdetail"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

var (
	_ ports.PostingReader   = Source{}
	_ ports.JobDetailReader = Source{}

	// A posting lives at /job/<slug>/ — the archive is /job-listings/ and a
	// taxonomy page is /job-category/<slug>/, neither of which this matches.
	euremotejobsPostingPathRe = regexp.MustCompile(`^/job/([^/]+)/?$`)
)

// MatchesPostingURL implements ports.PostingReader. It does no I/O and
// tolerates any input.
func (Source) MatchesPostingURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if !euremotejobsIsHost(parsed.Host) {
		return false
	}
	return euremotejobsPostingPathRe.MatchString(parsed.Path)
}

func euremotejobsIsHost(host string) bool {
	return strings.EqualFold(strings.TrimPrefix(strings.ToLower(host), "www."), "euremotejobs.com")
}

// ReadPosting implements ports.PostingReader by projecting the full detail, so
// one parser serves both paths.
func (a Source) ReadPosting(ctx context.Context, rawURL string, config map[string]any) (model.NormalizedJob, error) {
	detail, err := a.ReadJobDetail(ctx, rawURL, config)
	if err != nil {
		return model.NormalizedJob{}, err
	}
	return detail.Normalized(), nil
}

// ReadJobDetail implements ports.JobDetailReader.
//
// euremotejobs (Jobify / WP Job Manager) publishes a schema.org JobPosting
// block on every posting page — richer and far less brittle than parsing the
// visible markup, so that block is the primary source here. The visible page
// is consulted only for the job category, which the JSON-LD calls "industry"
// but the site itself surfaces as a distinct taxonomy.
func (a Source) ReadJobDetail(ctx context.Context, rawURL string, _ map[string]any) (model.JobDetail, error) {
	postingURL := canonicalPostingURL(rawURL)
	htmlStr, err := a.Scraping.FetchHTML(ctx, postingURL, nil)
	if err != nil {
		return model.JobDetail{}, fmt.Errorf("euremotejobs: fetch posting: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return model.JobDetail{}, fmt.Errorf("euremotejobs: parse posting: %w", err)
	}
	return ParseJobDetail(doc, postingURL), nil
}

// ldJobPosting is the schema.org JobPosting block euremotejobs embeds on every
// posting page. Fields the site renders inconsistently (an employment type
// that is sometimes a string and sometimes a list, a location that is
// sometimes a bare country string) are read through json.RawMessage and
// resolved by hand.
type ldJobPosting struct {
	Type           string          `json:"@type"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	DatePosted     string          `json:"datePosted"`
	ValidThrough   string          `json:"validThrough"`
	EmploymentType json.RawMessage `json:"employmentType"`
	HiringOrg      ldOrganization  `json:"hiringOrganization"`
	JobLocation    json.RawMessage `json:"jobLocation"`
	DirectApply    bool            `json:"directApply"`
	BaseSalary     *ldBaseSalary   `json:"baseSalary"`
	Industry       string          `json:"industry"`
}

type ldOrganization struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Logo string `json:"logo"`
}

type ldBaseSalary struct {
	Currency string `json:"currency"`
	Value    struct {
		Value    json.RawMessage `json:"value"`
		MinValue float64         `json:"minValue"`
		MaxValue float64         `json:"maxValue"`
		UnitText string          `json:"unitText"`
	} `json:"value"`
}

// ParseJobDetail extracts a euremotejobs posting page that has already been
// fetched.
func ParseJobDetail(doc *goquery.Document, sourceURL string) model.JobDetail {
	ld := findJobPostingLD(doc)

	descriptionHTML := html.UnescapeString(ld.Description)
	descriptionText := jobdetail.PlainText(htmlutil.HTMLToText(descriptionHTML))

	detail := model.JobDetail{
		SourceKey:   Key,
		ExternalID:  postingID(sourceURL),
		URL:         sourceURL,
		Title:       firstNonEmpty(ld.Title, squash(doc.Find("h1.page-title").First().Text())),
		Category:    firstNonEmpty(ld.Industry, squash(doc.Find(".job-category").First().Text())),
		Employment:  employmentType(ld.EmploymentType),
		DirectApply: ld.DirectApply,
		Company: model.Company{
			Name:    firstNonEmpty(ld.HiringOrg.Name, squash(doc.Find(".job-company a").First().Text())),
			URL:     ld.HiringOrg.URL,
			LogoURL: ld.HiringOrg.Logo,
		},
		Location: jobLocation(ld.JobLocation),
		Timeline: model.Timeline{
			PostedAt:  parseLDTime(ld.DatePosted),
			ValidThru: parseLDTime(ld.ValidThrough),
			ScrapedAt: time.Now().Unix(),
		},
		Content: model.Content{
			Description:     descriptionText,
			DescriptionHTML: strings.TrimSpace(descriptionHTML),
			MetaDescription: jobdetail.PlainText(squash(attrOf(doc.Find(`meta[name="description"]`).First(), "content"))),
			OGImage:         attrOf(doc.Find(`meta[property="og:image"]`).First(), "content"),
		},
	}

	if ld.BaseSalary != nil {
		detail.Salary = parseSalary(*ld.BaseSalary)
	}

	if level := jobdetail.LanguageLevel(descriptionText); level != model.LevelUnknown && strings.Contains(strings.ToLower(descriptionText), "english") {
		detail.Requirements.Languages = []model.LanguageReq{{Language: "English", Level: level}}
	}
	if years := jobdetail.Years(descriptionText); years > 0 {
		detail.Requirements.ExperienceYears = years
		detail.Requirements.MinimumExperienceYears = years
	}

	return detail
}

// findJobPostingLD reads the first application/ld+json block whose @type is
// JobPosting. The page also carries a BreadcrumbList block under the same tag
// name, which is why every block is tried rather than just the first.
func findJobPostingLD(doc *goquery.Document) ldJobPosting {
	var found ldJobPosting
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var probe struct {
			Type string `json:"@type"`
		}
		raw := s.Text()
		if err := json.Unmarshal([]byte(raw), &probe); err != nil || probe.Type != "JobPosting" {
			return true
		}
		_ = json.Unmarshal([]byte(raw), &found)
		return false
	})
	return found
}

// employmentType reads schema.org's employmentType, which euremotejobs
// renders as either a bare string or a one-element array depending on the
// posting.
func employmentType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return strings.Join(list, ", ")
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single
	}
	return ""
}

// jobLocation reads schema.org's jobLocation, which euremotejobs renders as a
// bare address string ({"address":"US"}) rather than a full PostalAddress.
func jobLocation(raw json.RawMessage) model.Location {
	loc := model.Location{Mode: model.WorkModeRemote, Modes: []model.WorkMode{model.WorkModeRemote}, Scope: model.LocationUnknown}
	if len(raw) == 0 {
		return loc
	}

	var place struct {
		Address json.RawMessage `json:"address"`
	}
	if err := json.Unmarshal(raw, &place); err != nil || len(place.Address) == 0 {
		return loc
	}

	var addr string
	if err := json.Unmarshal(place.Address, &addr); err == nil {
		loc.Scope, loc.Countries = jobdetail.Locations(addr)
		return loc
	}

	var structured struct {
		AddressCountry string `json:"addressCountry"`
	}
	if err := json.Unmarshal(place.Address, &structured); err == nil && structured.AddressCountry != "" {
		loc.Scope, loc.Countries = jobdetail.Locations(structured.AddressCountry)
	}
	return loc
}

// parseSalary reads baseSalary.value.value, which euremotejobs sets to the
// employer's own raw text ("$151,700 - $212,000 USD OTE") rather than a
// number — so a numeric MonetaryAmount is read where the posting has one, and
// the raw text is kept regardless as the field a caller can always display.
func parseSalary(ld ldBaseSalary) model.Salary {
	salary := model.Salary{Currency: ld.Currency, Public: true, Estimate: ld.Value.UnitText}

	var rawText string
	if err := json.Unmarshal(ld.Value.Value, &rawText); err == nil {
		salary.Raw = rawText
	}

	if ld.Value.MinValue > 0 {
		salary.Min = int(ld.Value.MinValue)
	}
	if ld.Value.MaxValue > 0 {
		salary.Max = int(ld.Value.MaxValue)
	}
	if salary.Min == 0 && salary.Max == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(string(ld.Value.Value))); err == nil {
			salary.Min, salary.Max = n, n
		}
	}
	return salary
}

// parseLDTime reads schema.org's ISO-8601 datePosted/validThrough stamp.
func parseLDTime(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func postingID(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	m := euremotejobsPostingPathRe.FindStringSubmatch(parsed.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

func squash(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

func attrOf(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
