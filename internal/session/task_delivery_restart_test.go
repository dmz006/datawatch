// A task-delivery claim is not proof of delivery.
//
// The claim is saved before anything is typed. A daemon that stops between the
// claim and the echo check used to leave a worker at an empty prompt that
// counted as started and was never typed into again. These tests reload the
// session store the way a restarted daemon does.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestRestartDuringDelivery

package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const rddTask = "count the files in this folder and report the total"

// rddSetup saves a daemon-delivered claude-code worker on manager A, takes the
// delivery claim without typing anything, then reloads the store in manager B
// over the same data folder and fake terminal showing pane.
func rddSetup(t *testing.T, pane, logText string) (*Manager, *FakeTmux, *Session) {
	t.Helper()
	dataDir := t.TempDir()
	a, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(a)
	fake := a.WithFakeTmux()
	sess := tdrSaveWaiting(t, a, "rd01")
	sess.Hostname = "testhost"
	sess.Task = rddTask
	sess.BackendFamily = "claude-code"
	sess.DaemonDeliversTask = true
	sess.ChannelReady = true
	sess.LogFile = filepath.Join(dataDir, "worker.log")
	if logText != "" {
		writeAgentLog(t, sess, logText)
	}
	if err := a.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	if !a.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	tdrSetPane(fake, sess.TmuxSession, pane)
	b, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(b)
	b.tmux = fake
	t.Cleanup(func() { _ = b.Close() })
	return b, fake, sess
}

// rddResume resumes the monitors and takes over unverified claims, as the
// daemon boot does, and stops everything when the test ends.
func rddResume(t *testing.T, b *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b.ResumeMonitors(ctx)
	b.recoverUnverifiedDeliveries(ctx)
	t.Cleanup(func() {
		cancel()
		b.WaitBackground()
	})
}

// rddTyped counts how often the task was typed into the pane.
func rddTyped(f *FakeTmux) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c.Op, "send-keys") && len(c.Args) > 0 && strings.Contains(c.Args[0], rddTask) {
			n++
		}
	}
	return n
}

func TestRestartDuringDelivery_UnverifiedClaimIsNotStartedAndIsRetyped(t *testing.T) {
	b, fake, sess := rddSetup(t, tdIdlePane, "")

	if b.AgentStarted(sess.FullID) {
		t.Fatal("a claim with nothing typed reads as a started worker after a restart")
	}
	rddResume(t, b)

	pollUntil(t, 20*time.Second, func() bool { return rddTyped(fake) >= 1 }, "the task was never typed again after the restart")
	pollUntil(t, 20*time.Second, func() bool { return b.AgentStarted(sess.FullID) }, "the retyped task never counted as started")
	// A channel bridge ready call after the retype finds the claim taken.
	if b.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("the claim was left open after delivery, so the task could be typed twice")
	}
	time.Sleep(2 * time.Second)
	if n := rddTyped(fake); n != 1 {
		t.Fatalf("task typed %d times, want exactly 1", n)
	}
}

// The daemon stopped after the task was submitted but before it recorded the
// echo: the screen shows the task above an empty box. It is not typed again.
func TestRestartDuringDelivery_SubmittedTaskIsNotRetyped(t *testing.T) {
	pane := "❯ " + rddTask + "\n\n  Looking at the folder.\n" +
		"──────────\n❯ \n──────────\n  ? for shortcuts"
	b, fake, sess := rddSetup(t, pane, "")
	rddResume(t, b)

	pollUntil(t, 10*time.Second, func() bool { return b.AgentStarted(sess.FullID) }, "a worker that holds its task reads as not started")
	time.Sleep(5 * time.Second)
	if n := rddTyped(fake); n != 0 {
		t.Fatalf("task typed %d times into a worker that already had it", n)
	}
	if cur, _ := b.GetSession(sess.FullID); cur.State.IsTerminal() {
		t.Fatalf("session ended: state %s", cur.State)
	}
}

// The daemon takes the claims over when it starts its one-shot reaper, which
// follows the monitor resume. The resume alone types and records nothing: a
// second process that only resumes monitors must leave delivery to the daemon.
func TestRestartDuringDelivery_DaemonBootRunsTheRecovery(t *testing.T) {
	pane := "❯ " + rddTask + "\n\n  Looking at the folder.\n" +
		"──────────\n❯ \n──────────\n  ? for shortcuts"
	b, _, sess := rddSetup(t, pane, "")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		b.WaitBackground()
	})
	b.ResumeMonitors(ctx)
	if cur, _ := b.GetSession(sess.FullID); b.store.Snapshot(cur).TaskVerifiedAt != nil {
		t.Fatal("resuming monitors alone took over the delivery claim")
	}
	b.StartOneShotReaper(ctx)
	if cur, _ := b.GetSession(sess.FullID); b.store.Snapshot(cur).TaskVerifiedAt == nil {
		t.Fatal("the daemon boot did not check the unverified claim")
	}
}

// An answer in the recorded output proves delivery even when the screen has
// scrolled the task away.
func TestRestartDuringDelivery_AssistantTurnInLogIsNotRetyped(t *testing.T) {
	b, fake, sess := rddSetup(t, tdIdlePane, "Welcome\n❯ x\n• Counting\n")
	rddResume(t, b)

	time.Sleep(5 * time.Second)
	if n := rddTyped(fake); n != 0 {
		t.Fatalf("task typed %d times into a worker that already answered", n)
	}
	if !b.AgentStarted(sess.FullID) {
		t.Fatal("a worker with an answer on record reads as not started")
	}
}

// The box already holds the unsent task: nothing more is typed, the session is
// left alone, and the launch does not count as started.
func TestRestartDuringDelivery_UnsentTextInBoxIsLeftAlone(t *testing.T) {
	pane := "──────────\n❯ " + rddTask + "\n──────────\n  ? for shortcuts"
	b, fake, sess := rddSetup(t, pane, "")
	rddResume(t, b)

	time.Sleep(5 * time.Second)
	if n := rddTyped(fake); n != 0 {
		t.Fatalf("task typed %d times on top of the unsent text", n)
	}
	if b.AgentStarted(sess.FullID) {
		t.Fatal("an unsent task counts as a started launch")
	}
	if cur, _ := b.GetSession(sess.FullID); cur.State.IsTerminal() {
		t.Fatalf("session ended: state %s", cur.State)
	}
}
