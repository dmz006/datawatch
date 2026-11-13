package autonomous

import "time"

// SummaryView is a read-only summary of a PRD for list and overview
// responses. It carries identity, status, revision, timestamps, decision
// and task counts, and a per-story breakdown of task statuses. It
// deliberately holds no spec text, story or task descriptions, task specs,
// decisions, verdicts, verification text or error text, so its size grows
// with the number of stories and tasks, not with how much text they carry.
type SummaryView struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	ProjectDir string `json:"project_dir"`
	// Project is the derived, read-only project alias (ResolveProjectAlias).
	// It is filled by the server when a list or detail is read and never
	// stored; empty when the record belongs to no registered project.
	Project       string         `json:"project,omitempty"`
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
	// PlanningFailure is the planning failure view (PlanningFailureView),
	// filled by the server on read and never stored; nil when the last
	// planning attempt did not fail.
	PlanningFailure *PlanningFailure `json:"planning_failure,omitempty"`
	// PushState / PushWarning surface the PRD's push outcome even in the
	// trimmed summary view, so list reads and the MCP default detail
	// ("summary") show pushed / not pushed state without a full fetch.
	// Empty when no push outcome was recorded yet.
	PushState   string `json:"push_state,omitempty"`
	PushWarning string `json:"push_warning,omitempty"`
	// Parallel-execution metrics copied from the PRD record.
	TotalTasks    int     `json:"total_tasks,omitempty"`
	ReadOnlyTasks int     `json:"read_only_tasks,omitempty"`
	Speedup       float64 `json:"speedup,omitempty"`

	// The shipped state is copied straight from the PRD: the release these
	// changes were merged into, the merge time, the merge commit, and the
	// origin of the record (ShippedSourceMergeStep or ShippedSourceBackfill).
	// Empty when the Automaton has never been recorded as shipped; see PRD
	// .IsShipped. Shipped state is orthogonal to Status and never changes it.
	MergedInRelease string     `json:"merged_in_release,omitempty"`
	MergedAt        *time.Time `json:"merged_at,omitempty"`
	MergeCommit     string     `json:"merge_commit,omitempty"`
	ShippedSource   string     `json:"shipped_source,omitempty"`

	// Shipped flags that the record carries shipped state (a non-empty merge
	// commit): a fact about the record, independent of any setting.
	// EffectiveStatus is the record's *display* state: "shipped" (state
	// present and the autonomous.shipped.enabled setting on), otherwise the
	// stored status. The stored Status never moves because of shipped state.
	Shipped         bool   `json:"shipped"`
	EffectiveStatus string `json:"effective_status"`

	// The fields below feed a collapsed Automata row. All are scalars, short
	// lists or small count maps, so none grows with the amount of text the
	// stories and tasks carry.
	Type    string `json:"type,omitempty"`
	Backend string `json:"backend,omitempty"`
	// Tags and PrimaryTagID are always present, so a tag browser reads the
	// same shape for an untagged Automaton as for a tagged one.
	Tags         []string `json:"tags"`
	PrimaryTagID string   `json:"primary_tag_id"`
	// SystemTags is the placement-derived system-tag set this Automaton
	// carries (its project, Automation and itself), copied from the PRD. It
	// feeds the object tag filter alongside Tags (the managed TagIDs) above
	// and names the object for the PWA tag browser.
	SystemTags []string `json:"system_tags,omitempty"`
	// InheritedTags is the read-only union of the system tags (placement
	// tags) carried by the sessions owned by this Automaton: a session
	// belongs to the Automaton when it carries this PRD's ID (Session.PRDID).
	// It is computed on every read from the session records, never stored on
	// the PRD and never editable — Tags above is the only tag list an
	// Automaton carries. Absent when no owned session carries a system tag.
	InheritedTags []string `json:"inherited_tags,omitempty"`
	Cost          float64  `json:"cost"`
	ParentPRDID   string   `json:"parent_prd_id,omitempty"`
	Priority      int      `json:"priority,omitempty"`
	IsTemplate    bool     `json:"is_template,omitempty"`
	AutomationID  string   `json:"automation_id,omitempty"`
	BlockedReason string   `json:"blocked_reason,omitempty"`
	// RulesReviewOutcome is the structured record of the last blocked
	// completion rules review (findings and recovery action), copied from
	// the PRD. It is omitted when there is none.
	RulesReviewOutcome *RulesReviewOutcome `json:"rules_review_outcome,omitempty"`
	QualityGateState   string              `json:"quality_gate_status,omitempty"`
	TaskStatusCounts   map[string]int      `json:"task_status_counts"`
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

	// ReviewState is the review view derived from the verdicts recorded on
	// the PRD and its stories and tasks (see ReviewStateOf): the state
	// (passed/blocked/timed_out/pending), the one-line cause, the action
	// the next operator should take, and the judge attempts behind the
	// deciding verdict. Always present (never omitempty) — a list row
	// with no verdicts serializes pending — and derived at build time,
	// never stored.
	ReviewState ReviewResult `json:"review_state"`
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
		// A nil PRD has no verdicts: the review stands at pending, which a
		// list row must still say (ReviewState never omits).
		return SummaryView{ReviewState: ReviewResult{State: ReviewStatePending}}
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
		// Derived once for the row and carried along: it is a scalar-ish
		// view of the verdicts this summary deliberately does not copy.
		ReviewState:   ReviewStateOf(prd),
		PushState:     prd.Git.PushState,
		PushWarning:   prd.Git.PushWarning,
		TotalTasks:    prd.TotalTasks,
		ReadOnlyTasks: prd.ReadOnlyTasks,
		Speedup:       prd.Speedup,

		MergedInRelease: prd.MergedInRelease,
		MergedAt:        prd.MergedAt,
		MergeCommit:     prd.MergeCommit,
		ShippedSource:   prd.ShippedSource,
		// Display state assuming the shipped surface is enabled (built
		// without a config here): shipped records show "shipped". The REST
		// layer folds the live autonomous.shipped.enabled setting in via
		// ApplyShippedSettings.
		Shipped:         prd.IsShipped(),
		EffectiveStatus: PRDEffectiveDisplayStatus(prd, true),

		Type:               prd.Type,
		Backend:            prd.Backend,
		Tags:               summaryTagIDs(prd.TagIDs),
		PrimaryTagID:       prd.PrimaryTagID,
		SystemTags:         prd.SystemTags,
		ParentPRDID:        prd.ParentPRDID,
		Priority:           prd.Priority,
		IsTemplate:         prd.IsTemplate,
		AutomationID:       prd.AutomationID,
		BlockedReason:      prd.BlockedReason,
		RulesReviewOutcome: prd.RulesReviewOutcome,
		QualityGateState:   prd.QualityGateStatus,
		TaskStatusCounts:   make(map[string]int),

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

// ApplyShippedSettings folds the autonomous.shipped.enabled setting into the
// view's display state: with the shipped surface enabled a record that
// carries shipped state displays as "shipped"; with it disabled the display
// state falls back to the stored status even though the record still carries
// the shipped fields. The Shipped flag and the four shipped fields never
// change — they describe the record, not the settings.
func (v *SummaryView) ApplyShippedSettings(enabled bool) {
	if v == nil {
		return
	}
	if enabled && v.Shipped {
		v.EffectiveStatus = ShippedDisplayStatus
		return
	}
	v.EffectiveStatus = string(v.Status)
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
