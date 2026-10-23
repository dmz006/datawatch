package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors for tag operations; the REST layer maps the refusal
// errors to 409 Conflict.
var (
	ErrTagNotFound    = errors.New("tag not found")
	ErrTagInvalid     = errors.New("invalid tag")
	ErrTagHasChildren = errors.New("tag has child tags")
	ErrTagInUse       = errors.New("tag is assigned to automata")
	ErrTagCycle       = errors.New("tag parent change would create a cycle")
)

// TagInUseError reports how many Automata still carry a tag. It matches
// ErrTagInUse via errors.Is.
type TagInUseError struct {
	TagID string
	Count int
}

func (e *TagInUseError) Error() string {
	return fmt.Sprintf("tag %q is assigned to %d automata", e.TagID, e.Count)
}

func (e *TagInUseError) Is(target error) bool { return target == ErrTagInUse }

func (m *Manager) requireTag(id string) (*Tag, error) {
	t, ok := m.store.GetTag(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTagNotFound, id)
	}
	return t, nil
}

// CreateTag creates a tag, optionally under an existing parent.
func (m *Manager) CreateTag(name, parentID, color, description string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	if parentID != "" {
		if _, err := m.requireTag(parentID); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	t := &Tag{ID: newID(), Name: name, ParentID: parentID, Color: color,
		Description: description, CreatedAt: now, UpdatedAt: now}
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	return t, nil
}

// RenameTag changes a tag's name.
func (m *Manager) RenameTag(id, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	t, err := m.requireTag(id)
	if err != nil {
		return nil, err
	}
	t.Name = name
	t.UpdatedAt = time.Now()
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	return t, nil
}

// SetTagParent reparents a tag ("" makes it a root). A move that would
// make the tag its own ancestor is refused.
func (m *Manager) SetTagParent(id, parentID string) (*Tag, error) {
	t, err := m.requireTag(id)
	if err != nil {
		return nil, err
	}
	if parentID != "" {
		if parentID == id {
			return nil, fmt.Errorf("%w: tag cannot be its own parent", ErrTagCycle)
		}
		// Walk the new parent chain to the root; the moved tag must not appear.
		seen := map[string]bool{}
		for cur := parentID; cur != ""; {
			if cur == id {
				return nil, fmt.Errorf("%w: %q is an ancestor of %q", ErrTagCycle, id, parentID)
			}
			if seen[cur] {
				break
			}
			seen[cur] = true
			p, err := m.requireTag(cur)
			if err != nil {
				return nil, err
			}
			cur = p.ParentID
		}
	}
	t.ParentID = parentID
	t.UpdatedAt = time.Now()
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	return t, nil
}

// SetTagColor sets a tag's color.
func (m *Manager) SetTagColor(id, color string) (*Tag, error) {
	t, err := m.requireTag(id)
	if err != nil {
		return nil, err
	}
	t.Color = color
	t.UpdatedAt = time.Now()
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTag removes a tag. Refused while it has child tags or is still
// assigned to any automaton.
func (m *Manager) DeleteTag(id string) error {
	if _, err := m.requireTag(id); err != nil {
		return err
	}
	for _, t := range m.store.ListTags() {
		if t.ParentID == id {
			return fmt.Errorf("%w: %q has child tag %q", ErrTagHasChildren, id, t.ID)
		}
	}
	if n := m.tagUseCounts(false)[id]; n > 0 {
		return &TagInUseError{TagID: id, Count: n}
	}
	return m.store.DeleteTag(id)
}

// ListTags returns all tags.
func (m *Manager) ListTags() []*Tag { return m.store.ListTags() }

// SetPRDTags replaces a PRD's full tag list, validating every ID and
// removing duplicates (first occurrence wins).
func (m *Manager) SetPRDTags(prdID string, tagIDs []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range tagIDs {
		if seen[id] {
			continue
		}
		if _, err := m.requireTag(id); err != nil {
			return err
		}
		seen[id] = true
		out = append(out, id)
	}
	prd.TagIDs = out
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// TagDescendants returns the IDs of every tag below id (not including id).
func (m *Manager) TagDescendants(id string) []string {
	children := map[string][]string{}
	for _, t := range m.store.ListTags() {
		children[t.ParentID] = append(children[t.ParentID], t.ID)
	}
	var out []string
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// PRDsWithTag returns PRDs carrying the tag, or (includeSub) the tag or
// any descendant.
func (m *Manager) PRDsWithTag(id string, includeSub bool) []*PRD {
	want := map[string]bool{id: true}
	if includeSub {
		for _, d := range m.TagDescendants(id) {
			want[d] = true
		}
	}
	var out []*PRD
	for _, p := range m.store.ListPRDs() {
		for _, t := range p.TagIDs {
			if want[t] {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// TagCounts returns a per-tag count of automata. With includeSub each
// tag's count also covers its descendants (an automaton is counted once
// per tag even if it carries several tags in the subtree).
func (m *Manager) TagCounts(includeSub bool) map[string]int {
	return m.tagUseCounts(includeSub)
}

func (m *Manager) tagUseCounts(includeSub bool) map[string]int {
	counts := map[string]int{}
	prds := m.store.ListPRDs()
	if !includeSub {
		for _, p := range prds {
			seen := map[string]bool{}
			for _, t := range p.TagIDs {
				if !seen[t] {
					seen[t] = true
					counts[t]++
				}
			}
		}
		return counts
	}
	parent := map[string]string{}
	for _, t := range m.store.ListTags() {
		parent[t.ID] = t.ParentID
	}
	for _, p := range prds {
		seen := map[string]bool{}
		for _, t := range p.TagIDs {
			for cur, hops := t, 0; cur != "" && hops <= len(parent); cur, hops = parent[cur], hops+1 {
				if !seen[cur] {
					seen[cur] = true
					counts[cur]++
				}
			}
		}
	}
	return counts
}
