// Package jobdetail holds the normalisation every source needs when it reads a
// posting page into a model.JobDetail: turning a board's wording for work
// modes, language levels, salary bands, locations and dates into the shared
// vocabulary.
//
// The selectors stay in each adapter — only two boards ever agree on markup —
// but the vocabulary must not, or "Full Remote" and "Remote work" become two
// different values and no caller can filter on either.
package jobdetail

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nonamecat19/job-scraper/model"
)

var (
	// listSepRe splits "Poland, Czechia" and "Countries of Europe or Ukraine".
	listSepRe = regexp.MustCompile(`(?i)\s*,\s*|\s+or\s+`)
	yearsRe   = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\+?\s*years?`)
	monthsRe  = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\+?\s*months?`)
	cefrRe    = regexp.MustCompile(`\b([ABC][12])\b`)
)

// PlainText normalises the fixed-width spaces a board's typesetter inserts —
// chiefly U+00A0, which Ukrainian and Russian typography glues after short
// words — into ordinary spaces.
//
// They are invisible in rendered HTML but not in plain text: they survive into
// stored descriptions, defeat a naive split on " ", and make a full-text search
// for "PwC is" miss a posting that reads "PwC\u00a0is". Line structure is left
// alone, so paragraphs still separate.
func PlainText(s string) string {
	return spaceReplacer.Replace(s)
}

var spaceReplacer = strings.NewReplacer(
	"\u00a0", " ", // no-break space
	"\u202f", " ", // narrow no-break space
	"\u2007", " ", // figure space
	"\u2009", " ", // thin space
	"\ufeff", "", // zero-width no-break space, which is never a space at all
)

// SplitList breaks a board's inline list into its parts. Boards use a comma, a
// bare "or", or both.
func SplitList(raw string) []string {
	var out []string
	for _, part := range listSepRe.Split(raw, -1) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Years reads an experience figure as a number of years. It accepts both units
// so a board stating "18 months" yields 1.5 rather than 18.
func Years(text string) float64 {
	if m := yearsRe.FindStringSubmatch(text); m != nil {
		return parseFloat(m[1])
	}
	if m := monthsRe.FindStringSubmatch(text); m != nil {
		return parseFloat(m[1]) / 12
	}
	return 0
}

func parseFloat(raw string) float64 {
	v, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	return v
}

// WorkModes reads every mode a row offers, in the order listed. "Hybrid
// Remote" is one mode, not two, so hybrid is matched before remote is looked
// for.
func WorkModes(text string) []model.WorkMode {
	var modes []model.WorkMode
	seen := map[model.WorkMode]bool{}

	for _, part := range SplitList(text) {
		lower := strings.ToLower(part)
		var mode model.WorkMode
		switch {
		case strings.Contains(lower, "hybrid"):
			mode = model.WorkModeHybrid
		case strings.Contains(lower, "remote"), strings.Contains(lower, "anywhere"):
			mode = model.WorkModeRemote
		case strings.Contains(lower, "office"), strings.Contains(lower, "on-site"), strings.Contains(lower, "onsite"):
			mode = model.WorkModeOffice
		default:
			continue
		}
		if !seen[mode] {
			seen[mode] = true
			modes = append(modes, mode)
		}
	}
	return modes
}

// WorkModeFromSchema reads schema.org's jobLocationType, which is the most
// reliable signal where a board publishes it. It returns unknown for anything
// else, leaving the visible page to answer.
func WorkModeFromSchema(jobLocationType string) model.WorkMode {
	switch strings.ToUpper(strings.TrimSpace(jobLocationType)) {
	case "TELECOMMUTE":
		return model.WorkModeRemote
	case "HYBRID":
		return model.WorkModeHybrid
	}
	return model.WorkModeUnknown
}

// Locations reads where candidates may be based, reserving the sentinel boards
// use for "anywhere" so it is never mistaken for a place name.
func Locations(raw string) (model.LocationScope, []string) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return model.LocationUnknown, nil
	case worldwideRe.MatchString(raw):
		return model.LocationWorldwide, nil
	}
	return model.LocationCountries, SplitList(raw)
}

var worldwideRe = regexp.MustCompile(`(?i)^(worldwide|anywhere|global|any country)$`)

// LanguageLevel maps a board's wording onto CEFR. An explicit code wins; the
// wording is the only signal for a native speaker, whose row carries no code.
func LanguageLevel(text string) model.LanguageLevel {
	if m := cefrRe.FindStringSubmatch(text); m != nil {
		return model.LanguageLevel(strings.ToUpper(m[1]))
	}
	lower := strings.ToLower(text)
	for _, entry := range levelWords {
		if strings.Contains(lower, entry.word) {
			return entry.level
		}
	}
	return model.LevelUnknown
}

// levelWords is ordered: "pre-intermediate" contains "intermediate" and must be
// tested first, or every A2 reads as B1.
var levelWords = []struct {
	word  string
	level model.LanguageLevel
}{
	{"native", model.LevelC2},
	{"proficient", model.LevelC2},
	{"advanced", model.LevelC1},
	{"fluent", model.LevelC1},
	{"upper", model.LevelB2},
	{"pre-intermediate", model.LevelA2},
	{"elementary", model.LevelA2},
	{"intermediate", model.LevelB1},
	{"beginner", model.LevelA1},
}

// SalaryTier reads a band published instead of a range, counting the marks a
// board uses to rank a job against its own market data.
func SalaryTier(marks string) model.SalaryTier {
	switch strings.Count(marks, "$") {
	case 1:
		return model.TierBelowAverage
	case 2:
		return model.TierAverage
	case 3:
		return model.TierAboveAverage
	case 4:
		return model.TierTopRange
	}
	return model.TierUnknown
}

// SalaryTierLabel renders a tier for display.
func SalaryTierLabel(tier model.SalaryTier) string {
	switch tier {
	case model.TierBelowAverage:
		return "below average"
	case model.TierAverage:
		return "around average"
	case model.TierAboveAverage:
		return "above average"
	case model.TierTopRange:
		return "top range"
	}
	return ""
}

// Count reads a figure a board may have grouped with spaces or commas —
// "1 204 views".
func Count(raw string) int {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}

// TimeIn parses a stamp against the first layout that fits and returns Unix
// seconds, or zero. loc is the board's wall clock: most print local time with
// no zone at all.
func TimeIn(loc *time.Location, raw string, layouts ...string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if loc == nil {
		loc = time.UTC
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// DayMonth reads a date printed without a year — "9 July" — against the year
// the posting belongs to. A date that would fall in the future belongs to the
// year before: a posting published "23 December" and read in January went up
// last year, not next.
func DayMonth(loc *time.Location, raw string, year int) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if loc == nil {
		loc = time.UTC
	}
	if unix := TimeIn(loc, raw, "2 January 2006", "January 2, 2006"); unix != 0 {
		return unix
	}
	t, err := time.ParseInLocation("2 January", raw, loc)
	if err != nil {
		if t, err = time.ParseInLocation("January 2", raw, loc); err != nil {
			return 0
		}
	}
	dated := time.Date(year, t.Month(), t.Day(), 0, 0, 0, 0, loc)
	if dated.After(time.Now().AddDate(0, 0, 1)) {
		dated = dated.AddDate(-1, 0, 0)
	}
	return dated.Unix()
}

// Location returns the named timezone, falling back to UTC on a machine with no
// timezone database rather than failing the parse.
func Location(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
