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
