// usage_wiring_test.go — the daemon's queue counter for the usage calculator's
// finish-early estimate (design doc decision 6).
package main

import (
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// countedTaskStatuses are the automation task states remaining work can be in: queued,
// approved to run (in progress), and running. Everything else is skipped by the counter.
var countedTaskStatuses = []autonomouspkg.TaskStatus{
	autonomouspkg.TaskQueued,
	autonomouspkg.TaskInProgress,
	autonomouspkg.TaskRunningTests,
	autonomouspkg.TaskVerifying,
}

// uncountedTaskStatuses are automation task states that are not remaining work: held
// (pending, awaiting approval, blocked, waiting for capacity, review) and finished
// (completed, failed, cancelled).
var uncountedTaskStatuses = []autonomouspkg.TaskStatus{
	autonomouspkg.TaskPending,
	autonomouspkg.TaskAwaitingApproval,
	autonomouspkg.TaskBlocked,
	autonomouspkg.TaskWaitingCapacity,
	autonomouspkg.TaskReviewNeeded,
	autonomouspkg.TaskCompleted,
	autonomouspkg.TaskFailed,
	autonomouspkg.TaskCancelled,
}

// usageTestPRD builds one PRD with one story and one task in each of the given task
// states, the fake Automata list the queue counter is checked against.
func usageTestPRD(prdID string, statuses []autonomouspkg.TaskStatus) *autonomouspkg.PRD {
	now := time.Now()
	story := autonomouspkg.Story{ID: prdID + "story1", PRDID: prdID, Title: "usage wiring",
		Status: autonomouspkg.StoryInProgress, CreatedAt: now, UpdatedAt: now}
	for i, status := range statuses {
		story.Tasks = append(story.Tasks, autonomouspkg.Task{
			ID: prdID + "task" + string(rune('a'+i)), StoryID: story.ID, PRDID: prdID,
			Title: string(status), Spec: "a task in the " + string(status) + " state",
			Status: status, CreatedAt: now, UpdatedAt: now,
		})
	}
	return &autonomouspkg.PRD{ID: prdID, Spec: "usage wiring test", ProjectDir: ".",
		Status: autonomouspkg.PRDRunning, CreatedAt: now, UpdatedAt: now, Story: []autonomouspkg.Story{story}}
}

// TestUsagecalcQueueCountsQueuedApprovedRunningOnly checks the daemon queue counter
// (usagecalc.QueueCounter) against a fake Automata list: queued, approved and running
// tasks count once each; pending, awaiting approval, blocked, waiting-capacity,
// review-needed and finished tasks do not count at all.
func TestUsagecalcQueueCountsQueuedApprovedRunningOnly(t *testing.T) {
	mgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.Config{}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	q := usagecalcQueue{m: mgr}

	// An empty Automata list counts zero.
	if n := q.QueuedTaskCount(); n != 0 {
		t.Fatalf("empty queue counted %d, want 0", n)
	}

	prds := map[string][]autonomouspkg.TaskStatus{
		"a1b2c3d4": countedTaskStatuses,
		"a1b2c3d5": uncountedTaskStatuses,
	}
	for id, statuses := range prds {
		prd := usageTestPRD(id, statuses)
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD %s: %v", id, err)
		}
	}

	got := q.QueuedTaskCount()
	want := len(countedTaskStatuses)
	if got != want {
		t.Fatalf("queue counted %d tasks, want %d (only queued, approved and running count)", got, want)
	}

	// A nil manager degrades to zero instead of panicking.
	var nilQueue usagecalcQueue
	if n := nilQueue.QueuedTaskCount(); n != 0 {
		t.Fatalf("nil manager counted %d, want 0", n)
	}
}
