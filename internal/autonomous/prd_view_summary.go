package autonomous

import "time"

// SummaryView is a read-only summary of a PRD for list and overview
// responses. It carries identity, status, revision, timestamps, decision
// and task counts, and a per-story breakdown of task statuses. It
// deliberately holds no spec text, story or task descriptions, task specs,
// decisions, verdicts, verification text or error text, so its size grows
// with the number of stories and tasks, not with how much text they carry.
type SummaryView struct {
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	ProjectDir    string         `json:"project_dir"`
	Status        PRDStatus      `json:"status"`
	Rev           int            `json:"rev"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DecisionCount int            `json:"decision_count"`
	Stories       []StorySummary `json:"stories"`
	StoryCount    int            `json:"story_count"`
	TaskCount     int            `json:"task_count"`
	// CIWatch is copied unchanged from the PRD. It is small, and it is the
	// automaton's own record of whether CI passed for its pushed commit.
	CIWatch *CIWatchResult `json:"ci_watch,omitempty"`

	// The fields below feed a collapsed Automata row. All are scalars, short
	// lists or small count maps, so none grows with the amount of text the
	// stories and tasks carry.
	Type    string `json:"type,omitempty"`
	Backend string `json:"backend,omitempty"`
	// Tags and PrimaryTagID are always present, so a tag browser reads the
	// same shape for an untagged Automaton as for a tagged one.
	Tags             []string       `json:"tags"`
	PrimaryTagID     string         `json:"primary_tag_id"`
	Cost             float64        `json:"cost"`
	ParentPRDID      string         `json:"parent_prd_id,omitempty"`
	Priority         int            `json:"priority,omitempty"`
	IsTemplate       bool           `json:"is_template,omitempty"`
	AutomationID     string         `json:"automation_id,omitempty"`
	BlockedReason    string         `json:"blocked_reason,omitempty"`
	QualityGateState string         `json:"quality_gate_status,omitempty"`
	TaskStatusCounts map[string]int `json:"task_status_counts"`
	// ReviewNeeded lists the tasks held for operator review (status
	// review_needed), in story order. It is omitted when none is held.
	ReviewNeeded []ReviewNeededEntry `json:"review_needed,omitempty"`
	// NotifyEventsEffective is the Automaton's effective notify-events
	// view (the tier that wins for it), copied from the PRD's read-time
	// copy when the API fill is present.
	NotifyEventsEffective *NotifyEventSet `json:"notify_events_effective,omitempty"`
	// UncommittedWork is the Automaton's own worktree hold (files no single
	// task owns), copied from the PRD. It is omitted when there is none.
	UncommittedWork *UncommittedWorkHold `json:"uncommitted_work,omitempty"`
}

// StorySummary is one story's entry in a SummaryView. TaskStatusCounts maps
// each task status string to the number of tasks in that status, so its
// values sum to TaskCount.
type StorySummary struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Status           StoryStatus    `json:"status"`
	TaskCount        int            `json:"task_count"`
	TaskStatusCounts map[string]int `json:"task_status_counts"`
}

// BuildSummaryView builds a SummaryView for prd. A nil prd yields a zero
// view. DecisionCount is the number of decisions recorded on the PRD; the
// decisions themselves are not copied.
func BuildSummaryView(prd *PRD) SummaryView {
	if prd == nil {
		return SummaryView{}
	}
	view := SummaryView{
		ID:            prd.ID,
		Title:         prd.Title,
		ProjectDir:    prd.ProjectDir,
		Status:        prd.Status,
		Rev:           prd.Rev,
		CreatedAt:     prd.CreatedAt,
		UpdatedAt:     prd.UpdatedAt,
		DecisionCount: len(prd.Decisions),
		Stories:       make([]StorySummary, 0, len(prd.Story)),
		StoryCount:    len(prd.Story),
		CIWatch:       prd.CIWatch,

		Type:             prd.Type,
		Backend:          prd.Backend,
		Tags:             summaryTagIDs(prd.TagIDs),
		PrimaryTagID:     prd.PrimaryTagID,
		ParentPRDID:      prd.ParentPRDID,
		Priority:         prd.Priority,
		IsTemplate:       prd.IsTemplate,
		AutomationID:     prd.AutomationID,
		BlockedReason:    prd.BlockedReason,
		QualityGateState: prd.QualityGateStatus,
		TaskStatusCounts: make(map[string]int),

		// The Automaton's effective notify-events view, set by the API
		// fill on the copy this summary was built from. Nil (omitted) when
		// the input PRD never carried the read-time fill.
		NotifyEventsEffective: prd.NotifyEventsEffective,
		UncommittedWork:       prd.UncommittedWork,
	}
	for i := range prd.Decisions {
		view.Cost += prd.Decisions[i].CostUSD
	}
	view.ReviewNeeded = reviewNeededEntries(prd)

	for i := range prd.Story {
		story := &prd.Story[i]
		summary := StorySummary{
			ID:               story.ID,
			Title:            story.Title,
			Status:           story.Status,
			TaskCount:        len(story.Tasks),
			TaskStatusCounts: make(map[string]int),
		}
		for j := range story.Tasks {
			summary.TaskStatusCounts[string(story.Tasks[j].Status)]++
			view.TaskStatusCounts[string(story.Tasks[j].Status)]++
		}
		view.Stories = append(view.Stories, summary)
		view.TaskCount += summary.TaskCount
	}
	return view
}

// SummaryRow is one row of the PRD list: a SummaryView without the
// per-story breakdown. Its size depends on the PRD's identity fields and
// the number of distinct task statuses, never on how many stories or tasks
// the PRD holds.
type SummaryRow struct {
	SummaryView
	Stories *struct{} `json:"stories,omitempty"`
}

// BuildSummaryRow builds the list row for prd from its SummaryView.
func BuildSummaryRow(prd *PRD) SummaryRow {
	return SummaryRow{SummaryView: BuildSummaryView(prd)}
}

// summaryTagIDs returns the Automaton's tag IDs as a non-nil slice, so an
// untagged Automaton serializes "tags": [] rather than null.
func summaryTagIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
