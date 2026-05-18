// Package multiserver manages the runtime-mutable registry of remote
// datawatch instances (BL312 S1). Entries persist to
// <dataDir>/servers.json and are merged with YAML-seeded entries
// from cfg.Servers at startup. YAML-seeded entries have Builtin=true
// and cannot be deleted or mutated at runtime.
package multiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ErrNotFound is returned when a named entry does not exist.
var ErrNotFound = errors.New("server not found")

// ErrConflict is returned when an entry with the same name already exists.
var ErrConflict = errors.New("server name already exists")

// ErrBuiltin is returned when an attempt is made to mutate or delete a
// YAML-seeded (read-only) entry.
var ErrBuiltin = errors.New("server is seeded from config and cannot be modified at runtime")

// Entry is a single remote datawatch instance in the registry.
type Entry struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Token     string    `json:"token,omitempty"`
	Label     string    `json:"label,omitempty"`
	Enabled   bool      `json:"enabled"`
	Builtin   bool      `json:"builtin,omitempty"` // seeded from YAML — read-only
	// BL316 — federation peer extensions.
	// Federated marks entries added via /api/federation/peers (vs plain multi-server entries).
	// AuthType is wire-ready for SPIFFE/SPIRE: "token" (default) | "spiffe".
	// Capabilities is the CBAC grant list; mix of group names and surface:action strings.
	Federated    bool     `json:"federated,omitempty"`
	AuthType     string   `json:"auth_type,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store manages the runtime-mutable server registry.
type Store struct {
	mu      sync.RWMutex
	path    string
	entries []*Entry
}

// NewStore loads (or creates) the JSON store at <dataDir>/servers.json
// and merges YAML-seeded entries from seeds. Seeds have Builtin=true
// and are not written to disk.
func NewStore(dataDir string, seeds []config.RemoteServerConfig) (*Store, error) {
	path := filepath.Join(dataDir, "servers.json")
	s := &Store{path: path}

	// Load persisted runtime entries.
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("multiserver store: load %s: %w", path, err)
	}

	// Merge YAML seeds (prepend, Builtin=true). If a seed name already
	// exists in the persisted runtime list, the runtime entry wins and
	// the seed entry is skipped (operator created it explicitly).
	builtins := make([]*Entry, 0, len(seeds))
	for _, sc := range seeds {
		if s.getByName(sc.Name) != nil {
			continue // runtime entry wins
		}
		builtins = append(builtins, &Entry{
			Name:         sc.Name,
			URL:          sc.URL,
			Token:        sc.Token,
			Enabled:      sc.Enabled,
			Builtin:      true,
			Federated:    sc.Federated,
			AuthType:     sc.AuthType,
			Capabilities: sc.Capabilities,
			CreatedAt:    time.Time{},
			UpdatedAt:    time.Time{},
		})
	}
	// Builtins first so they appear at the top of the list.
	s.entries = append(builtins, s.entries...)

	return s, nil
}

// List returns a snapshot of all entries (builtins + runtime).
func (s *Store) List() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, len(s.entries))
	for i, e := range s.entries {
		cp := *e
		out[i] = &cp
	}
	return out
}

// ListFederated returns only entries that were added as federation peers.
func (s *Store) ListFederated() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Entry
	for _, e := range s.entries {
		if e.Federated {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out
}

// Get returns a copy of the named entry, or (nil, false) if not found.
func (s *Store) Get(name string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.getByName(name)
	if e == nil {
		return nil, false
	}
	cp := *e
	return &cp, true
}

// Add inserts a new runtime entry. Returns ErrConflict if the name is
// already taken (builtin or runtime).
func (s *Store) Add(e *Entry) error {
	if e.Name == "" {
		return errors.New("name is required")
	}
	if e.URL == "" {
		return errors.New("url is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getByName(e.Name) != nil {
		return ErrConflict
	}
	now := time.Now().UTC()
	cp := *e
	cp.Builtin = false
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.entries = append(s.entries, &cp)
	return s.persist()
}

// Update replaces an existing runtime entry. Returns ErrNotFound if the
// name does not exist, ErrBuiltin if it is a YAML-seeded entry.
func (s *Store) Update(name string, updated *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.getByName(name)
	if existing == nil {
		return ErrNotFound
	}
	if existing.Builtin {
		return ErrBuiltin
	}
	// Preserve immutable fields.
	cp := *updated
	cp.Name = name
	cp.Builtin = false
	cp.CreatedAt = existing.CreatedAt
	cp.UpdatedAt = time.Now().UTC()
	*existing = cp
	return s.persist()
}

// Delete removes a runtime entry. Returns ErrNotFound or ErrBuiltin.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.Name == name {
			if e.Builtin {
				return ErrBuiltin
			}
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return s.persist()
		}
	}
	return ErrNotFound
}

// Test pings the named server's /api/health endpoint and returns the
// measured latency in milliseconds and the reported version string.
func (s *Store) Test(ctx context.Context, name string) (latencyMs int64, version string, err error) {
	e, ok := s.Get(name)
	if !ok {
		return 0, "", ErrNotFound
	}
	url := e.URL
	if url == "" {
		return 0, "", fmt.Errorf("server %q has no URL", name)
	}
	// Trim trailing slash.
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	healthURL := url + "/api/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("build request: %w", err)
	}
	if e.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.Token)
	}

	start := time.Now()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	latencyMs = time.Since(start).Milliseconds()
	if err != nil {
		return latencyMs, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	// Best-effort parse of {"version":"..."}.
	var info struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(body, &info)
	return latencyMs, info.Version, nil
}

// GetByToken returns a copy of the first entry whose Token matches tok,
// or (nil, false) if not found. Used by the federation auth middleware to
// identify which peer is making a request.
func (s *Store) GetByToken(tok string) (*Entry, bool) {
	if tok == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.Token == tok {
			cp := *e
			return &cp, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Internal helpers — callers must hold the appropriate lock.

func (s *Store) getByName(name string) *Entry {
	for _, e := range s.entries {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// persist writes only the runtime (non-builtin) entries to disk.
// Caller must hold s.mu (write lock).
func (s *Store) persist() error {
	runtime := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if !e.Builtin {
			runtime = append(runtime, e)
		}
	}
	data, err := json.MarshalIndent(runtime, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// load reads the persisted runtime entries from disk. If the file does
// not exist, it initialises an empty list (not an error).
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = []*Entry{}
			return nil
		}
		return err
	}
	var entries []*Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	// Ensure Builtin is never set on persisted entries (defensive).
	for _, e := range entries {
		e.Builtin = false
	}
	s.entries = entries
	return nil
}
