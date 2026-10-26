package tagregistry

import (
	"fmt"
	"sort"
	"strings"
)

// The functions in this file change the registry state. Every write a keeper
// applies, whether it came from a peer or from the keeper's own user, passes
// through applyLocked. Callers hold mu. applyLocked validates before it changes
// anything, so a refused write leaves the state as it was.

// applyAndCommitLocked applies one write and saves the result. A failed save
// undoes the write.
func (r *Registry) applyAndCommitLocked(w Write) (WriteResult, error) {
	var res WriteResult
	err := r.mutateLocked(func() error {
		var err error
		res, err = r.applyLocked(w)
		return err
	})
	if err != nil {
		return WriteResult{}, err
	}
	return res, nil
}

// applyLocked dispatches one write to the function for its kind.
func (r *Registry) applyLocked(w Write) (WriteResult, error) {
	switch w.Kind {
	case WriteCreate:
		return r.createLocked(w)
	case WriteRename:
		return r.editLocked(w, fieldName)
	case WriteReparent:
		return r.reparentLocked(w)
	case WriteRecolor:
		return r.editLocked(w, fieldColor)
	case WriteDescribe:
		return r.editLocked(w, fieldDescription)
	case WriteDelete:
		return r.deleteLocked(w)
	}
	return WriteResult{}, fmt.Errorf("%w: unknown write %q", ErrTagInvalid, w.Kind)
}

// createLocked adds a tag. A tag id the registry already holds is a retried
// write and returns the existing tag. A pending write whose parent and name
// match a tag of the keeper merges into that tag instead.
func (r *Registry) createLocked(w Write) (WriteResult, error) {
	name := strings.TrimSpace(w.Name)
	if w.TagID == "" || name == "" {
		return WriteResult{}, fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	if i := r.indexLocked(w.TagID); i >= 0 {
		return r.resultLocked(i), nil
	}
	if w.ParentID != "" && r.indexLocked(w.ParentID) < 0 {
		return WriteResult{}, fmt.Errorf("%w: parent %q", ErrTagNotFound, w.ParentID)
	}
	if w.Pending {
		if i := r.sameNameLocked(w.ParentID, name); i >= 0 {
			return r.mergeLocked(w, i), nil
		}
	}
	now := r.clock.Now()
	r.st.Tags = append(r.st.Tags, Tag{
		ID:          w.TagID,
		Name:        name,
		ParentID:    w.ParentID,
		Color:       w.Color,
		Description: w.Description,
		OriginNode:  w.OriginNode,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	r.bumpLocked(w.TagID, fieldName, fieldParent, fieldColor, fieldDescription)
	return r.resultLocked(len(r.st.Tags) - 1), nil
}

// mergeLocked folds a pending create into the keeper tag at index kept. It
// records a merge note naming both nodes and returns the kept tag. A retried
// push of the same pending tag returns the note it already recorded, so a push
// that failed after the keeper committed does not add a second note.
func (r *Registry) mergeLocked(w Write, kept int) WriteResult {
	if n, ok := r.mergeNoteLocked(w.TagID); ok {
		res := r.resultLocked(kept)
		res.Merge = &n
		return res
	}
	k := r.st.Tags[kept]
	note := Note{
		ID:         r.newID(),
		Kind:       NoteMerged,
		Name:       k.Name,
		ParentPath: pathOf(r.st.Tags, k.ParentID),
		KeptID:     k.ID,
		ReplacedID: w.TagID,
		Nodes:      uniqueNodes(k.OriginNode, w.OriginNode),
		MergedAt:   r.clock.Now(),
	}
	r.addNoteLocked(note)
	r.st.Revision++
	res := r.resultLocked(kept)
	res.Merge = &note
	return res
}

// mergeNoteLocked returns the merge note that recorded the merge of replacedID,
// if the registry holds one. Callers hold mu.
func (r *Registry) mergeNoteLocked(replacedID string) (Note, bool) {
	for _, n := range r.st.Notes {
		if n.Kind == NoteMerged && n.ReplacedID == replacedID {
			return n, true
		}
	}
	return Note{}, false
}

// editLocked changes one field of a tag: its name, color or description.
func (r *Registry) editLocked(w Write, field string) (WriteResult, error) {
	value := strings.TrimSpace(w.Name)
	if field == fieldName && value == "" {
		return WriteResult{}, fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	i, err := r.requireLocked(w.TagID)
	if err != nil {
		return WriteResult{}, err
	}
	if r.conflictLocked(w.TagID, field, w.BaseRevision) {
		return r.conflictResultLocked(i, field), nil
	}
	t := &r.st.Tags[i]
	switch field {
	case fieldName:
		t.Name = value
	case fieldColor:
		t.Color = w.Color
	case fieldDescription:
		t.Description = w.Description
	}
	t.UpdatedAt = r.clock.Now()
	r.bumpLocked(w.TagID, field)
	return r.resultLocked(i), nil
}

// reparentLocked moves a tag under another, or to the root when ParentID is
// empty. A move that would make the tag its own ancestor is refused.
func (r *Registry) reparentLocked(w Write) (WriteResult, error) {
	i, err := r.requireLocked(w.TagID)
	if err != nil {
		return WriteResult{}, err
	}
	if w.ParentID != "" && r.indexLocked(w.ParentID) < 0 {
		return WriteResult{}, fmt.Errorf("%w: parent %q", ErrTagNotFound, w.ParentID)
	}
	if r.conflictLocked(w.TagID, fieldParent, w.BaseRevision) {
		return r.conflictResultLocked(i, fieldParent), nil
	}
	if w.ParentID != "" && r.wouldCycleLocked(w.TagID, w.ParentID) {
		return WriteResult{}, fmt.Errorf("%w: %q under %q", ErrTagCycle, w.TagID, w.ParentID)
	}
	r.st.Tags[i].ParentID = w.ParentID
	r.st.Tags[i].UpdatedAt = r.clock.Now()
	r.bumpLocked(w.TagID, fieldParent)
	return r.resultLocked(i), nil
}

// deleteLocked removes a tag that has no children and no assignments here.
func (r *Registry) deleteLocked(w Write) (WriteResult, error) {
	i, err := r.checkDeleteLocked(w.TagID)
	if err != nil {
		return WriteResult{}, err
	}
	r.st.Tags = append(r.st.Tags[:i], r.st.Tags[i+1:]...)
	delete(r.st.FieldRevs, w.TagID)
	r.st.Revision++
	return WriteResult{Revision: r.st.Revision}, nil
}

// checkDeleteLocked reports whether a tag can be deleted and returns its index.
// It changes nothing, so a peer can run it before it forwards a delete.
func (r *Registry) checkDeleteLocked(id string) (int, error) {
	i, err := r.requireLocked(id)
	if err != nil {
		return 0, err
	}
	for _, t := range r.st.Tags {
		if t.ParentID == id {
			return 0, fmt.Errorf("%w: %q has child tag %q", ErrTagHasChildren, id, t.ID)
		}
	}
	n, err := r.store.AssignmentCount(id)
	if err != nil {
		return 0, fmt.Errorf("tag registry: count assignments: %w", err)
	}
	if n > 0 {
		return 0, &InUseError{TagID: id, Count: n}
	}
	return i, nil
}

// requireLocked returns the index of a tag, or ErrTagNotFound.
func (r *Registry) requireLocked(id string) (int, error) {
	i := r.indexLocked(id)
	if i < 0 {
		return 0, fmt.Errorf("%w: %q", ErrTagNotFound, id)
	}
	return i, nil
}

// indexLocked returns the index of a tag id, or -1.
func (r *Registry) indexLocked(id string) int {
	for i, t := range r.st.Tags {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// sameNameLocked returns the index of a tag under parent whose name matches
// name ignoring case, or -1.
func (r *Registry) sameNameLocked(parent, name string) int {
	for i, t := range r.st.Tags {
		if t.ParentID == parent && strings.EqualFold(t.Name, name) {
			return i
		}
	}
	return -1
}

// conflictLocked reports whether a field of a tag changed after the revision
// the writer based its edit on.
func (r *Registry) conflictLocked(id, field string, base int64) bool {
	return r.st.FieldRevs[id][field] > base
}

// conflictResultLocked reports the keeper's current value for a field the
// writer edited, with the conflict flag set. The keeper keeps that value.
func (r *Registry) conflictResultLocked(i int, field string) WriteResult {
	res := r.resultLocked(i)
	res.Conflict = true
	res.ConflictField = field
	return res
}

// bumpLocked advances the registry revision and stamps the given fields of a tag.
func (r *Registry) bumpLocked(id string, fields ...string) {
	r.st.Revision++
	if r.st.FieldRevs == nil {
		r.st.FieldRevs = map[string]map[string]int64{}
	}
	stamps := r.st.FieldRevs[id]
	if stamps == nil {
		stamps = map[string]int64{}
		r.st.FieldRevs[id] = stamps
	}
	for _, f := range fields {
		stamps[f] = r.st.Revision
	}
}

// wouldCycleLocked reports whether making id a child of parent would make id
// its own ancestor. It walks up from parent, and stops at a repeat.
func (r *Registry) wouldCycleLocked(id, parent string) bool {
	seen := map[string]bool{}
	for cur := parent; cur != ""; {
		if cur == id {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		i := r.indexLocked(cur)
		if i < 0 {
			return false
		}
		cur = r.st.Tags[i].ParentID
	}
	return false
}

// resultLocked returns the result for the tag at index i.
func (r *Registry) resultLocked(i int) WriteResult {
	return WriteResult{Tag: r.st.Tags[i], Revision: r.st.Revision}
}

// pathOf returns the names from the root down to id, joined with " / ". An
// empty id is the root and gives an empty path.
func pathOf(tags []Tag, id string) string {
	var names []string
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		seen[cur] = true
		found := false
		for _, t := range tags {
			if t.ID == cur {
				names = append([]string{t.Name}, names...)
				cur = t.ParentID
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return strings.Join(names, " / ")
}

// uniqueNodes returns the distinct non-empty node names, sorted.
func uniqueNodes(nodes ...string) []string {
	set := map[string]bool{}
	for _, n := range nodes {
		if n != "" {
			set[n] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
