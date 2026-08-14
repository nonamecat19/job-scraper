package euremotejobs

import (
	"fmt"

	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "euremotejobs"

// New builds the source. It scrapes HTML pages and posts to the site's own
// WP Job Manager AJAX endpoint, so it needs a Scraper.
func New(scraper ports.Scraper) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("euremotejobs: a Scraper is required")
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
