package autonomous

import (
	"fmt"
	"sort"
	"strings"
)

// TagView is a tag plus the number of Automata carrying it.
type TagView struct {
	*Tag
	Count int `json:"count"`
}

// ListTags returns every tag with its Automata count, sorted by name. With
// includeSub each count also covers the tag's descendants.
func (a *API) ListTags(includeSub bool) []any {
	counts := a.M.TagCounts(includeSub)
	tags := a.M.ListTags()
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Name != tags[j].Name {
			return tags[i].Name < tags[j].Name
		}
		return tags[i].ID < tags[j].ID
	})
	out := make([]any, len(tags))
	for i, t := range tags {
		out[i] = TagView{Tag: t, Count: counts[t.ID]}
	}
	return out
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

// SetPRDTags replaces a PRD's tag list and returns the updated PRD.
func (a *API) SetPRDTags(prdID string, tagIDs []string) (any, error) {
	if err := a.M.SetPRDTags(prdID, tagIDs); err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	prd, _ := a.M.Store().GetPRD(prdID)
	return prd, nil
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
