package planrecords

import (
	"errors"
	"testing"
)

func TestDerivePlanStatus(t *testing.T) {
	cases := []struct {
		name   string
		states []string
		want   State
	}{
		{"empty", nil, StateOpen},
		{"running", []string{"running"}, StateOpen},
		{"running and done", []string{"completed", "running"}, StateOpen},
		{"all done", []string{"completed", "done", "archived"}, StateDone},
		{"all shipped", []string{"shipped", "shipped"}, StateShipped},
		{"mixed done and shipped", []string{"shipped", "completed"}, StateDone},
		{"failed", []string{"completed", "failed"}, StateBlocked},
		{"blocked", []string{"blocked", "running"}, StateBlocked},
		{"waiting work", []string{"completed", "draft"}, StateOpen},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := DerivePlanStatus(c.states)
			if got != c.want || reason == "" {
				t.Fatalf("got %q (%q), want %q", got, reason, c.want)
			}
		})
	}
}

func rollupPlan(t *testing.T, s *Store, title string, states ...string) Record {
	t.Helper()
	p, err := s.Create(Record{Kind: KindPlan, Project: "proj", Title: title})
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range states {
		if _, err := s.AttachAutomaton(p.ID, title+"-a"+string(rune('0'+i)), LinkInfo{Title: "t", State: st}); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestReconcilePlans(t *testing.T) {
	var audits []map[string]any
	s, err := Open(t.TempDir(), nil, Options{Audit: func(a, _ string, d map[string]any) {
		if a == AuditRolledUp {
			audits = append(audits, d)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	rollupPlan(t, s, "empty")
	rollupPlan(t, s, "running", "running", "completed")
	rollupPlan(t, s, "alldone", "completed", "done")
	rollupPlan(t, s, "allshipped", "shipped", "shipped")
	rollupPlan(t, s, "mixed", "shipped", "completed")
	rollupPlan(t, s, "failed", "failed", "completed")
	rollupPlan(t, s, "blocked", "blocked")
	hand := rollupPlan(t, s, "hand", "completed")
	st := StateCancelled
	if _, err := s.Update(hand.ID, Patch{State: &st}); err != nil {
		t.Fatal(err)
	}

	want := map[string]State{"alldone": StateDone, "allshipped": StateShipped, "mixed": StateDone,
		"failed": StateBlocked, "blocked": StateBlocked}

	// Dry run changes nothing.
	ch, err := s.ReconcilePlans("proj", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) != len(want)+1 {
		t.Fatalf("dry run reported %d changes, want %d: %+v", len(ch), len(want)+1, ch)
	}
	for _, id := range []string{"alldone", "failed"} {
		if r, _ := s.Get(id); r.State != StateOpen {
			t.Fatalf("dry run changed %s to %s", id, r.State)
		}
	}
	if len(audits) != 0 {
		t.Fatalf("dry run audited: %v", audits)
	}

	ch, err = s.ReconcilePlans("proj", false)
	if err != nil {
		t.Fatal(err)
	}
	kept := 0
	for _, c := range ch {
		if c.SetByHandKept {
			kept++
			if c.Plan != hand.ID || c.From != StateCancelled || c.To != StateDone {
				t.Fatalf("bad kept entry %+v", c)
			}
			continue
		}
		if want[c.Plan] != c.To || c.From != StateOpen || c.Reason == "" {
			t.Fatalf("bad change %+v", c)
		}
	}
	if kept != 1 {
		t.Fatalf("kept = %d, want 1", kept)
	}
	for id, w := range want {
		r, _ := s.Get(id)
		if r.State != w || r.SetByHand {
			t.Fatalf("%s: state %s set_by_hand %v, want %s false", id, r.State, r.SetByHand, w)
		}
	}
	if r, _ := s.Get("alldone"); r.ClosedAt == "" {
		t.Fatal("a done plan should be closed")
	}
	for _, id := range []string{"empty", "running"} {
		if r, _ := s.Get(id); r.State != StateOpen {
			t.Fatalf("%s should stay open, is %s", id, r.State)
		}
	}
	if r, _ := s.Get(hand.ID); r.State != StateCancelled || !r.SetByHand {
		t.Fatalf("hand-set plan: %s %v", r.State, r.SetByHand)
	}
	if len(audits) != len(want) {
		t.Fatalf("%d audit lines, want %d", len(audits), len(want))
	}
	for _, d := range audits {
		if d["actor"] != RollupActor {
			t.Fatalf("audit actor %v", d["actor"])
		}
	}

	// Idempotent: a second reconcile writes nothing.
	ch, err = s.ReconcilePlans("proj", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ch {
		if !c.SetByHandKept {
			t.Fatalf("second reconcile changed %+v", c)
		}
	}
	if len(audits) != len(want) {
		t.Fatal("second reconcile audited again")
	}
}

func TestSetByHandClearAndNoteLinkState(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := rollupPlan(t, s, "p", "running")
	id := "p-a0"
	ch, err := s.NoteLinkState(LinkKindAutomaton, id, LinkInfo{State: "completed"})
	if err != nil || ch == nil || ch.To != StateDone {
		t.Fatalf("NoteLinkState = %+v, %v", ch, err)
	}
	if r, _ := s.Get(p.ID); r.State != StateDone || r.LinkInfo["automaton:"+id].Title != "t" {
		t.Fatalf("plan %s, info %+v", r.State, r.LinkInfo)
	}
	if _, err := s.NoteLinkState(LinkKindAutomaton, "nope", LinkInfo{State: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	// Hand-set, then cleared, lets the roll-up write again.
	open := StateOpen
	if _, err := s.Update(p.ID, Patch{State: &open}); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Get(p.ID); !r.SetByHand {
		t.Fatal("explicit state should set set_by_hand")
	}
	if c, _ := s.RollupPlan(p.ID, false); c == nil || !c.SetByHandKept {
		t.Fatalf("hand-set not kept: %+v", c)
	}
	no := false
	if _, err := s.Update(p.ID, Patch{SetByHand: &no}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.RollupPlan(p.ID, false); c == nil || c.SetByHandKept || c.To != StateDone {
		t.Fatalf("cleared plan not rolled up: %+v", c)
	}
}
