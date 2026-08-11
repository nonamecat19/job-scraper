// Package workable reads employer job boards hosted on Workable.
//
// It is one Fetcher plugged into adapters/atsboard, which owns the roster walk
// and the outcome bookkeeping shared by every ATS vendor.
package workable

import (
	"context"
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/adapters/atsboard"
	"github.com/nonamecat19/job-scraper/internal/httpjson"
	"github.com/nonamecat19/job-scraper/internal/jobtext"
	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "workable"

const boardURL = "https://apply.workable.com/api/v1/widget/accounts/%s"

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

// Fetcher reads one Workable board. It holds no state, so the zero value is
// ready to use.
type Fetcher struct{}

var _ atsboard.Fetcher = Fetcher{}

func (Fetcher) Vendor() string { return Key }

func (f Fetcher) FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (int, []model.NormalizedJob, error) {
	var res workableResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, fmt.Sprintf(boardURL, employer.EmployerIdentifier), nil, &res)
	if err != nil {
		return status, nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		loc := joinLocation(j.Location.City, j.Location.CountryName)
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   Key,
			ExternalID:  strutil.Ptr(j.Shortcode),
			Title:       j.Title,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(loc),
			Remote:      j.Remote || jobtext.IsRemote(loc),
			URL:         j.Url,
			Description: j.Description,
			PostedAt:    strutil.NilIfEmpty(j.CreatedAt),
			Raw:         j,
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
