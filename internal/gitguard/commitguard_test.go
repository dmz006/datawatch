package gitguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func guardGit(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func guardRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := guardGit(t, dir, nil, a...); err != nil {
			t.Fatalf("git %v: %v: %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := guardGit(t, dir, nil, "add", "f.txt"); err != nil {
		t.Fatalf("add: %v: %s", err, out)
	}
	return dir
}

func TestCommitGuardRefusesWithoutToken(t *testing.T) {
	g, err := NewCommitGuard("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	repo := guardRepo(t)
	out, err := guardGit(t, repo, g.Env(), "commit", "-m", "x")
	if err == nil {
		t.Fatalf("commit without token succeeded: %s", out)
	}
	if !strings.Contains(out, "harness commits after validation") {
		t.Fatalf("missing refusal message: %s", out)
	}
	if strings.HasPrefix(g.Dir(), repo) {
		t.Fatal("guard dir inside repo")
	}
}

func TestCommitGuardAllowsCommitEnv(t *testing.T) {
	g, err := NewCommitGuard("sess-2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	repo := guardRepo(t)
	if out, err := guardGit(t, repo, g.CommitEnv(), "commit", "-m", "x"); err != nil {
		t.Fatalf("commit with token failed: %v: %s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".git", "config")); strings.Contains(string(b), "hooksPath") {
		t.Fatal("hooksPath written to repo config")
	}
}

func TestCommitGuardNoVerifyStillRefused(t *testing.T) {
	g, err := NewCommitGuard("sess-3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	repo := guardRepo(t)
	if out, err := guardGit(t, repo, g.Env(), "commit", "--no-verify", "-m", "x"); err == nil {
		t.Fatalf("--no-verify commit succeeded: %s", out)
	}
	if _, err := guardGit(t, repo, nil, "rev-parse", "--verify", "HEAD"); err == nil {
		t.Fatal("HEAD exists after refused commit")
	}
}

func TestCommitGuardCleanupIdempotent(t *testing.T) {
	g, err := NewCommitGuard("sess-4")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("dir still present: %v", err)
	}
	if err := g.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestCommitGuardScrubHooksPath(t *testing.T) {
	repo := guardRepo(t)
	prefix := filepath.Join(os.TempDir(), guardDirPrefix)

	other := filepath.Join(t.TempDir(), "hooks")
	if out, err := guardGit(t, repo, nil, "config", "--local", "core.hooksPath", other); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := ScrubHooksPath(repo, prefix); err != nil {
		t.Fatal(err)
	}
	if out, _ := guardGit(t, repo, nil, "config", "--local", "--get", "core.hooksPath"); strings.TrimSpace(out) != other {
		t.Fatalf("foreign value was touched: %q", out)
	}

	if out, err := guardGit(t, repo, nil, "config", "--local", "core.hooksPath", prefix+"abc-123"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := ScrubHooksPath(repo, prefix); err != nil {
		t.Fatal(err)
	}
	if out, err := guardGit(t, repo, nil, "config", "--local", "--get", "core.hooksPath"); err == nil {
		t.Fatalf("guard value not removed: %q", out)
	}
	if err := ScrubHooksPath(repo, prefix); err != nil {
		t.Fatalf("scrub with key unset: %v", err)
	}
}

func TestCommitGuardReapStaleDirs(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	live, err := NewCommitGuard("live-sess")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Cleanup() })
	stale, err := NewCommitGuard("stale-sess")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stale.Cleanup() })
	ReapStaleGuardDirs(map[string]bool{"live-sess": true})
	if _, err := os.Stat(live.Dir()); err != nil {
		t.Fatalf("live dir reaped: %v", err)
	}
	if _, err := os.Stat(stale.Dir()); !os.IsNotExist(err) {
		t.Fatalf("stale dir kept: %v", err)
	}
}
