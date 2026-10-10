// BL407 Phase 1 — integration test proving Manager.Run actually wires
// EnsureWorktree in: a PRD with no ProjectDir/ProjectProfile/
// ClusterProfile, run against a daemon with WorktreeBaseRepo
// configured, ends up pointed at a real worktree instead of running
// bare.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_CreatesWorktreeWhenConfigured(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Empty projectDir — the gating condition for worktree creation.
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var gotProjectDir string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		gotProjectDir = req.ProjectDir
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectDir == "" {
		t.Fatal("ProjectDir was not set by worktree creation")
	}
	if got.ProjectDir == baseRepo {
		t.Fatal("ProjectDir points at the base repo itself, not an isolated worktree")
	}
	if got.Git.Branch == "" {
		t.Error("Git.Branch was not persisted")
	}
	if gotProjectDir != got.ProjectDir {
		t.Errorf("task spawn used ProjectDir=%q, want the worktree path %q", gotProjectDir, got.ProjectDir)
	}
	if _, err := os.Stat(filepath.Join(got.ProjectDir, ".git")); err != nil {
		t.Errorf("worktree .git missing at %s: %v", got.ProjectDir, err)
	}
}

func TestRun_NoWorktreeWhenBaseRepoNotConfigured(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil) // WorktreeBaseRepo empty
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectDir != "" {
		t.Errorf("ProjectDir=%q, want empty when WorktreeBaseRepo is not configured (backward compat)", got.ProjectDir)
	}
}

func TestRun_ExplicitProjectDirSkipsWorktree(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	explicitDir := t.TempDir()
	prd, _ := m.CreatePRD("spec", explicitDir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectDir != explicitDir {
		t.Errorf("ProjectDir=%q, want unchanged explicit %q (explicit must win over worktree auto-creation)", got.ProjectDir, explicitDir)
	}
}
