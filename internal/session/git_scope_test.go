package session

// Scoped auto-commit staging (auto-commit leak fix). The historical
// `git add -A`-of-everything sweeped unrelated untracked/unstaged files
// out of the same tree (sibling sessions, operator scratch) into the
// session's commit. These tests pin the new behavior: a non-empty scope
// stages only files within it, and out-of-scope changes never leak into
// the commit; an empty scope preserves the historical broad behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitSessionWork_ScopedStagesOnlyInScopeDir(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	scopeDir := filepath.Join(dir, "in")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "scope.txt"), []byte("in scope"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "leak.txt"), []byte("out of scope"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "task", "scoped commit", []string{scopeDir})
	if err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}
	if sha == "" {
		t.Error("empty sha for a scoped commit")
	}

	// The in-scope file is in HEAD; the out-of-scope file must not be.
	out, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "in/scope.txt") {
		t.Errorf("in-scope file missing from commit: %q", out)
	}
	if strings.Contains(out, "out/leak.txt") {
		t.Errorf("out-of-scope file leaked into commit: %q", out)
	}

	// And out/leak.txt is still untracked (never touched).
	st, err := gitOutput(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st, "out/leak.txt") {
		t.Errorf("out-of-scope file should remain untracked: %q", st)
	}
}

func TestCommitSessionWork_ScopedOutOfScopeDirtyTreeStillCommitsOnlyScope(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	// A tracked file outside the scope, dirtied after the baseline.
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "seed tracked file")

	// Dirty the tracked out-of-scope file, and add an in-scope change.
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scopeDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := headCount(t, dir)
	if _, err := g.CommitSessionWork("sess1", "task", "scoped", []string{scopeDir}); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}
	// The new commit must add app/main.go and must NOT touch notes.md.
	// (notes.md is tracked from the seed commit, so the full HEAD tree
	// legitimately contains it.)
	changed, err := gitOutput(dir, "diff", "--name-only", "HEAD~1", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changed, "app/main.go") {
		t.Errorf("in-scope file missing from commit: %q", changed)
	}
	if strings.Contains(changed, "notes.md") {
		t.Errorf("out-of-scope modified file leaked into commit: %q", changed)
	}
	// notes.md must still be modified/uncommitted.
	mod, err := gitOutput(dir, "diff", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mod, "notes.md") {
		t.Errorf("notes.md should still be uncommitted: %q", mod)
	}
}

func TestCommitSessionWork_ScopedNoChangesInScopeSkipsCommit(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	scopeDir := filepath.Join(dir, "in")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Dirty file OUTSIDE the scope only.
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "task", "should not commit", []string{scopeDir})
	if err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if sha != "" {
		t.Errorf("sha=%q want empty (nothing in scope staged)", sha)
	}
	if got := headCount(t, dir); got != before {
		t.Errorf("commits=%d want %d (no new commit)", got, before)
	}
	// scratch.txt must have been left alone (not staged either).
	staged, err := gitOutput(dir, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(staged, "scratch.txt") {
		t.Errorf("out-of-scope file was staged into the index: %q", staged)
	}
}

func TestCommitSessionWork_UnscopedStagesNewFiles(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "brand", "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brand", "new", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := headCount(t, dir)
	if _, err := g.CommitSessionWork("sess1", "task", "unscoped", nil); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}
	out, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "brand/new/f.txt") || !strings.Contains(out, "top.txt") {
		t.Errorf("unscoped commit should include all new files: %q", out)
	}
}

func TestPreSessionCommit_ScopedLeavesOutOfScopeUnstaged(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	scopeDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An out-of-scope untracked file that the historical `add -A`
	// would have swept into the pre-session snapshot.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := g.PreSessionCommit("sess1", "task", []string{scopeDir}); err != nil {
		t.Fatalf("PreSessionCommit: %v", err)
	}
	out, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "work/a.txt") {
		t.Errorf("in-scope file missing from pre-session snapshot: %q", out)
	}
	if strings.Contains(out, "unrelated.txt") {
		t.Errorf("out-of-scope file leaked into pre-session snapshot: %q", out)
	}
}

func TestStageChanges_ScopeDirOutsideRepoDoesNotLeakOrError(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Scope points at a directory that exists but lives OUTSIDE this
	// repo (another PRD's write dir, on another host or path). The
	// commit must skip cleanly — never fall back to `add -A`, which
	// would be the exact leak being fixed.
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "task", "nope", []string{outside})
	if err != nil {
		t.Fatalf("CommitSessionWork with out-of-repo scope: %v", err)
	}
	if sha != "" {
		t.Errorf("sha=%q want empty — out-of-repo scope must not commit in-repo dirt", sha)
	}
	if got := headCount(t, dir); got != before {
		t.Errorf("commits=%d want %d", got, before)
	}
	// dirty.txt must remain untracked and unstaged.
	staged, err := gitOutput(dir, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(staged, "dirty.txt") {
		t.Errorf("dirty.txt was staged despite out-of-repo scope: %q", staged)
	}
}

func TestCommitSessionWork_ScopeRelativePath(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rel", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Relative scope entries are resolved against the process CWD
	// (filepath.Abs inside stageChanges): chdir to the repo's parent
	// and pass the repo subpath relative to it — this also proves a
	// scope entry that is a SUBdirectory of the repo root works.
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd) //nolint:errcheck
	if err := os.Chdir(filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	before := headCount(t, dir)
	if _, err := g.CommitSessionWork("sess1", "task", "rel scope",
		[]string{filepath.Base(dir) + "/rel"}); err != nil {
		t.Fatalf("CommitSessionWork with relative scope: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}
	out, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rel/x.txt") {
		t.Errorf("in-scope file missing from commit: %q", out)
	}
}

func TestPostSessionCommit_Scoped(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	scopeDir := filepath.Join(dir, "svc")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "s.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "noise.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := headCount(t, dir)
	if err := g.PostSessionCommit("sess1", "task", StateComplete, []string{scopeDir}); err != nil {
		t.Fatalf("PostSessionCommit: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Fatalf("commits=%d want %d", got, before+1)
	}
	out, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "noise.txt") {
		t.Errorf("out-of-scope file leaked into post-session commit: %q", out)
	}
	if !strings.Contains(out, "svc/s.txt") {
		t.Errorf("in-scope file missing from post-session commit: %q", out)
	}
}
