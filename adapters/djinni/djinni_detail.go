package djinni

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/job-scraper/internal/jobdetail"
	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// detailLang is the UI language every detail page is read in. Djinni ignores
// Accept-Language and the django_language cookie, so the query parameter is the
// only lever — and pinning one language means one vocabulary to parse instead
// of a bilingual pattern table.
const detailLang = "en"

var _ ports.JobDetailReader = Source{}

// ReadJobDetail implements ports.JobDetailReader: it reads one job's own page,
// which carries far more than a search card — the structured requirements, the
// salary band, the whole description.
//
// It goes through fetchDoc, so an expired session is re-established and a login
// wall is reported the same way it is for searches.
func (d Source) ReadJobDetail(ctx context.Context, jobURL string, _ map[string]any) (model.JobDetail, error) {
	headers, err := d.authHeaders(ctx)
	if err != nil {
		return model.JobDetail{}, err
	}

	doc, err := d.fetchDoc(ctx, withLang(jobURL, detailLang), headers)
	if err != nil {
		return model.JobDetail{}, err
	}
	return ParseJobDetail(doc, jobURL)
}

// ParseJobDetail extracts a job page that has already been fetched. It is
// exported so a caller holding its own HTML — a fixture, a cached body — can
// reuse the parsing without going through the network.
//
// The page must be the English one: the sidebar is parsed by its labels, and
// Ukrainian labels would silently yield an empty record. Failing loudly here
// turns "half the fields are missing" into one readable error.
func ParseJobDetail(doc *goquery.Document, sourceURL string) (model.JobDetail, error) {
	// A page that declares another language is refused: the sidebar is parsed
	// by its English labels, and Ukrainian ones would yield a near-empty record
	// with no hint as to why. A page declaring nothing — a fragment, a fixture
	// — is parsed for whatever it does carry, which is the partial-read
	// contract PostingReader promises.
	if lang := pageLang(doc); lang != "" && lang != detailLang {
		return model.JobDetail{}, fmt.Errorf("djinni: expected the %s page, got %q — append ?lang=%s to the URL", detailLang, lang, detailLang)
	}

	ld := parseJobPostingLD(doc)
	apply := parseApplyFormData(doc)
	side := parseSidebar(doc)
	tier := jobdetail.SalaryTier(squashText(doc.Find(`[title*="Salary level"]`).First()))
	// The employer's own figure, where it published one, as against the
	// board's estimate for similar jobs.
	publicSalary := squashText(doc.Find(`.public-salary-item`).First())

	detail := model.JobDetail{
		SourceKey:   Key,
		ExternalID:  strconv.Itoa(ld.Identifier),
		URL:         strutil.FirstNonEmpty(ld.URL, sourceURL),
		Title:       strutil.FirstNonEmpty(ld.Title, squashText(doc.Find(`.detail--title, h1`).First())),
		Category:    strutil.FirstNonEmpty(ld.Category, side.category),
		Employment:  strutil.FirstNonEmpty(ld.EmploymentType, side.employment),
		DirectApply: ld.DirectApply,
		RecruiterID: apply.RecruiterID,

		Company: model.Company{
			Name: strutil.FirstNonEmpty(
				ld.HiringOrganization.Name,
				squashText(doc.Find(`a[href*="/jobs/company-"].text-secondary`).First()),
				squashText(doc.Find(`a[href^="/company/"], [data-analytics="company_page"]`).First()),
			),
			URL:      firstAttrOf(doc, "href", `a[href*="/jobs/company-"]`, `a[href^="/company/"]`),
			Site:     ld.HiringOrganization.SameAs,
			LogoURL:  attrOf(doc.Find(`header img.userpic-image`).First(), "src"),
			Type:     side.companyType,
			Domain:   side.domain,
			Industry: strutil.FirstNonEmpty(ld.Industry, side.domain),
		},

		Salary: model.Salary{
			Min:       int(ld.EstimatedSalary.MinValue),
			Max:       int(ld.EstimatedSalary.MaxValue),
			Currency:  ld.EstimatedSalary.Currency,
			Public:    apply.PublicSalaryMin != nil || apply.PublicSalaryMax != nil || publicSalary != "",
			Raw:       publicSalary,
			Tier:      tier,
			TierLabel: jobdetail.SalaryTierLabel(tier),
			Estimate:  squashText(doc.Find(`#salary-suggestion`).First()),
		},

		Location: model.Location{
			Mode:      workMode(ld.JobLocationType, side.workMode),
			Modes:     side.workModes,
			Scope:     side.locationScope,
			Countries: side.locationCountries,
			Offices:   side.officeLocations,
		},

		Requirements: model.Requirements{
			ExperienceYears:        ld.ExperienceRequirements.MonthsOfExperience / 12,
			MinimumExperienceYears: side.minExperienceYears,
			Skills:                 side.skills,
			Languages:              side.languages,
			HasTestTask:            side.hasTestTask,
		},

		Timeline: model.Timeline{
			PostedAt:  parseLDTime(ld.DatePosted),
			ValidThru: parseLDTime(ld.ValidThrough),
			// View counts and response times are only true as of the read.
			ScrapedAt: time.Now().Unix(),
		},

		Content: model.Content{
			// The JSON-LD description is Djinni's own SEO summary, silently capped
			// short (it ends mid-sentence with "..." past a few hundred chars) — it
			// is a fallback, not the primary source. body's job-post__description
			// carries the whole posting.
			Description:     jobdetail.PlainText(strings.TrimSpace(ld.Description)),
			MetaDescription: jobdetail.PlainText(squashAttr(doc.Find(`meta[name="description"]`).First(), "content")),
			OGImage:         attrOf(doc.Find(`meta[property="og:image"]`).First(), "content"),
		},

		OtherFacts: side.otherFacts,
	}

	if ld.Identifier == 0 {
		detail.ExternalID = idFromURL(sourceURL)
	}

	// Experience appears in JSON-LD or in the sidebar, not reliably in both.
	req := &detail.Requirements
	if req.ExperienceYears == 0 {
		req.ExperienceYears = side.experienceYears
	}
	// A posting that names no softer bound is its own floor.
	if req.MinimumExperienceYears == 0 {
		req.MinimumExperienceYears = req.ExperienceYears
	}

	if html, err := doc.Find(`.job-post__description`).First().Html(); err == nil {
		detail.Content.DescriptionHTML = strings.TrimSpace(html)
	}
	// The body block is the whole posting; the JSON-LD summary above is only
	// used when the body itself is missing (a fixture, a stripped-down page).
	if bodyText := jobdetail.PlainText(squashText(doc.Find(`.job-post__description`).First())); bodyText != "" {
		detail.Content.Description = bodyText
	}

	applyPublicationInfo(doc, &detail)

	// Older markup states the posting date in a <time> element instead of
	// JSON-LD, and the location in the fact list rather than the sidebar.
	if detail.Timeline.PostedAt == 0 {
		detail.Timeline.PostedAt = jobdetail.TimeIn(djinniTZ(), attrOf(doc.Find(`time[datetime]`).First(), "datetime"), time.RFC3339)
	}
	if detail.Location.Scope == model.LocationUnknown {
		detail.Location.Scope, detail.Location.Countries = jobdetail.Locations(squashText(doc.Find(`.location-text`).First()))
	}
	if detail.Location.Mode == model.WorkModeUnknown {
		if modes := jobdetail.WorkModes(squashText(doc.Find(`.job-additional-info, aside`).First())); len(modes) > 0 {
			detail.Location.Mode, detail.Location.Modes = modes[0], modes
		}
	}

	// Header badges are decoration: "🪖 DefTech" restates the domain and
	// "Product" the company type, both of which are already fields. Only a
	// badge carrying something new survives.
	doc.Find(`header .badge`).Each(func(_ int, s *goquery.Selection) {
		badge := squashText(s)
		if restatesFact(badge, detail.Company.Domain) || restatesFact(badge, detail.Company.Type) {
			return
		}
		detail.Badges = append(detail.Badges, badge)
	})
	return detail, nil
}

// jobPostingLD is the schema.org JobPosting Djinni embeds. It is the primary
// source for everything it carries: the numbers are exact there, while the
// visible page rounds and localises them.
type jobPostingLD struct {
	Title           string `json:"title"`
	Category        string `json:"category"`
	Industry        string `json:"industry"`
	Description     string `json:"description"`
	DatePosted      string `json:"datePosted"`
	ValidThrough    string `json:"validThrough"`
	EmploymentType  string `json:"employmentType"`
	JobLocationType string `json:"jobLocationType"`
	DirectApply     bool   `json:"directApply"`
	Identifier      int    `json:"identifier"`
	URL             string `json:"url"`
	EstimatedSalary struct {
		Currency string `json:"currency"`
		// Djinni serialises these as 4000.0, which will not decode into an int.
		MinValue float64 `json:"minValue"`
		MaxValue float64 `json:"maxValue"`
	} `json:"estimatedSalary"`
	ExperienceRequirements struct {
		MonthsOfExperience float64 `json:"monthsOfExperience"`
	} `json:"experienceRequirements"`
	HiringOrganization struct {
		Name   string `json:"name"`
		SameAs string `json:"sameAs"`
	} `json:"hiringOrganization"`
}

func parseJobPostingLD(doc *goquery.Document) jobPostingLD {
	var out jobPostingLD
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var probe struct {
			Type string `json:"@type"`
		}
		raw := []byte(s.Text())
		if json.Unmarshal(raw, &probe) != nil || probe.Type != "JobPosting" {
			return true
		}
		_ = json.Unmarshal(raw, &out)
		return false
	})
	return out
}

// applyFormData is the apply widget's bootstrap JSON, the only place the
// recruiter id and the employer's own salary range appear.
type applyFormData struct {
	JobID           int  `json:"job_id"`
	RecruiterID     int  `json:"recruiter_id"`
	PublicSalaryMin *int `json:"public_salary_min"`
	PublicSalaryMax *int `json:"public_salary_max"`
}

func parseApplyFormData(doc *goquery.Document) applyFormData {
	var out applyFormData
	_ = json.Unmarshal([]byte(doc.Find(`#apply_job_form-data`).First().Text()), &out)
	return out
}

// sidebarFacts is the right-hand card, split into its categories rather than
// left as a list of sentences.
type sidebarFacts struct {
	experienceYears    float64
	minExperienceYears float64

	workMode  model.WorkMode
	workModes []model.WorkMode

	officeLocations []string

	locationScope     model.LocationScope
	locationCountries []string
	locationRaw       string

	languages []model.LanguageReq
	skills    []model.SkillReq

	category    string
	employment  string
	domain      string
	companyType string
	hasTestTask bool
	otherFacts  []string
}

// parseSidebar reads the fact card by label rather than by position, so an
// added or reordered row moves to otherFacts instead of shifting every field.
func parseSidebar(doc *goquery.Document) sidebarFacts {
	facts := sidebarFacts{
		workMode:      model.WorkModeUnknown,
		locationScope: model.LocationUnknown,
		category:      squashText(doc.Find(`#job_extra_info .fw-medium`).First()),
		skills:        parseSkills(doc),
	}

	doc.Find(`aside li`).Each(func(_ int, li *goquery.Selection) {
		if li.Find("li").Length() > 0 { // a wrapper around nested rows
			return
		}
		// The category row carries a table of per-skill experience, whose text
		// reads as a requirement of its own. It is parsed separately.
		if li.Closest(`#job_extra_info`).Length() > 0 {
			return
		}
		text := squashText(li)
		if text == "" {
			return
		}
		lang := parseLanguage(text)

		switch {
		case experienceRe.MatchString(text):
			// One row can carry both bounds: "from 5 years of experience
			// Considering with 4 years of experience". The first number is the
			// requirement; the "considering" clause is the floor beneath it.
			if considerRe.MatchString(text) {
				facts.minExperienceYears = jobdetail.Years(considerRe.FindString(text))
				text = strings.TrimSpace(considerRe.ReplaceAllString(text, ""))
			}
			facts.experienceYears = jobdetail.Years(text)

		case noExperienceRe.MatchString(text):
			// "No experience required" states a floor of zero, which the zero
			// value already says.

		case officeLocationsRe.MatchString(text):
			// "Office: Poland, Czechia" — where the employer sits, which is a
			// different fact from which modes it offers.
			facts.officeLocations = jobdetail.SplitList(officeLocationsRe.ReplaceAllString(text, ""))

		case workModeRe.MatchString(text):
			// One row can offer several: "Office, Remote, Hybrid Remote".
			facts.workModes = jobdetail.WorkModes(text)
			if len(facts.workModes) > 0 {
				facts.workMode = facts.workModes[0]
			}

		case lang.Language != "":
			facts.languages = append(facts.languages, lang)

		case locationNoteRe.MatchString(text):
			facts.locationRaw = squashText(li.Find(`.location-text`).First())
			if facts.locationRaw == "" {
				facts.locationRaw = strings.TrimSpace(locationNoteRe.ReplaceAllString(text, ""))
			}
			facts.locationScope, facts.locationCountries = jobdetail.Locations(facts.locationRaw)

		case labelledRe.MatchString(text):
			m := labelledRe.FindStringSubmatch(text)
			switch strings.ToLower(m[1]) {
			case "employment":
				facts.employment = strings.TrimSpace(m[2])
			case "domain":
				facts.domain = strings.TrimSpace(m[2])
			default:
				facts.otherFacts = append(facts.otherFacts, text)
			}

		case testTaskRe.MatchString(text):
			facts.hasTestTask = true

		case companyTypeRe.MatchString(text):
			facts.companyType = text

		case text == facts.category:
			// already read from #job_extra_info

		default:
			facts.otherFacts = append(facts.otherFacts, text)
		}
	})
	return facts
}

// parseSkills reads the per-skill experience table Djinni puts under the
// category: "Node.js | 7 years".
func parseSkills(doc *goquery.Document) []model.SkillReq {
	var skills []model.SkillReq
	doc.Find(`#job_extra_info tr`).Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		name := squashText(cells.First())
		if name == "" {
			return
		}
		skills = append(skills, model.SkillReq{
			Name:  name,
			Years: jobdetail.Years(squashText(cells.Last())),
		})
	})
	return skills
}

// parseLanguage turns "English B2 - Upper Intermediate" into {English, B2}.
func parseLanguage(text string) model.LanguageReq {
	m := languageRe.FindStringSubmatch(text)
	if m == nil {
		return model.LanguageReq{}
	}
	return model.LanguageReq{Language: m[1], Level: jobdetail.LanguageLevel(text)}
}

// workMode trusts schema.org over the sidebar, and falls back to what the
// sidebar listed first.
func workMode(jobLocationType string, fromSidebar model.WorkMode) model.WorkMode {
	if mode := jobdetail.WorkModeFromSchema(jobLocationType); mode != model.WorkModeUnknown {
		return mode
	}
	if fromSidebar != "" {
		return fromSidebar
	}
	return model.WorkModeUnknown
}

// applyPublicationInfo reads the block above the apply button: how much
// competition the posting has drawn and how reliably the recruiter answers.
// It sits outside the fact card's list, so parseSidebar never sees it, and is
// absent altogether on a posting too fresh to have any.
func applyPublicationInfo(doc *goquery.Document, detail *model.JobDetail) {
	info := doc.Find(`#job-publication-info`).First()
	if info.Length() == 0 {
		return
	}
	text := squashText(info)

	detail.Activity.Views = jobdetail.Count(firstGroup(viewsRe, text))
	detail.Activity.Applications = jobdetail.Count(firstGroup(applicationsRe, text))
	detail.Activity.ResponseRate = squashText(info.Find(`.fw-medium.text-secondary`).First())

	// The visible dates carry no year, so they are read against the posting's
	// own timestamp rather than against today.
	year := time.Now().In(djinniTZ()).Year()
	if detail.Timeline.PostedAt != 0 {
		year = time.Unix(detail.Timeline.PostedAt, 0).In(djinniTZ()).Year()
	}
	detail.Timeline.PublishedAt = jobdetail.DayMonth(djinniTZ(), firstGroup(publishedRe, text), year)
	detail.Timeline.UpdatedAt = jobdetail.DayMonth(djinniTZ(), firstGroup(updatedRe, text), year)

	// "Last responded 6 days ago" is vague, but its tooltip is exact.
	lastResponded := info.Find(`[title]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return lastRespondedRe.MatchString(s.Text())
	}).First()
	detail.Activity.LastRespondedAt = parseTooltipTime(attrOf(lastResponded, "title"))
}

// djinniTZ is the wall clock Djinni prints its dates in.
func djinniTZ() *time.Location { return jobdetail.Location("Europe/Kyiv") }

// parseLDTime reads the JSON-LD stamp, "2026-08-10T15:16:46.775635", which
// carries no zone and is Djinni's local time.
func parseLDTime(raw string) int64 {
	return jobdetail.TimeIn(djinniTZ(), raw, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02")
}

// parseTooltipTime reads the exact stamp behind a relative label: "18:33
// 10.08.2026".
func parseTooltipTime(raw string) int64 {
	return jobdetail.TimeIn(djinniTZ(), raw, "15:04 02.01.2006", "02.01.2006 15:04", "02.01.2006")
}

// restatesFact reports whether a badge says the same thing as a field already
// read, once its decorative emoji and spacing are taken off.
func restatesFact(badge, fact string) bool {
	if fact == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(emojiRe.ReplaceAllString(badge, "")), fact)
}

func firstGroup(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func pageLang(doc *goquery.Document) string {
	lang, _ := doc.Find("html").First().Attr("lang")
	return strings.ToLower(strings.TrimSpace(lang))
}

// withLang forces Djinni's UI language on a job URL.
func withLang(raw, lang string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("lang", lang)
	u.RawQuery = q.Encode()
	return u.String()
}

func idFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	return strings.SplitN(segs[len(segs)-1], "-", 2)[0]
}

func squashText(s *goquery.Selection) string { return squashSpace(s.Text()) }

func squashAttr(s *goquery.Selection, name string) string { return squashSpace(attrOf(s, name)) }

// firstAttrOf returns the named attribute of the first selector that matches,
// which is how one parser covers markup a board has changed under it.
func firstAttrOf(doc *goquery.Document, name string, selectors ...string) string {
	for _, sel := range selectors {
		if v := attrOf(doc.Find(sel).First(), name); v != "" {
			return v
		}
	}
	return ""
}

func attrOf(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return v
}

func squashSpace(s string) string { return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " ")) }

var (
	whitespaceRe = regexp.MustCompile(`\s+`)
	// Pictographs and the variation selectors that follow them: everything a
	// badge wears that is not the word itself.
	emojiRe = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{FE0F}\x{200D}]`)

	// "Only from 5 years of experience"
	experienceRe   = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\+?\s*years?\s*(?:of\s+experience)?`)
	noExperienceRe = regexp.MustCompile(`(?i)no experience`)
	// "Considering with 4 years of experience"
	considerRe        = regexp.MustCompile(`(?i)consider\w*\s+with\s+(\d+(?:\.\d+)?)\s*\+?\s*years?[^.]*`)
	workModeRe        = regexp.MustCompile(`(?i)\b(remote|office|hybrid)\b`)
	officeLocationsRe = regexp.MustCompile(`(?i)^office:\s*`)
	listSepRe         = regexp.MustCompile(`(?i)\s*,\s*|\s+or\s+`)
	locationNoteRe    = regexp.MustCompile(`(?i)countries where we consider candidates`)
	labelledRe        = regexp.MustCompile(`^([A-Za-z ]+):\s*(.+)$`)
	testTaskRe        = regexp.MustCompile(`(?i)test task`)
	companyTypeRe     = regexp.MustCompile(`(?i)^product( company)?$|outsource|outstaff|agency|in-house`)
	languageRe        = regexp.MustCompile(`(?i)^(English|Ukrainian|German|Polish|French|Spanish|Italian|Portuguese)\s`)
	cefrRe            = regexp.MustCompile(`\b([ABC][12])\b`)

	// The publication block: "Published 23 July · Updated 10 August",
	// "400 views · 44 applications", "Last responded yesterday".
	viewsRe         = regexp.MustCompile(`([\d\s,\x{00a0}]+)views`)
	applicationsRe  = regexp.MustCompile(`([\d\s,\x{00a0}]+)applications?`)
	publishedRe     = regexp.MustCompile(`(?i)published\s+([^·]+?)(?:\s*·|\s*\d+\s*views|$)`)
	updatedRe       = regexp.MustCompile(`(?i)updated\s+([^·]+?)(?:\s*·|\s*\d+\s*views|$)`)
	lastRespondedRe = regexp.MustCompile(`(?i)last responded`)

	// Djinni's wording where it prints no CEFR code. Order matters:
	// "pre-intermediate" contains "intermediate" and must be tested first.
	levelWords = []struct {
		word  string
		level model.LanguageLevel
	}{
		{"native", model.LevelC2},
		{"proficient", model.LevelC2},
		{"advanced", model.LevelC1},
		{"fluent", model.LevelC1},
		{"upper", model.LevelB2},
		{"pre-intermediate", model.LevelA2},
		{"elementary", model.LevelA2},
		{"intermediate", model.LevelB1},
		{"beginner", model.LevelA1},
	}
)
