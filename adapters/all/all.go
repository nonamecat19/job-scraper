// Package all registers every bundled job source with adapter.DefaultCatalog.
//
// Import it for its side effects when you want the full set:
//
//	import _ "github.com/nonamecat19/jobscraper/adapters/all"
//
// The root jobscraper package imports it already, so a caller using
// jobscraper.New gets every source without doing anything.
//
// Import the individual vendor packages instead when you want a smaller
// binary or a deliberately restricted set — the catalog only ever holds what
// was imported, so the choice is made at link time rather than configured at
// runtime.
package all

import (
	_ "github.com/nonamecat19/jobscraper/adapters/adzuna"
	_ "github.com/nonamecat19/jobscraper/adapters/arbeitnow"
	_ "github.com/nonamecat19/jobscraper/adapters/ashby"
	_ "github.com/nonamecat19/jobscraper/adapters/djinni"
	_ "github.com/nonamecat19/jobscraper/adapters/dou"
	_ "github.com/nonamecat19/jobscraper/adapters/glassdoor"
	_ "github.com/nonamecat19/jobscraper/adapters/greenhouse"
	_ "github.com/nonamecat19/jobscraper/adapters/himalayas"
	_ "github.com/nonamecat19/jobscraper/adapters/indeed"
	_ "github.com/nonamecat19/jobscraper/adapters/jobgether"
	_ "github.com/nonamecat19/jobscraper/adapters/jobleads"
	_ "github.com/nonamecat19/jobscraper/adapters/jobspy"
	_ "github.com/nonamecat19/jobscraper/adapters/jooble"
	_ "github.com/nonamecat19/jobscraper/adapters/lever"
	_ "github.com/nonamecat19/jobscraper/adapters/manual"
	_ "github.com/nonamecat19/jobscraper/adapters/remoteok"
	_ "github.com/nonamecat19/jobscraper/adapters/remotive"
	_ "github.com/nonamecat19/jobscraper/adapters/robota"
	_ "github.com/nonamecat19/jobscraper/adapters/smartrecruiters"
	_ "github.com/nonamecat19/jobscraper/adapters/wellfound"
	_ "github.com/nonamecat19/jobscraper/adapters/workable"
	_ "github.com/nonamecat19/jobscraper/adapters/workua"
)
