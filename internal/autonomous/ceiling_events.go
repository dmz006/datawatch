package autonomous

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/audit"
)

// NoCeilingProcess is the process name recorded on the Events entry for a run
// that started with no memory ceiling.
const NoCeilingProcess = "none"

// raiseCeilingEvent writes the resource_ceiling_kill Events entry through the
// Manager's audit sink. Nil sinks are skipped.
func (m *Manager) raiseCeilingEvent(details map[string]any) {
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(audit.ActionResourceCeilingKill, details)
	}
}

// RecordCeilingKill writes a resource_ceiling_kill Events entry carrying the
// task, the process name, the peak and the ceiling (both in MB), and raises a
// warning alert through the existing alert sink.
func (m *Manager) RecordCeilingKill(taskID, process string, peakMB, ceilingMB int) {
	if process == "" {
		process = "unknown"
	}
	m.raiseCeilingEvent(map[string]any{
		"task":       taskID,
		"process":    process,
		"peak_mb":    peakMB,
		"ceiling_mb": ceilingMB,
		"reason":     AttemptMemoryKillReason,
	})
	if fn := m.AlertSink(); fn != nil {
		fn("warn", "Task stopped: memory ceiling exceeded", fmt.Sprintf(
			"Task %s was stopped because %s used %d MB, over the %d MB ceiling.",
			taskID, process, peakMB, ceilingMB))
	}
}

// RecordNoCeiling writes the resource_ceiling_kill Events entry for a run that
// started with no ceiling because the host has neither a user scope nor /proc.
func (m *Manager) RecordNoCeiling(taskID string) {
	m.raiseCeilingEvent(map[string]any{
		"task":       taskID,
		"process":    NoCeilingProcess,
		"peak_mb":    0,
		"ceiling_mb": 0,
		"reason":     "no memory ceiling: the host has neither a user scope nor /proc",
	})
}
