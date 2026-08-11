package remoteok

import (
	"fmt"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/ports"
)

// Key is this source's registry key.
const Key = "remoteok"

// New builds the source. An empty apiURL selects the public endpoint; tests
// point it at a fixture server.
func New(scraper ports.Scraper, apiURL string) (ports.JobSource, error) {
	if scraper == nil {
		return nil, fmt.Errorf("remoteok: a Scraper is required")
	}
	return Source{Scraping: scraper, APIURL: apiURL}, nil
}

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.ProviderFunc{
		SourceKey: Key,
		Build: func(deps adapter.Deps) (ports.JobSource, error) {
			return New(deps.Scraper, "")
		},
	}
}

func init() { adapter.Register(Provider()) }
