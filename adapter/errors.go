package adapter

import (
	"errors"
	"fmt"
)

// ErrNotRegistered is the sentinel behind NotRegisteredError, so callers can
// test with errors.Is without naming the concrete type.
var ErrNotRegistered = errors.New("job source not registered")

// ErrDuplicateKey is returned when two sources claim the same key. Keys index
// the registry and are stamped into every posting, so a collision is a wiring
// bug rather than something to resolve at runtime.
var ErrDuplicateKey = errors.New("duplicate job source key")

// NotRegisteredError names the key that was asked for and not found.
type NotRegisteredError struct{ Key string }

func (e NotRegisteredError) Error() string {
	return fmt.Sprintf("adapter: no job source registered for key %q", e.Key)
}

func (e NotRegisteredError) Unwrap() error { return ErrNotRegistered }

// DuplicateKeyError names the key that was registered twice.
type DuplicateKeyError struct{ Key string }

func (e DuplicateKeyError) Error() string {
	return fmt.Sprintf("adapter: job source key %q is already registered", e.Key)
}

func (e DuplicateKeyError) Unwrap() error { return ErrDuplicateKey }

// NoPostingReaderError reports that no registered source claimed a URL, so the
// caller cannot read it into a posting and must fall back to manual entry.
type NoPostingReaderError struct{ URL string }

func (e NoPostingReaderError) Error() string {
	return fmt.Sprintf("adapter: no job source can read posting %q", e.URL)
}
