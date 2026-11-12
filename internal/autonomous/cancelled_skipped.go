package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// cancelledTasksSkippedKind is the Decision kind recorded when the completion
// judge is about to run while some tasks were cancelled.
const cancelledTasksSkippedKind = "cancelled_tasks_skipped"

// completedAndCancelled counts the completed tasks of prd and lists the
// cancelled ones by title (id when untitled).
func completedAndCancelled(prd *PRD) (completed int, cancelled []string) {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			switch t.Status {
			case TaskCompleted:
				completed++
			case TaskCancelled:
				name := strings.TrimSpace(t.Title)
				if name == "" {
					name = t.ID
				}
				cancelled = append(cancelled, name)
			}
		}
	}
	return completed, cancelled
}

// firstBlockingTask is firstNonCompletedTask for the completion path: a
// cancelled task is skipped, not a blocker. It also reports a blocker (task
// id "-", status cancelled) when nothing completed at all.
func firstBlockingTask(prd *PRD) (storyID, taskID string, status TaskStatus) {
	completed := 0
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			switch t.Status {
			case TaskCompleted:
				completed++
			case TaskCancelled:
			default:
				return prd.Story[si].ID, t.ID, t.Status
			}
		}
	}
	if completed == 0 {
		return "", "-", TaskCancelled
	}
	return "", "", ""
}

// recordCancelledTasksSkipped appends one Decision naming the cancelled tasks
// the judge is about to skip. An identical note already on the Automaton is
// not repeated, so a re-fired rollup records it once. Returns whether a
// Decision was added.
func (m *Manager) recordCancelledTasksSkipped(prd *PRD) bool {
	added := false
	m.store.WithLock(func() {
		_, names := completedAndCancelled(prd)
		if len(names) == 0 {
			return
		}
		noun := "tasks were"
		if len(names) == 1 {
			noun = "task was"
		}
		note := fmt.Sprintf("%d cancelled %s skipped: %s", len(names), noun, strings.Join(names, ", "))
		for i := range prd.Decisions {
			if prd.Decisions[i].Kind == cancelledTasksSkippedKind && prd.Decisions[i].Note == note {
				return
			}
		}
		prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: cancelledTasksSkippedKind, Actor: "autonomous", Note: note})
		added = true
	})
	return added
}
