package autonomous

import (
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
