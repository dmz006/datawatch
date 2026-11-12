package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/costreport"
)

// workerStartupAttempts lists every recorded task attempt as a cost-report
// startup record (profile, role, startup token size and its source), for
// the worker_startup block of the stats snapshot. The attempts are copied
// out under the store lock; the store lock is not re-entrant, so the PRD
// listing runs before it is taken.
func workerStartupAttempts(mgr *autonomous.Manager) []costreport.Attempt {
	out := []costreport.Attempt{}
	if mgr == nil {
		return out
	}
	prds := mgr.Store().ListPRDs()
	mgr.Store().WithLock(func() {
		for _, prd := range prds {
			if prd == nil {
				continue
			}
			for _, story := range prd.Story {
				for _, task := range story.Tasks {
					for _, a := range task.Attempts {
						out = append(out, costreport.Attempt{
							Profile:       a.WorkerProfile,
							Role:          a.WorkerRole,
							StartupTokens: a.StartupTokens,
							Source:        a.StartupTokenSource,
						})
					}
				}
			}
		}
	})
	return out
}
