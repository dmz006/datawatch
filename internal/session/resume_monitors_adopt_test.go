// ResumeMonitors must adopt a live session with a delivered task and leave it
// running: it rehydrates the delivery stamp, starts the post-restart grace,
// and never ends, kills or flips the session on a re-rendered pane.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestResumeMonitorsAdopts

package session

import (
	"context"
	"testing"
	"time"
)

func TestResumeMonitorsAdopts_HydratesDeliveryAndGrace(t *testing.T) {
	b, _, sess, _ := restartEchoSetup(t, tdEchoed)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.ResumeMonitors(ctx)

	persisted, _ := b.GetSession(sess.FullID)
	b.mu.Lock()
	got, armed := b.taskDeliveredAt[sess.FullID]
	restarted, graced := b.restartedAt[sess.FullID]
	b.mu.Unlock()
	if !armed || !got.Equal(*persisted.TaskDeliveredAt) {
		t.Errorf("taskDeliveredAt not hydrated from the persisted stamp: armed=%v got=%v want=%v", armed, got, persisted.TaskDeliveredAt)
	}
	if !graced || time.Since(restarted) > 5*time.Second {
		t.Errorf("post-restart grace not recorded: set=%v at=%v", graced, restarted)
	}
}

func TestResumeMonitorsAdopts_NoDeliveryLeftAlone(t *testing.T) {
	b, _, sess, _ := restartEchoSetup(t, tdIdlePane)
	cur, _ := b.GetSession(sess.FullID)
	cur.TaskDeliveredAt = nil
	if err := b.SaveSession(cur); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.ResumeMonitors(ctx)

	b.mu.Lock()
	_, armed := b.taskDeliveredAt[sess.FullID]
	_, graced := b.restartedAt[sess.FullID]
	b.mu.Unlock()
	if armed || graced {
		t.Errorf("session without TaskDeliveredAt was touched: armed=%v graced=%v", armed, graced)
	}
}

// A live delivered-task session stays running through several scan intervals
// whatever the pane shows: the echoed task, a needs-input style prompt, or the
// idle claude prompt. It is never killed or ended.
func TestResumeMonitorsAdopts_LiveSessionStaysRunning(t *testing.T) {
	panes := map[string]string{
		"task_echo": tdEchoed,
		// The task text also instructs the model to print a needs-input marker;
		// echoed on the pane it must not flip the session.
		"needs_input": tdEchoed + "\n  When blocked, print DATAWATCH_NEEDS_INPUT: <your question>\n",
		"idle_prompt": tdIdlePane,
	}
	for name, pane := range panes {
		t.Run(name, func(t *testing.T) {
			b, fake, sess, ended := restartEchoSetup(t, pane)
			t.Cleanup(func() { time.Sleep(300 * time.Millisecond) }) // runs after the observer cancels: let monitors drain
			cur := restartEchoObserve(t, b, sess)
			if cur.State != StateRunning {
				t.Errorf("live delivered-task session flipped: state=%q, want %q", cur.State, StateRunning)
			}
			if n := ended.Load(); n != 0 {
				t.Errorf("onSessionEnd called %d times for a live session, want 0", n)
			}
			if n := fake.Count("kill"); n != 0 {
				t.Errorf("live session was killed %d times", n)
			}
			if !fake.SessionExists(sess.TmuxSession) {
				t.Error("tmux session no longer exists")
			}
		})
	}
}

// A real summary line after the echoed task still completes normally.
func TestResumeMonitorsAdopts_RealSummaryStillCompletes(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, reRealSummaryPane)
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateComplete {
		t.Errorf("real completion not detected after adoption: state=%q, want %q", cur.State, StateComplete)
	}
	if ended.Load() == 0 {
		t.Error("onSessionEnd not called for a real completion")
	}
}
