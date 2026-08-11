package manual

import (
	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "manual"

// New builds the source. It is never crawled — it exists so hand-entered
// vacancies on hosts no source reads have a source key to belong to — so its
// Search fails permanently and it needs no dependencies.
func New() ports.JobSource { return Source{} }

// Provider is the factory entry for this source.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return New() })
}

func init() { adapter.Register(Provider()) }
