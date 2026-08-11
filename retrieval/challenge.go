package retrieval

import (
	"strings"

	"github.com/nonamecat19/jobscraper/ports"
)

// MarkerDetector classifies a fetched body by looking for known interstitial
// and block markers in it. It is the default ports.ChallengeDetector; a
// consumer facing a host with unusual challenge markup supplies its own through
// WithChallengeDetector rather than forking the engine.
//
// The zero value uses the built-in marker lists. Setting either field replaces
// that list outright, so a consumer that wants to extend rather than replace
// should append to DefaultChallengeMarkers.
type MarkerDetector struct {
	ChallengeMarkers []string
	RefusalMarkers   []string

	// MinBodyBytes is the length below which a body with a real status code is
	// treated as a challenge — an interstitial is almost always shorter than
	// the page it is standing in for. Zero selects the default.
	MinBodyBytes int
}

// DefaultChallengeMarkers are the strings that indicate a soft block: a bot
// check standing between the client and the page.
var DefaultChallengeMarkers = []string{
	"checking your browser",
	"attention required",
	"just a moment",
	"cf-please-wait",
	"verify you are human",
	"access denied",
	"captcha",
	"you have been blocked",
	"noindex",
}

// DefaultRefusalMarkers are the strings that indicate a hard block: the host
// has decided not to serve this client, and a costlier rung will not change
// its mind.
var DefaultRefusalMarkers = []string{
	"access denied",
	"you have been blocked",
	"request blocked",
	"rate limit exceeded",
}

const defaultMinBodyBytes = 200

// DefaultDetector is the detector the engine uses when none is supplied.
var DefaultDetector ports.ChallengeDetector = MarkerDetector{}

func (d MarkerDetector) IsChallenged(body string, statusCode int) bool {
	if statusCode == 403 || statusCode == 429 {
		return true
	}
	minBytes := d.MinBodyBytes
	if minBytes == 0 {
		minBytes = defaultMinBodyBytes
	}
	if len(body) < minBytes && statusCode > 0 {
		return true
	}
	return containsAny(body, d.challengeMarkers())
}

func (d MarkerDetector) IsRefused(body string, statusCode int) bool {
	if statusCode == 403 || statusCode == 429 || statusCode == 503 {
		return true
	}
	return containsAny(body, d.refusalMarkers())
}

func (d MarkerDetector) challengeMarkers() []string {
	if d.ChallengeMarkers != nil {
		return d.ChallengeMarkers
	}
	return DefaultChallengeMarkers
}

func (d MarkerDetector) refusalMarkers() []string {
	if d.RefusalMarkers != nil {
		return d.RefusalMarkers
	}
	return DefaultRefusalMarkers
}

func containsAny(body string, markers []string) bool {
	lower := strings.ToLower(body)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
