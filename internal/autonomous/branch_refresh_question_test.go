package autonomous

import (
	"context"
	"strings"
	"testing"
)

type fakeBRRaiser struct{ got Question }

func (f *fakeBRRaiser) RaiseUnlessOpenSourceLine(q Question) (Question, bool, error) {
	f.got = q
	return q, true, nil
}

func TestBranchRefreshQuestionValidAndNamesFiles(t *testing.T) {
	r := &fakeBRRaiser{}
	if _, _, err := RaiseBranchRefreshQuestion(r, "p1", "main", []string{"a.go", "b.go"}); err != nil {
		t.Fatal(err)
	}
	q := r.got
	if err := ValidateNewQuestion(&q); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if len(q.Options) != 3 || !strings.Contains(q.Text, "a.go") || !strings.Contains(q.Text, "b.go") {
		t.Fatalf("bad question: %+v", q)
	}
	if strings.Contains(q.Text, "PRD") {
		t.Fatal("operator text must say Automaton")
	}
	if _, _, err := RaiseBranchRefreshQuestion(r, "p1", "main", nil); err == nil {
		t.Fatal("want error without files")
	}
}

func TestApplyBranchRefreshAnswer(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Skipf("manager: %v", err)
	}
	prd, err := m.store.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Skipf("create: %v", err)
	}
	for opt, want := range map[string]bool{BranchRefreshOptionLeave: true, BranchRefreshOptionResolve: true, BranchRefreshOptionRetry: false} {
		if err := m.ApplyBranchRefreshAnswer(prd.ID, opt); err != nil {
			t.Fatal(err)
		}
		got, _ := m.store.GetPRD(prd.ID)
		if got.BranchRefresh == nil || got.BranchRefresh.Stopped != want {
			t.Fatalf("%s: stopped want %v", opt, want)
		}
	}
	if err := m.ApplyBranchRefreshAnswer(prd.ID, "bogus"); err == nil {
		t.Fatal("want error")
	}
}

// brConflictRig raises the real conflict question through the store and wires
// the store's change hook to the Automaton API the way the daemon does.
func brConflictRig(t *testing.T) (*Manager, *brFake, *QuestionStore, Question) {
	t.Helper()
	f := brGit("6\n")
	f.fail["merge --no-ff --no-edit main"] = true
	f.out["diff --name-only --diff-filter=U"] = "a.go\n"
	m, prd, qs := brManager(t, true, f)
	api := NewAPI(m)
	qs.SetChangeHook(func(c QuestionChange) {
		m.RecordQuestionChange(c)
		api.OnQuestionChange(c)
	})
	m.maybeRefreshBranch(context.Background(), prd)
	open := qs.List(QuestionFilter{State: QuestionOpen, Asker: BranchRefreshQuestionAsker})
	if len(open) != 1 {
		t.Fatalf("open branch refresh questions = %d, want 1", len(open))
	}
	if r := brResult(m); r == nil || !r.Stopped {
		t.Fatalf("conflict must stop refreshing: %+v", r)
	}
	return m, f, qs, open[0]
}

func brAnswer(t *testing.T, qs *QuestionStore, q Question, option string) {
	t.Helper()
	if _, err := qs.Answer(q.ID, Answer{ChosenOptionID: option, Actor: "operator", Surface: "test"}); err != nil {
		t.Fatalf("answer %s: %v", option, err)
	}
}

func TestBranchRefreshAnswerRetryLaterThroughStoreRefreshesAgain(t *testing.T) {
	m, f, qs, q := brConflictRig(t)
	brAnswer(t, qs, q, BranchRefreshOptionRetry)
	r := brResult(m)
	if r == nil || r.Stopped {
		t.Fatalf("retry later must clear Stopped: %+v", r)
	}
	// The next pass tries the merge again (the fake still conflicts).
	merges := 0
	for _, c := range f.calls {
		if strings.Contains(c, "merge --no-ff --no-edit main") {
			merges++
		}
	}
	p, _ := m.store.GetPRD("p1")
	m.maybeRefreshBranch(context.Background(), p)
	after := 0
	for _, c := range f.calls {
		if strings.Contains(c, "merge --no-ff --no-edit main") {
			after++
		}
	}
	if after != merges+1 {
		t.Fatalf("merges %d -> %d, want one more after retry later", merges, after)
	}
	p, _ = m.store.GetPRD("p1")
	found := false
	for _, d := range p.Decisions {
		if d.Kind == "branch_refresh" && d.QuestionID == q.ID && strings.Contains(d.Note, "retry later") {
			found = true
		}
	}
	if !found {
		t.Fatal("the answer must be on the decision log with its question id")
	}
}

func TestBranchRefreshAnswerLeaveAndResolveKeepStopped(t *testing.T) {
	for _, opt := range []string{BranchRefreshOptionLeave, BranchRefreshOptionResolve} {
		m, f, qs, q := brConflictRig(t)
		brAnswer(t, qs, q, opt)
		if r := brResult(m); r == nil || !r.Stopped {
			t.Fatalf("%s must keep refreshing stopped: %+v", opt, r)
		}
		before := len(f.calls)
		p, _ := m.store.GetPRD("p1")
		m.maybeRefreshBranch(context.Background(), p)
		if len(f.calls) != before {
			t.Fatalf("%s: a stopped Automaton must not touch git", opt)
		}
	}
}

func TestBranchRefreshAnswerTextOnlyChangesNothing(t *testing.T) {
	m, _, qs, q := brConflictRig(t)
	if _, err := qs.Answer(q.ID, Answer{Quote: "looking at it", Actor: "operator", Surface: "test"}); err != nil {
		t.Fatal(err)
	}
	if r := brResult(m); r == nil || !r.Stopped {
		t.Fatalf("a text-only answer must leave refreshing stopped: %+v", r)
	}
}

func TestBranchRefreshOverrideSetResetsStopped(t *testing.T) {
	m, f, _, _ := brConflictRig(t)
	on := true
	if err := m.SetBranchRefreshOverride("p1", &on); err != nil {
		t.Fatal(err)
	}
	if r := brResult(m); r == nil || r.Stopped {
		t.Fatalf("setting the override must reset Stopped: %+v", r)
	}
	before := len(f.calls)
	p, _ := m.store.GetPRD("p1")
	m.maybeRefreshBranch(context.Background(), p)
	if len(f.calls) == before {
		t.Fatal("after the reset the next pass must run git again")
	}
	// Clearing with nothing stopped adds no decision.
	n := len(p.Decisions)
	if err := m.SetBranchRefreshOverride("p1", nil); err != nil {
		t.Fatal(err)
	}
	p2, _ := m.store.GetPRD("p1")
	if len(p2.Decisions) < n {
		t.Fatal("decisions must not shrink")
	}
}
