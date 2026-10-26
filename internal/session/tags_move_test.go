package session

import (
	"reflect"
	"testing"
)

func TestTagRegistryMoveSessionTagsRewritesOwnTagsOnly(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"from", "to", "keep"}, map[string][]string{"p1": {"from"}})
	moved := saveTagsTestSession(t, mgr, "a1", "", []string{"from", "keep"})
	dup := saveTagsTestSession(t, mgr, "a2", "", []string{"to", "from"})
	other := saveTagsTestSession(t, mgr, "a3", "", []string{"keep"})
	inherits := saveTagsTestSession(t, mgr, "a4", "p1", []string{})

	changed, err := mgr.MoveSessionTags("from", "to")
	if err != nil {
		t.Fatalf("MoveSessionTags: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want the two sessions that carried from", changed)
	}
	if got := sessionOwnTags(t, mgr, moved.ID); !reflect.DeepEqual(got, []string{"to", "keep"}) {
		t.Fatalf("moved session tags = %v, want to then keep", got)
	}
	if got := sessionOwnTags(t, mgr, dup.ID); !reflect.DeepEqual(got, []string{"to"}) {
		t.Fatalf("session with both tags = %v, want the destination once", got)
	}
	if got := sessionOwnTags(t, mgr, other.ID); !reflect.DeepEqual(got, []string{"keep"}) {
		t.Fatalf("unrelated session changed: %v", got)
	}
	if got := sessionOwnTags(t, mgr, inherits.ID); len(got) != 0 {
		t.Fatalf("session with no own tags changed: %v", got)
	}
}

func TestTagRegistryMoveSessionTagsDropsWithEmptyDestination(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"gone", "keep"}, nil)
	sess := saveTagsTestSession(t, mgr, "b1", "", []string{"gone", "keep"})
	changed, err := mgr.MoveSessionTags("gone", "")
	if err != nil {
		t.Fatalf("MoveSessionTags drop: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if got := sessionOwnTags(t, mgr, sess.ID); !reflect.DeepEqual(got, []string{"keep"}) {
		t.Fatalf("tags after drop = %v, want keep only", got)
	}
}

// sessionOwnTags reads a saved session's own tags.
func sessionOwnTags(t *testing.T, mgr *Manager, id string) []string {
	t.Helper()
	sess, ok := mgr.GetSession(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}
	return sess.Tags
}
