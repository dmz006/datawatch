// Failure escalation views — how a task held for operator review shows in the
// Automaton summary, the task listing and the Automation status.
//
// A held task (TaskReviewNeeded) is listed with enough to find it and decide
// what to do: its failure count, the worker of its last attempt and when it was
// held. The full per-attempt report stays on the task (ReviewSummary.Text) and
// is shown in the task listing only as the review_summary text.
package autonomous

import "time"

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
	return e
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
