package model

import (
	"strconv"
	"strings"
	"time"
)

// WorkMode is how a job is worked, normalised away from any one site's wording.
type WorkMode string

const (
	WorkModeRemote  WorkMode = "remote"
	WorkModeOffice  WorkMode = "office"
	WorkModeHybrid  WorkMode = "hybrid"
	WorkModeUnknown WorkMode = "unknown"
)

// LocationScope reserves the sentinel boards use for "no country restriction"
// so it is never mistaken for a place name.
type LocationScope string

const (
	LocationWorldwide LocationScope = "worldwide"
	LocationCountries LocationScope = "countries"
	LocationUnknown   LocationScope = "unknown"
)

// LanguageLevel is CEFR. Boards that print their own wording — "Native",
// "Upper Intermediate" — are mapped onto it by their adapter.
type LanguageLevel string

const (
	LevelA1      LanguageLevel = "A1"
	LevelA2      LanguageLevel = "A2"
	LevelB1      LanguageLevel = "B1"
	LevelB2      LanguageLevel = "B2"
	LevelC1      LanguageLevel = "C1"
	LevelC2      LanguageLevel = "C2"
	LevelUnknown LanguageLevel = ""
)

// SalaryTier is a band published instead of a range: some boards rank a job
// against their own market data rather than showing numbers.
type SalaryTier string

const (
	TierBelowAverage SalaryTier = "below_average"
	TierAverage      SalaryTier = "average"
	TierAboveAverage SalaryTier = "above_average"
	TierTopRange     SalaryTier = "top_range"
	TierUnknown      SalaryTier = ""
)

// LanguageReq is one language requirement: "English B2 - Upper Intermediate"
// becomes {English, B2}.
type LanguageReq struct {
	Language string        `json:"language"`
	Level    LanguageLevel `json:"level"`
}

func (l LanguageReq) String() string {
	if l.Level == LevelUnknown {
		return l.Language
	}
	return l.Language + " " + string(l.Level)
}

// Company is who is hiring.
type Company struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	Site    string `json:"site,omitempty"`
	LogoURL string `json:"logoUrl,omitempty"`
	// Type is how the company is organised — product, outstaff, agency.
	Type string `json:"type,omitempty"`
	// Domain is the board's label for the field it works in ("DefTech");
	// Industry is the machine-readable spelling ("miltech"). They differ often
	// enough to be worth keeping apart.
	Domain   string `json:"domain,omitempty"`
	Industry string `json:"industry,omitempty"`
}

// Salary is what the posting says about pay. A board may publish an exact
// range, a band relative to its own market data, or nothing at all.
type Salary struct {
	Min      int    `json:"min,omitempty"`
	Max      int    `json:"max,omitempty"`
	Currency string `json:"currency,omitempty"`
	// Public reports whether the employer itself published the range, as
	// opposed to the board estimating one.
	Public bool `json:"public"`
	// Raw is the employer's own salary text, where it published one.
	Raw string `json:"raw,omitempty"`
	// Tier is the board's band where it shows one instead of numbers.
	Tier      SalaryTier `json:"tier,omitempty"`
	TierLabel string     `json:"tierLabel,omitempty"`
	// Estimate is the board's own suggested range for similar jobs.
	Estimate string `json:"estimate,omitempty"`
}

// Location is where the work happens and who may do it.
type Location struct {
	// Mode is the first mode the posting lists; Modes is every mode it
	// accepts. A job open to office, remote and hybrid at once is common.
	Mode  WorkMode   `json:"mode"`
	Modes []WorkMode `json:"modes,omitempty"`

	Scope LocationScope `json:"scope"`
	// Countries are where candidates may be based. Entries may be regions
	// rather than countries: "Countries of Europe" is one value boards use.
	Countries []string `json:"countries,omitempty"`
	// Offices are where the employer sits, stated separately from where
	// candidates may live.
	Offices []string `json:"offices,omitempty"`
}

// Remote reports whether the work can be done remotely — true for a posting
// offering remote among several modes, not only for a remote-only one.
func (l Location) Remote() bool {
	if l.Mode == WorkModeRemote {
		return true
	}
	for _, mode := range l.Modes {
		if mode == WorkModeRemote {
			return true
		}
	}
	return false
}

// String renders where candidates may be based, for callers that want one
// line rather than the parts.
func (l Location) String() string {
	if l.Scope == LocationWorldwide {
		return "Worldwide"
	}
	return strings.Join(l.Countries, ", ")
}

// SkillReq is one named skill with the experience the posting asks for it —
// "Azure, 3 years" — which can be shorter than the overall requirement.
type SkillReq struct {
	Name  string  `json:"name"`
	Years float64 `json:"years,omitempty"`
}

func (s SkillReq) String() string {
	if s.Years == 0 {
		return s.Name
	}
	return s.Name + " " + strconv.FormatFloat(s.Years, 'f', -1, 64) + "y"
}

// Requirements is what the posting asks of a candidate.
type Requirements struct {
	// ExperienceYears is fractional where the posting is: a board stating 18
	// months means 1.5 years.
	ExperienceYears float64 `json:"experienceYears,omitempty"`
	// MinimumExperienceYears is the floor the employer will still consider —
	// "Considering with 4 years" under a 5-year requirement. Where the posting
	// names no lower bound it equals the requirement.
	MinimumExperienceYears float64 `json:"minimumExperienceYears,omitempty"`

	// Skills are the named technologies and the experience asked for each.
	Skills      []SkillReq    `json:"skills,omitempty"`
	Languages   []LanguageReq `json:"languages,omitempty"`
	HasTestTask bool          `json:"hasTestTask"`
}

// Skill returns the requirement for one named skill, if the posting states one.
func (r Requirements) Skill(name string) (SkillReq, bool) {
	for _, s := range r.Skills {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return SkillReq{}, false
}

// Language returns the requirement for one language, if the posting states one.
func (r Requirements) Language(name string) (LanguageReq, bool) {
	for _, l := range r.Languages {
		if l.Language == name {
			return l, true
		}
	}
	return LanguageReq{}, false
}

// Timeline is every date on the posting, as Unix seconds. Zero means the
// posting does not state it.
type Timeline struct {
	// PostedAt is when the posting last went live; PublishedAt is when it
	// first did. They differ on a posting that has been renewed.
	PostedAt    int64 `json:"postedAt,omitempty"`
	PublishedAt int64 `json:"publishedAt,omitempty"`
	UpdatedAt   int64 `json:"updatedAt,omitempty"`
	ValidThru   int64 `json:"validThru,omitempty"`
	// ScrapedAt is when this record was read, which dates everything above it:
	// view counts and "last responded" are only true as of this moment.
	ScrapedAt int64 `json:"scrapedAt,omitempty"`
}

// Activity is how much competition the posting has drawn and how reliably the
// employer answers. Absent on a posting too fresh to have any.
type Activity struct {
	Views        int `json:"views,omitempty"`
	Applications int `json:"applications,omitempty"`
	// ResponseRate is the board's own verdict on the recruiter — "Low".
	ResponseRate string `json:"responseRate,omitempty"`
	// LastRespondedAt is Unix seconds; zero when never or not stated.
	LastRespondedAt int64 `json:"lastRespondedAt,omitempty"`
}

// Content is the posting's own text.
type Content struct {
	Description     string `json:"description,omitempty"`
	DescriptionHTML string `json:"descriptionHtml,omitempty"`
	MetaDescription string `json:"metaDescription,omitempty"`
	OGImage         string `json:"ogImage,omitempty"`
}

// JobDetail is one job's own page, which carries more than a search card does.
// Normalized projects it onto NormalizedJob for callers wanting the common
// shape.
type JobDetail struct {
	SourceKey  string `json:"sourceKey"`
	ExternalID string `json:"externalId,omitempty"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Category   string `json:"category,omitempty"`
	// Employment is the schema.org term: FULL_TIME, PART_TIME, CONTRACTOR.
	Employment string `json:"employment,omitempty"`
	// DirectApply reports whether the application is made on the board itself
	// rather than on the employer's site.
	DirectApply bool `json:"directApply"`
	RecruiterID int  `json:"recruiterId,omitempty"`

	Company      Company      `json:"company"`
	Salary       Salary       `json:"salary"`
	Location     Location     `json:"location"`
	Requirements Requirements `json:"requirements"`
	Timeline     Timeline     `json:"timeline"`
	Activity     Activity     `json:"activity"`
	Content      Content      `json:"content"`

	// Badges are labels the board shows that no other field already carries.
	Badges []string `json:"badges,omitempty"`
	// OtherFacts are rows the parser recognised as facts but has no field for,
	// which is where a newly added row shows up before it earns one.
	OtherFacts []string `json:"otherFacts,omitempty"`
}

// Remote reports whether the work can be done remotely.
func (d JobDetail) Remote() bool { return d.Location.Remote() }

// Normalized projects the detail onto the shape every source shares, so a
// caller mixing detail reads with searches handles one type.
func (d JobDetail) Normalized() NormalizedJob {
	job := NormalizedJob{
		SourceKey:   d.SourceKey,
		Title:       d.Title,
		Company:     d.Company.Name,
		Remote:      d.Remote(),
		URL:         d.URL,
		Description: d.Content.Description,
		Raw:         d,
	}
	if d.ExternalID != "" {
		job.ExternalID = ptr(d.ExternalID)
	}
	if where := d.Location.String(); where != "" {
		job.Location = ptr(where)
	}
	if d.Timeline.PostedAt != 0 {
		job.PostedAt = ptr(time.Unix(d.Timeline.PostedAt, 0).UTC().Format(time.RFC3339))
	}
	// ExperienceMinYears is a floor, so it takes the softer bound where the
	// posting names one.
	if d.Requirements.MinimumExperienceYears > 0 {
		job.ExperienceMinYears = ptr(int(d.Requirements.MinimumExperienceYears))
	}
	if lang, ok := d.Requirements.Language("English"); ok && lang.Level != LevelUnknown {
		job.EnglishLevel = ptr(string(lang.Level))
	}
	if d.Salary.Min > 0 {
		job.SalaryEstimateMin = ptr(d.Salary.Min)
	}
	if d.Salary.Max > 0 {
		job.SalaryEstimateMax = ptr(d.Salary.Max)
	}
	if d.Salary.Currency != "" {
		job.SalaryEstimateCurrency = ptr(d.Salary.Currency)
	}
	if d.Salary.Estimate != "" {
		job.SalaryEstimateRaw = ptr(d.Salary.Estimate)
	}
	if d.Salary.Raw != "" {
		job.SalaryRaw = ptr(d.Salary.Raw)
	}
	return job
}

// JobDetailFrom projects a NormalizedJob into a JobDetail, so a source that
// cannot parse a posting page in full still answers in the shared shape.
//
// Only what the job states is filled: the rest stays zero, which is how a
// caller tells "this source does not report views" from "this posting has no
// views". It is the floor every source meets, not a substitute for a real
// JobDetailReader.
func JobDetailFrom(job NormalizedJob) JobDetail {
	detail := JobDetail{
		SourceKey: job.SourceKey,
		URL:       job.URL,
		Title:     job.Title,
		Company:   Company{Name: job.Company},
		Content:   Content{Description: plainText(job.Description)},
		Timeline:  Timeline{ScrapedAt: time.Now().Unix()},
	}
	if job.ExternalID != nil {
		detail.ExternalID = *job.ExternalID
	}
	if job.Remote {
		detail.Location.Mode = WorkModeRemote
		detail.Location.Modes = []WorkMode{WorkModeRemote}
	} else {
		detail.Location.Mode = WorkModeUnknown
	}

	detail.Location.Scope = LocationUnknown
	if job.Location != nil && *job.Location != "" {
		detail.Location.Scope, detail.Location.Countries = LocationCountries, []string{*job.Location}
	}

	if job.SalaryEstimateMin != nil {
		detail.Salary.Min = *job.SalaryEstimateMin
	}
	if job.SalaryEstimateMax != nil {
		detail.Salary.Max = *job.SalaryEstimateMax
	}
	if job.SalaryEstimateCurrency != nil {
		detail.Salary.Currency = *job.SalaryEstimateCurrency
	}
	if job.SalaryEstimateRaw != nil {
		detail.Salary.Estimate = *job.SalaryEstimateRaw
	} else if job.SalaryRaw != nil {
		detail.Salary.Estimate = *job.SalaryRaw
	}

	if job.ExperienceMinYears != nil {
		detail.Requirements.ExperienceYears = float64(*job.ExperienceMinYears)
		detail.Requirements.MinimumExperienceYears = detail.Requirements.ExperienceYears
	}
	if job.EnglishLevel != nil && *job.EnglishLevel != "" {
		detail.Requirements.Languages = []LanguageReq{{
			Language: "English",
			Level:    LanguageLevel(*job.EnglishLevel),
		}}
	}
	if job.PostedAt != nil {
		if t, err := time.Parse(time.RFC3339, *job.PostedAt); err == nil {
			detail.Timeline.PostedAt = t.Unix()
		}
	}
	return detail
}

// plainText mirrors internal/jobdetail.PlainText for the projection path.
// The table is duplicated rather than shared because jobdetail imports model,
// so model cannot import it back.
func plainText(s string) string { return spaceReplacer.Replace(s) }

var spaceReplacer = strings.NewReplacer(
	"\u00a0", " ", // no-break space
	"\u202f", " ", // narrow no-break space
	"\u2007", " ", // figure space
	"\u2009", " ", // thin space
	"\ufeff", "", // zero-width no-break space
)

func ptr[T any](v T) *T { return &v }
