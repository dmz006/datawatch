// Failure escalation — holding a task for operator review.
//
// When a task has used up its failed attempts under an enabled failure
// escalation policy, it is not marked failed. holdTaskForReview parks it in
// TaskReviewNeeded with a report of every recorded attempt, writes the
// decision, and raises one operator alert. A held task is neither done nor
// failed: the Run coordinator makes its dependents wait and blocks the
// Automaton for review once nothing else can run. The operator acts through
// ResolveTaskReview. The attempt records the report is built from are kept in
// failure_escalation_attempts.go.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// reviewSummaryMaxChars bounds the whole review report of one task. Each
// attempt is already bounded by the retry prompt's limits; this bound keeps a
// task with many attempts from storing an unbounded report.
const reviewSummaryMaxChars = 24000

// failureEscalationHoldSource is the FailureReason.Source of a held task.
const failureEscalationHoldSource = "failure-escalation-hold"

// reviewNeededDecisionKind is the Decision.Kind written when a task is held.
const reviewNeededDecisionKind = "review_needed"

// buildReviewSummary renders the operator's report for t: a heading with the
// failure count, every recorded attempt in order (worker, duration, outcome,
// files changed and the verifier report), and the total time spent across
// attempts. now is the time the task is held. The text is bounded by
// reviewSummaryMaxChars. It reads t without locking, so callers that share t
// with running goroutines call it under the store lock.
func buildReviewSummary(t *Task, now time.Time) *ReviewSummary {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (%s) is held for operator review after %d failed attempts.\n\n",
		t.ID, t.Title, t.EscalationFailures)
	if len(t.Attempts) == 0 {
		b.WriteString("No attempts were recorded.\n")
	}
	total := 0
	for _, a := range t.Attempts {
		b.WriteString(formatEscalationAttemptFull(a))
		b.WriteString("\n")
		total += a.DurationSeconds
	}
	fmt.Fprintf(&b, "Total time spent: %d s across %d recorded attempts.\n", total, len(t.Attempts))
	return &ReviewSummary{
		HeldAt:       now,
		FailureCount: t.EscalationFailures,
		TotalSeconds: total,
		Text:         truncateEscalationText(b.String(), reviewSummaryMaxChars),
	}
}

// holdTaskForReview parks t in TaskReviewNeeded once its failed attempts have
// reached the escalation limit. lastCategory is the failure category of the
// last attempt and detail its failure text. The task keeps its attempts and
// gets a ReviewSummary, a FailureReason with Source failure-escalation-hold
// and Retryable false, and an Error line. It is saved and announced, the
// failure is recorded as a learning, a review_needed decision is appended to
// the Automaton, and one error alert is raised. It returns the error from
// saving the task; the other side effects are best effort.
func (m *Manager) holdTaskForReview(prd *PRD, t *Task, lastCategory, detail string) error {
	now := m.attemptNow()
	var oldStatus TaskStatus
	var failures int
	var last *TaskAttempt
	m.store.WithLock(func() {
		oldStatus = t.Status
		failures = t.EscalationFailures
		t.Status = TaskReviewNeeded
		t.Error = fmt.Sprintf("held for operator review after %d failed attempts", failures)
		t.ReviewSummary = buildReviewSummary(t, now)
		t.FailureReason = &FailureReason{
			Category: orContentCategory(lastCategory), Detail: detail,
			Source: failureEscalationHoldSource, Retryable: false, OccurredAt: now,
		}
		last = lastTaskAttempt(t)
	})
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	dec := Decision{
		At:    now,
		Kind:  reviewNeededDecisionKind,
		Actor: "autonomous",
		Note:  fmt.Sprintf("task %s held for operator review after %d failed attempts", t.ID, failures),
	}
	if last != nil {
		dec.Backend, dec.Model = last.Backend, last.Model
	}
	if p, ok := m.store.GetPRD(prd.ID); ok {
		m.store.WithLock(func() { p.Decisions = append(p.Decisions, dec) })
		if err := m.store.SavePRD(p); err != nil {
			log.Printf("[autonomous] prd=%s save review_needed decision: %v", prd.ID, err)
		}
	}
	m.emitTaskStatus(prd, t, oldStatus, &dec, nil)
	m.recordFailureLearning(prd, t.ID, lastCategory, detail)
	m.raiseReviewAlert(prd, t, failures, last)
	return nil
}

// lastTaskAttempt returns a copy of the task's most recent attempt, or nil
// when it has none. Callers hold the store lock.
func lastTaskAttempt(t *Task) *TaskAttempt {
	n := len(t.Attempts)
	if n == 0 {
		return nil
	}
	a := t.Attempts[n-1]
	return &a
}

// raiseReviewAlert sends the one operator alert for a task held for review.
// Nothing is sent when no alert sink is wired.
func (m *Manager) raiseReviewAlert(prd *PRD, t *Task, failures int, last *TaskAttempt) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	worker := "none recorded"
	if last != nil {
		worker = escalationWorkerLabel(*last)
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nFailed attempts: %d\nLast worker: %s\n"+
			"Ways to act: retry the task, change its spec, change its worker, "+
			"narrow its scope to fewer files, or cancel it.",
		prd.ID, prd.Title, t.ID, t.Title, failures, worker)
	alert("error", "Automaton task needs review", body)
}

// taskOnReview reports whether t is held in TaskReviewNeeded.
func (m *Manager) taskOnReview(t *Task) (held bool) {
	m.store.WithLock(func() { held = t.Status == TaskReviewNeeded })
	return held
}

// reviewHeldTaskID returns the ID of the first task of prd held for review, or
// "" when no task is held.
func (m *Manager) reviewHeldTaskID(prd *PRD) (id string) {
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if t := &prd.Story[si].Tasks[ti]; id == "" && t.Status == TaskReviewNeeded {
					id = t.ID
				}
			}
		}
	})
	return id
}

// taskHeldIn reports whether task id of prd is held in TaskReviewNeeded.
func (m *Manager) taskHeldIn(prd *PRD, id string) bool {
	t := lookupTask(prd, id)
	return t != nil && m.taskOnReview(t)
}

// waitsOnReview reports whether a dependency of t is held for review, or
// waits behind one (listed in heldIDs). Such a task does not run; it stays
// pending until the held task is resolved.
func (m *Manager) waitsOnReview(prd *PRD, t *Task, heldIDs map[string]bool) bool {
	for _, dep := range t.DependsOn {
		if heldIDs[dep] {
			return true
		}
		if d := lookupTask(prd, dep); d != nil && m.taskOnReview(d) {
			return true
		}
	}
	return false
}

// blockForTaskReview moves the Automaton to PRDBlocked with a reason that names
// the held task. The Run coordinator calls it only after every task that can
// run has run, so the Automaton stays open until the operator acts.
func (m *Manager) blockForTaskReview(prd *PRD, taskID string) {
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		prd.Status = PRDBlocked
		prd.BlockedReason = "task " + taskID + " needs operator review"
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s block for task review: %v", prd.ID, err)
		return
	}
	m.emitPRDStatus(prd, old, nil, nil)
	m.EmitPRDUpdate(prd.ID)
}
