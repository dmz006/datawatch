// B93 — executor skips re-spawning when a task is already TaskVerifying
// with a live session on daemon restart.

package autonomous

import (
	"context"
	"fmt"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestB93_SkipSpawnForLiveVerifyingSession(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{
			{Title: "task", Status: TaskVerifying, SessionID: "sess-alive"},
		}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)

	m.SetSessionAliveFn(func(sessionID string) bool {
		return sessionID == "sess-alive"
	})

	spawnCalled := false
	spawnFn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalled = true
		return SpawnResult{SessionID: "new-sess"}, nil
	}
	verifyCalled := false
	verifyFn := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		verifyCalled = true
		if task.SessionID != "sess-alive" {
			return VerificationResult{}, fmt.Errorf("unexpected session %q, want sess-alive", task.SessionID)
		}
		return VerificationResult{OK: true}, nil
	}

	task := &prd.Story[0].Tasks[0]
	if err := m.executeOne(context.Background(), prd, task, spawnFn, verifyFn, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	if spawnCalled {
		t.Error("spawn must NOT be called for live TaskVerifying session on restart")
	}
	if !verifyCalled {
		t.Error("verify must still be called to wait for the live session")
	}
}

func TestB93_DeadVerifyingSessionSpawnsNew(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{
			{Title: "task", Status: TaskVerifying, SessionID: "sess-dead"},
		}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)

	m.SetSessionAliveFn(func(_ string) bool { return false }) // session is dead

	spawnCalled := false
	spawnFn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalled = true
		return SpawnResult{SessionID: "new-sess"}, nil
	}
	verifyFn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	task := &prd.Story[0].Tasks[0]
	if err := m.executeOne(context.Background(), prd, task, spawnFn, verifyFn, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	if !spawnCalled {
		t.Error("spawn must be called when the previous session is dead")
	}
}
