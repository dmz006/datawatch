package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestDescribeAttemptBackendReason covers each reason text, with a fake for
// every executor source the attempt worker can come from.
func TestDescribeAttemptBackendReason(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		kind     string
		failures int
		want     string
	}{
		{"first attempt note task", ExecutorSourceFirstAttempt, attemptKindNote, 0, "first attempt for note tasks"},
		{"first attempt implementation task", ExecutorSourceFirstAttempt, attemptKindImplementation, 0, "first attempt for implementation tasks"},
		{"escalated after one failure", ExecutorSourceEscalation, attemptKindNote, 1, "escalated after 1 failure"},
		{"escalated after three failures", ExecutorSourceEscalation, attemptKindNote, 3, "escalated after 3 failures"},
		{"escalation step before any failure", ExecutorSourceEscalation, attemptKindNote, 0, "escalation ladder step"},
		{"task pin", ExecutorSourceTask, attemptKindNote, 0, "operator pinned"},
		{"story pin", ExecutorSourceStory, attemptKindNote, 2, "operator pinned"},
		{"PRD pin", ExecutorSourcePRD, attemptKindNote, 0, "operator pinned"},
		{"Automaton pin", ExecutorSourceAutomation, attemptKindNote, 0, "operator pinned"},
		{"global default", ExecutorSourceGlobal, attemptKindNote, 0, "default choice"},
		{"burn guard route", ExecutorSourceBurnGuard, attemptKindNote, 0, "default choice"},
		{"no source", "", attemptKindNote, 0, "default choice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeAttemptBackendReason(tc.source, tc.kind, tc.failures); got != tc.want {
				t.Errorf("describeAttemptBackendReason(%q, %q, %d) = %q, want %q",
					tc.source, tc.kind, tc.failures, got, tc.want)
			}
		})
	}
}

// TestRecordFailedAttemptStoresBackendReason runs the attempt recorder on a
// real store and checks the reason is carried onto the stored record, both
// when the attempt carries one and when it must derive one from the task.
func TestRecordFailedAttemptStoresBackendReason(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	fail := attemptFailure{Outcome: attemptOutcomeFailed, Category: "verifier", Summary: "not done"}

	// The reason the caller set is written as given.
	task := &prd.Story[0].Tasks[0]
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{
		Backend: "pi-prompt", Model: "qwen3.8:27b", BackendReason: "escalated after 1 failure",
	}, fail)

	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after recording")
	}
	got := stored.Story[0].Tasks[0].Attempts
	if len(got) != 1 {
		t.Fatalf("Attempts = %d, want 1", len(got))
	}
	if got[0].BackendReason != "escalated after 1 failure" {
		t.Errorf("BackendReason = %q, want the caller's reason", got[0].BackendReason)
	}
	if got[0].Backend != "pi-prompt" || got[0].Model != "qwen3.8:27b" {
		t.Errorf("worker = %s/%s, want pi-prompt/qwen3.8:27b", got[0].Backend, got[0].Model)
	}

	// With no worker on the run, the reason comes from the task's own resolution
	// and is one of the known texts.
	task = &stored.Story[0].Tasks[0]
	m.recordFailedAttempt(context.Background(), stored, task, attemptRun{}, fail)
	stored, _ = m.Store().GetPRD(prd.ID)
	got = stored.Story[0].Tasks[0].Attempts
	if len(got) != 2 {
		t.Fatalf("Attempts = %d, want 2", len(got))
	}
	switch got[1].BackendReason {
	case "operator pinned", "default choice":
	default:
		t.Errorf("derived BackendReason = %q, want operator pinned or default choice", got[1].BackendReason)
	}
}

// TestAttemptBackendReasonJSONRoundTrip: the record writes backend_reason and
// reads it back unchanged.
func TestAttemptBackendReasonJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	in := TaskAttempt{
		Number: 2, Backend: "claude-code", Model: "claude-opus-5-5",
		StartedAt: start, Outcome: attemptOutcomeFailed,
		Escalated: true, BackendReason: "escalated after 2 failures",
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"backend_reason":"escalated after 2 failures"`) {
		t.Errorf("JSON lacks backend_reason: %s", raw)
	}
	var out TaskAttempt
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.BackendReason != in.BackendReason || out.Backend != in.Backend || out.Model != in.Model {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}

	// An empty reason is omitted from the JSON.
	empty, _ := json.Marshal(TaskAttempt{Number: 1, Outcome: attemptOutcomeFailed})
	if strings.Contains(string(empty), "backend_reason") {
		t.Errorf("empty reason should be omitted: %s", empty)
	}
}

// TestOldAttemptRecordLoadsWithoutBackendReason: a record stored before the
// field existed loads with the reason empty and every other field intact.
func TestOldAttemptRecordLoadsWithoutBackendReason(t *testing.T) {
	old := []byte(`{"number":1,"backend":"pi-prompt","model":"qwen3.8:27b","outcome":"failed",` +
		`"failure_category":"verifier","failure_summary":"tests failed","escalated":false,` +
		`"end_reason":"verifier_failed"}`)
	var a TaskAttempt
	if err := json.Unmarshal(old, &a); err != nil {
		t.Fatalf("Unmarshal old record: %v", err)
	}
	if a.BackendReason != "" {
		t.Errorf("BackendReason = %q, want empty for an old record", a.BackendReason)
	}
	if a.Backend != "pi-prompt" || a.Model != "qwen3.8:27b" || a.FailureSummary != "tests failed" || a.EndReason != "verifier_failed" {
		t.Errorf("old record fields lost: %+v", a)
	}
}

// TestHeldEntryCarriesLastBackendReason: the Automaton summary row for a held
// task shows the reason of its last attempt, and an old attempt shows none.
func TestHeldEntryCarriesLastBackendReason(t *testing.T) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	prd := &PRD{
		ID: "p-reason", Title: "reason view", Status: PRDBlocked,
		Story: []Story{{ID: "s1", Tasks: []Task{
			{
				ID: "t-new", Title: "new record", Status: TaskReviewNeeded,
				ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 1},
				Attempts: []TaskAttempt{
					{Number: 1, Backend: "claude-code", Model: "claude-opus-5-5", Outcome: "failed", BackendReason: "escalated after 1 failure"},
				},
			},
			{
				ID: "t-old", Title: "old record", Status: TaskReviewNeeded,
				ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 1},
				Attempts: []TaskAttempt{
					{Number: 1, Backend: "pi-prompt", Model: "m", Outcome: "failed"},
				},
			},
		}}},
	}
	view := BuildSummaryView(prd)
	if len(view.ReviewNeeded) != 2 {
		t.Fatalf("ReviewNeeded = %d entries, want 2", len(view.ReviewNeeded))
	}
	if got := view.ReviewNeeded[0].LastBackendReason; got != "escalated after 1 failure" {
		t.Errorf("new record LastBackendReason = %q", got)
	}
	if got := view.ReviewNeeded[1].LastBackendReason; got != "" {
		t.Errorf("old record LastBackendReason = %q, want empty", got)
	}
	raw, err := json.Marshal(view.ReviewNeeded[0])
	if err != nil {
		t.Fatalf("Marshal entry: %v", err)
	}
	if !strings.Contains(string(raw), `"last_backend_reason":"escalated after 1 failure"`) {
		t.Errorf("entry JSON lacks last_backend_reason: %s", raw)
	}
}
