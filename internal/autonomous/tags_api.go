package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// TagView is a tag plus the number of Automata carrying it.
type TagView struct {
	*Tag
	Count int `json:"count"`
}

// TagCountView is a tag in the counts form of the tag list (counts=true).
// Count keeps its meaning from ListTags. SubtreeCount and DirectCount count
// Automata; the session fields count sessions, with a session's inherited
// tags included. Subtree counts include the tag's descendants, direct counts
// only the exact tag.
type TagCountView struct {
	*Tag
	Count               int `json:"count"`
	SubtreeCount        int `json:"subtree_count"`
	DirectCount         int `json:"direct_count"`
	SessionSubtreeCount int `json:"session_subtree_count"`
	SessionDirectCount  int `json:"session_direct_count"`
}

// UntaggedCounts counts the Automata and the sessions that carry no tag at
// all. A session's inherited tags count, so a session is untagged only when
// neither it nor its Automaton has a tag.
type UntaggedCounts struct {
	Count        int `json:"count"`
	SessionCount int `json:"session_count"`
}

// sortedTags returns every tag sorted by name, then ID.
func (m *Manager) sortedTags() []*Tag {
	tags := m.ListTags()
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Name != tags[j].Name {
			return tags[i].Name < tags[j].Name
		}
		return tags[i].ID < tags[j].ID
	})
	return tags
}

// ListTags returns every tag with its Automata count, sorted by name. With
// includeSub each count also covers the tag's descendants.
func (a *API) ListTags(includeSub bool) []any {
	counts := a.M.TagCounts(includeSub)
	tags := a.M.sortedTags()
	out := make([]any, len(tags))
	for i, t := range tags {
		out[i] = TagView{Tag: t, Count: counts[t.ID]}
	}
	return out
}

// ListTagsWithCounts returns the tag list in the counts form, sorted like
// ListTags, with the untagged totals. Count follows includeSub as in ListTags;
// the subtree and direct counts do not depend on it.
func (a *API) ListTagsWithCounts(includeSub bool) ([]any, UntaggedCounts) {
	count := a.M.TagCounts(includeSub)
	subtree := a.M.tagUseCounts(true)
	direct := a.M.tagUseCounts(false)
	sessions := a.M.sessionTagSets()
	parents := a.M.tagParents()
	sessSubtree := countTagSubtrees(sessions, parents, true)
	sessDirect := countTagSubtrees(sessions, parents, false)

	tags := a.M.sortedTags()
	out := make([]any, len(tags))
	for i, t := range tags {
		out[i] = TagCountView{
			Tag:                 t,
			Count:               count[t.ID],
			SubtreeCount:        subtree[t.ID],
			DirectCount:         direct[t.ID],
			SessionSubtreeCount: sessSubtree[t.ID],
			SessionDirectCount:  sessDirect[t.ID],
		}
	}

	var untagged UntaggedCounts
	for _, set := range a.M.automataTagSets() {
		if len(set) == 0 {
			untagged.Count++
		}
	}
	for _, set := range sessions {
		if len(set) == 0 {
			untagged.SessionCount++
		}
	}
	return out, untagged
}

// CreateTag creates a tag.
func (a *API) CreateTag(name, parentID, color, description string) (any, error) {
	return a.M.CreateTag(name, parentID, color, description)
}

// UpdateTag applies the non-nil fields to a tag. Every requested change is
// validated before any is written, so a refused update changes nothing.
func (a *API) UpdateTag(id string, name, parentID, color, description *string) (any, error) {
	t, err := a.M.requireTag(id)
	if err != nil {
		return nil, err
	}
	if name != nil && strings.TrimSpace(*name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	if parentID != nil && *parentID != t.ParentID {
		if _, err := a.M.SetTagParent(id, *parentID); err != nil {
			return nil, err
		}
	}
	if name != nil {
		if _, err := a.M.RenameTag(id, *name); err != nil {
			return nil, err
		}
	}
	if color != nil {
		if _, err := a.M.SetTagColor(id, *color); err != nil {
			return nil, err
		}
	}
	if description != nil {
		if _, err := a.M.SetTagDescription(id, *description); err != nil {
			return nil, err
		}
	}
	nt, _ := a.M.requireTag(id)
	return nt, nil
}

// DeleteTag removes a tag; refused while it has children or is assigned.
func (a *API) DeleteTag(id string) error { return a.M.DeleteTag(id) }

// SetPRDTags replaces a PRD's tag list and returns the updated PRD. The
// primary tag is kept while it stays in the list, as SetPRDTagsAndPrimary does.
func (a *API) SetPRDTags(prdID string, tagIDs []string) (any, error) {
	return a.SetPRDTagsAndPrimary(prdID, tagIDs, nil)
}

// SetPRDTagsAndPrimary replaces a PRD's tag list and sets its primary tag (see
// Manager.SetPRDTagsAndPrimary for the nil and refusal rules), then returns the
// updated PRD.
func (a *API) SetPRDTagsAndPrimary(prdID string, tagIDs []string, primary *string) (any, error) {
	if err := a.M.SetPRDTagsAndPrimary(prdID, tagIDs, primary); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
}

// AttentionAutomata returns the Automata that need the operator's attention:
// those whose status is in statuses, plus every Automaton with a task held for
// operator review. Each is its summary view, newest first. The result is never
// nil.
func (a *API) AttentionAutomata(statuses []string) []any {
	want := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		want[s] = true
	}
	prds := a.M.Store().ListPRDs()
	sort.Slice(prds, func(i, j int) bool {
		return prdBefore(prds[i].UpdatedAt, prds[i].ID, prds[j].UpdatedAt, prds[j].ID)
	})
	out := []any{}
	for _, p := range prds {
		view := BuildSummaryView(p)
		if want[string(p.Status)] || len(view.ReviewNeeded) > 0 {
			out = append(out, view)
		}
	}
	return out
}

// prdBefore reports whether an Automaton sorts before another: the later
// updated_at first, then the greater ID.
func prdBefore(aT time.Time, aID string, bT time.Time, bID string) bool {
	if !aT.Equal(bT) {
		return aT.After(bT)
	}
	return aID > bID
}

// ListPRDsByTag returns the PRDs carrying a tag (or any descendant), each
// as the read-time copy that carries notify_events_effective (same fill as
// ListPRDs).
func (a *API) ListPRDsByTag(tagID string, includeSub bool) ([]any, error) {
	if _, err := a.M.requireTag(tagID); err != nil {
		return nil, err
	}
	return a.M.PRDsWithNotifyEffective(a.M.PRDsWithTag(tagID, includeSub)), nil
}

// RequireTag reports whether a tag exists. A missing tag returns an error
// matching ErrTagNotFound; the session side uses it to validate its tags.
func (a *API) RequireTag(id string) error {
	_, err := a.M.requireTag(id)
	return err
}

// AutomatonTagIDs returns a copy of an Automaton's tag IDs, and false when no
// Automaton has the given PRD ID. A session spawned by the Automaton inherits
// these tags.
func (a *API) AutomatonTagIDs(prdID string) ([]string, bool) {
	prd, ok := a.M.Store().GetPRD(prdID)
	if !ok {
		return nil, false
	}
	return append([]string(nil), prd.TagIDs...), true
}

// SetSessionTagCounter wires the session-side tag counter used by DeleteTag.
func (a *API) SetSessionTagCounter(c SessionTagCounter) {
	a.M.SetSessionTagCounter(c)
}
