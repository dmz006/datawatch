// Operator cancel-with-reason reopens a blocked Automaton so the next Run
// can roll it up: cancelled tasks count as terminal, a story whose only
// non-completed tasks are cancelled rolls up as completed (not failed), and
// a genuinely failed task still fails the story.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cancelReopenFixture builds a two-story PRD in a project dir that carries a
// rules file (so the PRD-completion rules-review judge has something to point
// at), with a guardrail model configured and a recording pass judge wired.
func cancelReopenFixture(t *testing.T) (*Manager, *PRD, *[]string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.GuardrailModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			ID: "story-1", Title: "Story One",
			Tasks: []Task{
				{ID: "task-1a", Title: "Task 1A"},
				{ID: "task-1b", Title: "Task 1B"},
			},
		},
		{
			ID: "story-2", Title: "Story Two",
			Tasks: []Task{
				{ID: "task-2a", Title: "Task 2A"},
				{ID: "task-2b", Title: "Task 2B"},
			},
		},
	})
	judged := &[]string{}
	m.SetGuardrail(func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		*judged = append(*judged, inv.Level+"/"+inv.Guardrail)
		return GuardrailVerdict{Outcome: "pass", Summary: "clean"}, nil
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDBlocked
	prd.BlockedReason = "task task-1a needs operator review"
	// story-1: task-1a held for review, task-1b done; story-2: all done.
	prd.Story[0].Status = StoryInProgress
	prd.Story[0].Tasks[0].Status = TaskReviewNeeded
	prd.Story[0].Tasks[1].Status = TaskCompleted
	prd.Story[1].Status = StoryCompleted
	prd.Story[1].Tasks[0].Status = TaskCompleted
	prd.Story[1].Tasks[1].Status = TaskCompleted
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, judged
}

func cancelReopenDecision(m *Manager, prdID, kind string) *Decision {
	p, _ := m.Store().GetPRD(prdID)
	for i := range p.Decisions {
		if p.Decisions[i].Kind == kind {
			d := p.Decisions[i]
			return &d
		}
	}
	return nil
}

// A blocked Automaton whose last outstanding task is cancelled with a reason
// is reopened (running, blocked reason cleared, review decision recorded),
// and the next Run takes it all the way to the completion review and
// completes it: a story whose only non-completed task was cancelled is not a
// failure.
func TestCancelReopens_BlockedPRD_CompletionReview(t *testing.T) {
	m, prd, judged := cancelReopenFixture(t)

	if _, err := m.CancelTask(prd.ID, "task-1a", "operator", "not worth another try"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskCancelled {
		t.Fatalf("task-1a status = %q, want cancelled", st)
	}
	if got.Status != PRDRunning {
		t.Fatalf("Automaton status = %q, want running after a reasoned cancel closed out the work", got.Status)
	}
	if got.BlockedReason != "" {
		t.Fatalf("blocked reason = %q, want cleared", got.BlockedReason)
	}
	d := cancelReopenDecision(m, prd.ID, reviewDecisionKind)
	if d == nil {
		t.Fatal("no review_resolved decision after the reasoned cancel")
	}
	if !strings.Contains(d.Note, "task-1a") {
		t.Fatalf("review decision note %q does not name the cancelled task", d.Note)
	}

	// The caller's Run rolls the Automaton up and reaches the completion
	// review (rules-review judged at the PRD level).
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	done, _ := m.Store().GetPRD(prd.ID)
	if done.Status != PRDCompleted {
		t.Fatalf("Automaton status = %q, want completed", done.Status)
	}
	foundPRDReview := false
	for _, j := range *judged {
		if strings.Contains(j, "rules-review") {
			foundPRDReview = true
		}
	}
	if !foundPRDReview {
		t.Fatalf("completion rules-review never ran; guardrail calls = %v", *judged)
	}
}

// A cancel without a reason stops the task but does not close out the work:
// a blocked Automaton keeps its hold and no review decision is recorded.
func TestCancelReopens_WithoutReason_StaysBlocked(t *testing.T) {
	m, prd, _ := cancelReopenFixture(t)
	decisionCount := len(prd.Decisions)

	if _, err := m.CancelTask(prd.ID, "task-1a", "operator", ""); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskCancelled {
		t.Fatalf("task-1a status = %q, want cancelled", st)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("Automaton status = %q, want still blocked for a cancel without a reason", got.Status)
	}
	if got.BlockedReason == "" {
		t.Fatal("blocked reason was cleared, want unchanged")
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if len(p.Decisions) < decisionCount+1 {
		t.Fatal("the cancel_task decision was not recorded")
	}
	if cancelReopenDecision(m, prd.ID, reviewDecisionKind) != nil {
		t.Fatal("a review_resolved decision was recorded for a cancel without a reason")
	}
}

// Cancelling with a reason while another task is still in flight is not a
// close-out: the Automaton stays blocked until the operator resolves the
// remaining work.
func TestCancelReopens_OtherTaskStillPending_StaysBlocked(t *testing.T) {
	m, prd, _ := cancelReopenFixture(t)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[1].Status = TaskInProgress // task-1b still running
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	if _, err := m.CancelTask(prd.ID, "task-1a", "operator", "not worth another try"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskCancelled {
		t.Fatalf("task-1a status = %q, want cancelled", st)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("Automaton status = %q, want still blocked while task-1b is in flight", got.Status)
	}
	if got.BlockedReason == "" {
		t.Fatal("blocked reason was cleared, want unchanged")
	}
}

// A story whose non-completed tasks are all cancelled rolls up as completed,
// never as failed, and the Automaton reaches the completed state.
func TestCancelReopens_CancelledTaskDoesNotFailStory(t *testing.T) {
	m, prd, _ := cancelReopenFixture(t)

	if _, err := m.CancelTask(prd.ID, "task-1a", "operator", "not worth another try"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	done, _ := m.Store().GetPRD(prd.ID)
	if st := done.Story[0].Status; st == StoryFailed {
		t.Fatalf("story-1 status = %q, want not failed: its only non-completed task was cancelled", st)
	}
	if done.Status != PRDCompleted {
		t.Fatalf("Automaton status = %q, want completed", done.Status)
	}
	if done.Status == PRDFailed {
		t.Fatal("a cancelled task took the Automaton down as a failure")
	}
}

// A genuinely failed task is still a failure: the story fails and the
// Automaton rolls up as failed, even after the last held task is cancelled.
func TestCancelReopens_FailedTaskStillFailsStory(t *testing.T) {
	m, prd, _ := cancelReopenFixture(t)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[1].Status = TaskFailed
	prd.Story[0].Tasks[1].Error = "boom"
	prd.Story[0].Status = StoryFailed
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	if got := storyRollupStatus(&prd.Story[0]); got != StoryFailed {
		t.Fatalf("story-1 rollup = %q, want failed (task-1b failed)", got)
	}

	if _, err := m.CancelTask(prd.ID, "task-1a", "operator", "not worth another try"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDRunning {
		t.Fatalf("Automaton status = %q, want running after the close-out cancel", got.Status)
	}

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	done, _ := m.Store().GetPRD(prd.ID)
	if done.Status != PRDFailed {
		t.Fatalf("Automaton status = %q, want failed: a failed task is a failure", done.Status)
	}
}
