package memstore

import (
	"context"
	"maps"
	"sync"

	"github.com/nonamecat19/jobscraper/model"
)

// SourceConfigStore is an in-memory ports.SourceConfigStore. Credentialed
// sources write their session cookie back through it, so with this store a
// login lasts as long as the process and no longer.
type SourceConfigStore struct {
	mu      sync.Mutex
	configs map[string]map[string]any
	enabled map[string]bool
}

func NewSourceConfigStore() *SourceConfigStore {
	return &SourceConfigStore{
		configs: map[string]map[string]any{},
		enabled: map[string]bool{},
	}
}

// Set seeds the configuration for a source — the credentials and endpoints the
// consumer would otherwise load from its database.
func (s *SourceConfigStore) Set(key string, config map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[key] = maps.Clone(config)
	s.enabled[key] = true
}

// Config returns a copy, so a source cannot mutate the stored map in place.
func (s *SourceConfigStore) Config(_ context.Context, key string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg, ok := s.configs[key]; ok {
		return maps.Clone(cfg), nil
	}
	return map[string]any{}, nil
}

// Update merges configPatch over the stored config rather than replacing it, so
// a source writing back one key does not wipe the credentials next to it.
func (s *SourceConfigStore) Update(_ context.Context, key string, enabled *bool, configPatch map[string]any) (*model.JobSourceDto, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, ok := s.configs[key]
	if !ok {
		cfg = map[string]any{}
		s.configs[key] = cfg
	}
	maps.Copy(cfg, configPatch)
	if enabled != nil {
		s.enabled[key] = *enabled
	}

	return &model.JobSourceDto{
		ID:      key,
		Key:     key,
		Enabled: s.enabled[key],
		Config:  maps.Clone(cfg),
	}, nil
}
