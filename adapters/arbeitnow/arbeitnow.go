package arbeitnow

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nonamecat19/jobscraper/internal/htmlutil"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/internal/strutil"
	"github.com/nonamecat19/jobscraper/model"
)

type arbeitnowJob struct {
	Slug        string   `json:"slug"`
	CompanyName string   `json:"company_name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Remote      bool     `json:"remote"`
	URL         string   `json:"url"`
	Tags        []string `json:"tags"`
	JobTypes    []string `json:"job_types"`
	Location    string   `json:"location"`
	CreatedAt   int64    `json:"created_at"`
}

type arbeitnowResponse struct {
	Data  []arbeitnowJob `json:"data"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

type Source struct{}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindAPI }

func (Source) Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error) {
	var collected []arbeitnowJob
	for page := 1; page <= 3; page++ {
		params := url.Values{"page": {strconv.Itoa(page)}}
		var res arbeitnowResponse
		if err := httpjson.GetJSON(ctx, nil, "https://api.arbeitnow.com/api/job-board-api", params, &res); err != nil {
			return nil, err
		}
		collected = append(collected, res.Data...)
		if res.Links.Next == nil {
			break
		}
	}

	terms := strings.Fields(strings.ToLower(query.Keywords))
	jobs := make([]model.NormalizedJob, 0, len(collected))
	for _, j := range collected {
		haystack := strings.ToLower(j.Title + " " + strings.Join(j.Tags, " ") + " " + j.Description)
		matched := len(terms) == 0
		for _, t := range terms {
			if strings.Contains(haystack, t) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		var postedAt *string
		if j.CreatedAt != 0 {
			postedAt = strutil.Ptr(time.Unix(j.CreatedAt, 0).UTC().Format(time.RFC3339))
		}
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   "arbeitnow",
			ExternalID:  strutil.Ptr(j.Slug),
			Title:       j.Title,
			Company:     j.CompanyName,
			Location:    strutil.NilIfEmpty(j.Location),
			Remote:      j.Remote,
			URL:         j.URL,
			Description: htmlutil.HTMLToText(j.Description),
			PostedAt:    postedAt,
			Raw:         j,
		})
	}
	return jobs, nil
}

func (Source) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	var res arbeitnowResponse
	if err := httpjson.GetJSON(ctx, nil, "https://api.arbeitnow.com/api/job-board-api", url.Values{"page": {"1"}}, &res); err != nil {
		return false, nil
	}
	return res.Data != nil, nil
}
