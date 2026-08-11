package adapters

import (
	"context"
	"fmt"

	"github.com/job-finder/jobscraper/httpjson"
	"github.com/job-finder/jobscraper/model"
	"github.com/job-finder/jobscraper/rosterport"
	"github.com/job-finder/jobscraper/strutil"
)

type ashbyJob struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Location        string `json:"location"`
	IsRemote        bool   `json:"isRemote"`
	JobURL          string `json:"jobUrl"`
	DescriptionHTML string `json:"descriptionHtml"`
	PublishedAt     string `json:"publishedAt"`
}

type ashbyResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type AshbyAdapter struct {
	Roster rosterport.RosterPort
	boardRunState
}

func (a *AshbyAdapter) Key() string            { return "ashby" }
func (a *AshbyAdapter) Kind() model.SourceKind { return model.SourceKindAPI }

func (a *AshbyAdapter) Search(ctx context.Context, _ model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	return runBoardVendor(ctx, a.Roster, &a.boardRunState, "ashby", a.fetchEmployer)
}

func (a *AshbyAdapter) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	return vendorHealthCheck(ctx, a.Roster, "ashby", a.fetchEmployer)
}

func (a *AshbyAdapter) fetchEmployer(ctx context.Context, employer rosterport.EmployerBoard) (int, []model.NormalizedJob, error) {
	url := fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s", employer.EmployerIdentifier)
	var res ashbyResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, url, nil, &res)
	if err != nil {
		return status, nil, err
	}
	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   a.Key(),
			ExternalID:  strutil.Ptr(j.ID),
			Title:       j.Title,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(j.Location),
			Remote:      j.IsRemote || remoteWordRe.MatchString(j.Location),
			URL:         j.JobURL,
			Description: j.DescriptionHTML,
			PostedAt:    strutil.NilIfEmpty(j.PublishedAt),
			Raw:         j,
		})
	}
	return status, jobs, nil
}
