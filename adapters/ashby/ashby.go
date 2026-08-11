// Package ashby reads employer job boards hosted on Ashby.
//
// It is one Fetcher plugged into adapters/atsboard, which owns the roster walk
// and the outcome bookkeeping shared by every ATS vendor.
package ashby

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
const Key = "ashby"

const boardURL = "https://api.ashbyhq.com/posting-api/job-board/%s"

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

// Fetcher reads one Ashby board. It holds no state, so the zero value is ready
// to use.
type Fetcher struct{}

var _ atsboard.Fetcher = Fetcher{}

func (Fetcher) Vendor() string { return Key }

func (f Fetcher) FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (int, []model.NormalizedJob, error) {
	var res ashbyResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, fmt.Sprintf(boardURL, employer.EmployerIdentifier), nil, &res)
	if err != nil {
		return status, nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:  Key,
			ExternalID: strutil.Ptr(j.ID),
			Title:      j.Title,
			Company:    employer.DisplayName,
			Location:   strutil.NilIfEmpty(j.Location),
			// Ashby states remoteness explicitly; the location text is only
			// consulted for boards that leave the flag unset.
			Remote:      j.IsRemote || jobtext.IsRemote(j.Location),
			URL:         j.JobURL,
			Description: j.DescriptionHTML,
			PostedAt:    strutil.NilIfEmpty(j.PublishedAt),
			Raw:         j,
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
