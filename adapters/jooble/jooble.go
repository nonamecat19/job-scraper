package jooble

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/nonamecat19/jobscraper/internal/htmlutil"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/internal/jobtext"
	"github.com/nonamecat19/jobscraper/internal/strutil"
	"github.com/nonamecat19/jobscraper/model"
)

type joobleJob struct {
	Title       string `json:"title"`
	CompanyName string `json:"companyName"`
	Location    string `json:"location"`
	Snippet     string `json:"snippet"`
	URL         string `json:"url"`
	Salary      string `json:"salary"`
	Date        string `json:"date"`
	Source      string `json:"source"`
}

type joobleResponse struct {
	TotalCount string      `json:"totalCount"`
	Jobs       []joobleJob `json:"jobs"`
}

type Source struct {
	APIKey string
}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindAPI }

func keywordsFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return ""
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return decoded
}

func (a Source) Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error) {
	apiKey := strutil.StringOr(config["apiKey"], a.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("jooble: apiKey not configured")
	}

	keywords := query.Keywords
	if query.SubscriptionURL != "" {
		if k := keywordsFromURL(query.SubscriptionURL); k != "" {
			keywords = k
		}
	}

	body := map[string]any{
		"keywords": keywords,
		"page":     "1",
	}
	if query.Location != nil && *query.Location != "" {
		body["location"] = *query.Location
	}

	var res joobleResponse
	apiURL := fmt.Sprintf("https://jooble.org/api/%s", apiKey)
	if err := httpjson.PostJSON(ctx, nil, apiURL, body, &res, 0); err != nil {
		return nil, fmt.Errorf("jooble: %w", err)
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		company := j.CompanyName
		if company == "" {
			company = "Unknown"
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   "jooble",
			Title:       j.Title,
			Company:     company,
			Location:    strutil.NilIfEmpty(j.Location),
			Remote:      jobtext.IsRemote(j.Title, j.Snippet),
			SalaryRaw:   strutil.NilIfEmpty(j.Salary),
			URL:         j.URL,
			Description: htmlutil.HTMLToText(j.Snippet),
			PostedAt:    strutil.NilIfEmpty(j.Date),
			Raw:         j,
		})
	}
	return jobs, nil
}

func (a Source) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	_, err := a.Search(ctx, model.SearchQuery{Keywords: "developer"}, config)
	return err == nil, nil
}
