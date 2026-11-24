package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitFileT commits one file on the current branch of dir.
func commitFileT(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "add "+name)
}

// With the teardown push off, a kill pushes nothing, records nothing and says
// why in the report. The commits stay in the checkout.
func TestTeardownPush_OffPushesNothingAndRecordsNothing(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	mgr.SetPushBeforeTeardown(func() bool { return false })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if rep.Attempted || rep.Pushed || !strings.Contains(rep.Reason, "off") {
		t.Fatalf("report = %+v, want no attempt and an 'off' reason", rep)
	}
	if got := bareHead(t, bare, "work"); got != "" {
		t.Fatalf("branch reached the remote with the setting off: %q", got)
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.PushState != "" || got.PushWarning != "" || len(*alerts) != 0 {
		t.Fatalf("recorded something with the setting off: state=%q warn=%q alerts=%v", got.PushState, got.PushWarning, *alerts)
	}
}

// With it on, a branch holding only its own commits is pushed.
func TestTeardownPush_OnPushesOwnCommits(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	head := localHead(t, dir)
	mgr, sess, _ := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	mgr.SetPushBeforeTeardown(func() bool { return true })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || !rep.Pushed {
		t.Fatalf("report = %+v, want pushed", rep)
	}
	if got := bareHead(t, bare, "work"); got != head {
		t.Fatalf("remote work = %q, want %q", got, head)
	}
}

// A task branch cut from a local main that is ahead of the remote holds main's
// unpushed commits as well as its own. The push would carry main's commits to
// the remote, so nothing is pushed: the Automaton's own commits only.
func TestTeardownPush_RefusesWhenBranchCarriesOtherCommits(t *testing.T) {
	bare, dir := setupBareClone(t)
	// Local main runs ahead of the remote (the operator's unpushed work).
	commitFileT(t, dir, "main-ahead-1.txt")
	commitFileT(t, dir, "main-ahead-2.txt")
	gitRunT(t, dir, "checkout", "-q", "-b", "automaton/65170001-task")
	commitFileT(t, dir, "own.txt")
	mgr, sess, alerts := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	mgr.SetPushBeforeTeardown(func() bool { return true })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if rep.Attempted || rep.Pushed {
		t.Fatalf("report = %+v, want a refusal with no push attempt", rep)
	}
	if !strings.Contains(rep.Reason, "not this session's own") {
		t.Fatalf("reason = %q, want it to say the commits are not the session's own", rep.Reason)
	}
	if got := bareHead(t, bare, "automaton/65170001-task"); got != "" {
		t.Fatalf("the branch was created on the remote: %q", got)
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.PushState != "not_pushed" || !strings.Contains(got.PushWarning, "not this session's own") {
		t.Fatalf("session records state=%q warning=%q", got.PushState, got.PushWarning)
	}
	if len(*alerts) != 0 {
		t.Fatalf("a refusal by rule is not a failure, yet alerts = %v", *alerts)
	}
}

// Unwired (no setting reader, as in unit tests and the CLI) keeps the old
// behaviour; the daemon always wires the setting, off by default.
func TestTeardownPush_UnwiredSettingKeepsLegacyBehaviour(t *testing.T) {
	bare, dir := setupBareClone(t)
	addUnpushedWorkCommits(t, dir)
	head := localHead(t, dir)
	mgr, sess, _ := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	if rep := mgr.pushUnpushedBeforeTeardown(sess); !rep.Pushed {
		t.Fatalf("report = %+v", rep)
	}
	if got := bareHead(t, bare, "work"); got != head {
		t.Fatalf("remote work = %q", got)
	}
}
