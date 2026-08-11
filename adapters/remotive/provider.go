package remotive

import (
	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "remotive"

// New builds the source. Remotive's board is a public JSON feed, so the source
// has no dependencies and the zero value works.
func New() ports.JobSource { return Source{} }

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return New() })
}

func init() { adapter.Register(Provider()) }
