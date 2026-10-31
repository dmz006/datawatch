// Teardown push: a session being killed, deleted, or reaped saves its
// locally committed-but-unpushed commits to the remote FIRST — before
// the pane dies, the record is removed, or the checkout is reaped. The
// push is bounded, never fails the kill, and keeps the commits in the
// local checkout on failure; the remote's default branch is never
// pushed at teardown (its final gates are unknown there).

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// teardownManager builds a bare Manager (store + hostname, no tmux) like
// the completion-commit tests, plus a running one-shot session bound to
// dir. TmuxSession is deliberately empty so Kill never touches tmux.
func teardownManager(t *testing.T, dir string) (*Manager, *Session, *[]string) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost"}
	sess := &Session{
		ID: "abcd", FullID: "testhost-abcd", Hostname: "testhost",
		State: StateRunning, OneShot: true, ProjectDir: dir, TmuxSession: "",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	alerts := []string{}
	mgr.SetPushAlertFn(func(level, title, body string) { alerts = append(alerts, level+" | "+title+" | "+body) })
	return mgr, sess, &alerts
}

// addUnpushedWorkCommits leaves two committed-but-unpushed commits on a
// new "work" branch of the clone.
func addUnpushedWorkCommits(t *testing.T, dir string) {
	t.Helper()
	gitRunT(t, dir, "checkout", "-q", "-b", "work")
	if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "work one")
	if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "work two")
}

// Kill with unpushed work: the two work-branch commits land on the bare
// remote before the session is killed; the PushReport says what
// happened; the session ends Killed with PushState=pushed, the pushed
// SHA, no warning, and no alert.
func TestTeardown_KillPushesUnpushedWorkBranchCommitsBeforeKilling(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	head := localHead(t, dir)

	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || !rep.Pushed || rep.Reason != "" {
		t.Fatalf("report = %+v — want attempted, pushed, no reason", rep)
	}
	if rep.Count != 2 {
		t.Errorf("report.Count = %d — want 2 unpushed commits", rep.Count)
	}
	if got := bareHead(t, bare, "work"); got != head {
		t.Fatalf("bare remote work %q — want the pushed HEAD %q", got, head)
	}
	if up := gitOut(t, dir, "config", "branch.work.remote"); up != "origin" {
		t.Errorf("branch.work.remote %q — want origin (upstream tracking)", up)
	}

	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateKilled {
		t.Errorf("session state after Kill: %v — want Killed", got.State)
	}
	if got.PushState != "pushed" || got.PushWarning != "" {
		t.Errorf("PushState=%q PushWarning=%q — want pushed, no warning", got.PushState, got.PushWarning)
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
	}
	if got := bareHead(t, bare, "work"); got != head {
		t.Errorf("bare remote work %q — must remain at the pushed HEAD %q", got, head)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected on a successful teardown push, got: %v", *alerts)
	}
}

// Kill without a remote: the push fails (no_remote), but the kill still
// happens, the commits stay committed locally, the session records
// PushState=not_pushed with an operator-visible warning, and exactly one
// warn alert fires.
func TestTeardown_KillWithoutRemoteStillKillsAndAlerts(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "seed")
	addUnpushedWorkCommits(t, dir)
	headBefore := localHead(t, dir)

	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("a failed push must not fail the kill: %v", err)
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateKilled {
		t.Errorf("session state after Kill: %v — want Killed", got.State)
	}
	if got.PushState != "not_pushed" {
		t.Errorf("PushState %q — want \"not_pushed\"", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "not pushed:") {
		t.Errorf("PushWarning %q — want operator-visible \"not pushed: ...\"", got.PushWarning)
	}
	if localHead(t, dir) != headBefore {
		t.Error("the commits must be kept in the local checkout")
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one alert expected on a failed teardown push, got %d: %v", len(*alerts), *alerts)
	}
	if !strings.HasPrefix((*alerts)[0], "warn | ") {
		t.Errorf("the teardown push alert must fire at warn level, got %q", (*alerts)[0])
	}
}

// Kill under policy never: nothing is pushed, no push state is recorded,
// and no alert fires — the kill proceeds.
func TestTeardown_NeverPolicyPushesNothingAtTeardown(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	headBefore := localHead(t, dir)

	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "never", "origin" })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if rep.Attempted || rep.Pushed {
		t.Fatalf("report = %+v — policy never must attempt nothing", rep)
	}
	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateKilled {
		t.Errorf("session state after Kill: %v — want Killed", got.State)
	}
	if got.PushState != "" || got.PushWarning != "" || got.PushedSHA != "" {
		t.Errorf("policy never must record no push state, got state=%q warning=%q pushedSha=%q",
			got.PushState, got.PushWarning, got.PushedSHA)
	}
	if got := bareHead(t, bare, "work"); got != "" {
		t.Errorf("bare remote work %q — must be untouched under policy never", got)
	}
	if localHead(t, dir) != headBefore {
		t.Error("the local commits must be untouched")
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected under policy never, got: %v", *alerts)
	}
}

// Kill with everything already pushed: zero unpushed commits means the
// helper does NO push attempt at all — proven by leaving a remote the
// attempt could only report as a failure: no state, no alert, no remote
// change.
func TestTeardown_NothingUnpushedMakesNoPushAttempt(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	gitRunT(t, dir, "push", "-q", "-u", "origin", "work")
	headPushed := localHead(t, dir)
	// Break the remote URL: a push attempt would now fail, leaving
	// not_pushed state behind. Zero unpushed commits means no attempt.
	gitRunT(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "no-such-remote.git"))

	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if rep.Attempted {
		t.Fatalf("report = %+v — nothing unpushed, so no push attempt expected", rep)
	}
	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateKilled {
		t.Errorf("session state after Kill: %v — want Killed", got.State)
	}
	if got.PushState != "" || got.PushWarning != "" {
		t.Errorf("no push state expected when nothing was unpushed, got state=%q warning=%q",
			got.PushState, got.PushWarning)
	}
	if got := bareHead(t, bare, "work"); got != headPushed {
		t.Errorf("bare remote work %q — must stay at the previously pushed HEAD %q", got, headPushed)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected when there was nothing to push, got: %v", *alerts)
	}
}

// Kill on the remote default branch under the default_branch policy:
// teardown is NOT the moment to push the default branch — the final
// gates are unknown here — so nothing is pushed, the commits stay local,
// and the session records not_pushed with a warning that the
// default-branch push waits for the final gates.
func TestTeardown_DefaultBranchPolicyRefusesPushingTheDefaultBranchAtTeardown(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")
	// Two unpushed commits, on the default branch itself (main).
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRunT(t, dir, "add", "-A")
		gitRunT(t, dir, "commit", "-q", "-m", "unpushed "+f)
	}
	headBefore := localHead(t, dir)

	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "default_branch", "origin" })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if rep.Attempted || rep.Pushed || rep.Reason == "" {
		t.Fatalf("report = %+v — the default branch must be refused at teardown with a reason", rep)
	}
	if !strings.Contains(rep.Reason, "final gates") {
		t.Errorf("reason %q — want it to say the default-branch push waits for the final gates", rep.Reason)
	}
	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateKilled {
		t.Errorf("session state after Kill: %v — want Killed", got.State)
	}
	if got.PushState != "not_pushed" {
		t.Errorf("PushState %q — want \"not_pushed\"", got.PushState)
	}
	if !strings.Contains(got.PushWarning, "final gates") {
		t.Errorf("PushWarning %q — want a warning that the default-branch push waits for the final gates", got.PushWarning)
	}
	if localHead(t, dir) != headBefore {
		t.Error("the local commits must be kept")
	}
	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("bare remote main %q — must stay at the seed %q (never pushed at teardown)", got, seed)
	}
	if len(*alerts) != 0 {
		t.Errorf("a refused default-branch push is a policy decision, not a failure — no alert expected, got: %v", *alerts)
	}
}
