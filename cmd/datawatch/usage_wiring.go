// usage_wiring.go — the daemon-side seam the usage calculator advice service uses to
// price the finish-early estimate (design doc decision 6: the daemon's queued-task count
// times a cost per task).
package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// usagecalcQueue counts the daemon's queued automation tasks for the usagecalc service:
// the tasks that are not finished yet. Completed, failed and cancelled tasks count as
// done; everything else (queued, in progress, running tests, waiting for capacity,
// blocked, parked...) is still work the allowance must carry.
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
		for _, story := range prd.Story {
			for _, task := range story.Tasks {
				switch task.Status {
				case autonomouspkg.TaskCompleted, autonomouspkg.TaskFailed, autonomouspkg.TaskCancelled:
					// finished: not remaining work
				default:
					n++
				}
			}
		}
	}
	return n
}
