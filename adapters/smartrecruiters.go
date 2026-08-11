package adapters

import (
	"context"
	"fmt"

	"github.com/nonamecat19/jobscraper/httpjson"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/rosterport"
	"github.com/nonamecat19/jobscraper/strutil"
)

type smartRecruitersPosting struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ReleasedAt string `json:"releasedDate"`
	Location   struct {
		City    string `json:"city"`
		Country string `json:"country"`
		Remote  bool   `json:"remote"`
	} `json:"location"`
	JobAd struct {
		Sections struct {
			JobDescription struct {
				Text string `json:"text"`
			} `json:"jobDescription"`
		} `json:"sections"`
	} `json:"jobAd"`
	Ref string `json:"ref"`
}

type smartRecruitersResponse struct {
	Content []smartRecruitersPosting `json:"content"`
}

type SmartRecruitersAdapter struct {
	Roster rosterport.RosterPort
	boardRunState
}

func (a *SmartRecruitersAdapter) Key() string            { return "smartrecruiters" }
func (a *SmartRecruitersAdapter) Kind() model.SourceKind { return model.SourceKindAPI }

func (a *SmartRecruitersAdapter) Search(ctx context.Context, _ model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	return runBoardVendor(ctx, a.Roster, &a.boardRunState, "smartrecruiters", a.fetchEmployer)
}

func (a *SmartRecruitersAdapter) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	return vendorHealthCheck(ctx, a.Roster, "smartrecruiters", a.fetchEmployer)
}

func (a *SmartRecruitersAdapter) fetchEmployer(ctx context.Context, employer rosterport.EmployerBoard) (int, []model.NormalizedJob, error) {
	url := fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings", employer.EmployerIdentifier)
	var res smartRecruitersResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, url, nil, &res)
	if err != nil {
		return status, nil, err
	}
	jobs := make([]model.NormalizedJob, 0, len(res.Content))
	for _, p := range res.Content {
		loc := p.Location.City
		if p.Location.Country != "" {
			if loc != "" {
				loc += ", "
			}
			loc += p.Location.Country
		}
		applyURL := p.Ref
		if applyURL == "" {
			applyURL = fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", employer.EmployerIdentifier, p.ID)
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   a.Key(),
			ExternalID:  strutil.Ptr(p.ID),
			Title:       p.Name,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(loc),
			Remote:      p.Location.Remote || remoteWordRe.MatchString(loc),
			URL:         applyURL,
			Description: p.JobAd.Sections.JobDescription.Text,
			PostedAt:    strutil.NilIfEmpty(p.ReleasedAt),
			Raw:         p,
		})
	}
	return status, jobs, nil
}
