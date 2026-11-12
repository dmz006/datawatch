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
	return finishReviewSummary(&b, t, now)
}

// finishReviewSummary appends every recorded attempt of t and the total time
// spent to the heading already written to b, and returns the bounded report.
// It reads t without locking.
func finishReviewSummary(b *strings.Builder, t *Task, now time.Time) *ReviewSummary {
	if len(t.Attempts) == 0 {
		b.WriteString("No attempts were recorded.\n")
	}
	total := 0
	for _, a := range t.Attempts {
		b.WriteString(formatEscalationAttemptFull(a))
		b.WriteString("\n")
		total += a.DurationSeconds
	}
	fmt.Fprintf(b, "Total time spent: %d s across %d recorded attempts.\n", total, len(t.Attempts))
	return &ReviewSummary{
		HeldAt:       now,
		FailureCount: t.EscalationFailures,
		TotalSeconds: total,
		Text:         truncateEscalationText(b.String(), reviewSummaryMaxChars),
	}
}

// reviewParking is what differs between the holds that park a task in
// TaskReviewNeeded: the failure reason, the Error line and the review_needed
// decision note, the report, and any further decisions written with it.
type reviewParking struct {
	// reason is the task's FailureReason; OccurredAt is set when it is parked.
	reason FailureReason
	// describe returns the task's Error line and the review_needed decision
	// note. It runs under the store lock.
	describe func(t *Task) (errText, note string)
	// report builds the review report. It runs under the store lock.
	report func(t *Task, now time.Time) *ReviewSummary
	// extraDecisions are appended to the Automaton after review_needed. Their
	// At is set when they are written.
	extraDecisions []Decision
}

// parkTaskForReview parks t in TaskReviewNeeded as p describes. The task keeps
// its attempts. It is saved and announced, a review_needed decision (and p's
// extra decisions) is appended to the Automaton, and the failure is recorded as
// a learning. It returns a copy of the task's last attempt, or nil, and the
// error from saving the task; the other side effects are best effort. The
// caller raises the alert.
func (m *Manager) parkTaskForReview(prd *PRD, t *Task, p reviewParking) (*TaskAttempt, error) {
	now := m.attemptNow()
	var oldStatus TaskStatus
	var note string
	var last *TaskAttempt
	m.store.WithLock(func() {
		oldStatus = t.Status
		t.Status = TaskReviewNeeded
		t.Error, note = p.describe(t)
		t.ReviewSummary = p.report(t, now)
		reason := p.reason
		reason.Retryable, reason.OccurredAt = false, now
		t.FailureReason = &reason
		last = lastTaskAttempt(t)
	})
	if err := m.store.SaveTask(t); err != nil {
		return last, err
	}
	dec := Decision{
		At:    now,
		Kind:  reviewNeededDecisionKind,
		Actor: "autonomous",
		Note:  note,
	}
	if last != nil {
		dec.Backend, dec.Model = last.Backend, last.Model
	}
	if prdNow, ok := m.store.GetPRD(prd.ID); ok {
		m.store.WithLock(func() {
			prdNow.Decisions = append(prdNow.Decisions, dec)
			for _, extra := range p.extraDecisions {
				extra.At = now
				prdNow.Decisions = append(prdNow.Decisions, extra)
			}
		})
		if err := m.store.SavePRD(prdNow); err != nil {
			log.Printf("[autonomous] prd=%s save review_needed decision: %v", prd.ID, err)
		}
	}
	m.emitTaskStatus(prd, t, oldStatus, &dec, nil)
	m.recordFailureLearning(prd, t.ID, p.reason.Category, p.reason.Detail)
	return last, nil
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
	var failures int
	m.store.WithLock(func() { failures = t.EscalationFailures })
	last, err := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{Category: orContentCategory(lastCategory), Detail: detail, Source: failureEscalationHoldSource},
		describe: func(t *Task) (string, string) {
			return fmt.Sprintf("held for operator review after %d failed attempts", t.EscalationFailures),
				fmt.Sprintf("task %s held for operator review after %d failed attempts", t.ID, t.EscalationFailures)
		},
		report: buildReviewSummary,
	})
	if err != nil {
		return err
	}
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
