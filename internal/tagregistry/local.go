package tagregistry

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The registry is not the only writer of the tag records: the daemon writes
// its auto tags to them directly. The functions in this file keep the registry
// from dropping a record it did not delete itself:
//
//   - before the registry reads or changes its tags it takes in what the
//     records hold (absorbLocked);
//   - a start adds the saved copy and the records together, and removes
//     nothing (loadCached);
//   - a store that can change single records is asked to remove only the tags
//     a change of the registry removed (mirrorLocked).

// changeStore is a Store that can save and remove single records. A Store
// without it gets the whole tag list through ApplySnapshot.
type changeStore interface {
	// ApplyChanges saves the tags in save and removes the ids in remove. Every
	// other record is left as it is.
	ApplyChanges(save []Tag, remove []string) error
}

// watchedStore is a Store that reports a record written or removed without the
// registry.
type watchedStore interface {
	// WatchLocalChanges registers fn, which runs after each such change. fn
	// takes the registry lock, so the store must not call it from inside one
	// of its Store methods.
	WatchLocalChanges(fn func())
}

// sameRecord reports whether two tags would be stored as the same record. The
// origin node and the sync flags are not part of the record.
func sameRecord(a, b Tag) bool {
	return a.ID == b.ID && a.Name == b.Name && a.ParentID == b.ParentID &&
		a.Color == b.Color && a.Description == b.Description && a.System == b.System &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}

// withRecord returns t with the record fields of rec. The fields only the
// registry holds stay those of t.
func withRecord(t, rec Tag) Tag {
	rec.OriginNode, rec.PendingSync, rec.Local = t.OriginNode, t.PendingSync, t.Local
	return rec
}

// tagsByID maps each tag's id to the tag.
func tagsByID(tags []Tag) map[string]Tag {
	out := make(map[string]Tag, len(tags))
	for _, t := range tags {
		out[t.ID] = t
	}
	return out
}

// absorbLocked makes the registry's tags follow the records for every change
// the daemon made to them without the registry. Every committed change of the
// registry is written to the records, so a difference found here is such a
// change. A record the registry does not know is local to this daemon: a sync
// from a keeper keeps it. Callers hold mu.
func (r *Registry) absorbLocked() error {
	records, err := r.store.ListTags()
	if err != nil {
		return fmt.Errorf("tag registry: list tags: %w", err)
	}
	unseen := tagsByID(records)
	tags := make([]Tag, 0, len(records))
	changed := false
	for _, have := range r.st.Tags {
		rec, ok := unseen[have.ID]
		if !ok {
			changed = true // the daemon removed its own record
			continue
		}
		delete(unseen, have.ID)
		if !sameRecord(have, rec) {
			have = withRecord(have, rec)
			changed = true
		}
		tags = append(tags, have)
	}
	for _, rec := range records {
		if _, ok := unseen[rec.ID]; !ok {
			continue
		}
		rec.OriginNode, rec.Local = r.node, true
		tags = append(tags, rec)
		changed = true
	}
	if changed {
		r.st.Tags = tags
		r.unsaved = true
	}
	return nil
}

// refreshLocked takes in the local changes and saves the copy when it changed.
// Callers hold mu.
func (r *Registry) refreshLocked() error {
	if err := r.absorbLocked(); err != nil {
		return err
	}
	if !r.unsaved {
		return nil
	}
	return r.saveLocked()
}

// AbsorbLocal takes the records the daemon wrote without the registry into the
// registry and its saved copy. A failure shows in View as the last error, and
// the next call tries again.
func (r *Registry) AbsorbLocal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.refreshLocked(); err != nil {
		r.lastErr = err.Error()
	}
}

// loadCached builds the starting state from the saved copy, already decoded
// into r.st, and the records. It removes nothing: a tag in either one is kept.
// A tag in both takes the side changed last, and the saved copy when neither
// is newer. The records then get what only the saved copy held.
func (r *Registry) loadCached() error {
	records, err := r.store.ListTags()
	if err != nil {
		return fmt.Errorf("tag registry: list tags: %w", err)
	}
	unseen := tagsByID(records)
	var save []Tag
	changed := false
	for i, cached := range r.st.Tags {
		rec, ok := unseen[cached.ID]
		delete(unseen, cached.ID)
		switch {
		case !ok:
			save = append(save, cached)
		case sameRecord(cached, rec):
		case rec.UpdatedAt.After(cached.UpdatedAt):
			r.st.Tags[i] = withRecord(cached, rec)
			changed = true
		default:
			save = append(save, cached)
		}
	}
	for _, rec := range records {
		if _, ok := unseen[rec.ID]; !ok {
			continue
		}
		rec.OriginNode, rec.Local = r.node, true
		r.st.Tags = append(r.st.Tags, rec)
		changed = true
	}
	if cs, ok := r.store.(changeStore); ok {
		err = cs.ApplyChanges(save, nil)
	} else {
		err = r.store.ApplySnapshot(copyTags(r.st.Tags))
	}
	if err != nil {
		return err
	}
	if changed {
		r.unsaved = true
		if err := r.saveLocked(); err != nil {
			r.lastErr = err.Error()
		}
	}
	return nil
}

// mirrorLocked writes the change from prev to the current tags to the records.
// Callers hold mu.
func (r *Registry) mirrorLocked(prev []Tag) error {
	cs, ok := r.store.(changeStore)
	if !ok {
		return r.store.ApplySnapshot(copyTags(r.st.Tags))
	}
	before := tagsByID(prev)
	var save []Tag
	for _, t := range r.st.Tags {
		old, had := before[t.ID]
		delete(before, t.ID)
		if !had || !sameRecord(old, t) {
			save = append(save, t)
		}
	}
	var remove []string
	for _, t := range prev {
		if _, gone := before[t.ID]; gone {
			remove = append(remove, t.ID)
		}
	}
	return cs.ApplyChanges(save, remove)
}

// markLocalAutoTags marks the auto tags among tags as local to this daemon, so
// a sync from a keeper keeps them and their assignments.
func markLocalAutoTags(tags []Tag) {
	for i := range tags {
		if autonomous.IsAutoTagRecord(tags[i].Description) {
			tags[i].Local = true
		}
	}
}
