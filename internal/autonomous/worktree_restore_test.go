package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureWorktree_RestoresSavedScripts(t *testing.T) {
	repo := scratchGitRepo(t)
	wtDir := t.TempDir()
	prd := &PRD{ID: "rest0001", Title: "Restore"}

	path, branch, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}
	if _, err := runGitIn(repo, "worktree", "remove", "--force", path); err != nil {
		t.Fatalf("worktree remove: %v", err)
	}

	saved := savedVerifyScriptsDir(wtDir, prd.ID)
	if err := os.MkdirAll(saved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saved, "check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prd.Git.Branch = branch
	path2, _, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatalf("second EnsureWorktree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(path2, ".datawatch", "verify", "check.sh"))
	if err != nil {
		t.Fatalf("script not restored: %v", err)
	}
	if !strings.Contains(string(got), "exit 0") {
		t.Fatalf("unexpected content %q", got)
	}
}

func TestEnsureWorktree_StrayFolderMovedAside(t *testing.T) {
	repo := scratchGitRepo(t)
	wtDir := t.TempDir()
	prd := &PRD{ID: "stray001", Title: "Stray"}

	path := filepath.Join(wtDir, prd.ID)
	verifyDir := filepath.Join(path, ".datawatch", "verify")
	if err := os.MkdirAll(verifyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verifyDir, "old.sh"), []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	gotPath, branch, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %s, want %s", gotPath, path)
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
		t.Fatalf("new worktree has no .git: %v", err)
	}
	head, err := runGitIn(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(head) != branch {
		t.Fatalf("HEAD = %q (err %v), want %s", head, err, branch)
	}
	if cerr := CheckProjectWorktree(path, repo); cerr != nil {
		t.Fatalf("not a valid worktree: %v", cerr)
	}

	matches, _ := filepath.Glob(filepath.Join(savedVerifyScriptsDir(wtDir, prd.ID), "stray-*", ".datawatch", "verify", "old.sh"))
	if len(matches) != 1 {
		t.Fatalf("old contents not under saved folder, matches=%v", matches)
	}
	if _, err := os.Stat(filepath.Join(path, ".datawatch", "verify", "old.sh")); err != nil {
		t.Fatalf("script not restored into new worktree: %v", err)
	}
}
