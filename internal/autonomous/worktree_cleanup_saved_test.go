package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// savedFixture builds a base repo with the Automaton worktree placed at
// <worktreeDir>/<prdID> so the save step finds it. The verify folder is
// excluded from git so the work tree stays clean.
func savedFixture(t *testing.T, withScript bool) (m *Manager, prd *PRD, wtDir, worktree, baseRepo string) {
	t.Helper()
	baseRepo, orig := fixtureRepo(t)
	gitCmd(t, baseRepo, "worktree", "remove", "--force", orig)
	gitCmd(t, baseRepo, "branch", "-D", "automaton/fixture")
	m, _ = buildManager(t, baseRepo, config.CleanupModeOn, 0)
	wtDir = t.TempDir()
	m.mu.Lock()
	m.cfg.WorktreeDir = wtDir
	m.mu.Unlock()
	prd = saveFixturePRD(t, m, "automaton/savetest", PRDCancelled, "", time.Now().Add(-48*time.Hour))
	worktree = filepath.Join(wtDir, prd.ID)
	gitCmd(t, baseRepo, "worktree", "add", "-q", "-B", "automaton/savetest", worktree, "trunk")
	if err := os.MkdirAll(filepath.Join(baseRepo, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	ex := filepath.Join(baseRepo, ".git", "info", "exclude")
	if err := os.WriteFile(ex, []byte(".datawatch/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withScript {
		dir := filepath.Join(worktree, ".datawatch", "verify")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "x.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestCancelSavesVerifyScripts_CopyAndRelease(t *testing.T) {
	m, prd, wtDir, worktree, baseRepo := savedFixture(t, true)
	m.ReleaseAutomatonWorktree(prd)
	got, err := os.ReadFile(filepath.Join(savedVerifyScriptsDir(wtDir, prd.ID), "x.sh"))
	if err != nil || string(got) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("saved copy missing or wrong: %q, %v", got, err)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Fatalf("the Automaton worktree must still be removed")
	}
	if branchExists(t, baseRepo, "automaton/savetest") {
		t.Fatalf("the Automaton branch must still be removed")
	}
}

func TestCancelSavesVerifyScripts_HeldReleaseStillSaves(t *testing.T) {
	m, prd, wtDir, worktree, baseRepo := savedFixture(t, true)
	m.sessionListerFn = func() []SessionInfo {
		return []SessionInfo{{ID: "sess-1", Name: "autonomous:savetest", ProjectDir: worktree, Alive: true}}
	}
	m.ReleaseAutomatonWorktree(prd)
	if _, err := os.Stat(filepath.Join(savedVerifyScriptsDir(wtDir, prd.ID), "x.sh")); err != nil {
		t.Fatalf("scripts must be saved even when the release is held: %v", err)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("held release must leave the worktree: %v", err)
	}
	if !branchExists(t, baseRepo, "automaton/savetest") {
		t.Fatalf("held release must leave the branch")
	}
}

func TestCancelSavesVerifyScripts_DeleteRemovesSaved(t *testing.T) {
	m, prd, wtDir, _, _ := savedFixture(t, true)
	m.ReleaseAutomatonWorktree(prd)
	saved := savedVerifyScriptsDir(wtDir, prd.ID)
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("precondition: saved folder missing: %v", err)
	}
	if err := m.DeletePRD(prd.ID); err != nil {
		t.Fatalf("DeletePRD: %v", err)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatalf("delete must remove the saved folder, stat err = %v", err)
	}
}

func TestCancelSavesVerifyScripts_NoScriptsSavesNothing(t *testing.T) {
	m, prd, wtDir, worktree, _ := savedFixture(t, false)
	m.ReleaseAutomatonWorktree(prd)
	if _, err := os.Stat(savedVerifyScriptsDir(wtDir, prd.ID)); !os.IsNotExist(err) {
		t.Fatalf("nothing should be saved, stat err = %v", err)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Fatalf("release must still remove the worktree")
	}
}
