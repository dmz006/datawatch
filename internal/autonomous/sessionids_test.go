// S13 follow — SessionIDsForPRD accessor tests.

package autonomous

import (
	"path/filepath"
	"testing"
)

func TestAPI_SessionIDsForPRD_UnknownPRD(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "prds.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	m := &Manager{store: st}
	a := &API{M: m}
	if got := a.SessionIDsForPRD("nope"); got != nil {
		t.Errorf("unknown PRD should return nil, got %v", got)
	}
}

func TestAPI_SessionIDsForPRD_ScheduledTasksOnly(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "prds.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	prd := &PRD{
		ID:   "p1",
		Spec: "test",
		Story: []Story{
			{
				ID: "s1", PRDID: "p1",
				Tasks: []Task{
					{ID: "t1", StoryID: "s1", PRDID: "p1", SessionID: "sess-a"},
					{ID: "t2", StoryID: "s1", PRDID: "p1"}, // not scheduled yet
					{ID: "t3", StoryID: "s1", PRDID: "p1", SessionID: "sess-b"},
				},
			},
			{
				ID: "s2", PRDID: "p1",
				Tasks: []Task{
					{ID: "t4", StoryID: "s2", PRDID: "p1", SessionID: "sess-c"},
				},
			},
		},
	}
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("save: %v", err)
	}
	m := &Manager{store: st}
	a := &API{M: m}
	got := a.SessionIDsForPRD("p1")
	want := map[string]bool{"sess-a": false, "sess-b": false, "sess-c": false}
	if len(got) != 3 {
		t.Fatalf("got %d session ids, want 3 (only scheduled tasks): %v", len(got), got)
	}
	for _, sid := range got {
		if _, ok := want[sid]; !ok {
			t.Errorf("unexpected session id %q", sid)
		}
		want[sid] = true
	}
	for sid, seen := range want {
		if !seen {
			t.Errorf("missing session id %q", sid)
		}
	}
}

func TestAPI_SessionIDsForPRD_EmptyPRD(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(filepath.Join(dir, "prds.json"))
	prd := &PRD{ID: "p2", Spec: "empty"}
	_ = st.SavePRD(prd)
	a := &API{M: &Manager{store: st}}
	got := a.SessionIDsForPRD("p2")
	if len(got) != 0 {
		t.Errorf("PRD with no tasks should return empty, got %v", got)
	}
}
