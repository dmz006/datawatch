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
	ErrTagInUse       = errors.New("tag is assigned to automata or sessions")
	ErrTagCycle       = errors.New("tag parent change would create a cycle")
)

// TagInUseError reports how many Automata and sessions still carry a tag.
// It matches ErrTagInUse via errors.Is.
type TagInUseError struct {
	TagID string
	Count int
}

func (e *TagInUseError) Error() string {
	return fmt.Sprintf("tag %q is assigned to %d automata or sessions", e.TagID, e.Count)
}

func (e *TagInUseError) Is(target error) bool { return target == ErrTagInUse }

// SessionTagCounter reads the tags of the sessions, active and archived. The
// session manager implements it, and the server wires it in so DeleteTag
// refuses a tag that a session still carries and the counts view can count
// sessions.
type SessionTagCounter interface {
	// CountSessionsWithTag counts the sessions that carry a tag as one of
	// their own tags (inherited tags are not counted).
	CountSessionsWithTag(tagID string) int
	// SessionEffectiveTagSets returns one tag-ID list per session: its own
	// tags followed by the tags it inherits from the Automaton that spawned
	// it. The lists may repeat a tag; the counts ignore repeats.
	SessionEffectiveTagSets() [][]string
}

// AttentionStatusesKey is the autonomous config key that sets the statuses of
// the attention group.
const AttentionStatusesKey = "autonomous.tags.attention_statuses"

// DefaultAttentionStatuses are the Automaton statuses that put an Automaton in
// the attention group when the setting is absent.
var DefaultAttentionStatuses = []string{"blocked", "needs_review", "paused"}

// SettingLookup reads one autonomous setting, given as a list of strings, by
// its config key. ok is false when the key is not set.
type SettingLookup func(key string) (values []string, ok bool)

// ResolveAttentionStatuses returns the statuses of the attention group: the
// list the lookup sets under AttentionStatusesKey, else DefaultAttentionStatuses.
// A nil lookup, a missing key and a list with no usable entry all fall back to
// the default. Entries are trimmed, lower-cased and de-duplicated.
func ResolveAttentionStatuses(lookup SettingLookup) []string {
	if lookup != nil {
		if vals, ok := lookup(AttentionStatusesKey); ok {
			if out := normalizeStatusList(vals); len(out) > 0 {
				return out
			}
		}
	}
	return append([]string(nil), DefaultAttentionStatuses...)
}

// normalizeStatusList trims, lower-cases and de-duplicates a status list,
// keeping the first occurrence and dropping empty entries.
func normalizeStatusList(vals []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// SetSessionTagCounter wires the session-side counter used by DeleteTag.
func (m *Manager) SetSessionTagCounter(c SessionTagCounter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionTags = c
}

// tagInUseCount is the number of Automata plus sessions carrying a tag.
func (m *Manager) tagInUseCount(id string) int {
	n := m.tagUseCounts(false)[id]
	m.mu.Lock()
	counter := m.sessionTags
	m.mu.Unlock()
	if counter != nil {
		n += counter.CountSessionsWithTag(id)
	}
	return n
}

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
	m.emitTagEvent(TagEvent{Kind: TagEventCreated, TagID: t.ID})
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
	m.emitTagEvent(TagEvent{Kind: TagEventRenamed, TagID: id})
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
	m.emitTagEvent(TagEvent{Kind: TagEventReparented, TagID: id})
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
	m.emitTagEvent(TagEvent{Kind: TagEventRecolored, TagID: id})
	return t, nil
}

// DeleteTag removes a tag. Refused while it has child tags or is still
// assigned to any automaton or session.
func (m *Manager) DeleteTag(id string) error {
	if _, err := m.requireTag(id); err != nil {
		return err
	}
	for _, t := range m.store.ListTags() {
		if t.ParentID == id {
			return fmt.Errorf("%w: %q has child tag %q", ErrTagHasChildren, id, t.ID)
		}
	}
	if n := m.tagInUseCount(id); n > 0 {
		return &TagInUseError{TagID: id, Count: n}
	}
	if err := m.store.DeleteTag(id); err != nil {
		return err
	}
	m.emitTagEvent(TagEvent{Kind: TagEventDeleted, TagID: id})
	return nil
}

// ListTags returns all tags.
func (m *Manager) ListTags() []*Tag { return m.store.ListTags() }

// SetPRDTags replaces a PRD's full tag list, validating every ID and
// removing duplicates (first occurrence wins). The primary tag is kept while
// it stays in the list and cleared when it is removed.
func (m *Manager) SetPRDTags(prdID string, tagIDs []string) error {
	return m.SetPRDTagsAndPrimary(prdID, tagIDs, nil)
}

// SetPRDTagsAndPrimary replaces a PRD's tag list like SetPRDTags and sets its
// primary tag. A nil primary keeps the current primary while it is still in
// the list and clears it otherwise. A non-nil primary must be one of the
// tags, or empty to clear it; anything else is refused with ErrTagInvalid and
// nothing is written.
func (m *Manager) SetPRDTagsAndPrimary(prdID string, tagIDs []string, primary *string) error {
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
	primaryID := prd.PrimaryTagID
	if primary != nil {
		primaryID = *primary
		if primaryID != "" && !seen[primaryID] {
			return fmt.Errorf("%w: primary tag %q is not one of the automaton's tags", ErrTagInvalid, primaryID)
		}
	} else if primaryID != "" && !seen[primaryID] {
		primaryID = ""
	}
	prd.TagIDs = out
	prd.PrimaryTagID = primaryID
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.emitTagEvent(TagEvent{Kind: TagEventAssigned, PRDID: prdID})
	return nil
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
	return countTagSubtrees(m.automataTagSets(), m.tagParents(), includeSub)
}

// automataTagSets returns the tag IDs of every Automaton, one list each.
func (m *Manager) automataTagSets() [][]string {
	prds := m.store.ListPRDs()
	sets := make([][]string, 0, len(prds))
	for _, p := range prds {
		sets = append(sets, p.TagIDs)
	}
	return sets
}

// sessionTagSets returns the effective tag IDs of every session, or nil when
// no session counter is wired. It runs without the Manager lock, since the
// counter reads the session side, which reads the Automata back.
func (m *Manager) sessionTagSets() [][]string {
	m.mu.Lock()
	counter := m.sessionTags
	m.mu.Unlock()
	if counter == nil {
		return nil
	}
	return counter.SessionEffectiveTagSets()
}

// tagParents maps every tag ID to its parent's ID, "" for a root.
func (m *Manager) tagParents() map[string]string {
	parent := map[string]string{}
	for _, t := range m.store.ListTags() {
		parent[t.ID] = t.ParentID
	}
	return parent
}

// countTagSubtrees counts, per tag, how many items reach it. Each element of
// sets is one item's tag IDs. Without includeSub an item counts toward a tag
// it carries exactly. With includeSub it also counts toward every ancestor of
// those tags, so an item carrying a child tag counts toward the parent. Either
// way an item counts once per tag, however many of its tags sit in that
// subtree.
func countTagSubtrees(sets [][]string, parent map[string]string, includeSub bool) map[string]int {
	counts := map[string]int{}
	for _, set := range sets {
		seen := map[string]bool{}
		for _, t := range set {
			for cur, hops := t, 0; cur != "" && hops <= len(parent); cur, hops = parent[cur], hops+1 {
				if !seen[cur] {
					seen[cur] = true
					counts[cur]++
				}
				if !includeSub {
					break
				}
			}
		}
	}
	return counts
}

// SetTagDescription sets a tag's description.
func (m *Manager) SetTagDescription(id, description string) (*Tag, error) {
	t, err := m.requireTag(id)
	if err != nil {
		return nil, err
	}
	t.Description = description
	t.UpdatedAt = time.Now()
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	return t, nil
}
