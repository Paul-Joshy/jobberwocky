// Package states keeps jobberwocky's own per-job states (done, archived) in
// its data directory. The workspace's seen_jobs.json is never written.
package states

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	Done     = "done"
	Archived = "archived"
)

// Entry is one job's state and when it was set.
type Entry struct {
	State string `json:"state"`
	At    string `json:"at"`
}

// Store maps job keys to states.
type Store struct {
	path string
	m    map[string]Entry
}

// Load reads the store; a missing or unreadable file is an empty store.
func Load(dataDir string) *Store {
	s := &Store{path: filepath.Join(dataDir, "states.json"), m: map[string]Entry{}}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &s.m)
		if s.m == nil {
			s.m = map[string]Entry{}
		}
	}
	return s
}

// Get returns a job's state, or "" when it has none.
func (s *Store) Get(key string) string { return s.m[key].State }

// Count reports how many jobs have a state.
func (s *Store) Count(state string) int {
	n := 0
	for _, e := range s.m {
		if e.State == state {
			n++
		}
	}
	return n
}

// Toggle sets state on every key, or clears it when every key already has it,
// so pressing the same key twice is an undo. It returns whether it set (true)
// or cleared (false).
func (s *Store) Toggle(keys []string, state string) (bool, error) {
	all := len(keys) > 0
	for _, k := range keys {
		if s.m[k].State != state {
			all = false
			break
		}
	}
	today := time.Now().Format("2006-01-02")
	for _, k := range keys {
		if all {
			delete(s.m, k)
		} else {
			s.m[k] = Entry{State: state, At: today}
		}
	}
	return !all, s.save()
}

// save writes atomically: a crash mid-write never leaves a half file.
func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
