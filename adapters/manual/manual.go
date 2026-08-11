package manual

import (
	"context"
	"errors"

	"github.com/nonamecat19/job-scraper/model"
)

// ErrManualNotCrawlable is what the manual source returns when anything tries
// to crawl it. It fails loudly rather than returning an empty result: nothing
// should ever schedule this source, and a silent zero-result run would be
// indistinguishable from a healthy crawl that found nothing (041 D4).
var ErrManualNotCrawlable = errors.New("the manual source is not crawlable — vacancies under it are entered by hand")

// Source exists so hand-entered vacancies on hosts with no PostingReader
// have a registered source to reference. Job.sourceKey is NOT NULL and
// Subscription.sourceKey is a foreign key to JobSource.key, and the sources
// list drops DB rows with no registered adapter — a no-op adapter satisfies
// both without special-casing either.
//
// It deliberately implements no PostingReader: it reads nothing, it claims no
// host, and a URL only reaches it after every real reader has declined.
type Source struct{}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindManual }

func (Source) Search(context.Context, model.SearchQuery, map[string]any) ([]model.NormalizedJob, error) {
	return nil, ErrManualNotCrawlable
}

func (Source) HealthCheck(context.Context, map[string]any) (bool, error) {
	return true, nil
}
