// BL416 — presence-aware guardrail judge LLM setters at PRD + task level.
//
//   - SetPRDGuardrailLLM persists both fields + logs a decision
//   - nil pointer leaves the field unchanged; pointer to "" clears it
//   - PRD setter locked while running
//   - SetTaskGuardrailLLM persists, unknown task → not-found error,
//     locked outside needs_review / revisions_asked / cancelled
//   - SetTaskLLM afterwards leaves the guardrail fields intact

package autonomous

import (
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func newBL416Manager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", "/tmp/ts-bl416", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []Story{{ID: "s1", Title: "story", Tasks: []Task{{ID: "t1", Title: "task"}}}}
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func findBL416Task(t *testing.T, prd *PRD, id string) *Task {
	t.Helper()
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == id {
				return &prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %q not found", id)
	return nil
}

func TestBL416_SetPRDGuardrailLLM_PersistsAndLogsDecision(t *testing.T) {
	m, prd := newBL416Manager(t)
	if _, err := m.SetPRDGuardrailLLM(prd.ID, strp("claude-code"), strp("opus"), "test"); err != nil {
		t.Fatalf("SetPRDGuardrailLLM: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.GuardrailBackend != "claude-code" || stored.GuardrailModel != "opus" {
		t.Fatalf("stored guardrail = %q/%q; want claude-code/opus", stored.GuardrailBackend, stored.GuardrailModel)
	}
	var found bool
	for _, d := range stored.Decisions {
		if d.Kind == "set_prd_guardrail_llm" && strings.Contains(d.Note, "guardrail_backend=claude-code guardrail_model=opus") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing set_prd_guardrail_llm decision; decisions: %v", stored.Decisions)
	}
}

func TestBL416_SetPRDGuardrailLLM_NilLeavesUnchanged_EmptyClears(t *testing.T) {
	m, prd := newBL416Manager(t)
	if _, err := m.SetPRDGuardrailLLM(prd.ID, strp("claude-code"), strp("opus"), "test"); err != nil {
		t.Fatalf("SetPRDGuardrailLLM: %v", err)
	}
	// nil backend leaves it alone; model changes.
	updated, err := m.SetPRDGuardrailLLM(prd.ID, nil, strp("sonnet"), "test")
	if err != nil {
		t.Fatalf("SetPRDGuardrailLLM nil backend: %v", err)
	}
	if updated.GuardrailBackend != "claude-code" || updated.GuardrailModel != "sonnet" {
		t.Fatalf("after nil backend: %q/%q; want claude-code/sonnet", updated.GuardrailBackend, updated.GuardrailModel)
	}
	// pointer to "" clears; nil model leaves it alone.
	updated, err = m.SetPRDGuardrailLLM(prd.ID, strp(""), nil, "test")
	if err != nil {
		t.Fatalf("SetPRDGuardrailLLM clear backend: %v", err)
	}
	if updated.GuardrailBackend != "" || updated.GuardrailModel != "sonnet" {
		t.Fatalf("after clear backend: %q/%q; want \"\"/sonnet", updated.GuardrailBackend, updated.GuardrailModel)
	}
}

func TestBL416_SetPRDGuardrailLLM_LockedOnRunning(t *testing.T) {
	m, prd := newBL416Manager(t)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	if _, err := m.SetPRDGuardrailLLM(prd.ID, strp("claude-code"), nil, "test"); err == nil {
		t.Error("SetPRDGuardrailLLM on PRDRunning should return error")
	}
}

func TestBL416_SetTaskGuardrailLLM_Persists(t *testing.T) {
	m, prd := newBL416Manager(t)
	if _, err := m.SetTaskGuardrailLLM(prd.ID, "t1", strp("ollama"), strp("qwen3:8b"), "test"); err != nil {
		t.Fatalf("SetTaskGuardrailLLM: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	task := findBL416Task(t, stored, "t1")
	if task.GuardrailBackend != "ollama" || task.GuardrailModel != "qwen3:8b" {
		t.Fatalf("task guardrail = %q/%q; want ollama/qwen3:8b", task.GuardrailBackend, task.GuardrailModel)
	}
	var found bool
	for _, d := range stored.Decisions {
		if d.Kind == "set_task_guardrail_llm" && strings.Contains(d.Note, "guardrail_backend=ollama guardrail_model=qwen3:8b") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing set_task_guardrail_llm decision; decisions: %v", stored.Decisions)
	}
	// nil leaves unchanged, "" clears.
	updated, err := m.SetTaskGuardrailLLM(prd.ID, "t1", nil, strp(""), "test")
	if err != nil {
		t.Fatalf("SetTaskGuardrailLLM clear model: %v", err)
	}
	task = findBL416Task(t, updated, "t1")
	if task.GuardrailBackend != "ollama" || task.GuardrailModel != "" {
		t.Fatalf("after clear model: %q/%q; want ollama/\"\"", task.GuardrailBackend, task.GuardrailModel)
	}
}

func TestBL416_SetTaskGuardrailLLM_UnknownTask(t *testing.T) {
	m, prd := newBL416Manager(t)
	_, err := m.SetTaskGuardrailLLM(prd.ID, "nope", strp("ollama"), nil, "test")
	if err == nil || !strings.Contains(err.Error(), `task "nope" not found`) {
		t.Fatalf("want task-not-found error, got %v", err)
	}
}

func TestBL416_SetTaskGuardrailLLM_LockedOutsideReviewStates(t *testing.T) {
	for _, st := range []PRDStatus{PRDApproved, PRDRunning, PRDCompleted} {
		m, prd := newBL416Manager(t)
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.Status = st
		_ = m.Store().SavePRD(prd)
		if _, err := m.SetTaskGuardrailLLM(prd.ID, "t1", strp("ollama"), nil, "test"); err == nil {
			t.Errorf("status %q: SetTaskGuardrailLLM should be locked", st)
		}
	}
	for _, st := range []PRDStatus{PRDNeedsReview, PRDRevisionsAsked, PRDCancelled} {
		m, prd := newBL416Manager(t)
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.Status = st
		_ = m.Store().SavePRD(prd)
		if _, err := m.SetTaskGuardrailLLM(prd.ID, "t1", strp("ollama"), nil, "test"); err != nil {
			t.Errorf("status %q: SetTaskGuardrailLLM should be allowed: %v", st, err)
		}
	}
}

func TestBL416_SetTaskLLM_LeavesGuardrailIntact(t *testing.T) {
	m, prd := newBL416Manager(t)
	if _, err := m.SetTaskGuardrailLLM(prd.ID, "t1", strp("claude-code"), strp("opus"), "test"); err != nil {
		t.Fatalf("SetTaskGuardrailLLM: %v", err)
	}
	if _, err := m.SetPRDGuardrailLLM(prd.ID, strp("ollama"), strp("qwen3:8b"), "test"); err != nil {
		t.Fatalf("SetPRDGuardrailLLM: %v", err)
	}
	if _, err := m.SetTaskLLM(prd.ID, "t1", "", "", "", "test"); err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if _, err := m.SetPRDLLM(prd.ID, "", "", "", "", "", "test"); err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	task := findBL416Task(t, stored, "t1")
	if task.GuardrailBackend != "claude-code" || task.GuardrailModel != "opus" {
		t.Errorf("task guardrail wiped by SetTaskLLM: %q/%q", task.GuardrailBackend, task.GuardrailModel)
	}
	if stored.GuardrailBackend != "ollama" || stored.GuardrailModel != "qwen3:8b" {
		t.Errorf("PRD guardrail wiped by SetPRDLLM: %q/%q", stored.GuardrailBackend, stored.GuardrailModel)
	}
}
