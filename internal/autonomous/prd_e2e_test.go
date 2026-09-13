// PRD E2E tests — full lifecycle coverage for the autonomous PRD pipeline.
// Run with: go test ./internal/autonomous/ -run TestPRD_E2E -v
//
// These tests exercise the full create→decompose→approve→run→complete path so
// regressions (stall bugs, status-gate bypasses, retry logic) are caught before
// tagging a release.

package autonomous

import (
	"context"
	"errors"
	"testing"
)

// TestPRDE2E_FullLifecycle exercises the entire pipeline from spec to
// PRDCompleted using a fake LLM and synchronous mock spawn/verify.
func TestPRDE2E_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"Auth Feature","stories":[
			{"title":"Login","tasks":[
				{"title":"Add login endpoint","spec":"implement POST /login"},
				{"title":"Add logout endpoint","spec":"implement POST /logout"}
			]}
		]}`, nil
	}
	m, err := NewManager(dir, DefaultConfig(), fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("build login and logout", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.Status != PRDDraft {
		t.Fatalf("new PRD status = %q, want PRDDraft", prd.Status)
	}

	decomposed, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if decomposed.Status != PRDNeedsReview {
		t.Fatalf("post-decompose status = %q, want PRDNeedsReview", decomposed.Status)
	}
	if decomposed.Title != "Auth Feature" {
		t.Fatalf("title = %q, want Auth Feature", decomposed.Title)
	}
	if len(decomposed.Story) != 1 || len(decomposed.Story[0].Tasks) != 2 {
		t.Fatalf("unexpected story/task shape: %+v", decomposed.Story)
	}

	// Approve — skip the human gate.
	decomposed.Status = PRDApproved
	if err := m.Store().SavePRD(decomposed); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	var spawnedIDs []string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawnedIDs = append(spawnedIDs, req.TaskID)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "tests pass"}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	final, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("PRD not found after Run")
	}
	if final.Status != PRDCompleted {
		t.Fatalf("final PRD status = %q, want PRDCompleted", final.Status)
	}
	if len(spawnedIDs) != 2 {
		t.Fatalf("expected 2 spawns, got %d: %v", len(spawnedIDs), spawnedIDs)
	}
	for _, s := range final.Story {
		if s.Status != StoryCompleted {
			t.Errorf("story %q status = %q, want StoryCompleted", s.Title, s.Status)
		}
		for _, tk := range s.Tasks {
			if tk.Status != TaskCompleted {
				t.Errorf("task %q status = %q, want TaskCompleted", tk.Title, tk.Status)
			}
		}
	}
}

// TestPRDE2E_RunRejectsUnapprovedPRD verifies the approval gate: Run must
// return an error (or silently no-op) when the PRD has not reached PRDApproved.
func TestPRDE2E_RunRejectsUnapprovedPRD(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	// Status is PRDDraft — NOT approved.

	spawnCalled := false
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalled = true
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	err := m.Run(context.Background(), prd.ID, spawn, verify)
	if err == nil && spawnCalled {
		t.Fatalf("Run with un-approved PRD must not spawn tasks")
	}
}

// TestPRDE2E_SpawnErrorFailsTask verifies that a spawn-side error causes the
// task to land in TaskFailed and the PRD to not reach PRDCompleted.
// The executor continues to remaining tasks rather than aborting Run early, so
// Run itself returns nil — the caller inspects task/PRD status to detect failure.
func TestPRDE2E_SpawnErrorFailsTask(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{}, errors.New("compute node unreachable")
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status == PRDCompleted {
		t.Fatalf("PRD must NOT be completed when spawn failed")
	}
	for _, s := range final.Story {
		for _, tk := range s.Tasks {
			if tk.Status != TaskFailed {
				t.Errorf("task %q status = %q, want TaskFailed after spawn error", tk.Title, tk.Status)
			}
		}
	}
}

// TestPRDE2E_MultiStoryOrdering exercises two independent stories each with
// their own tasks — all should complete and the PRD should reach Completed.
func TestPRDE2E_MultiStoryOrdering(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("multi-story spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "Story A", Tasks: []Task{
			{Title: "A1", Spec: "do a1"},
			{Title: "A2", Spec: "do a2"},
		}},
		{Title: "Story B", Tasks: []Task{
			{Title: "B1", Spec: "do b1"},
		}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawnCount := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCount++
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCount != 3 {
		t.Fatalf("expected 3 spawns (A1, A2, B1), got %d", spawnCount)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDCompleted {
		t.Fatalf("final status = %q, want PRDCompleted", final.Status)
	}
	for _, s := range final.Story {
		if s.Status != StoryCompleted {
			t.Errorf("story %q not completed: %q", s.Title, s.Status)
		}
	}
}

// TestPRDE2E_RetryExhaustedFails verifies that when verify keeps returning
// NOT-OK after all retries are spent, the task lands in TaskFailed and the PRD
// does not reach PRDCompleted. Run itself returns nil (executor continues to
// other tasks rather than aborting early) — callers check task/PRD status.
func TestPRDE2E_RetryExhaustedFails(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1 // only one retry allowed
	m, _ := NewManager(dir, cfg, nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawnCount := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCount++
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "still broken"}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// With AutoFixRetries=1, executor spawns: initial + 1 retry = 2 attempts.
	if spawnCount != 2 {
		t.Fatalf("expected 2 spawn attempts (initial + 1 retry), got %d", spawnCount)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status == PRDCompleted {
		t.Fatalf("PRD must NOT be completed when retries exhausted")
	}
	for _, s := range final.Story {
		for _, tk := range s.Tasks {
			if tk.Status != TaskFailed {
				t.Errorf("task %q status = %q, want TaskFailed after retry exhaustion", tk.Title, tk.Status)
			}
		}
	}
}

// TestPRDE2E_DependencyOrderRespected places two tasks in a single story where
// task 2 depends on task 1 and verifies they ran in order.
func TestPRDE2E_DependencyOrderRespected(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "setup", Spec: "setup env"},
			{Title: "build", Spec: "build app"},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	// Wire dependency: build depends on setup.
	prd.Story[0].Tasks[1].DependsOn = []string{prd.Story[0].Tasks[0].ID}
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var order []string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		order = append(order, req.Title)
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 2 || order[0] != "setup" || order[1] != "build" {
		t.Fatalf("task order wrong: %v", order)
	}
}
