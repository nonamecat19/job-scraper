package model

type SourceKind string

const (
	SourceKindAPI     SourceKind = "api"
	SourceKindScrape  SourceKind = "scrape"
	SourceKindSidecar SourceKind = "sidecar"
	// SourceKindManual backs hand-entered vacancies on hosts no adapter reads.
	// It is never crawled — its adapter's Search fails permanently (041 D4).
	SourceKindManual SourceKind = "manual"
)

type NormalizedJob struct {
	SourceKey   string  `json:"sourceKey"`
	ExternalID  *string `json:"externalId,omitempty"`
	Title       string  `json:"title"`
	Company     string  `json:"company"`
	Location    *string `json:"location,omitempty"`
	Remote      bool    `json:"remote"`
	SalaryRaw   *string `json:"salaryRaw,omitempty"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	PostedAt    *string `json:"postedAt,omitempty"`
	Raw         any     `json:"raw"`

	ExperienceLevel        *string `json:"experienceLevel,omitempty"`
	ExperienceMinYears     *int    `json:"experienceMinYears,omitempty"`
	EnglishLevel           *string `json:"englishLevel,omitempty"`
	SalaryEstimateRaw      *string `json:"salaryEstimateRaw,omitempty"`
	SalaryEstimateMin      *int    `json:"salaryEstimateMin,omitempty"`
	SalaryEstimateMax      *int    `json:"salaryEstimateMax,omitempty"`
	SalaryEstimateCurrency *string `json:"salaryEstimateCurrency,omitempty"`
}

type SearchQuery struct {
	Keywords        string   `json:"keywords"`
	Location        *string  `json:"location,omitempty"`
	Remote          *bool    `json:"remote,omitempty"`
	SalaryMin       *float64 `json:"salaryMin,omitempty"`
	Country         *string  `json:"country,omitempty"`
	Sources         []string `json:"sources,omitempty"`
	SubscriptionURL string   `json:"subscriptionUrl,omitempty"`
}

type JobSourceDto struct {
	ID      string         `json:"id"`
	Key     string         `json:"key"`
	Kind    SourceKind     `json:"kind"`
	Enabled bool           `json:"enabled"`
	Healthy bool           `json:"healthy"`
	Config  map[string]any `json:"config"`
}
