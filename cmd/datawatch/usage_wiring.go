// usage_wiring.go — the daemon-side seam the usage calculator advice service uses to
// price the finish-early estimate (design doc decision 6: the daemon's queued-task count
// times a cost per task).
package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// usagecalcQueue counts this daemon's queued automation tasks for the usagecalc service.
// Only tasks that are queued, approved or running count as remaining work: queued, in
// progress, running tests and verifying. Completed, failed and cancelled tasks are
// finished, and skipped tasks are dropped; held work that is not running yet (pending,
// awaiting approval, blocked, waiting for capacity, held for review) is not counted.
type usagecalcQueue struct {
	m *autonomouspkg.Manager
}

// QueuedTaskCount implements usagecalc.QueueCounter (named to satisfy its method set;
// the count spans stories of every PRD in the autonomous store).
func (q usagecalcQueue) QueuedTaskCount() int {
	if q.m == nil || q.m.Store() == nil {
		return 0
	}
	n := 0
	for _, prd := range q.m.Store().ListPRDs() {
		if prd == nil {
			continue
		}
		for _, story := range prd.Story {
			for _, task := range story.Tasks {
				switch task.Status {
				case autonomouspkg.TaskQueued, autonomouspkg.TaskInProgress,
					autonomouspkg.TaskRunningTests, autonomouspkg.TaskVerifying:
					n++
				}
			}
		}
	}
	return n
}
