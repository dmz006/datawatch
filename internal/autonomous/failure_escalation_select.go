// Failure escalation — worker selection for the next attempt.
//
// After a failed attempt, the next attempt runs on the worker named by the
// effective policy's ladder of steps. selectEscalationStep maps the number of
// failed attempts so far onto that ladder, and recordFailureEscalationDecision
// writes the audit entry when the worker changes. The policy model lives in
// failure_escalation.go; the executor applies the chosen step in executeOne.
package autonomous

import (
	"fmt"
	"log"
	"time"
)

// selectEscalationStep returns the ladder step that the next attempt runs on,
// given the number of failed attempts recorded since the last operator action.
// Step i covers its Attempts consecutive failed attempts (0 reads as 1), and
// the last step covers every attempt past the end of the ladder. index is the
// step's position in eff.Steps.
//
// ok is false when no step applies: the ladder is empty, or the chosen step
// has no backend, so the attempt uses the task's normally resolved worker. A
// step with a backend and no model is ok; its model stays empty and does not
// cascade from another level.
func selectEscalationStep(eff EffectiveFailureEscalation, failures int) (step FailureEscalationStep, index int, ok bool) {
	if len(eff.Steps) == 0 {
		return FailureEscalationStep{}, 0, false
	}
	if failures < 0 {
		failures = 0
	}
	index = len(eff.Steps) - 1
	covered := 0
	for i, s := range eff.Steps {
		n := max(s.Attempts, 1)
		if failures < covered+n {
			index = i
			break
		}
		covered += n
	}
	step = eff.Steps[index]
	return step, index, step.Backend != ""
}

// recordFailureEscalationDecision appends a failure_escalation entry to the
// Automaton's decision log when an attempt moves the task to a different
// worker. It reads the PRD fresh from the store, so it is safe while other
// tasks of the same PRD are running, and it never changes the task.
func (m *Manager) recordFailureEscalationDecision(prdID, taskID, note, backend, model string) {
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{
			At:      time.Now(),
			Kind:    "failure_escalation",
			Actor:   "autonomous",
			Backend: backend,
			Model:   model,
			Note:    fmt.Sprintf("task %s: %s", taskID, note),
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save failure_escalation decision: %v", prdID, err)
	}
}

// escalationFailureCount returns how many failed attempts the task has
// recorded since the last operator action.
func (m *Manager) escalationFailureCount(t *Task) int {
	var n int
	m.store.WithLock(func() { n = t.EscalationFailures })
	return n
}

// escalationWorkerChangeNote returns the Decision note for an attempt that
// runs on backend/model when the task's most recent recorded attempt ran on a
// different worker. stepIdx is the ladder step the attempt uses, or -1 when it
// uses the normally resolved worker. It returns "" when there is no earlier
// attempt or the worker is unchanged.
func (m *Manager) escalationWorkerChangeNote(t *Task, backend, model string, stepIdx int) string {
	var last *TaskAttempt
	m.store.WithLock(func() {
		if n := len(t.Attempts); n > 0 {
			a := t.Attempts[n-1]
			last = &a
		}
	})
	if last == nil || (last.Backend == backend && last.Model == model) {
		return ""
	}
	next := "the task's normally resolved worker"
	if stepIdx >= 0 {
		next = fmt.Sprintf("step %d", stepIdx+1)
	}
	return fmt.Sprintf("attempt %d failed on %s; next attempt uses %s: %s",
		last.Number, escalationWorkerPair(last.Backend, last.Model), next, escalationWorkerPair(backend, model))
}

// escalationWorkerPair renders a worker as backend/model for a Decision note.
// An empty model reads as the backend's default model.
func escalationWorkerPair(backend, model string) string {
	if model == "" {
		return backend + " (default model)"
	}
	return backend + "/" + model
}
