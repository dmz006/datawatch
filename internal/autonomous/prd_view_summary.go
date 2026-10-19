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
	}

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
		}
		view.Stories = append(view.Stories, summary)
		view.TaskCount += summary.TaskCount
	}
	return view
}
