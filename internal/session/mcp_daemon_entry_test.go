package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const plainMCPJSON = `{"mcpServers":{"tool":{"command":"/bin/tool"}}}`

// A parent folder of a tracked .mcp.json swapped for a link that leaves the
// repository: git still lists the tracked path, and the daemon-token check must
// not read through the link. What the file outside holds cannot change the
// answer.
func TestClassifyCandidates_LinkedParentOutsideRepoNotRead(t *testing.T) {
	for name, link := range map[string]func(repo, outside string) (string, error){
		"absolute link": func(_, outside string) (string, error) { return outside, nil },
		"relative link": func(repo, outside string) (string, error) { return filepath.Rel(repo, outside) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := mcpGitRepo(t)
			outside := t.TempDir()
			mcpWrite(t, dir, "sub/.mcp.json", plainMCPJSON)
			mcpGit(t, dir, "add", "sub/.mcp.json")
			mcpGit(t, dir, "commit", "-qm", "project mcp file")
			mcpWrite(t, outside, ".mcp.json", daemonEntryMCPJSON)
			if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
				t.Fatal(err)
			}
			target, err := link(dir, outside)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, "sub")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			_, skipped := NewProjectGit(dir).classifyCandidates([]string{"sub/.mcp.json"})
			for _, s := range skipped {
				if s.Reason == "daemon-token" {
					t.Fatalf("the file outside the repository was read: %+v", s)
				}
			}
		})
	}
}

// A link that stays inside the repository is still followed, and a linked
// .mcp.json itself is still left alone: git stores only its target path.
func TestClassifyCandidates_LinksInsideRepo(t *testing.T) {
	dir := mcpGitRepo(t)
	mcpWrite(t, dir, "real/.mcp.json", daemonEntryMCPJSON)
	if err := os.Symlink("real", filepath.Join(dir, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real/.mcp.json", filepath.Join(dir, "linked", ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	kept, skipped := NewProjectGit(dir).classifyCandidates([]string{"real/.mcp.json", "alias/.mcp.json", "linked/.mcp.json"})
	if len(kept) != 1 || kept[0] != "linked/.mcp.json" {
		t.Fatalf("kept = %v, want only linked/.mcp.json", kept)
	}
	if len(skipped) != 2 || skipped[0].Reason != "daemon-token" || skipped[1].Reason != "daemon-token" {
		t.Fatalf("skipped = %+v, want two daemon-token skips", skipped)
	}
}

// An empty directory means the working directory, as it does for git.
func TestIsMCPFileWithDaemonEntry_EmptyDirIsWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	mcpWrite(t, dir, ".mcp.json", daemonEntryMCPJSON)
	t.Chdir(dir)
	if !IsMCPFileWithDaemonEntry("", ".mcp.json") {
		t.Fatal("a daemon entry in the working directory was not seen")
	}
}

// Whatever the check now lets through, git adds nothing through a linked
// parent folder: no commit ever holds the token from outside the repository.
func TestPostSessionCommit_LinkedParentNeverCommitsOutsideToken(t *testing.T) {
	for name, scoped := range map[string]bool{"scoped": true, "unscoped": false} {
		t.Run(name, func(t *testing.T) {
			dir := mcpGitRepo(t)
			outside := t.TempDir()
			mcpWrite(t, dir, "sub/.mcp.json", plainMCPJSON)
			mcpGit(t, dir, "add", "sub/.mcp.json")
			mcpGit(t, dir, "commit", "-qm", "project mcp file")
			mcpWrite(t, outside, ".mcp.json", daemonEntryMCPJSON)
			if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "sub")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			mcpWrite(t, dir, "work.txt", "the task's work\n")
			var scope []string
			if scoped {
				scope = []string{dir}
			}
			err := NewProjectGit(dir).PostSessionCommit("s1", "task", StateComplete, scope)
			t.Logf("commit result: %v; head files: %q", err, mcpGit(t, dir, "show", "--name-status", "--format=%s", "HEAD"))
			if log := mcpGit(t, dir, "log", "-p", "--all"); strings.Contains(log, "live-session-token") {
				t.Fatal("the token from outside the repository was committed")
			}
		})
	}
}
