package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// gateRestartFake swaps in a gate runner that returns results in order and
// counts the calls.
func gateRestartFake(t *testing.T, results ...GateResult) *int {
	t.Helper()
	old := runGateCheckFn
	calls := 0
	runGateCheckFn = func(_ context.Context, _ GateCheck, _ string, _ time.Duration) GateResult {
		r := results[len(results)-1]
		if calls < len(results) {
			r = results[calls]
		}
		calls++
		return r
	}
	t.Cleanup(func() { runGateCheckFn = old })
	return &calls
}

var gateKilled = GateResult{ExitCode: -1, Reason: gateSignalKilledReason, Output: "killed\n"}

func gateRestartSetup(t *testing.T, sinceStart time.Duration) (*Manager, *PRD, *Task) {
	m, prd, task, _ := scriptGateFixture(t, ".datawatch/verify/gate.sh", "exit 0\n", 30)
	base := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	m.startedAt = base
	m.genArt.now = func() time.Time { return base.Add(sinceStart) }
	return m, prd, task
}

func TestGateRestartRetryRecentRestartRetriedOnce(t *testing.T) {
	calls := gateRestartFake(t, gateKilled, GateResult{Passed: true})
	m, prd, task := gateRestartSetup(t, time.Minute)
	prdCeilingBefore := prd.RetryCeilingTotal
	if err := runScriptGate(t, m, prd, task); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("gate ran %d times, want 2", *calls)
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskCompleted || !got.GateRestartRetryUsed {
		t.Fatalf("status=%q used=%v", got.Status, got.GateRestartRetryUsed)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if got.RetryCount != 0 || got.InfraRetryCount != 0 || got.PriorAttemptCount != 0 || len(got.Attempts) != 0 ||
		len(p.RetryCountsByCategory) != 0 || p.RetryCeilingTotal != prdCeilingBefore {
		t.Fatalf("budget counters changed: %+v / %+v", got, p.RetryCountsByCategory)
	}
	found := false
	for _, d := range p.Decisions {
		if d.Kind == gateRestartRetryDecisionKind && strings.Contains(d.Note, "daemon restarted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no restart retry decision line: %+v", p.Decisions)
	}
}

func TestGateRestartRetryLongAfterRestartIsNormalFailure(t *testing.T) {
	calls := gateRestartFake(t, gateKilled, GateResult{Passed: true})
	m, prd, task := gateRestartSetup(t, gateRestartRecentWindow+time.Minute)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("want failure")
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if *calls != 1 || got.Status != TaskFailed || got.GateRestartRetryUsed {
		t.Fatalf("calls=%d status=%q used=%v", *calls, got.Status, got.GateRestartRetryUsed)
	}
}

func TestGateRestartRetrySecondKillIsNormalFailure(t *testing.T) {
	calls := gateRestartFake(t, gateKilled, gateKilled)
	m, prd, task := gateRestartSetup(t, time.Minute)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("want failure")
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if *calls != 2 || got.Status != TaskFailed || !got.GateRestartRetryUsed {
		t.Fatalf("calls=%d status=%q used=%v", *calls, got.Status, got.GateRestartRetryUsed)
	}
	// A later kill on a task that already used its retry is not retried again.
	calls2 := gateRestartFake(t, gateKilled)
	got.Status = TaskPending
	_ = runScriptGate(t, m, prd, &got)
	if *calls2 != 1 {
		t.Fatalf("second incident reran the gate %d times", *calls2)
	}
}

func TestGateRestartRetryNonSignalFailureNeverRetried(t *testing.T) {
	calls := gateRestartFake(t, GateResult{ExitCode: 3, Reason: "gate exited with code 3"})
	m, prd, task := gateRestartSetup(t, time.Minute)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("want failure")
	}
	if *calls != 1 {
		t.Fatalf("gate ran %d times", *calls)
	}
}
