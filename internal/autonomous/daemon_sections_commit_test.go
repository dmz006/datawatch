package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dscBaseRules is the operator's rules file as committed.
const dscBaseRules = "# Project\n\nBuild with go build ./... .\n"

// dscDaemonOnlyRules is the same file after the daemon injected its own
// sections at session start: no operator-written line changed.
func dscDaemonOnlyRules() string {
	return dscBaseRules +
		"\n# Memory & Knowledge (datawatch)\n\nUse the memory tools first.\n\n" +
		"<!-- rtk-instructions -->\nalways prefix with rtk\n<!-- /rtk-instructions -->\n"
}

// dscRealEditRules is the same file with an operator edit on top of the
// daemon's sections.
func dscRealEditRules() string {
	return dscBaseRules +
		"\nTest with go test -race ./... .\n" +
		"\n# Memory & Knowledge (datawatch)\n\nUse the memory tools first.\n\n" +
		"<!-- rtk-instructions -->\nalways prefix with rtk\n<!-- /rtk-instructions -->\n"
}

// dscSetup makes a repository with a committed rules file (CLAUDE.md) and a
// manager that shells out to git for its HEAD reads.
func dscSetup(t *testing.T) (string, *Manager) {
	t.Helper()
	dir := uwRepo(t)
	const name = "CLAUDE.md"
	uwWrite(t, dir, name, dscBaseRules)
	uwGit(t, dir, "add", name)
	uwGit(t, dir, "commit", "-q", "-m", "rules")
	return dir, &Manager{}
}

func TestDaemonSectionsCommitRulesFileOnlyDaemonDiffNotCommitted(t *testing.T) {
	dir, m := dscSetup(t)
	uwWrite(t, dir, "CLAUDE.md", dscDaemonOnlyRules())

	filtered := m.pathspecWithoutDaemonOnlyChanges(context.Background(), dir, []string{"CLAUDE.md"})
	if len(filtered) != 0 {
		t.Fatalf("pathspec = %v; a daemon-section-only diff must be left out", filtered)
	}

	// An empty pathspec skips the commit exactly as an empty list always has:
	// HEAD does not move and the file is not committed anywhere.
	if count := uwGit(t, dir, "rev-list", "--count", "HEAD"); count != "2" {
		t.Fatalf("HEAD must not move: count = %s, want 2", count)
	}
	// The worktree is never cleaned up: the daemon's own sections stay on disk
	// and the file is still an uncommitted modification. (uwGit trims the
	// output, so the porcelain " M" line arrives as "M".)
	if got := uwGit(t, dir, "status", "--porcelain"); got != "M CLAUDE.md" {
		t.Fatalf("status = %q; CLAUDE.md must still show as uncommitted and modified", got)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); err != nil || !strings.Contains(string(body), "rtk-instructions") {
		t.Fatalf("the daemon sections must remain in the working file (err %v)", err)
	}
}

func TestDaemonSectionsCommitRulesFileRealEditCommitted(t *testing.T) {
	dir, m := dscSetup(t)
	uwWrite(t, dir, "CLAUDE.md", dscRealEditRules())

	filtered := m.pathspecWithoutDaemonOnlyChanges(context.Background(), dir, []string{"CLAUDE.md"})
	if !sameSet(filtered, []string{"CLAUDE.md"}) {
		t.Fatalf("pathspec = %v; a real edit must stay in", filtered)
	}

	count := func() string { return uwGit(t, dir, "rev-list", "--count", "HEAD") }
	sha, err := DefaultTaskCommit(context.Background(), dir, filtered, "task: real edit")
	if err != nil {
		t.Fatal(err)
	}
	if count() != "3" {
		t.Fatalf("HEAD must advance by exactly one commit, count = %s", count())
	}
	if uwGit(t, dir, "rev-parse", "HEAD") != sha {
		t.Fatalf("commit sha %s is not HEAD", sha)
	}
	if got := uwHeadChanges(t, dir); !sameSet(got, []string{"M\tCLAUDE.md"}) {
		t.Fatalf("commit records %q, want M\\tCLAUDE.md", got)
	}
	if st := uwGit(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("status = %q; the edit must be committed", st)
	}
}

func TestDaemonSectionsCommitOtherChangedFilesStillCommit(t *testing.T) {
	dir, m := dscSetup(t)
	// The rules file carries only the daemon's sections; the task's real work
	// is an edit to base.txt and a new file.
	uwWrite(t, dir, "CLAUDE.md", dscDaemonOnlyRules())
	uwWrite(t, dir, "base.txt", "base\nedited by the task\n")
	uwWrite(t, dir, "new.txt", "new file from the task\n")

	filtered := m.pathspecWithoutDaemonOnlyChanges(context.Background(), dir, []string{"CLAUDE.md", "base.txt", "new.txt"})
	if !sameSet(filtered, []string{"base.txt", "new.txt"}) {
		t.Fatalf("pathspec = %v; the task's files stay, the daemon-only file goes", filtered)
	}

	count := func() string { return uwGit(t, dir, "rev-list", "--count", "HEAD") }
	_, err := DefaultTaskCommit(context.Background(), dir, filtered, "task: other files")
	if err != nil {
		t.Fatal(err)
	}
	if count() != "3" {
		t.Fatalf("HEAD must advance by exactly one commit, count = %s", count())
	}
	if got := uwHeadChanges(t, dir); !sameSet(got, []string{"M\tbase.txt", "A\tnew.txt"}) {
		t.Fatalf("commit records %q, want M\\tbase.txt and A\\tnew.txt", got)
	}
	// The rules file with only the daemon's sections is still uncommitted and
	// still modified in the worktree (uwGit trims the porcelain leading space).
	if st := uwGit(t, dir, "status", "--porcelain"); st != "M CLAUDE.md" {
		t.Fatalf("status = %q; CLAUDE.md must remain uncommitted and modified", st)
	}
}

func TestDaemonSectionsCommitFailsClosedKeepsPath(t *testing.T) {
	dir, m := dscSetup(t)
	ctx := context.Background()

	// A tracked file the working tree deleted has no working copy to compare:
	// the deletion must still be committed, so the path stays in.
	uwGit(t, dir, "rm", "-q", "--cached", "base.txt")
	if err := os.Remove(filepath.Join(dir, "base.txt")); err != nil {
		t.Fatal(err)
	}
	if got := m.pathspecWithoutDaemonOnlyChanges(ctx, dir, []string{"base.txt"}); !sameSet(got, []string{"base.txt"}) {
		t.Fatalf("pathspec = %v; a deletion must be kept", got)
	}

	// An untracked new file has no HEAD copy: it must stay in.
	uwWrite(t, dir, "fresh.txt", "fresh\n")
	if got := m.pathspecWithoutDaemonOnlyChanges(ctx, dir, []string{"fresh.txt"}); !sameSet(got, []string{"fresh.txt"}) {
		t.Fatalf("pathspec = %v; an untracked file must be kept", got)
	}

	// Unsafe path shapes are never compared, only kept.
	unsafe := []string{"/etc/passwd", "../escape.txt", "a/b\x00c"}
	if got := m.pathspecWithoutDaemonOnlyChanges(ctx, dir, unsafe); !sameSet(got, unsafe) {
		t.Fatalf("pathspec = %v; unsafe shapes must be kept as-is", got)
	}

	// A git failure reading the HEAD copy keeps the path too: inject a runner
	// that fails, against a real daemon-only file.
	uwWrite(t, dir, "CLAUDE.md", dscDaemonOnlyRules())
	failRun := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		return "", os.ErrPermission
	}}
	if got := failRun.pathspecWithoutDaemonOnlyChanges(ctx, dir, []string{"CLAUDE.md"}); !sameSet(got, []string{"CLAUDE.md"}) {
		t.Fatalf("pathspec = %v; a HEAD-read failure must keep the path", got)
	}
}
