package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stagingDaemonMCPJSON = `{"mcpServers":{"datawatch":{"command":"bridge","args":[],"env":{"DATAWATCH_API_URL":"http://127.0.0.1:1","DATAWATCH_TOKEN":"live-session-token"}}}}`

func stagingMCPRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		stagingMCPGit(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stagingMCPGit(t, dir, "add", "README")
	stagingMCPGit(t, dir, "commit", "-qm", "base")
	for rel, body := range map[string]string{".mcp.json": stagingDaemonMCPJSON, "work.txt": "work\n"} {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func stagingMCPGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// The task commit never takes a .mcp.json holding a daemon entry, even when
// the task lists it among its files.
func TestDefaultTaskCommitSkipsMCPFileWithDaemonEntry(t *testing.T) {
	dir := stagingMCPRepo(t)
	if _, err := DefaultTaskCommit(context.Background(), dir, []string{".mcp.json", "work.txt"}, "task work"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stagingMCPGit(t, dir, "log", "-p", "--all"), "live-session-token") {
		t.Fatal("the token was committed")
	}
	if !strings.Contains(stagingMCPGit(t, dir, "show", "--name-only", "--format=", "HEAD"), "work.txt") {
		t.Fatal("the task's work was not committed")
	}
}

// The attempt snapshot ref never holds a .mcp.json with a daemon entry.
func TestSnapshotCandidatesDropMCPFileWithDaemonEntry(t *testing.T) {
	dir := stagingMCPRepo(t)
	got, err := snapshotCandidates(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "work.txt" {
		t.Fatalf("snapshot candidates = %v, want only work.txt", got)
	}
}
