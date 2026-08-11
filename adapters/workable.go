package adapters

import (
	"context"
	"fmt"

	"github.com/nonamecat19/jobscraper/httpjson"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/rosterport"
	"github.com/nonamecat19/jobscraper/strutil"
)

type workableJob struct {
	Shortcode   string `json:"shortcode"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Url         string `json:"url"`
	Remote      bool   `json:"remote"`
	CreatedAt   string `json:"created_at"`
	Location    struct {
		City        string `json:"city"`
		CountryName string `json:"country_name"`
	} `json:"location"`
}

type workableResponse struct {
	Jobs []workableJob `json:"jobs"`
}

type WorkableAdapter struct {
	Roster rosterport.RosterPort
	boardRunState
}

func (a *WorkableAdapter) Key() string            { return "workable" }
func (a *WorkableAdapter) Kind() model.SourceKind { return model.SourceKindAPI }

func (a *WorkableAdapter) Search(ctx context.Context, _ model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	return runBoardVendor(ctx, a.Roster, &a.boardRunState, "workable", a.fetchEmployer)
}

func (a *WorkableAdapter) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	return vendorHealthCheck(ctx, a.Roster, "workable", a.fetchEmployer)
}

func (a *WorkableAdapter) fetchEmployer(ctx context.Context, employer rosterport.EmployerBoard) (int, []model.NormalizedJob, error) {
	url := fmt.Sprintf("https://apply.workable.com/api/v1/widget/accounts/%s", employer.EmployerIdentifier)
	var res workableResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, url, nil, &res)
	if err != nil {
		return status, nil, err
	}
	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		loc := j.Location.City
		if j.Location.CountryName != "" {
			if loc != "" {
				loc += ", "
			}
			loc += j.Location.CountryName
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   a.Key(),
			ExternalID:  strutil.Ptr(j.Shortcode),
			Title:       j.Title,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(loc),
			Remote:      j.Remote || remoteWordRe.MatchString(loc),
			URL:         j.Url,
			Description: j.Description,
			PostedAt:    strutil.NilIfEmpty(j.CreatedAt),
			Raw:         j,
		})
	}
	return status, jobs, nil
}
