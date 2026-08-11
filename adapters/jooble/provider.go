package jooble

import (
	"github.com/nonamecat19/job-scraper/adapter"
	"github.com/nonamecat19/job-scraper/ports"
)

// Key is this source's registry key.
const Key = "jooble"

// New builds the source with the API key to fall back on. The per-search config
// map takes precedence, so a consumer holding a key per user can leave this
// blank.
func New(apiKey string) ports.JobSource { return Source{APIKey: apiKey} }

// Provider is the factory entry for this source. It builds the key-free form:
// Jooble's API key arrives through the per-search config map.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return Source{} })
}

func init() { adapter.Register(Provider()) }
