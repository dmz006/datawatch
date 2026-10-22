package autonomous

// Session-start retry budget: a spawned session that never started
// (ErrSessionNeverStarted, or a prompt that was never submitted and got
// mapped onto that sentinel at the cmd/datawatch boundary) re-queues the
// SAME attempt on its own small budget (SessionStartRetryBudget) without
// consuming the PRD's normal auto-retry budget (RetryPolicy categories,
// RetryCeilingTotal, AutoFixRetries). Exceeding the budget fails the task
// with the "session never started" reason, keeping StartFailureTail.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// startErr builds a verify error the way the daemon's one-shot wait loop
// builds it: the autonomous start sentinel wrapped with the worker-stall
// sentinel (ErrSessionNeverStarted must win in the executor branch).
func startErr(what string) error {
	return fmt.Errorf("session never started: %s — %w; %w", what, ErrSessionNeverStarted, ErrWorkerStalled)
}

// waitForPRDTerminal polls the store until the PRD leaves PRDRunning.
func waitForPRDTerminal(t *testing.T, m *Manager, prdID string) *PRD {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := m.Store().GetPRD(prdID)
		if !ok {
			t.Fatal("PRD disappeared")
		}
		if got.Status != PRDRunning {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("PRD still PRDRunning after 10s")
	return nil
}

func TestSessionStartBudget_RequeuesAndFailsOnThirdFailure(t *testing.T) {
	m, api, baseSpawn, _ := apiFixture(t)

	var mu sync.Mutex
	var spawnCalls, verifyCalls []int

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		spawnCalls = append(spawnCalls, 1)
		return SpawnResult{SessionID: "sess-" + fmt.Sprint(len(spawnCalls))}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		verifyCalls = append(verifyCalls, 1)
		// Mimic the daemon's wait loop: it sets the failure tail on the task
		// before the kill, so the same task object carries it here.
		return VerificationResult{}, startErr("no assistant output after bound")
	}

	prd, err := m.CreatePRD("test spec", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Story[0].Tasks[0].StartFailureTail = []string{"❯ paste prompt", "[Pasted text #1 +2 lines]", "still waiting…"}
	fresh.Status = PRDApproved
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	_ = baseSpawn

	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitForPRDTerminal(t, m, prd.ID)
	if got.Story[0].Tasks[0].Status != TaskFailed {
		t.Fatalf("want TaskFailed after the start-failure budget, got %q", got.Story[0].Tasks[0].Status)
	}
	task := got.Story[0].Tasks[0]
	// SessionStartRetryBudget = 2 re-queues: failures 1 and 2 re-queue,
	// the 3rd fails the task — 3 verify calls and 3 spawns total.
	mu.Lock()
	lenSpawn, lenVerify := len(spawnCalls), len(verifyCalls)
	mu.Unlock()
	if lenVerify != 3 {
		t.Fatalf("want 3 verify calls (2 re-queued + 1 terminal), got %d", lenVerify)
	}
	if lenSpawn != 3 {
		t.Fatalf("want 3 spawns (initial + 2 re-queues), got %d", lenSpawn)
	}
	if task.StartFailures != 3 {
		t.Fatalf("want StartFailures=3 on the failed task, got %d", task.StartFailures)
	}
	if task.Error != SessionStartFailureReason {
		t.Fatalf("want Error=%q, got %q", SessionStartFailureReason, task.Error)
	}
	// StartFailureTail survives to the failed task.
	if len(task.StartFailureTail) != 3 || task.StartFailureTail[2] != "still waiting…" {
		t.Fatalf("StartFailureTail not preserved on the failed task: %v", task.StartFailureTail)
	}
	// Normal retry counters are untouched by start failures.
	if len(got.RetryCountsByCategory) != 0 {
		t.Fatalf("want no RetryPolicy counts consumed, got %v", got.RetryCountsByCategory)
	}
	// RetryCeilingTotal on the PRD is 0 = "use config default" — start
	// failures must not set it, and the manager's AutoFixRetries is intact.
	if got.RetryCeilingTotal != 0 {
		t.Fatalf("RetryCeilingTotal changed on the PRD: %d", got.RetryCeilingTotal)
	}
	if m.cfg.AutoFixRetries != DefaultConfig().AutoFixRetries {
		t.Fatalf("AutoFixRetries changed: %d", m.cfg.AutoFixRetries)
	}
}

func TestSessionStartBudget_RequeueStillSucceeds(t *testing.T) {
	m, api, baseSpawn, _ := apiFixture(t)
	_ = baseSpawn

	var count int
	var mu sync.Mutex
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-requeue"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		count++
		if count == 1 {
			return VerificationResult{}, startErr("first spawn never started")
		}
		return VerificationResult{OK: true}, nil
	}

	prd, err := m.CreatePRD("test spec", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Status = PRDApproved
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitForPRDTerminal(t, m, prd.ID)

	task := got.Story[0].Tasks[0]
	if task.Status != TaskCompleted {
		t.Fatalf("want TaskCompleted after a start-failure re-queue, got %q (err=%q)", task.Status, task.Error)
	}
	if task.StartFailures != 1 {
		t.Fatalf("want StartFailures=1 recorded on the task, got %d", task.StartFailures)
	}
	if len(got.RetryCountsByCategory) != 0 {
		t.Fatalf("start-failure re-queue must not touch RetryPolicy counts; got %v", got.RetryCountsByCategory)
	}
}

func TestSessionStartBudget_NonStartErrorStillUsesNormalBudget(t *testing.T) {
	m, api, baseSpawn, _ := apiFixture(t)
	_ = baseSpawn

	retries := m.cfg.AutoFixRetries
	mu := new(sync.Mutex)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-stall"}, nil
	}
	// A genuine (non-start) stall: wraps ErrWorkerStalled but NOT
	// ErrSessionNeverStarted. Must fall through to the normal stall branch
	// and consume the normal attempt budget.
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return VerificationResult{}, fmt.Errorf("log stale: %w", ErrWorkerStalled)
	}

	prd, err := m.CreatePRD("test spec", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Status = PRDApproved
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitForPRDTerminal(t, m, prd.ID)

	task := got.Story[0].Tasks[0]
	if task.Status != TaskFailed {
		t.Fatalf("want TaskFailed after the normal budget, got %q", task.Status)
	}
	if task.StartFailures != 0 {
		t.Fatalf("non-start errors must not touch StartFailures, got %d", task.StartFailures)
	}
	if task.Error == "" || task.Error == SessionStartFailureReason {
		t.Fatalf("want the normal exhaustion error, got %q", task.Error)
	}
	// AutoFixRetries itself (the config value) is untouched.
	if m.cfg.AutoFixRetries != retries {
		t.Fatalf("AutoFixRetries changed: %d -> %d", retries, m.cfg.AutoFixRetries)
	}
}
