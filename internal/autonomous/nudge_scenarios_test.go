// Scenario tests for the claude-code idle nudge along the paths a worker takes
// through the executor: a task verifying on its live worker lease, a task
// adopted after a daemon restart, and a nudge that stalls and retries in a fresh
// session. Every wait runs on an injected clock through the hooks the daemon
// wires. Fakes only: no tmux, no claude-code, no sleeps. The unit halves live in
// cmd/datawatch/post_turn_nudge_test.go.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// scenarioTick is the poll interval of the injected clock in the scenario waits.
// It divides every window the scenarios use, so each deadline lands on a tick.
const scenarioTick = 10 * time.Second

// scenarioWait is when the adopted or verify wait starts: ten minutes after
// nudgeT0, so an earlier nudge can be persisted before it.
var scenarioWait = nudgeT0.Add(10 * time.Minute)

// scenarioHooks wires the fake worker to the manager as the daemon does, and
// keeps the time of every nudge recorded on the task.
func scenarioHooks(t *testing.T, m *Manager, w *fakeNudgeWorker, prdID, taskID, sessionID string, recorded *[]time.Time) WorkerNudgeHooks {
	t.Helper()
	hooks := nudgeRetryHooks(t, m, w, prdID, taskID, sessionID)
	record := hooks.Record
	hooks.Record = func(at time.Time) (int, error) {
		*recorded = append(*recorded, at)
		return record(at)
	}
	return hooks
}

// scenarioLegacyRetries sets the retry budget of the nudge retry tests: one
// attempt plus two retries, under the legacy failure policy.
func scenarioLegacyRetries(t *testing.T, m *Manager, prdID string) {
	t.Helper()
	cfg := m.Config()
	cfg.AutoFixRetries = 2
	m.SetConfig(cfg)
	fp, _ := m.Store().GetPRD(prdID)
	fp.FailureEscalation = &FailureEscalationPolicy{Enabled: escalationOn(false)}
	_ = m.Store().SavePRD(fp)
}

// scenarioAdoptedWaitFn is the daemon's adopted-session wait for the fake
// worker: WaitAdopted on the injected clock, with the task's nudge hooks, and a
// worker that stays idle at its prompt. endedAt receives the clock when the wait
// returns.
func scenarioAdoptedWaitFn(t *testing.T, m *Manager, w *fakeNudgeWorker, recorded *[]time.Time, endedAt *time.Time) func(context.Context, AdoptedWait) error {
	return func(ctx context.Context, aw AdoptedWait) error {
		now := scenarioWait
		err := WaitAdopted(ctx, aw, AdoptedWaitPorts{
			NudgeAfter: nudgeTestWindow,
			StallAfter: nudgeTestStall,
			Status: func(string) (session.State, string, bool) {
				return session.StateWaitingInput, WorkerNudgeFamily, true
			},
			Completed: func(string) bool { return false },
			Nudge: func(sessionID string) WorkerNudgeHooks {
				return scenarioHooks(t, m, w, aw.PRDID, aw.TaskID, sessionID, recorded)
			},
			Now: func() time.Time { return now },
			Pause: func(ctx context.Context) error {
				now = now.Add(scenarioTick)
				return ctx.Err()
			},
		})
		*endedAt = now
		return err
	}
}

// scenarioVerifyWait is a resumed task's verify wait: a tracker seeded from the
// nudge the task holds for its session, polled on the injected clock until the
// nudge stalls it. It returns the stall time and the stall error, or a nil error
// when the worker never stalls.
func scenarioVerifyWait(t *testing.T, m *Manager, w *fakeNudgeWorker, prdID string, vt *Task, recorded *[]time.Time) (time.Time, error) {
	t.Helper()
	hooks := scenarioHooks(t, m, w, prdID, vt.ID, vt.SessionID, recorded)
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	tr.SeedNudgedAt(vt.NudgeTimeForSession(vt.SessionID))
	label := prdID + "/" + vt.ID
	end := scenarioWait.Add(nudgeTestWindow + nudgeTestStall)
	for now := scenarioWait; !now.After(end); now = now.Add(scenarioTick) {
		if err := StepWorkerNudge(tr, now, WorkerNudgeFamily, vt.SessionID, label, hooks); err != nil {
			return now, err
		}
	}
	return time.Time{}, nil
}

// TestNudgeScenario_VerifyingWorkerWithOnlyWorkerLease: the task is verifying on a
// live claude-code worker that holds its worker lease and no verifier lease. The
// executor resumes it without spawning, and the verify wait nudges the idle worker
// once, one window after the wait starts. The status label and the leases are not
// inputs to that decision, so the schedule is the one a fresh worker gets.
func TestNudgeScenario_VerifyingWorkerWithOnlyWorkerLease(t *testing.T) {
	m, api, led := capFixture(t, 1)
	fs := newFakeSessions("sess-v")
	fs.wire(m)

	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskVerifying
	tk.SessionID = "sess-v"
	_ = m.Store().SaveTask(tk)
	m.adoptCapacity(cur, tk)
	if !led.Held(tk.ID) || led.Held(VerifyHolder(tk.ID)) {
		t.Fatalf("setup: want only the worker lease, worker held=%v verifier held=%v", led.Held(tk.ID), led.Held(VerifyHolder(tk.ID)))
	}

	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: tk.Spec}
	var sentAt []time.Time
	spawns, verifyCalls := 0, 0
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: fmt.Sprintf("s-new-%d", spawns)}, nil
	}
	verify := func(_ context.Context, p *PRD, vt *Task) (VerificationResult, error) {
		verifyCalls++
		if vt.Status != TaskVerifying {
			t.Errorf("verify saw status %s, want verifying", vt.Status)
		}
		if !led.Held(vt.ID) || led.Held(VerifyHolder(vt.ID)) {
			t.Errorf("verify saw worker held=%v verifier held=%v, want the worker lease only", led.Held(vt.ID), led.Held(VerifyHolder(vt.ID)))
		}
		hooks := scenarioHooks(t, m, w, p.ID, vt.ID, vt.SessionID, &sentAt)
		tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
		tr.SeedNudgedAt(vt.NudgeTimeForSession(vt.SessionID))
		label := p.ID + "/" + vt.ID
		// Idle from the wait start; silent through the stall window's last tick
		// before it ends the session. Sends expected after each step.
		steps := []time.Duration{0, nudgeTestWindow - scenarioTick, nudgeTestWindow, nudgeTestWindow + nudgeTestStall - scenarioTick}
		wantSent := []int{0, 0, 1, 1}
		for i, at := range steps {
			if err := StepWorkerNudge(tr, scenarioWait.Add(at), WorkerNudgeFamily, vt.SessionID, label, hooks); err != nil {
				t.Errorf("verify wait at %s: %v", at, err)
			}
			if len(w.sent) != wantSent[i] {
				t.Errorf("after the step at %s: sent %d, want %d", at, len(w.sent), wantSent[i])
			}
		}
		return VerificationResult{OK: true, Summary: "good"}, nil
	}

	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD ended %s, want completed", got.Status)
	}
	if spawns != 0 {
		t.Fatalf("verifying task spawned %d sessions, want the live worker resumed", spawns)
	}
	if verifyCalls != 1 {
		t.Fatalf("verify calls = %d, want 1", verifyCalls)
	}
	if len(w.sent) != 1 || w.sent[0] != WorkerNudgeText {
		t.Fatalf("sent = %q, want the completion nudge once", w.sent)
	}
	if len(sentAt) != 1 || !sentAt[0].Equal(scenarioWait.Add(nudgeTestWindow)) {
		t.Fatalf("nudge recorded at %v, want once at the wait start plus the window", sentAt)
	}
	if len(w.ended) != 0 {
		t.Fatalf("worker ended %d times, want 0: the worker answered nothing and stalled nowhere", len(w.ended))
	}

	tk2 := nudgeRetryTask(m, prd.ID, tk.ID)
	if tk2.NudgeCount != 1 {
		t.Fatalf("task NudgeCount = %d, want 1", tk2.NudgeCount)
	}
	if len(tk2.NudgeEvents) != 1 {
		t.Fatalf("nudge events = %+v, want one", tk2.NudgeEvents)
	}
	if ev := tk2.NudgeEvents[0]; ev.SessionID != "sess-v" || ev.Outcome != NudgeOutcomeSent {
		t.Fatalf("nudge event = %+v, want session sess-v with outcome sent", ev)
	}
}

// TestNudgeScenario_AdoptedSessionAfterRestartIsNudged covers the two restart
// shapes that wait on a live worker: (a) an in-progress task adopted at boot,
// waited through WaitAdopted; (b) a task already verifying, resumed into the
// verify wait with no spawn. Each shape runs with no earlier nudge, with a nudge
// persisted for the live session, and with one persisted for another session.
func TestNudgeScenario_AdoptedSessionAfterRestartIsNudged(t *testing.T) {
	const live = "sess-adopt"
	// An earlier nudge sent 100s before the wait starts.
	persistedAt := scenarioWait.Add(-100 * time.Second)
	freshSendAt := scenarioWait.Add(nudgeTestWindow)
	freshStallAt := freshSendAt.Add(nudgeTestStall)

	for _, shape := range []struct {
		name    string
		status  TaskStatus
		adopted bool
	}{
		{"adopted in-progress task waits through WaitAdopted", TaskInProgress, true},
		{"verifying task resumes into the verify wait", TaskVerifying, false},
	} {
		for _, tc := range []struct {
			name      string
			persisted string // session the earlier nudge was typed into; "" for none
			wantSend  bool
			wantStall time.Time
			wantCount int // task NudgeCount once the restart's wait is over
		}{
			{name: "no earlier nudge", persisted: "", wantSend: true, wantStall: freshStallAt, wantCount: 1},
			// Stall window counts from the persisted nudge: persistedAt + 600s.
			{name: "nudge persisted for this session", persisted: live, wantSend: false, wantStall: persistedAt.Add(nudgeTestStall), wantCount: 1},
			{name: "nudge persisted for another session", persisted: "sess-earlier", wantSend: true, wantStall: freshStallAt, wantCount: 2},
		} {
			t.Run(shape.name+": "+tc.name, func(t *testing.T) {
				m, api, _ := capFixture(t, 1)
				fs := newFakeSessions(live)
				fs.wire(m)

				prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
				_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
				scenarioLegacyRetries(t, m, prd.ID)
				cur, _ := m.Store().GetPRD(prd.ID)
				tk := &cur.Story[0].Tasks[0]
				tk.Status = shape.status
				tk.AdoptedSession = shape.adopted
				tk.SessionID = live
				_ = m.Store().SaveTask(tk)
				if tc.persisted != "" {
					if err := m.RecordTaskNudge(prd.ID, tk.ID, tc.persisted, persistedAt); err != nil {
						t.Fatalf("RecordTaskNudge: %v", err)
					}
				}

				w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: tk.Spec}
				var sentAt []time.Time
				var endedAt time.Time
				if shape.adopted {
					m.SetSessionWaitFn(scenarioAdoptedWaitFn(t, m, w, &sentAt, &endedAt))
				}

				spawns, verifyCalls := 0, 0
				spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
					spawns++
					return SpawnResult{SessionID: fmt.Sprintf("s-new-%d", spawns)}, nil
				}
				verify := func(_ context.Context, p *PRD, vt *Task) (VerificationResult, error) {
					verifyCalls++
					if !shape.adopted && verifyCalls == 1 {
						at, err := scenarioVerifyWait(t, m, w, p.ID, vt, &sentAt)
						endedAt = at
						if err != nil {
							return VerificationResult{}, err
						}
					}
					return VerificationResult{OK: true, Summary: "good"}, nil
				}

				got := runToTerminal(t, m, api, prd.ID, spawn, verify)
				if got.Status != PRDCompleted {
					t.Fatalf("PRD ended %s, want completed after the stalled session is retried", got.Status)
				}
				if spawns != 1 {
					t.Fatalf("spawns = %d, want 1: the retry after the stall", spawns)
				}
				if !endedAt.Equal(tc.wantStall) {
					t.Fatalf("the restart's wait ended at %s, want the stall at %s", endedAt, tc.wantStall)
				}
				if tc.wantSend {
					if len(w.sent) != 1 || w.sent[0] != WorkerNudgeText {
						t.Fatalf("sent = %q, want the completion nudge once", w.sent)
					}
					if len(sentAt) != 1 || !sentAt[0].Equal(freshSendAt) {
						t.Fatalf("nudge recorded at %v, want once at %s", sentAt, freshSendAt)
					}
				} else if len(w.sent) != 0 || len(sentAt) != 0 {
					t.Fatalf("persisted nudge for this session was sent again: typed %q, recorded at %v", w.sent, sentAt)
				}

				tk2 := nudgeRetryTask(m, prd.ID, tk.ID)
				if tk2.NudgeCount != tc.wantCount {
					t.Fatalf("task NudgeCount = %d, want %d", tk2.NudgeCount, tc.wantCount)
				}
			})
		}
	}
}

// TestNudgeScenario_NudgeThenStallThenRetry: an idle worker is nudged, stays
// silent for the stall window, and the session is ended with a reason that names
// the stall. The error wraps ErrWorkerStalled and the executor retries in a fresh
// session. That session is not seeded from the first session's nudge, and it is
// nudged again on its own schedule. The log names the session and the task at
// each step.
func TestNudgeScenario_NudgeThenStallThenRetry(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	scenarioLegacyRetries(t, m, prd.ID)

	logs := &nudgeRetryLog{}
	prevOut := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(prevOut)

	spawns, verifyCalls := 0, 0
	var stallErr error
	var retryHint string
	var secondSentAt []time.Time
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: fmt.Sprintf("s-%d", spawns)}, nil
	}
	// The retry's own idle clock starts well after the first session's stall, so a
	// tracker seeded from the first session would stall it on its first look.
	retryStart := scenarioWait.Add(20 * time.Minute)
	verify := func(_ context.Context, p *PRD, vt *Task) (VerificationResult, error) {
		verifyCalls++
		if verifyCalls == 1 {
			err := nudgeRetryFirstSession(t, m, p.ID, vt)
			stallErr = err
			return VerificationResult{}, err
		}
		retryHint = vt.Error
		if vt.SessionID != "s-2" {
			t.Errorf("retry ran in session %q, want a fresh s-2", vt.SessionID)
		}
		w2 := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: vt.Spec}
		hooks := scenarioHooks(t, m, w2, p.ID, vt.ID, vt.SessionID, &secondSentAt)
		tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
		tr.SeedNudgedAt(vt.NudgeTimeForSession(vt.SessionID))
		if tr.Nudged() {
			t.Errorf("retry session %s is seeded from an earlier session's nudge", vt.SessionID)
		}
		label := p.ID + "/" + vt.ID
		for _, at := range []time.Duration{0, nudgeTestWindow - scenarioTick, nudgeTestWindow} {
			if err := StepWorkerNudge(tr, retryStart.Add(at), WorkerNudgeFamily, vt.SessionID, label, hooks); err != nil {
				t.Errorf("retry session step at %s: %v", at, err)
			}
		}
		if len(w2.sent) != 1 || w2.sent[0] != WorkerNudgeText {
			t.Errorf("retry session sent %q, want the completion nudge once at its own window", w2.sent)
		}
		if len(secondSentAt) != 1 || !secondSentAt[0].Equal(retryStart.Add(nudgeTestWindow)) {
			t.Errorf("retry nudge recorded at %v, want once at %s", secondSentAt, retryStart.Add(nudgeTestWindow))
		}
		return VerificationResult{OK: true, Summary: "good"}, nil
	}

	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("a stalled nudge that recovers on retry must complete, got %s", got.Status)
	}
	if verifyCalls != 2 || spawns != 2 {
		t.Fatalf("want 2 verify calls and 2 spawns, got %d/%d", verifyCalls, spawns)
	}
	if !errors.Is(stallErr, ErrWorkerStalled) {
		t.Fatalf("first session ended with %v, want an error wrapping ErrWorkerStalled", stallErr)
	}
	if !strings.Contains(stallErr.Error(), "session=s-1") {
		t.Fatalf("stall error %q does not name the ended session s-1", stallErr)
	}
	if !strings.Contains(retryHint, "session=s-1") || !strings.Contains(retryHint, ErrWorkerStalled.Error()) {
		t.Fatalf("the retry got hint %q, want the stall of s-1", retryHint)
	}

	taskID := got.Story[0].Tasks[0].ID
	label := prd.ID + "/" + taskID
	tk := nudgeRetryTask(m, prd.ID, taskID)
	if len(tk.NudgeEvents) != 2 {
		t.Fatalf("nudge events = %+v, want the first session's stalled nudge and the retry's sent nudge", tk.NudgeEvents)
	}
	if ev := tk.NudgeEvents[0]; ev.SessionID != "s-1" || ev.Outcome != NudgeOutcomeStalled {
		t.Fatalf("first nudge event = %+v, want session s-1 with outcome stalled", ev)
	}
	if ev := tk.NudgeEvents[1]; ev.SessionID != "s-2" || ev.Outcome != NudgeOutcomeSent {
		t.Fatalf("second nudge event = %+v, want session s-2 with outcome sent", ev)
	}

	// The wait-start line for the first session is written by the post-turn wait
	// in cmd/datawatch, which this package cannot reach; its test checks it there.
	out := logs.String()
	for _, want := range []string{
		fmt.Sprintf("[autonomous] idle worker s-1 task %s: nudge 1 sent after 3m0s idle", label),
		fmt.Sprintf("[autonomous] idle worker s-1 task %s: still silent for 10m0s after nudge; ending session for retry", label),
		fmt.Sprintf("[autonomous] task %s: stall retry 1 of 2 after session s-1 (after completion nudge)", label),
		fmt.Sprintf("[autonomous] idle worker s-2 task %s: nudge 2 sent after 3m0s idle", label),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q\nlog:\n%s", want, out)
		}
	}
}
