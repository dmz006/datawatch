package autonomous

import (
	"sort"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// BackfillLedger lists the leases a capacity ledger currently holds.
// *capacity.Ledger implements it (Ledger.Leases); tests use a fake.
type BackfillLedger interface {
	Leases() []capacity.Lease
}

// BackfillSessionStore reads a session's state and current system tags and
// replaces the system tags. *session.Manager implements it through
// GetSession and SetSystemTags; tests use a fake.
type BackfillSessionStore interface {
	GetSession(id string) (*session.Session, bool)
	SetSystemTags(id string, tags []string) error
}

// BackfillResult summarizes one startup placement-tag backfill run.
type BackfillResult struct {
	// Leases is the number of held leases scanned.
	Leases int
	// Backfilled is the number of sessions whose system tags were written.
	Backfilled int
	// Skipped is the number of leases that produced no write (empty
	// session id, unknown or finished session, or tags already in place).
	Skipped int
	// Failed is the number of tag writes that errored.
	Failed int
}

// BackfillPlacementTags restores the placement system tags of the running
// sessions that hold ledger leases, once at daemon startup after the
// capacity ledger has loaded the leases that survived the restart.
//
// For every held lease that names a session, the session is looked up and:
//   - skipped when it is unknown or no longer running (finished or killed);
//   - otherwise its placement tags are derived from the lease's facts (the
//     node the lease was granted on, and the PRD/Model it names) under the
//     live tags.placement block — exactly what TagTaskPlacement would have
//     derived when the task was placed — and replaced on the session when
//     they are missing or differ.
//
// Sessions that hold no lease are never touched. The run is idempotent: a
// session whose tags already match the derived set is left alone, so
// re-running it is a no-op. Operator tags (Session.Tags) and inherited
// tags are never part of either list and are never touched; only
// Session.SystemTags is written (session.Manager.SetSystemTags).
func BackfillPlacementTags(led BackfillLedger, store BackfillSessionStore, pc config.PlacementConfig) (res BackfillResult) {
	if led == nil || store == nil {
		return res
	}
	for _, ls := range led.Leases() {
		res.Leases++
		if ls.SessionID == "" {
			res.Skipped++
			continue
		}
		sess, ok := store.GetSession(ls.SessionID)
		if !ok || sess == nil || sessionFinished(sess.State) {
			// The lease outlives nothing: nothing to backfill, and an
			// unknown session means no placement ever landed.
			res.Skipped++
			continue
		}
		p := Placement{
			Node:      ls.Node,
			Model:     ls.Model,
			Automaton: ls.PRDID,
		}
		tags := DerivePlacementTags(p, pc)
		if tagsEqual(sess.SystemTags, tags) {
			// Already correct — idempotent no-op.
			res.Skipped++
			continue
		}
		if err := store.SetSystemTags(ls.SessionID, tags); err != nil {
			res.Failed++
			continue
		}
		res.Backfilled++
	}
	return res
}

// sessionFinished reports the terminal session states: a finished session
// keeps no lease and takes no backfill (state vocabulary: store.go).
func sessionFinished(st session.State) bool {
	switch st {
	case session.StateComplete, session.StateFailed, session.StateKilled:
		return true
	default:
		return false
	}
}

// tagsEqual compares two tag lists as sets: order and duplicates do not
// matter, which is all that matters for "the session already carries the
// derived tags".
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ka := make([]string, len(a))
	kb := make([]string, len(b))
	copy(ka, a)
	copy(kb, b)
	sort.Strings(ka)
	sort.Strings(kb)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}
