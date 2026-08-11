// Package greenhouse reads employer job boards hosted on Greenhouse.
//
// It is one Fetcher plugged into adapters/atsboard, which owns the roster walk
// and the outcome bookkeeping shared by every ATS vendor.
package greenhouse

import (
	"context"
	"fmt"
	"strconv"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/adapters/atsboard"
	"github.com/nonamecat19/jobscraper/internal/httpjson"
	"github.com/nonamecat19/jobscraper/internal/jobtext"
	"github.com/nonamecat19/jobscraper/internal/strutil"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

// Key is this source's registry key.
const Key = "greenhouse"

// boardURL serves a board's whole posting list. content=true asks Greenhouse to
// inline each description, which is what lets this source skip a detail pass.
const boardURL = "https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true"

type greenhouseJob struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	AbsURL   string `json:"absolute_url"`
	UpdateAt string `json:"updated_at"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
}

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

// Fetcher reads one Greenhouse board. It holds no state, so the zero value is
// ready to use.
type Fetcher struct{}

var _ atsboard.Fetcher = Fetcher{}

func (Fetcher) Vendor() string { return Key }

func (f Fetcher) FetchEmployer(ctx context.Context, employer ports.EmployerBoard) (int, []model.NormalizedJob, error) {
	var res greenhouseResponse
	status, err := httpjson.GetJSONStatus(ctx, nil, fmt.Sprintf(boardURL, employer.EmployerIdentifier), nil, &res)
	if err != nil {
		return status, nil, err
	}

	jobs := make([]model.NormalizedJob, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   Key,
			ExternalID:  strutil.Ptr(strconv.FormatInt(j.ID, 10)),
			Title:       j.Title,
			Company:     employer.DisplayName,
			Location:    strutil.NilIfEmpty(j.Location.Name),
			Remote:      jobtext.IsRemote(j.Location.Name, j.Title),
			URL:         j.AbsURL,
			Description: j.Content,
			PostedAt:    strutil.NilIfEmpty(j.UpdateAt),
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

// HealthChecker probes a single board that is not on the roster yet, so a
// consumer can validate an employer identifier before adding it.
func HealthChecker() ports.EmployerHealthChecker { return atsboard.HealthChecker(Fetcher{}) }

func init() { adapter.Register(Provider()) }
