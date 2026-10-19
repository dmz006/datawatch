package autonomous

import (
	"strings"
	"testing"
)

// scopeLintTaskError is the Task.Error the executor writes when the pre-spawn
// scope lint rejects a task.
const scopeLintTaskError = "scope: spec line 1 references /home/other/elsewhere/tool outside the allowed directories; edit the task spec to remove or quote it (task not spawned)"

// setScopeTestState puts prd into prdStatus and, when taskID is set, marks that
// task failed with taskErr. It saves through the store so the edit path reads
// the same state a real run would leave behind.
func setScopeTestState(t *testing.T, m *Manager, prd *PRD, prdStatus PRDStatus, taskID, taskErr string) {
	t.Helper()
	prd.Status = prdStatus
	if taskID != "" {
		task := lookupTask(prd, taskID)
		task.Status = TaskFailed
		task.Error = taskErr
	}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
}

// TestEditTaskSpec_FailedAutomatonScopeFailedTaskAccepted: a failed automaton
// accepts a spec edit for the one task the pre-spawn scope lint rejected, and
// records an edit_task Decision.
func TestEditTaskSpec_FailedAutomatonScopeFailedTaskAccepted(t *testing.T) {
	m, _, prd, _, b, _ := blockedPRDFixture(t)
	setScopeTestState(t, m, prd, PRDFailed, b, scopeLintTaskError)

	const fixed = "write docs/x.md"
	if _, err := m.EditTaskSpec(prd.ID, b, fixed, "tester"); err != nil {
		t.Fatalf("EditTaskSpec on scope-failed task of failed PRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, b).Spec; s != fixed {
		t.Fatalf("new spec not persisted: %q", s)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.Kind != "edit_task" || last.Actor != "tester" || !strings.Contains(last.Note, b) {
		t.Fatalf("want edit_task decision for %s by tester, got %+v", b, last)
	}
	if got.Status != PRDFailed {
		t.Fatalf("edit must not change PRD status, got %s", got.Status)
	}
}

// TestEditTaskSpec_FailedAutomatonNonScopeFailureRefused: a failed automaton
// refuses a spec edit for a task that failed for a reason other than the scope
// lint.
func TestEditTaskSpec_FailedAutomatonNonScopeFailureRefused(t *testing.T) {
	m, _, prd, _, b, _ := blockedPRDFixture(t)
	setScopeTestState(t, m, prd, PRDFailed, b, "spawn failed: tmux server not running")
	before := lookupTask(prd, b).Spec

	_, err := m.EditTaskSpec(prd.ID, b, "write docs/x.md", "tester")
	if err == nil || !strings.Contains(err.Error(), "is locked") {
		t.Fatalf("want locked error for non-scope failure on failed PRD, got %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, b).Spec; s != before {
		t.Fatalf("spec changed despite refusal: %q", s)
	}
}

// TestEditTaskSpec_FailedAutomatonCompletedTaskRefused: a failed automaton keeps
// a completed task's spec immutable.
func TestEditTaskSpec_FailedAutomatonCompletedTaskRefused(t *testing.T) {
	m, _, prd, a, _, _ := blockedPRDFixture(t)
	setScopeTestState(t, m, prd, PRDFailed, "", "")

	_, err := m.EditTaskSpec(prd.ID, a, "rewrite history", "tester")
	if err == nil || !strings.Contains(err.Error(), "completed task specs are immutable") {
		t.Fatalf("want completed-task immutability error on failed PRD, got %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, a).Spec; s != "write docs/a.md" {
		t.Fatalf("completed task spec changed to %q", s)
	}
}

// TestEditTaskSpec_RunningAutomatonScopeFailedTaskRefused: a running automaton
// stays locked even for a task the scope lint rejected.
func TestEditTaskSpec_RunningAutomatonScopeFailedTaskRefused(t *testing.T) {
	m, _, prd, _, b, _ := blockedPRDFixture(t)
	setScopeTestState(t, m, prd, PRDRunning, b, scopeLintTaskError)

	_, err := m.EditTaskSpec(prd.ID, b, "write docs/x.md", "tester")
	if err == nil || !strings.Contains(err.Error(), "is locked") {
		t.Fatalf("want locked error on running PRD, got %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, b).Spec; s != outOfScopeSpec {
		t.Fatalf("spec changed on running PRD: %q", s)
	}
}
