package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/resources"
)

// Memory ceiling source labels reported in the status. A project or task kind
// override replaces the default for the tasks it covers; the status carries the
// daemon-wide view, so it reports default or none.
const (
	MemoryCeilingSourceDefault  = "default"
	MemoryCeilingSourceProject  = "project"
	MemoryCeilingSourceTaskKind = "task_kind"
	MemoryCeilingSourceNone     = "none"
)

// MemoryCeilingStatus is the memory ceiling a verification run gets when no
// project or task kind override applies, with where it came from and how it is
// enforced. An MCP client or the REST status reads it to see the effective
// ceiling without reading the settings.
type MemoryCeilingStatus struct {
	// EffectiveMB is the ceiling in MB; 0 when Source is none.
	EffectiveMB int `json:"effective_mb"`
	// Source is default, project, task_kind or none.
	Source string `json:"source"`
	// EnforcementPath is scope (systemd user scope), watchdog (the /proc
	// watchdog) or none (no enforcement available, or no ceiling).
	EnforcementPath string `json:"enforcement_path"`
	// SharePercent, FloorMB and CapMB are the settings the default is computed from.
	SharePercent int `json:"share_percent"`
	FloorMB      int `json:"floor_mb"`
	CapMB        int `json:"cap_mb"`
}

// MemoryCeiling returns the effective memory ceiling status for callers
// outside the package (the session status in the REST API).
func (m *Manager) MemoryCeiling() MemoryCeilingStatus { return m.memoryCeilingStatus() }

// memoryCeilingStatus builds the status block. With no ceiling settings wired,
// verification scripts run uncapped, so the source is none.
func (m *Manager) memoryCeilingStatus() MemoryCeilingStatus {
	m.mu.Lock()
	lim := m.currentResourceLimits()
	m.mu.Unlock()
	if lim == nil {
		return MemoryCeilingStatus{Source: MemoryCeilingSourceNone, EnforcementPath: VerifyCeilingNone}
	}
	limits := config.DefaultResourceLimits()
	if lim.MemorySharePercent > 0 {
		limits.MemorySharePercent = lim.MemorySharePercent
	}
	if lim.MemoryFloorMB > 0 {
		limits.MemoryFloorMB = lim.MemoryFloorMB
	}
	if lim.MemoryCapMB > 0 {
		limits.MemoryCapMB = lim.MemoryCapMB
	}
	total, _ := verifyHostTotalMBFn()
	c := resources.ResolveCeiling(total, limits, resources.Override{})
	l := chooseVerifyLaunch(c, "status")
	return MemoryCeilingStatus{
		EffectiveMB:     c.MB,
		Source:          MemoryCeilingSourceDefault,
		EnforcementPath: l.Path,
		SharePercent:    limits.MemorySharePercent,
		FloorMB:         limits.MemoryFloorMB,
		CapMB:           limits.MemoryCapMB,
	}
}
