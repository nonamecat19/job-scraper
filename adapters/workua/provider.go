package workua

import (
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "workua"

// New builds the source. It scrapes HTML pages, so it needs a Scraper.
func New(scraper ports.Scraper) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("workua: a Scraper is required")
	}
	return Source{Scraping: scraper}, nil
}

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			return New(deps.Scraper)
		},
	}
}

func init() { adapter.Register(Provider()) }
