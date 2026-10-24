// WriteScope handoff — these tests prove the PRD's effective write dirs
// actually reach the spawned task session: the executor builds
// SpawnRequest.WriteScope from EffectiveTaskWriteDirs(prd, task), and for a
// worktree-mode PRD that is exactly the PRD's own worktree
// (prd.ProjectDir, set by ensureWorktreeIfConfigured) when no WriteDirs are
// declared. They reuse spawnGitCapture from executor_git_test.go, which runs
// one executeOne cycle with a fake SpawnFn that records the SpawnRequest and
// a fake VerifyFn that passes. No real sessions, no daemon, no network.

package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWriteScopeForwardedFromDeclaredWriteDirs(t *testing.T) {
	cases := []struct {
		name string
		// taskProjectDir, when set, applies a per-task ProjectDir override.
		taskProjectDir func(writeDir string) string
	}{
		{name: "declared PRD write dirs reach the spawn request"},
		{
			name:           "task project dir override with declared PRD write dirs",
			taskProjectDir: func(writeDir string) string { return filepath.Join(writeDir, "nested") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			m, err := NewManager(dir, DefaultConfig(), nil)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			// ProjectDir elsewhere; the declared WriteDirs are two other
			// absolute temp subdirs.
			projectDir := t.TempDir()
			writeDirs := []string{filepath.Join(t.TempDir(), "scope-one"), filepath.Join(t.TempDir(), "scope-two")}
			for _, d := range writeDirs {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			prd, _ := m.CreatePRD("spec", projectDir, "", "", "")
			_ = m.Store().SetStories(prd.ID, []Story{
				{Title: "story", Tasks: []Task{{Title: "task"}}},
			})
			prd, _ = m.Store().GetPRD(prd.ID)
			prd.WriteDirs = writeDirs
			if tc.taskProjectDir != nil {
				prd.Story[0].Tasks[0].ProjectDir = tc.taskProjectDir(writeDirs[0])
			}
			prd.Status = PRDRunning
			_ = m.Store().SavePRD(prd)
			prd, _ = m.Store().GetPRD(prd.ID)
			task := &prd.Story[0].Tasks[0]

			req := spawnGitCapture(t, m, prd, task)
			if req == nil {
				t.Fatal("spawn was not called")
			}

			want := []string{writeDirs[0], writeDirs[1]}
			if !reflect.DeepEqual(req.WriteScope, want) {
				t.Errorf("SpawnRequest.WriteScope = %v, want exactly the declared PRD write dirs %v (not ProjectDir %q; a task ProjectDir override must not replace them)", req.WriteScope, want, projectDir)
			}
			for _, d := range req.WriteScope {
				if d == projectDir {
					t.Errorf("SpawnRequest.WriteScope contains ProjectDir %q: declared WriteDirs must win", projectDir)
				}
			}
		})
	}
}

func TestWriteScopeWorktreeModeIsOwnWorktree(t *testing.T) {
	// Real throwaway worktree: a scratch git repo as the base repo, and
	// ensureWorktreeIfConfigured (the same gate Run() uses) creating the
	// PRD's own worktree under a temp dir. No WriteDirs are declared, so the
	// spawn request's WriteScope must be exactly the PRD's own worktree path.
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
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)

	created, werr := m.ensureWorktreeIfConfigured(prd)
	if !created {
		t.Fatalf("ensureWorktreeIfConfigured: created=false (worktree should have been created; err=%v)", werr)
	}
	if prd.ProjectDir == "" {
		t.Fatal("ensureWorktreeIfConfigured did not set prd.ProjectDir to the worktree path")
	}
	if len(prd.WriteDirs) != 0 {
		t.Fatalf("prd.WriteDirs = %v, want empty: the test asserts the undeclared-dirs worktree path", prd.WriteDirs)
	}
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.ProjectDir == "" {
		t.Fatal("persisted prd.ProjectDir is empty")
	}
	if prd.ProjectDir == baseRepo {
		t.Fatal("worktree path points at the base repo itself, not an isolated worktree")
	}
	// Sanity: this is a real worktree (shares the base repo's object store).
	if _, err := os.Stat(filepath.Join(prd.ProjectDir, ".git")); err != nil {
		t.Fatalf("worktree .git missing at %s: %v", prd.ProjectDir, err)
	}

	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	req := spawnGitCapture(t, m, prd, task)
	if req == nil {
		t.Fatal("spawn was not called")
	}

	want := []string{prd.ProjectDir}
	if !reflect.DeepEqual(req.WriteScope, want) {
		t.Errorf("SpawnRequest.WriteScope = %v, want exactly the PRD's own worktree path %v (no WriteDirs declared)", req.WriteScope, want)
	}
}
