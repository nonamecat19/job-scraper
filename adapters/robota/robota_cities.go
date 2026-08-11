package robota

import (
	"context"
	"strings"
	"sync"

	"github.com/nonamecat19/job-scraper/internal/httpjson"
)

// cityLang is the language city names are reported in. The vacancy payload
// itself carries only the Russian spelling ("Харьков"), while the dictionary
// holds all three, so resolving through it is what makes a robota city
// comparable with the Ukrainian names other sources report.
//
// The dictionary also carries "en" ("Kharkiv"), should a caller ever want the
// library to speak one language across every source.
const cityLang = "ua"

// cityDictionary caches robota's city list for the life of the process. The
// list is static reference data — 3,700 rows that change when Ukraine gains a
// city — so it is fetched once and shared.
//
// A failure is never fatal: the caller falls back to the name the vacancy
// itself carried, which is right but Russian.
type cityDictionary struct {
	once  sync.Once
	mu    sync.RWMutex
	names map[uint64]cityNames
}

type cityNames struct {
	UA string `json:"ua"`
	RU string `json:"ru"`
	EN string `json:"en"`
	ID uint64 `json:"id"`
}

func (c cityNames) in(lang string) string {
	switch lang {
	case "ua":
		return c.UA
	case "en":
		return c.EN
	}
	return c.RU
}

var cities cityDictionary

// name returns the city's name in lang, or "" when the dictionary could not be
// read or does not know the id.
func (d *cityDictionary) name(ctx context.Context, id uint64, lang string) string {
	if id == 0 {
		return ""
	}
	d.once.Do(func() { d.load(ctx) })

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.names[id].in(lang)
}

func (d *cityDictionary) load(ctx context.Context) {
	d.loadFrom(ctx, robotaAPIBase+"/dictionary/city")
}

// loadFrom is load with the endpoint named, so a test can serve its own.
func (d *cityDictionary) loadFrom(ctx context.Context, endpoint string) {
	var list []cityNames
	if err := httpjson.GetJSON(ctx, nil, endpoint, nil, &list); err != nil {
		return
	}

	names := make(map[uint64]cityNames, len(list))
	for _, city := range list {
		names[city.ID] = city
	}

	d.mu.Lock()
	d.names = names
	d.mu.Unlock()
}

// cityName resolves a vacancy's city, preferring the dictionary's Ukrainian
// spelling and falling back to whatever the vacancy itself stated.
func cityName(ctx context.Context, id uint64, fallback string) string {
	if name := cities.name(ctx, id, cityLang); name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}
