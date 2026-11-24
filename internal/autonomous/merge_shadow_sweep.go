package autonomous

import (
	"context"
	"log"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// mergeTrialSweepAuditAction is the audit event recorded when leftover merge
// trials are removed.
const mergeTrialSweepAuditAction = "autonomous_merge_trial_sweep"

// SweepStaleMergeTrials removes the merge trial worktrees a previous daemon
// left behind (mergeshadow.SweepScratch) and records which. The daemon calls
// it once at start, before anything can begin a new trial. It is not governed
// by a setting: the folders are the daemon's own scratch space. It returns the
// number removed.
func (m *Manager) SweepStaleMergeTrials(ctx context.Context) int {
	removed, err := mergeshadow.SweepScratch(ctx, m.dataDir)
	if err != nil {
		log.Printf("[autonomous] merge trial sweep: %v", err)
	}
	if len(removed) == 0 {
		return 0
	}
	log.Printf("[autonomous] merge trial sweep: removed %d leftover trial folder(s)", len(removed))
	m.mu.Lock()
	audit := m.auditFn
	m.mu.Unlock()
	if audit != nil {
		audit(mergeTrialSweepAuditAction, map[string]any{"removed": removed})
	}
	return len(removed)
}
