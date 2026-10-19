// Boot-time end-to-end scenarios for TaskVerifying tasks at store level.
//
// These three tests cover the boot reconciliation contract for tasks that
// were left in TaskVerifying when the daemon restarted, driving the same
// boot pass (reconcileStuckTasks) and store (GetPRD/SavePRD) that the CLI
// and REST endpoints report from:
//
//  1. recorded ok verification verdict + dead session → TaskCompleted
//     (the verdict stands; the task is NOT blindly re-run)
//  2. live session → left alone (not killed, status/identity preserved)
//  3. no recorded verdict + dead session → TaskPending, then exactly ONE
//     normal retry through executeOne (one spawn at retries=0), ending
//     completed when the fake verifier passes.
//
// Safety: only recorded fake callbacks are used — no tmux kill-server,
// kill-session -a, pkill, killall or systemctl anywhere.

package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// bootVerifyingSetup creates a t.TempDir() manager with one running PRD
// containing a single TaskVerifying task (optionally seeded via the given
// mutate fn) and wires the recorded fake killer hook. The returned killed
// slice records every session ID the killer fake was asked to kill.
func bootVerifyingSetup(t *testing.T, mutate func(task *Task)) (*Manager, *PRD, *[]string) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "story",
			Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-boot-1"},
			},
		},
	})
	if mutate != nil {
		stored, ok := m.Store().GetPRD(prd.ID)
		if !ok {
			t.Fatal("GetPRD after SetStories: not found")
		}
		mutate(&stored.Story[0].Tasks[0])
		_ = m.Store().SavePRD(stored)
	}
	prd, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found")
	}
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, ok = m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found")
	}

	killed := &[]string{}
	m.SetSessionKillerFn(func(sessionID string) error {
		*killed = append(*killed, sessionID)
		return nil
	})
	return m, prd, killed
}

// TestBootVerifyingOkVerdict: a TaskVerifying task with a recorded ok
// verification verdict and a DEAD session is the relaunched-after-restart
// case — the boot pass must complete it from the recorded verdict instead
// of re-running it.
func TestBootVerifyingOkVerdict(t *testing.T) {
	m, prd, _ := bootVerifyingSetup(t, func(task *Task) {
		task.Verification = &VerificationResult{OK: true, Summary: "ok"}
	})

	// Fake alive: the session is dead.
	m.SetSessionAliveFn(func(_ string) bool { return false })

	// Run the boot pass.
	m.reconcileStuckTasks()

	// Read the task back through the store — the same source the CLI and
	// REST endpoints report from.
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found")
	}
	task := got.Story[0].Tasks[0]

	if task.Status == TaskVerifying {
		t.Fatalf("task must NEVER still be TaskVerifying after the boot pass")
	}
	if task.Status != TaskCompleted {
		t.Fatalf("want TaskCompleted (recorded ok verdict stands), got %q", task.Status)
	}
	if task.SessionID != "" {
		t.Fatalf("want cleared SessionID, got %q", task.SessionID)
	}
	if task.Verification == nil || !task.Verification.OK || task.Verification.Summary != "ok" {
		t.Fatalf("want recorded verification {OK:true, Summary:\"ok\"} to be preserved, got %+v", task.Verification)
	}
}

// TestBootVerifyingLiveSession: a TaskVerifying task whose session is still
// alive must be left alone: still TaskVerifying with the same SessionID,
// not killed, session identity preserved for adoption by the executor.
func TestBootVerifyingLiveSession(t *testing.T) {
	m, prd, killed := bootVerifyingSetup(t, nil)

	// Fake alive: this session is live.
	m.SetSessionAliveFn(func(sessionID string) bool {
		return sessionID == "sess-boot-1"
	})

	m.reconcileStuckTasks()

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found")
	}
	task := got.Story[0].Tasks[0]

	if task.Status != TaskVerifying {
		t.Fatalf("want task left as TaskVerifying (live session), got %q", task.Status)
	}
	if task.SessionID != "sess-boot-1" {
		t.Fatalf("want session identity preserved (sess-boot-1), got %q", task.SessionID)
	}
	for _, s := range *killed {
		if s == "sess-boot-1" {
			t.Fatalf("live session sess-boot-1 must not have been killed")
		}
	}
}

// TestBootVerifyingNoVerdictRetry: a TaskVerifying task whose verifier
// exited with NO recorded verdict and a dead session must go to the normal
// retry path exactly once — TaskPending after boot, then ONE spawn through
// executeOne at retries=0, ending completed when the fake verifier passes.
func TestBootVerifyingNoVerdictRetry(t *testing.T) {
	// No verdict recorded, dead session.
	m, prd, _ := bootVerifyingSetup(t, func(task *Task) {
		task.Verification = nil
	})
	m.SetSessionAliveFn(func(_ string) bool { return false })

	m.reconcileStuckTasks()

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found")
	}
	task := got.Story[0].Tasks[0]
	if task.Status != TaskPending {
		t.Fatalf("want TaskPending (no recorded verdict, dead session), got %q", task.Status)
	}
	if task.SessionID != "" {
		t.Fatalf("want cleared SessionID after reset, got %q", task.SessionID)
	}

	// Drive it through one executeOne-style retry with fake spawn/verify
	// closures (mirrors how executeOne is invoked by the executor loop).
	spawnCount := 0
	spawnFn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCount++
		return SpawnResult{SessionID: "sess-relaunch"}, nil
	}
	verifyFn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	if err := m.executeOne(context.Background(), got, &got.Story[0].Tasks[0], spawnFn, verifyFn, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}

	if spawnCount != 1 {
		t.Fatalf("want exactly ONE spawn at retries=0, got %d", spawnCount)
	}
	after := got.Story[0].Tasks[0]
	if after.Status != TaskCompleted {
		t.Fatalf("want TaskCompleted after the single successful attempt, got %q", after.Status)
	}
}
