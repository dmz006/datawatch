package autonomous

import (
	"fmt"
	"log"
	"time"
)

// WorkerNudgeHooks are the daemon touch points of one idle-nudge decision.
// Observe reads the worker now. Delivery reports whether the task was typed
// into the session and returns its text. SendInput types a nudge. Record
// stores a sent nudge on the task. End kills a stalled worker.
type WorkerNudgeHooks struct {
	Observe   func() WorkerNudgeObservation
	Delivery  func() (delivered bool, task string)
	SendInput func(text, source string) error
	Record    func(at time.Time) error
	End       func(obs WorkerNudgeObservation)
}

// StepWorkerNudge makes one idle-nudge decision for a worker and carries it
// out. It is shared by the fresh-session wait and the adopted-session wait so
// both nudge, stall and record the same way. Workers of other families are
// left alone without observing them.
//
// A send types the task itself when it was never delivered, otherwise the
// completion nudge, and records it. A stall ends the worker and returns an
// error wrapping ErrWorkerStalled, so the executor's retry path takes it. Every
// other outcome, including the worker responding after the nudge, returns nil.
// label names the task in log lines; sessionID names the worker.
func StepWorkerNudge(tr *WorkerNudgeTracker, now time.Time, family, sessionID, label string, hooks WorkerNudgeHooks) error {
	if family != WorkerNudgeFamily {
		return nil
	}
	obs := hooks.Observe()
	switch tr.Observe(now, family, true, obs) {
	case WorkerNudgeSend:
		delivered, task := hooks.Delivery()
		text, redelivery := NudgeForDelivery(delivered, task)
		source := "idle-nudge"
		if redelivery {
			source = "task-redelivery"
			log.Printf("[autonomous] idle worker %s task %s: task was never delivered — re-sending the task itself", sessionID, label)
		} else {
			log.Printf("[autonomous] idle worker %s task %s: sending completion nudge", sessionID, label)
		}
		if err := hooks.SendInput(text, source); err != nil {
			log.Printf("[autonomous] idle nudge send failed for %s: %v", sessionID, err)
		}
		if hooks.Record != nil {
			if err := hooks.Record(now); err != nil {
				log.Printf("[autonomous] record nudge for %s (task %s): %v", sessionID, label, err)
			}
		}
	case WorkerNudgeStall:
		log.Printf("[autonomous] idle worker %s task %s still silent after nudge; killing for retry", sessionID, label)
		hooks.End(obs)
		return fmt.Errorf("worker idle after completion nudge (session=%s): %w", sessionID, ErrWorkerStalled)
	}
	return nil
}
