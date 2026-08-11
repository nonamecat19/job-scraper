package rosterport

import (
	"context"
	"time"
)

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

type BoardCandidate struct {
	ID                 string
	Vendor             string
	EmployerIdentifier string
	InferredFromJobID  *string
	State              string
}

type EmployerHealthChecker func(ctx context.Context, employerIdentifier string) (postingCount int, err error)

type RosterPort interface {
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

	ListApplyURLsForDiscovery(ctx context.Context, limit int32) ([]string, error)
}
