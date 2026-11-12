package autonomous

import (
	"sort"
)

// SessionSystemTagReader reads the system tags of the sessions owned by a
// given Automaton: a session belongs to an Automaton when it carries the
// Automaton's PRD ID (Session.PRDID). *session.Manager implements it
// (SystemTagsByPRD); tests use a fake. Nil = no reader is wired and the
// inherited tags read as empty.
type SessionSystemTagReader interface {
	// SystemTagsByPRD returns the system tags carried by the sessions with
	// the given PRD ID, in any order and with repeats.
	SystemTagsByPRD(prdID string) []string
}

// SystemTagSnapshotter is an optional extension of SessionSystemTagReader,
// checked by type assertion. SystemTagSnapshot returns a lookup, computed from
// one pass over the sessions, that gives the same tags SystemTagsByPRD would
// for any Automaton. *session.Manager implements it from its cached index.
type SystemTagSnapshotter interface {
	SystemTagSnapshot() func(prdID string) []string
}

// normalizeInheritedTags sorts and de-duplicates tags, dropping empty ones.
// It returns nil when nothing is left.
func normalizeInheritedTags(tags []string) []string {
	set := map[string]bool{}
	for _, t := range tags {
		if t != "" {
			set[t] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// InheritedTagsSnapshot returns a function giving the same result as
// InheritedTagsForPRD, computed from one snapshot of the reader. A list
// handler takes it once and calls it per Automaton. When the reader has no
// snapshot method it falls back to calling the reader per Automaton.
func (m *Manager) InheritedTagsSnapshot() func(prdID string) []string {
	m.mu.Lock()
	reader := m.inheritedTagReader
	m.mu.Unlock()
	if reader == nil {
		return func(string) []string { return nil }
	}
	var lookup func(string) []string
	if sn, ok := reader.(SystemTagSnapshotter); ok {
		lookup = sn.SystemTagSnapshot()
	} else {
		lookup = reader.SystemTagsByPRD
	}
	return func(prdID string) []string {
		if prdID == "" {
			return nil
		}
		return normalizeInheritedTags(lookup(prdID))
	}
}

// SetInheritedTagReader wires the session-side reader that backs the
// inherited_tags read field. It is a read-only source: it must never write
// tags. Nil restores the default (no inherited tags).
func (m *Manager) SetInheritedTagReader(r SessionSystemTagReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inheritedTagReader = r
}

// InheritedTagsForPRD returns the sorted, de-duplicated union of the system
// tags carried by the sessions owned by the Automaton with the given PRD ID.
// It is computed on every call and is never stored on the PRD record: the
// PRD's own Tags are the only tag list an Automaton carries, and they are
// left untouched by this read. It returns nil when no owned session carries
// a system tag (or no reader is wired), so views omit the field.
func (m *Manager) InheritedTagsForPRD(prdID string) []string {
	if prdID == "" {
		return nil
	}
	m.mu.Lock()
	reader := m.inheritedTagReader
	m.mu.Unlock()
	if reader == nil {
		return nil
	}
	return normalizeInheritedTags(reader.SystemTagsByPRD(prdID))
}
