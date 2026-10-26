package autonomous

// Characterization tests for how a session start is judged and booked, for an
// adopted worker and for a launched one. The start-confirm tests drive
// waitForAdoptedSession on the fake clock (adoptStepClock) and the fake hooks
// from adopted_start_confirm_test.go. The booking test drives the retry loop
// through the session fakes in adopt_live_session_test.go.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startOutcomesBase is the fixed start of every fake clock in this file.
var startOutcomesBase = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

// startOutcomesSilentPane returns a pane of n non-empty rows, none of which is
// an assistant turn or a tool call.
func startOutcomesSilentPane(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "pane row %02d\n", i)
	}
	return b.String()
}

// TestAdoptedWorkerFirstTurnInsideLimitRunsToCompletion: an adopted worker that
// shows its first assistant turn inside the start-confirm limit has started.
// It then sits silent in waiting_input far past the limit, and it is never
// ended. The wait returns the completion of the wait hook, not an error.
func TestAdoptedWorkerFirstTurnInsideLimitRunsToCompletion(t *testing.T) {
	const probesToFinish = 30 // 300s of probes, well past the 60s limit
	clock := &adoptStepClock{at: startOutcomesBase, step: adoptedTestStep}
	paneCalls, probes := 0, 0
	finished := make(chan struct{})
	var killed []string
	sc := &adoptedStartProbe{
		paneFn: func(string) (string, error) {
			paneCalls++
			// Probe 2 is 20s after adoption, inside the 60s limit.
			if paneCalls == 2 {
				return adoptedTestPaneFirstTurn, nil
			}
			return adoptedTestPaneSilent, nil
		},
		stateFn: func(string) string {
			probes++
			if probes == probesToFinish {
				close(finished)
			}
			return "waiting_input"
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
	if err := waitForAdoptedSession(context.Background(), waitFn, nil, AdoptedWait{SessionID: "sess-turn"}, sc); err != nil {
		t.Fatalf("adopted worker with a turn inside the limit: wait returned %v, want nil", err)
	}
	if len(killed) != 0 {
		t.Fatalf("started worker was ended: %v", killed)
	}
	if probes != probesToFinish {
		t.Fatalf("probes = %d, want %d", probes, probesToFinish)
	}
	if silent := clock.reads[len(clock.reads)-1].Sub(clock.reads[0]); silent <= adoptedTestLimit {
		t.Fatalf("waited %s on the fake clock, want more than the %s limit", silent, adoptedTestLimit)
	}
}

// TestAdoptedWorkerNeverStartedIsEndedPastLimitWithPaneTail: an adopted worker
// that never shows an assistant turn is ended on the first probe past the
// limit. The error wraps both ErrSessionNeverStarted and ErrWorkerStalled, and
// the task's failure tail is the last 20 pane rows.
func TestAdoptedWorkerNeverStartedIsEndedPastLimitWithPaneTail(t *testing.T) {
	clock := &adoptStepClock{at: startOutcomesBase, step: adoptedTestStep}
	pane := startOutcomesSilentPane(25)
	var killed []string
	sc := &adoptedStartProbe{
		paneFn:   func(string) (string, error) { return pane, nil },
		stateFn:  func(string) string { return "waiting_input" },
		killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:    adoptedTestLimit,
		now:      clock.now,
		interval: time.Millisecond,
	}
	err := waitForAdoptedSession(context.Background(), blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-never"}, sc)
	if !errors.Is(err, ErrSessionNeverStarted) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("want ErrSessionNeverStarted wrapped with ErrWorkerStalled, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), SessionStartFailureReason) {
		t.Fatalf("error %q does not carry the reason %q", err.Error(), SessionStartFailureReason)
	}
	if !reflect.DeepEqual(killed, []string{"sess-never"}) {
		t.Fatalf("killed = %v, want the stalled session ended once", killed)
	}
	// reads[0] is the adoption time; probe k reads reads[k]. Probe 6 is 60s of
	// silence, which is not past the limit; probe 7 is 70s, the first past it.
	if probe := len(clock.reads) - 1; probe != 7 {
		t.Fatalf("bound fired at probe %d, want probe 7", probe)
	}
	if got := clock.reads[7].Sub(clock.reads[0]); got != 70*time.Second {
		t.Fatalf("bound fired after %s of silence, want 70s", got)
	}
	var wantTail []string
	for i := 6; i <= 25; i++ {
		wantTail = append(wantTail, fmt.Sprintf("pane row %02d", i))
	}
	if !reflect.DeepEqual(sc.failureTail(), wantTail) {
		t.Fatalf("failure tail = %v\nwant the last 20 pane rows %v", sc.failureTail(), wantTail)
	}
}

// TestAdoptedWorkerBusyUpToRestartIsNotChargedForDowntime: an adopted worker
// that was busy right up to the restart has a recent last evidence of life and
// an old task launch time. The hour the daemon was down is not charged: the
// silent window runs from the last evidence of life, so the bound fires on the
// probe that is 70s past it, not on the first probe.
func TestAdoptedWorkerBusyUpToRestartIsNotChargedForDowntime(t *testing.T) {
	clock := &adoptStepClock{at: startOutcomesBase, step: adoptedTestStep}
	lastLife := startOutcomesBase.Add(-10 * time.Second)
	startedAt := startOutcomesBase.Add(-time.Hour)
	var killed []string
	sc := &adoptedStartProbe{
		paneFn:     func(string) (string, error) { return adoptedTestPaneSilent, nil },
		stateFn:    func(string) string { return "waiting_input" },
		killFn:     func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:      adoptedTestLimit,
		startedAt:  &startedAt,
		lastLifeFn: func(string) time.Time { return lastLife },
		now:        clock.now,
		interval:   time.Millisecond,
	}
	err := waitForAdoptedSession(context.Background(), blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-busy"}, sc)
	if !errors.Is(err, ErrSessionNeverStarted) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("want ErrSessionNeverStarted wrapped with ErrWorkerStalled, got %v", err)
	}
	if !reflect.DeepEqual(killed, []string{"sess-busy"}) {
		t.Fatalf("killed = %v, want the session ended once", killed)
	}
	if probe := len(clock.reads) - 1; probe != 6 {
		t.Fatalf("bound fired at probe %d, want probe 6 (the first 70s past the last evidence of life)", probe)
	}
	if got := clock.reads[6].Sub(lastLife); got != 70*time.Second {
		t.Fatalf("bound fired %s after the last evidence of life, want 70s", got)
	}
}

// TestAdoptedWorkerWithoutEvidenceFailsAtOnceOnOldLaunchTime: a task whose
// recorded launch time is an hour old, and whose session shows no evidence of
// life, fails on the first probe.
func TestAdoptedWorkerWithoutEvidenceFailsAtOnceOnOldLaunchTime(t *testing.T) {
	clock := &adoptStepClock{at: startOutcomesBase, step: adoptedTestStep}
	startedAt := startOutcomesBase.Add(-time.Hour)
	var killed []string
	sc := &adoptedStartProbe{
		paneFn:     func(string) (string, error) { return adoptedTestPaneSilent, nil },
		stateFn:    func(string) string { return "waiting_input" },
		killFn:     func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:      adoptedTestLimit,
		startedAt:  &startedAt,
		lastLifeFn: func(string) time.Time { return time.Time{} },
		now:        clock.now,
		interval:   time.Millisecond,
	}
	err := waitForAdoptedSession(context.Background(), blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-old"}, sc)
	if !errors.Is(err, ErrSessionNeverStarted) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("want ErrSessionNeverStarted wrapped with ErrWorkerStalled, got %v", err)
	}
	if !reflect.DeepEqual(killed, []string{"sess-old"}) {
		t.Fatalf("killed = %v, want the session ended once", killed)
	}
	if probe := len(clock.reads) - 1; probe != 1 {
		t.Fatalf("bound fired at probe %d, want probe 1", probe)
	}
}

// startOutcomesBooking is what a start failure leaves on the task and in its
// failure escalation record, read once the PRD has settled.
type startOutcomesBooking struct {
	StartFailures       int
	Status              string
	Error               string
	RetryCount          int
	PriorAttemptCount   int
	EscalationFailures  int
	AttemptOutcomes     []string
	ReasonedAttempts    int
	EscalationDecisions int
}

func startOutcomesBookingOf(prd *PRD) startOutcomesBooking {
	tk := prd.Story[0].Tasks[0]
	b := startOutcomesBooking{
		StartFailures:      tk.StartFailures,
		Status:             string(tk.Status),
		Error:              tk.Error,
		RetryCount:         tk.RetryCount,
		PriorAttemptCount:  tk.PriorAttemptCount,
		EscalationFailures: tk.EscalationFailures,
	}
	for _, a := range tk.Attempts {
		b.AttemptOutcomes = append(b.AttemptOutcomes, a.Outcome)
		if strings.HasPrefix(a.FailureSummary, SessionStartFailureReason) {
			b.ReasonedAttempts++
		}
	}
	for _, d := range prd.Decisions {
		if d.Kind == "failure_escalation" {
			b.EscalationDecisions++
		}
	}
	return b
}

// startOutcomesLaunchedBooking runs one pending task whose spawned worker
// never starts: every verify reports a start failure. Three spawns are made.
func startOutcomesLaunchedBooking(t *testing.T) startOutcomesBooking {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	prd, err := m.CreatePRD("s", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do it"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	var spawns int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		n := atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: fmt.Sprintf("sess-launched-%d", n)}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{}, startErr("no assistant output after bound")
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if n := atomic.LoadInt32(&spawns); n != 3 {
		t.Fatalf("launched worker: %d spawns, want 3 (initial + 2 re-queues)", n)
	}
	return startOutcomesBookingOf(got)
}

// startOutcomesAdoptedBooking runs one in_progress task adopted at boot whose
// adopted session never starts. The adopted start failure is the first booking;
// the two re-queues after it spawn fresh workers that also never start.
func startOutcomesAdoptedBooking(t *testing.T) startOutcomesBooking {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	fs := newFakeSessions("sess-adopted")
	fs.wire(m)
	m.SetSessionWaitFn(blockUntilCancelled)
	m.SetSessionPaneFn(func(string) (string, error) { return adoptedTestPaneSilent, nil })
	m.SetSessionStateFn(func(string) string { return "waiting_input" })
	m.SetSessionStartLimit(time.Second)

	cur := setupBootedAdoptState(t, m, "sess-adopted")
	m.resetInProgressTasksForResume(cur)
	got, _ := m.Store().GetPRD(cur.ID)
	if tk := got.Story[0].Tasks[0]; tk.Status != TaskInProgress || !tk.AdoptedSession {
		t.Fatalf("precondition: task must be adopted (status=%s adopted=%v)", tk.Status, tk.AdoptedSession)
	}

	var spawns int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		n := atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: fmt.Sprintf("sess-replacement-%d", n)}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{}, startErr("no assistant output after bound")
	}
	done := runToTerminal(t, m, api, cur.ID, spawn, verify)
	if n := atomic.LoadInt32(&spawns); n != 2 {
		t.Fatalf("adopted worker: %d spawns, want 2 (the adopted start failure spawns nothing)", n)
	}
	return startOutcomesBookingOf(done)
}

// TestStartFailureBookingMatchesLaunchedAndAdoptedWorkers: a start failure is
// booked the same way whether the worker was launched or adopted. Each failure
// is counted in StartFailures; the first two re-queue the same attempt with
// the reason "session never started"; the third fails the task. The failure
// escalation record (EscalationFailures and any escalation decision) is
// identical in both cases.
func TestStartFailureBookingMatchesLaunchedAndAdoptedWorkers(t *testing.T) {
	want := startOutcomesBooking{
		StartFailures:       3,
		Status:              string(TaskFailed),
		Error:               SessionStartFailureReason,
		RetryCount:          0,
		PriorAttemptCount:   0,
		EscalationFailures:  1,
		AttemptOutcomes:     []string{"failed", "failed", "failed"},
		ReasonedAttempts:    3,
		EscalationDecisions: 0,
	}
	launched := startOutcomesLaunchedBooking(t)
	if !reflect.DeepEqual(launched, want) {
		t.Fatalf("launched worker booking = %+v\nwant %+v", launched, want)
	}
	adopted := startOutcomesAdoptedBooking(t)
	if !reflect.DeepEqual(adopted, want) {
		t.Fatalf("adopted worker booking = %+v\nwant %+v", adopted, want)
	}
}
