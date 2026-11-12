package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// heldViewPRD is an Automaton with one ordinary task and two tasks held for
// review: one with two recorded attempts, one with no attempts and a report.
func heldViewPRD(now time.Time) *PRD {
	return &PRD{
		ID:     "p-held",
		Title:  "held view",
		Status: PRDBlocked,
		Story: []Story{
			{
				ID: "s1",
				Tasks: []Task{
					{ID: "t-ok", Title: "ordinary", Status: TaskPending},
					{
						ID: "t-held", Title: "held after attempts", Status: TaskReviewNeeded,
						Backend: "claude-code", Model: "claude-opus-5-5",
						EscalationFailures: 3,
						Attempts: []TaskAttempt{
							{Number: 1, Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Outcome: "failed", DurationSeconds: 40},
							{Number: 2, Backend: "claude-code", Model: "claude-opus-5-5", Outcome: "failed", DurationSeconds: 60, Escalated: true},
						},
						ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 3, TotalSeconds: 100, Text: "held report"},
					},
				},
			},
			{
				ID: "s2",
				Tasks: []Task{
					{
						ID: "t-held-noattempt", Title: "held with no attempt", Status: TaskReviewNeeded,
						Backend: "pi-prompt", Model: "m1",
						ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 2, Text: "short report"},
					},
				},
			},
		},
	}
}

// TestReviewNeededSummaryListsHeldTasks: the Automaton summary lists every held
// task in story order with its failure count, the worker of its last attempt
// and when it was held, and the status count shows review_needed.
func TestReviewNeededSummaryListsHeldTasks(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	view := BuildSummaryView(heldViewPRD(now))

	if len(view.ReviewNeeded) != 2 {
		t.Fatalf("ReviewNeeded has %d entries, want 2: %+v", len(view.ReviewNeeded), view.ReviewNeeded)
	}
	first := view.ReviewNeeded[0]
	if first.TaskID != "t-held" || first.TaskTitle != "held after attempts" || first.StoryID != "s1" {
		t.Errorf("first entry identity = %+v", first)
	}
	if first.FailureCount != 3 {
		t.Errorf("first FailureCount = %d, want 3", first.FailureCount)
	}
	if first.LastBackend != "claude-code" || first.LastModel != "claude-opus-5-5" {
		t.Errorf("first last worker = %s/%s, want the last attempt's claude-code/claude-opus-5-5", first.LastBackend, first.LastModel)
	}
	if !first.HeldAt.Equal(now) {
		t.Errorf("first HeldAt = %v, want %v", first.HeldAt, now)
	}

	second := view.ReviewNeeded[1]
	if second.TaskID != "t-held-noattempt" || second.StoryID != "s2" {
		t.Errorf("second entry identity = %+v", second)
	}
	if second.FailureCount != 2 {
		t.Errorf("second FailureCount = %d, want the report's 2", second.FailureCount)
	}
	if second.LastBackend != "pi-prompt" || second.LastModel != "m1" {
		t.Errorf("second worker = %s/%s, want the task's own pi-prompt/m1 when no attempt is recorded", second.LastBackend, second.LastModel)
	}

	if got := view.TaskStatusCounts[string(TaskReviewNeeded)]; got != 2 {
		t.Errorf("TaskStatusCounts[review_needed] = %d, want 2", got)
	}
	if got := view.TaskStatusCounts[string(TaskPending)]; got != 1 {
		t.Errorf("TaskStatusCounts[pending] = %d, want 1", got)
	}
	if view.Stories[0].TaskStatusCounts[string(TaskReviewNeeded)] != 1 {
		t.Errorf("story s1 review_needed count = %d, want 1", view.Stories[0].TaskStatusCounts[string(TaskReviewNeeded)])
	}
}

// TestReviewNeededSummaryJSONCarriesFields: the JSON names match the contract.
func TestReviewNeededSummaryJSONCarriesFields(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	b, err := json.Marshal(BuildSummaryView(heldViewPRD(now)))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ReviewNeeded []map[string]json.RawMessage `json:"review_needed"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.ReviewNeeded) != 2 {
		t.Fatalf("review_needed JSON has %d entries", len(got.ReviewNeeded))
	}
	for _, k := range []string{"task_id", "task_title", "story_id", "failure_count", "last_backend", "last_model", "held_at"} {
		if _, ok := got.ReviewNeeded[0][k]; !ok {
			t.Errorf("review_needed entry lacks %q: %v", k, got.ReviewNeeded[0])
		}
	}
}

// TestReviewNeededAbsentWhenNothingHeld: with no held task the summary and the
// task listing carry none of the review fields, so the wire shape is unchanged.
func TestReviewNeededAbsentWhenNothingHeld(t *testing.T) {
	prd := &PRD{
		ID: "p-plain", Title: "plain", Status: PRDRunning,
		Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Title: "one", Status: TaskPending}}}},
	}
	summary, err := json.Marshal(BuildSummaryView(prd))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(summary), "review_needed\"") {
		t.Errorf("summary JSON has review_needed although nothing is held: %s", summary)
	}
	tasks, err := json.Marshal(BuildTasksView(prd))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"escalation_failures", "attempt_count", "review_summary"} {
		if strings.Contains(string(tasks), "\""+k+"\"") {
			t.Errorf("tasks JSON carries %s for a task with no escalation state: %s", k, tasks)
		}
	}
	if view := BuildSummaryView(prd); view.ReviewNeeded != nil {
		t.Errorf("ReviewNeeded = %+v, want nil", view.ReviewNeeded)
	}
}

// TestReviewNeededTasksViewCarriesFailureFields: the task listing shows the
// escalation counter, the attempt count and the report text of a held task.
func TestReviewNeededTasksViewCarriesFailureFields(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	view := BuildTasksView(heldViewPRD(now))
	var held TaskEntry
	for _, e := range view.Tasks {
		if e.ID == "t-held" {
			held = e
		}
	}
	if held.Status != TaskReviewNeeded {
		t.Fatalf("held entry status = %q", held.Status)
	}
	if held.EscalationFailures != 3 {
		t.Errorf("EscalationFailures = %d, want 3", held.EscalationFailures)
	}
	if held.AttemptCount != 2 {
		t.Errorf("AttemptCount = %d, want 2", held.AttemptCount)
	}
	if held.ReviewSummary != "held report" {
		t.Errorf("ReviewSummary = %q, want the report text", held.ReviewSummary)
	}
	if len(view.ReviewNeeded) != 2 {
		t.Errorf("TasksView summary lists %d held tasks, want 2", len(view.ReviewNeeded))
	}
}

// TestReviewNeededAutomationStatusShowsHeldTasks: the Automation status names
// the held tasks of its current member Automaton, and reports none once the
// task is no longer held.
func TestReviewNeededAutomationStatusShowsHeldTasks(t *testing.T) {
	h := newCoordHarness(t, 3)
	h.force(h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3))

	if s := h.m.Status().Automations[0]; s.ReviewNeededCount != 0 || s.ReviewNeededTaskIDs != nil {
		t.Fatalf("no task is held yet, got count=%d ids=%v", s.ReviewNeededCount, s.ReviewNeededTaskIDs)
	}

	prd, ok := h.m.store.GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("current member PRD missing")
	}
	h.m.store.WithLock(func() {
		prd.Story = append(prd.Story, Story{
			ID:    "s-held",
			Tasks: []Task{{ID: "t-held", Title: "held", Status: TaskReviewNeeded, EscalationFailures: 3}},
		})
	})
	if err := h.m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	s := h.m.Status().Automations[0]
	if s.ReviewNeededCount != 1 {
		t.Fatalf("ReviewNeededCount = %d, want 1", s.ReviewNeededCount)
	}
	if len(s.ReviewNeededTaskIDs) != 1 || s.ReviewNeededTaskIDs[0] != "t-held" {
		t.Fatalf("ReviewNeededTaskIDs = %v, want [t-held]", s.ReviewNeededTaskIDs)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"review_needed_count":1`) || !strings.Contains(string(b), `"review_needed_task_ids":["t-held"]`) {
		t.Errorf("status JSON lacks the held-task fields: %s", b)
	}
}

// TestFailureEscalationConfigSetValidation: the config-set path refuses an
// out-of-range failure escalation value before anything is merged, and a valid
// value round-trips through the live config.
func TestFailureEscalationConfigSetValidation(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := NewAPI(m)

	for name, raw := range map[string]string{
		"negative max_failures": `{"failure_escalation":{"max_failures":-1}}`,
		"bad effort in a step":  `{"failure_escalation":{"steps":[{"backend":"claude-code","effort":"bogus"}]}}`,
		"model without backend": `{"failure_escalation":{"steps":[{"model":"x"}]}}`,
		"too many steps":        `{"failure_escalation":{"steps":[{},{},{},{},{},{},{},{},{}]}}`,
	} {
		if err := api.SetConfig(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: SetConfig accepted %s", name, raw)
		}
	}
	if got := m.Config().FailureEscalation.MaxFailures; got != 3 {
		t.Fatalf("refused writes changed max_failures to %d", got)
	}

	if err := api.SetConfig(json.RawMessage(`{"failure_escalation":{"max_failures":4,"steps":[{"backend":"claude-code","model":"claude-opus-5-5","attempts":2}]}}`)); err != nil {
		t.Fatalf("valid SetConfig: %v", err)
	}
	cfg := m.Config().FailureEscalation
	if cfg.MaxFailures != 4 || len(cfg.Steps) != 1 || cfg.Steps[0].Attempts != 2 {
		t.Fatalf("live config after valid write = %+v", cfg)
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		t.Fatalf("an omitted enabled key must keep the current value, got %v", cfg.Enabled)
	}
}

// TestFailureEscalationValidatorIsExported: the exported validator agrees with
// the internal one on a nil policy, a valid policy and an invalid one.
func TestFailureEscalationValidatorIsExported(t *testing.T) {
	if err := ValidateFailureEscalationPolicy(nil); err != nil {
		t.Fatalf("nil policy: %v", err)
	}
	if err := ValidateFailureEscalationPolicy(&FailureEscalationPolicy{MaxFailures: 2}); err != nil {
		t.Fatalf("valid policy: %v", err)
	}
	if err := ValidateFailureEscalationPolicy(&FailureEscalationPolicy{MaxFailures: -2}); err == nil {
		t.Fatal("negative max_failures accepted")
	}
}
