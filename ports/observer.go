package ports

import (
	"time"

	"github.com/nonamecat19/job-scraper/model"
)

// RunEvent is one notification emitted while a search runs.
type RunEvent struct {
	SourceKey string
	Query     model.SearchQuery
	Jobs      int
	Duration  time.Duration
	Err       error
}

// Observer watches a run without taking part in it. The library calls it for
// every source it starts and finishes; implementations feed logs, metrics, or a
// progress UI.
//
// Both methods are called on the goroutine running the source, so an
// implementation that blocks slows the run down. Neither may panic — the
// library does not recover on an observer's behalf.
type Observer interface {
	OnSourceStart(ev RunEvent)
	OnSourceFinish(ev RunEvent)
}

// ObserverFunc adapts a plain function to Observer, receiving both the start
// and the finish event. Distinguish them by Duration, which is zero on start.
type ObserverFunc func(ev RunEvent)

func (f ObserverFunc) OnSourceStart(ev RunEvent)  { f(ev) }
func (f ObserverFunc) OnSourceFinish(ev RunEvent) { f(ev) }

// Observers fans one notification out to several observers in order. A nil or
// empty Observers is a valid no-op observer, which is what lets the library
// call an observer unconditionally.
type Observers []Observer

func (o Observers) OnSourceStart(ev RunEvent) {
	for _, obs := range o {
		if obs != nil {
			obs.OnSourceStart(ev)
		}
	}
}

func (o Observers) OnSourceFinish(ev RunEvent) {
	for _, obs := range o {
		if obs != nil {
			obs.OnSourceFinish(ev)
		}
	}
}
