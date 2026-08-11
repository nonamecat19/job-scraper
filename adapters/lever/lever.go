// Package lever reads employer job boards hosted on Lever.
//
// It is one Fetcher plugged into adapters/atsboard, which owns the roster walk
// and the outcome bookkeeping shared by every ATS vendor.
package lever

import (
	"context"
	"fmt"
	"time"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/adapters/atsboard"
	"github.com/nonamecat19/job-scraper/internal/httpjson"
	"github.com/nonamecat19/job-scraper/internal/jobtext"
	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "lever"

const boardURL = "https://api.lever.co/v0/postings/%s?mode=json"

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

// Fetcher reads one Lever board. It holds no state, so the zero value is ready
// to use.
type Fetcher struct{}

var _ atsboard.Fetcher = Fetcher{}

func (Fetcher) Vendor() string { return Key }

func (f Fetcher) FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (int, []model.NormalizedJob, error) {
	var res []leverPosting
	status, err := httpjson.GetJSONStatus(ctx, nil, fmt.Sprintf(boardURL, employer.EmployerIdentifier), nil, &res)
	if err != nil {
		return status, nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res))
	for _, p := range res {
		// Lever serves both a plain-text and an HTML description. Prefer the
		// plain text: it is what downstream keyword matching reads, and the
		// HTML is only a fallback for postings that lack it.
		desc := p.DescriptionPlain
		if desc == "" {
			desc = p.Description
		}

		var postedAt *string
		if p.CreatedAt > 0 {
			postedAt = strutil.Ptr(time.UnixMilli(p.CreatedAt).UTC().Format(time.RFC3339))
		}

		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   Key,
			ExternalID:  strutil.Ptr(p.ID),
			Title:       p.Text,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(p.Categories.Location),
			Remote:      jobtext.IsRemote(p.Categories.Location, p.Text),
			URL:         p.HostedURL,
			Description: desc,
			PostedAt:    postedAt,
			Raw:         p,
		})
	}
	return status, jobs, nil
}

// New builds the source. It needs a roster of the boards to crawl.
func New(roster ports.Roster) (ports.JobSource, error) {
	return atsboard.New(Fetcher{}, roster)
}

// Provider is the factory entry for this source.
func Provider() adapter.Provider { return atsboard.Provider(Fetcher{}) }

// HealthChecker probes a single board that is not on the roster yet.
func HealthChecker() ports.EmployerHealthChecker { return atsboard.HealthChecker(Fetcher{}) }

func init() { adapter.Register(Provider()) }
