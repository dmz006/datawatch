package session

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// commitGuardDirty writes a new file into dir so the worktree has work to commit.
func commitGuardDirty(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// commitGuardHeadCount returns the number of commits reachable from HEAD, or 0
// for a repository with no commit yet.
func commitGuardHeadCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range strings.TrimSpace(string(out)) {
		n = n*10 + int(r-'0')
	}
	return n
}

// commitGuardHooksPath returns the local core.hooksPath of dir, "" when unset.
func commitGuardHooksPath(dir string) string {
	out, _ := exec.Command("git", "-C", dir, "config", "--local", "--get", "core.hooksPath").Output()
	return strings.TrimSpace(string(out))
}

// commitGuardSetHooksPath writes core.hooksPath into the local config of dir.
func commitGuardSetHooksPath(t *testing.T, dir, path string) {
	t.Helper()
	c := exec.Command("git", "-C", dir, "config", "--local", "core.hooksPath", path)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("set hooksPath: %v %s", err, out)
	}
}

// commitGuardCaptureStdout runs fn and returns what it printed to stdout.
func commitGuardCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	return <-done
}

// commitGuardOneShotManager returns a manager with a guard registered for a
// validated one-shot session working in dir, plus the session and its guard.
func commitGuardOneShotManager(t *testing.T, dir string, validate []string) (*Manager, *Session, *gitguard.CommitGuard) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{store: store, hostname: "testhost"}
	sess := &Session{
		ID: "cg01", FullID: "testhost-cg01", Hostname: "testhost",
		State: StateRunning, OneShot: true, Git: true, ValidateCommands: validate,
		ProjectDir: dir, Task: "guarded work",
		CreatedAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now(),
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	g, err := gitguard.NewCommitGuard(sess.FullID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	m.commitGuards = map[string]*gitguard.CommitGuard{sess.FullID: g}
	return m, sess, g
}

func TestCommitGuardWorkerCommitRejectedWithHarnessMessage(t *testing.T) {
	dir := guardTestRepo(t)
	g, err := gitguard.NewCommitGuard("host-cgmsg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Cleanup() }()
	commitGuardDirty(t, dir, "w.txt")
	add := exec.Command("git", "add", "w.txt")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	c := exec.Command("git", "commit", "-q", "-m", "worker")
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	c.Env = append(c.Env, g.Env()...)
	out, err := c.CombinedOutput()
	if err == nil {
		t.Fatal("worker commit under the guard env succeeded")
	}
	if !strings.Contains(string(out), "the harness commits after validation") {
		t.Fatalf("rejection does not say the harness commits after validation: %s", out)
	}
	if n := commitGuardHeadCount(t, dir); n != 0 {
		t.Fatalf("worker commit created %d commits", n)
	}
}

func TestCommitGuardCompletionCommitAfterPassingValidation(t *testing.T) {
	dir := guardTestRepo(t)
	m, sess, g := commitGuardOneShotManager(t, dir, []string{"true"})
	commitGuardDirty(t, dir, "work.txt")
	pg := NewProjectGit(dir)

	m.commitOnCompletion(sess, pg)

	if sess.ValidationResult != "passed" {
		t.Fatalf("ValidationResult = %q", sess.ValidationResult)
	}
	if n := commitGuardHeadCount(t, dir); n != 1 {
		t.Fatalf("commit count = %d, want 1", n)
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if sess.CommitSHA == "" || sess.CommitSHA != strings.TrimSpace(string(head)) {
		t.Fatalf("CommitSHA = %q, HEAD = %q", sess.CommitSHA, head)
	}
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("guard dir kept after the completion commit: %v", err)
	}
	if p := commitGuardHooksPath(dir); p != "" {
		t.Fatalf("worktree config has hooksPath %q", p)
	}
}

func TestCommitGuardFailingValidationLeavesNoCommit(t *testing.T) {
	dir := guardTestRepo(t)
	m, sess, g := commitGuardOneShotManager(t, dir, []string{"false"})
	commitGuardDirty(t, dir, "work.txt")
	pg := NewProjectGit(dir)

	m.commitOnCompletion(sess, pg)

	if !strings.HasPrefix(sess.ValidationResult, "failed") {
		t.Fatalf("ValidationResult = %q", sess.ValidationResult)
	}
	if n := commitGuardHeadCount(t, dir); n != 0 {
		t.Fatalf("failed validation left %d commits", n)
	}
	if sess.CommitSHA != "" {
		t.Fatalf("CommitSHA = %q", sess.CommitSHA)
	}
	if pg.commitEnv != nil {
		t.Fatalf("commit token applied despite failed validation: %v", pg.commitEnv)
	}
	if m.commitGuardFor(sess.FullID) != g {
		t.Fatal("guard was released before the session ended")
	}
	// The token still blocks a commit made without it.
	add := exec.Command("git", "add", "work.txt")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	c := exec.Command("git", "commit", "-q", "-m", "late")
	c.Dir = dir
	c.Env = append(os.Environ(), g.Env()...)
	if err := c.Run(); err == nil {
		t.Fatal("commit without the token succeeded after failed validation")
	}
}

func TestCommitGuardCleanupOnSessionEnd(t *testing.T) {
	dir := guardTestRepo(t)
	m, sess, g := commitGuardOneShotManager(t, dir, []string{"true"})
	commitGuardSetHooksPath(t, dir, g.Dir())

	m.releaseCommitGuard(sess.FullID, dir)

	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("guard dir remains: %v", err)
	}
	if p := commitGuardHooksPath(dir); p != "" {
		t.Fatalf("hooksPath remains: %q", p)
	}
	// A repeated release is harmless.
	m.releaseCommitGuard(sess.FullID, dir)
}

func TestCommitGuardAdoptResumeMonitorsReapsStaleKeepsLive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	mgr, fake := newTestManagerWithFake(t)

	deadDir := guardTestRepo(t)
	liveDir := guardTestRepo(t)
	deadGuard, err := gitguard.NewCommitGuard("testhost-dead1")
	if err != nil {
		t.Fatal(err)
	}
	liveGuard, err := gitguard.NewCommitGuard("testhost-live1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deadGuard.Cleanup(); _ = liveGuard.Cleanup() }()
	commitGuardSetHooksPath(t, deadDir, deadGuard.Dir())

	_ = mgr.SaveSession(&Session{
		ID: "dead1", FullID: "testhost-dead1", TmuxSession: "cs-dead1",
		Hostname: "testhost", State: StateRunning, PRDID: "p", UpdatedAt: time.Now(),
		ProjectDir: deadDir, LogFile: filepath.Join(t.TempDir(), "dead1.log"),
	})
	_ = mgr.SaveSession(&Session{
		ID: "live1", FullID: "testhost-live1", TmuxSession: "cs-live1",
		Hostname: "testhost", State: StateRunning, PRDID: "p", UpdatedAt: time.Now(),
		ProjectDir: liveDir, LogFile: filepath.Join(t.TempDir(), "live1.log"),
	})
	_ = fake.NewSessionWithSize("cs-live1", 80, 24)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	if _, err := os.Stat(deadGuard.Dir()); !os.IsNotExist(err) {
		t.Fatalf("stale guard dir kept: %v", err)
	}
	if _, err := os.Stat(liveGuard.Dir()); err != nil {
		t.Fatalf("live guard dir removed: %v", err)
	}
	g := mgr.commitGuardFor("testhost-live1")
	if g == nil || g.Dir() != liveGuard.Dir() || g.Token() != liveGuard.Token() {
		t.Fatalf("live guard not adopted: %+v", g)
	}
	if mgr.commitGuardFor("testhost-dead1") != nil {
		t.Fatal("dead session still has a guard")
	}
	if p := commitGuardHooksPath(deadDir); p != "" {
		t.Fatalf("dead worktree still has hooksPath %q", p)
	}
	if p := commitGuardHooksPath(liveDir); p != "" {
		t.Fatalf("live worktree config has hooksPath %q", p)
	}
}

func TestCommitGuardHomeDirNonProjectRefusedAuditedOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	commitGuardDirty(t, home, "notes.txt")
	pg := NewProjectGit(home)

	var firstErr, secondErr error
	first := commitGuardCaptureStdout(t, func() { firstErr = pg.stageChanges(nil) })
	if !gitguard.IsNonProject(firstErr) {
		t.Fatalf("home stageChanges error = %v, want a non-project refusal", firstErr)
	}
	if n := strings.Count(first, "commit_refused_non_project"); n != 1 {
		t.Fatalf("first refusal logged %d audit lines, want 1: %q", n, first)
	}

	var sha string
	second := commitGuardCaptureStdout(t, func() {
		sha, secondErr = pg.CommitSessionWork("cg02", "task", "summary", nil)
	})
	if !gitguard.IsNonProject(secondErr) || sha != "" {
		t.Fatalf("home commit = (%q, %v), want a non-project refusal", sha, secondErr)
	}
	if strings.Contains(second, "commit_refused_non_project") {
		t.Fatalf("second refusal logged again: %q", second)
	}
	if _, err := os.Stat(filepath.Join(home, ".git")); !os.IsNotExist(err) {
		t.Fatalf("refused call touched the directory: %v", err)
	}
}
