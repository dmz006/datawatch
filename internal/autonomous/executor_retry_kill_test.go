// Regression test for the retry-session-leak bug (found 2026-09-22 while
// debugging PRD 51c531b6 task b32e9409): when verification fails and the
// executor spawns a new session for the retry attempt, the previous
// attempt's session was never killed — it kept running as an orphaned tmux
// session (opencode/claude process still alive) alongside the new one,
// double-counting toward session.max_sessions and wasting compute. Fixed
// in executor.go's executeOne by killing t.SessionID via sessionKillerFn
// before overwriting it with the new spawn's SessionID.

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestExecutor_RetrySpawn_KillsStaleSessionBeforeRespawn(t *testing.T) {
	m, api, _, _ := apiFixture(t)

	var mu sync.Mutex
	var spawnedIDs []string
	var killedIDs []string
	spawnCount := 0

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		spawnCount++
		id := fmt.Sprintf("sess-%d", spawnCount)
		spawnedIDs = append(spawnedIDs, id)
		return SpawnResult{SessionID: id}, nil
	}
	verifyCount := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		verifyCount++
		if verifyCount == 1 {
			return VerificationResult{OK: false, Summary: "first attempt failed, forcing retry"}, nil
		}
		return VerificationResult{OK: true}, nil
	}
	m.SetSessionKillerFn(func(sessionID string) error {
		mu.Lock()
		defer mu.Unlock()
		killedIDs = append(killedIDs, sessionID)
		return nil
	})

	prd, err := m.CreatePRD("test spec", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := m.Store().GetPRD(prd.ID)
		if !ok {
			t.Fatal("PRD disappeared")
		}
		if got.Status == PRDCompleted || got.Status == PRDFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted, got %q", got.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(spawnedIDs) != 2 {
		t.Fatalf("want 2 spawns (initial + 1 retry after failed verify), got %d: %v", len(spawnedIDs), spawnedIDs)
	}
	if len(killedIDs) != 1 || killedIDs[0] != spawnedIDs[0] {
		t.Fatalf("want first session %q killed before the retry spawned %q, got killed=%v",
			spawnedIDs[0], spawnedIDs[1], killedIDs)
	}
}

// TestResumeRunningPRDs_SkipsPRDWithLiveExecutor is a regression test for a
// flake first caught in v8.35.1's release CI (go test, no -race): boot-resume
// (resumeRunningPRDs) processed every PRDRunning PRD unconditionally,
// including one that's already being driven by a live executor — for
// example a caller's own Run() call racing with SetExecutors' background
// scan, or (in production) an operator's Run() landing while boot-resume is
// still working through a large backlog after a restart. Before this fix,
// resetInProgressTasksForResume would unconditionally kill/reset an
// in-progress task's session, even though it was never actually orphaned —
// runCancels (the same liveness signal API.Run() uses for its own
// idempotency) is now checked first, and boot-resume skips recovery
// entirely when an executor is already live for that PRD.
func TestResumeRunningPRDs_SkipsPRDWithLiveExecutor(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd := prdInRunningState(t, m)

	// Simulate a task mid-flight under a live executor: TaskInProgress with
	// a session that's genuinely still running.
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[0].Status = TaskInProgress
	prd.Story[0].Tasks[0].SessionID = "sess-live"
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	killed := false
	m.SetSessionKillerFn(func(string) error { killed = true; return nil })

	// Register runCancels[prd.ID] directly — mimics API.Run() already
	// having claimed this PRD, without needing a real spawn/verify walk.
	api.runMu.Lock()
	if api.runCancels == nil {
		api.runCancels = map[string]context.CancelFunc{}
	}
	api.runCancels[prd.ID] = func() {}
	api.runMu.Unlock()

	api.resumeRunningPRDs()

	if killed {
		t.Fatal("resumeRunningPRDs killed a session for a PRD with an already-live executor")
	}
	updated, _ := m.Store().GetPRD(prd.ID)
	got := updated.Story[0].Tasks[0]
	if got.Status != TaskInProgress || got.SessionID != "sess-live" {
		t.Fatalf("task was reset despite a live executor: status=%q sessionID=%q", got.Status, got.SessionID)
	}
}
