package autonomous

import (
	"fmt"
	"time"
)

// recordAttemptSnapshot appends ref to the task's AttemptRefs, in order. An
// empty ref (SnapshotAttemptWork found nothing to preserve) is ignored, so the
// list only holds refs that exist in the repository.
func recordAttemptSnapshot(t *Task, ref string) {
	if t == nil || ref == "" {
		return
	}
	t.AttemptRefs = append(t.AttemptRefs, ref)
}

// RecordTaskAttemptSnapshot is the daemon-internal hook the autonomous verifier
// calls right before it kills a stalled worker whose work was snapshotted. It
// appends ref to the stored task and persists the PRD, mirroring
// RecordTaskAttemptCommit.
func (m *Manager) RecordTaskAttemptSnapshot(prdID, taskID, ref string) error {
	if m == nil {
		return fmt.Errorf("autonomous manager not available")
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				recordAttemptSnapshot(&prd.Story[si].Tasks[ti], ref)
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}
