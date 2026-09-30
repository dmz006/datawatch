package autonomous

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// runToTerminalOrBlocked is runToTerminal (scope_test.go) plus PRDBlocked as
// an accepted terminal state, since the halt-on-story-failure feature under
// test intentionally stops a PRD there instead of PRDCompleted/PRDFailed.
func runToTerminalOrBlocked(t *testing.T, m *Manager, api *API, id string, spawn SpawnFn, verify VerifyFn) *PRD {
	t.Helper()
	prd, _ := m.Store().GetPRD(id)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	api.SetExecutors(spawn, verify)
	if err := api.Run(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := m.Store().GetPRD(id)
		if got.Status == PRDCompleted || got.Status == PRDFailed || got.Status == PRDBlocked {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("PRD did not reach a terminal state")
	return nil
}

// TestExecutor_StoryFailureHaltsPRDByDefault verifies the operator's core
// complaint is fixed: a failed story used to let the executor barrel ahead
// into later, independent stories unattended. By default it must now halt
// the PRD (status -> PRDBlocked) the moment the failing story rolls up,
// leaving the later story untouched (pending, never spawned) for the
// operator to review/re-edit/rerun.
func TestExecutor_StoryFailureHaltsPRDByDefault(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "S1-fails", Tasks: []Task{{Title: "T1", Spec: "write docs/x.md"}}},
		{Title: "S2-independent", Tasks: []Task{{Title: "T2", Spec: "write docs/y.md"}}},
	})

	var spawnedTitles []string
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawnedTitles = append(spawnedTitles, r.TaskID)
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		if task.Title == "T1" {
			return VerificationResult{}, fmt.Errorf("simulated failure")
		}
		return VerificationResult{OK: true}, nil
	}

	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDBlocked {
		t.Fatalf("want PRDBlocked after story 1 fails, got %s", got.Status)
	}
	if got.Story[0].Status != StoryFailed {
		t.Fatalf("story 1 should be failed, got %s", got.Story[0].Status)
	}
	if got.Story[1].Status != "" && got.Story[1].Status != StoryPending {
		t.Fatalf("story 2 must be left untouched (pending), got %s", got.Story[1].Status)
	}
	if len(got.Story[1].Tasks) != 1 || got.Story[1].Tasks[0].Status != "" {
		t.Fatalf("story 2's task must never have been touched, got %+v", got.Story[1].Tasks)
	}
	for _, id := range spawnedTitles {
		if id == got.Story[1].Tasks[0].ID {
			t.Fatal("story 2's task must never have been spawned once story 1 failed")
		}
	}
}

// TestExecutor_CancelledStoryDoesNotFailPRDRollup is a regression test for a
// bug found live on PRD a2833a5e (v8.36.3): isTaskTerminal treats
// TaskCancelled as terminal (correctly), but the PRD rollup at the end of
// Run() then also counted it as a *failure* — a PRD with one deliberately
// cancelled story and every other story genuinely completed still rolled up
// to PRDFailed instead of PRDCompleted. Cancellation is an operator choice,
// not a failure.
func TestExecutor_CancelledStoryDoesNotFailPRDRollup(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "S1-completes", Tasks: []Task{{Title: "T1", Spec: "write docs/x.md"}}},
		{Title: "S2-was-cancelled", Tasks: []Task{{Title: "T2", Spec: "write docs/y.md"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[1].Status = StoryCancelled
	prd.Story[1].Tasks[0].Status = TaskCancelled
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted (only a pre-cancelled story + a genuinely completed story), got %s", got.Status)
	}
}

// TestExecutor_CancelledDependencyFailsDependent is a regression test for a
// bug found live on PRD a2833a5e (v8.36.5): a task depending on an
// already-cancelled task (an earlier story the operator cancelled and never
// restarted) proceeded as if that dependency were satisfied — only
// TaskFailed populated failedIDs in the sequential path's terminal-task
// skip, not TaskCancelled. "Write final recommendations" completed
// depending on two files a cancelled story never produced. A cancelled
// dependency was never fulfilled either; it must propagate the same as a
// failed one, in both the sequential and concurrent executor paths.
func TestExecutor_CancelledDependencyFailsDependent(t *testing.T) {
	for _, concurrency := range []int{0, 2} { // 0 = sequential path, 2 = concurrent path
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			m, api, _, _ := apiFixture(t)
			prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
			_ = m.Store().SetStories(prd.ID, []Story{
				{Title: "S1-cancelled", Tasks: []Task{{Title: "T1", Spec: "write docs/x.md"}}},
				{Title: "S2-depends-on-s1", Tasks: []Task{{Title: "T2", Spec: "write docs/y.md", DependsOn: []string{"T1"}}}},
			})
			prd, _ = m.Store().GetPRD(prd.ID)
			prd.Story[0].Status = StoryCancelled
			prd.Story[0].Tasks[0].Status = TaskCancelled
			if concurrency > 0 {
				prd.MaxConcurrentTasks = concurrency
			}
			_ = m.Store().SavePRD(prd)

			spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
				return SpawnResult{SessionID: "s-" + r.TaskID}, nil
			}
			verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
				return VerificationResult{OK: true}, nil
			}

			got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
			t2 := got.Story[1].Tasks[0]
			if t2.Status != TaskFailed {
				t.Fatalf("T2 (depends on cancelled T1) status = %q, want TaskFailed — it must not silently proceed as if T1 were satisfied", t2.Status)
			}
			if t2.Error == "" {
				t.Error("T2 should have an error explaining the unmet dependency")
			}
		})
	}
}

// TestExecutor_ContinueOnStoryFailure_PRDOverride restores the pre-fix
// "continue regardless" behavior when the operator explicitly opts in via
// the per-PRD override, confirming the opt-out path still works.
func TestExecutor_ContinueOnStoryFailure_PRDOverride(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "S1-fails", Tasks: []Task{{Title: "T1", Spec: "write docs/x.md"}}},
		{Title: "S2-independent", Tasks: []Task{{Title: "T2", Spec: "write docs/y.md"}}},
	})
	if _, err := m.SetPRDContinueOnStoryFailure(prd.ID, true); err != nil {
		t.Fatalf("SetPRDContinueOnStoryFailure: %v", err)
	}

	var spawnedIDs []string
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawnedIDs = append(spawnedIDs, r.TaskID)
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		if task.Title == "T1" {
			return VerificationResult{}, fmt.Errorf("simulated failure")
		}
		return VerificationResult{OK: true}, nil
	}

	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDFailed {
		t.Fatalf("want PRDFailed (story 2 still ran to completion despite story 1 failing), got %s", got.Status)
	}
	if got.Story[0].Status != StoryFailed {
		t.Fatalf("story 1 should be failed, got %s", got.Story[0].Status)
	}
	if got.Story[1].Status != StoryCompleted {
		t.Fatalf("story 2 should have completed under continue-on-story-failure, got %s", got.Story[1].Status)
	}
	if len(spawnedIDs) != 2 {
		t.Fatalf("both tasks should have spawned, got %v", spawnedIDs)
	}
}

// TestResolveContinueOnStoryFailure_PRDOverridesGlobalDefault covers the
// resolution order directly: per-PRD override wins over the daemon-wide
// default in both directions.
func TestResolveContinueOnStoryFailure_PRDOverridesGlobalDefault(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	got, _ := m.Store().GetPRD(prd.ID)

	if m.resolveContinueOnStoryFailure(got) != false {
		t.Fatal("default (no global config, no PRD override) must be false (halt)")
	}

	m.mu.Lock()
	m.cfg.ContinueOnStoryFailure = true
	m.mu.Unlock()
	if m.resolveContinueOnStoryFailure(got) != true {
		t.Fatal("global default should take effect when PRD has no override")
	}

	f := false
	got.ContinueOnStoryFailure = &f
	if m.resolveContinueOnStoryFailure(got) != false {
		t.Fatal("explicit per-PRD override=false must win over a global default=true")
	}
}
