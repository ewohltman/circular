// Package store persists the latest fetched deals and generated plan so the
// web page loads instantly and survives restarts.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/planner"
)

// Snapshot is everything the UI needs for one week.
type Snapshot struct {
	FetchedAt time.Time    `json:"fetched_at"`
	Deals     []deals.Deal `json:"deals"`
	Plan      planner.Plan `json:"plan"`
	Errors    []string     `json:"errors,omitempty"`
}

// Store is a JSON file guarded by a mutex.
type Store struct {
	path string
	mu   sync.RWMutex
	cur  *Snapshot
}

// Open loads path if it exists.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	s.cur = &snap
	return s, nil
}

// Get returns the current snapshot, or nil if none has been saved.
func (s *Store) Get() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Put replaces the snapshot and writes it atomically.
func (s *Store) Put(snap *Snapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.cur = snap
	s.mu.Unlock()
	return nil
}
