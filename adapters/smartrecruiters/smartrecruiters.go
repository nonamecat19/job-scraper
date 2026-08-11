// Package smartrecruiters reads employer job boards hosted on SmartRecruiters.
//
// It is one Fetcher plugged into adapters/atsboard, which owns the roster walk
// and the outcome bookkeeping shared by every ATS vendor.
package smartrecruiters

import (
	"context"
	"fmt"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/adapters/atsboard"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/internal/jobtext"
	"github.com/nonamecat19/jobscraper/internal/strutil"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// Key is this source's registry key.
const Key = "smartrecruiters"

const (
	boardURL    = "https://api.smartrecruiters.com/v1/companies/%s/postings"
	fallbackURL = "https://jobs.smartrecruiters.com/%s/%s"
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

// Fetcher reads one SmartRecruiters board. It holds no state, so the zero value
// is ready to use.
type Fetcher struct{}

var _ atsboard.Fetcher = Fetcher{}

func (Fetcher) Vendor() string { return Key }

func (f Fetcher) FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (int, []model.NormalizedJob, error) {
	var res smartRecruitersResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, fmt.Sprintf(boardURL, employer.EmployerIdentifier), nil, &res)
	if err != nil {
		return status, nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Content))
	for _, p := range res.Content {
		loc := joinLocation(p.Location.City, p.Location.Country)

		// Not every posting carries a ref; the canonical public URL is
		// reconstructible from the company and posting IDs when it does not.
		applyURL := p.Ref
		if applyURL == "" {
			applyURL = fmt.Sprintf(fallbackURL, employer.EmployerIdentifier, p.ID)
		}

		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   Key,
			ExternalID:  strutil.Ptr(p.ID),
			Title:       p.Name,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(loc),
			Remote:      p.Location.Remote || jobtext.IsRemote(loc),
			URL:         applyURL,
			Description: p.JobAd.Sections.JobDescription.Text,
			PostedAt:    strutil.NilIfEmpty(p.ReleasedAt),
			Raw:         p,
		})
	}
	return status, jobs, nil
}

// joinLocation renders "City, Country", dropping either part when the board
// left it blank rather than emitting a stray comma.
func joinLocation(city, country string) string {
	switch {
	case city != "" && country != "":
		return city + ", " + country
	case city != "":
		return city
	default:
		return country
	}
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
