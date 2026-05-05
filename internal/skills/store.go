package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Registry is one place skills come from. Today only kind="git" is wired;
// the schema leaves room for kind="local" / kind="http" in v6.7.x.
type Registry struct {
	Name           string    `json:"name"`
	Kind           string    `json:"kind"` // git
	URL            string    `json:"url"`
	Branch         string    `json:"branch,omitempty"`
	AuthSecretRef  string    `json:"auth_secret_ref,omitempty"` // ${secret:...} per Secrets-Store Rule
	Enabled        bool      `json:"enabled"`
	Description    string    `json:"description,omitempty"`
	IsBuiltin      bool      `json:"is_builtin,omitempty"` // marks PAI default registry
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastSyncedAt   time.Time `json:"last_synced_at,omitempty"`
	LastSyncError  string    `json:"last_sync_error,omitempty"`
}

// AvailableSkill is what `connect+browse` discovers in a registry's
// shallow clone — present in the registry but not yet copied into the
// synced area.
type AvailableSkill struct {
	Registry    string    `json:"registry"`
	Name        string    `json:"name"`
	Path        string    `json:"path"` // path relative to registry root
	Manifest    *Manifest `json:"manifest,omitempty"`
	Synced      bool      `json:"synced"`
}

// Synced is the canonical record of a skill that's been copied to
// ~/.datawatch/skills/<registry>/<name>/. The Path is filesystem-absolute.
type Synced struct {
	Registry  string    `json:"registry"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Manifest  *Manifest `json:"manifest,omitempty"`
	SyncedAt  time.Time `json:"synced_at"`
	Version   string    `json:"version,omitempty"`
}

// Index is the persistent state for the skills subsystem.
type Index struct {
	Registries []*Registry `json:"registries"`
	Synced     []*Synced   `json:"synced"`
	// AvailableCache stores last-known-available per registry so the
	// browse/select UX doesn't re-clone on every page load. Refreshed
	// on Connect and on explicit refresh.
	AvailableCache map[string][]*AvailableSkill `json:"available_cache,omitempty"`
	UpdatedAt      time.Time                     `json:"updated_at"`
}

// Store persists the Index to a single JSON file. Thread-safe.
type Store struct {
	mu    sync.Mutex
	path  string
	idx   *Index
}

// NewStore opens (or creates) a skills index at path.
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}
	s := &Store{path: path, idx: &Index{AvailableCache: map[string][]*AvailableSkill{}}}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load skills index: %w", err)
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	idx := &Index{AvailableCache: map[string][]*AvailableSkill{}}
	if err := json.Unmarshal(data, idx); err != nil {
		return err
	}
	if idx.AvailableCache == nil {
		idx.AvailableCache = map[string][]*AvailableSkill{}
	}
	s.idx = idx
	return nil
}

func (s *Store) save() error {
	s.idx.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(s.idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skills index: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write skills index: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// ── Registry CRUD ───────────────────────────────────────────────────────

// ListRegistries returns a copy of every configured registry.
func (s *Store) ListRegistries() []*Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Registry, len(s.idx.Registries))
	for i, r := range s.idx.Registries {
		c := *r
		out[i] = &c
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetRegistry returns a copy by name, or false.
func (s *Store) GetRegistry(name string) (*Registry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.idx.Registries {
		if r.Name == name {
			c := *r
			return &c, true
		}
	}
	return nil, false
}

// CreateRegistry inserts a new registry. Rejects duplicate names.
func (s *Store) CreateRegistry(r *Registry) error {
	if r.Name == "" {
		return fmt.Errorf("registry name required")
	}
	if r.Kind == "" {
		r.Kind = "git"
	}
	if r.Kind != "git" {
		return fmt.Errorf("registry kind %q not supported (v1: git only)", r.Kind)
	}
	if r.URL == "" {
		return fmt.Errorf("registry URL required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.idx.Registries {
		if e.Name == r.Name {
			return fmt.Errorf("registry %q already exists", r.Name)
		}
	}
	now := time.Now().UTC()
	c := *r
	c.CreatedAt = now
	c.UpdatedAt = now
	s.idx.Registries = append(s.idx.Registries, &c)
	return s.save()
}

// UpdateRegistry replaces fields by name. Preserves CreatedAt + IsBuiltin.
func (s *Store) UpdateRegistry(r *Registry) error {
	if r.Name == "" {
		return fmt.Errorf("registry name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.idx.Registries {
		if e.Name == r.Name {
			c := *r
			c.CreatedAt = e.CreatedAt
			c.IsBuiltin = e.IsBuiltin
			c.UpdatedAt = time.Now().UTC()
			s.idx.Registries[i] = &c
			return s.save()
		}
	}
	return fmt.Errorf("registry %q not found", r.Name)
}

// DeleteRegistry removes a registry and any synced skills from it.
// Returns the number of synced skills removed from the index.
func (s *Store) DeleteRegistry(name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, e := range s.idx.Registries {
		if e.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, fmt.Errorf("registry %q not found", name)
	}
	s.idx.Registries = append(s.idx.Registries[:idx], s.idx.Registries[idx+1:]...)
	delete(s.idx.AvailableCache, name)
	removed := 0
	kept := s.idx.Synced[:0]
	for _, sk := range s.idx.Synced {
		if sk.Registry == name {
			removed++
			continue
		}
		kept = append(kept, sk)
	}
	s.idx.Synced = kept
	return removed, s.save()
}

// SetLastSync updates the timestamp + error after a sync attempt.
func (s *Store) SetLastSync(name, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.idx.Registries {
		if e.Name == name {
			e.LastSyncedAt = time.Now().UTC()
			e.LastSyncError = errMsg
			return s.save()
		}
	}
	return fmt.Errorf("registry %q not found", name)
}

// ── Available cache ─────────────────────────────────────────────────────

// SetAvailable replaces the available-skills cache for a registry.
func (s *Store) SetAvailable(registry string, available []*AvailableSkill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx.AvailableCache == nil {
		s.idx.AvailableCache = map[string][]*AvailableSkill{}
	}
	// Mark already-synced entries
	syncedSet := map[string]bool{}
	for _, sk := range s.idx.Synced {
		if sk.Registry == registry {
			syncedSet[sk.Name] = true
		}
	}
	for _, a := range available {
		a.Synced = syncedSet[a.Name]
	}
	s.idx.AvailableCache[registry] = available
	return s.save()
}

// Available returns the cached available list for a registry.
func (s *Store) Available(registry string) []*AvailableSkill {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*AvailableSkill, len(s.idx.AvailableCache[registry]))
	for i, a := range s.idx.AvailableCache[registry] {
		c := *a
		out[i] = &c
	}
	return out
}

// ── Synced CRUD ─────────────────────────────────────────────────────────

// ListSynced returns every synced skill, optionally filtered by registry.
func (s *Store) ListSynced(registryFilter string) []*Synced {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Synced
	for _, sk := range s.idx.Synced {
		if registryFilter != "" && sk.Registry != registryFilter {
			continue
		}
		c := *sk
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Registry != out[j].Registry {
			return out[i].Registry < out[j].Registry
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// GetSynced returns the first match for `name` (registry-agnostic) or
// the exact `registry/name` if registry is non-empty.
func (s *Store) GetSynced(registry, name string) (*Synced, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sk := range s.idx.Synced {
		if registry != "" && sk.Registry != registry {
			continue
		}
		if sk.Name == name {
			c := *sk
			return &c, true
		}
	}
	return nil, false
}

// UpsertSynced inserts or replaces a synced-skill entry.
func (s *Store) UpsertSynced(sk *Synced) error {
	if sk.Registry == "" || sk.Name == "" {
		return fmt.Errorf("synced skill needs registry + name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sk.SyncedAt = time.Now().UTC()
	for i, e := range s.idx.Synced {
		if e.Registry == sk.Registry && e.Name == sk.Name {
			c := *sk
			s.idx.Synced[i] = &c
			return s.save()
		}
	}
	c := *sk
	s.idx.Synced = append(s.idx.Synced, &c)
	return s.save()
}

// RemoveSynced unsyncs a single skill from the index (caller is
// responsible for deleting the on-disk content).
func (s *Store) RemoveSynced(registry, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.idx.Synced {
		if e.Registry == registry && e.Name == name {
			s.idx.Synced = append(s.idx.Synced[:i], s.idx.Synced[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("synced skill %s/%s not found", registry, name)
}
