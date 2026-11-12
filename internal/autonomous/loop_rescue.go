// loop_rescue.go — what happens when the executor's periodic progress poll
// DetectLoop (loop_detect.go)'s the worker session's log and finds a loop.
//
// The rescue has three parts:
//  1. record a "looping" warning on the task (Task.LoopingWarning) and emit
//     a task.looping lifecycle event, so an operator (or an LLM supervisor
//     polling the PRD) can see what the worker is stuck on and that the
//     rescue fired;
//  2. input-capable worker sessions (interactive pi, claude-code,
//     opencode, ...) get one short corrective message via the session
//     manager's SendInput — naming the announced action and telling the
//     worker to call the write tool now;
//  3. one-shot print sessions (pi-prompt: `pi --print`, accepts no typed
//     input) are stopped, and the caller routes the task through the
//     existing retry path with the finding's Hint appended to the task
//     spec (see LoopRetryError + the waitLoop in cmd/datawatch/main.go).
//
// A distinct finding (same LoopKind + same action/tool name) fires at most
// once per session: repeated detections of the same finding on later polls
// report LoopRescueSkipped without re-record, re-emit, re-send, or
// re-stop. A finding whose delivery failed is not considered fired, so the
// next poll may retry it with no event/record side effects on failure.

package autonomous

import (
	"fmt"
)

// LoopRescueAction names what RescueLoop did for one finding.
type LoopRescueAction string

const (
	// LoopRescueSkipped: nothing happened — the finding was not new (already
	// rescued this session), it had no deliverable (no Manager/session), or
	// delivery failed (retryable on the next poll).
	LoopRescueSkipped LoopRescueAction = "skipped"
	// LoopRescueCorrected: the corrective message was delivered to an
	// input-capable worker; the worker keeps running.
	LoopRescueCorrected LoopRescueAction = "corrected"
	// LoopRescueStopped: the one-shot print worker was stopped; the caller
	// must now route the task through the retry path with the finding's Hint
	// appended to the task spec (LoopRetryError).
	LoopRescueStopped LoopRescueAction = "stopped"
)

// Key identifies a distinct finding: its kind together with the action (or
// tool name) it names. Two detections with the same key are the same
// finding; a different key (different kind or different named action) is a
// new finding and fires on its own.
func (f LoopFinding) Key() string { return string(f.Kind) + "\x00" + f.Action }

// Warning is the human-readable text recorded on Task.LoopingWarning when
// the finding fires: it names the loop and the action the worker is stuck
// on, and is explicitly a warning, not a verdict.
func (f LoopFinding) Warning() string {
	switch f.Kind {
	case KindAnnounce:
		return fmt.Sprintf("looping: recent turns only announce their next step (latest: %q) with no tool calls", f.Action)
	case KindRepeat:
		return fmt.Sprintf("looping: repeated identical %s call across recent turns with no progress", f.Action)
	default:
		return fmt.Sprintf("looping: %q (kind %s)", f.Action, f.Kind)
	}
}

// CorrectiveMessage is the short instruction sent to an input-capable
// worker when the finding fires: it names the announced (or repeated)
// action and tells the worker to call the write tool now.
func (f LoopFinding) CorrectiveMessage() string {
	if f.Kind == KindRepeat {
		return fmt.Sprintf("You are looping: you have repeated the %q call with identical arguments and nothing has changed. Stop re-issuing the same call — call the write tool now and actually produce the file or change you described.", f.Action)
	}
	return fmt.Sprintf("You are looping: your recent turns only announce the next step (%q) without acting on it. Stop announcing — call the write tool now and actually create or update the file.", f.Action)
}

// LoopRetryError is the verify-path error for a stopped looping worker. Its
// Error text carries the finding's Hint (naming the action), so the
// executor's stall-retry path (executor.go: errors.Is(err, ErrWorkerStalled)
// → hint = err.Error() → SpawnRequest.RetryHint) appends it to the task
// spec on the next attempt; wrapping ErrWorkerStalled routes it into that
// retry path instead of failing the task.
func LoopRetryError(f LoopFinding) error {
	return fmt.Errorf("worker looping on %q: %s: %w", f.Action, f.Hint, ErrWorkerStalled)
}

// LoopSession is the narrow view of a live worker session the loop rescue
// needs. Tests use a fake; production (cmd/datawatch) adapts a
// *session.Session: SendInput -> session.Manager.SendInput, Stop ->
// session.Manager.Kill, AcceptsInput -> backend family is not a one-shot
// print backend (pi-prompt).
type LoopSession interface {
	// ID identifies the session (dedup key for the fires-once guarantee).
	ID() string
	// AcceptsInput reports whether the session accepts typed input
	// (interactive backends: true; one-shot print backends: false).
	AcceptsInput() bool
	// SendInput delivers the corrective message (input-capable sessions only).
	SendInput(text string) error
	// Stop terminates the session (one-shot sessions).
	Stop() error
}

// RescueLoop applies the loop rescue for one finding the executor's
// periodic progress poll just detected:
//
//  1. input-capable session: send the corrective message naming the
//     announced action and directing the worker to call the write tool now;
//     one-shot print session (no input): stop the worker — the caller then
//     routes the retry through the existing path (LoopRetryError →
//     ErrWorkerStalled) with the finding's Hint appended to its spec;
//  2. record the "looping" warning on the task (Task.LoopingWarning —
//     idempotent with the same poll's RefreshTaskProgress);
//  3. emit a task.looping lifecycle event (KindTaskLooping, lifecycle_event.go)
//     so an operator/supervisor can see what the worker was stuck on.
//
// Delivery is attempted before (2)/(3) so a failed send/stop leaves no
// record, no event, and no fires-once mark — the next poll retries
// cleanly. Fires at most once per distinct finding: once a (session,
// finding) pair has been rescued, further detections of that same finding
// report LoopRescueSkipped with no side effects.
func (m *Manager) RescueLoop(taskID string, s LoopSession, f LoopFinding) (LoopRescueAction, error) {
	if m == nil || s == nil {
		return LoopRescueSkipped, nil
	}
	key := s.ID() + "\x00" + f.Key()

	// Fires-once dedup: once a (session, finding) pair has been rescued,
	// further detections of the same finding are reported as skipped.
	m.mu.Lock()
	if m.loopFired == nil {
		m.loopFired = map[string]bool{}
	}
	if m.loopFired[key] {
		m.mu.Unlock()
		return LoopRescueSkipped, nil
	}
	m.mu.Unlock()

	// 1) Deliver the rescue first — delivery may fail and must not consume
	// the fires-once opportunity, so the next poll can retry it.
	var err error
	if s.AcceptsInput() {
		err = s.SendInput(f.CorrectiveMessage())
	} else {
		err = s.Stop()
	}
	if err != nil {
		return LoopRescueSkipped, err
	}

	m.mu.Lock()
	m.loopFired[key] = true
	m.mu.Unlock()

	t, ok := m.store.GetTask(taskID)
	if !ok {
		// The rescue was already delivered; the caller logs this.
		return LoopRescueSkipped, fmt.Errorf("loop rescue: task %s not found", taskID)
	}

	// 2) Record the "looping" warning on the task (idempotent: the same
	// poll's RefreshTaskProgress already writes the same text).
	if t.LoopingWarning != f.Warning() {
		m.store.MutateTask(taskID, func(tk *Task) { tk.LoopingWarning = f.Warning() })
	}

	// 3) Emit the task.looping lifecycle event (once — on fire, not on skips).
	ev := newTaskEvent(nil, t)
	ev.Kind = KindTaskLooping
	ev.Details = f.Warning()
	m.emit(ev)

	if s.AcceptsInput() {
		return LoopRescueCorrected, nil
	}
	return LoopRescueStopped, nil
}
