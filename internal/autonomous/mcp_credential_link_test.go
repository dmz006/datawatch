package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// linkedMCPRepo is a repository with a tracked sub/.mcp.json, and a folder
// outside it whose .mcp.json holds a daemon entry. swap replaces sub with a
// link to that folder, as a worker could while the daemon is at work.
func linkedMCPRepo(t *testing.T) (repo string, swap func()) {
	t.Helper()
	repo = snapTestRepo(t)
	outside := t.TempDir()
	for _, p := range []string{filepath.Join(repo, "sub"), outside} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", ".mcp.json"), []byte(`{"mcpServers":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapTestGit(t, repo, "add", "-f", "sub/.mcp.json")
	snapTestGit(t, repo, "commit", "-q", "-m", "project mcp file")
	if err := os.WriteFile(filepath.Join(outside, ".mcp.json"), []byte(stagingDaemonMCPJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, func() {
		if err := os.RemoveAll(filepath.Join(repo, "sub")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(repo, "sub")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
}

// The parent folder is swapped after the paths were listed and before they are
// checked: the task commit's daemon-token check does not read outside.
func TestClassifyTaskCommitPaths_ParentSwappedForLinkNotRead(t *testing.T) {
	repo, swap := linkedMCPRepo(t)
	listed := []string{"sub/.mcp.json"}
	swap()
	add, _, skipped := classifyTaskCommitPaths(context.Background(), repo, listed)
	if slices.Contains(skipped, "sub/.mcp.json") || !slices.Contains(add, "sub/.mcp.json") {
		t.Fatalf("the file outside the repository decided the answer: add=%v skipped=%v", add, skipped)
	}
}

// git lists a tracked file under a folder that became a link; the attempt
// snapshot's daemon-token check does not read through it.
func TestSnapshotCandidates_LinkedParentNotRead(t *testing.T) {
	repo, swap := linkedMCPRepo(t)
	swap()
	got, err := snapshotCandidates(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "sub/.mcp.json") {
		t.Fatalf("the file outside the repository decided the answer: candidates=%v", got)
	}
	// The staging guard that runs next still keeps it out of the snapshot.
	if kept, _ := gitguard.Default.Keep(repo, got); slices.Contains(kept, "sub/.mcp.json") {
		t.Fatalf("the staging guard kept the path behind the link: %v", kept)
	}
}

// The task commit cannot take the file behind the link either: git refuses it,
// and no commit holds the token from outside the repository.
func TestDefaultTaskCommit_LinkedParentNeverCommitsOutsideToken(t *testing.T) {
	repo, swap := linkedMCPRepo(t)
	swap()
	_, err := DefaultTaskCommit(context.Background(), repo, []string{"sub/.mcp.json"}, "task work")
	t.Logf("commit result: %v", err)
	if log := snapTestGit(t, repo, "log", "-p", "--all"); strings.Contains(log, "live-session-token") ||
		strings.Contains(log, "DATAWATCH_TOKEN") {
		t.Fatal("the token from outside the repository was committed")
	}
}
