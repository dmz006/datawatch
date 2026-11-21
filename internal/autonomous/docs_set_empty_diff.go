package autonomous

import "strings"

// emptyDiffSummaryFragment is the part of the verifier's empty-diff summary
// this package matches on. The daemon package cannot import the verifier text,
// so the fragment is matched literally.
const emptyDiffSummaryFragment = "no changes detected (diff empty and no new files)"

// nothingToDocumentNotePrefix leads the note stored on a docs set task that
// finished with nothing to document.
const nothingToDocumentNotePrefix = "nothing to document: "

// docsSetEmptyDiffFinishable reports whether a failing task is the docs set
// standard task whose attempts ended with an empty diff while every other task
// of its story is completed. Such a task has nothing to document and may finish.
func docsSetEmptyDiffFinishable(prd *PRD, t *Task, lastSummary string) bool {
	if prd == nil || t == nil {
		return false
	}
	marker := TaskTemplateIDMarker(TemplateDocsSet)
	if !strings.Contains(t.Title, marker) && !strings.Contains(t.Spec, marker) {
		return false
	}
	if !strings.Contains(lastSummary, emptyDiffSummaryFragment) {
		return false
	}
	found := false
	for si := range prd.Story {
		story := &prd.Story[si]
		inStory := false
		for ti := range story.Tasks {
			if story.Tasks[ti].ID == t.ID {
				inStory = true
				break
			}
		}
		if !inStory {
			continue
		}
		found = true
		for ti := range story.Tasks {
			other := &story.Tasks[ti]
			if other.ID == t.ID {
				continue
			}
			if other.Status != TaskCompleted {
				return false
			}
		}
	}
	return found
}

// completeDocsSetNothingToDocument finishes the task as completed with a note
// carrying the verifier summary. The task and the Automaton are not held.
func (m *Manager) completeDocsSetNothingToDocument(prd *PRD, t *Task, summary string) error {
	var oldStatus TaskStatus
	m.store.WithLock(func() {
		oldStatus = t.Status
		t.Status = TaskCompleted
		t.Error = nothingToDocumentNotePrefix + summary
	})
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.emitTaskStatus(prd, t, oldStatus, nil, nil)
	return nil
}
