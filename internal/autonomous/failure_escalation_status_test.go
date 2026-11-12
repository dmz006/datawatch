package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// heldStatusPRD is an Automaton with one task held for review at heldAt.
func heldStatusPRD(id, title, taskID string, heldAt time.Time) *PRD {
	return &PRD{
		ID:     id,
		Title:  title,
		Status: PRDBlocked,
		Story: []Story{{
			ID: "s-" + id,
			Tasks: []Task{{
				ID: taskID, Title: "task " + taskID, Status: TaskReviewNeeded,
				Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", EscalationFailures: 3,
				Attempts: []TaskAttempt{
					{Number: 1, Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Outcome: "failed"},
					{Number: 2, Backend: "claude-code", Model: "claude-opus-5-5", Outcome: "failed", Escalated: true},
					{Number: 3, Backend: "claude-code", Model: "claude-opus-5-5", Outcome: "failed", Escalated: true},
				},
				ReviewSummary: &ReviewSummary{HeldAt: heldAt, FailureCount: 3, Text: "report"},
			}},
		}},
	}
}

// TestReviewNeededStatusListsHeldTasksAcrossAutomata: the daemon status lists
// every held task of every Automaton, with the Automaton identity, the task
// identity, the failure count, the worker of the last attempt and when the
// task was held. The oldest hold comes first.
func TestReviewNeededStatusListsHeldTasksAcrossAutomata(t *testing.T) {
	h := newCoordHarness(t, 3)
	newer := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	older := newer.Add(-2 * time.Hour)
	for _, p := range []*PRD{
		heldStatusPRD("pa-newer", "Newer Automaton", "t-new", newer),
		heldStatusPRD("pb-older", "Older Automaton", "t-old", older),
	} {
		if err := h.m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}

	rows := h.m.Status().ReviewNeeded
	if len(rows) != 2 {
		t.Fatalf("ReviewNeeded has %d rows, want 2: %+v", len(rows), rows)
	}
	first, second := rows[0], rows[1]
	if first.AutomatonID != "pb-older" || first.AutomatonTitle != "Older Automaton" || first.TaskID != "t-old" {
		t.Errorf("oldest hold first: got %+v", first)
	}
	if second.AutomatonID != "pa-newer" || second.TaskID != "t-new" {
		t.Errorf("second row: got %+v", second)
	}
	for _, r := range rows {
		if r.FailureCount != 3 || r.LastBackend != "claude-code" || r.LastModel != "claude-opus-5-5" {
			t.Errorf("row %s: failure count %d, last worker %s/%s", r.TaskID, r.FailureCount, r.LastBackend, r.LastModel)
		}
	}

	b, err := json.Marshal(h.m.Status())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"review_needed":[`, `"automaton_id":"pb-older"`, `"automaton_title":"Older Automaton"`, `"task_id":"t-old"`, `"held_at":"2026-10-24T07:00:00Z"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("status JSON lacks %s", want)
		}
	}
}

// TestReviewNeededStatusOmitsArrayWhenNothingHeld: with no held task the
// review_needed key is left out of the status.
func TestReviewNeededStatusOmitsArrayWhenNothingHeld(t *testing.T) {
	h := newCoordHarness(t, 3)
	if rows := h.m.Status().ReviewNeeded; len(rows) != 0 {
		t.Fatalf("no task is held, got %+v", rows)
	}
	b, err := json.Marshal(h.m.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"review_needed"`) {
		t.Errorf("status JSON carries review_needed with nothing held: %s", b)
	}
}

// TestFailureEscalationHeldTasksAcrossPRDsSkipsRunningTasks: only tasks in
// TaskReviewNeeded are listed; a task that is still running or failed is not a
// held task.
func TestFailureEscalationHeldTasksAcrossPRDsSkipsRunningTasks(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	held := heldStatusPRD("pa", "A", "t-held", now)
	running := &PRD{ID: "pb", Title: "B", Story: []Story{{ID: "s", Tasks: []Task{
		{ID: "t-run", Status: TaskInProgress},
		{ID: "t-fail", Status: TaskFailed},
	}}}}
	got := heldTasksAcrossPRDs(map[string]*PRD{"pa": held, "pb": running})
	if len(got) != 1 || got[0].TaskID != "t-held" {
		t.Fatalf("held tasks = %+v, want only t-held", got)
	}
	if heldTasksAcrossPRDs(map[string]*PRD{"pb": running}) != nil {
		t.Fatal("no held task must yield nil")
	}
}
