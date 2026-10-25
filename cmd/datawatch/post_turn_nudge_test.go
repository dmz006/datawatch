package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// Findings — the post-turn nudge of a claude-code worker in the verify wait.
// Run: go test ./cmd/datawatch/ -run TestPostTurnNudge_
//
// Before the fix the headline test failed in three subtests (b, c, f). Each
// confirmed cause and its fix:
//
//   (b) session appears a few ticks after the wait starts — FIXED
//       Cause: the backend family was read once, at wait start. The unit saw no
//       session, built no tracker, and reported Active() false, so the legacy
//       waiting_input debounce (claude-code && !Active) stalled the worker.
//       Fix: post_turn_nudge.go binds the family and the tracker on every tick
//       until the session is visible, and again when its identity changes. The
//       tracker is seeded from the task's persisted nudge for that session.
//       Active() is true while the family is unknown or claude-code.
//   (c) pane redraws keep the log file fresh while the pane is idle — FIXED
//       Cause: the idle clock was the shorter of the idle run and the log age
//       (worker_nudge.go). A redraw touches the log every tick, so the clock
//       never passed 2s. Fix: the idle clock is the idle run; the log age is
//       read only after a nudge, where real output still ends tracking.
//   (f) bare template summary line "DATAWATCH_COMPLETE: <summary...>" — FIXED
//       Cause: ExtractCompletionSummary matched only the placeholder
//       "<one-sentence summary", so "<summary of this task>" counted as a
//       completion and ended nudge tracking. Fix: it skips any marker line whose
//       summary is an angle-bracket template, using isCompletionPlaceholder
//       (the same rule live detection uses).
//
// Passing and kept:
//   (a) session visible from the first tick — the baseline nudge at 180s.
//   (d) the tail echoes the task text and the nudge sentence — neither is a
//       completion (the task echo's placeholder and the nudge sentence has no
//       "DATAWATCH_COMPLETE:" prefix).
//   (e) status verifying and only a worker lease — the unit takes the task's
//       identity and nudge events only, so status and leases cannot gate it.
//
// Regression tests for the other requirements, all in this file unless noted:
//   - the late-session subtests above, plus TestPostTurnNudge_LateSessionAdoptsPersistedNudge
//     and TestPostTurnNudge_ReplacedSessionIsRebound
//   - TestPostTurnNudge_NegativeSettingsUseDefaultWindows (zero and negative settings)
//   - TestPostTurnNudge_NudgeWindowIsShorterThanDefaultIdleLimit
//   - internal/session: TestExtractCompletionSummary (template and echo rows)
//   - internal/autonomous: TestWorkerNudgeIdleClock_* (redraws do not hold the clock)
//
// Not changed: the family the verify wait captures once for its stale-log and
// exit-silence checks (main.go) and the start-confirm tracker still read at wait
// start. They are outside the nudge.

const (
	postTurnSessionID = "host-claude01"
	postTurnPRD       = "prd01"
	postTurnTask      = "task01"
	postTurnTaskText  = "Implement the parser and record the result. When finished, print DATAWATCH_COMPLETE: <one-sentence summary>."
	postTurnTick      = 3 * time.Second // the verify wait loop's tick
	postTurnTicks     = 100             // 300s of wait: past the 180s window
)

var postTurnT0 = time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)

// fakeClock is the injected clock every fake reads.
type fakeClock struct {
	now time.Time
}

// fakeClaudeSession stands in for one claude-code worker session as the
// session manager and the host present it to the nudge: an idle worker at an
// empty prompt, no completion marker, no live work child, its output older than
// the nudge window, and its task typed in. It is visible to the manager from
// visibleFrom on; before that GetSession reports no such session, as it does
// right after a spawn. Nothing here touches tmux or the process table.
type fakeClaudeSession struct {
	clock       *fakeClock
	id          string
	snap        postTurnSnapshot
	visibleFrom time.Time
	output      []string  // pane and log lines, oldest first
	delivered   bool      // the task was typed into the session
	childWork   bool      // a runner child process is doing work
	logMTime    time.Time // last write to the log file
	lastAge     time.Duration
	recorder    fakeNudgeRecorder

	sent    []string
	sources []string
	ended   []session.EndReason
}

// newFakeClaudeSession returns an idle claude-code worker at an empty prompt,
// visible from the start, with its log last written four minutes before the
// wait starts.
func newFakeClaudeSession(clock *fakeClock, family string) *fakeClaudeSession {
	return &fakeClaudeSession{
		clock:       clock,
		id:          postTurnSessionID,
		visibleFrom: clock.now,
		snap: postTurnSnapshot{
			State:         session.StateWaitingInput,
			BackendFamily: family,
			LogFile:       "/home/worker/.datawatch/logs/" + postTurnSessionID + ".log",
			TmuxSession:   "dw-" + postTurnSessionID,
			ProjectDir:    "/home/worker/project",
			Task:          postTurnTaskText,
			FullID:        "host-" + postTurnSessionID,
		},
		output:    idleClaudeTail(),
		delivered: true,
		logMTime:  clock.now.Add(-4 * time.Minute),
	}
}

// idleClaudeTail is a pane whose assistant has finished a turn and is waiting
// at an empty prompt. It holds the task echoed back and the hand-typed nudge
// sentence the way a real pane would, and no real completion line.
func idleClaudeTail() []string {
	return []string{
		"> " + postTurnTaskText,
		"⏺ Parser written; the unit tests pass.",
		"",
		"╭──────────────────────────────────────╮",
		"│ >                                    │",
		"╰──────────────────────────────────────╯",
		"  ? for shortcuts",
	}
}

// Snapshot reports the session as the manager would, once it is visible.
func (f *fakeClaudeSession) Snapshot(id string) (postTurnSnapshot, bool) {
	if id != f.id || f.clock.now.Before(f.visibleFrom) {
		return postTurnSnapshot{}, false
	}
	return f.snap, true
}

// TailOutput returns the last lines of the fake's output, as the manager does.
func (f *fakeClaudeSession) TailOutput(id string, lines int) (string, error) {
	if _, ok := f.Snapshot(id); !ok {
		return "", fmt.Errorf("no session %s", id)
	}
	start := len(f.output) - lines
	if start < 0 {
		start = 0
	}
	return strings.Join(f.output[start:], "\n"), nil
}

// TaskDelivered reports whether the task was typed into the session.
func (f *fakeClaudeSession) TaskDelivered(string) bool { return f.delivered }

// SendInput records what the nudge typed into the session.
func (f *fakeClaudeSession) SendInput(_, text, source string) error {
	f.sent = append(f.sent, text)
	f.sources = append(f.sources, source)
	return nil
}

// EndSession records a kill of the session.
func (f *fakeClaudeSession) EndSession(_ string, why session.EndReason) error {
	f.ended = append(f.ended, why)
	return nil
}

// fakeNudgeRecorder counts the nudges recorded on the task, as the task store
// does, and keeps how each one ended.
type fakeNudgeRecorder struct {
	nudges   []time.Time
	outcomes []string
}

// RecordTaskNudge stores a sent nudge.
func (r *fakeNudgeRecorder) RecordTaskNudge(_, _, _ string, at time.Time) error {
	r.nudges = append(r.nudges, at)
	return nil
}

// TaskNudgeCount reports the nudges recorded so far.
func (r *fakeNudgeRecorder) TaskNudgeCount(_, _ string) int { return len(r.nudges) }

// RecordTaskNudgeOutcome stores how a nudge ended.
func (r *fakeNudgeRecorder) RecordTaskNudgeOutcome(_, _, _, outcome string) error {
	r.outcomes = append(r.outcomes, outcome)
	return nil
}

// deps wires the fake in as the nudge's sessions, recorder and host probes.
func (f *fakeClaudeSession) deps() postTurnDeps {
	return postTurnDeps{
		sessions: f,
		recorder: &f.recorder,
		probes: postTurnProbes{
			hasWork: func(string, string) bool { return f.childWork },
			logAge: func(string) time.Duration {
				f.lastAge = f.clock.now.Sub(f.logMTime)
				return f.lastAge
			},
		},
	}
}

// postTurnTaskFor is the task a wait watches: its session is the fake's.
func postTurnTaskFor(status autonomouspkg.TaskStatus) *autonomouspkg.Task {
	return &autonomouspkg.Task{
		ID:        postTurnTask,
		PRDID:     postTurnPRD,
		SessionID: postTurnSessionID,
		Status:    status,
	}
}

// runPostTurnWait drives a nudge the way the verify wait loop does: one tick
// per postTurnTick, a tick skipped while the session is not visible, then one
// nudge step. onTick runs first on every tick, to model the pane and log
// activity the scenario needs. It returns the first stall error, or nil.
func runPostTurnWait(clock *fakeClock, f *fakeClaudeSession, nudge *postTurnNudge, ticks int, onTick func()) error {
	for i := 0; i < ticks; i++ {
		clock.now = clock.now.Add(postTurnTick)
		if onTick != nil {
			onTick()
		}
		if _, ok := f.Snapshot(f.id); !ok {
			continue
		}
		if err := nudge.Step(clock.now); err != nil {
			return err
		}
	}
	return nil
}

// capturePostTurnLog points the standard logger at a buffer for one test and
// restores the previous output when the test ends.
func capturePostTurnLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// assertNudgedOnce checks that exactly one nudge reached the session, that it
// is the standard idle nudge, and that the log records it for this task.
func assertNudgedOnce(t *testing.T, f *fakeClaudeSession, logBuf *bytes.Buffer) {
	t.Helper()
	if len(f.sent) != 1 {
		t.Fatalf("nudge inputs typed into the session = %d (%q), want exactly 1; last output age the probe saw = %s; log:\n%s",
			len(f.sent), f.sent, f.lastAge, logBuf.String())
	}
	if f.sent[0] != autonomouspkg.WorkerNudgeText {
		t.Fatalf("typed %q, want the standard nudge %q", f.sent[0], autonomouspkg.WorkerNudgeText)
	}
	if f.sources[0] != "idle-nudge" {
		t.Fatalf("nudge source = %q, want idle-nudge", f.sources[0])
	}
	want := fmt.Sprintf("idle worker %s task %s/%s: nudge 1 sent after", postTurnSessionID, postTurnPRD, postTurnTask)
	if !strings.Contains(logBuf.String(), want) {
		t.Fatalf("log has no %q line; log:\n%s", want, logBuf.String())
	}
}

func TestPostTurnNudge_WindowsUseDefaultsWhenSettingsUnset(t *testing.T) {
	nudgeAfter, stallAfter := workerNudgeWindows(autonomouspkg.Config{})
	if nudgeAfter != 180*time.Second {
		t.Errorf("nudge window with unset config = %s, want 180s", nudgeAfter)
	}
	if stallAfter != 600*time.Second {
		t.Errorf("stall window with unset config = %s, want 600s", stallAfter)
	}
}

func TestPostTurnNudge_OtherFamilyIsNeverNudged(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "opencode")

	nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
	if nudge.Active() {
		t.Fatal("a nudge tracker was built for an opencode session; only claude-code is nudged")
	}
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
		t.Fatalf("unexpected stall for a non-claude-code worker: %v", err)
	}
	if len(f.sent) != 0 || len(f.ended) != 0 {
		t.Fatalf("opencode worker was sent %q and ended %d time(s); want neither", f.sent, len(f.ended))
	}
	if strings.Contains(logBuf.String(), "idle worker") {
		t.Fatalf("nudge logged for an opencode worker:\n%s", logBuf.String())
	}
}

func TestPostTurnNudge_WorkChildBlocksTheNudge(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	f.childWork = true // a build or test run is alive under the runner

	nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
	if !nudge.Active() {
		t.Fatal("no nudge tracker for a claude-code session")
	}
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
		t.Fatalf("a worker with live work was stalled: %v", err)
	}
	if len(f.sent) != 0 || len(f.ended) != 0 {
		t.Fatalf("worker with live work was sent %q and ended %d time(s); want neither", f.sent, len(f.ended))
	}
	if strings.Contains(logBuf.String(), "nudge 1 sent") {
		t.Fatalf("nudge logged while work was live:\n%s", logBuf.String())
	}
}

// TestPostTurnNudge_FakeClaudeSessionAtEmptyPromptIsNudged is the headline
// reproduction of the production failure: a claude-code worker idles at an
// empty prompt and never receives its nudge. Every scenario that should fire
// the nudge runs as a subtest below.
func TestPostTurnNudge_FakeClaudeSessionAtEmptyPromptIsNudged(t *testing.T) {
	t.Run("a: session is visible from the first tick", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")

		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})

	t.Run("b: session appears a few ticks after the wait starts", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")
		f.visibleFrom = postTurnT0.Add(3 * postTurnTick) // not visible when the wait starts

		// The session is unknown at wait start. The nudge still owns the worker's
		// idleness, so the legacy waiting_input debounce (claude-code && !Active)
		// must not be armed for it.
		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		if !nudge.Active() {
			t.Fatal("the legacy waiting_input debounce is armed for a claude-code worker whose session was not visible at wait start")
		}
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})

	t.Run("c: pane redraws keep the log file fresh while the pane is idle", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")

		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		redraw := func() { f.logMTime = clock.now.Add(-2 * time.Second) } // a redraw 2s ago every tick
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, redraw); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})

	t.Run("d: the tail echoes the task and the nudge sentence, which are not completions", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")
		f.output = append(f.output,
			"> "+postTurnTaskText,
			autonomouspkg.WorkerNudgeText,
		)

		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})

	t.Run("e: task status is verifying and only a worker lease exists", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")

		// The unit takes the task's identity and nudge events only. Its status
		// and the worker lease are not inputs, so nothing here can gate it.
		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskVerifying))
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})

	t.Run("f: a bare template summary line is not a completion", func(t *testing.T) {
		logBuf := capturePostTurnLog(t)
		clock := &fakeClock{now: postTurnT0}
		f := newFakeClaudeSession(clock, "claude-code")
		f.output = append(f.output, "DATAWATCH_COMPLETE: <summary of this task>")
		// The completion check the nudge tracker reads on every tick.
		tail, _ := f.TailOutput(f.id, 50)
		t.Logf("completion check on the 50-line tail returns %q; the tracker ends nudge tracking on any non-empty summary", session.ExtractCompletionSummary(tail))

		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
			t.Fatalf("unexpected stall: %v", err)
		}
		assertNudgedOnce(t, f, logBuf)
	})
}

// TestPostTurnNudge_LateSessionAdoptsPersistedNudge proves that a nudge already
// recorded for the session is adopted when the session appears late, so the
// worker is not nudged a second time once it is visible.
func TestPostTurnNudge_LateSessionAdoptsPersistedNudge(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	f.visibleFrom = postTurnT0.Add(3 * postTurnTick)

	task := postTurnTaskFor(autonomouspkg.TaskInProgress)
	task.NudgeEvents = []autonomouspkg.NudgeEvent{
		{At: postTurnT0.Add(-time.Minute), Count: 1, SessionID: postTurnSessionID, Outcome: autonomouspkg.NudgeOutcomeSent},
	}
	nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, task)
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
		t.Fatalf("unexpected stall: %v", err)
	}
	if nudge.tracker == nil || !nudge.tracker.Nudged() {
		t.Fatal("the tracker was not built from the persisted nudge once the session became visible")
	}
	if len(f.sent) != 0 {
		t.Fatalf("a worker nudged before its session became visible was nudged again: typed %q; log:\n%s", f.sent, logBuf.String())
	}
}

// TestPostTurnNudge_ReplacedSessionIsRebound proves that when the task's session
// is replaced by a claude-code session, the nudge binds the new session's family
// and nudges it, instead of keeping the family of the session it first saw.
func TestPostTurnNudge_ReplacedSessionIsRebound(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "opencode")

	nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
	if nudge.Active() {
		t.Fatal("a nudge tracker was built for an opencode session")
	}
	replaced := false
	swapSession := func() {
		if replaced || clock.now.Before(postTurnT0.Add(30*time.Second)) {
			return
		}
		replaced = true
		f.snap.BackendFamily = "claude-code"
		f.snap.FullID = "host-" + postTurnSessionID + "-replacement"
	}
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, swapSession); err != nil {
		t.Fatalf("unexpected stall: %v", err)
	}
	if !replaced {
		t.Fatal("the session was never replaced; the scenario did not run")
	}
	assertNudgedOnce(t, f, logBuf)
}

// TestPostTurnNudge_NegativeSettingsUseDefaultWindows proves that zero and
// negative nudge settings give the 180s nudge and 600s stall windows, both in the
// window helper that the fresh and the adopted wait share and on the fresh wait's
// own nudge timing.
func TestPostTurnNudge_NegativeSettingsUseDefaultWindows(t *testing.T) {
	for _, cfg := range []autonomouspkg.Config{
		{WorkerIdleNudgeSeconds: -1, WorkerIdleNudgeStallSeconds: -600},
		{WorkerIdleNudgeSeconds: 0, WorkerIdleNudgeStallSeconds: 0},
	} {
		nudgeAfter, stallAfter := workerNudgeWindows(cfg)
		if nudgeAfter != 180*time.Second || stallAfter != 600*time.Second {
			t.Errorf("windows for %+v = %s and %s, want 180s and 600s", cfg, nudgeAfter, stallAfter)
		}
	}

	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	cfg := autonomouspkg.Config{WorkerIdleNudgeSeconds: -1, WorkerIdleNudgeStallSeconds: -1}
	nudge := newPostTurnNudge(f.deps(), cfg, postTurnTaskFor(autonomouspkg.TaskInProgress))
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, nil); err != nil {
		t.Fatalf("unexpected stall: %v", err)
	}
	assertNudgedOnce(t, f, logBuf)
	if !strings.Contains(logBuf.String(), "sent after 3m0s idle") {
		t.Fatalf("the fresh wait did not nudge at the 180s default window; log:\n%s", logBuf.String())
	}
}

// TestPostTurnNudge_NudgeWindowIsShorterThanDefaultIdleLimit proves that under
// default settings the nudge fires before the session idle detector can end the
// session: the nudge window is shorter than the default idle limit.
func TestPostTurnNudge_NudgeWindowIsShorterThanDefaultIdleLimit(t *testing.T) {
	nudgeAfter, _ := workerNudgeWindows(autonomouspkg.Config{})
	idleLimit := time.Duration(config.NormalizeSessionIdleTimeoutSeconds(0)) * time.Second
	if nudgeAfter >= idleLimit {
		t.Fatalf("nudge window %s is not shorter than the default session idle limit %s; the idle detector would end the session before the nudge", nudgeAfter, idleLimit)
	}
}
