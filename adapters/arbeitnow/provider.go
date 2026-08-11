package arbeitnow

import (
	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/ports"
)

// Key is this source's registry key.
const Key = "arbeitnow"

// New builds the source. Arbeitnow's board is a public JSON feed, so the source
// has no dependencies and the zero value works.
func New() ports.JobSource { return Source{} }

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return New() })
}

func init() { adapter.Register(Provider()) }
