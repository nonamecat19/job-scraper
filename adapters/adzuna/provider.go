package adzuna

import (
	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/ports"
)

// Key is this source's registry key.
const Key = "adzuna"

// New builds the source with the credentials to fall back on.
//
// The per-search config map takes precedence over these: a consumer that stores
// credentials per user passes them there and leaves these blank, while one with
// a single application-wide account sets them once here.
func New(appID, appKey, country string) ports.JobSource {
	return Source{AppID: appID, AppKey: appKey, Country: country}
}

// Provider is the factory entry for this source. It builds the credential-free
// form: Adzuna's keys arrive through the per-search config map.
func Provider() adapter.Provider {
	return adapter.Simple(Key, func(adapter.Deps) ports.JobSource { return Source{} })
}

func init() { adapter.Register(Provider()) }
