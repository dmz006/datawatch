package autonomous

import "time"

// SafeDeleteStats is a snapshot of the safe-to-delete cache counters:
// the number of successful computations (cold and background refreshes),
// the number of failed computations, the number of computations that ran
// longer than safeDeleteSlowWarn, the duration of the most recent
// computation in milliseconds, when the most recent successful one ended,
// and the worktree count of the most recent successful report.
type SafeDeleteStats struct {
	Refreshes       uint64    `json:"refreshes"`
	RefreshFailures uint64    `json:"refresh_failures"`
	SlowReports     uint64    `json:"slow_reports"`
	LastDurationMs  int64     `json:"last_duration_ms"`
	LastComputedAt  time.Time `json:"last_computed_at"`
	Worktrees       int       `json:"worktrees"`
}

// SafeDeleteStats returns a snapshot of the safe-to-delete cache counters
// for this manager. It never creates the cache: when no safe-to-delete
// report has been requested yet it returns the zero value.
func (m *Manager) SafeDeleteStats() SafeDeleteStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.safeDeleteCache == nil {
		return SafeDeleteStats{}
	}
	return m.safeDeleteCache.statsSnapshot()
}
