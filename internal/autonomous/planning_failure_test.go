package autonomous

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type pfFixture struct {
	m      *Manager
	prd    *PRD
	alerts []string
	mu     sync.Mutex
	replan []string
}

const pfBadSpec = "Release version 1.2.3 of the tool and tidy up."

func newPFFixture(t *testing.T, spec string) *pfFixture {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Skipf("manager: %v", err)
	}
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	m.decisionStore = store
	prd, err := m.store.CreatePRD(spec, "", "", "", "")
	if err != nil {
		t.Skipf("create: %v", err)
	}
	f := &pfFixture{m: m, prd: prd}
	m.SetAlertSink(func(level, title, body string) string {
		f.alerts = append(f.alerts, title+"|"+body)
		return "a1"
	})
	m.planningFailure.replan = func(id string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		p, _ := m.store.GetPRD(id)
		f.replan = append(f.replan, p.Spec)
		return nil
	}
	return f
}

// fail records a terminal failure the way the decompose give-up does.
func (f *pfFixture) fail(source, detail string) {
	f.m.retryDecomposeFailure(f.prd, "content", source, detail) // budget may allow a retry
	for f.m.retryBudgetRemaining(f.prd, "content") {
		f.m.retryDecomposeFailure(f.prd, "content", source, detail)
	}
	f.m.retryDecomposeFailure(f.prd, "content", source, detail)
}

func (f *pfFixture) failTerminal(source string, cause string) {
	f.m.failDecomposeAttempt(f.prd, source, errString(cause))
}

type errString string

func (e errString) Error() string { return string(e) }

func TestPlanningFailureRecordCarriesPhrasesAndCounts(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	detail := `story "S": fixed version number "1.2.3" in the story title — reword`
	f.m.retryDecomposeFailure(f.prd, "content", SpecRuleReleaseVersionFixed, detail)
	v := f.m.PlanningFailureView(f.prd)
	if v == nil {
		t.Fatal("no view")
	}
	if v.Source != SpecRuleReleaseVersionFixed || v.Category != "content" || v.Reason != detail {
		t.Fatalf("view: %+v", v)
	}
	found := false
	for _, p := range v.Phrases {
		if p == "1.2.3" || p == "version 1.2.3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("phrases: %v", v.Phrases)
	}
	if v.Retryable && v.RetryCounts["content"] == 0 {
		t.Fatalf("a retry was spent but counts are empty: %+v", v)
	}
	if v.RetryBudget != f.m.resolveRetryPolicy(f.prd, "content") {
		t.Fatalf("budget: %d", v.RetryBudget)
	}
}

func TestPlanningFailureNoViewWithoutFailure(t *testing.T) {
	f := newPFFixture(t, "clean spec")
	if f.m.PlanningFailureView(f.prd) != nil {
		t.Fatal("want nil view")
	}
}

func TestPlanningFailureOneAlertOnRepeatedDetection(t *testing.T) {
	f := newPFFixture(t, "Plan with a bad path /usr/local/bin/tool here.")
	f.m.retryDecomposeFailure(f.prd, "scope", "x", "boom") // scope budget is 0: not retryable
	for i := 0; i < 3; i++ {
		f.m.onPlanningFailureTerminal(f.prd)
	}
	if len(f.alerts) != 1 {
		t.Fatalf("alerts = %d: %v", len(f.alerts), f.alerts)
	}
	if !strings.Contains(f.alerts[0], "boom") {
		t.Fatalf("alert lacks reason: %s", f.alerts[0])
	}
}

func TestPlanningFailureQuestionRaisedOnce(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	f.m.retryDecomposeFailure(f.prd, "scope", SpecRuleReleaseVersionFixed, "bad version")
	f.m.onPlanningFailureTerminal(f.prd)
	f.m.onPlanningFailureTerminal(f.prd)
	qs := f.m.decisionStore.List(QuestionFilter{State: QuestionOpen, Asker: PlanningFailureQuestionAsker, PRDID: f.prd.ID, Limit: 10})
	if len(qs) != 1 {
		t.Fatalf("questions = %d", len(qs))
	}
	q := qs[0]
	if q.Default != PlanningFailureOptionLeave || len(q.Options) != 2 ||
		q.Options[0].Label != PlanningFailureLabelFix || q.Options[1].Label != PlanningFailureLabelLeave {
		t.Fatalf("question: %+v", q)
	}
	if strings.Contains(q.Text, "PRD") {
		t.Fatal("text must say Automaton")
	}
}

func TestPlanningFailureNoQuestionWhenNotCorrectable(t *testing.T) {
	f := newPFFixture(t, "Deploy to 10.0.0.5 now.") // private address: error, not correctable
	f.m.retryDecomposeFailure(f.prd, "scope", SpecRulePrivateAddress, "private address")
	f.m.onPlanningFailureTerminal(f.prd)
	if n := len(f.m.decisionStore.List(QuestionFilter{State: QuestionOpen, Asker: PlanningFailureQuestionAsker, Limit: 10})); n != 0 {
		t.Fatalf("questions = %d", n)
	}
	if len(f.alerts) != 1 {
		t.Fatalf("a failure that is not correctable still alerts once: %d", len(f.alerts))
	}
	v := f.m.PlanningFailureView(f.prd)
	if v == nil || v.Reason != "private address" || v.Retryable {
		t.Fatalf("view: %+v", v)
	}
}

func TestPlanningFailureRetryableRaisesNeither(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	f.m.retryDecomposeFailure(f.prd, "infra", "decompose-llm", "x")
	f.m.store.WithLock(func() { f.prd.LastDecomposeFailure.Retryable = true })
	f.m.onPlanningFailureTerminal(f.prd)
	if len(f.alerts) != 0 {
		t.Fatalf("alerts: %v", f.alerts)
	}
	if n := len(f.m.decisionStore.List(QuestionFilter{Asker: PlanningFailureQuestionAsker, Limit: 10})); n != 0 {
		t.Fatalf("questions = %d", n)
	}
}

func pfAnswer(f *pfFixture, option string) bool {
	q := BuildPlanningFailureQuestion(f.prd.ID, SpecRuleReleaseVersionFixed, LintSpecText(pfBadSpec))
	q.ID = "q1"
	return f.m.onPlanningFailureAnswer(QuestionChange{
		Kind: QuestionChangeAnswered, Question: q, Answer: &Answer{ID: "a1", QuestionID: "q1", ChosenOptionID: option},
	})
}

func TestPlanningFailureAnswerFixReplansWithRewordedSpec(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	if !pfAnswer(f, PlanningFailureOptionFix) {
		t.Fatal("not handled")
	}
	got, _ := f.m.store.GetPRD(f.prd.ID)
	if strings.Contains(got.Spec, "1.2.3") || !strings.Contains(got.Spec, "the release") {
		t.Fatalf("spec: %q", got.Spec)
	}
	if len(f.replan) != 1 || f.replan[0] != got.Spec {
		t.Fatalf("replan: %v", f.replan)
	}
	if len(LintSpecText(got.Spec)) != 0 {
		t.Fatalf("reworded spec still has findings: %v", LintSpecText(got.Spec))
	}
}

func TestPlanningFailureAnswerLeaveChangesNothing(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	before, _ := f.m.store.GetPRD(f.prd.ID)
	spec, status := before.Spec, before.Status
	if !pfAnswer(f, PlanningFailureOptionLeave) {
		t.Fatal("not handled")
	}
	got, _ := f.m.store.GetPRD(f.prd.ID)
	if got.Spec != spec || got.Status != status || len(f.replan) != 0 {
		t.Fatalf("changed: spec=%q status=%s replan=%v", got.Spec, got.Status, f.replan)
	}
}

func TestPlanningFailureAnswerIgnoresOtherAskers(t *testing.T) {
	f := newPFFixture(t, pfBadSpec)
	q := BuildBranchRefreshQuestion(f.prd.ID, "main", []string{"a.go"})
	if f.m.onPlanningFailureAnswer(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &Answer{}}) {
		t.Fatal("must not claim another asker's answer")
	}
}

func TestPlanningFailureRewordRules(t *testing.T) {
	got, n := RewordSpecText("Do a version bump to version 2.0.1 and fix BL123 using /usr/local/bin/jq today.")
	if n != 3 || strings.Contains(got, "2.0.1") || strings.Contains(got, "BL123") || strings.Contains(got, "/usr/local/bin/") {
		t.Fatalf("got %q n=%d", got, n)
	}
}
