package ports

import (
	"context"

	"github.com/nonamecat19/job-scraper/model"
)

// JobDetailReader is implemented by sources that can read a posting page into a
// full model.JobDetail — the structured record: company, salary, location,
// requirements, timeline, activity.
//
// It is the richer half of PostingReader, which every implementor must also
// satisfy: URL claiming is shared, so a source states once, in
// MatchesPostingURL, which pages are its own.
//
// Optional. A source without it still yields a JobDetail through the client,
// projected from whatever its PostingReader returns — the same shape with
// fewer fields filled, so a caller never has to branch on which source a job
// came from.
//
// Implementors must honour the PostingReader rules, plus two of their own:
//
//  1. A field the page does not state is left at its zero value. An absent
//     salary is not a range of zero, and an absent view count is not zero
//     views — callers distinguish the two by whether the field is set.
//  2. Timeline.ScrapedAt is set to the moment of the read, because the
//     activity figures beside it are only true as of then.
type JobDetailReader interface {
	PostingReader

	// ReadJobDetail reads one posting page into a JobDetail. It returns a
	// partial record rather than erroring when the page loads but omits
	// fields; it errors only when the page could not be read at all.
	ReadJobDetail(ctx context.Context, rawURL string, config map[string]any) (model.JobDetail, error)
}
