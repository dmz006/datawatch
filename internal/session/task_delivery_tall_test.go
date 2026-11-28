// A task taller than the pane. Claude Code draws on the terminal's alternate
// screen, so once such a task is sent its first line is on no screen and in no
// terminal history: only the end of the task is left, above the working line.
package session

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTaskDelivery_TaskTallerThanThePaneIsDelivery(t *testing.T) {
	lines := []string{"You are running non-interactively as part of an automated pipeline.", ""}
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("Step %d of the made-up report module work.", i+1))
	}
	lines = append(lines,
		"Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary of what you did> — that",
		"summary becomes the commit message.",
		"---")
	task := strings.Join(lines, "\n")
	// What a 40-row pane shows just after Enter, as Claude Code 2.1.332 draws it.
	working := "  " + strings.Join(lines[len(lines)-30:], "\n  ") +
		"\n\n· Doodling…\n────────\n❯ \n────────\n  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt"

	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tall1")
	mgr.store.Mutate(func() { sess.BackendFamily, sess.DaemonDeliversTask, sess.Task = "claude-code", true, task })
	tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	fake.NoEcho = true // the pane is set below, as the agent redraws it
	c := tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	mgr.sleepFn = func(d time.Duration) {
		if taskSends(fake, sess.TmuxSession, task) > 0 {
			tdrSetPane(fake, sess.TmuxSession, working)
		}
		c.sleep(d)
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, task, 1, 5*time.Second, 100*time.Millisecond); err != nil {
		t.Fatalf("DeliverOneShotTask: %v (%s)", err, mgr.TaskDeliveryFailure(sess.FullID))
	}
	if got := taskSends(fake, sess.TmuxSession, task); got != 1 {
		t.Errorf("task sent %d times, want exactly 1", got)
	}
	if snap := mgr.store.Snapshot(sess); snap.TaskVerifiedAt == nil || snap.State.IsTerminal() {
		t.Errorf("verified=%v state=%v, want a verified task on a live session", snap.TaskVerifiedAt, snap.State)
	}
}
