package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/nonamecat19/jobscraper/httpjson"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/rosterport"
	"github.com/nonamecat19/jobscraper/strutil"
)

type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	CreatedAt  int64  `json:"createdAt"`
	Categories struct {
		Location   string `json:"location"`
		Commitment string `json:"commitment"`
	} `json:"categories"`
	DescriptionPlain string `json:"descriptionPlain"`
	Description      string `json:"description"`
}

type LeverAdapter struct {
	Roster rosterport.RosterPort
	boardRunState
}

func (a *LeverAdapter) Key() string            { return "lever" }
func (a *LeverAdapter) Kind() model.SourceKind { return model.SourceKindAPI }

func (a *LeverAdapter) Search(ctx context.Context, _ model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	return runBoardVendor(ctx, a.Roster, &a.boardRunState, "lever", a.fetchEmployer)
}

func (a *LeverAdapter) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	return vendorHealthCheck(ctx, a.Roster, "lever", a.fetchEmployer)
}

func (a *LeverAdapter) fetchEmployer(ctx context.Context, employer rosterport.EmployerBoard) (int, []model.NormalizedJob, error) {
	url := fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", employer.EmployerIdentifier)
	var res []leverPosting
	status, err := httpjson.GetJSONStatus(ctx, nil, url, nil, &res)
	if err != nil {
		return status, nil, err
	}
	jobs := make([]model.NormalizedJob, 0, len(res))
	for _, p := range res {
		desc := p.DescriptionPlain
		if desc == "" {
			desc = p.Description
		}
		var postedAt *string
		if p.CreatedAt > 0 {
			postedAt = strutil.Ptr(time.UnixMilli(p.CreatedAt).UTC().Format(time.RFC3339))
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   a.Key(),
			ExternalID:  strutil.Ptr(p.ID),
			Title:       p.Text,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(p.Categories.Location),
			Remote:      remoteWordRe.MatchString(p.Categories.Location) || remoteWordRe.MatchString(p.Text),
			URL:         p.HostedURL,
			Description: desc,
			PostedAt:    postedAt,
			Raw:         p,
		})
	}
	return status, jobs, nil
}
