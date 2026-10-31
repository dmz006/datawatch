// Failure escalation views — how a task held for operator review shows in the
// Automaton summary, the task listing and the Automation status.
//
// A held task (TaskReviewNeeded) is listed with enough to find it and decide
// what to do: its failure count, the worker of its last attempt and when it was
// held. The full per-attempt report stays on the task (ReviewSummary.Text) and
// is shown in the task listing only as the review_summary text.
package autonomous

import (
	"sort"
	"time"
)

// ReviewNeededEntry is one task held for operator review, as the Automaton
// summary lists it. It carries identity, counts and the worker of the last
// attempt, never the task spec or the attempt report.
type ReviewNeededEntry struct {
	TaskID       string    `json:"task_id"`
	TaskTitle    string    `json:"task_title,omitempty"`
	StoryID      string    `json:"story_id"`
	FailureCount int       `json:"failure_count"`
	LastBackend  string    `json:"last_backend,omitempty"`
	LastModel    string    `json:"last_model,omitempty"`
	HeldAt       time.Time `json:"held_at"`
	// HeldCause names the infrastructure failure when the task is held because
	// its infrastructure retries ran out. It is empty for any other hold.
	HeldCause string `json:"held_cause,omitempty"`
}

// reviewNeededEntries lists the tasks of prd held in TaskReviewNeeded, in story
// order. It returns nil when none is held, so the summary field is omitted.
func reviewNeededEntries(prd *PRD) []ReviewNeededEntry {
	var out []ReviewNeededEntry
	for i := range prd.Story {
		story := &prd.Story[i]
		for j := range story.Tasks {
			if t := &story.Tasks[j]; t.Status == TaskReviewNeeded {
				out = append(out, reviewNeededEntry(story, t))
			}
		}
	}
	return out
}

// reviewNeededEntry builds the summary row for one held task. The failure count
// is the escalation counter, or the count the review report recorded when the
// counter is empty. The worker is the one the last attempt ran on, or the
// task's own worker when no attempt was recorded.
func reviewNeededEntry(story *Story, t *Task) ReviewNeededEntry {
	e := ReviewNeededEntry{
		TaskID:       t.ID,
		TaskTitle:    t.Title,
		StoryID:      story.ID,
		FailureCount: t.EscalationFailures,
		LastBackend:  t.Backend,
		LastModel:    t.Model,
	}
	if rs := t.ReviewSummary; rs != nil {
		e.HeldAt = rs.HeldAt
		if e.FailureCount == 0 {
			e.FailureCount = rs.FailureCount
		}
	}
	if e.FailureCount == 0 {
		e.FailureCount = len(t.Attempts)
	}
	if last := lastTaskAttempt(t); last != nil {
		e.LastBackend, e.LastModel = last.Backend, last.Model
	}
	if fr := t.FailureReason; fr != nil && fr.Source == infraRetryHoldSource {
		e.HeldCause = fr.Detail
	}
	return e
}

// HeldTaskStatus is one task held for operator review, as the daemon status
// lists it across every Automaton. It names the Automaton the task belongs to
// and carries the same row the Automaton summary lists.
type HeldTaskStatus struct {
	AutomatonID    string `json:"automaton_id"`
	AutomatonTitle string `json:"automaton_title,omitempty"`
	ReviewNeededEntry
}

// heldTasksAcrossPRDs lists every task of the given Automata held in
// TaskReviewNeeded. The rows are ordered by when each task was held, oldest
// first, since the oldest hold is the one waiting longest for the operator. It
// returns nil when no task is held. The caller holds the store lock.
func heldTasksAcrossPRDs(prds map[string]*PRD) []HeldTaskStatus {
	var out []HeldTaskStatus
	for _, p := range prds {
		for _, e := range reviewNeededEntries(p) {
			out = append(out, HeldTaskStatus{AutomatonID: p.ID, AutomatonTitle: p.Title, ReviewNeededEntry: e})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].HeldAt.Equal(out[j].HeldAt) {
			return out[i].HeldAt.Before(out[j].HeldAt)
		}
		if out[i].AutomatonID != out[j].AutomatonID {
			return out[i].AutomatonID < out[j].AutomatonID
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out
}

// heldTaskIDs returns the IDs of the tasks of prd held in TaskReviewNeeded, in
// story order. It returns nil when none is held.
func heldTaskIDs(prd *PRD) []string {
	var ids []string
	for _, e := range reviewNeededEntries(prd) {
		ids = append(ids, e.TaskID)
	}
	return ids
}
