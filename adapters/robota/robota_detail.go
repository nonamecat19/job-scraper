package robota

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nonamecat19/job-scraper/internal/htmlutil"
	"github.com/nonamecat19/job-scraper/internal/httpjson"
	"github.com/nonamecat19/job-scraper/internal/jobdetail"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

var (
	_ ports.PostingReader   = Source{}
	_ ports.JobDetailReader = Source{}

	// robota.ua/company53660/vacancy11270559 — both ids are in the path, with
	// no separators. The older /company/53660/vacancy/11270559 form is still
	// served, so both are claimed.
	robotaPostingPathRe = regexp.MustCompile(`^/company/?(\d+)/vacancy/?(\d+)/?$`)
)

// robotaTZ is the wall clock the API stamps its dates in.
func robotaTZ() *time.Location { return jobdetail.Location("Europe/Kyiv") }

// MatchesPostingURL implements ports.PostingReader. It does no I/O and
// tolerates any input, because it is called for every registered source on
// every read.
func (Source) MatchesPostingURL(rawURL string) bool {
	_, ok := postingID(rawURL)
	return ok
}

func postingID(rawURL string) (uint64, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return 0, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return 0, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "robota.ua" && host != "rabota.ua" {
		return 0, false
	}
	m := robotaPostingPathRe.FindStringSubmatch(parsed.Path)
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
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
// It reads the API rather than the page: robota.ua serves its HTML behind a
// Cloudflare challenge, while api.rabota.ua answers plainly and with more
// structure than the page shows — the facets a scrape would have to infer from
// prose are already fielded there.
func (a Source) ReadJobDetail(ctx context.Context, rawURL string, _ map[string]any) (model.JobDetail, error) {
	id, ok := postingID(rawURL)
	if !ok {
		return model.JobDetail{}, fmt.Errorf("robota: %q is not a vacancy URL", rawURL)
	}

	var v robotaVacancyDetail
	params := url.Values{"id": {strconv.FormatUint(id, 10)}}
	if err := httpjson.GetJSON(ctx, nil, robotaAPIBase+"/vacancy", params, &v); err != nil {
		return model.JobDetail{}, fmt.Errorf("robota: read vacancy %d: %w", id, err)
	}
	if v.ID == 0 {
		return model.JobDetail{}, fmt.Errorf("robota: vacancy %d not found", id)
	}
	// The payload names the city in Russian only; the dictionary has the rest.
	return v.toDetail(cityName(ctx, v.CityID, v.CityName)), nil
}

// robotaVacancyDetail is one vacancy as the API serves it, which carries far
// more than the search document does.
type robotaVacancyDetail struct {
	ID            uint64 `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	CompanyName   string `json:"companyName"`
	NotebookID    uint64 `json:"notebookId"`
	Logo          string `json:"logo"`
	ContactURL    string `json:"contactUrl"`
	BranchName    string `json:"branchName"`
	CityName      string `json:"cityName"`
	CityID        uint64 `json:"cityId"`
	Date          string `json:"date"`
	IsActive      bool   `json:"isActive"`
	SalaryFrom    int    `json:"salaryFrom"`
	SalaryTo      int    `json:"salaryTo"`
	SalaryComment string `json:"salaryComment"`
	// FormApplyCustomURL is set when applying happens on the employer's own
	// site, which is precisely when DirectApply is false.
	FormApplyCustomURL string `json:"formApplyCustomUrl"`

	SearchTags []struct {
		Name string `json:"name"`
	} `json:"searchTags"`
	Clusters []struct {
		Name   string `json:"name"`
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
	} `json:"clusters"`
	Badges []struct {
		Name string `json:"name"`
	} `json:"badges"`
}

func (v robotaVacancyDetail) toDetail(city string) model.JobDetail {
	facets := v.facets()

	detail := model.JobDetail{
		SourceKey:  Key,
		ExternalID: strconv.FormatUint(v.ID, 10),
		URL:        fmt.Sprintf("https://robota.ua/company%d/vacancy%d", v.NotebookID, v.ID),
		Title:      strings.TrimSpace(v.Name),
		// The API applies on robota.ua unless the employer redirected it.
		DirectApply: v.FormApplyCustomURL == "",
		Company: model.Company{
			Name:     strings.TrimSpace(v.CompanyName),
			URL:      fmt.Sprintf("https://robota.ua/company%d", v.NotebookID),
			Site:     v.ContactURL,
			LogoURL:  logoURL(v.Logo),
			Domain:   v.BranchName,
			Industry: strings.ToLower(v.BranchName),
		},
		Salary:   v.salary(),
		Location: locationFrom(city, facets["вид занятости"]),
		Requirements: model.Requirements{
			Skills:    skillsFrom(facets),
			Languages: languagesFrom(facets),
		},
		Timeline: model.Timeline{
			PostedAt: jobdetail.TimeIn(robotaTZ(), v.Date, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05"),
			// The record is only true as of the read.
			ScrapedAt: time.Now().Unix(),
		},
		Content: model.Content{
			Description:     jobdetail.PlainText(htmlutil.HTMLToText(v.Description)),
			DescriptionHTML: strings.TrimSpace(v.Description),
		},
	}

	detail.Category = firstOf(facets["тип разработки"])
	detail.Employment = employmentFrom(facets["вид занятости"])
	detail.Requirements.ExperienceYears = experienceFrom(facets["опыт работы"])
	detail.Requirements.MinimumExperienceYears = detail.Requirements.ExperienceYears

	for _, badge := range v.Badges {
		if name := strings.TrimSpace(badge.Name); name != "" {
			detail.Badges = append(detail.Badges, name)
		}
	}
	return detail
}

// facets flattens the API's clusters into lowercase name -> values, which is
// how every structured fact on a robota vacancy is published.
func (v robotaVacancyDetail) facets() map[string][]string {
	out := map[string][]string{}
	for _, cluster := range v.Clusters {
		key := strings.ToLower(strings.TrimSpace(cluster.Name))
		for _, group := range cluster.Groups {
			if name := strings.TrimSpace(group.Name); name != "" {
				out[key] = append(out[key], name)
			}
		}
	}
	return out
}

func (v robotaVacancyDetail) salary() model.Salary {
	salary := model.Salary{Raw: strings.TrimSpace(v.SalaryComment)}
	if v.SalaryFrom == 0 && v.SalaryTo == 0 && salary.Raw == "" {
		return model.Salary{}
	}
	// Whatever robota shows here the employer entered: it estimates nothing.
	salary.Public = true
	salary.Min, salary.Max = v.SalaryFrom, v.SalaryTo
	if salary.Min > 0 || salary.Max > 0 {
		salary.Currency = "UAH"
	}
	return salary
}

// locationFrom reads the one city the API names plus the employment facet,
// which is where "Удаленная работа" lives.
func locationFrom(city string, employment []string) model.Location {
	loc := model.Location{Mode: model.WorkModeUnknown, Scope: model.LocationUnknown}

	for _, value := range employment {
		switch lower := strings.ToLower(value); {
		case strings.Contains(lower, "гібрид"), strings.Contains(lower, "гибрид"):
			loc.Modes = appendMode(loc.Modes, model.WorkModeHybrid)
		case strings.Contains(lower, "віддален"), strings.Contains(lower, "удален"):
			loc.Modes = appendMode(loc.Modes, model.WorkModeRemote)
		}
	}
	if city = strings.TrimSpace(city); city != "" {
		loc.Scope, loc.Countries, loc.Offices = model.LocationCountries, []string{city}, []string{city}
		loc.Modes = appendMode(loc.Modes, model.WorkModeOffice)
	}
	if len(loc.Modes) > 0 {
		loc.Mode = loc.Modes[0]
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

// employmentFrom maps the employment facet onto the schema.org term the other
// sources use, so a caller filters on one vocabulary.
func employmentFrom(values []string) string {
	for _, value := range values {
		switch lower := strings.ToLower(value); {
		case strings.Contains(lower, "повна"), strings.Contains(lower, "полная"):
			return "FULL_TIME"
		case strings.Contains(lower, "частков"), strings.Contains(lower, "частичн"):
			return "PART_TIME"
		}
	}
	return ""
}

// skillsFrom lifts the technology facets. Robota states no per-skill
// experience, so only the names are known.
func skillsFrom(facets map[string][]string) []model.SkillReq {
	var skills []model.SkillReq
	seen := map[string]bool{}
	for _, key := range []string{"язык программирования", "framework javascript", "база данных", "мова програмування"} {
		for _, name := range facets[key] {
			if seen[name] {
				continue
			}
			seen[name] = true
			skills = append(skills, model.SkillReq{Name: name})
		}
	}
	return skills
}

// languagesFrom pairs the language facet with the level facet, which robota
// publishes as two separate clusters.
func languagesFrom(facets map[string][]string) []model.LanguageReq {
	var langs []model.LanguageReq
	for _, named := range facets["знание языка"] {
		lang := model.LanguageReq{Language: languageName(named)}
		if lang.Language == "" {
			continue
		}
		lang.Level = levelFrom(facets["уровень владения языком"])
		langs = append(langs, lang)
	}
	return langs
}

func languageName(named string) string {
	lower := strings.ToLower(named)
	switch {
	case strings.Contains(lower, "англ"):
		return "English"
	case strings.Contains(lower, "нім"), strings.Contains(lower, "нем"):
		return "German"
	case strings.Contains(lower, "поль"):
		return "Polish"
	case strings.Contains(lower, "укра"):
		return "Ukrainian"
	}
	return strings.TrimSpace(named)
}

// levelFrom maps robota's wording — "Английский (выше среднего)" — onto CEFR.
// The shared table is English-only, so the wording is translated first.
func levelFrom(values []string) model.LanguageLevel {
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, entry := range robotaLevels {
			if strings.Contains(lower, entry.word) {
				return entry.level
			}
		}
	}
	return model.LevelUnknown
}

// robotaLevels is ordered: "вище середнього" contains "середнього", so the
// longer phrase must be tested first.
var robotaLevels = []struct {
	word  string
	level model.LanguageLevel
}{
	{"вільн", model.LevelC1}, {"свободн", model.LevelC1},
	{"продвинут", model.LevelC1}, {"просунут", model.LevelC1},
	{"вище середнього", model.LevelB2}, {"выше среднего", model.LevelB2},
	{"нижче середнього", model.LevelA2}, {"ниже среднего", model.LevelA2},
	{"середн", model.LevelB1}, {"средн", model.LevelB1},
	{"початков", model.LevelA1}, {"начальн", model.LevelA1}, {"базов", model.LevelA1},
	{"носій", model.LevelC2}, {"носител", model.LevelC2},
}

// experienceFrom reads "Опыт работы от 3 лет".
func experienceFrom(values []string) float64 {
	for _, value := range values {
		if m := robotaYearsRe.FindStringSubmatch(value); m != nil {
			years, err := strconv.ParseFloat(m[1], 64)
			if err == nil {
				return years
			}
		}
	}
	return 0
}

var robotaYearsRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(?:рок|років|роки|лет|года|год|years?)`)

func logoURL(logo string) string {
	if strings.TrimSpace(logo) == "" {
		return ""
	}
	if strings.HasPrefix(logo, "http") {
		return logo
	}
	return "https://cdn.rabota.ua/img/companyprofile/" + logo
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
