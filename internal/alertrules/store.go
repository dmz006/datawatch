package alertrules

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	maxFirings = 100
	storeFile  = "alert-rules.yaml"
)

// Store is a thread-safe, YAML-backed store for AlertRules plus an
// in-memory ring buffer of recent Firings.
type Store struct {
	mu      sync.RWMutex
	path    string
	rules   []AlertRule
	firings []Firing // ring buffer — newest at the end
}

// NewStore loads (or creates) the rule store from dataDir.
// Returns an error only when the file exists but cannot be parsed.
func NewStore(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, storeFile)
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("alertrules: load store: %w", err)
	}
	return s, nil
}

// load reads the YAML file from disk. A missing file is silently
// treated as an empty store.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &s.rules)
}

// save persists the current rule list atomically.
func (s *Store) save() error {
	data, err := yaml.Marshal(s.rules)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns a copy of all rules.
func (s *Store) List() []AlertRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AlertRule, len(s.rules))
	copy(out, s.rules)
	return out
}

// Get returns the named rule and whether it was found.
func (s *Store) Get(name string) (AlertRule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rules {
		if r.Name == name {
			return r, true
		}
	}
	return AlertRule{}, false
}

// Add inserts a new rule. Returns an error if a rule with the same
// name already exists.
func (s *Store) Add(r AlertRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.rules {
		if existing.Name == r.Name {
			return fmt.Errorf("alertrules: rule %q already exists", r.Name)
		}
	}
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	s.rules = append(s.rules, r)
	return s.save()
}

// Update replaces the rule with the same Name. Returns an error if
// no matching rule exists.
func (s *Store) Update(r AlertRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.rules {
		if existing.Name == r.Name {
			r.CreatedAt = existing.CreatedAt
			r.UpdatedAt = time.Now()
			s.rules[i] = r
			return s.save()
		}
	}
	return fmt.Errorf("alertrules: rule %q not found", r.Name)
}

// Delete removes the named rule. Returns true if the rule was found
// and deleted, false if it did not exist.
func (s *Store) Delete(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.Name == name {
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			s.save() //nolint:errcheck
			return true
		}
	}
	return false
}

// SetEnabled enables or disables the named rule. Returns true when
// the rule was found (regardless of prior state).
func (s *Store) SetEnabled(name string, on bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.Name == name {
			s.rules[i].Enabled = on
			s.rules[i].UpdatedAt = time.Now()
			s.save() //nolint:errcheck
			return true
		}
	}
	return false
}

// RecordFiring appends a Firing to the in-memory ring buffer, capped
// at maxFirings entries (oldest discarded first).
func (s *Store) RecordFiring(f Firing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firings = append(s.firings, f)
	if len(s.firings) > maxFirings {
		s.firings = s.firings[len(s.firings)-maxFirings:]
	}
}

// Firings returns a copy of the ring buffer (newest last).
func (s *Store) Firings() []Firing {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Firing, len(s.firings))
	copy(out, s.firings)
	return out
}
