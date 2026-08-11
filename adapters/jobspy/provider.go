package jobspy

import (
	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "jobspy"

// New builds the source pointed at a JobSpy sidecar. The per-search config map
// can override the URL, so a consumer running several sidecars can route each
// search to a different one.
func New(sidecarURL string) ports.JobSource { return Source{URL: sidecarURL} }

// Provider is the factory entry for this source. It builds the URL-free form:
// the sidecar address arrives through the per-search config map.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return Source{} })
}

func init() { adapter.Register(Provider()) }
