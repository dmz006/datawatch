package session

// Incident pin: 2026-10-19. An out-of-scope (nil scope) session
// auto-commit ran `git add -A` and swept a PRE-EXISTING untracked file
// (operator scratch predating the run) into the session's commit, next
// to the files the run actually touched.
//
// This test reproduces that exact scenario and locks in the fix:
// with a nil scope, the commit contains the run's real change set —
// the tracked file it modified plus the file it created — and never
// the untracked file that already existed; the pre-existing file stays
// untracked in the working tree afterward.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCommitSessionWork_Unscoped2026_10_19Incident pins the incident
// end-to-end through the public CommitSessionWork API.
func TestCommitSessionWork_Unscoped2026_10_19Incident(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	// Tracked file, committed so HEAD contains it at run start.
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "--", "tracked.txt")
	mustGit(t, dir, "commit", "-q", "-m", "seed tracked.txt")

	// Pre-existing untracked file, mtime 24h old — clearly before the
	// simulated run, so mtime filtering can't get credit from a fresh
	// file either.
	preexisting := filepath.Join(dir, "preexisting-untracked.txt")
	if err := os.WriteFile(preexisting, []byte("operator scratch from before the run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pre := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(preexisting, pre, pre); err != nil {
		t.Fatal(err)
	}

	// Simulated run started 30s ago.
	runStart := time.Now().Add(-30 * time.Second)
	g.SetRunStart(runStart)

	// The run: modify the tracked file, create a new file. Both get
	// fresh mtimes (after runStart).
	if err := os.WriteFile(tracked, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCreated := filepath.Join(dir, "run-created.txt")
	if err := os.WriteFile(runCreated, []byte("created by this run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act: unscoped staging + commit, exactly the path the incident
	// went through.
	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "task", "unscoped commit", nil)
	if err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a commit, got empty sha")
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}

	// The new HEAD tree contains the two run files ...
	tree, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree, "tracked.txt") {
		t.Errorf("modified tracked file missing from commit tree: %q", tree)
	}
	if !strings.Contains(tree, "run-created.txt") {
		t.Errorf("run-created file missing from commit tree: %q", tree)
	}
	// ... and NOT the pre-existing untracked file (the incident).
	treeLines := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(tree), "\n") {
		treeLines[strings.TrimSpace(l)] = true
	}
	if treeLines["preexisting-untracked.txt"] {
		t.Errorf("PRE-EXISTING untracked file leaked into commit (2026-10-19 incident): %q", tree)
	}

	// The pre-existing file must still be untracked in the working
	// tree — `git status --porcelain` lists it as `??`.
	status, err := gitOutput(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	foundUntracked := false
	for _, l := range strings.Split(strings.TrimSpace(status), "\n") {
		if l == "?? preexisting-untracked.txt" {
			foundUntracked = true
		}
	}
	if !foundUntracked {
		t.Errorf("preexisting-untracked.txt should remain untracked (\"??\"), status=%q", status)
	}
	// And nothing left staged behind, for tracked.txt / run-created.txt.
	staged, err := gitOutput(dir, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(staged, "tracked.txt") || strings.Contains(staged, "run-created.txt") {
		t.Errorf("run files left staged after commit: %q", staged)
	}
}

// TestUnscopedChangedFiles_2026_10_19Fixture unit-tests the changed-
// files helper the fix introduced on the same fixture: with a recorded
// run start it returns the two run files and not the pre-existing one;
// with no repo changes at all it returns nothing.
func TestUnscopedChangedFiles_2026_10_19Fixture(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	// Clean state first: no repo changes, helper returns no files.
	if files := g.unscopedChangedFiles(time.Now().Add(-30 * time.Second)); len(files) != 0 {
		t.Fatalf("unscopedChangedFiles on a clean tree = %v, want empty", files)
	}

	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "--", "tracked.txt")
	mustGit(t, dir, "commit", "-q", "-m", "seed")

	preexisting := filepath.Join(dir, "preexisting-untracked.txt")
	if err := os.WriteFile(preexisting, []byte("pre-run scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(preexisting, old, old); err != nil {
		t.Fatal(err)
	}

	runStart := time.Now().Add(-30 * time.Second)
	if err := os.WriteFile(tracked, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run-created.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := g.unscopedChangedFiles(runStart)
	contains := func(name string) bool {
		for _, f := range files {
			if f == name {
				return true
			}
		}
		return false
	}
	if !contains("tracked.txt") {
		t.Errorf("modified tracked file missing from changed files: %v", files)
	}
	if !contains("run-created.txt") {
		t.Errorf("run-created file missing from changed files: %v", files)
	}
	if contains("preexisting-untracked.txt") {
		t.Errorf("pre-existing untracked file must be excluded with a run start: %v", files)
	}
	if len(files) != 2 {
		t.Errorf("changed files = %v, want exactly [run-created.txt tracked.txt]", files)
	}
}
