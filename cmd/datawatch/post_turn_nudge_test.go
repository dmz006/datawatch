package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// Findings — the post-turn nudge as extracted in post_turn_nudge.go, behaviour
// unchanged from the verify wait loop. Run: go test ./cmd/datawatch/ -run TestPostTurnNudge_
//
// Plain tests:
//   TestPostTurnNudge_WindowsUseDefaultsWhenSettingsUnset  PASS
//   TestPostTurnNudge_OtherFamilyIsNeverNudged             PASS
//   TestPostTurnNudge_WorkChildBlocksTheNudge              PASS
//
// Headline TestPostTurnNudge_FakeClaudeSessionAtEmptyPromptIsNudged  FAIL (by subtest):
//   (a) session visible from the first tick                        PASS
//       Baseline: the nudge fires at the 180s window and is logged as
//       "nudge 1 sent after".
//   (b) session appears a few ticks after the wait starts          FAIL
//       Confirmed cause: the backend family is read once, at wait start.
//       newPostTurnNudge sees no session and returns nil, so no tracker is built,
//       Active() is false and Step is inert for the whole wait. The legacy
//       waiting_input debounce condition (family == claude-code && !Active)
//       is therefore TRUE for this worker: once the delivery grace has passed,
//       three waiting_input ticks stall it (checked by decomposeWaitingInputTick
//       in the subtest, with the grace at zero). The debounce is
//       armed here, not disabled, which corrects the reading that it is off.
//       A/B against (a): the only difference is when the session becomes visible.
//   (c) pane redraws keep the log file fresh while idle            FAIL
//       Confirmed cause: the idle clock is the shorter of the observed output
//       age and the idle run (worker_nudge.go). A redraw touches the log every
//       tick, so the probe reports 2s each time and the idle clock never reaches
//       180s. A/B against (a): the only difference is the redraw.
//   (d) tail echoes the task text and the nudge sentence           PASS
//       Neither counts as completion: the task text's instruction carries the
//       placeholder "<one-sentence summary", and the nudge sentence has no
//       "DATAWATCH_COMPLETE:" prefix.
//   (e) status verifying and only a worker lease                   PASS
//       The unit takes the task's identity and nudge events only; status and
//       leases are not inputs, so nothing here gates it. This shows the unit
//       does not read them. It does not test the production scheduling of the
//       wait while a task is verifying.
//   (f) bare template summary line "DATAWATCH_COMPLETE: <summary...>"  FAIL
//       Confirmed cause: ExtractCompletionSummary (completion_summary.go)
//       returns "<summary of this task>" for this line, because the placeholder
//       check matches only "<one-sentence summary". Any non-empty summary sets
//       the tracker to done, so nudging ends permanently. A/B against (a): the
//       only difference is this line.
//
// Candidates not reproduced by the fake:
//   - A persisted nudge event seeding the tracker as already nudged: the fake
//     task has no NudgeEvents, so SeedNudgedAt is never given a time. Not tested.
//   - The live config snapshot missing the two nudge settings: read, not tested.
//     main.go copies WorkerIdleNudgeSeconds and WorkerIdleNudgeStallSeconds into
//     the live config (main.go:4269-4270), and the windows come from that
//     snapshot. The fake passes autonomouspkg.Config directly.

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

		nudge := newPostTurnNudge(f.deps(), autonomouspkg.Config{}, postTurnTaskFor(autonomouspkg.TaskInProgress))
		if !nudge.Active() {
			// The family was read once, at wait start, while the session was
			// unknown. Without a tracker the nudge never steps, and the legacy
			// waiting_input debounce (family == claude-code && !Active) applies.
			t.Errorf("no nudge tracker: the family was read at wait start, before the session was visible; the legacy debounce condition is true for this worker (family=%q, Active=false)", f.snap.BackendFamily)
			// The debounce the wait then arms: three consecutive waiting_input
			// ticks stall the worker (grace already passed in this scenario).
			ticks, stalled := 0, false
			for i := 0; i < 3; i++ {
				ticks, stalled = decomposeWaitingInputTick(ticks, session.StateWaitingInput, time.Hour, 0)
			}
			t.Logf("legacy debounce armed: three waiting_input ticks stall the worker = %v", stalled)
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
