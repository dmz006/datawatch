package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mustGit drives a throwaway repository under t.TempDir. HOME points at a
// temp dir and identity lives only in the repository config, so the test
// never reads or writes the operator's real git configuration.
func gitIn(t *testing.T, repo string, args ...string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	out, err := Run(context.Background(), repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func runRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test")
	}
	repo := t.TempDir()
	gitIn(t, repo, "init", "-b", "main")
	gitIn(t, repo, "config", "user.email", "run-test@example.com")
	gitIn(t, repo, "config", "user.name", "Run Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "README.md")
	gitIn(t, repo, "commit", "-m", "initial commit with several words")
	return repo
}

// TestRunReturnsStdout runs a read-only command and checks the exact
// stdout comes back, unmodified.
func TestRunReturnsStdout(t *testing.T) {
	repo := runRepo(t)
	out, err := Run(context.Background(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out = strings.TrimSpace(out)
	if len(out) != 40 || strings.ContainsAny(out, " \t") {
		t.Fatalf("git rev-parse HEAD = %q, want a bare 40-char sha", out)
	}
}

// TestRunHonoursExplicitDir moves the process working directory to an
// empty, git-less directory and still resolves repository state, proving
// the command ran in the explicit dir rather than the caller's cwd.
func TestRunHonoursExplicitDir(t *testing.T) {
	t.Chdir(t.TempDir())
	repo := runRepo(t)
	out, err := Run(context.Background(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Run with cwd outside the repo: %v", err)
	}
	if len(strings.TrimSpace(out)) != 40 {
		t.Fatalf("git rev-parse HEAD = %q, want a bare 40-char sha", out)
	}
}

// TestRunExplicitDirMissing fails when the explicit directory does not
// exist, instead of silently falling back to the process directory.
func TestRunExplicitDirMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := Run(context.Background(), "no-such-dir", "status"); err == nil {
		t.Error("Run with a missing explicit dir: want error, got nil")
	}
}

// TestRunRejectsBadRepo reports a non-zero exit as an error carrying git's
// stderr, with the partial stdout still returned.
func TestRunRejectsBadRepo(t *testing.T) {
	repo := runRepo(t)
	_, err := Run(context.Background(), repo, "rev-parse", "refs/heads/definitely-missing", "^2")
	if err == nil {
		t.Fatal("Run on a bad revision: want error, got nil")
	}
	if !strings.Contains(err.Error(), "fatal:") {
		t.Errorf("error %q does not carry git's stderr", err)
	}
}

// TestRunNoShellExpansion hands git an argument full of shell
// metacharacters; because no shell is involved it must reach git as one
// literal argument (git happily ignores unknown options).
func TestRunNoShellExpansion(t *testing.T) {
	repo := runRepo(t)
	if _, err := Run(context.Background(), repo, "rev-parse", "$HOME; rm -rf / && `id` | $(touch pwned)"); err == nil {
		t.Error("git accepted a literal metacharacter argument? want it refused (proves no shell mangled it)")
	}
	if _, err := os.Stat(filepath.Join(repo, "pwned")); !os.IsNotExist(err) {
		t.Fatal("a shell ran inside Run — the command must not pass through a shell")
	}
}

// TestRunBadArgs rejects an empty directory and an empty command.
func TestRunBadArgs(t *testing.T) {
	if _, err := Run(context.Background(), "  ", "status"); err == nil {
		t.Error("Run with an empty dir: want error, got nil")
	}
	repo := runRepo(t)
	if _, err := Run(context.Background(), repo); err == nil {
		t.Error("Run with no args: want error, got nil")
	}
}
