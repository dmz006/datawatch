package autonomous

import (
	"path/filepath"
	"testing"
	"time"
)

// costFixture is a manager with a ledger on a fake clock and one PRD whose
// task, verifier, judge and decompose sessions are s-task, s-verify, s-judge
// and s-decomp.
type costFixture struct {
	m   *Manager
	led *BudgetLedger
	clk *budgetFakeClock
}

func newCostManager(t *testing.T) costFixture {
	t.Helper()
	clk := &budgetFakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	led := NewBudgetLedger(filepath.Join(t.TempDir(), "budget-ledger.jsonl"))
	led.Clock = clk.Now
	m.SetCostSources(led, nil)
	return costFixture{m: m, led: led, clk: clk}
}

// newCostPRD creates a PRD with the given worker, verifier and judge session
// ids on a single task and verdict.
func newCostPRD(t *testing.T, m *Manager, task, verify, judge string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("cost spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	stored.Story = []Story{{
		ID: "st1", PRDID: prd.ID,
		Tasks: []Task{{
			ID: "t1", StoryID: "st1", PRDID: prd.ID, SessionID: task,
			Verification: &VerificationResult{VerifierID: verify},
			Verdicts:     []GuardrailVerdict{{Guardrail: "rules", JudgeSessionID: judge}},
		}},
	}}
	if err := m.Store().SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

func TestPRDCostUSDSumsTaskDecomposeAndVerifySessions(t *testing.T) {
	f := newCostManager(t)
	prd := newCostPRD(t, f.m, "s-task", "s-verify", "s-judge")
	f.m.SetDecomposeSessionsFn(func(id string) []string {
		if id == prd.ID {
			return []string{"s-decomp", "s-task", ""}
		}
		return nil
	})
	now := f.clk.Now()
	f.led.RecordSession("claude", "s-task", now, 1, 0, 0)
	f.led.RecordSession("claude", "s-verify", now, 2, 0, 0)
	f.led.RecordSession("claude", "s-judge", now, 4, 0, 0)
	f.led.RecordSession("claude", "s-decomp", now, 8, 0, 0)
	f.led.RecordSession("claude", "s-unrelated", now, 100, 0, 0)
	f.led.Record("claude", now, 1000, 0, 0)

	ids := f.m.PRDSessionIDs(prd.ID)
	if len(ids) != 4 {
		t.Fatalf("session ids = %v, want 4 distinct", ids)
	}
	since := now.Add(-time.Hour)
	if got := f.m.PRDCostUSD(prd.ID, since, ""); got != 15 {
		t.Fatalf("PRD cost = %v, want 15", got)
	}
	if got := f.m.PRDCostUSD(prd.ID, since, "other"); got != 0 {
		t.Fatalf("PRD cost under another LLM = %v, want 0", got)
	}
	if got := f.m.PRDCostUSD("missing", since, ""); got != 0 {
		t.Fatalf("missing PRD cost = %v, want 0", got)
	}
}

func TestPRDCostUSDSinceExcludesEarlierSpend(t *testing.T) {
	f := newCostManager(t)
	prd := newCostPRD(t, f.m, "s-task", "", "")
	now := f.clk.Now()
	f.led.RecordSession("claude", "s-task", now.Add(-3*time.Hour), 5, 0, 0)
	f.led.RecordSession("claude", "s-task", now.Add(-time.Hour), 7, 0, 0)

	if got := f.m.PRDCostUSD(prd.ID, now.Add(-2*time.Hour), ""); got != 7 {
		t.Fatalf("cost since 2h ago = %v, want 7", got)
	}
	if got := f.m.PRDCostUSD(prd.ID, now.Add(-4*time.Hour), ""); got != 12 {
		t.Fatalf("cost since 4h ago = %v, want 12", got)
	}
}

func TestAutomationSpendSumsMemberCostRecords(t *testing.T) {
	f := newCostManager(t)
	a := newCostPRD(t, f.m, "s-a", "", "")
	b := newCostPRD(t, f.m, "s-b", "", "")
	c := newCostPRD(t, f.m, "s-c", "", "")
	_ = newCostPRD(t, f.m, "s-other", "", "") // not a member of any Automation
	now := f.clk.Now()
	for sid, usd := range map[string]float64{"s-a": 1, "s-b": 2, "s-c": 4, "s-other": 100} {
		f.led.RecordSession("claude", sid, now, usd, 0, 0)
	}

	child, err := f.m.CreateAutomation(AutomationInput{Name: "child", Members: []AutomationMember{{PRDID: c.ID}}}, "operator")
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent, err := f.m.CreateAutomation(AutomationInput{Name: "parent", Members: []AutomationMember{
		{PRDID: a.ID}, {PRDID: b.ID}, {Kind: MemberKindAutomation, AutomationID: child.ID},
	}}, "operator")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	since := now.Add(-time.Hour)
	if got := f.m.AutomationSpendUSD(parent, since, ""); got != 7 {
		t.Fatalf("automation spend = %v, want 7 (member PRDs and nested child, not the outside PRD)", got)
	}
	if got := f.m.AutomationSpendUSD(child, since, ""); got != 4 {
		t.Fatalf("child spend = %v, want 4", got)
	}
	if got := f.m.AutomationSpendUSD(nil, since, ""); got != 0 {
		t.Fatalf("nil automation spend = %v, want 0", got)
	}

	// A cycle must terminate: a copy of the parent that nests the parent itself.
	loop := *parent
	loop.Members = append(append([]AutomationMember(nil), parent.Members...), AutomationMember{Kind: MemberKindAutomation, AutomationID: parent.ID})
	if got := f.m.AutomationSpendUSD(&loop, since, ""); got != 7 {
		t.Fatalf("self-nesting automation spend = %v, want 7", got)
	}
}

func TestAutomationSpendLifetimeFallsBackToSessionTotals(t *testing.T) {
	f := newCostManager(t)
	a := newCostPRD(t, f.m, "s-a", "s-av", "")
	b := newCostPRD(t, f.m, "s-b", "", "")
	totals := map[string]struct {
		usd float64
		llm string
	}{
		"s-a": {3, "claude"}, "s-av": {1, "claude"}, "s-b": {10, "local"},
	}
	f.m.SetCostSources(f.led, func(id string) (float64, string, bool) {
		v, ok := totals[id]
		return v.usd, v.llm, ok
	})
	// The ledger holds only a slice of the lifetime; the fallback must not use it.
	f.led.RecordSession("claude", "s-a", f.clk.Now(), 0.5, 0, 0)

	auto, err := f.m.CreateAutomation(AutomationInput{Name: "life", Members: []AutomationMember{{PRDID: a.ID}, {PRDID: b.ID}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if got := f.m.AutomationSpendUSD(auto, time.Time{}, ""); got != 14 {
		t.Fatalf("lifetime spend (zero since) = %v, want 14", got)
	}
	if got := f.m.AutomationSpendUSD(auto, time.Time{}, "claude"); got != 4 {
		t.Fatalf("lifetime spend for claude = %v, want 4", got)
	}
	old := f.clk.Now().Add(-budgetRetention - time.Hour)
	if got := f.m.AutomationSpendUSD(auto, old, ""); got != 14 {
		t.Fatalf("lifetime spend (since older than retention) = %v, want 14", got)
	}
	if got := f.m.AutomationSpendUSD(auto, f.clk.Now().Add(-time.Hour), ""); got != 0.5 {
		t.Fatalf("windowed spend = %v, want ledger-only 0.5", got)
	}
}
