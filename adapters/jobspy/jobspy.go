package jobspy

import (
	"context"
	"time"

	"github.com/nonamecat19/job-scraper/internal/httpjson"
	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
)

type jobspySidecarJob struct {
	ID          *string `json:"id"`
	Site        string  `json:"site"`
	Title       string  `json:"title"`
	Company     *string `json:"company"`
	Location    *string `json:"location"`
	IsRemote    *bool   `json:"is_remote"`
	Salary      *string `json:"salary"`
	JobURL      string  `json:"job_url"`
	Description *string `json:"description"`
	DatePosted  *string `json:"date_posted"`
}

type jobspySearchRequest struct {
	Site          string `json:"site"`
	SearchTerm    string `json:"search_term"`
	Location      string `json:"location,omitempty"`
	IsRemote      bool   `json:"is_remote"`
	ResultsWanted int    `json:"results_wanted"`
}

type jobspySearchResponse struct {
	Jobs []jobspySidecarJob `json:"jobs"`
}

type jobspyHealthResponse struct {
	OK bool `json:"ok"`
}

type Source struct {
	URL string
}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindSidecar }

func (a Source) baseURL(config map[string]any) string {
	return strutil.StringOr(config["url"], a.URL)
}

func (a Source) Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error) {
	site := "linkedin"
	if s, ok := config["site"].(string); ok && s != "" {
		site = s
	}
	location := ""
	if query.Location != nil {
		location = *query.Location
	}
	remote := false
	if query.Remote != nil {
		remote = *query.Remote
	}

	var res jobspySearchResponse
	err := httpjson.PostJSON(ctx, nil, a.baseURL(config)+"/search", jobspySearchRequest{
		Site:          site,
		SearchTerm:    query.Keywords,
		Location:      location,
		IsRemote:      remote,
		ResultsWanted: 30,
	}, &res, 120*time.Second)
	if err != nil {
		return nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		company := "Unknown"
		if j.Company != nil && *j.Company != "" {
			company = *j.Company
		}
		remoteVal := false
		if j.IsRemote != nil {
			remoteVal = *j.IsRemote
		}
		description := ""
		if j.Description != nil {
			description = *j.Description
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   "jobspy",
			ExternalID:  j.ID,
			Title:       j.Title,
			Company:     company,
			Location:    j.Location,
			Remote:      remoteVal,
			SalaryRaw:   j.Salary,
			URL:         j.JobURL,
			Description: description,
			PostedAt:    j.DatePosted,
			Raw:         j,
		})
	}
	return jobs, nil
}

func (a Source) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	var res jobspyHealthResponse
	if err := httpjson.GetJSON(ctx, nil, a.baseURL(config)+"/health", nil, &res); err != nil {
		return false, nil
	}
	return res.OK, nil
}
