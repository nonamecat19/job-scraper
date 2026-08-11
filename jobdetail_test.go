package jobscraper

import (
	"context"
	"strings"
	"testing"

	"github.com/nonamecat19/job-scraper/model"
	"github.com/nonamecat19/job-scraper/ports"
)

// plainSource reads a posting but cannot parse one in full: the floor every
// source meets.
type plainSource struct{ job model.NormalizedJob }

func (plainSource) Key() string            { return "plain" }
func (plainSource) Kind() model.SourceKind { return model.SourceKindScrape }
func (plainSource) Search(context.Context, model.SearchQuery, map[string]any) ([]model.NormalizedJob, error) {
	return nil, nil
}
func (plainSource) HealthCheck(context.Context, map[string]any) (bool, error) { return true, nil }
func (plainSource) MatchesPostingURL(rawURL string) bool {
	return strings.Contains(rawURL, "plain.example")
}
func (p plainSource) ReadPosting(context.Context, string, map[string]any) (model.NormalizedJob, error) {
	return p.job, nil
}

// richSource parses a posting page in full.
type richSource struct{ plainSource }

func (richSource) Key() string { return "rich" }
func (richSource) MatchesPostingURL(rawURL string) bool {
	return strings.Contains(rawURL, "rich.example")
}
func (richSource) ReadJobDetail(context.Context, string, map[string]any) (model.JobDetail, error) {
	return model.JobDetail{
		SourceKey: "rich",
		Title:     "Parsed In Full",
		Company:   model.Company{Name: "Rich Co", Domain: "DefTech"},
		Salary:    model.Salary{Min: 4000, Max: 5500, Currency: "USD"},
		Activity:  model.Activity{Views: 400},
	}, nil
}

var (
	_ ports.PostingReader   = plainSource{}
	_ ports.JobDetailReader = richSource{}
)

func TestClientJobDetailUsesTheDetailReader(t *testing.T) {
	client, err := New(WithSources(), WithSource(richSource{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	detail, err := client.JobDetail(context.Background(), "https://rich.example/jobs/1")
	if err != nil {
		t.Fatalf("JobDetail: %v", err)
	}
	if detail.Title != "Parsed In Full" || detail.Company.Domain != "DefTech" {
		t.Errorf("detail = %+v, want the source's own parse", detail)
	}
	if detail.Salary.Min != 4000 || detail.Activity.Views != 400 {
		t.Errorf("detail lost the rich fields: %+v", detail)
	}
}

// A source with no JobDetailReader still answers in the shared shape, so a
// caller never branches on which site the URL belongs to.
func TestClientJobDetailProjectsFromPostingReader(t *testing.T) {
	location := "Kyiv"
	src := plainSource{job: model.NormalizedJob{
		SourceKey:   "plain",
		Title:       "Go Engineer",
		Company:     "Plain Co",
		Location:    &location,
		Remote:      true,
		Description: "We build things.",
	}}

	client, err := New(WithSources(), WithSource(src))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	detail, err := client.JobDetail(context.Background(), "https://plain.example/jobs/1")
	if err != nil {
		t.Fatalf("JobDetail: %v", err)
	}

	if detail.SourceKey != "plain" || detail.Title != "Go Engineer" || detail.Company.Name != "Plain Co" {
		t.Errorf("detail = %+v, want the posting projected", detail)
	}
	if !detail.Remote() || detail.Location.Mode != model.WorkModeRemote {
		t.Errorf("Remote lost in projection: %+v", detail.Location)
	}
	if detail.Location.Scope != model.LocationCountries || detail.Location.String() != "Kyiv" {
		t.Errorf("Location = %+v, want the stated location carried over", detail.Location)
	}
	// What the source cannot know stays zero rather than being invented.
	if detail.Salary.Min != 0 || detail.Activity.Views != 0 || detail.Requirements.ExperienceYears != 0 {
		t.Errorf("projection invented data: %+v", detail)
	}
	if detail.Timeline.ScrapedAt == 0 {
		t.Error("ScrapedAt unset — the record must date itself")
	}
}

func TestClientJobDetailUnknownURL(t *testing.T) {
	client, err := New(WithSources(), WithSource(plainSource{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	if _, err := client.JobDetail(context.Background(), "https://nobody.example/jobs/1"); err == nil {
		t.Fatal("expected an error for a URL no source claims")
	}
}
