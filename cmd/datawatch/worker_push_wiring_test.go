package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func hgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=w", "-c", "user.email=w@example.com"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The pull request hook's real pusher never moves the remote's default branch,
// whatever it is called.
func TestWorkerPRHookPusher_NeverMovesADefaultBranchOfAnyName(t *testing.T) {
	root := t.TempDir()
	bare, work := filepath.Join(root, "remote.git"), filepath.Join(root, "work")
	hgit(t, root, "init", "-q", "--bare", "-b", "trunk", bare)
	hgit(t, root, "init", "-q", "-b", "trunk", work)
	hgit(t, work, "commit", "-q", "--allow-empty", "-m", "seed")
	hgit(t, work, "remote", "add", "origin", bare)
	hgit(t, work, "push", "-q", "origin", "trunk")
	before := hgit(t, bare, "rev-parse", "trunk")
	hgit(t, work, "commit", "-q", "--allow-empty", "-m", "worker commit on the default branch")

	err := (projectGitPusher{}).PushBranch(work, "origin", "trunk", "", "")

	if got := hgit(t, bare, "rev-parse", "trunk"); got != before {
		t.Errorf("the default branch moved from %s to %s", before, got)
	}
	if err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Errorf("err = %v, want a default branch refusal", err)
	}
}

// A container worker's own sessions never push; its one push follows the
// parent's settings.
func TestSessionGitPushSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Git.PushOnComplete = "branch"
	if policy, _ := sessionGitPushSettings(cfg, true)(); policy != "never" {
		t.Errorf("worker sessions: policy %q, want never", policy)
	}
	if policy, _ := sessionGitPushSettings(cfg, false)(); policy != "branch" {
		t.Errorf("daemon sessions: policy %q, want the configured one", policy)
	}
}

// Wiring that no unit can reach without starting a daemon.
func TestPushWiringInMain(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for what, want := range map[string]string{
		"the daemon secret scan policy":           "gitpkg.SetDaemonScanPolicy(daemonSecretScan)",
		"the session push settings":               "mgr.SetGitPushSettings(sessionGitPushSettings(cfg, bootEnv.IsWorker()))",
		"the cluster spawn's push override":       `"push_on_complete": req.PushOnComplete,`,
		"the worker's push and its stated reason": `" (not pushed: " + perr.Error() + ")"`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.go no longer wires %s (%s)", what, want)
		}
	}
}
