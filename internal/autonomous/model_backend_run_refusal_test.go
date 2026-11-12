package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// newRefusalPRD builds an approved PRD on a fresh Manager. The PRD-level
// backend is set to backend, and each entry of tasks becomes one task in a
// single story with the given backend/model overrides.
func newRefusalPRD(t *testing.T, backend string, tasks []Task) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Backend = backend
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func refusalVerify(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

// TestModelBackendRunRefusalPiPromptClaudeAliasNeverSpawns is the regression
// case: a pi-prompt PRD whose task pins a bare Claude alias must be refused
// by Run before any spawn, and the PRD must not be moved to running.
func TestModelBackendRunRefusalPiPromptClaudeAliasNeverSpawns(t *testing.T) {
	m, prd := newRefusalPRD(t, "pi-prompt", []Task{{Title: "t", Spec: "do it", Model: "claude-haiku-5-5"}})

	var spawns int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: "s1"}, nil
	}

	err := m.Run(context.Background(), prd.ID, spawn, refusalVerify)
	if err == nil {
		t.Fatal("Run returned nil; want a model/backend refusal")
	}
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Errorf("Run error %v does not wrap ErrModelBackendMismatch", err)
	}
	if got := atomic.LoadInt32(&spawns); got != 0 {
		t.Errorf("spawn called %d times; want 0", got)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status == PRDRunning {
		t.Errorf("PRD status = %q after refused Run; want it not running", got.Status)
	}
	if got.Status != PRDApproved {
		t.Errorf("PRD status = %q; want it left approved", got.Status)
	}
}

// TestModelBackendRunRefusalListsOnlyMismatchedTask checks that a valid PRD
// with one provider-qualified mismatch on a single task refuses to run and
// names only that task.
func TestModelBackendRunRefusalListsOnlyMismatchedTask(t *testing.T) {
	m, prd := newRefusalPRD(t, "pi-prompt", []Task{
		{Title: "ok", Spec: "fine", Model: "anthropic/claude-haiku-5-5"},
		{Title: "bad", Spec: "broken", Backend: "claude-code", Model: "ollama/qwen3.8:27b"},
	})
	okID, badID := "", ""
	for _, tk := range prd.Story[0].Tasks {
		if tk.Title == "ok" {
			okID = tk.ID
		} else {
			badID = tk.ID
		}
	}

	var spawns int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: "s1"}, nil
	}

	err := m.Run(context.Background(), prd.ID, spawn, refusalVerify)
	if err == nil {
		t.Fatal("Run returned nil; want a model/backend refusal")
	}
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Errorf("Run error %v does not wrap ErrModelBackendMismatch", err)
	}
	if !strings.Contains(err.Error(), badID) {
		t.Errorf("error %q should name the mismatched task %q", err.Error(), badID)
	}
	if strings.Contains(err.Error(), okID) {
		t.Errorf("error %q should not name the valid task %q", err.Error(), okID)
	}
	if got := atomic.LoadInt32(&spawns); got != 0 {
		t.Errorf("spawn called %d times; want 0", got)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDApproved {
		t.Errorf("PRD status = %q; want it left approved", got.Status)
	}
}

// TestModelBackendRunRefusalDispatchBackstopNeverSpawns exercises the dispatch
// backstop directly: a task whose resolved pair is invalid must not reach
// spawn, must be marked failed with the mismatch named, and must return an
// error wrapping ErrModelBackendMismatch.
func TestModelBackendRunRefusalDispatchBackstopNeverSpawns(t *testing.T) {
	m, prd := newRefusalPRD(t, "pi-prompt", []Task{{Title: "t", Spec: "do it", Model: "claude-haiku-5-5"}})
	task := &prd.Story[0].Tasks[0]

	var spawns int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: "s1"}, nil
	}

	err := m.executeOne(context.Background(), prd, task, spawn, refusalVerify, 0, pipeline.QualityGateConfig{}, nil)
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Fatalf("executeOne error = %v; want ErrModelBackendMismatch", err)
	}
	if got := atomic.LoadInt32(&spawns); got != 0 {
		t.Errorf("spawn called %d times; want 0", got)
	}
	if task.Status != TaskFailed {
		t.Errorf("task status = %q; want %q", task.Status, TaskFailed)
	}
	if !strings.Contains(task.Error, "claude-haiku-5-5") || !strings.Contains(task.Error, "pi-prompt") {
		t.Errorf("task error %q should name the model and backend", task.Error)
	}
}
