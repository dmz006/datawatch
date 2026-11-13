package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// restartFakeClock advances only when Sleep is called, so a wait of minutes takes no
// real time. onSleep runs after each advance, to change the fake state.
type restartFakeClock struct {
	now     time.Time
	sleeps  int
	onSleep func(n int)
}

func (c *restartFakeClock) Now() time.Time { return c.now }
func (c *restartFakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	c.sleeps++
	if c.onSleep != nil {
		c.onSleep(c.sleeps)
	}
	return nil
}

// restartFakeWork is the running-task state the policy counts.
type restartFakeWork struct {
	mu       sync.Mutex
	tasks    []InterruptedTask
	sessions []string
	drain    []bool
}

func (w *restartFakeWork) count() RestartInterruption {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := RestartInterruption{
		RunningTasks: append([]InterruptedTask{}, w.tasks...),
		LiveSessions: append([]string{}, w.sessions...),
	}
	r.RunningTaskCnt, r.LiveSessionCnt = len(r.RunningTasks), len(r.LiveSessions)
	return r
}
func (w *restartFakeWork) finishAll() { w.mu.Lock(); w.tasks = nil; w.mu.Unlock() }
func (w *restartFakeWork) setDrain(on bool) {
	w.mu.Lock()
	w.drain = append(w.drain, on)
	w.mu.Unlock()
}

func restartBusyWork() *restartFakeWork {
	return &restartFakeWork{
		tasks:    []InterruptedTask{{PRDID: "p1", TaskID: "t1", Status: "in_progress", SessionID: "ab12"}},
		sessions: []string{"ab12", "cd34"},
	}
}

func applyRestart(t *testing.T, w *restartFakeWork, clk *restartFakeClock, policy, onTimeout string, quiet time.Duration) (RestartDecision, []string) {
	t.Helper()
	var logs []string
	dec := ApplyRestartPolicy(context.Background(), RestartPolicyDeps{
		Policy: policy, QuietTimeout: quiet, OnTimeout: onTimeout,
		PollInterval: 2 * time.Second, Clock: clk,
		Count: w.count, SetLaunchDrain: w.setDrain,
		Logf: func(f string, a ...any) { logs = append(logs, strings.TrimSpace(fmt.Sprintf(f, a...))) },
	})
	return dec, logs
}

func TestRestartPolicyImmediateProceedsAtOnceAndReportsCount(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	dec, logs := applyRestart(t, w, clk, "immediate", "", time.Minute)
	if !dec.Proceed || clk.sleeps != 0 || dec.Waited != 0 {
		t.Fatalf("immediate must not wait: %+v sleeps=%d", dec, clk.sleeps)
	}
	if dec.Initial.RunningTaskCnt != 1 || dec.Initial.LiveSessionCnt != 2 {
		t.Fatalf("count = %+v", dec.Initial)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "1 running task(s)") || !strings.Contains(logs[0], "p1/t1") || !strings.Contains(logs[0], "cd34") {
		t.Fatalf("the count with ids must be logged first: %v", logs)
	}
	if len(w.drain) != 0 {
		t.Fatalf("immediate must not touch the launch gate: %v", w.drain)
	}
}

func TestRestartPolicyUnknownValueIsImmediate(t *testing.T) {
	dec, _ := applyRestart(t, restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}, "bogus", "", time.Minute)
	if !dec.Proceed || dec.Policy != "immediate" {
		t.Fatalf("unknown policy must read as immediate: %+v", dec)
	}
}

func TestRestartPolicyWaitQuietReachesQuiet(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	clk.onSleep = func(n int) {
		if n == 3 {
			w.finishAll()
		}
	}
	dec, _ := applyRestart(t, w, clk, "wait_quiet", "abort", 10*time.Minute)
	if !dec.Proceed || dec.TimedOut || dec.Reason != "quiet" {
		t.Fatalf("must proceed once quiet: %+v", dec)
	}
	if dec.Waited != 6*time.Second {
		t.Fatalf("waited %s, want 6s (three 2s polls)", dec.Waited)
	}
	if dec.Initial.RunningTaskCnt != 1 || dec.Final.RunningTaskCnt != 0 {
		t.Fatalf("initial %d / final %d", dec.Initial.RunningTaskCnt, dec.Final.RunningTaskCnt)
	}
}

func TestRestartPolicyWaitQuietAlreadyQuietDoesNotWait(t *testing.T) {
	w := &restartFakeWork{}
	clk := &restartFakeClock{now: time.Unix(0, 0)}
	dec, _ := applyRestart(t, w, clk, "wait_quiet", "abort", time.Minute)
	if !dec.Proceed || clk.sleeps != 0 {
		t.Fatalf("an already-quiet daemon restarts without waiting: %+v", dec)
	}
}

func TestRestartPolicyWaitQuietTimeoutProceeds(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	dec, logs := applyRestart(t, w, clk, "wait_quiet", "proceed", 7*time.Second)
	if !dec.Proceed || !dec.TimedOut {
		t.Fatalf("timeout with proceed must restart: %+v", dec)
	}
	if dec.Waited != 7*time.Second {
		t.Fatalf("waited %s, want exactly the 7s timeout", dec.Waited)
	}
	if dec.Final.RunningTaskCnt != 1 {
		t.Fatalf("the task was still running at the decision: %+v", dec.Final)
	}
	if !strings.Contains(logs[len(logs)-1], "restarting anyway") {
		t.Fatalf("decision must be logged: %v", logs)
	}
}

func TestRestartPolicyWaitQuietTimeoutAborts(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	dec, logs := applyRestart(t, w, clk, "wait_quiet", "abort", 7*time.Second)
	if dec.Proceed || !dec.TimedOut {
		t.Fatalf("timeout with abort must not restart: %+v", dec)
	}
	if !strings.Contains(logs[len(logs)-1], "aborted") {
		t.Fatalf("abort must be logged: %v", logs)
	}
}

func TestRestartPolicyWaitQuietDefaultOnTimeoutProceeds(t *testing.T) {
	dec, _ := applyRestart(t, restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}, "wait_quiet", "", 4*time.Second)
	if !dec.Proceed || !dec.TimedOut {
		t.Fatalf("an unset restart_on_timeout reads as proceed: %+v", dec)
	}
}

func TestRestartPolicyDrainClosesGateWaitsThenRestarts(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	clk.onSleep = func(n int) {
		if n == 5 {
			w.finishAll()
		}
	}
	dec, _ := applyRestart(t, w, clk, "drain", "abort", time.Second) // drain ignores the quiet timeout
	if !dec.Proceed || dec.Reason != "drained" {
		t.Fatalf("drain must restart once the running task finished: %+v", dec)
	}
	if len(w.drain) != 1 || !w.drain[0] {
		t.Fatalf("the launch gate must be closed once and left closed: %v", w.drain)
	}
	if clk.sleeps != 5 {
		t.Fatalf("slept %d times, want 5 (it outlasts the 1s quiet timeout)", clk.sleeps)
	}
}

func TestRestartPolicyDrainCancelledAbortsAndReopensGate(t *testing.T) {
	w, clk := restartBusyWork(), &restartFakeClock{now: time.Unix(0, 0)}
	ctx, cancel := context.WithCancel(context.Background())
	clk.onSleep = func(n int) {
		if n == 2 {
			cancel()
		}
	}
	dec := ApplyRestartPolicy(ctx, RestartPolicyDeps{
		Policy: "drain", PollInterval: time.Second, Clock: clk,
		Count: w.count, SetLaunchDrain: w.setDrain,
	})
	if dec.Proceed {
		t.Fatalf("a cancelled drain must not restart: %+v", dec)
	}
	if len(w.drain) != 2 || !w.drain[0] || w.drain[1] {
		t.Fatalf("the gate must close, then reopen on abort: %v", w.drain)
	}
}

func TestBuildRestartInterruptionCountsRunningAndVerifyingAndLiveSessions(t *testing.T) {
	mk := func(id string, st TaskStatus, sess string) Task { return Task{ID: id, Status: st, SessionID: sess} }
	prds := []*PRD{
		{ID: "pB", Story: []Story{{Tasks: []Task{mk("t2", TaskVerifying, "s2"), mk("t3", TaskCompleted, "s3"), mk("t4", TaskPending, "")}}}},
		{ID: "pA", Story: []Story{{Tasks: []Task{mk("t1", TaskInProgress, "s1"), mk("t5", TaskQueued, ""), mk("t6", TaskWaitingCapacity, ""), mk("t7", TaskRunningTests, "")}}}},
	}
	sessions := []SessionInfo{{ID: "s2", Alive: true}, {ID: "s1", Alive: true}, {ID: "dead", Alive: false}, {ID: "s9", Alive: true}}
	got := BuildRestartInterruption(prds, sessions, nil, nil, "")
	if got.RunningTaskCnt != 3 || got.LiveSessionCnt != 3 {
		t.Fatalf("counts = %d tasks, %d sessions; want 3 and 3: %+v", got.RunningTaskCnt, got.LiveSessionCnt, got)
	}
	var ids []string
	for _, tk := range got.RunningTasks {
		ids = append(ids, tk.PRDID+"/"+tk.TaskID+":"+tk.Status)
	}
	want := "pA/t1:in_progress pA/t7:running_tests pB/t2:verifying"
	if strings.Join(ids, " ") != want {
		t.Fatalf("running tasks = %v, want %q (sorted; queued, waiting, completed and pending are not interrupted)", ids, want)
	}
	if strings.Join(got.LiveSessions, ",") != "s1,s2,s9" {
		t.Fatalf("live sessions = %v", got.LiveSessions)
	}
	if empty := BuildRestartInterruption(nil, nil, nil, nil, ""); empty.RunningTasks == nil || empty.LiveSessions == nil || !empty.Quiet() {
		t.Fatalf("empty report must carry empty lists, not null: %+v", empty)
	}
}

func TestWaitLaunchDrainHoldsUntilGateOpens(t *testing.T) {
	m := &Manager{}
	m.SetLaunchDraining(true)
	if !m.LaunchDraining() {
		t.Fatal("gate must read closed")
	}
	done := make(chan error, 1)
	go func() { done <- m.waitLaunchDrain(context.Background()) }()
	select {
	case err := <-done:
		_ = err
		t.Fatal("a task must wait while the gate is closed")
	case <-time.After(2 * launchDrainPoll):
	}
	m.SetLaunchDraining(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task not released after the gate opened")
	}
}

func TestWaitLaunchDrainReturnsOnCancel(t *testing.T) {
	m := &Manager{}
	m.SetLaunchDraining(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.waitLaunchDrain(ctx); err == nil {
		t.Fatal("a cancelled executor must not wait on the gate")
	}
}

func TestRestartInterruptionTextListsIds(t *testing.T) {
	txt := RestartInterruptionText(RestartInterruption{
		RunningTasks: []InterruptedTask{{PRDID: "p1", TaskID: "t1", Status: "verifying", SessionID: "ab12"}},
		LiveSessions: []string{"ab12"}, RunningTaskCnt: 1, LiveSessionCnt: 1,
		Policy: "drain", QuietTimeoutSec: 600, OnTimeout: "proceed", LaunchDraining: true,
	})
	for _, w := range []string{"1 running task(s)", "p1/t1 verifying session ab12", "session ab12", "drain", "new tasks held"} {
		if !strings.Contains(txt, w) {
			t.Errorf("text lacks %q:\n%s", w, txt)
		}
	}
}
