package autonomous

import "time"

// The helpers in this file let the shared tag registry (internal/tagregistry)
// keep the tag records and move the assignments they name. The registry decides
// what changes and when it is refused; these helpers only store the result.
// Tag events are not emitted here: the daemon's registry service emits them, so
// each change produces one tag_update frame.

// SaveTagRecords replaces the tag records with tags. Each tag is saved, and a
// stored tag that is missing from the list is deleted. Assignments are not
// checked here; the registry checks them before it asks for a removal.
func (m *Manager) SaveTagRecords(tags []*Tag) error {
	keep := make(map[string]bool, len(tags))
	for _, t := range tags {
		keep[t.ID] = true
		if err := m.store.SaveTag(t); err != nil {
			return err
		}
	}
	for _, t := range m.store.ListTags() {
		if keep[t.ID] {
			continue
		}
		if err := m.store.DeleteTag(t.ID); err != nil {
			return err
		}
	}
	return nil
}

// TagAssignmentCount returns how many Automata and sessions carry a tag, counting
// a session's inherited tags.
func (m *Manager) TagAssignmentCount(tagID string) int {
	return m.tagInUseCount(tagID)
}

// sessionTagMover is the session side of a tag move. The session manager
// implements it, and the autonomous manager finds it through its session tag
// counter.
type sessionTagMover interface {
	MoveSessionTags(fromID, toID string) (int, error)
}

// RewriteTagAssignments moves every Automaton and session that carries fromID
// over to toID. An Automaton that carries both keeps toID once, and a primary
// tag that names fromID moves with it. It returns the number of items changed.
func (m *Manager) RewriteTagAssignments(fromID, toID string) (int, error) {
	return m.moveTagAssignments(fromID, toID)
}

// RemoveTagAssignments drops tagID from every Automaton and session that carries
// it, and clears a primary tag that names it. It returns the number of items
// changed.
func (m *Manager) RemoveTagAssignments(tagID string) (int, error) {
	return m.moveTagAssignments(tagID, "")
}

// moveTagAssignments replaces fromID with toID on every Automaton and session
// that carries fromID. An empty toID removes fromID instead.
func (m *Manager) moveTagAssignments(fromID, toID string) (int, error) {
	changed := 0
	for _, p := range m.store.ListPRDs() {
		next, hit := replaceTagID(p.TagIDs, fromID, toID)
		primaryHit := p.PrimaryTagID == fromID
		if !hit && !primaryHit {
			continue
		}
		p.TagIDs = next
		if primaryHit {
			p.PrimaryTagID = toID
		}
		p.UpdatedAt = time.Now()
		if err := m.store.SavePRD(p); err != nil {
			return changed, err
		}
		changed++
	}
	m.mu.Lock()
	counter := m.sessionTags
	m.mu.Unlock()
	if mover, ok := counter.(sessionTagMover); ok {
		n, err := mover.MoveSessionTags(fromID, toID)
		changed += n
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// replaceTagID returns ids with fromID replaced by toID, each ID kept once, and
// reports whether fromID was present. An empty toID drops fromID. The input is
// not changed.
func replaceTagID(ids []string, fromID, toID string) ([]string, bool) {
	hit := false
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == fromID {
			hit = true
			if toID == "" {
				continue
			}
			id = toID
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if !hit {
		return ids, false
	}
	return out, true
}
