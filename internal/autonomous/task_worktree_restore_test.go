package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeRestoreTestScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func restoreTestTask() *Task {
	return &Task{ID: "t1", Title: "Do the thing", VerificationScript: ".datawatch/verify/check.sh"}
}

func TestTaskWorktree_RestoresVerifyScriptFromAutomatonWorktree(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	writeRestoreTestScript(t, filepath.Join(wtDir, prd.ID, ".datawatch", "verify"), "check.sh", "#!/bin/bash\necho fromprd\n")
	writeRestoreTestScript(t, savedVerifyScriptsDir(wtDir, prd.ID), "check.sh", "#!/bin/bash\necho saved\n")
	path, _, err := EnsureTaskWorktree(prd, restoreTestTask(), repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(path, ".datawatch", "verify", "check.sh"))
	if err != nil {
		t.Fatalf("script not restored: %v", err)
	}
	if string(got) != "#!/bin/bash\necho fromprd\n" {
		t.Fatalf("wrong source used: %q", got)
	}
}

func TestTaskWorktree_RestoresVerifyScriptFromSavedFolder(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	writeRestoreTestScript(t, savedVerifyScriptsDir(wtDir, prd.ID), "check.sh", "#!/bin/bash\necho saved\n")
	path, _, err := EnsureTaskWorktree(prd, restoreTestTask(), repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(path, ".datawatch", "verify", "check.sh"))
	if err != nil {
		t.Fatalf("script not restored from saved folder: %v", err)
	}
	if string(got) != "#!/bin/bash\necho saved\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestTaskWorktree_RestoresVerifyScriptMissingEverywhereIsNotAnError(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	path, _, err := EnsureTaskWorktree(prd, restoreTestTask(), repo, wtDir)
	if err != nil {
		t.Fatalf("EnsureTaskWorktree must not fail: %v", err)
	}
	_, ok, rerr := RunVerificationScript(".datawatch/verify/check.sh", path)
	if ok || !errors.Is(rerr, ErrVerificationScriptMissing) {
		t.Fatalf("ok=%v err=%v, want ErrVerificationScriptMissing", ok, rerr)
	}
}

func TestTaskWorktree_RestoresVerifyScriptAfterRecreate(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	writeRestoreTestScript(t, savedVerifyScriptsDir(wtDir, prd.ID), "check.sh", "#!/bin/bash\necho saved\n")
	task := restoreTestTask()
	path, branch, err := EnsureTaskWorktree(prd, task, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "worktree", "remove", "--force", path)
	gitRun(t, repo, "branch", "-D", branch)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("worktree should be gone")
	}
	path2, _, err := EnsureTaskWorktree(prd, task, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path2, ".datawatch", "verify", "check.sh")); err != nil {
		t.Fatalf("script not back after recreate: %v", err)
	}
}
