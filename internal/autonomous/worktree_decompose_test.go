// BL407 Phase 1 extension (found live 2026-10-11 creating BL404's own
// PRD): session-based decompose needs a real project_dir just like
// Run() does, but worktree creation only happened at Run() time —
// so decompose could never succeed for a worktree-mode PRD at all.
// Fixed by extracting ensureWorktreeIfConfigured and calling it from
// Decompose()/DecomposeStreaming() too. These tests also cover the
// orphan-cleanup backstop added at the same time (Reject/Archive).

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDecompose_CreatesWorktreeWhenConfigured(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	var capturedProjectDir string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedProjectDir = req.ProjectDir
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if capturedProjectDir != "" {
		t.Fatal("sanity: decompose should not have run yet")
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if capturedProjectDir == "" {
		t.Fatal("decompose ran without a project_dir — worktree was not created in time")
	}
	if _, serr := os.Stat(filepath.Join(capturedProjectDir, ".git")); serr != nil {
		t.Fatalf("project_dir %q is not a real git worktree: %v", capturedProjectDir, serr)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectDir != capturedProjectDir {
		t.Errorf("PRD.ProjectDir=%q not persisted to match the worktree used for decompose (%q)", got.ProjectDir, capturedProjectDir)
	}
	if got.Git.Branch == "" {
		t.Error("PRD.Git.Branch not set by decompose-time worktree creation")
	}
}

func TestDecomposeStreaming_CreatesWorktreeWhenConfigured(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	var capturedProjectDir string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedProjectDir = req.ProjectDir
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}
	if capturedProjectDir == "" {
		t.Fatal("decompose ran without a project_dir — worktree was not created in time")
	}
}

func TestDecompose_RunStillReusesTheSameWorktree(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	var decomposeDir string
	fakeFn := func(req DecomposeRequest) (string, error) {
		decomposeDir = req.ProjectDir
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	prd, _ = m.Store().GetPRD(prd.ID)
	branchAfterDecompose := prd.Git.Branch
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var runSpawnDir string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		runSpawnDir = req.ProjectDir
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if runSpawnDir != decomposeDir {
		t.Errorf("Run() created a SECOND worktree (%q) instead of reusing decompose's (%q) — EnsureWorktree's idempotency broke", runSpawnDir, decomposeDir)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Git.Branch != branchAfterDecompose {
		t.Errorf("branch changed between decompose (%q) and run (%q)", branchAfterDecompose, final.Git.Branch)
	}
}

func TestReject_CleansUpOrphanedWorktree(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	fakeFn := func(req DecomposeRequest) (string, error) { return fakePlanResp, nil }

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	worktreePath := prd.ProjectDir
	if _, serr := os.Stat(worktreePath); serr != nil {
		t.Fatalf("sanity: worktree should exist before reject: %v", serr)
	}

	if _, err := m.Reject(prd.ID, "operator", "not needed"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if _, serr := os.Stat(worktreePath); serr == nil {
		t.Errorf("worktree %q still exists after Reject — orphan cleanup did not run", worktreePath)
	}
}

func TestReject_LeavesWorktreeWithUncommittedChangesForInspection(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	fakeFn := func(req DecomposeRequest) (string, error) { return fakePlanResp, nil }

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	worktreePath := prd.ProjectDir
	if err := os.WriteFile(filepath.Join(worktreePath, "uncommitted.txt"), []byte("real work in progress"), 0o644); err != nil {
		t.Fatalf("seed uncommitted file: %v", err)
	}

	if _, err := m.Reject(prd.ID, "operator", "not needed"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if _, serr := os.Stat(worktreePath); serr != nil {
		t.Errorf("worktree with uncommitted content was removed despite the safety net: %v", serr)
	}
}

func TestArchive_CleansUpOrphanedWorktree(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	fakeFn := func(req DecomposeRequest) (string, error) { return fakePlanResp, nil }

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	worktreePath := prd.ProjectDir

	if err := NewAPI(m).Cancel(prd.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, serr := os.Stat(worktreePath); serr != nil {
		t.Fatalf("sanity: worktree must survive Cancel per Decision 5 (kept for inspection): %v", serr)
	}

	if _, err := m.Archive(prd.ID, "operator"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, serr := os.Stat(worktreePath); serr == nil {
		t.Errorf("worktree %q still exists after Archive — the universal backstop did not run", worktreePath)
	}
}
