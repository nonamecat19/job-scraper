package adapters

import (
	"context"
	"net/url"
	"strconv"

	"github.com/job-finder/jobscraper/htmlutil"
	"github.com/job-finder/jobscraper/httpjson"
	"github.com/job-finder/jobscraper/model"
	"github.com/job-finder/jobscraper/strutil"
)

type remotiveJob struct {
	ID                        int64  `json:"id"`
	Title                     string `json:"title"`
	CompanyName               string `json:"company_name"`
	CandidateRequiredLocation string `json:"candidate_required_location"`
	URL                       string `json:"url"`
	Description               string `json:"description"`
	PublicationDate           string `json:"publication_date"`
	Salary                    string `json:"salary"`
}

type remotiveResponse struct {
	Jobs []remotiveJob `json:"jobs"`
}

type RemotiveAdapter struct{}

func (RemotiveAdapter) Key() string            { return "remotive" }
func (RemotiveAdapter) Kind() model.SourceKind { return model.SourceKindAPI }

func (RemotiveAdapter) Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error) {
	params := url.Values{"search": {query.Keywords}, "limit": {"50"}}
	var res remotiveResponse
	if err := httpjson.GetJSON(ctx, nil, "https://remotive.com/api/remote-jobs", params, &res); err != nil {
		return nil, err
	}
	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   "remotive",
			ExternalID:  strutil.Ptr(strconv.FormatInt(j.ID, 10)),
			Title:       j.Title,
			Company:     j.CompanyName,
			Location:    strutil.NilIfEmpty(j.CandidateRequiredLocation),
			Remote:      true,
			SalaryRaw:   strutil.NilIfEmpty(j.Salary),
			URL:         j.URL,
			Description: htmlutil.HTMLToText(j.Description),
			PostedAt:    strutil.NilIfEmpty(j.PublicationDate),
			Raw:         j,
		})
	}
	return jobs, nil
}

func (RemotiveAdapter) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	var res remotiveResponse
	if err := httpjson.GetJSON(ctx, nil, "https://remotive.com/api/remote-jobs", url.Values{"limit": {"1"}}, &res); err != nil {
		return false, nil
	}
	return res.Jobs != nil, nil
}
