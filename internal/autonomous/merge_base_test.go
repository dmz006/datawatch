package autonomous

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// mergeBaseGit runs git in dir for a test fixture and fails the test on error.
func mergeBaseGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestResolveMergeBaseUsesTheRepositorysOwnDefaultBranch(t *testing.T) {
	for _, name := range []string{"trunk", "master", "main", "develop"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			mergeBaseGit(t, dir, "init", "-q", "-b", name)
			mergeBaseGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
			if got := resolveMergeBase(context.Background(), NewMergeGitRunner(), dir); got != name {
				t.Fatalf("resolveMergeBase = %q, want %q", got, name)
			}
		})
	}
}

func TestResolveMergeBasePrefersTheRemoteDefaultBranch(t *testing.T) {
	remote := t.TempDir()
	mergeBaseGit(t, remote, "init", "-q", "-b", "trunk")
	mergeBaseGit(t, remote, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	clone := t.TempDir() + "/c"
	mergeBaseGit(t, t.TempDir(), "clone", "-q", remote, clone)
	// the clone's checked-out branch moves away; the remote HEAD still names trunk
	mergeBaseGit(t, clone, "checkout", "-q", "-b", "scratch")
	if got := resolveMergeBase(context.Background(), NewMergeGitRunner(), clone); got != "trunk" {
		t.Fatalf("resolveMergeBase = %q, want the remote default %q", got, "trunk")
	}
}

func TestResolveMergeBaseNeverReturnsAnAutomatonBranch(t *testing.T) {
	dir := t.TempDir()
	mergeBaseGit(t, dir, "init", "-q", "-b", automatonBranchPrefix+"work")
	if got := resolveMergeBase(context.Background(), NewMergeGitRunner(), dir); got != mergePendingFallbackBase {
		t.Fatalf("resolveMergeBase = %q, want the fallback %q", got, mergePendingFallbackBase)
	}
}

func TestAPIExposesCleanupStatus(t *testing.T) {
	// the server serves GET /api/autonomous/cleanup only when its manager
	// has this method; without it the route answers 503 in a running daemon
	var _ interface{ CleanupStatus() CleanupStatus } = (*API)(nil)
}
