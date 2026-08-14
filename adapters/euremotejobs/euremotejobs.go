// Package euremotejobs reads https://euremotejobs.com/, a WP Job Manager
// (Jobify theme) board. Listings come from the theme's own AJAX endpoint —
// the same one the "Load More Jobs" button drives — which returns a JSON
// envelope wrapping a rendered HTML fragment rather than a data API. A single
// posting page carries a schema.org JobPosting block instead, which is why
// detail reads (euremotejobs_detail.go) do not reuse this file's HTML
// parsing.
package euremotejobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nonamecat19/job-scraper/internal/strutil"
	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

const (
	euremotejobsBaseURL = "https://euremotejobs.com"
	// euremotejobsAjaxURL is the theme's own listings endpoint — what the
	// search form and "Load More Jobs" button both call. It differs from
	// WP Job Manager's stock job_manager_get_listings admin-ajax action in
	// one load-bearing way: it is the only one of the two that actually
	// honours a region filter.
	euremotejobsAjaxURL  = euremotejobsBaseURL + "/jm-ajax/get_listings/"
	euremotejobsPerPage  = 20
	euremotejobsMaxPages = 5

	// euremotejobsUserAgent matches the Scraper's own GET identity: the site's
	// edge blocks the AJAX endpoint's default net/http User-Agent outright.
	euremotejobsUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// euremotejobsRegionIDs maps the site's own region/country picker (a
// taxonomy, not free text) onto the term IDs the search form submits.
// Captured from the "search_region" <select> on /job-listings/. The board
// accepts a location a caller passes only insofar as it names one of these —
// its own free-text "search_location" field is a geocoder that comes back
// near-empty for a plain country name, so it is not used here.
var euremotejobsRegionIDs = map[string]string{
	"amer": "434", "canada": "105", "costa rica": "2660", "mexico": "539",
	"north america": "2681", "us": "111", "united states": "111",
	"apac": "427", "asia": "651", "australia": "517", "bangladesh": "2659",
	"china": "2959", "india": "160", "indonesia": "2938", "japan": "2704",
	"malaysia": "2706", "new zealand": "2557", "pakistan": "2958", "singapore": "2500",
	"bosnia and herzegovina": "3217", "chile": "3231", "eastern europe": "3196",
	"emea": "72", "africa": "167", "ghana": "165", "kenya": "213", "morocco": "3021",
	"nigeria": "2657", "senegal": "2808", "belarus": "2731", "dubai": "2900",
	"egypt": "436", "ethiopia": "3110", "georgia": "115", "jordan": "2930",
	"kazakhstan": "3000", "lebanon": "3052", "south africa": "2490", "uae": "405",
	"europe": "90", "albania": "2980", "armenia": "426", "austria": "110",
	"belgium": "247", "bulgaria": "188", "croatia": "152", "cyprus": "2483",
	"czech republic": "2734", "czechia": "2510", "denmark": "109", "estonia": "189",
	"finland": "104", "france": "2473", "germany": "2474", "greece": "190",
	"hungary": "162", "iceland": "392", "ireland": "2476", "italy": "2475",
	"latvia": "2513", "lithuania": "2511", "luxembourg": "249", "macedonia": "258",
	"malta": "221", "netherlands": "98", "norway": "251", "poland": "101",
	"portugal": "2484", "romania": "2497", "serbia": "208", "slovakia": "2508",
	"slovenia": "259", "spain": "2479", "sweden": "155", "switzerland": "100",
	"turkey": "226", "uk": "2524", "united kingdom": "2653", "ukraine": "116",
	"latam": "252", "argentina": "382", "brazil": "186", "colombia": "198",
	"uruguay": "2815", "moldova": "2733", "north macedonia": "2818", "peru": "2998",
	"philippines": "2708", "saudi arabia": "2967", "south korea": "2661",
	"thailand": "2662", "tunisia": "2966", "uzbekistan": "3047", "vietnam": "2957",
	"worldwide": "73",
}

// regionID resolves a caller's free-text location against the board's own
// region taxonomy. It returns "" — no filter — when the location names
// nothing the board recognises, rather than guessing.
func regionID(location string) string {
	return euremotejobsRegionIDs[strings.ToLower(strings.TrimSpace(location))]
}

// configTags reads the "tags" entry a caller may set in Search's config map —
// the board's own "Filter by tag" pills (Go, Python, Typescript, ...) have no
// home in model.SearchQuery, so they travel through the per-source config
// like every other extension this library does not model generically.
// Accepts []string or []any of strings, so a config store round-tripped
// through JSON still works.
func configTags(config map[string]any) []string {
	raw, ok := config["tags"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				tags = append(tags, s)
			}
		}
		return tags
	default:
		return nil
	}
}

// euremotejobsPostedAgoRe reads the card's relative timestamp — "Posted 51
// minutes ago", "Posted 3 weeks ago". The detail page gives an exact one; the
// card does not.
var euremotejobsPostedAgoRe = regexp.MustCompile(`(?i)posted\s+(\d+)\s+(minute|hour|day|week|month|year)s?\s+ago`)

type Source struct {
	Scraping ports.Scraper
}

func (Source) Key() string            { return Key }
func (Source) Kind() model.SourceKind { return model.SourceKindScrape }

// NeedsDetail reports true: the listing AJAX fragment carries no description,
// only title, company, location, job type and a relative post date.
func (Source) NeedsDetail() bool { return true }

func (a Source) Search(ctx context.Context, query model.SearchQuery, config map[string]any) ([]model.NormalizedJob, error) {
	keywords := strings.TrimSpace(query.Keywords)
	var location string
	if query.Location != nil {
		location = strings.TrimSpace(*query.Location)
	}
	region := regionID(location)
	if location != "" && region == "" {
		slog.Warn("euremotejobs: location not recognised, searching unfiltered", "location", location)
	}
	tags := configTags(config)

	var allJobs []model.NormalizedJob
	for page := 1; page <= euremotejobsMaxPages; page++ {
		select {
		case <-ctx.Done():
			return allJobs, ctx.Err()
		default:
		}

		res, err := a.fetchListings(ctx, keywords, region, tags, page)
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("euremotejobs: search: %w", err)
			}
			slog.Warn("euremotejobs: page fetch failed", "page", page, "error", err)
			break
		}

		jobs := parseListingsHTML(res.HTML)
		if len(jobs) == 0 {
			break
		}
		allJobs = append(allJobs, jobs...)

		if page >= res.MaxNumPages {
			break
		}
	}

	if location != "" {
		allJobs = filterByLocation(allJobs, location)
	}

	if len(allJobs) == 0 {
		slog.Warn("euremotejobs: returned 0 jobs", "keywords", keywords)
	}
	return allJobs, nil
}

// filterByLocation narrows a result set down to jobs that name location
// themselves. The board's region taxonomy is hierarchical: asking for
// "Europe" also returns a job tagged only "UK" or only "Spain", because those
// are child terms of the Europe region server-side. This re-checks each job's
// own printed location list rather than trusting which term matched there, so
// a caller asking for "Europe" gets jobs that say Europe.
func filterByLocation(jobs []model.NormalizedJob, location string) []model.NormalizedJob {
	filtered := jobs[:0]
	for _, job := range jobs {
		if job.Location == nil {
			continue
		}
		for _, part := range strings.Split(*job.Location, ",") {
			if strings.EqualFold(strings.TrimSpace(part), location) {
				filtered = append(filtered, job)
				break
			}
		}
	}
	return filtered
}

type euremotejobsAjaxResponse struct {
	FoundJobs   bool   `json:"found_jobs"`
	MaxNumPages int    `json:"max_num_pages"`
	HTML        string `json:"html"`
}

// fetchListings drives the theme's own search endpoint — what the search
// form and "Load More Jobs" button both call. It needs no authentication and
// no nonce.
//
// A region or tag filter cannot be passed as a top-level field: the endpoint
// only reads them out of form_data, a second, inner url-encoded blob
// mirroring what the visible form serialises. This is the same endpoint's own
// doing, not a WP Job Manager convention — captured by watching the live
// search form and its "Filter by tag" cloud.
//
// tags are matched by the board's own tag term name — "Go", "Python",
// "Typescript" (the site's own capitalisation, not "TypeScript") — exactly as
// the pills under "Filter by tag:" on /job-listings/ read. Several tags OR
// together, the same as clicking more than one pill.
func (a Source) fetchListings(ctx context.Context, keywords, region string, tags []string, page int) (euremotejobsAjaxResponse, error) {
	formData := url.Values{}
	formData.Set("search_keywords", keywords)
	if region != "" {
		formData.Set("search_region", region)
	}
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			formData.Add("job_tag[]", tag)
		}
	}

	form := url.Values{}
	form.Set("search_keywords", keywords)
	form.Set("search_location", "")
	form.Set("page", strconv.Itoa(page))
	form.Set("per_page", strconv.Itoa(euremotejobsPerPage))
	form.Set("orderby", "featured")
	form.Set("order", "DESC")
	form.Set("show_pagination", "false")
	form.Set("form_data", formData.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, euremotejobsAjaxURL, strings.NewReader(form.Encode()))
	if err != nil {
		return euremotejobsAjaxResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", euremotejobsUserAgent)
	req.Header.Set("Referer", euremotejobsBaseURL+"/job-listings/")
	req.Header.Set("Accept", "*/*")

	resp, err := a.Scraping.HTTPClient().Do(req)
	if err != nil {
		return euremotejobsAjaxResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return euremotejobsAjaxResponse{}, fmt.Errorf("ajax returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return euremotejobsAjaxResponse{}, err
	}

	var res euremotejobsAjaxResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return euremotejobsAjaxResponse{}, fmt.Errorf("decode ajax response: %w", err)
	}
	return res, nil
}

// parseListingsHTML reads the rendered <li class="job_listing"> fragment the
// AJAX endpoint returns.
func parseListingsHTML(fragment string) []model.NormalizedJob {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil
	}

	var jobs []model.NormalizedJob
	doc.Find("li.job_listing").Each(func(_ int, li *goquery.Selection) {
		title := strings.TrimSpace(li.Find(".job_listing-title").First().Text())
		href, _ := li.Find("a.job_listing-clickbox").First().Attr("href")
		href = canonicalPostingURL(href)
		if title == "" || href == "" {
			return
		}

		company := strings.TrimSpace(li.Find(".job_listing-company strong").First().Text())
		if company == "" {
			company = "Unknown"
		}

		location := strings.TrimSpace(li.Find(".job_listing-location a.google_map_link").First().Text())

		var jobTypes []string
		li.Find(".job_listing-meta li.job_listing-type").Each(func(_ int, t *goquery.Selection) {
			if v := strings.TrimSpace(t.Text()); v != "" {
				jobTypes = append(jobTypes, v)
			}
		})

		postedText := strings.TrimSpace(li.Find(".job_listing-meta li.job_listing-date").First().Text())

		jobs = append(jobs, model.NormalizedJob{
			SourceKey:   Key,
			ExternalID:  externalIDFromListItem(li),
			Title:       title,
			Company:     company,
			Location:    strutil.NilIfEmpty(location),
			Remote:      true, // the whole board is remote-only listings.
			URL:         href,
			Description: strings.Join(jobTypes, ", "),
			PostedAt:    relativePostedAt(postedText),
			Raw:         map[string]string{"jobTypes": strings.Join(jobTypes, ", "), "postedText": postedText},
		})
	})
	return jobs
}

// euremotejobsListItemIDRe reads the numeric post ID off the fragment's own
// li#job_listing-47831.
var euremotejobsListItemIDRe = regexp.MustCompile(`^job_listing-(\d+)$`)

func externalIDFromListItem(li *goquery.Selection) *string {
	id, _ := li.Attr("id")
	m := euremotejobsListItemIDRe.FindStringSubmatch(id)
	if m == nil {
		return nil
	}
	return strutil.Ptr(m[1])
}

// relativePostedAt turns "Posted 51 minutes ago" into an approximate RFC3339
// stamp. The card gives no exact date; ReadJobDetail reads the real one off
// the posting's schema.org block.
func relativePostedAt(text string) *string {
	m := euremotejobsPostedAgoRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	var d time.Duration
	switch strings.ToLower(m[2]) {
	case "minute":
		d = time.Duration(n) * time.Minute
	case "hour":
		d = time.Duration(n) * time.Hour
	case "day":
		d = time.Duration(n) * 24 * time.Hour
	case "week":
		d = time.Duration(n) * 7 * 24 * time.Hour
	case "month":
		d = time.Duration(n) * 30 * 24 * time.Hour
	case "year":
		d = time.Duration(n) * 365 * 24 * time.Hour
	default:
		return nil
	}
	t := time.Now().Add(-d).UTC().Format(time.RFC3339)
	return &t
}

// canonicalPostingURL resolves a possibly-relative href against the site and
// drops the query string, which the theme decorates with a tracking token.
func canonicalPostingURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	base, err := url.Parse(euremotejobsBaseURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	resolved := base.ResolveReference(ref)
	resolved.RawQuery = ""
	resolved.Fragment = ""
	return resolved.String()
}

func (a Source) HealthCheck(ctx context.Context, _ map[string]any) (bool, error) {
	res, err := a.fetchListings(ctx, "", "", nil, 1)
	if err != nil {
		return false, nil
	}
	return res.FoundJobs, nil
}
