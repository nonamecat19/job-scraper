package djinni

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/job-scraper/internal/htmlutil"
	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

const djinniMaxSubscriptionPages = 50

var djinniRemoteRe = regexp.MustCompile(`(?i)remote|віддалено`)

type Source struct {
	Scraping ports.Scraper
	Session  ports.SessionProvider
}

func (d Source) authHeaders(ctx context.Context) (map[string]string, error) {
	headers := map[string]string{}
	if d.Session == nil {
		return headers, nil
	}
	cookie, err := d.Session.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	setDjinniCookie(headers, cookie)
	return headers, nil
}

func setDjinniCookie(headers map[string]string, cookie string) {
	if cookie != "" {
		headers["Cookie"] = "sessionid=" + cookie
	} else {
		delete(headers, "Cookie")
	}
}

func (d Source) fetchDoc(ctx context.Context, pageURL string, headers map[string]string) (*goquery.Document, error) {
	doc, err := d.fetchParse(ctx, pageURL, headers)
	if err != nil {
		return nil, err
	}
	if !djinniIsLoginPage(doc) {
		return doc, nil
	}
	if d.Session == nil {
		return nil, fmt.Errorf("djinni requires login but no credentials configured: set DJINNI_EMAIL and DJINNI_PASSWORD")
	}
	cookie, err := d.Session.Refresh(ctx)
	if err != nil {
		return nil, fmt.Errorf("djinni session expired and re-login failed: %w", err)
	}
	setDjinniCookie(headers, cookie)

	doc, err = d.fetchParse(ctx, pageURL, headers)
	if err != nil {
		return nil, err
	}
	if djinniIsLoginPage(doc) {
		return nil, fmt.Errorf("djinni still at login after re-login (check DJINNI_EMAIL/DJINNI_PASSWORD)")
	}
	return doc, nil
}

func (d Source) fetchParse(ctx context.Context, pageURL string, headers map[string]string) (*goquery.Document, error) {
	html, err := d.Scraping.FetchHTML(ctx, pageURL, headers)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindScrape }

// NeedsDetail reports that a search-listing card carries only Djinni's own
// truncated summary, not the full posting — a detail-page pass (FetchDetail)
// is required to get the whole description.
func (Source) NeedsDetail() bool { return true }

func (d Source) Search(ctx context.Context, query model.SearchQuery, _ map[string]any) ([]model.NormalizedJob, error) {
	headers, err := d.authHeaders(ctx)
	if err != nil {
		return nil, err
	}

	if query.SubscriptionURL != "" {
		jobs, err := d.scrapeSubscription(ctx, query.SubscriptionURL, headers)
		if len(jobs) == 0 && err == nil {
			slog.Warn("djinni subscription returned 0 jobs — markup may have changed", "url", query.SubscriptionURL)
		}
		return jobs, err
	}

	params := url.Values{}
	if query.Keywords != "" {
		params.Set("keywords", query.Keywords)
	}
	if query.Remote != nil && *query.Remote {
		params.Set("employment", "remote")
	}
	pageURL := "https://djinni.co/jobs/?" + params.Encode()

	doc, err := d.fetchDoc(ctx, pageURL, headers)
	if err != nil {
		return nil, err
	}
	jobs := parseDjinniCards(doc)
	if len(jobs) == 0 {
		slog.Warn("djinni returned 0 jobs — markup may have changed")
	}
	return jobs, nil
}

func (d Source) scrapeSubscription(ctx context.Context, subURL string, headers map[string]string) ([]model.NormalizedJob, error) {
	base, err := url.Parse(subURL)
	if err != nil {
		return nil, fmt.Errorf("djinni: invalid subscription url %q: %w", subURL, err)
	}

	var jobs []model.NormalizedJob
	seenFirstHref := ""
	for page := 1; page <= djinniMaxSubscriptionPages; page++ {
		pageURL := *base
		q := pageURL.Query()
		q.Set("page", strconv.Itoa(page))
		pageURL.RawQuery = q.Encode()

		doc, err := d.fetchDoc(ctx, pageURL.String(), headers)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			slog.Warn("djinni subscription page fetch failed, stopping pagination", "url", pageURL.String(), "error", err)
			break
		}

		cards := parseDjinniCards(doc)
		if len(cards) == 0 {
			break
		}
		if cards[0].URL == seenFirstHref {
			break
		}
		seenFirstHref = cards[0].URL

		jobs = append(jobs, cards...)
	}
	return jobs, nil
}

func djinniIsLoginPage(doc *goquery.Document) bool {
	return doc.Find(`input[name="password"]`).Length() > 0
}

func parseDjinniCards(doc *goquery.Document) []model.NormalizedJob {
	var jobs []model.NormalizedJob
	doc.Find(`[id^="job-item-"], li.list-jobs__item`).Each(func(_ int, item *goquery.Selection) {
		link := item.Find(`a[href^="/jobs/"]`).First()
		href, hasHref := link.Attr("href")
		// The card's link wraps the whole header, so its text is the title,
		// the company and the salary run together. The heading is the title.
		title := strings.TrimSpace(item.Find(`.job-item__position`).First().Text())
		if title == "" {
			title = strings.TrimSpace(link.Text())
		}
		if !hasHref || href == "" || title == "" {
			return
		}

		company := strings.TrimSpace(item.Find(`.job-item__company-name, a[href*="/jobs/company-"], a[href^="/company/"], .js-analytics-company, [data-analytics="company_page"], .job-item__position + div span`).First().Text())
		description := htmlutil.SelectionText(item.Find(`.js-truncated-text, .js-original-text, .text-card`).First())
		salary := strings.TrimSpace(item.Find(`.public-salary-item, .text-success`).First().Text())
		location := strings.TrimSpace(item.Find(`.location-text`).First().Text())

		itemHTML, _ := item.Html()
		itemHTML = strutil.Truncate(itemHTML, 4000)

		full, err := url.Parse(href)
		absURL := href
		if err == nil {
			base, _ := url.Parse("https://djinni.co")
			absURL = base.ResolveReference(full).String()
		}

		segs := strings.Split(strings.Trim(href, "/"), "/")
		var externalID *string
		if len(segs) > 0 && segs[len(segs)-1] != "" {
			externalID = strutil.Ptr(segs[len(segs)-1])
		}

		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   "djinni",
			ExternalID:  externalID,
			Title:       title,
			Company:     strutil.FirstNonEmpty(company, "Unknown"),
			Location:    strutil.NilIfEmpty(location),
			Remote:      djinniRemoteRe.MatchString(item.Text()),
			SalaryRaw:   strutil.NilIfEmpty(salary),
			URL:         absURL,
			Description: description,
			Raw:         map[string]string{"html": itemHTML},
		})
	})
	return jobs
}

type DjinniDetailPatch struct {
	Description string
	SalaryRaw   *string
	Location    *string
	Remote      bool
	PostedAt    *string
	Raw         map[string]string
}

func (d Source) FetchDetail(ctx context.Context, jobURL string, _ map[string]any) (DjinniDetailPatch, error) {
	headers, err := d.authHeaders(ctx)
	if err != nil {
		return DjinniDetailPatch{}, err
	}
	doc, err := d.fetchDoc(ctx, jobURL, headers)
	if err != nil {
		return DjinniDetailPatch{}, err
	}

	description := htmlutil.SelectionText(doc.Find(`.job-post__description, .job-post-page__description, .js-original-text, [data-qa="job-description"], article`).First())
	salary := strings.TrimSpace(doc.Find(`.public-salary-item, .job-additional-info-item .text-success, .text-success`).First().Text())
	location := strings.TrimSpace(doc.Find(`.location-text, .job-additional-info-item .location`).First().Text())
	postedAt, _ := doc.Find(`.job-post__details time, time[datetime]`).First().Attr("datetime")

	remote := djinniRemoteRe.MatchString(doc.Find("body").Text())

	bodyHTML, _ := doc.Find("body").Html()
	bodyHTML = strutil.Truncate(bodyHTML, 8000)

	patch := DjinniDetailPatch{
		Description: description,
		SalaryRaw:   strutil.NilIfEmpty(salary),
		Location:    strutil.NilIfEmpty(location),
		Remote:      remote,
		Raw:         map[string]string{"html": bodyHTML},
	}
	if postedAt != "" {
		patch.PostedAt = &postedAt
	}
	return patch, nil
}

// djinniPostingPathRe matches the single-posting shape: /jobs/<digits>-<slug>.
// A preset-search URL is /jobs or /jobs/ with a query, which this rejects — the
// host is Djinni's either way, but only one of the two is a posting.
var djinniPostingPathRe = regexp.MustCompile(`^/jobs/\d+-`)

func djinniIsHost(host string) bool {
	host = strings.ToLower(host)
	return host == "djinni.co" || host == "www.djinni.co"
}

// ClaimsHost lets manual add tell "this is a Djinni search URL" from "nobody
// reads this host". Both fail, but only the first has a subscription as its
// recovery.
func (Source) ClaimsHost(host string) bool { return djinniIsHost(host) }

// MatchesPostingURL implements adapter.PostingReader. It does no I/O and
// tolerates any input, because it is called for every registered adapter on
// every manual add.
func (Source) MatchesPostingURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if !djinniIsHost(parsed.Host) {
		return false
	}
	return djinniPostingPathRe.MatchString(parsed.Path)
}

// ReadPosting implements ports.PostingReader by delegating to the detail path
// and projecting the result, so a manually added Djinni vacancy is parsed by
// exactly the code that parses a crawled one. Two parsers over one site's
// markup is one parser too many: the second is always the stale one.
//
// Missing fields come back empty rather than as an error: a page that loads but
// omits a description is the fill-in case, not a failure.
func (d Source) ReadPosting(ctx context.Context, rawURL string, config map[string]any) (model.NormalizedJob, error) {
	detail, err := d.ReadJobDetail(ctx, rawURL, config)
	if err != nil {
		return model.NormalizedJob{}, err
	}
	job := detail.Normalized()
	job.URL = djinniCanonicalPostingURL(rawURL)
	if id := djinniPostingID(rawURL); id != "" {
		job.ExternalID = &id
	}
	return job, nil
}

// djinniCanonicalPostingURL drops the query and fragment so the same posting
// reached through a tracking link produces the same dedupe key as a crawl.
func djinniCanonicalPostingURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return rawURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func djinniPostingID(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segs) == 0 {
		return ""
	}
	return segs[len(segs)-1]
}

func (d Source) HealthCheck(ctx context.Context, config map[string]any) (bool, error) {
	html, err := d.Scraping.FetchHTML(ctx, "https://djinni.co/jobs/", nil)
	if err != nil {
		return false, nil
	}
	return strings.Contains(html, "djinni"), nil
}
