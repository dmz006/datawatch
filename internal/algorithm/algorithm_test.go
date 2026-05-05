package algorithm

import (
	"testing"
)

func TestPhasesCanonicalOrder(t *testing.T) {
	all := Phases()
	want := []Phase{Observe, Orient, Decide, Act, Measure, Learn, Improve}
	if len(all) != len(want) {
		t.Fatalf("len: %d (want %d)", len(all), len(want))
	}
	for i, p := range all {
		if p != want[i] {
			t.Errorf("phase[%d]: %s (want %s)", i, p, want[i])
		}
	}
}

func TestStartStartsAtObserve(t *testing.T) {
	tr := NewTracker()
	s := tr.Start("sess-1")
	if s.Current != Observe {
		t.Errorf("start phase: %s", s.Current)
	}
	if s.SessionID != "sess-1" {
		t.Errorf("id: %s", s.SessionID)
	}
}

func TestStartIdempotent(t *testing.T) {
	tr := NewTracker()
	s1 := tr.Start("sess-1")
	if _, err := tr.Advance("sess-1", "first observation"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	s2 := tr.Start("sess-1") // should not reset
	if s2.Current != Orient {
		t.Errorf("Start re-init: %s (want Orient)", s2.Current)
	}
	_ = s1
}

func TestAdvanceWalksAllSevenPhasesAndCappedAtImprove(t *testing.T) {
	tr := NewTracker()
	tr.Start("sess-1")
	for i, want := range []Phase{Orient, Decide, Act, Measure, Learn, Improve} {
		got, err := tr.Advance("sess-1", "answer for phase "+string(want))
		if err != nil {
			t.Fatalf("advance[%d]: %v", i, err)
		}
		if got.Current != want {
			t.Errorf("advance[%d] -> %s (want %s)", i, got.Current, want)
		}
	}
	// One more advance: cap at Improve, history grows but Current stays.
	hLen := len(tr.Get("sess-1").History)
	got, err := tr.Advance("sess-1", "extra")
	if err != nil {
		t.Fatalf("advance past Improve: %v", err)
	}
	if got.Current != Improve {
		t.Errorf("post-cap phase: %s", got.Current)
	}
	if len(got.History) != hLen+1 {
		t.Errorf("history not extended: %d -> %d", hLen, len(got.History))
	}
}

func TestAdvanceUnknownSession(t *testing.T) {
	tr := NewTracker()
	if _, err := tr.Advance("nope", ""); err == nil {
		t.Errorf("expected error")
	}
}

func TestAbortBlocksAdvance(t *testing.T) {
	tr := NewTracker()
	tr.Start("sess-1")
	if _, err := tr.Abort("sess-1"); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if _, err := tr.Advance("sess-1", "after-abort"); err == nil {
		t.Error("expected advance error after abort")
	}
}

func TestEditMostRecentOutput(t *testing.T) {
	tr := NewTracker()
	tr.Start("sess-1")
	if _, err := tr.Advance("sess-1", "old observation"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := tr.Edit("sess-1", "new observation"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	s := tr.Get("sess-1")
	if got := s.History[0].Output; got != "new observation" {
		t.Errorf("edit not applied: %q", got)
	}
}

func TestEditWithoutHistory(t *testing.T) {
	tr := NewTracker()
	tr.Start("sess-1")
	if _, err := tr.Edit("sess-1", "x"); err == nil {
		t.Error("expected error on edit-without-history")
	}
}

func TestResetClearsState(t *testing.T) {
	tr := NewTracker()
	tr.Start("sess-1")
	_, _ = tr.Advance("sess-1", "x")
	tr.Reset("sess-1")
	if got := tr.Get("sess-1"); got != nil {
		t.Errorf("Get after Reset: %+v", got)
	}
}

func TestAllReturnsSnapshot(t *testing.T) {
	tr := NewTracker()
	tr.Start("a")
	tr.Start("b")
	if got := len(tr.All()); got != 2 {
		t.Errorf("All: %d", got)
	}
}

func TestIndexOf(t *testing.T) {
	if got := IndexOf(Observe); got != 0 {
		t.Errorf("Observe: %d", got)
	}
	if got := IndexOf(Improve); got != 6 {
		t.Errorf("Improve: %d", got)
	}
	if got := IndexOf("nope"); got != -1 {
		t.Errorf("invalid: %d", got)
	}
}

func TestIsValid(t *testing.T) {
	for _, p := range Phases() {
		if !IsValid(p) {
			t.Errorf("IsValid(%s) = false", p)
		}
	}
	if IsValid("not-a-phase") {
		t.Error("IsValid(not-a-phase) = true")
	}
}
