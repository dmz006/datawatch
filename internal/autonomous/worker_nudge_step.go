package autonomous

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// WorkerNudgeHooks are the daemon touch points of one idle-nudge decision.
// Observe reads the worker now. Delivery reports whether the task was typed
// into the session and returns its text. SendInput types a nudge. Record
// stores a sent nudge on the task and returns the task's nudge count. Outcome
// stores how the nudge ended: answered or stalled. End kills a stalled worker.
type WorkerNudgeHooks struct {
	Observe   func() WorkerNudgeObservation
	Delivery  func() (delivered bool, task string)
	SendInput func(text, source string) error
	Record    func(at time.Time) (nudgeCount int, err error)
	Outcome   func(outcome string)
	End       func(obs WorkerNudgeObservation)
}

// NudgeStallError is the stall of a worker that stayed silent for the stall
// window after its completion nudge. It wraps ErrWorkerStalled, so the retry
// path handles it like any other stall, and it names the session that was
// ended.
type NudgeStallError struct {
	SessionID string
}

// Error names the session and the stall, in the text the daemon has always
// recorded on the task.
func (e NudgeStallError) Error() string {
	return fmt.Sprintf("worker idle after completion nudge (session=%s): %s", e.SessionID, ErrWorkerStalled)
}

// Unwrap makes errors.Is(err, ErrWorkerStalled) hold for a nudge stall.
func (e NudgeStallError) Unwrap() error { return ErrWorkerStalled }

// StallRetryReason names why a stalled attempt is retried, for the executor's
// retry log line: "after completion nudge" for a nudge stall, otherwise
// "worker stall". The stall's own text is recorded on the task as the hint.
func StallRetryReason(err error) string {
	var nerr NudgeStallError
	if errors.As(err, &nerr) {
		return "after completion nudge"
	}
	return "worker stall"
}

// StepWorkerNudge makes one idle-nudge decision for a worker and carries it
// out. It is shared by the fresh-session wait and the adopted-session wait so
// both nudge, stall and record the same way. Workers of other families are
// left alone without observing them.
//
// A send types the task itself when it was never delivered, otherwise the
// completion nudge, and records it. Output or completion after the nudge
// records the nudge as answered and logs it. A stall records it as stalled,
// ends the worker and returns a NudgeStallError, so the executor's retry path
// takes it. Every other outcome returns nil. label names the task in log lines
// ("PRD/TASK"); sessionID names the worker.
func StepWorkerNudge(tr *WorkerNudgeTracker, now time.Time, family, sessionID, label string, hooks WorkerNudgeHooks) error {
	if family != WorkerNudgeFamily {
		return nil
	}
	obs := hooks.Observe()
	wasDone := tr.done
	action := tr.Observe(now, family, true, obs)
	if !wasDone && tr.done && tr.Nudged() {
		cause := "new output"
		if obs.CompletionSeen {
			cause = "completion marker"
		}
		log.Printf("[autonomous] idle worker %s task %s: nudge answered by %s", sessionID, label, cause)
		if hooks.Outcome != nil {
			hooks.Outcome(NudgeOutcomeAnswered)
		}
	}
	switch action {
	case WorkerNudgeSend:
		delivered, task := hooks.Delivery()
		text, redelivery := NudgeForDelivery(delivered, task)
		source := "idle-nudge"
		if redelivery {
			source = "task-redelivery"
			log.Printf("[autonomous] idle worker %s task %s: task was never delivered — re-sending the task itself", sessionID, label)
		}
		if err := hooks.SendInput(text, source); err != nil {
			log.Printf("[autonomous] idle nudge send failed for %s task %s: %v", sessionID, label, err)
		}
		count, rerr := 0, error(nil)
		if hooks.Record != nil {
			count, rerr = hooks.Record(now)
		}
		if rerr != nil {
			log.Printf("[autonomous] idle worker %s task %s: nudge sent after %s idle; recording it failed: %v", sessionID, label, tr.idleAtSend, rerr)
		} else {
			log.Printf("[autonomous] idle worker %s task %s: nudge %d sent after %s idle", sessionID, label, count, tr.idleAtSend)
		}
	case WorkerNudgeStall:
		silent := now.Sub(tr.nudgedAt)
		log.Printf("[autonomous] idle worker %s task %s: still silent for %s after nudge; ending session for retry", sessionID, label, silent)
		if hooks.Outcome != nil {
			hooks.Outcome(NudgeOutcomeStalled)
		}
		hooks.End(obs)
		return NudgeStallError{SessionID: sessionID}
	}
	return nil
}

// CloseNudgeWait ends nudge tracking because the wait finished on completion.
// A nudge still open (sent, never answered) is recorded as answered and logged
// with the session and task. A wait that already recorded its answer, or that
// never nudged, records nothing.
func CloseNudgeWait(tr *WorkerNudgeTracker, hooks WorkerNudgeHooks, sessionID, label string) {
	if tr == nil || tr.done {
		return
	}
	if tr.Nudged() {
		log.Printf("[autonomous] idle worker %s task %s: nudge answered by session completion", sessionID, label)
		if hooks.Outcome != nil {
			hooks.Outcome(NudgeOutcomeAnswered)
		}
	}
	tr.done = true
}
