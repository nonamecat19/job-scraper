package glassdoor

import (
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "glassdoor"

// New builds the source. This host challenges plain requests, so the source
// fetches through the retrieval ladder rather than the scraper alone — but it
// still needs the scraper for the requests that never get blocked.
func New(scraper ports.Scraper, retriever ports.Retriever) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("glassdoor: a Scraper is required")
	}
	if retriever == nil {
		return nil, fmt.Errorf("glassdoor: a Retriever is required — this host challenges plain requests")
	}
	return Source{Scraping: scraper, Retrieval: retriever}, nil
}

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			return New(deps.Scraper, deps.Retriever)
		},
	}
}

func init() { adapter.Register(Provider()) }
