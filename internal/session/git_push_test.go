// Push-on-complete for ad-hoc one-shot sessions (no PRD owner), driven
// end to end with the fake tmux / fake LLM launch harness: a bare
// temp remote plus a working clone, a one-shot that writes a file and
// finishes, and — per the daemon's effective push settings — the commit
// either pushed, left local with a recorded failure + exactly one alert,
// or never pushed at all (policy never / blocked validation / a PRD
// session the autonomous executor owns).

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupBareClone creates a temp bare remote (default branch main) plus a
// working clone with one committed-and-pushed seed, and returns both paths.
func setupBareClone(t *testing.T) (bare, dir string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	gitRunT(t, root, "init", "-q", "--bare", "-b", "main", bare)
	dir = filepath.Join(root, "work")
	gitRunT(t, root, "clone", "-q", bare, dir)
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "seed")
	gitRunT(t, dir, "push", "-q", "-u", "origin", "main")
	return bare, dir
}

// recordAlerts builds a SetPushAlertFn sink; the returned slice is what
// the test inspects.
func recordAlerts(t *testing.T) *[]string {
	t.Helper()
	rows := []string{}
	return &rows
}

// startFakeOneShot starts a one-shot in dir whose fake launch drops a
// worker file into the repo (the uncommitted work the completion commit
// will pick up), like the flow-test harness.
func startFakeOneShot(t *testing.T, mgr *Manager, fake *FakeTmux, dir, task string, validate []string) *Session {
	t.Helper()
	mgr.SetLLMBackend("fake-push", func(_ context.Context, _, tmuxSession, projectDir string, _ string) error {
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Errorf("fake launch: write worker file: %v", err)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})
	sess, err := mgr.Start(context.Background(), task, "", dir, &StartOptions{OneShot: true, ValidateCommands: validate})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return sess
}

// finishOneShot feeds the completion marker through the same
// processOutputLine path a real log tail would.
func finishOneShot(t *testing.T, mgr *Manager, sess *Session, dir string) {
	t.Helper()
	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatch time.Time
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(dir),
		"DATAWATCH_COMPLETE: done", &lastOutputTime, &pendingLines, &lastPromptMatch,
		func() *Tracker { return nil })
}

// localHead / bareHead return the HEAD sha of the work clone and the sha
// of a named ref on the bare remote ("" when the ref is absent).
func localHead(t *testing.T, dir string) string {
	t.Helper()
	out := gitOut(t, dir, "rev-parse", "HEAD")
	if out == "" {
		t.Fatal("work clone has no HEAD")
	}
	return out
}

func bareHead(t *testing.T, bare, ref string) string {
	t.Helper()
	// --quiet keeps stdout empty when the ref is absent; that is a
	// legitimate answer ("ref does not exist"), not a failure.
	out, err := exec.Command("git", "-C", bare, "rev-parse", "--verify", "--quiet", "refs/heads/"+ref).Output()
	if err != nil {
		t.Logf("rev-parse refs/heads/%s on bare (absent?): %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

// Push, branch policy: a validated one-shot's completion commit is pushed
// to its remote branch and the session records PushState=pushed with the
// pushed SHA, no warning, and no alert.
func TestPushAfterCommit_BranchPolicyPushesTheOneShotCommit(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"true"})
	finishOneShot(t, mgr, sess, dir)

	head := localHead(t, dir)
	if head == seed {
		t.Fatal("expected the completion commit to land on the work clone")
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateComplete {
		t.Fatalf("session state after the marker: %v — want Complete", got.State)
	}
	if got.CommitSHA != head {
		t.Errorf("CommitSHA %q — want local HEAD %q", got.CommitSHA, head)
	}
	if got.PushState != "pushed" {
		t.Errorf("PushState %q — want \"pushed\"", got.PushState)
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
	}
	if got.PushWarning != "" {
		t.Errorf("PushWarning %q — want empty on success", got.PushWarning)
	}
	if got := bareHead(t, bare, "main"); got != head {
		t.Errorf("bare remote main %q — want the pushed commit %q", got, head)
	}
	// --set-upstream: tracking must be established on the local branch.
	if up := gitOut(t, dir, "config", "branch.main.remote"); up != "origin" {
		t.Errorf("branch.main.remote %q — want origin (upstream tracking)", up)
	}
	if len(*alerts) != 0 {
		t.Errorf("alerts expected on a successful push, got %d: %v", len(*alerts), *alerts)
	}
}

// Push, unvalidated: a one-shot whose validation command fails gets no
// completion commit, hence no push — the remote is untouched, the
// session records no push state, and no alert fires.
func TestPushAfterCommit_NoPushWhenOneShotValidationFails(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")
	headBefore := localHead(t, dir)

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"false"})
	finishOneShot(t, mgr, sess, dir)

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.CommitSHA != "" {
		t.Errorf("failed validation must block the commit, CommitSHA=%q", got.CommitSHA)
	}
	if localHead(t, dir) != headBefore {
		t.Error("no new commit may land when validation fails")
	}
	if got.PushState != "" || got.PushWarning != "" || got.PushedSHA != "" {
		t.Errorf("no push state expected, got state=%q warning=%q pushedSha=%q", got.PushState, got.PushWarning, got.PushedSHA)
	}
	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("bare remote main %q — must stay at the seed %q", got, seed)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected when no commit was made, got: %v", *alerts)
	}
}

// Push, default-branch policy: a one-shot finishing on a side branch
// fast-forwards HEAD onto the remote's default branch — not onto a same-
// named branch — proving the default-branch refspec was used.
func TestPushAfterCommit_DefaultBranchPolicyFastForwardsDefaultBranch(t *testing.T) {
	bare, dir := setupBareClone(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "feature")

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "default_branch", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"true"})
	finishOneShot(t, mgr, sess, dir)

	head := localHead(t, dir)
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "pushed" || got.PushWarning != "" {
		t.Fatalf("expected a pushed default-branch outcome, got state=%q warning=%q", got.PushState, got.PushWarning)
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want %q", got.PushedSHA, head)
	}
	if got := bareHead(t, bare, "main"); got != head {
		t.Errorf("bare remote main %q — want the fast-forwarded HEAD %q", got, head)
	}
	if got := bareHead(t, bare, "feature"); got != "" {
		t.Errorf("a same-named branch ref must not have been created on the remote (got %q)", got)
	}
	if len(*alerts) != 0 {
		t.Errorf("alerts expected on a successful push, got: %v", *alerts)
	}
}

// Push failure: the commit stays local, PushState=not_pushed with an
// operator-visible warning, exactly one warn alert, and the session is
// still Complete — the push never changes the completion state.
func TestPushAfterCommit_FailedPushKeepsCommitAndFiresOneWarnAlert(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")
	// Break the remote URL after the seed push: the attempt will fail
	// (non-repository path), the work must stay committed locally.
	gitRunT(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "no-such-remote.git"))

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"true"})
	finishOneShot(t, mgr, sess, dir)

	head := localHead(t, dir)
	if head == seed {
		t.Fatal("expected the completion commit to land on the work clone")
	}
	logOut := gitOut(t, dir, "log", "-1", "--name-only", "--pretty=")
	if !strings.Contains(logOut, "hello.txt") {
		t.Errorf("the worker's file must remain committed locally, HEAD commit: %s", logOut)
	}

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.State != StateComplete {
		t.Fatalf("session state after a failed push: %v — must stay Complete", got.State)
	}
	if got.CommitSHA != head {
		t.Errorf("CommitSHA %q — the local commit %q must be retained", got.CommitSHA, head)
	}
	if got.PushState != "not_pushed" {
		t.Errorf("PushState %q — want \"not_pushed\"", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "not pushed:") {
		t.Errorf("PushWarning %q — want operator-visible \"not pushed: ...\"", got.PushWarning)
	}
	if got.PushedSHA != "" {
		t.Errorf("PushedSHA %q — nothing reached the remote", got.PushedSHA)
	}
	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("bare remote main %q — must be untouched by a failed push", got)
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one alert expected on a failed push, got %d: %v", len(*alerts), *alerts)
	}
	if !strings.HasPrefix((*alerts)[0], "warn | ") {
		t.Errorf("the push alert must fire at warn level, got %q", (*alerts)[0])
	}
}

// Push, policy never: the commit still lands locally, but nothing is
// pushed, no push state is recorded, and no alert fires.
func TestPushAfterCommit_NeverPolicyLeavesCommitUnpushedAndSilent(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "never", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"true"})
	finishOneShot(t, mgr, sess, dir)

	if localHead(t, dir) == seed {
		t.Fatal("expected the completion commit to land locally even under policy never")
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "" || got.PushWarning != "" || got.PushedSHA != "" {
		t.Errorf("policy never must record no push state, got state=%q warning=%q pushedSha=%q",
			got.PushState, got.PushWarning, got.PushedSHA)
	}
	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("bare remote main %q — must be untouched under policy never", got)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected under policy never, got: %v", *alerts)
	}
}

// Push, PRD-owned session: a one-shot with a PRD owner is pushed by the
// autonomous executor, not the one-shot path — so even though the commit
// is made (PRD sessions skip the one-shot validation gate) this path must
// not push, record, or alert.
func TestPushAfterCommit_PRDOwnedOneShotIsLeftToThePRDPushEngine(t *testing.T) {
	bare, dir := setupBareClone(t)
	seed := bareHead(t, bare, "main")

	mgr, fake := newTestManagerWithFake(t)
	alerts := recordAlerts(t)
	mgr.SetPushAlertFn(func(level, title, body string) { *alerts = append(*alerts, level+" | "+title+" | "+body) })
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })

	sess := startFakeOneShot(t, mgr, fake, dir, "create hello.txt", []string{"false"})
	// Give the session a PRD owner before completion.
	sess.PRDID = "p-test"
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	finishOneShot(t, mgr, sess, dir)

	head := localHead(t, dir)
	if head == seed {
		t.Fatal("expected the completion commit to land locally (PRD sessions skip the one-shot gate)")
	}
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.CommitSHA != head {
		t.Errorf("CommitSHA %q — want the local commit %q", got.CommitSHA, head)
	}
	if got.PushState != "" || got.PushWarning != "" || got.PushedSHA != "" {
		t.Errorf("a PRD-owned session must not carry one-shot push state, got state=%q warning=%q pushedSha=%q",
			got.PushState, got.PushWarning, got.PushedSHA)
	}
	if got := bareHead(t, bare, "main"); got != seed {
		t.Errorf("bare remote main %q — the PRD's push engine, not the one-shot path, owns this push", got)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected from the one-shot path for a PRD session, got: %v", *alerts)
	}
}
