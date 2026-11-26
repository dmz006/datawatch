package session

import (
	"strings"
	"testing"
)

// A one-shot's completion push never moves the remote's default branch, even
// under the branch policy when the checkout is on it.
func TestPushAfterCommit_NeverMovesTheRemoteDefaultBranch(t *testing.T) {
	for _, policy := range []string{"branch", "default_branch"} {
		t.Run(policy, func(t *testing.T) {
			bare, dir := setupBareClone(t)
			seed := bareHead(t, bare, "main")
			if policy == "default_branch" {
				gitRunT(t, dir, "checkout", "-q", "-b", "feature")
			}
			mgr, fake := newTestManagerWithFake(t)
			mgr.SetGitPushSettings(func() (string, string) { return policy, "origin" })

			sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"true"})
			finishOneShot(t, mgr, sess, dir)

			if got := bareHead(t, bare, "main"); got != seed {
				t.Errorf("remote main moved from %s to %s", seed, got)
			}
			got, ok := mgr.store.Get(sess.FullID)
			if !ok {
				t.Fatal("session disappeared")
			}
			if got.PushState != "not_pushed" {
				t.Errorf("PushState %q, want not_pushed (warning %q)", got.PushState, got.PushWarning)
			}
		})
	}
}

// The save push at a kill never moves the remote's default branch under the
// branch policy either, when the checkout is on it.
func TestTeardown_BranchPolicyNeverPushesTheDefaultBranch(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")
	gitRunT(t, dir, "commit", "-q", "--allow-empty", "-m", "unpushed work on main")
	mgr, sess, _ := teardownManager(t, dir)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	mgr.SetPushBeforeTeardown(func() bool { return true })

	rep := mgr.pushUnpushedBeforeTeardown(sess)

	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("remote main moved from %s to %s", seed, got)
	}
	if rep.Pushed || !strings.Contains(rep.Reason, "default branch") {
		t.Errorf("report %+v, want a default branch refusal", rep)
	}
}
