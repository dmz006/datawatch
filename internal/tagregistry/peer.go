package tagregistry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// The functions in this file are the peer side. A keeper or standalone daemon
// applies its own edits with localWrite and never forwards them. A peer forwards
// each edit to the keeper while the keeper is reachable, and refuses edits that
// need the keeper while it is not. A peer creates a tag without the keeper and
// marks it pending sync, then pushes it when the keeper returns.

// Create makes a tag. A peer forwards it to the keeper when the keeper is
// reachable. Otherwise, or if the keeper cannot be reached, the tag is kept
// locally, marked pending sync, and pushed by the next sync.
func (r *Registry) Create(ctx context.Context, in CreateInput) (WriteResult, error) {
	w := Write{
		Kind:        WriteCreate,
		TagID:       r.newID(),
		Name:        in.Name,
		ParentID:    in.ParentID,
		Color:       in.Color,
		Description: in.Description,
		OriginNode:  r.node,
	}
	if r.Role() != RolePeer {
		return r.localWrite(w)
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()
	if r.isReachable() {
		res, err := r.forward(ctx, w)
		if err == nil {
			if err := r.adopt(w, res); err != nil {
				return WriteResult{}, err
			}
			_ = r.syncNoLock(ctx)
			return res, nil
		}
		if !errors.Is(err, ErrKeeperUnreachable) {
			return WriteResult{}, err
		}
	}
	return r.createPending(w)
}

// Rename changes a tag's name.
func (r *Registry) Rename(ctx context.Context, id, name string) (WriteResult, error) {
	return r.edit(ctx, "rename", Write{Kind: WriteRename, TagID: id, Name: name})
}

// Reparent moves a tag under parentID, or to the root when parentID is empty.
func (r *Registry) Reparent(ctx context.Context, id, parentID string) (WriteResult, error) {
	return r.edit(ctx, "re-parent", Write{Kind: WriteReparent, TagID: id, ParentID: parentID})
}

// Recolor sets a tag's color.
func (r *Registry) Recolor(ctx context.Context, id, color string) (WriteResult, error) {
	return r.edit(ctx, "recolor", Write{Kind: WriteRecolor, TagID: id, Color: color})
}

// Describe sets a tag's description.
func (r *Registry) Describe(ctx context.Context, id, description string) (WriteResult, error) {
	return r.edit(ctx, "describe", Write{Kind: WriteDescribe, TagID: id, Description: description})
}

// Delete removes a tag. A peer refuses a tag that it still has assignments on,
// as it refuses one with children, and the keeper applies its own checks too.
func (r *Registry) Delete(ctx context.Context, id string) (WriteResult, error) {
	w := Write{Kind: WriteDelete, TagID: id}
	if r.Role() != RolePeer {
		return r.localWrite(w)
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()
	if err := r.checkDeleteLocal(id); err != nil {
		return WriteResult{}, err
	}
	return r.forwardEdit(ctx, "delete", w)
}

// edit applies a field edit on a peer by forwarding it, or on a keeper locally.
func (r *Registry) edit(ctx context.Context, op string, w Write) (WriteResult, error) {
	if r.Role() != RolePeer {
		return r.localWrite(w)
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()
	if err := r.checkEditLocal(w); err != nil {
		return WriteResult{}, err
	}
	return r.forwardEdit(ctx, op, w)
}

// forwardEdit sends an edit to the keeper, adopts the keeper's value and
// refreshes the copy so the next edit is based on the current revision. It
// refuses with ErrKeeperUnreachable when the keeper is not reachable. The
// caller holds opMu.
func (r *Registry) forwardEdit(ctx context.Context, op string, w Write) (WriteResult, error) {
	if !r.isReachable() {
		return WriteResult{}, unreachableErr(op, r.KeeperName())
	}
	w.BaseRevision = r.revision()
	res, err := r.forward(ctx, w)
	if errors.Is(err, ErrKeeperUnreachable) {
		return WriteResult{}, unreachableErr(op, r.KeeperName())
	}
	if err != nil {
		return WriteResult{}, err
	}
	if err := r.adopt(w, res); err != nil {
		return WriteResult{}, err
	}
	_ = r.syncNoLock(ctx)
	return res, nil
}

// unreachableErr is the refusal for an edit that needs the keeper.
func unreachableErr(op, keeper string) error {
	return fmt.Errorf("%w: %s is refused while keeper %q is unreachable; the last copy is shown", ErrKeeperUnreachable, op, keeper)
}

// checkEditLocal rejects an edit that cannot apply, before any call to the keeper.
func (r *Registry) checkEditLocal(w Write) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w.Kind == WriteRename && strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrTagInvalid)
	}
	_, err := r.requireLocked(w.TagID)
	return err
}

// checkDeleteLocal runs the delete checks against this daemon's copy.
func (r *Registry) checkDeleteLocal(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.checkDeleteLocked(id)
	return err
}

// createPending keeps a tag made while the keeper is unreachable.
func (r *Registry) createPending(w Write) (WriteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res WriteResult
	err := r.mutateLocked(func() error {
		var err error
		res, err = r.applyLocked(w)
		if err != nil {
			return err
		}
		r.st.Tags[r.indexLocked(w.TagID)].PendingSync = true
		return nil
	})
	if err != nil {
		return WriteResult{}, err
	}
	res.Tag = r.st.Tags[r.indexLocked(w.TagID)]
	return res, nil
}

// forward sends one write to the keeper and records whether the keeper answered.
func (r *Registry) forward(ctx context.Context, w Write) (WriteResult, error) {
	res, err := r.client.Write(ctx, w)
	r.setReachable(!errors.Is(err, ErrKeeperUnreachable))
	return res, err
}

// adopt applies the keeper's answer to an edit to the local copy.
func (r *Registry) adopt(w Write, res WriteResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mutateLocked(func() error {
		if w.Kind == WriteDelete {
			r.dropTagLocked(w.TagID, "")
		} else {
			r.upsertLocked(res.Tag)
		}
		return nil
	})
}

// Sync pushes this peer's pending tags and then pulls the keeper's snapshot. It
// does nothing on a standalone or keeper daemon. A keeper that cannot be reached
// marks the copy stale and returns ErrKeeperUnreachable.
func (r *Registry) Sync(ctx context.Context) error {
	if r.Role() != RolePeer {
		return nil
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()
	return r.syncNoLock(ctx)
}

// syncNoLock is Sync for a caller that holds opMu. Pending tags are pushed
// first, so the snapshot that follows already includes any merges. A push that
// the keeper refuses leaves its tag pending, and the error is returned after
// the snapshot is applied.
func (r *Registry) syncNoLock(ctx context.Context) error {
	pushErr := r.pushPending(ctx)
	if errors.Is(pushErr, ErrKeeperUnreachable) {
		r.recordErr(pushErr)
		return pushErr
	}
	snap, err := r.client.FetchSnapshot(ctx)
	if err != nil {
		r.setReachable(!errors.Is(err, ErrKeeperUnreachable))
		r.recordErr(err)
		return err
	}
	r.setReachable(true)
	if err := r.applySnapshot(snap); err != nil {
		r.recordErr(err)
		return err
	}
	r.recordErr(pushErr)
	return pushErr
}

// pushPending sends each pending tag to the keeper, oldest first. A refusal is
// kept and the next tag still goes. Only an unreachable keeper stops the push.
func (r *Registry) pushPending(ctx context.Context) error {
	var firstErr error
	for _, id := range r.pendingIDs() {
		w, ok := r.pendingWrite(id)
		if !ok {
			continue
		}
		res, err := r.forward(ctx, w)
		if errors.Is(err, ErrKeeperUnreachable) {
			return err
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("push tag %q: %w", w.Name, err)
			}
			continue
		}
		if err := r.adoptPush(id, res); err != nil {
			return err
		}
	}
	return firstErr
}

// pendingIDs returns the ids of the pending tags, oldest first.
func (r *Registry) pendingIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending []Tag
	for _, t := range r.st.Tags {
		if t.PendingSync {
			pending = append(pending, t)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if !pending[i].CreatedAt.Equal(pending[j].CreatedAt) {
			return pending[i].CreatedAt.Before(pending[j].CreatedAt)
		}
		return pending[i].ID < pending[j].ID
	})
	ids := make([]string, 0, len(pending))
	for _, t := range pending {
		ids = append(ids, t.ID)
	}
	return ids
}

// pendingWrite builds the push for a pending tag from its current values. A
// parent that a merge already moved is read fresh here.
func (r *Registry) pendingWrite(id string) (Write, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexLocked(id)
	if i < 0 || !r.st.Tags[i].PendingSync {
		return Write{}, false
	}
	t := r.st.Tags[i]
	return Write{
		Kind:        WriteCreate,
		TagID:       t.ID,
		Name:        t.Name,
		ParentID:    t.ParentID,
		Color:       t.Color,
		Description: t.Description,
		OriginNode:  t.OriginNode,
		Pending:     true,
	}, true
}

// adoptPush applies the keeper's answer to a pushed pending tag. A merge rewrites
// this daemon's assignments to the kept tag, moves children of the replaced tag
// to the kept tag, and records the merge note.
func (r *Registry) adoptPush(pendingID string, res WriteResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mutateWithStoreLocked(func(steps *[]storeStep) error {
		if res.Merge == nil {
			r.upsertLocked(res.Tag)
			return nil
		}
		kept := res.Tag
		*steps = append(*steps, func() error {
			if _, err := r.store.RewriteAssignments(pendingID, kept.ID); err != nil {
				return fmt.Errorf("tag registry: rewrite assignments of %q: %w", pendingID, err)
			}
			return nil
		})
		r.dropTagLocked(pendingID, kept.ID)
		r.upsertLocked(kept)
		r.addNoteLocked(*res.Merge)
		return nil
	})
}

// applySnapshot replaces the keeper's tags with the snapshot and keeps the
// pending tags. A tag the keeper no longer has is removed from this daemon's
// assignments, and a note records it when there were any. A pending tag whose
// parent is removed moves to the root.
//
// When the tags or the notes change, Options.OnSnapshotChange runs after the
// lock is released.
func (r *Registry) applySnapshot(snap Snapshot) error {
	changed, err := r.applySnapshotChanged(snap)
	if err == nil && changed && r.onChange != nil {
		r.onChange()
	}
	return err
}

// applySnapshotChanged applies a snapshot and reports whether the tags or the
// notes this daemon holds differ afterwards.
func (r *Registry) applySnapshotChanged(snap Snapshot) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	beforeTags, beforeNotes := copyTags(r.st.Tags), copyNotes(r.st.Notes)
	err := r.mutateWithStoreLocked(func(steps *[]storeStep) error {
		return r.applySnapshotLocked(snap, steps)
	})
	if err != nil {
		return false, err
	}
	return !sameTags(beforeTags, r.st.Tags) || !sameNotes(beforeNotes, r.st.Notes), nil
}

// sameTags reports whether two tag lists hold the same tags in the same order.
func sameTags(a, b []Tag) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// sameNotes reports whether two note lists hold the same notes in the same order.
func sameNotes(a, b []Note) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// applySnapshotLocked is applySnapshot's change. A tag the keeper no longer has
// is counted in the store before anything changes. When it has assignments, the
// removed-on-keeper note goes into the state and the removal goes into steps,
// so both commit together or neither does. Callers hold mu through
// mutateWithStoreLocked.
func (r *Registry) applySnapshotLocked(snap Snapshot, steps *[]storeStep) error {
	known := map[string]bool{}
	for _, t := range snap.Tags {
		known[t.ID] = true
	}
	before := copyTags(r.st.Tags)
	gone := map[string]bool{}
	for _, t := range before {
		if t.PendingSync || known[t.ID] {
			continue
		}
		gone[t.ID] = true
		n, err := r.store.AssignmentCount(t.ID)
		if err != nil {
			return fmt.Errorf("tag registry: count assignments of %q: %w", t.ID, err)
		}
		if n == 0 {
			continue
		}
		id := t.ID
		*steps = append(*steps, func() error {
			if _, err := r.store.RemoveAssignments(id); err != nil {
				return fmt.Errorf("tag registry: drop assignments of %q: %w", id, err)
			}
			return nil
		})
		r.addNoteLocked(Note{
			ID:         r.newID(),
			Kind:       NoteRemovedOnKeeper,
			Name:       t.Name,
			ParentPath: pathOf(before, t.ParentID),
			TagID:      t.ID,
			MergedAt:   r.clock.Now(),
		})
	}
	tags := make([]Tag, 0, len(snap.Tags))
	for _, t := range snap.Tags {
		t.PendingSync = false
		tags = append(tags, t)
	}
	for _, t := range before {
		if t.PendingSync && !known[t.ID] {
			if gone[t.ParentID] {
				t.ParentID = ""
			}
			tags = append(tags, t)
		}
	}
	r.st.Tags = tags
	for _, n := range snap.Notes {
		r.addNoteLocked(n)
	}
	r.st.Revision = snap.Revision
	r.st.LastSyncedAt = r.clock.Now()
	return nil
}

// dropTagLocked removes a tag. Its children move to repointTo, which is the
// root for a deleted tag and the kept tag for a merge. Callers hold mu.
func (r *Registry) dropTagLocked(id, repointTo string) {
	kept := make([]Tag, 0, len(r.st.Tags))
	for _, t := range r.st.Tags {
		if t.ID == id {
			continue
		}
		if t.ParentID == id {
			t.ParentID = repointTo
		}
		kept = append(kept, t)
	}
	r.st.Tags = kept
}

// upsertLocked stores a keeper tag, replacing one with the same id. A keeper
// tag is never pending. Callers hold mu.
func (r *Registry) upsertLocked(t Tag) {
	t.PendingSync = false
	if i := r.indexLocked(t.ID); i >= 0 {
		r.st.Tags[i] = t
		return
	}
	r.st.Tags = append(r.st.Tags, t)
}
