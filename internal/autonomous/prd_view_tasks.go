package autonomous

// TasksView is a read-only view of a PRD for task-level listings. It embeds
// SummaryView, so its JSON carries the summary fields plus a flat list of
// per-task entries. Each entry holds only identity, status and outcome
// metadata: no task spec, story description or decisions.
type TasksView struct {
	SummaryView
	Tasks []TaskEntry `json:"tasks"`
}

// TaskEntry is one task's row in a TasksView. Error, Verification,
// FailureCategory, SessionID, the failure escalation fields and the
// infrastructure retry fields are omitted when empty. ReviewSummary is the plain-text report of a task held for review.
// Progress and
// LoopingWarning are omitted when unset — i.e. for every task that is not
// actively in progress (the store clears them at persist time), so their
// presence is itself the signal that the task's worker is running right
// now.
type TaskEntry struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Status          TaskStatus        `json:"status"`
	Error           string            `json:"error,omitempty"`
	Verification    *TaskVerification `json:"verification,omitempty"`
	FailureCategory string            `json:"failure_category,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	// EscalationFailures counts failed attempts since the last operator action.
	EscalationFailures int `json:"escalation_failures,omitempty"`
	// AttemptCount is the number of recorded attempts of the task.
	AttemptCount  int    `json:"attempt_count,omitempty"`
	ReviewSummary string `json:"review_summary,omitempty"`
	// InfraRetries is the history of infrastructure retries of the task and
	// InfraRetryCount the consecutive ones in the current incident.
	InfraRetries    []InfraRetryEntry `json:"infra_retries,omitempty"`
	InfraRetryCount int               `json:"infra_retry_count,omitempty"`
	Progress        *TaskProgress     `json:"progress,omitempty"`
	LoopingWarning  string            `json:"looping_warning,omitempty"`
	// UncommittedWork is the hold on a task whose worktree still has
	// uncommitted files; omitted when the task carries none.
	UncommittedWork *UncommittedWorkHold `json:"uncommitted_work,omitempty"`
}

// TaskVerification is the outcome of a task's verification. It deliberately
// omits the issue list and the diff SHA, which can carry code and paths.
type TaskVerification struct {
	OK       bool   `json:"ok"`
	Severity string `json:"severity,omitempty"`
	Summary  string `json:"summary"`
}

// BuildTasksView builds a TasksView for prd. A nil prd yields a zero view.
// Tasks are listed in story order, then in the order they appear within
// each story.
func BuildTasksView(prd *PRD) TasksView {
	if prd == nil {
		return TasksView{}
	}
	view := TasksView{SummaryView: BuildSummaryView(prd)}

	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			view.Tasks = append(view.Tasks, buildTaskEntry(&prd.Story[i].Tasks[j]))
		}
	}
	return view
}

// buildTaskEntry copies the listing fields of one task into a TaskEntry.
func buildTaskEntry(task *Task) TaskEntry {
	entry := TaskEntry{
		ID:              task.ID,
		Title:           task.Title,
		Status:          task.Status,
		Error:           task.Error,
		SessionID:       task.SessionID,
		Progress:        task.Progress,
		LoopingWarning:  task.LoopingWarning,
		UncommittedWork: task.UncommittedWork,
	}
	if v := task.Verification; v != nil {
		entry.Verification = &TaskVerification{
			OK:       v.OK,
			Severity: v.Severity,
			Summary:  v.Summary,
		}
	}
	if task.FailureReason != nil {
		entry.FailureCategory = task.FailureReason.Category
	}
	entry.EscalationFailures = task.EscalationFailures
	entry.AttemptCount = len(task.Attempts)
	if task.ReviewSummary != nil {
		entry.ReviewSummary = task.ReviewSummary.Text
	}
	entry.InfraRetryCount = task.InfraRetryCount
	for _, r := range task.InfraRetries {
		entry.InfraRetries = append(entry.InfraRetries, InfraRetryEntry{
			Number: r.Number, Class: r.Class, Reason: r.Reason,
			At: r.At, DelaySeconds: r.DelaySeconds, NextAt: r.NextAt,
		})
	}
	return entry
}
