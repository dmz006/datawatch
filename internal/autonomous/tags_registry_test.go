package autonomous

import (
	"testing"
)

// fakeTagMover is a session side that records the moves it is asked for. It
// is also the session tag counter, as the session manager is in production.
type fakeTagMover struct {
	moves []string
	n     int
}

func (f *fakeTagMover) CountSessionsWithTag(string) int     { return 0 }
func (f *fakeTagMover) SessionEffectiveTagSets() [][]string { return nil }

func (f *fakeTagMover) MoveSessionTags(from, to string) (int, error) {
	f.moves = append(f.moves, from+">"+to)
	return f.n, nil
}

func TestTagRegistrySaveTagRecordsReplacesRecords(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	keep := mustTag(t, m, "keep", "")
	drop := mustTag(t, m, "drop", "")
	if err := m.SaveTagRecords([]*Tag{{ID: keep.ID, Name: "kept"}, {ID: "new1", Name: "fresh"}}); err != nil {
		t.Fatalf("SaveTagRecords: %v", err)
	}
	if _, ok := st.GetTag(drop.ID); ok {
		t.Fatal("a tag missing from the list was kept")
	}
	if got, ok := st.GetTag(keep.ID); !ok || got.Name != "kept" {
		t.Fatalf("kept tag = %+v ok=%v, want the new name", got, ok)
	}
	if _, ok := st.GetTag("new1"); !ok {
		t.Fatal("a new tag was not saved")
	}
}

func TestTagRegistryAssignmentCountCountsAutomata(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	tg := mustTag(t, m, "t", "")
	mustPRD(t, st, "p1")
	if err := m.SetPRDTags("p1", []string{tg.ID}); err != nil {
		t.Fatal(err)
	}
	if got := m.TagAssignmentCount(tg.ID); got != 1 {
		t.Fatalf("TagAssignmentCount = %d, want the one Automaton", got)
	}
}

func TestTagRegistryRewriteMovesAutomataAndKeepsOneCopy(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	from := mustTag(t, m, "from", "")
	to := mustTag(t, m, "to", "")
	other := mustTag(t, m, "other", "")
	mustPRD(t, st, "both")
	mustPRD(t, st, "only-from")
	mustPRD(t, st, "unrelated")
	if err := m.SetPRDTagsAndPrimary("both", []string{from.ID, to.ID, other.ID}, strPtr(from.ID)); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPRDTags("only-from", []string{from.ID}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPRDTags("unrelated", []string{other.ID}); err != nil {
		t.Fatal(err)
	}

	changed, err := m.RewriteTagAssignments(from.ID, to.ID)
	if err != nil {
		t.Fatalf("RewriteTagAssignments: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2 Automata", changed)
	}
	both, _ := st.GetPRD("both")
	if len(both.TagIDs) != 2 || both.TagIDs[0] != to.ID || both.TagIDs[1] != other.ID {
		t.Fatalf("both tags = %v, want the destination once and the other tag", both.TagIDs)
	}
	if both.PrimaryTagID != to.ID {
		t.Fatalf("primary = %q, want it moved to %q", both.PrimaryTagID, to.ID)
	}
	only, _ := st.GetPRD("only-from")
	if len(only.TagIDs) != 1 || only.TagIDs[0] != to.ID {
		t.Fatalf("only-from tags = %v", only.TagIDs)
	}
	unrelated, _ := st.GetPRD("unrelated")
	if len(unrelated.TagIDs) != 1 || unrelated.TagIDs[0] != other.ID {
		t.Fatalf("unrelated Automaton changed: %v", unrelated.TagIDs)
	}
}

func TestTagRegistryRemoveDropsAssignmentAndPrimary(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	gone := mustTag(t, m, "gone", "")
	keep := mustTag(t, m, "keep", "")
	mustPRD(t, st, "p1")
	if err := m.SetPRDTagsAndPrimary("p1", []string{gone.ID, keep.ID}, strPtr(gone.ID)); err != nil {
		t.Fatal(err)
	}
	changed, err := m.RemoveTagAssignments(gone.ID)
	if err != nil {
		t.Fatalf("RemoveTagAssignments: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	p, _ := st.GetPRD("p1")
	if len(p.TagIDs) != 1 || p.TagIDs[0] != keep.ID || p.PrimaryTagID != "" {
		t.Fatalf("after remove: tags %v primary %q", p.TagIDs, p.PrimaryTagID)
	}
}

func TestTagRegistryMovesSessionsThroughCounter(t *testing.T) {
	m, _, _ := newTagTestManager(t)
	mover := &fakeTagMover{n: 3}
	m.SetSessionTagCounter(mover)
	changed, err := m.RemoveTagAssignments("x")
	if err != nil {
		t.Fatalf("RemoveTagAssignments: %v", err)
	}
	if changed != 3 || len(mover.moves) != 1 || mover.moves[0] != "x>" {
		t.Fatalf("changed = %d moves = %v, want the session side asked to drop x", changed, mover.moves)
	}
	if _, err := m.RewriteTagAssignments("x", "y"); err != nil {
		t.Fatalf("RewriteTagAssignments: %v", err)
	}
	if mover.moves[1] != "x>y" {
		t.Fatalf("moves = %v, want x moved to y", mover.moves)
	}
}

func TestTagRegistryReplaceTagIDDropsAndDedupes(t *testing.T) {
	out, hit := replaceTagID([]string{"a", "b", "a"}, "a", "")
	if !hit || len(out) != 1 || out[0] != "b" {
		t.Fatalf("drop a: %v hit=%v", out, hit)
	}
	out, hit = replaceTagID([]string{"b", "a"}, "a", "b")
	if !hit || len(out) != 1 || out[0] != "b" {
		t.Fatalf("rewrite a to b: %v hit=%v", out, hit)
	}
	in := []string{"c"}
	out, hit = replaceTagID(in, "a", "b")
	if hit || len(out) != 1 || &out[0] != &in[0] {
		t.Fatalf("no match should return the input unchanged: %v hit=%v", out, hit)
	}
}

func strPtr(s string) *string { return &s }
