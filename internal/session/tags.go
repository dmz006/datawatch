package session

import (
	"errors"
	"fmt"
	"time"
)

// TagRegistry checks tag IDs against the shared tag registry. RequireTag
// returns nil for a known tag. A refusal is returned unchanged so the REST
// layer maps it with the same rule as every other tag refusal.
type TagRegistry interface {
	RequireTag(id string) error
}

// TagOwnerLookup reports the tag IDs carried by the Automaton whose PRD ID
// is prdID. ok is false when no such Automaton exists.
type TagOwnerLookup interface {
	AutomatonTagIDs(prdID string) (tagIDs []string, ok bool)
}

// ErrTagRegistryUnavailable is returned when a session is given tags but
// no tag registry has been wired.
var ErrTagRegistryUnavailable = errors.New("tag registry not available")

// SetTagRegistry wires the registry that validates session tag IDs.
func (m *Manager) SetTagRegistry(r TagRegistry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagRegistry = r
}

// SetTagOwnerLookup wires the lookup that resolves the tags a session
// inherits from the Automaton that spawned it.
func (m *Manager) SetTagOwnerLookup(o TagOwnerLookup) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagOwners = o
}

// tagRegistryRef returns the wired registry, or nil, under the lock.
func (m *Manager) tagRegistryRef() TagRegistry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tagRegistry
}

// ValidateTagIDs checks every tag ID against the registry and returns the
// IDs with duplicates dropped (first occurrence wins). Any refused ID fails
// the whole call, so a caller either applies every tag or none.
func (m *Manager) ValidateTagIDs(tagIDs []string) ([]string, error) {
	out := dedupeTagIDs(tagIDs)
	if len(out) == 0 {
		return nil, nil
	}
	reg := m.tagRegistryRef()
	if reg == nil {
		return nil, ErrTagRegistryUnavailable
	}
	for _, id := range out {
		if err := reg.RequireTag(id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetSessionTags replaces a session's own tags. id is the full or short
// session ID. The inherited tags are not touched: they follow the owning
// Automaton. The returned session is the stored record.
func (m *Manager) SetSessionTags(id string, tagIDs []string) (*Session, error) {
	sess, ok := m.GetSession(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	out, err := m.ValidateTagIDs(tagIDs)
	if err != nil {
		return nil, err
	}
	sess.Tags = out
	sess.UpdatedAt = time.Now()
	if err := m.store.Save(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// InheritedTags returns the tag IDs a session inherits from the Automaton
// that spawned it (sess.PRDID). The set is computed on every call and is
// never stored. A session outside any Automaton gets an empty, non-nil slice.
func (m *Manager) InheritedTags(sess *Session) []string {
	out := []string{}
	if sess == nil || sess.PRDID == "" {
		return out
	}
	m.mu.Lock()
	owners := m.tagOwners
	m.mu.Unlock()
	if owners == nil {
		return out
	}
	ids, ok := owners.AutomatonTagIDs(sess.PRDID)
	if !ok {
		return out
	}
	return append(out, dedupeTagIDs(ids)...)
}

// CountSessionsWithTag counts the sessions, active and archived, that carry
// tagID as one of their own tags. Inherited tags are not counted. The
// autonomous tag registry calls it to refuse deleting a tag still in use.
func (m *Manager) CountSessionsWithTag(tagID string) int {
	has := func(sess *Session) bool { return sessionHasTag(sess, tagID) }
	n := 0
	for _, sess := range m.store.List() {
		if has(sess) {
			n++
		}
	}
	n += len(m.ListArchivedSessions(SessionFilter{}, has, "", time.Time{}, 0))
	return n
}

// sessionHasTag reports whether tagID is among the session's own tags.
func sessionHasTag(sess *Session, tagID string) bool {
	for _, t := range sess.Tags {
		if t == tagID {
			return true
		}
	}
	return false
}

// dedupeTagIDs returns tagIDs with repeats dropped, keeping the first
// occurrence. An empty input returns nil so an untagged session stores no
// tags field.
func dedupeTagIDs(tagIDs []string) []string {
	var out []string
	seen := make(map[string]bool, len(tagIDs))
	for _, id := range tagIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
