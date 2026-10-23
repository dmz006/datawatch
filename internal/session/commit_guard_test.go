package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

func guardTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestSessionGuarded(t *testing.T) {
	if sessionGuarded(nil) {
		t.Fatal("nil session guarded")
	}
	if !sessionGuarded(&Session{PRDID: "p"}) {
		t.Fatal("task session not guarded")
	}
	if !sessionGuarded(&Session{OneShot: true, ValidateCommands: []string{"true"}}) {
		t.Fatal("validated one-shot not guarded")
	}
	if sessionGuarded(&Session{OneShot: true}) || sessionGuarded(&Session{}) {
		t.Fatal("plain session guarded")
	}
}

func TestProjectGitCommitEnvOnlyWhenSet(t *testing.T) {
	dir := guardTestRepo(t)
	g, err := gitguard.NewCommitGuard("host-guardtest")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Cleanup() }()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Worker-style commit under the guard env alone is refused.
	add := exec.Command("git", "add", "a.txt")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	c := exec.Command("git", "commit", "-q", "-m", "x")
	c.Dir = dir
	c.Env = append(os.Environ(), g.Env()...)
	if err := c.Run(); err == nil {
		t.Fatal("commit without token succeeded")
	}
	// The harness commit with the token goes through.
	pg := NewProjectGit(dir)
	pg.SetCommitEnv(g.CommitEnv())
	if err := pg.commit("-q", "-m", "ok"); err != nil {
		t.Fatalf("token commit: %v", err)
	}
}

func TestReleaseCommitGuardRemovesDirAndHooksPath(t *testing.T) {
	dir := guardTestRepo(t)
	m := &Manager{}
	sess := &Session{FullID: "host-rel1", ProjectDir: dir, PRDID: "p"}
	g, err := gitguard.NewCommitGuard(sess.FullID)
	if err != nil {
		t.Fatal(err)
	}
	m.commitGuards = map[string]*gitguard.CommitGuard{sess.FullID: g}
	c := exec.Command("git", "config", "--local", "core.hooksPath", g.Dir())
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	m.releaseCommitGuard(sess.FullID, dir)
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("guard dir remains: %v", err)
	}
	get := exec.Command("git", "config", "--local", "--get", "core.hooksPath")
	get.Dir = dir
	if err := get.Run(); err == nil {
		t.Fatal("hooksPath still set")
	}
	if m.commitGuardFor(sess.FullID) != nil {
		t.Fatal("guard still registered")
	}
}

func TestAuthorizeCommitUnguardedUnchanged(t *testing.T) {
	m := &Manager{}
	pg := NewProjectGit(t.TempDir())
	m.AuthorizeCommit("none", pg)
	if pg.commitEnv != nil {
		t.Fatal("unguarded session got a commit env")
	}
}

func TestAdoptCommitGuardsKeepsLiveRemovesStale(t *testing.T) {
	live, err := gitguard.NewCommitGuard("host-live1")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := gitguard.NewCommitGuard("host-stale1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = live.Cleanup(); _ = stale.Cleanup() }()
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{hostname: "host", store: st, tmux: NewFakeTmux()}
	sess := &Session{FullID: "host-live1", PRDID: "p", TmuxSession: "cs-live"}
	m.adoptCommitGuards([]*Session{sess})
	if _, err := os.Stat(stale.Dir()); !os.IsNotExist(err) {
		t.Fatal("stale guard dir kept")
	}
	g := m.commitGuardFor("host-live1")
	if g == nil || g.Dir() != live.Dir() || g.Token() != live.Token() {
		t.Fatalf("live guard not adopted: %+v", g)
	}
}
