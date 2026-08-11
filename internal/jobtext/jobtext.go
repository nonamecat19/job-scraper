// Package jobtext holds the small text judgements several sources need to make
// about a posting — chiefly, whether its title or location says it is remote.
//
// It exists so those judgements stay consistent: a source that rolls its own
// remote test will disagree with the next one at the edges, and a posting's
// Remote flag is something a consumer filters on.
package jobtext

import "regexp"

// remoteWord matches a bare mention of remote work in English. It is
// deliberately loose: source-specific phrasing (Ukrainian "віддалено",
// "work from home" variants) belongs in the source that needs it, layered on
// top of this.
var remoteWord = regexp.MustCompile(`(?i)remote`)

// IsRemote reports whether any of the given fields mentions remote work.
// Passing title and location together is the usual call: sites put the fact in
// whichever of the two suits them.
func IsRemote(fields ...string) bool {
	for _, f := range fields {
		if remoteWord.MatchString(f) {
			return true
		}
	}
	return false
}
