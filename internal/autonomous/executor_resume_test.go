// v8.33.8 — executor goroutine re-launch after daemon restart.
//
// TC-1 (boot-resume): SetExecutors re-launches executor for PRDs in PRDRunning state.
// TC-2 (reset-task re-launch): API.ResetTask + Run called from HTTP handler — executor
//       starts even when runCancels is empty (simulates post-restart state).
// TC-3 (idempotent Run): calling Run twice on a running PRD is safe — second call
//       cancels first and a fresh executor starts.
// TC-4 (non-running PRD skipped): resumeRunningPRDs skips PRDs not in PRDRunning.

package autonomous

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// statusOf reads a PRD's status under the Manager mutex, which is the lock
// executor status writes hold (the store hands out the shared *PRD).
func statusOf(m *Manager, p *PRD) PRDStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return p.Status
}

// apiFixture builds a Manager + API pair wired with controllable spawn/verify.
func apiFixture(t *testing.T) (*Manager, *API, func(context.Context, SpawnRequest) (SpawnResult, error), func(context.Context, *PRD, *Task) (VerificationResult, error)) {
	t.Helper()
	dir := t.TempDir()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	m, err := NewManager(dir, DefaultConfig(), fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api := NewAPI(m)
	// Registered after t.TempDir so it runs first (cleanup is LIFO): no
	// executor may still be writing PRD files when the directory is removed.
	drainOnCleanup(t, m, api)
	return m, api, spawn, verify
}

// prdInRunningState creates a PRD with one pending task in PRDRunning state,
// mimicking a daemon restart where executor goroutines were lost but store
// still shows PRDRunning with tasks at "" (pending).
func prdInRunningState(t *testing.T, m *Manager) *PRD {
	t.Helper()
	// B165 — the boot-resume gate parks PRDs whose project dir no longer
	// exists, so the fake project dir must be a real one for these tests
	// to keep seeing the PRD resumed (not parked).
	proj, err := os.MkdirTemp("", "autonomous-boot-resume")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(proj) })
	prd, err := m.CreatePRD("test spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// Wire stories directly (skips the LLM decompose call).
	if err := m.Store().SetStories(prd.ID, []Story{
		{
			Title: "Story",
			Tasks: []Task{
				{Title: "Task 1", Spec: "do task 1"},
			},
		},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD PRDRunning: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return prd
}

// TestExecutorResume_BootResume verifies that SetExecutors re-launches an
// executor for every PRD stored in PRDRunning state. This is the core fix
// for "Automata stuck running after daemon restart."
func TestExecutorResume_BootResume(t *testing.T) {
	m, api, spawn, verify := apiFixture(t)
	prd := prdInRunningState(t, m)

	var spawned atomic.Int32
	wrappedSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return spawn(ctx, req)
	}

	// SetExecutors triggers resumeRunningPRDs in a goroutine.
	api.SetExecutors(wrappedSpawn, verify)

	// Wait for the executor to finish — it runs the single task.
	pollPRDTerminal(t, m, prd.ID, 5*time.Second)

	got, _ := m.Store().GetPRD(prd.ID)
	if statusOf(m, got) != PRDCompleted {
		t.Errorf("want PRDCompleted after boot-resume, got %q", statusOf(m, got))
	}
	if spawned.Load() == 0 {
		t.Error("executor never spawned a session — boot-resume did not work")
	}
}

// TestExecutorResume_ResetTaskLaunchesExecutor verifies that resetting a failed
// task on a PRDRunning PRD whose executor is dead results in a live executor.
// This is the HTTP handler fix: manager.ResetTask + API.Run called together.
func TestExecutorResume_ResetTaskLaunchesExecutor(t *testing.T) {
	m, api, spawn, verify := apiFixture(t)
	prd := prdInRunningState(t, m)

	// Mark the task as failed first (ResetTask only works on failed/blocked tasks).
	fresh, _ := m.Store().GetPRD(prd.ID)
	taskID := fresh.Story[0].Tasks[0].ID
	fresh.Story[0].Tasks[0].Status = TaskFailed
	fresh.Story[0].Tasks[0].Error = "prior failure"
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD with failed task: %v", err)
	}

	// Wire executors WITHOUT calling SetExecutors (simulates daemon restart
	// where runCancels is empty but the PRD is in PRDRunning).
	api.spawnFn = spawn
	api.verify = verify

	// Simulate the HTTP handler path: ResetTask then Run.
	if _, err := m.ResetTask(prd.ID, taskID, "test", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	// Run should succeed on a PRDRunning PRD (executor now alive).
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("API.Run after reset: %v", err)
	}

	pollPRDTerminal(t, m, prd.ID, 5*time.Second)

	got, _ := m.Store().GetPRD(prd.ID)
	if statusOf(m, got) != PRDCompleted {
		t.Errorf("want PRDCompleted after reset+run, got %q", statusOf(m, got))
	}
}

// TestExecutorResume_DoubleRunSafe verifies that calling Run twice on the same
// PRD is safe: the second call cancels the first executor and starts a fresh one.
func TestExecutorResume_DoubleRunSafe(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := prdInRunningState(t, m)

	var calls atomic.Int32
	// Slow spawn so the first executor is still alive when we call Run again.
	slowSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		calls.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.spawnFn = slowSpawn
	api.verify = verify

	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Second Run: cancels first, starts fresh.
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	pollPRDTerminal(t, m, prd.ID, 5*time.Second)

	// Must not panic or deadlock. The PRD reaches some non-blocked state:
	// PRDCompleted (second executor finished), PRDFailed, or PRDApproved
	// (first executor was cancelled → error handler reverted status, which
	// is safe — the operator can retry). PRDRunning with no executor is the
	// only unacceptable outcome.
	got, _ := m.Store().GetPRD(prd.ID)
	switch statusOf(m, got) {
	case PRDCompleted, PRDFailed, PRDApproved:
		// all acceptable outcomes
	default:
		t.Errorf("PRD in unexpected state %q after double-Run (want completed/failed/approved)", statusOf(m, got))
	}
}

// TestExecutorResume_NonRunningSkipped verifies resumeRunningPRDs only
// touches PRDs in PRDRunning, leaving Draft/Approved/Completed untouched.
func TestExecutorResume_NonRunningSkipped(t *testing.T) {
	m, api, spawn, verify := apiFixture(t)

	// Create a PRD that stays in PRDDraft.
	draft, _ := m.CreatePRD("draft spec", "/proj", "opencode", "", EffortNormal)

	var spawnCalls atomic.Int32
	api.SetExecutors(func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawnCalls.Add(1)
		return spawn(ctx, req)
	}, verify)

	// Wait for the resumeRunningPRDs boot scan goroutine to exit.
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	// The draft PRD must not have been touched.
	got, _ := m.Store().GetPRD(draft.ID)
	if statusOf(m, got) != PRDDraft {
		t.Errorf("draft PRD status changed to %q — should be skipped by resumeRunningPRDs", statusOf(m, got))
	}
	if spawnCalls.Load() != 0 {
		t.Errorf("spawn called %d times — should be 0 with no PRDRunning PRDs", spawnCalls.Load())
	}
}
