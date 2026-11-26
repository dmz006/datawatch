package session

import (
	"reflect"
	"testing"
	"time"
)

// A tag move changes an archived session where it is: the session stays in the
// archive and does not come back to the active list.
func TestMoveSessionTagsLeavesArchivedSessionsArchived(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"from", "to"}, nil)
	mgr.SetArchiveAfter(time.Hour)
	sess := saveTagsTestSession(t, mgr, "old1", "", []string{"from"})
	sess.State = StateComplete
	sess.UpdatedAt = time.Now().Add(-90 * 24 * time.Hour)
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if n, err := mgr.ArchiveSessions(time.Now()); err != nil || n != 1 {
		t.Fatalf("setup: archived %d, err %v; want 1", n, err)
	}

	changed, err := mgr.MoveSessionTags("from", "to")
	if err != nil || changed != 1 {
		t.Fatalf("MoveSessionTags = %d, %v; want the one archived session", changed, err)
	}
	if got := mgr.store.List(); len(got) != 0 {
		t.Fatalf("the archived session came back to the active list: %d active", len(got))
	}
	arc, ok := mgr.store.GetArchived(sess.FullID)
	if !ok || !reflect.DeepEqual(arc.Tags, []string{"to"}) {
		t.Fatalf("archived session = %+v ok=%v, want tags [to]", arc, ok)
	}
}
