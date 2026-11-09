package mergepipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pushGuardGit runs git in dir with a fixed identity and the operator's tmux
// variables removed, failing the test on error.
func pushGuardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=test", "-c", "user.email=test@example.invalid",
		"-c", "init.defaultBranch=main", "-c", "tag.gpgSign=false",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = scrubbedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFile writes name with content in dir and commits it.
func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pushGuardGit(t, dir, "add", name)
	pushGuardGit(t, dir, "commit", "-m", "add "+name)
}

// pushGuardFixture is a bare repository standing in for the remote, a
// project clone with the initial commit on main, and a second clone that
// lands commits on the remote's main from elsewhere.
type pushGuardFixture struct {
	bare  string
	local string
	other string
}

func newPushGuardFixture(t *testing.T) pushGuardFixture {
	t.Helper()
	// The production tag step runs git with whatever identity the host has;
	// a clean CI runner has none, so give the test process one.
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	root := t.TempDir()
	f := pushGuardFixture{
		bare:  filepath.Join(root, "remote.git"),
		local: filepath.Join(root, "local"),
		other: filepath.Join(root, "other"),
	}
	pushGuardGit(t, root, "init", "--bare", "--initial-branch=main", f.bare)
	pushGuardGit(t, root, "init", "--initial-branch=main", f.local)
	commitFile(t, f.local, "a.txt", "one\n")
	pushGuardGit(t, f.local, "remote", "add", "origin", f.bare)
	pushGuardGit(t, f.local, "push", "origin", "main")
	pushGuardGit(t, root, "clone", "--quiet", f.bare, f.other)
	return f
}

// remoteMain returns the commit the bare remote's main points at.
func (f pushGuardFixture) remoteMain(t *testing.T) string {
	t.Helper()
	return pushGuardGit(t, f.bare, "rev-parse", "refs/heads/main")
}

func TestPushGuardPushesWhenRemoteIsBehind(t *testing.T) {
	f := newPushGuardFixture(t)
	commitFile(t, f.local, "b.txt", "two\n")
	head := pushGuardGit(t, f.local, "rev-parse", "HEAD")
	if err := (ExecShipRunner{}).Tag(context.Background(), f.local, "v0.0.1", "Release v0.0.1"); err != nil {
		t.Fatal(err)
	}

	if err := (ExecShipRunner{}).Push(context.Background(), f.local, "main", "v0.0.1"); err != nil {
		t.Fatalf("push when remote is behind: %v", err)
	}
	if got := f.remoteMain(t); got != head {
		t.Fatalf("remote main = %s, want local HEAD %s", got, head)
	}
}

func TestPushGuardRefusesWhenMainMoved(t *testing.T) {
	f := newPushGuardFixture(t)
	commitFile(t, f.local, "b.txt", "local two\n")
	commitFile(t, f.other, "c.txt", "remote two\n")
	pushGuardGit(t, f.other, "push", "origin", "main")
	remoteBefore := f.remoteMain(t)
	localHead := pushGuardGit(t, f.local, "rev-parse", "HEAD")

	err := (ExecShipRunner{}).Push(context.Background(), f.local, "main", "")
	if !errors.Is(err, ErrMainMoved) {
		t.Fatalf("push after remote main moved: err = %v, want ErrMainMoved", err)
	}
	if !strings.Contains(err.Error(), remoteBefore) || !strings.Contains(err.Error(), localHead) {
		t.Fatalf("error %q does not name both heads %s and %s", err, remoteBefore, localHead)
	}
	if got := f.remoteMain(t); got != remoteBefore {
		t.Fatalf("remote main changed to %s, want it left at %s", got, remoteBefore)
	}
}

func TestPushGuardSecondPushLeavesRemoteUnchanged(t *testing.T) {
	f := newPushGuardFixture(t)
	commitFile(t, f.local, "b.txt", "two\n")
	if err := (ExecShipRunner{}).Tag(context.Background(), f.local, "v0.0.2", "Release v0.0.2"); err != nil {
		t.Fatal(err)
	}
	if err := (ExecShipRunner{}).Push(context.Background(), f.local, "main", "v0.0.2"); err != nil {
		t.Fatalf("first push: %v", err)
	}
	afterFirst := f.remoteMain(t)

	if err := (ExecShipRunner{}).Push(context.Background(), f.local, "main", "v0.0.2"); err != nil {
		t.Fatalf("second push: %v", err)
	}
	if got := f.remoteMain(t); got != afterFirst {
		t.Fatalf("second push moved remote main from %s to %s", afterFirst, got)
	}
}

func TestPushGuardSkipsWhenProjectHasNoRemote(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "solo")
	pushGuardGit(t, root, "init", "--initial-branch=main", local)
	commitFile(t, local, "a.txt", "one\n")

	err := (ExecShipRunner{}).Push(context.Background(), local, "main", "")
	if err == nil {
		t.Fatal("push with no remote succeeded; want the plain git push failure as before")
	}
	if errors.Is(err, ErrMainMoved) {
		t.Fatalf("guard reported ErrMainMoved with no remote: %v", err)
	}
}
