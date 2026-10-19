package autonomous

// WriteView is the small summary returned after a write to a PRD.
// It carries the PRD's identity, status and revision, plus the current
// status of the one task or story that changed. It deliberately holds no
// spec text, descriptions, decisions, verdicts or logs, so the response
// stays small no matter how large the PRD is.
type WriteView struct {
	ID                 string      `json:"id"`
	Status             PRDStatus   `json:"status"`
	Rev                int         `json:"rev"`
	ChangedTaskID      string      `json:"changed_task_id,omitempty"`
	ChangedTaskStatus  TaskStatus  `json:"changed_task_status,omitempty"`
	ChangedStoryID     string      `json:"changed_story_id,omitempty"`
	ChangedStoryStatus StoryStatus `json:"changed_story_status,omitempty"`
	KilledSessions     int         `json:"killed_sessions,omitempty"`
	Message            string      `json:"message"`
}

// BuildWriteView builds a WriteView for prd after a change to one task or
// one story. Pass an empty string for taskID or storyID when that kind of
// unit did not change. If an ID is given but no matching unit exists in
// the PRD, its changed_* fields are left empty. The killed_sessions field
// is left at zero; only the cancel action sets it.
func BuildWriteView(prd *PRD, taskID, storyID, message string) WriteView {
	view := WriteView{Message: message}
	if prd == nil {
		return view
	}
	view.ID = prd.ID
	view.Status = prd.Status
	view.Rev = prd.Rev

	for i := range prd.Story {
		story := &prd.Story[i]
		if storyID != "" && story.ID == storyID {
			view.ChangedStoryID = story.ID
			view.ChangedStoryStatus = story.Status
		}
		if taskID == "" {
			continue
		}
		for j := range story.Tasks {
			if story.Tasks[j].ID == taskID {
				view.ChangedTaskID = story.Tasks[j].ID
				view.ChangedTaskStatus = story.Tasks[j].Status
			}
		}
	}
	return view
}
