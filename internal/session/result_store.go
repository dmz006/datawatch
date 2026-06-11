package session

// BL360 — structured agent result store.
// A small key-value store for sub-agent result JSONs.
// Agents (or users) can store named result payloads, retrieve them by name,
// list them (with optional prefix filter), and delete them.
// WAL-backed (file-backed), host-durable, optional TTL.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ResultEntry is a single named result in the store.
type ResultEntry struct {
	Name      string         `json:"name"`
	Payload   map[string]any `json:"payload"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ResultStore is a durable named-result store backed by a JSON file.
// After every mutation the state is persisted atomically.
type ResultStore struct {
	mu      sync.Mutex
	path    string
	entries map[string]*ResultEntry
}

// NewResultStore creates or loads a result store from path.
func NewResultStore(path string) (*ResultStore, error) {
	s := &ResultStore{
		path:    path,
		entries: make(map[string]*ResultEntry),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ResultStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read result store: %w", err)
	}
	var entries []*ResultEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse result store: %w", err)
	}
	for _, e := range entries {
		s.entries[e.Name] = e
	}
	return nil
}

func (s *ResultStore) save() error {
	entries := make([]*ResultEntry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	// stable order by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Put upserts a named result entry. If ttlSeconds > 0, an ExpiresAt is set.
// TTL=0 means no expiry (ExpiresAt = nil).
func (s *ResultStore) Put(name string, payload map[string]any, ttlSeconds int) (*ResultEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if payload == nil {
		payload = map[string]any{}
	}

	var expiresAt *time.Time
	if ttlSeconds > 0 {
		t := now.Add(time.Duration(ttlSeconds) * time.Second)
		expiresAt = &t
	}

	if existing, ok := s.entries[name]; ok {
		// Update existing entry
		existing.Payload = payload
		existing.ExpiresAt = expiresAt
		existing.UpdatedAt = now
		return existing, s.save()
	}

	// Insert new entry
	e := &ResultEntry{
		Name:      name,
		Payload:   payload,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.entries[name] = e
	return e, s.save()
}

// Get returns an entry by name. Returns nil, false if not found or expired.
func (s *ResultStore) Get(name string) (*ResultEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[name]
	if !ok {
		return nil, false
	}
	// Treat expired entries as not found
	if e.ExpiresAt != nil && time.Now().After(*e.ExpiresAt) {
		return nil, false
	}
	return e, true
}

// List returns all non-expired entries whose names start with prefix.
// prefix="" returns all entries. Results are sorted alphabetically by Name.
func (s *ResultStore) List(prefix string) []*ResultEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var out []*ResultEntry
	for _, e := range s.entries {
		// Skip expired
		if e.ExpiresAt != nil && now.After(*e.ExpiresAt) {
			continue
		}
		// Apply prefix filter
		if prefix != "" && !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Delete removes an entry by name. Returns an error if not found.
func (s *ResultStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.entries[name]; !ok {
		return fmt.Errorf("result entry %q not found", name)
	}
	delete(s.entries, name)
	return s.save()
}

// ExpireEntries removes all entries whose ExpiresAt is in the past.
// Returns the count of removed entries.
func (s *ResultStore) ExpireEntries() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	n := 0
	for name, e := range s.entries {
		if e.ExpiresAt != nil && now.After(*e.ExpiresAt) {
			delete(s.entries, name)
			n++
		}
	}
	if n > 0 {
		_ = s.save()
	}
	return n
}
