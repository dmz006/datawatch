// ResumeMonitors must adopt a live session with a delivered task and leave it
// running: it rehydrates the delivery stamp, starts the post-restart grace,
// and never ends, kills or flips the session on a re-rendered pane.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run 'TestResumeMonitorsAdopts|TestClassifyNeedsInput'

package session

import (
	"context"
	"sync"
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

// adoptClock is the injected scan clock: the grace window, task-echo window
// and prompt debounce all read it, so the tests below never wait on wall time.
type adoptClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *adoptClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *adoptClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

const adoptScanInterval = 20 * time.Millisecond

// adoptWithClock injects the fake clock and a fast scan cadence into b, then
// resumes monitors. The clock starts at the real time so the persisted
// TaskDeliveredAt stamp sits just behind it, as it would right after a boot.
func adoptWithClock(t *testing.T, b *Manager) *adoptClock {
	t.Helper()
	fc := &adoptClock{t: time.Now()}
	b.clock = fc.Now
	b.scanInterval = adoptScanInterval
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.ResumeMonitors(ctx)
	return fc
}

// settle gives the monitors several scan ticks to act on the current clock.
func settle() { time.Sleep(10 * adoptScanInterval) }

// adoptRunsPastWindows advances the fake clock well past the 30s grace, the
// task-echo window and the 3s prompt debounce, a step at a time with scans in
// between, and returns the session as soon as it leaves running.
func adoptRunsPastWindows(t *testing.T, b *Manager, sess *Session, fc *adoptClock) *Session {
	t.Helper()
	for i := 0; i < 8; i++ {
		fc.Advance(10 * time.Second)
		settle()
		cur, ok := b.GetSession(sess.FullID)
		if !ok {
			t.Fatalf("session %s vanished", sess.FullID)
		}
		if cur.State != StateRunning {
			return cur
		}
	}
	cur, _ := b.GetSession(sess.FullID)
	return cur
}

// A live delivered-task session stays running through two minutes of scans
// whatever the pane shows: the echoed task, the task's own instruction to print
// a needs-input marker, or the idle claude prompt. It is never killed or ended.
func TestResumeMonitorsAdopts_LiveSessionStaysRunning(t *testing.T) {
	panes := map[string]string{
		"task_echo": tdEchoed,
		// The task text also instructs the model to print a needs-input marker;
		// that instruction line ends in ">" and used to read as a prompt, flipping
		// the session once the prompt debounce elapsed.
		"needs_input": tdEchoed + "\n  When blocked, print DATAWATCH_NEEDS_INPUT: <your question>\n",
		"idle_prompt": tdIdlePane,
	}
	for name, pane := range panes {
		t.Run(name, func(t *testing.T) {
			b, fake, sess, ended := restartEchoSetup(t, pane)
			t.Cleanup(func() { time.Sleep(300 * time.Millisecond) }) // runs after the observer cancels: let monitors drain
			fc := adoptWithClock(t, b)
			cur := adoptRunsPastWindows(t, b, sess, fc)
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

// A standalone marker line the worker prints after the task text is a real
// request and moves the session to waiting_input, but only once the
// post-restart grace has passed: inside it the pane may be replaying history.
func TestResumeMonitorsAdopts_RealNeedsInputAfterTaskFlips(t *testing.T) {
	const question = "which branch should I use?"
	pane := tdEchoed + "\n● DATAWATCH_NEEDS_INPUT: " + question + "\n"
	b, _, sess, _ := restartEchoSetup(t, pane)
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })
	fc := adoptWithClock(t, b)

	settle()
	if cur, _ := b.GetSession(sess.FullID); cur.State != StateRunning {
		t.Fatalf("marker inside the post-restart grace flipped the session: state=%q", cur.State)
	}

	fc.Advance(b.restartGraceWindow() + time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cur, _ := b.GetSession(sess.FullID); cur.State == StateWaitingInput {
			if cur.LastPrompt != question {
				t.Errorf("LastPrompt=%q, want the marker's question %q", cur.LastPrompt, question)
			}
			return
		}
		time.Sleep(adoptScanInterval)
	}
	cur, _ := b.GetSession(sess.FullID)
	t.Fatalf("real standalone marker after the task never flipped the session: state=%q", cur.State)
}

func TestClassifyNeedsInput(t *testing.T) {
	pats := inputNeededPatterns
	cases := []struct {
		line string
		want needsInputKind
		q    string
	}{
		{"just output", needsInputNone, ""},
		{"DATAWATCH_NEEDS_INPUT: which db?", needsInputReal, "which db?"},
		{"● DATAWATCH_NEEDS_INPUT: which db?", needsInputReal, "which db?"},
		{"\x1b[33mDATAWATCH_NEEDS_INPUT: need a token\x1b[0m", needsInputReal, "need a token"},
		{"When blocked, print DATAWATCH_NEEDS_INPUT: <your question>", needsInputInstruction, ""},
		{"DATAWATCH_NEEDS_INPUT: <your question>", needsInputInstruction, ""},
		{"DATAWATCH_NEEDS_INPUT:", needsInputInstruction, ""},
		{"$ echo 'DATAWATCH_NEEDS_INPUT: which db?'", needsInputInstruction, ""},
	}
	for _, c := range cases {
		got, q := classifyNeedsInput(c.line, pats)
		if got != c.want || q != c.q {
			t.Errorf("classifyNeedsInput(%q) = (%v, %q), want (%v, %q)", c.line, got, q, c.want, c.q)
		}
	}
}

// A real summary line after the echoed task still completes normally.
func TestResumeMonitorsAdopts_RealSummaryStillCompletes(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, reRealSummaryPane)
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateComplete {
		t.Errorf("real completion not detected after adoption: state=%q, want %q", cur.State, StateComplete)
	}
	// The state flips to complete before the end callback runs; wait for it
	// instead of reading the counter at the instant the state changed.
	for wait := time.Now().Add(3 * time.Second); ended.Load() == 0 && time.Now().Before(wait); {
		time.Sleep(20 * time.Millisecond)
	}
	if ended.Load() == 0 {
		t.Error("onSessionEnd not called for a real completion")
	}
}
