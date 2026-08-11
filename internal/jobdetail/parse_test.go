package jobdetail

import (
	"strings"
	"testing"
	"time"

	"github.com/nonamecat19/job-scraper/model"
)

func TestPlainText(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		// U+00A0 is what Ukrainian typography glues after short words.
		{"PwC\u00a0is a\u00a0global network", "PwC is a global network"},
		{"5+\u00a0years\u2009experience", "5+ years experience"},
		{"\ufeffLeading BOM", "Leading BOM"},
		// Line structure is left alone: paragraphs must still separate.
		{"First line\n\nSecond line", "First line\n\nSecond line"},
		{"already plain", "already plain"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := PlainText(tc.raw); got != tc.want {
			t.Errorf("PlainText(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"Poland, Czechia", []string{"Poland", "Czechia"}},
		// A bare "or" separates entries as readily as a comma.
		{"Countries of Europe or Ukraine", []string{"Countries of Europe", "Ukraine"}},
		{"Ukraine", []string{"Ukraine"}},
		{"", nil},
	}
	for _, tc := range tests {
		got := SplitList(tc.raw)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("SplitList(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestYears(t *testing.T) {
	tests := []struct {
		text string
		want float64
	}{
		{"Only from 5 years of experience", 5},
		{"3 years", 3},
		{"1 year", 1},
		{"4.5 years", 4.5},
		// Months are converted rather than read as years.
		{"18 months", 1.5},
		{"no experience", 0},
		{"", 0},
	}
	for _, tc := range tests {
		if got := Years(tc.text); got != tc.want {
			t.Errorf("Years(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestWorkModes(t *testing.T) {
	tests := []struct {
		text string
		want []model.WorkMode
	}{
		{"Office, Remote, Hybrid Remote", []model.WorkMode{model.WorkModeOffice, model.WorkModeRemote, model.WorkModeHybrid}},
		{"Full Remote", []model.WorkMode{model.WorkModeRemote}},
		// "Hybrid Remote" is one mode, not hybrid plus a second remote.
		{"Hybrid Remote", []model.WorkMode{model.WorkModeHybrid}},
		{"Office", []model.WorkMode{model.WorkModeOffice}},
		{"On-site", []model.WorkMode{model.WorkModeOffice}},
		{"Work from anywhere", []model.WorkMode{model.WorkModeRemote}},
		{"", nil},
	}
	for _, tc := range tests {
		got := WorkModes(tc.text)
		if len(got) != len(tc.want) {
			t.Errorf("WorkModes(%q) = %v, want %v", tc.text, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("WorkModes(%q)[%d] = %q, want %q", tc.text, i, got[i], tc.want[i])
			}
		}
	}
}

func TestWorkModeFromSchema(t *testing.T) {
	tests := map[string]model.WorkMode{
		"TELECOMMUTE": model.WorkModeRemote,
		"telecommute": model.WorkModeRemote,
		"HYBRID":      model.WorkModeHybrid,
		"":            model.WorkModeUnknown,
		"WHATEVER":    model.WorkModeUnknown,
	}
	for in, want := range tests {
		if got := WorkModeFromSchema(in); got != want {
			t.Errorf("WorkModeFromSchema(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocations(t *testing.T) {
	if scope, countries := Locations("Worldwide"); scope != model.LocationWorldwide || countries != nil {
		t.Errorf(`Locations("Worldwide") = %q %v, want worldwide and no list`, scope, countries)
	}
	if scope, _ := Locations("Anywhere"); scope != model.LocationWorldwide {
		t.Errorf(`Locations("Anywhere") = %q, want worldwide`, scope)
	}
	scope, countries := Locations("Countries of Europe, Ukraine")
	if scope != model.LocationCountries || len(countries) != 2 || countries[1] != "Ukraine" {
		t.Errorf("Locations(countries) = %q %v", scope, countries)
	}
	if scope, _ := Locations(""); scope != model.LocationUnknown {
		t.Errorf(`Locations("") = %q, want unknown`, scope)
	}
}

func TestLanguageLevel(t *testing.T) {
	tests := []struct {
		text string
		want model.LanguageLevel
	}{
		{"English B2 - Upper Intermediate", model.LevelB2},
		{"Ukrainian Native", model.LevelC2},
		// "pre-intermediate" contains "intermediate": the word table's order
		// decides whether this is A2 or wrongly B1.
		{"English Pre-Intermediate", model.LevelA2},
		{"English Intermediate", model.LevelB1},
		{"English Advanced", model.LevelC1},
		{"English Beginner", model.LevelA1},
		{"English", model.LevelUnknown},
	}
	for _, tc := range tests {
		if got := LanguageLevel(tc.text); got != tc.want {
			t.Errorf("LanguageLevel(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestSalaryTier(t *testing.T) {
	tests := map[string]model.SalaryTier{
		"$":    model.TierBelowAverage,
		"$$":   model.TierAverage,
		"$$$":  model.TierAboveAverage,
		"$$$$": model.TierTopRange,
		"":     model.TierUnknown,
	}
	for marks, want := range tests {
		if got := SalaryTier(marks); got != want {
			t.Errorf("SalaryTier(%q) = %q, want %q", marks, got, want)
		}
		if want != model.TierUnknown && SalaryTierLabel(want) == "" {
			t.Errorf("SalaryTierLabel(%q) is empty", want)
		}
	}
}

func TestCount(t *testing.T) {
	tests := map[string]int{
		"400":   400,
		"1 204": 1204,
		"1,204": 1204,
		"":      0,
		"none":  0,
	}
	for raw, want := range tests {
		if got := Count(raw); got != want {
			t.Errorf("Count(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestTimeIn(t *testing.T) {
	kyiv := Location("Europe/Kyiv")

	if got, want := TimeIn(kyiv, "2026-08-10T15:16:46.775635", "2006-01-02T15:04:05.999999"),
		time.Date(2026, 8, 10, 15, 16, 46, 0, kyiv).Unix(); got != want {
		t.Errorf("TimeIn = %d, want %d", got, want)
	}
	// The first layout that fits wins; a stamp matching none yields zero.
	if got := TimeIn(kyiv, "13:26 05.08.2026", "2006-01-02", "15:04 02.01.2006"); got == 0 {
		t.Error("TimeIn tried only the first layout")
	}
	if got := TimeIn(kyiv, "not a date", "2006-01-02"); got != 0 {
		t.Errorf("TimeIn(garbage) = %d, want 0", got)
	}
	if got := TimeIn(nil, "2026-08-10", "2006-01-02"); got == 0 {
		t.Error("TimeIn(nil location) should fall back to UTC, not fail")
	}
}

func TestDayMonth(t *testing.T) {
	kyiv := Location("Europe/Kyiv")

	if got, want := DayMonth(kyiv, "9 July", 2026),
		time.Date(2026, 7, 9, 0, 0, 0, 0, kyiv).Unix(); got != want {
		t.Errorf("DayMonth = %d, want %d", got, want)
	}
	// An explicit year is taken as given.
	if got, want := DayMonth(kyiv, "9 July 2024", 2026),
		time.Date(2024, 7, 9, 0, 0, 0, 0, kyiv).Unix(); got != want {
		t.Errorf("DayMonth(with year) = %d, want %d", got, want)
	}
	// A date that would land in the future belongs to the year before.
	next := time.Now().In(kyiv).AddDate(0, 2, 0)
	got := DayMonth(kyiv, next.Format("2 January"), next.Year())
	if parsed := time.Unix(got, 0).In(kyiv); !parsed.Before(time.Now()) {
		t.Errorf("DayMonth(%q) = %s, want a date in the past", next.Format("2 January"), parsed)
	}
	if got := DayMonth(kyiv, "", 2026); got != 0 {
		t.Errorf(`DayMonth("") = %d, want 0`, got)
	}
}

// Location must never fail the parse on a machine with no timezone database.
func TestLocationFallsBackToUTC(t *testing.T) {
	if Location("Nowhere/Nothing") != time.UTC {
		t.Error("Location(unknown) should fall back to UTC")
	}
}
