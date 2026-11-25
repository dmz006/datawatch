// Tests for the git inputs that come with a request: the branch name of a reap
// restore, the arguments the task-worktree git wrapper accepts, and the reflog
// read of the id check.
package autonomous

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// restoreFakeGit answers every backup lookup, so only the name check can stop
// a restore from reaching `git branch`.
type restoreFakeGit struct{ calls [][]string }

func (g *restoreFakeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	g.calls = append(g.calls, append([]string(nil), args...))
	switch args[0] {
	case "for-each-ref":
		return args[len(args)-1] + "1760000000\n", nil
	case "rev-parse":
		return "deadbeef\n", nil
	}
	return "", nil
}

func (g *restoreFakeGit) ranBranch() bool {
	for _, c := range g.calls {
		if c[0] == "branch" {
			return true
		}
	}
	return false
}

type restoreFn func(context.Context, MergeGitRunner, string, string) (string, string, error)

func restoreFns() map[string]restoreFn {
	return map[string]restoreFn{"reap": RestoreReapedDetail, "remote reap": RestoreRemoteReapedDetail}
}

func TestReapRestoreRefusesNamesThatAreNotBranches(t *testing.T) {
	for kind, restore := range restoreFns() {
		for _, branch := range []string{
			"-D", "--force", "-m", "--delete", "a b", "a..b", "a~1", "a^", "a:b", "a?b", "a*b", "a[b",
			`a\b`, "a\nb", "a\x00b", "a\x7fb", "a@{1}", "@", "HEAD", "/a", "a/", "a//b", "a.", "a.lock",
			"a/.b", ".a", "a.lock/b",
		} {
			git := &restoreFakeGit{}
			_, _, err := restore(context.Background(), git, "/repo", branch)
			if !errors.Is(err, ErrBadRestoreBranch) {
				t.Errorf("%s %q: err = %v, want ErrBadRestoreBranch", kind, branch, err)
			}
			if git.ranBranch() {
				t.Errorf("%s %q: git branch ran: %q", kind, branch, git.calls)
			}
		}
	}
}

func TestReapRestoreAcceptsRealBranchNames(t *testing.T) {
	for kind, restore := range restoreFns() {
		for _, branch := range []string{
			"automaton/0123abcd", "automaton/prd-1--tasks-fix-t1", "feature/x_y.z", "release/1.0",
			"fix#12", "user@host", "a+b=c,d", "café/ünïcode", "v1.2.3-rc.1", "x",
		} {
			git := &restoreFakeGit{}
			if _, _, err := restore(context.Background(), git, "/repo", branch); err != nil || !git.ranBranch() {
				t.Errorf("%s %q: err = %v, calls %q", kind, branch, err, git.calls)
			}
		}
	}
}

// The wrapper runs only the git commands the task-worktree code uses. A caller
// cannot choose another command or put an option in front of it.
func TestTaskGitOutRunsOnlyItsCommands(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	ctx := context.Background()
	for _, args := range [][]string{
		nil,
		{"config", "user.name", "changed"},
		{"clone", "--upload-pack=x", "src"},
		{"--exec-path=/tmp", "status"},
		{"--host", "tcp://x", "info"},
		{"-c", "core.pager=cat", "status"},
		{"-c", showUntrackedOverride},
		{"-c", showUntrackedOverride, "config", "a.b", "c"},
		{"status", "a\x00b"},
	} {
		if _, err := taskGitOut(ctx, repo, args...); !errors.Is(err, errTaskGitArgs) {
			t.Errorf("args %q: err = %v, want errTaskGitArgs", args, err)
		}
	}
	for _, args := range [][]string{
		{"status", "--porcelain=v1"},
		{"worktree", "list", "--porcelain"},
		{"-c", showUntrackedOverride, "worktree", "list"},
		{"ls-files", "--others", "-z", "--", ".datawatch"},
	} {
		if _, err := taskGitOut(ctx, repo, args...); err != nil {
			t.Errorf("args %q: %v", args, err)
		}
	}
}

// A reflog that is a link leaving the git directory is not read.
func TestReflogSinceStaysInsideGitDir(t *testing.T) {
	gitDir, outside := t.TempDir(), t.TempDir()
	line := strings.Repeat("0", 40) + " " + strings.Repeat("1", 40) + " a <a@b> 1700000000 +0000\tbranch: Created\n"
	other := filepath.Join(outside, "log")
	if err := os.WriteFile(other, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	const ref = "refs/heads/automaton/abc"
	logPath := filepath.Join(gitDir, "logs", filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkTo(t, filepath.Dir(logPath), "abc", other)
	if got := reflogSince(gitDir, ref); !got.IsZero() {
		t.Fatalf("read a reflog outside the git directory: %v", got)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := reflogSince(gitDir, ref); !got.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("reflog time = %v", got)
	}
}

// A listed repository that is gone, or is a file, is skipped without a log
// line; a folder that is a repository is still scanned.
func TestScanIDLeftoversSkipsMissingRepoQuietly(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x")
	gitRun(t, repo, "branch", "automaton/abc123")
	file := filepath.Join(repo, "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logged := captureLog(t)
	left := scanIDLeftovers(idRoots{repos: []string{filepath.Join(repo, "gone"), file, filepath.Join(file, "under"), repo}})
	if !left.holds("abc123") || len(left) != 1 {
		t.Fatalf("leftovers = %+v", left)
	}
	if out := logged(); out != "" {
		t.Fatalf("logged: %q", out)
	}
}

// captureLog collects the standard logger's output until the test ends and
// returns a reader of what was logged.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf.String
}
