package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const daemonEntryMCPJSON = `{"mcpServers":{"datawatch":{"command":"bridge","args":[],"env":{"DATAWATCH_API_URL":"http://127.0.0.1:1","DATAWATCH_TOKEN":"live-session-token"}}}}`

func mcpGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		mcpGit(t, dir, args...)
	}
	mcpWrite(t, dir, "README", "base\n")
	mcpGit(t, dir, "add", "README")
	mcpGit(t, dir, "commit", "-qm", "base")
	return dir
}

func mcpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func mcpWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An automatic commit never takes a .mcp.json holding a daemon entry, with
// no .gitignore to help: untracked at the root or in a subfolder, tracked
// and modified, or already staged by the worker.
func TestAutomaticCommitNeverTakesMCPFileWithDaemonEntry(t *testing.T) {
	for _, tc := range []struct {
		name, rel                 string
		tracked, preStaged, scope bool
	}{
		{name: "untracked unscoped", rel: ".mcp.json"},
		{name: "untracked scoped", rel: ".mcp.json", scope: true},
		{name: "subfolder", rel: "sub/.mcp.json", scope: true},
		{name: "tracked and modified", rel: ".mcp.json", tracked: true, scope: true},
		{name: "staged by the worker", rel: ".mcp.json", preStaged: true, scope: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := mcpGitRepo(t)
			if tc.tracked {
				mcpWrite(t, dir, tc.rel, `{"mcpServers":{}}`)
				mcpGit(t, dir, "add", tc.rel)
				mcpGit(t, dir, "commit", "-qm", "track")
			}
			mcpWrite(t, dir, tc.rel, daemonEntryMCPJSON)
			if tc.preStaged {
				mcpGit(t, dir, "add", tc.rel)
			}
			mcpWrite(t, dir, "work.txt", "the task's work\n")
			var scope []string
			if tc.scope {
				scope = []string{dir}
			}
			g := NewProjectGit(dir)
			if err := g.PostSessionCommit("s1", "task", StateComplete, scope); err != nil {
				t.Fatalf("commit: %v", err)
			}
			if log := mcpGit(t, dir, "log", "-p", "--all"); strings.Contains(log, "live-session-token") {
				t.Fatalf("the token was committed")
			}
			if !strings.Contains(mcpGit(t, dir, "show", "--name-only", "--format=", "HEAD"), "work.txt") {
				t.Fatalf("the task's own work was not committed")
			}
			if raw, err := os.ReadFile(filepath.Join(dir, tc.rel)); err != nil || string(raw) != daemonEntryMCPJSON {
				t.Fatalf("working file changed or removed: %v", err)
			}
		})
	}
}

// A project's own .mcp.json with no token is ordinary work.
func TestAutomaticCommitKeepsProjectOwnMCPFile(t *testing.T) {
	dir := mcpGitRepo(t)
	mcpWrite(t, dir, ".mcp.json", `{"mcpServers":{"tool":{"command":"/bin/tool","args":[],"env":{"DATAWATCH_TOKEN":"${DATAWATCH_TOKEN}"}}}}`)
	if err := NewProjectGit(dir).PostSessionCommit("s1", "task", StateComplete, []string{dir}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !strings.Contains(mcpGit(t, dir, "show", "--name-only", "--format=", "HEAD"), ".mcp.json") {
		t.Fatal("a .mcp.json without a token was not committed")
	}
}
