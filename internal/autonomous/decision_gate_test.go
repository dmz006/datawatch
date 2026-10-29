package autonomous

import (
	"path/filepath"
	"strings"
	"testing"
)

func newDecisionGateStore(t *testing.T) *QuestionStore {
	t.Helper()
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	return s
}

func TestDecisionGateNoRefsNoHolds(t *testing.T) {
	s := newDecisionGateStore(t)
	if got := DecisionHold(s, "do the work, nothing to wait for; q-123 is too short"); len(got) != 0 {
		t.Fatalf("holds = %+v; want none", got)
	}
	if got := DecisionRefs(""); len(got) != 0 {
		t.Fatalf("refs = %v; want none", got)
	}
}

func TestDecisionGateAnsweredPasses(t *testing.T) {
	s := newDecisionGateStore(t)
	id := mustRaise(t, s, sampleQuestion("Which way?"))
	if _, err := s.Answer(id, Answer{Quote: "go first"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got := DecisionHold(s, "blocked on "+id+" please"); len(got) != 0 {
		t.Fatalf("holds = %+v; want none for an answered decision", got)
	}
}

func TestDecisionGateOpenHoldsWithText(t *testing.T) {
	s := newDecisionGateStore(t)
	id := mustRaise(t, s, sampleQuestion("Which way should we go?"))
	got := DecisionHold(s, "see "+id)
	if len(got) != 1 {
		t.Fatalf("holds = %+v; want 1", got)
	}
	if got[0].ID != id || got[0].State != "open" {
		t.Errorf("hold = %+v; want id %s state open", got[0], id)
	}
	want := "operator decision " + id + " is waiting for an answer: Which way should we go?"
	if got[0].Reason != want {
		t.Errorf("reason = %q; want %q", got[0].Reason, want)
	}
}

func TestDecisionGateOpenReasonTruncatesText(t *testing.T) {
	s := newDecisionGateStore(t)
	id := mustRaise(t, s, sampleQuestion(strings.Repeat("x", 400)))
	got := DecisionHold(s, id)
	if len(got) != 1 {
		t.Fatalf("holds = %+v; want 1", got)
	}
	prefix := "operator decision " + id + " is waiting for an answer: "
	if !strings.HasPrefix(got[0].Reason, prefix) {
		t.Fatalf("reason = %q; want prefix %q", got[0].Reason, prefix)
	}
	if n := len(got[0].Reason) - len(prefix); n != 160 {
		t.Errorf("quoted text length = %d; want 160", n)
	}
}

func TestDecisionGateWithdrawnExpiredMissingHold(t *testing.T) {
	s := newDecisionGateStore(t)
	wid := mustRaise(t, s, sampleQuestion("Withdraw me?"))
	if _, err := s.Withdraw(wid, "operator", "not needed"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	eid := mustRaise(t, s, sampleQuestion("Expire me?"))
	if _, err := s.Expire(eid, "stale"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	const mid = "q-0123456789abcdef0123"

	cases := []struct{ id, state, reason string }{
		{wid, "withdrawn", "operator decision " + wid + " was withdrawn without an answer"},
		{eid, "expired", "operator decision " + eid + " was expired without an answer"},
		{mid, "missing", "operator decision " + mid + " was not found"},
	}
	for _, c := range cases {
		got := DecisionHold(s, "needs "+c.id)
		if len(got) != 1 {
			t.Fatalf("%s: holds = %+v; want 1", c.state, got)
		}
		if got[0].ID != c.id || got[0].State != c.state || got[0].Reason != c.reason {
			t.Errorf("%s: hold = %+v; want state %q reason %q", c.state, got[0], c.state, c.reason)
		}
	}
}

func TestDecisionGateTwoIdsBothChecked(t *testing.T) {
	s := newDecisionGateStore(t)
	open1 := mustRaise(t, s, sampleQuestion("First?"))
	answered := mustRaise(t, s, sampleQuestion("Second?"))
	if _, err := s.Answer(answered, Answer{Quote: "yes"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	const mid = "q-ffffffffffffffffffff"
	got := DecisionHold(s, open1+" and "+answered+" and "+mid)
	if len(got) != 2 {
		t.Fatalf("holds = %+v; want 2 (open and missing)", got)
	}
	if got[0].ID != open1 || got[0].State != "open" || got[1].ID != mid || got[1].State != "missing" {
		t.Errorf("holds = %+v", got)
	}
}

func TestDecisionGateSameIdReportedOnce(t *testing.T) {
	s := newDecisionGateStore(t)
	id := mustRaise(t, s, sampleQuestion("Once?"))
	if refs := DecisionRefs(id + " then again " + id); len(refs) != 1 || refs[0] != id {
		t.Fatalf("refs = %v; want [%s]", refs, id)
	}
	if got := DecisionHold(s, id+" then again "+id); len(got) != 1 {
		t.Fatalf("holds = %+v; want 1", got)
	}
}

func TestDecisionGateWholeTokenOnly(t *testing.T) {
	const id = "q-0123456789abcdef0123"
	for _, spec := range []string{"x" + id, id + "z", "Q-0123456789abcdef0123", "q-0123456789abcdef012"} {
		if refs := DecisionRefs(spec); len(refs) != 0 {
			t.Errorf("DecisionRefs(%q) = %v; want none", spec, refs)
		}
	}
	if refs := DecisionRefs("(" + id + ")."); len(refs) != 1 || refs[0] != id {
		t.Errorf("punctuated id refs = %v; want [%s]", refs, id)
	}
}

func TestDecisionGateNilStoreHolds(t *testing.T) {
	const id = "q-0123456789abcdef0123"
	got := DecisionHold(nil, "waits on "+id)
	if len(got) != 1 || got[0].ID != id || got[0].State != "missing" {
		t.Fatalf("holds = %+v; want one missing hold", got)
	}
	if got := DecisionHold(nil, "no ids here"); len(got) != 0 {
		t.Fatalf("holds = %+v; want none", got)
	}
}
