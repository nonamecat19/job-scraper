// Package all registers every bundled job source with adapter.DefaultCatalog.
//
// Import it for its side effects when you want the full set:
//
//	import _ "github.com/nonamecat19/job-scraper/adapters/all"
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
	_ "github.com/nonamecat19/job-scraper/adapters/adzuna"
	_ "github.com/nonamecat19/job-scraper/adapters/arbeitnow"
	_ "github.com/nonamecat19/job-scraper/adapters/ashby"
	_ "github.com/nonamecat19/job-scraper/adapters/djinni"
	_ "github.com/nonamecat19/job-scraper/adapters/dou"
	_ "github.com/nonamecat19/job-scraper/adapters/euremotejobs"
	_ "github.com/nonamecat19/job-scraper/adapters/glassdoor"
	_ "github.com/nonamecat19/job-scraper/adapters/greenhouse"
	_ "github.com/nonamecat19/job-scraper/adapters/himalayas"
	_ "github.com/nonamecat19/job-scraper/adapters/indeed"
	_ "github.com/nonamecat19/job-scraper/adapters/jobgether"
	_ "github.com/nonamecat19/job-scraper/adapters/jobleads"
	_ "github.com/nonamecat19/job-scraper/adapters/jobspy"
	_ "github.com/nonamecat19/job-scraper/adapters/jooble"
	_ "github.com/nonamecat19/job-scraper/adapters/lever"
	_ "github.com/nonamecat19/job-scraper/adapters/manual"
	_ "github.com/nonamecat19/job-scraper/adapters/remoteok"
	_ "github.com/nonamecat19/job-scraper/adapters/remotive"
	_ "github.com/nonamecat19/job-scraper/adapters/robota"
	_ "github.com/nonamecat19/job-scraper/adapters/smartrecruiters"
	_ "github.com/nonamecat19/job-scraper/adapters/wellfound"
	_ "github.com/nonamecat19/job-scraper/adapters/workable"
	_ "github.com/nonamecat19/job-scraper/adapters/workua"
)
