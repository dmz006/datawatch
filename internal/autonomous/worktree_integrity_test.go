// Tests for CheckProjectWorktree and the EnsureWorktree reuse guard,
// built on scratch repositories in t.TempDir; no real worktree folder
// is read or written.
package autonomous

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// integrityGit runs git in dir with an environment stripped of any
// inherited repository selection and of global/system configuration.
func integrityGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// isolateGitEnv keeps the code under test from seeing an inherited
// repository selection, which would redirect its git calls.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v)
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unset %s: %v", k, err)
			}
		}
	}
}

// integrityRepo builds a base repository (via scratchGitRepo) and a
// linked worktree of it, returning both paths.
func integrityRepo(t *testing.T) (base, wt string) {
	t.Helper()
	isolateGitEnv(t)
	base = scratchGitRepo(t)
	wt = filepath.Join(t.TempDir(), "wt")
	integrityGit(t, base, "worktree", "add", "-b", "automaton/test", wt, "main")
	return base, wt
}

func requireIntegrityError(t *testing.T, err error, reasonPart string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an integrity error, got nil")
	}
	var ierr *WorktreeIntegrityError
	if !errors.As(err, &ierr) {
		t.Fatalf("error is %T, want *WorktreeIntegrityError: %v", err, err)
	}
	if !errors.Is(err, ErrWorktreeIntegrity) {
		t.Errorf("errors.Is(err, ErrWorktreeIntegrity) = false for %v", err)
	}
	if !strings.Contains(ierr.Reason, reasonPart) {
		t.Errorf("reason %q does not mention %q", ierr.Reason, reasonPart)
	}
	if !strings.Contains(err.Error(), ierr.Dir) || !strings.Contains(err.Error(), ierr.Reason) {
		t.Errorf("message %q must name the directory and the reason", err.Error())
	}
}

func TestCheckProjectWorktree_AcceptsLinkedWorktree(t *testing.T) {
	base, wt := integrityRepo(t)
	if err := CheckProjectWorktree(wt, base); err != nil {
		t.Fatalf("linked worktree rejected: %v", err)
	}
	// Without a base repository only the structural checks apply.
	if err := CheckProjectWorktree(wt, ""); err != nil {
		t.Fatalf("linked worktree rejected without a base repo: %v", err)
	}
}

func TestCheckProjectWorktree_RejectsFreshRepoWithOnlySessionFiles(t *testing.T) {
	isolateGitEnv(t)
	base := scratchGitRepo(t)
	dir := filepath.Join(t.TempDir(), "5f440001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	integrityGit(t, dir, "init", "-b", "master")
	for _, f := range []string{"CLAUDE.md", ".mcp.json", "CHECKPOINT.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	requireIntegrityError(t, CheckProjectWorktree(dir, base), "standalone repository")
	requireIntegrityError(t, CheckProjectWorktree(dir, ""), "standalone repository")
}

func TestCheckProjectWorktree_RejectsMissingDir(t *testing.T) {
	isolateGitEnv(t)
	base := scratchGitRepo(t)
	requireIntegrityError(t, CheckProjectWorktree(filepath.Join(t.TempDir(), "gone"), base), "does not exist")
	requireIntegrityError(t, CheckProjectWorktree("", base), "empty")
}

func TestCheckProjectWorktree_RejectsWorktreeOfDifferentRepo(t *testing.T) {
	isolateGitEnv(t)
	base := scratchGitRepo(t)
	other := scratchGitRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	integrityGit(t, other, "worktree", "add", "-b", "automaton/other", wt, "main")
	requireIntegrityError(t, CheckProjectWorktree(wt, base), "different repository")
}

func TestCheckProjectWorktree_RejectsUnregisteredAfterRemove(t *testing.T) {
	base, wt := integrityRepo(t)
	integrityGit(t, base, "worktree", "remove", "--force", wt)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	requireIntegrityError(t, CheckProjectWorktree(wt, base), ".git")
}

func TestCheckProjectWorktree_RejectsMissingMarkerFile(t *testing.T) {
	isolateGitEnv(t)
	base := scratchGitRepo(t)
	if err := os.WriteFile(filepath.Join(base, "go.mod"), []byte("module scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	integrityGit(t, base, "add", "go.mod")
	integrityGit(t, base, "commit", "-m", "add go.mod")
	wt := filepath.Join(t.TempDir(), "wt")
	integrityGit(t, base, "worktree", "add", "-b", "automaton/test", wt, "main")
	if err := CheckProjectWorktree(wt, base); err != nil {
		t.Fatalf("intact worktree rejected: %v", err)
	}
	if err := os.Remove(filepath.Join(wt, "go.mod")); err != nil {
		t.Fatal(err)
	}
	requireIntegrityError(t, CheckProjectWorktree(wt, base), "go.mod")
}

func TestEnsureWorktree_RefusesFakeRepoOnReuse(t *testing.T) {
	isolateGitEnv(t)
	base := scratchGitRepo(t)
	worktreeDir := t.TempDir()
	prd := &PRD{ID: "5f440001", Title: "Fake repo"}
	prd.Git.Branch = "automaton/5f440001-fake-repo"

	path := filepath.Join(worktreeDir, prd.ID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	integrityGit(t, path, "init", "-b", "master")
	if err := os.WriteFile(filepath.Join(path, "CLAUDE.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := EnsureWorktree(prd, base, worktreeDir)
	if err == nil {
		t.Fatal("EnsureWorktree reused a standalone repository")
	}
	if !errors.Is(err, ErrWorktreeIntegrity) {
		t.Errorf("error does not satisfy errors.Is(ErrWorktreeIntegrity): %v", err)
	}
	if st, serr := os.Stat(filepath.Join(path, ".git")); serr != nil || !st.IsDir() {
		t.Errorf("standalone .git must be left untouched: %v", serr)
	}
	if _, serr := os.Stat(filepath.Join(path, "CLAUDE.md")); serr != nil {
		t.Errorf("session file must be left untouched: %v", serr)
	}
}
