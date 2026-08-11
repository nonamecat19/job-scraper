package dou

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
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

	// A posting lives under a company: /companies/<slug>/vacancies/<id>/.
	douPostingPathRe = regexp.MustCompile(`^/companies/([^/]+)/vacancies/(\d+)/?$`)
	douSalaryRe      = regexp.MustCompile(`\$\s?(\d[\d\s]*)\s*[–—-]\s*\$?\s?(\d[\d\s]*)|\$\s?(\d[\d\s]*)`)
	// "Львів, віддалено" — the place list doubles as the work-mode signal.
	douRemoteWordRe = regexp.MustCompile(`(?i)віддалено|remote`)
	douOfficeWordRe = regexp.MustCompile(`(?i)в офісі|office`)
	douHybridWordRe = regexp.MustCompile(`(?i)гібрид|hybrid`)
	douAbroadRe     = regexp.MustCompile(`(?i)за кордоном`)
	// DOU's body text is littered with non-breaking spaces — "5+\u00a0years" —
	// which \s does not match, so they are normalised away before matching.
	douExpRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*\+?\s*(?:years?|рок(?:и|ів)?|рік)`)
	// "English level minimum B2": the level trails the word by a few tokens.
	douEnglishRe = regexp.MustCompile(`(?i)(?:English|англійськ\p{L}*)[^.\n]{0,40}?\b([A-C][12])\b`)
)

// douTZ is the wall clock DOU prints its dates in.
func douTZ() *time.Location { return jobdetail.Location("Europe/Kyiv") }

// MatchesPostingURL implements ports.PostingReader. It does no I/O and
// tolerates any input, because it is called for every registered source on
// every read.
//
// It claims only vacancy pages: a company page or a search listing on the same
// host is a DOU URL that is not a posting.
func (Source) MatchesPostingURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if !douIsHost(parsed.Host) {
		return false
	}
	return douPostingPathRe.MatchString(parsed.Path)
}

func douIsHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	return host == "jobs.dou.ua" || host == "dou.ua"
}

// ReadPosting implements ports.PostingReader by projecting the full detail, so
// one parser serves both paths.
func (d Source) ReadPosting(ctx context.Context, rawURL string, config map[string]any) (model.NormalizedJob, error) {
	detail, err := d.ReadJobDetail(ctx, rawURL, config)
	if err != nil {
		return model.NormalizedJob{}, err
	}
	return detail.Normalized(), nil
}

// ReadJobDetail implements ports.JobDetailReader.
//
// DOU publishes no JSON-LD JobPosting and no structured sidebar: everything
// comes off the visible page, which is why several fields a board like Djinni
// fills stay empty here. That is the point of the shared shape — the caller
// reads the same struct and sees which parts this site does not state.
func (d Source) ReadJobDetail(ctx context.Context, rawURL string, _ map[string]any) (model.JobDetail, error) {
	html, err := d.Scraping.FetchHTML(ctx, canonicalPostingURL(rawURL), nil)
	if err != nil {
		return model.JobDetail{}, fmt.Errorf("dou: fetch posting: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return model.JobDetail{}, fmt.Errorf("dou: parse posting: %w", err)
	}
	return ParseJobDetail(doc, rawURL), nil
}

// ParseJobDetail extracts a DOU vacancy page that has already been fetched.
func ParseJobDetail(doc *goquery.Document, sourceURL string) model.JobDetail {
	vacancy := doc.Find(`.l-vacancy`).First()
	places := squash(vacancy.Find(`.sh-info .place`).First().Text())
	body := vacancy.Find(`.b-typo.vacancy-section`).First()
	if body.Length() == 0 {
		body = doc.Find(`.b-typo`).First()
	}
	bodyText := jobdetail.PlainText(htmlutil.SelectionText(body))
	bodyHTML, _ := body.Html()

	detail := model.JobDetail{
		SourceKey:  Key,
		ExternalID: postingID(sourceURL),
		URL:        canonicalPostingURL(sourceURL),
		Title: firstNonEmpty(
			squash(vacancy.Find("h1").First().Text()),
			attrOf(doc.Find(`meta[property="og:title"]`).First(), "content"),
		),
		Category: squash(doc.Find(`.breadcrumbs a[href*="category="]`).First().Text()),
		Company: model.Company{
			Name:    companyName(doc, sourceURL),
			URL:     companyURL(sourceURL),
			LogoURL: attrOf(doc.Find(`meta[property="og:image"]`).First(), "content"),
		},
		Location: parsePlaces(places),
		Timeline: model.Timeline{
			PostedAt: parseDate(squash(vacancy.Find(`.date`).First().Text())),
			// The activity figures beside it are only true as of the read.
			ScrapedAt: time.Now().Unix(),
		},
		Content: model.Content{
			Description:     bodyText,
			DescriptionHTML: strings.TrimSpace(bodyHTML),
			MetaDescription: jobdetail.PlainText(squash(attrOf(doc.Find(`meta[name="description"]`).First(), "content"))),
			OGImage:         attrOf(doc.Find(`meta[property="og:image"]`).First(), "content"),
		},
	}

	// The salary sits in the header where the employer published one, and only
	// then — DOU estimates nothing, so an absent figure means absent.
	if salary := squash(vacancy.Find(`.salary`).First().Text()); salary != "" {
		detail.Salary = parseSalary(salary)
	}

	// Requirements are prose here, not fields. Only what states itself
	// unambiguously is lifted; the rest stays in Description.
	if m := douExpRe.FindStringSubmatch(bodyText); m != nil {
		detail.Requirements.ExperienceYears = jobdetail.Years(m[0])
		detail.Requirements.MinimumExperienceYears = detail.Requirements.ExperienceYears
	}
	if level := englishLevel(bodyText); level != model.LevelUnknown {
		detail.Requirements.Languages = []model.LanguageReq{{Language: "English", Level: level}}
	}
	return detail
}

// parsePlaces reads DOU's one-line place list, which doubles as the work-mode
// signal: "Львів, віддалено" is an office in Lviv and remote both.
func parsePlaces(places string) model.Location {
	loc := model.Location{Mode: model.WorkModeUnknown, Scope: model.LocationUnknown}
	if places == "" {
		return loc
	}

	var cities []string
	for _, part := range jobdetail.SplitList(places) {
		switch {
		case douHybridWordRe.MatchString(part):
			loc.Modes = appendMode(loc.Modes, model.WorkModeHybrid)
		case douRemoteWordRe.MatchString(part):
			loc.Modes = appendMode(loc.Modes, model.WorkModeRemote)
		case douOfficeWordRe.MatchString(part):
			loc.Modes = appendMode(loc.Modes, model.WorkModeOffice)
		case douAbroadRe.MatchString(part):
			// "за кордоном" widens where candidates may be, not how they work.
			cities = append(cities, part)
		default:
			// A bare city name means an office in that city.
			cities = append(cities, part)
			loc.Modes = appendMode(loc.Modes, model.WorkModeOffice)
		}
	}

	if len(loc.Modes) > 0 {
		loc.Mode = loc.Modes[0]
	}
	if len(cities) > 0 {
		loc.Scope, loc.Countries = model.LocationCountries, cities
		loc.Offices = cities
	} else if loc.Mode == model.WorkModeRemote {
		// Remote with no city named: DOU states no country restriction.
		loc.Scope = model.LocationWorldwide
	}
	return loc
}

func appendMode(modes []model.WorkMode, mode model.WorkMode) []model.WorkMode {
	for _, m := range modes {
		if m == mode {
			return modes
		}
	}
	return append(modes, mode)
}

// parseSalary reads "$3500–4500" or "$3500", which is the employer's own
// figure: DOU publishes no estimate of its own.
func parseSalary(raw string) model.Salary {
	salary := model.Salary{Raw: raw, Currency: "USD", Public: true}
	m := douSalaryRe.FindStringSubmatch(raw)
	if m == nil {
		return model.Salary{Raw: raw, Public: true}
	}
	if m[3] != "" {
		salary.Min = jobdetail.Count(m[3])
		return salary
	}
	salary.Min, salary.Max = jobdetail.Count(m[1]), jobdetail.Count(m[2])
	return salary
}

func englishLevel(text string) model.LanguageLevel {
	m := douEnglishRe.FindStringSubmatch(text)
	if m == nil {
		return model.LevelUnknown
	}
	return model.LanguageLevel(strings.ToUpper(m[1]))
}

// parseDate reads DOU's Ukrainian date line, "11 серпня 2026".
func parseDate(raw string) int64 {
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return 0
	}
	month, ok := ukrainianMonths[strings.ToLower(fields[1])]
	if !ok {
		return 0
	}
	return jobdetail.TimeIn(douTZ(), fmt.Sprintf("%s %02d %s", fields[0], month, fields[2]), "2 01 2006")
}

// ukrainianMonths is in the genitive case DOU prints: "11 серпня".
var ukrainianMonths = map[string]int{
	"січня": 1, "лютого": 2, "березня": 3, "квітня": 4, "травня": 5, "червня": 6,
	"липня": 7, "серпня": 8, "вересня": 9, "жовтня": 10, "листопада": 11, "грудня": 12,
}

func companyName(doc *goquery.Document, sourceURL string) string {
	if name := squash(doc.Find(`.b-compinfo .l-n a, .b-compinfo a`).First().Text()); name != "" {
		return name
	}
	// The title is "<position> в <company>, <places> | DOU".
	title := squash(doc.Find("title").First().Text())
	if _, after, found := strings.Cut(title, " в "); found {
		if before, _, ok := strings.Cut(after, ","); ok {
			return strings.TrimSpace(before)
		}
	}
	return companySlug(sourceURL)
}

func companySlug(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	m := douPostingPathRe.FindStringSubmatch(parsed.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

func companyURL(rawURL string) string {
	if slug := companySlug(rawURL); slug != "" {
		return "https://jobs.dou.ua/companies/" + slug + "/"
	}
	return ""
}

func postingID(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	m := douPostingPathRe.FindStringSubmatch(parsed.Path)
	if m == nil {
		return ""
	}
	return m[2]
}

// canonicalPostingURL drops the query and fragment so the same posting reached
// through a tracking link produces the same dedupe key as a crawl.
func canonicalPostingURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return rawURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
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
