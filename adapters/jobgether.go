package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/jobscraper/htmlutil"
	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/retrieval"
	"github.com/nonamecat19/jobscraper/scraping"
	"github.com/nonamecat19/jobscraper/strutil"
)

const (
	jobgetherMaxSubscriptionPages = 50
	jobgetherRequestDelay         = 500 * time.Millisecond
)

var (
	jobgetherRemoteRe    = regexp.MustCompile(`(?i)\bremote\b|work from home`)
	jobgetherPostedAgoRe = regexp.MustCompile(`(?i)(\d+)\s*(day|hour)s?\s*ago`)
)

type JobgetherAdapter struct {
	Scraping  *scraping.HTTPScraper
	Retrieval retrieval.Service
}

func (JobgetherAdapter) Key() string            { return "jobgether" }
func (JobgetherAdapter) Kind() model.SourceKind { return model.SourceKindScrape }

func (JobgetherAdapter) NeedsDetail() bool { return true }

func (d JobgetherAdapter) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	html, err := d.fetchPage(ctx, "https://jobgether.com/", nil)
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToLower(html), "jobgether"), nil
}

func (d JobgetherAdapter) fetchPage(ctx context.Context, url string, headers map[string]string) (string, error) {
	if d.Retrieval != nil {
		result, err := d.Retrieval.Fetch(ctx, retrieval.FetchRequest{URL: url, Headers: headers})
		if err != nil {
			return "", err
		}
		if result.Outcome.Status == retrieval.PageChallenged {
			return "", fmt.Errorf("jobgether: challenged: %s", result.Outcome.Reason)
		}
		if result.Outcome.Status == retrieval.PageRefused {
			return "", fmt.Errorf("jobgether: refused: %s", result.Outcome.Reason)
		}
		if result.Outcome.Status == retrieval.PageDeferred {
			return "", fmt.Errorf("jobgether: deferred: %s", result.Outcome.Reason)
		}
		return result.Body, nil
	}
	html, err := d.Scraping.FetchHTML(ctx, url, headers)
	if err != nil {
		return "", err
	}
	if jobgetherIsBlockedPage(html) {
		return "", fmt.Errorf("jobgether: request blocked by rate-limit/challenge interstitial: %s", url)
	}
	return html, nil
}

func jobgetherIsBlockedPage(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "rate limit exceeded") && strings.Contains(lower, "too many requests")
}

func (d JobgetherAdapter) Search(ctx context.Context, query model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	if query.SubscriptionURL == "" {
		return nil, fmt.Errorf("jobgether keyword search not implemented — use subscription URL instead")
	}
	jobs, err := d.scrapeSubscription(ctx, query.SubscriptionURL)
	if len(jobs) == 0 && err == nil {
		slog.Warn("jobgether subscription returned 0 jobs — markup may have changed or search has no matches", "url", query.SubscriptionURL)
	}
	return jobs, err
}

func (d JobgetherAdapter) scrapeSubscription(ctx context.Context, subURL string) ([]model.NormalizedJob, error) {
	base, err := url.Parse(subURL)
	if err != nil {
		return nil, fmt.Errorf("jobgether: invalid subscription url %q: %w", subURL, err)
	}

	var jobs []model.NormalizedJob
	seenFirstID := ""
	for page := 1; page <= jobgetherMaxSubscriptionPages; page++ {
		pageURL := *base
		q := pageURL.Query()
		q.Set("page", strconv.Itoa(page))
		pageURL.RawQuery = q.Encode()

		if page > 1 {
			time.Sleep(jobgetherRequestDelay)
		}

		htmlStr, err := d.fetchPage(ctx, pageURL.String(), nil)
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("jobgether subscription fetch: %w", err)
			}
			slog.Warn("jobgether subscription page fetch failed, stopping pagination", "url", pageURL.String(), "error", err)
			break
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("parse jobgether subscription page: %w", err)
			}
			slog.Warn("jobgether subscription page parse failed, stopping pagination", "url", pageURL.String(), "error", err)
			break
		}

		cards := parseJobgetherCards(doc, pageURL.String())
		if len(cards) == 0 {
			break
		}
		firstID := ""
		if cards[0].ExternalID != nil {
			firstID = *cards[0].ExternalID
		}
		if firstID != "" && firstID == seenFirstID {
			break
		}
		seenFirstID = firstID

		jobs = append(jobs, cards...)
	}
	return jobs, nil
}

func parseJobgetherCards(doc *goquery.Document, pageURL string) []model.NormalizedJob {
	var results []model.NormalizedJob

	base, _ := url.Parse(pageURL)
	if base == nil {
		base, _ = url.Parse("https://jobgether.com")
	}

	doc.Find(`.job-card`).Each(func(i int, card *goquery.Selection) {
		titleLink := card.Find(`.job-card__title`).First()
		title := strings.TrimSpace(titleLink.Text())
		href, hasHref := titleLink.Attr("href")
		if title == "" || !hasHref || href == "" {
			return
		}

		absURL := href
		if full, err := url.Parse(href); err == nil {
			absURL = base.ResolveReference(full).String()
		}

		company := strings.TrimSpace(card.Find(`.job-card__company`).First().Text())
		location := strings.TrimSpace(card.Find(`.job-card__location`).First().Text())
		salary := strings.TrimSpace(card.Find(`.job-card__salary`).First().Text())
		posted := strings.TrimSpace(card.Find(`.job-card__posted`).First().Text())
		matchScore := strings.TrimSpace(card.Find(`.job-card__match-score`).First().Text())
		summary := strings.TrimSpace(htmlutil.SelectionText(card.Find(`.job-card__summary`).First()))

		isRemote := jobgetherRemoteRe.MatchString(location) || jobgetherRemoteRe.MatchString(title)

		var externalID *string
		if id, ok := card.Attr("data-job-id"); ok && id != "" {
			externalID = strutil.Ptr(id)
		}

		raw := map[string]any{}
		if matchScore != "" {
			raw["jobgetherMatchScore"] = matchScore
		}

		results = append(results, model.NormalizedJob{
			SourceKey:   "jobgether",
			ExternalID:  externalID,
			Title:       title,
			Company:     company,
			Location:    strutil.NilIfEmpty(location),
			Remote:      isRemote,
			SalaryRaw:   strutil.NilIfEmpty(salary),
			URL:         absURL,
			Description: summary,
			PostedAt:    jobgetherPostedAtFromText(posted),
			Raw:         raw,
		})
	})

	return results
}

func jobgetherPostedAtFromText(text string) *string {
	m := jobgetherPostedAgoRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	var d time.Duration
	switch strings.ToLower(m[2]) {
	case "hour":
		d = time.Duration(n) * time.Hour
	case "day":
		d = time.Duration(n) * 24 * time.Hour
	default:
		return nil
	}
	t := time.Now().Add(-d).Format(time.RFC3339)
	return &t
}

type JobgetherDetailPatch struct {
	Description string
	SalaryRaw   *string
	PostedAt    *string
	Available   bool
	Raw         map[string]any
}

func (d JobgetherAdapter) FetchDetail(ctx context.Context, jobURL string, _ map[string]any) (JobgetherDetailPatch, error) {
	htmlStr, err := d.fetchPage(ctx, jobURL, nil)
	if err != nil {
		return JobgetherDetailPatch{}, fmt.Errorf("jobgether detail fetch: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return JobgetherDetailPatch{}, fmt.Errorf("parse jobgether detail page: %w", err)
	}

	descSel := doc.Find(`.job-detail__description`)
	description := strings.TrimSpace(htmlutil.SelectionText(descSel.First()))
	if description == "" {
		return JobgetherDetailPatch{Available: false}, nil
	}

	salary := strings.TrimSpace(doc.Find(`.job-detail__salary`).First().Text())
	posted := strings.TrimSpace(doc.Find(`.job-detail__posted`).First().Text())
	matchScore := strings.TrimSpace(doc.Find(`.job-detail__match-score`).First().Text())

	raw := map[string]any{}
	if matchScore != "" {
		raw["jobgetherMatchScore"] = matchScore
	}

	return JobgetherDetailPatch{
		Description: description,
		SalaryRaw:   strutil.NilIfEmpty(salary),
		PostedAt:    jobgetherPostedAtFromText(posted),
		Available:   true,
		Raw:         raw,
	}, nil
}
