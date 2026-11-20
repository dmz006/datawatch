package planrecords

import "testing"

// Guard: the plan roll-up feature must stay present after merges.
func TestPlanFeaturePresenceRollup(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := rollupPlan(t, s, "presence-rollup", "completed", "completed")
	ch, err := s.RollupPlan(p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if ch == nil || ch.To != StateDone {
		t.Fatalf("roll-up change = %+v, want state done", ch)
	}
	if r, _ := s.Get(p.ID); r.State != StateDone {
		t.Fatalf("plan state = %s, want done (derived from linked Automata)", r.State)
	}
	// A failed linked Automaton flips it to blocked.
	q := rollupPlan(t, s, "presence-rollup-failed", "failed", "completed")
	if _, err := s.RollupPlan(q.ID, false); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Get(q.ID); r.State != StateBlocked {
		t.Fatalf("plan state = %s, want blocked", r.State)
	}
}

// Guard: the plan rename/edit feature must stay present after merges.
func TestPlanFeaturePresenceRename(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := mustCreate(t, s, plan("Presence old"))
	got, err := s.RenamePlan(p.ID, "Presence new", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Presence new" {
		t.Fatalf("title = %q, want renamed", got.Title)
	}
	if err := ValidatePlanTitle(""); err == nil {
		t.Fatal("validation accepted an empty title")
	}
	if _, err := s.RenamePlan(p.ID, "   ", "tester"); err == nil {
		t.Fatal("RenamePlan accepted an empty title")
	}
	if r, _ := s.Get(p.ID); r.Title != "Presence new" {
		t.Fatalf("title changed by rejected rename: %q", r.Title)
	}
}
