package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Fakes-only tests for the start-confirm bound on an adopted session. The
// clock is an adoptStepClock: each read moves it forward by a fixed step and records
// the reading, so a test can see exactly which probe fired the bound.

const (
	adoptedTestLimit = 60 * time.Second
	adoptedTestStep  = 10 * time.Second
)

// adoptedTestPaneFirstTurn is a claude-code pane that has rendered its first
// assistant turn after the prompt.
const adoptedTestPaneFirstTurn = "❯ run the task\n• Reading the spec now.\n"

// adoptedTestPaneSilent is a claude-code pane parked on a startup dialog with
// no assistant turn.
const adoptedTestPaneSilent = "Do you trust this folder?\n❯ 1. Yes\n  2. No\n"

type adoptStepClock struct {
	at    time.Time
	step  time.Duration
	reads []time.Time
}

func (c *adoptStepClock) now() time.Time {
	c.reads = append(c.reads, c.at)
	c.at = c.at.Add(c.step)
	return c.reads[len(c.reads)-1]
}

// blockUntilCancelled is a wait hook for a session that runs until the wait is
// cancelled.
func blockUntilCancelled(ctx context.Context, _ AdoptedWait) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestAdoptedSessionWaitHealthyRunsToCompletion(t *testing.T) {
	cases := []struct {
		name  string
		state string
		pane  string
	}{
		{"first turn shown while waiting", "waiting_input", adoptedTestPaneFirstTurn},
		{"running with no turn in view", "running", adoptedTestPaneSilent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
			finished := make(chan struct{})
			probes := 0
			var killed []string
			sc := &adoptedStartConfirm{
				paneFn: func(string) (string, error) { return tc.pane, nil },
				stateFn: func(string) string {
					probes++
					if probes == 4 {
						close(finished)
					}
					return tc.state
				},
				killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
				limit:    adoptedTestLimit,
				now:      clock.now,
				interval: time.Millisecond,
			}
			waitFn := func(ctx context.Context, _ AdoptedWait) error {
				select {
				case <-finished:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err := waitForAdoptedSession(context.Background(), waitFn, nil, AdoptedWait{SessionID: "sess-healthy"}, sc); err != nil {
				t.Fatalf("healthy adopted session failed: %v", err)
			}
			if len(killed) != 0 {
				t.Fatalf("healthy session was ended: %v", killed)
			}
			if probes < 4 {
				t.Fatalf("wait returned before the session finished: %d probes", probes)
			}
		})
	}
}

func TestAdoptedSessionWaitWithoutPaneHookUnchanged(t *testing.T) {
	// No pane hook: the wait is the plain adopted wait, so a session parked in
	// waiting_input with no turn is not ended by any bound.
	var killed []string
	sc := &adoptedStartConfirm{
		stateFn: func(string) string { return "waiting_input" },
		killFn:  func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:   time.Nanosecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := waitForAdoptedSession(ctx, blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-plain"}, sc)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the plain wait to run until its context ends, got %v", err)
	}
	if errors.Is(err, ErrSessionNeverStarted) || len(killed) != 0 {
		t.Fatalf("plain wait applied the start bound: err=%v killed=%v", err, killed)
	}
}

func TestAdoptedStartFailureFromAdoption(t *testing.T) {
	// No recorded launch time: the silent window runs from adoption, so the
	// session fails on the first probe past the limit (probe 7 at a 10s step
	// against a 60s limit).
	clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
	var killed []string
	sc := &adoptedStartConfirm{
		paneFn:   func(string) (string, error) { return adoptedTestPaneSilent, nil },
		stateFn:  func(string) string { return "waiting_input" },
		killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:    adoptedTestLimit,
		now:      clock.now,
		interval: time.Millisecond,
	}
	err := waitForAdoptedSession(context.Background(), blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-adopt"}, sc)
	if !errors.Is(err, ErrSessionNeverStarted) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("want ErrSessionNeverStarted wrapped with ErrWorkerStalled, got %v", err)
	}
	if len(killed) != 1 || killed[0] != "sess-adopt" {
		t.Fatalf("want the stalled session ended once, got %v", killed)
	}
	last := clock.reads[len(clock.reads)-1].Sub(clock.reads[0])
	prev := clock.reads[len(clock.reads)-2].Sub(clock.reads[0])
	if last != 70*time.Second || prev != 60*time.Second {
		t.Fatalf("bound fired at %s (previous probe %s); want 70s, with 60s not yet past the limit", last, prev)
	}
	if len(sc.failureTail) == 0 {
		t.Fatal("failure tail not recorded for the task")
	}
}

func TestAdoptedStartFailureMeasuresFromLaunchTime(t *testing.T) {
	base := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		startedAt *time.Time
		// wantProbe is the probe index (reads[index]) at which the bound fires.
		wantProbe int
	}{
		{"unset launch time counts from adoption", nil, 7},
		{"launch time 30s before adoption counts from launch", timePtr(base.Add(-30 * time.Second)), 4},
		{"launch time an hour old fails at the first probe", timePtr(base.Add(-time.Hour)), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &adoptStepClock{at: base, step: adoptedTestStep}
			sc := &adoptedStartConfirm{
				paneFn:    func(string) (string, error) { return adoptedTestPaneSilent, nil },
				stateFn:   func(string) string { return "waiting_input" },
				limit:     adoptedTestLimit,
				now:       clock.now,
				interval:  time.Millisecond,
				startedAt: tc.startedAt,
			}
			err := waitForAdoptedSession(context.Background(), blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-launch"}, sc)
			if !errors.Is(err, ErrSessionNeverStarted) {
				t.Fatalf("want ErrSessionNeverStarted, got %v", err)
			}
			if got := len(clock.reads) - 1; got != tc.wantProbe {
				t.Fatalf("bound fired at probe %d, want %d", got, tc.wantProbe)
			}
		})
	}
}

func TestAdoptedStartFailureNotFailedAfterFirstTurn(t *testing.T) {
	// The first probe shows the assistant's first turn; later panes have scrolled
	// it out of view. A session that has started is never failed by the bound,
	// however long it then sits in waiting_input.
	clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probes, paneCalls := 0, 0
	var killed []string
	sc := &adoptedStartConfirm{
		paneFn: func(string) (string, error) {
			paneCalls++
			if paneCalls == 1 {
				return adoptedTestPaneFirstTurn, nil
			}
			return adoptedTestPaneSilent, nil
		},
		stateFn: func(string) string {
			probes++
			if probes == 20 {
				cancel()
			}
			return "waiting_input"
		},
		killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:    adoptedTestLimit,
		now:      clock.now,
		interval: time.Millisecond,
	}
	err := waitForAdoptedSession(ctx, blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-started"}, sc)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want the wait to end on cancel, got %v", err)
	}
	if errors.Is(err, ErrSessionNeverStarted) || len(killed) != 0 {
		t.Fatalf("started session was failed: err=%v killed=%v", err, killed)
	}
	if last := clock.reads[len(clock.reads)-1].Sub(clock.reads[0]); last <= adoptedTestLimit {
		t.Fatalf("test ran only %s; it must run past the limit to mean anything", last)
	}
}

func TestAdoptedStartFailureLeavingWaitingInputNotFailed(t *testing.T) {
	// Waiting for five probes, then running, then waiting again. Leaving
	// waiting_input restarts the silent window, so neither waiting run exceeds
	// the 60s limit and the session is not failed.
	clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probes := 0
	var killed []string
	sc := &adoptedStartConfirm{
		paneFn: func(string) (string, error) { return adoptedTestPaneSilent, nil },
		stateFn: func(string) string {
			probes++
			switch {
			case probes <= 5, probes >= 13 && probes <= 17:
				return "waiting_input"
			case probes > 17:
				cancel()
			}
			return "running"
		},
		killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:    adoptedTestLimit,
		now:      clock.now,
		interval: time.Millisecond,
	}
	err := waitForAdoptedSession(ctx, blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-restart"}, sc)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want the wait to end on cancel, got %v", err)
	}
	if errors.Is(err, ErrSessionNeverStarted) || len(killed) != 0 {
		t.Fatalf("session that left waiting_input was failed: err=%v killed=%v", err, killed)
	}
}

func TestAdoptedStartFailureCountsAgainstStartBudget(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd, err := m.CreatePRD("test spec", "/proj", "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]
	task.Status = TaskInProgress
	task.SessionID = "sess-adopt"

	// Each start failure is booked against SessionStartRetryBudget; the normal
	// retry budget (RetryCount, PriorAttemptCount) is untouched.
	stall := errors.Join(ErrSessionNeverStarted, ErrWorkerStalled)
	for i := 1; i <= SessionStartRetryBudget; i++ {
		exhausted, serr := m.countSessionStartFailure(context.Background(), prd, task, attemptRun{}, stall)
		if serr != nil || exhausted {
			t.Fatalf("start failure %d: exhausted=%v err=%v; want re-queue within budget", i, exhausted, serr)
		}
		if task.StartFailures != i || task.Status != TaskInProgress {
			t.Fatalf("after start failure %d: StartFailures=%d status=%q", i, task.StartFailures, task.Status)
		}
	}
	exhausted, serr := m.countSessionStartFailure(context.Background(), prd, task, attemptRun{}, stall)
	if serr != nil || !exhausted {
		t.Fatalf("budget overrun: exhausted=%v err=%v; want the task failed", exhausted, serr)
	}
	if task.Status != TaskFailed || task.Error != SessionStartFailureReason {
		t.Fatalf("task after overrun: status=%q error=%q", task.Status, task.Error)
	}
	if task.FailureReason == nil || task.FailureReason.Source != "session-start" {
		t.Fatalf("failure reason = %+v, want session-start source", task.FailureReason)
	}
	if task.RetryCount != 0 || task.PriorAttemptCount != 0 {
		t.Fatalf("normal retry budget consumed: RetryCount=%d PriorAttemptCount=%d", task.RetryCount, task.PriorAttemptCount)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
