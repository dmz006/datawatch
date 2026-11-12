// Pending item persistence — a small separate store, one JSON file
// (pending_items.json) next to the other Automaton stores. Writes are atomic
// (temp file then rename, mode 0600). The store never reads a document.

package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const pendingItemsFile = "pending_items.json"

// PendingItemStore is the in-memory, file-backed registry of pending items.
type PendingItemStore struct {
	mu    sync.Mutex // guards items and the file
	path  string
	items map[string]*PendingItem // keyed by PendingItem.ID
}

// NewPendingItemStore opens (or creates) pending_items.json under dir. A
// missing file is an empty store; an unreadable one is an error.
func NewPendingItemStore(dir string) (*PendingItemStore, error) {
	s := &PendingItemStore{
		path:  filepath.Join(dir, pendingItemsFile),
		items: map[string]*PendingItem{},
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pendingItemsFile, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	var list []*PendingItem
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", pendingItemsFile, err)
	}
	for _, p := range list {
		if p == nil || p.ID == "" {
			continue
		}
		s.items[p.ID] = p
	}
	return s, nil
}

// persistLocked writes every item atomically; the caller holds s.mu.
func (s *PendingItemStore) persistLocked() error {
	list := make([]*PendingItem, 0, len(s.items))
	for _, p := range s.items {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		}
		return list[i].ID < list[j].ID
	})
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".tmp-pending-items-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// refTakenLocked reports whether another item in source already has ref.
func (s *PendingItemStore) refTakenLocked(source PendingItemSource, ref, exceptID string) (*PendingItem, bool) {
	if ref == "" {
		return nil, false
	}
	for _, p := range s.items {
		if p.ID != exceptID && p.Source == source && p.Ref == ref {
			return p, true
		}
	}
	return nil, false
}

// Create adds a waiting item. The title is required (trimmed, at most 300
// characters); ref is optional and unique within source. Returns a copy.
func (s *PendingItemStore) Create(title, ref string, source PendingItemSource) (*PendingItem, error) {
	title, err := normalizePendingItemTitle(title)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = PendingItemSourceInternal
	}
	if !validPendingItemSource(source) {
		return nil, fmt.Errorf("400: unknown pending item source %q", source)
	}
	ref = strings.TrimSpace(ref)

	s.mu.Lock()
	defer s.mu.Unlock()
	if other, taken := s.refTakenLocked(source, ref, ""); taken {
		return nil, fmt.Errorf("409: a %s pending item with ref %q already exists (%s)", source, ref, other.ID)
	}
	id := newAutomationID()
	for s.items[id] != nil {
		id = newAutomationID()
	}
	now := time.Now()
	p := &PendingItem{
		ID:        id,
		Ref:       ref,
		Title:     title,
		State:     PendingItemWaiting,
		Source:    source,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.items[id] = p
	if err := s.persistLocked(); err != nil {
		delete(s.items, id)
		return nil, fmt.Errorf("500: save pending items: %w", err)
	}
	return clonePendingItem(p), nil
}

// Get returns a copy of the item with id.
func (s *PendingItemStore) Get(id string) (*PendingItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[id]
	if !ok {
		return nil, false
	}
	return clonePendingItem(p), true
}

// List returns copies of the items matching f, oldest first.
func (s *PendingItemStore) List(f PendingItemFilter) []*PendingItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PendingItem, 0, len(s.items))
	for _, p := range s.items {
		if f.State != "" && p.State != f.State {
			continue
		}
		if f.Source != "" && p.Source != f.Source {
			continue
		}
		out = append(out, clonePendingItem(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// FindByRef returns the item in source whose ref is ref. An empty ref never
// matches.
func (s *PendingItemStore) FindByRef(source PendingItemSource, ref string) (*PendingItem, bool) {
	ref = strings.TrimSpace(ref)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.refTakenLocked(source, ref, "")
	if !ok {
		return nil, false
	}
	return clonePendingItem(p), true
}

// Update applies mutate to a copy of the item and stores the result. The
// mutation may change Title, Ref, State and AutomatonID; the id, source and
// created_at are fixed. The new title and ref are validated, a state change
// must be an allowed move, and the timestamps follow the move. When mutate
// returns an error nothing changes. Returns a copy of the stored item.
func (s *PendingItemStore) Update(id string, mutate func(*PendingItem) error) (*PendingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("404: pending item %s not found", id)
	}
	next := clonePendingItem(cur)
	if mutate != nil {
		if err := mutate(next); err != nil {
			return nil, err
		}
	}
	next.ID, next.Source, next.CreatedAt = cur.ID, cur.Source, cur.CreatedAt
	next.AttachedAt, next.DoneAt = cur.AttachedAt, cur.DoneAt
	if cur.AttachedAt != nil {
		t := *cur.AttachedAt
		next.AttachedAt = &t
	}
	if cur.DoneAt != nil {
		t := *cur.DoneAt
		next.DoneAt = &t
	}
	title, err := normalizePendingItemTitle(next.Title)
	if err != nil {
		return nil, err
	}
	next.Title = title
	next.Ref = strings.TrimSpace(next.Ref)
	if other, taken := s.refTakenLocked(cur.Source, next.Ref, cur.ID); taken {
		return nil, fmt.Errorf("409: a %s pending item with ref %q already exists (%s)", cur.Source, next.Ref, other.ID)
	}
	if err := checkPendingItemMove(cur.State, next.State); err != nil {
		return nil, err
	}
	now := time.Now()
	if next.State != cur.State {
		switch next.State {
		case PendingItemAttached:
			next.AttachedAt = &now
		case PendingItemDone:
			next.DoneAt = &now
		}
	}
	if next.State == PendingItemAttached && next.AutomatonID == "" {
		return nil, fmt.Errorf("400: an attached pending item needs an automaton id")
	}
	if next.State == PendingItemWaiting {
		next.AutomatonID = ""
	}
	next.UpdatedAt = now
	s.items[id] = next
	if err := s.persistLocked(); err != nil {
		s.items[id] = cur
		return nil, fmt.Errorf("500: save pending items: %w", err)
	}
	return clonePendingItem(next), nil
}

// Delete removes the item with id.
func (s *PendingItemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return fmt.Errorf("404: pending item %s not found", id)
	}
	delete(s.items, id)
	if err := s.persistLocked(); err != nil {
		s.items[id] = cur
		return fmt.Errorf("500: save pending items: %w", err)
	}
	return nil
}
