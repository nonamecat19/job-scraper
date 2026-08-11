package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nonamecat19/job-scraper/ports"
)

// Roster is an in-memory ports.Roster. It is what makes the ATS sources usable
// without a database: seed it with the boards to crawl and run.
type Roster struct {
	mu         sync.Mutex
	boards     []ports.EmployerBoard
	candidates []ports.BoardCandidate
	applyURLs  []string
	nextID     int
}

var _ ports.Roster = (*Roster)(nil)

func NewRoster() *Roster { return &Roster{} }

// Seed adds boards in bulk, assigning an ID to any board that lacks one. It is
// the setup call for a consumer that keeps its board list in configuration
// rather than a database.
func (r *Roster) Seed(boards ...ports.EmployerBoard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range boards {
		if b.ID == "" {
			r.nextID++
			b.ID = fmt.Sprintf("board-%d", r.nextID)
		}
		r.boards = append(r.boards, b)
	}
}

// SeedApplyURLs supplies the apply URLs that ListApplyURLsForDiscovery mines.
func (r *Roster) SeedApplyURLs(urls ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyURLs = append(r.applyURLs, urls...)
}

// ListForRun returns the enabled boards for vendor.
func (r *Roster) ListForRun(_ context.Context, vendor string) ([]ports.EmployerBoard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.EmployerBoard
	for _, b := range r.boards {
		if b.Vendor == vendor && b.Enabled {
			out = append(out, b)
		}
	}
	return out, nil
}

// RecordRunOutcome updates the board's success bookkeeping. An empty run
// increments the empty-run counter, which is how a consumer spots a board whose
// employer has quietly gone away.
func (r *Roster) RecordRunOutcome(_ context.Context, employerID string, postingCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.boards {
		if r.boards[i].ID != employerID {
			continue
		}
		now := time.Now()
		r.boards[i].LastSuccessAt = &now
		r.boards[i].LastPostingCount = postingCount
		if postingCount == 0 {
			r.boards[i].ConsecutiveEmptyRuns++
		} else {
			r.boards[i].ConsecutiveEmptyRuns = 0
		}
		return nil
	}
	return fmt.Errorf("memstore: no employer board with id %q", employerID)
}

func (r *Roster) GetByVendorAndEmployer(_ context.Context, vendor, employerIdentifier string) (ports.EmployerBoard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.boards {
		if b.Vendor == vendor && b.EmployerIdentifier == employerIdentifier {
			return b, nil
		}
	}
	return ports.EmployerBoard{}, fmt.Errorf("memstore: no %s board for employer %q", vendor, employerIdentifier)
}

func (r *Roster) InsertEmployerBoard(_ context.Context, vendor, employerIdentifier, displayName, addedVia string) (ports.EmployerBoard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	b := ports.EmployerBoard{
		ID:                 fmt.Sprintf("board-%d", r.nextID),
		Vendor:             vendor,
		EmployerIdentifier: employerIdentifier,
		DisplayName:        displayName,
		AddedVia:           addedVia,
		Enabled:            true,
	}
	r.boards = append(r.boards, b)
	return b, nil
}

func (r *Roster) DeleteEmployerBoard(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, b := range r.boards {
		if b.ID == id {
			r.boards = append(r.boards[:i], r.boards[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("memstore: no employer board with id %q", id)
}

func (r *Roster) ListBoardCandidates(context.Context) ([]ports.BoardCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ports.BoardCandidate, len(r.candidates))
	copy(out, r.candidates)
	return out, nil
}

func (r *Roster) GetBoardCandidate(_ context.Context, vendor, employerIdentifier string) (ports.BoardCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.candidates {
		if c.Vendor == vendor && c.EmployerIdentifier == employerIdentifier {
			return c, nil
		}
	}
	return ports.BoardCandidate{}, fmt.Errorf("memstore: no %s candidate for employer %q", vendor, employerIdentifier)
}

func (r *Roster) GetBoardCandidateByID(_ context.Context, id string) (ports.BoardCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.candidates {
		if c.ID == id {
			return c, nil
		}
	}
	return ports.BoardCandidate{}, fmt.Errorf("memstore: no board candidate with id %q", id)
}

func (r *Roster) InsertBoardCandidate(_ context.Context, vendor, employerIdentifier string) (ports.BoardCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	c := ports.BoardCandidate{
		ID:                 fmt.Sprintf("candidate-%d", r.nextID),
		Vendor:             vendor,
		EmployerIdentifier: employerIdentifier,
		State:              "pending",
	}
	r.candidates = append(r.candidates, c)
	return c, nil
}

func (r *Roster) DecideBoardCandidate(_ context.Context, id, state string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.candidates {
		if r.candidates[i].ID == id {
			r.candidates[i].State = state
			return nil
		}
	}
	return fmt.Errorf("memstore: no board candidate with id %q", id)
}

func (r *Roster) ListApplyURLsForDiscovery(_ context.Context, limit int32) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit > 0 && int(limit) < len(r.applyURLs) {
		out := make([]string, limit)
		copy(out, r.applyURLs[:limit])
		return out, nil
	}
	out := make([]string, len(r.applyURLs))
	copy(out, r.applyURLs)
	return out, nil
}
