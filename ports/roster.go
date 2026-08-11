package ports

import (
	"context"
	"time"
)

// EmployerBoard is one employer's job board on an ATS vendor — the unit an ATS
// source iterates during a run.
type EmployerBoard struct {
	ID                   string
	Vendor               string
	EmployerIdentifier   string
	DisplayName          string
	AddedVia             string
	Enabled              bool
	LastSuccessAt        *time.Time
	LastPostingCount     int
	ConsecutiveEmptyRuns int
}

// BoardCandidate is a board inferred from a posting's apply URL but not yet
// accepted into the roster.
type BoardCandidate struct {
	ID                 string
	Vendor             string
	EmployerIdentifier string
	InferredFromJobID  *string
	State              string
}

// EmployerHealthChecker probes a single employer board and reports how many
// postings it currently serves. Vendor sources expose one so the consumer can
// validate a board before adding it.
type EmployerHealthChecker func(ctx context.Context, employerIdentifier string) (postingCount int, err error)

// Roster is the persistence port for employer boards. The library reads the
// roster and records run outcomes; the consumer owns the storage.
type Roster interface {
	// ListForRun returns the boards to crawl for vendor this run.
	ListForRun(ctx context.Context, vendor string) ([]EmployerBoard, error)
	RecordRunOutcome(ctx context.Context, employerID string, postingCount int) error
	GetByVendorAndEmployer(ctx context.Context, vendor, employerIdentifier string) (EmployerBoard, error)
	InsertEmployerBoard(ctx context.Context, vendor, employerIdentifier, displayName, addedVia string) (EmployerBoard, error)
	DeleteEmployerBoard(ctx context.Context, id string) error

	ListBoardCandidates(ctx context.Context) ([]BoardCandidate, error)
	GetBoardCandidate(ctx context.Context, vendor, employerIdentifier string) (BoardCandidate, error)
	GetBoardCandidateByID(ctx context.Context, id string) (BoardCandidate, error)
	InsertBoardCandidate(ctx context.Context, vendor, employerIdentifier string) (BoardCandidate, error)
	DecideBoardCandidate(ctx context.Context, id, state string) error

	// ListApplyURLsForDiscovery returns apply URLs to mine for new candidate
	// boards, newest first, capped at limit.
	ListApplyURLsForDiscovery(ctx context.Context, limit int32) ([]string, error)
}
