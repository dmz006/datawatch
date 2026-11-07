// Capped verification runs: each verification script runs inside the memory
// ceiling resolved for its task's kind and project, either wrapped in a
// systemd user scope or watched by the /proc watchdog when no scope exists.
// How a result is judged is not touched here; only how the script is launched.
package autonomous

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/resources"
)

// Ceiling paths recorded on VerificationResult.CeilingPath.
const (
	VerifyCeilingScope    = "scope"
	VerifyCeilingWatchdog = "watchdog"
	VerifyCeilingNone     = "none"
)

// Seams for tests; production values talk to the host.
var (
	verifyScopeAvailableFn = resources.ScopeAvailable
	verifyProcExistsFn     = func() bool { return resources.ProcFS{Root: "/proc"}.Exists() }
	verifyHostTotalMBFn    = resources.HostTotalMB
	verifyScopeCommandFn   = resources.ScopeCommand
)

// verifyLaunch says how one verification script is wrapped.
type verifyLaunch struct {
	Path      string // VerifyCeiling* constant
	CeilingMB int    // resolved ceiling; 0 when none
	UnitID    string // scope unit id

	// Killed, PeakMB and KilledProcess are set by runVerifyCommand when the
	// watchdog ended the run for passing the ceiling.
	Killed        bool
	PeakMB        int
	KilledProcess string
}

// CeilingKill records a run ended for using more memory than its ceiling.
type CeilingKill struct {
	Process   string `json:"process"`
	PeakMB    int    `json:"peak_mb"`
	CeilingMB int    `json:"ceiling_mb"`
}

// SetResourceCeiling wires the ceiling settings and the resolver that picks
// the per-task-kind / per-project override for a task. A nil resolver means
// no override. Without this call verification scripts run uncapped.
func (m *Manager) SetResourceCeiling(limits config.ResourceLimitsConfig, override func(prd *PRD, t *Task) resources.Override) {
	m.mu.Lock()
	m.resourceLimits = &limits
	m.ceilingOverrideFn = override
	m.mu.Unlock()
}

// SetResourceLimitsSource wires a live reader for the resource_limits settings,
// so a change made through PUT /api/config applies to the next verification
// script and to the status output without a restart. It is the production
// wiring; SetResourceCeiling remains for a fixed snapshot.
func (m *Manager) SetResourceLimitsSource(src func() config.ResourceLimitsConfig, override func(prd *PRD, t *Task) resources.Override) {
	m.mu.Lock()
	m.resourceLimitsFn = src
	if override != nil {
		m.ceilingOverrideFn = override
	}
	m.mu.Unlock()
}

// currentResourceLimits returns the wired settings, or nil when none are wired.
// The caller must hold m.mu.
func (m *Manager) currentResourceLimits() *config.ResourceLimitsConfig {
	if m.resourceLimitsFn != nil {
		l := m.resourceLimitsFn()
		return &l
	}
	return m.resourceLimits
}

// planVerifyLaunch resolves the ceiling and the path for a task's script.
func (m *Manager) planVerifyLaunch(prd *PRD, t *Task) *verifyLaunch {
	m.mu.Lock()
	lim, ovFn := m.currentResourceLimits(), m.ceilingOverrideFn
	m.mu.Unlock()
	if lim == nil || t == nil {
		return nil
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
	var ov resources.Override
	if ovFn != nil {
		ov = ovFn(prd, t)
	}
	total, _ := verifyHostTotalMBFn() // 0 on error: the floor applies
	return chooseVerifyLaunch(resources.ResolveCeiling(total, limits, ov), fmt.Sprintf("%s-%d", t.ID, time.Now().Unix()))
}

// chooseVerifyLaunch picks scope, watchdog or none for a resolved ceiling.
func chooseVerifyLaunch(c resources.Ceiling, unitID string) *verifyLaunch {
	if c.NoCeiling() {
		return &verifyLaunch{Path: VerifyCeilingNone}
	}
	switch resources.SelectStatus(verifyScopeAvailableFn(), verifyProcExistsFn()) {
	case resources.StatusScope:
		return &verifyLaunch{Path: VerifyCeilingScope, CeilingMB: c.MB, UnitID: unitID}
	case resources.StatusWatchdog:
		return &verifyLaunch{Path: VerifyCeilingWatchdog, CeilingMB: c.MB}
	}
	return &verifyLaunch{Path: VerifyCeilingNone}
}

// verifyScriptArgv builds the command for a verification run. The script
// always runs under env -u TMUX -u TMUX_TMPDIR, so nothing it runs can reach
// the operator's tmux server. On the scope path that command is wrapped in
// the systemd-run argument list; on any other path it is returned bare.
func verifyScriptArgv(scriptPath string, l *verifyLaunch) ([]string, string, error) {
	base := []string{"env", "-u", "TMUX", "-u", "TMUX_TMPDIR", "bash", scriptPath}
	return scopedArgv(base, "verify", l)
}

// scopedArgv is the one argument builder shared by verification scripts and
// one-shot guardrail runs: base is wrapped in the systemd-run argument list on
// the scope path and returned bare on any other path. A scope launcher that
// cannot be built falls back to the watchdog when /proc exists.
func scopedArgv(base []string, kind string, l *verifyLaunch) ([]string, string, error) {
	if l == nil {
		return base, "", nil
	}
	if l.Path != VerifyCeilingScope {
		return base, l.Path, nil
	}
	bin, args, err := verifyScopeCommandFn(resources.ScopeSpec{
		ID: l.UnitID, Kind: kind, CeilingMB: l.CeilingMB, Command: base,
	})
	if err != nil {
		if verifyProcExistsFn() {
			l.Path = VerifyCeilingWatchdog
			return base, l.Path, nil
		}
		l.Path, l.CeilingMB = VerifyCeilingNone, 0
		return base, l.Path, nil
	}
	return append([]string{bin}, args...), l.Path, nil
}

// runVerifyCommand runs argv in dir and returns combined output and the
// error from the run. On the watchdog path the run is watched against the
// ceiling; a kill is noted at the end of the output.
func runVerifyCommand(argv []string, dir string, l *verifyLaunch) ([]byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- argv built by verifyScriptArgv from a validated script path
	cmd.Dir = dir
	if l == nil || l.Path != VerifyCeilingWatchdog || l.CeilingMB <= 0 {
		return cmd.CombinedOutput()
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	res, err := resources.Watch(context.Background(), cmd, l.CeilingMB)
	if err == resources.ErrNoCeiling {
		l.Path, l.CeilingMB = VerifyCeilingNone, 0
		return runVerifyUncapped(argv, dir)
	}
	if res.Killed {
		l.Killed, l.PeakMB, l.KilledProcess = true, int(res.PeakKB/1024), res.LargestName
		fmt.Fprintf(&buf, "\nverification script passed its %s MB memory ceiling and was ended\n", strconv.Itoa(l.CeilingMB))
	}
	return buf.Bytes(), err
}

func runVerifyUncapped(argv []string, dir string) ([]byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- see runVerifyCommand
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// recordVerifyCeiling writes the ceiling path onto the task's verification
// result. It never changes the verdict.
func (m *Manager) recordVerifyCeiling(t *Task, l *verifyLaunch) {
	if t == nil || l == nil || l.Path == "" {
		return
	}
	changed := false
	m.store.WithLock(func() {
		if t.Verification != nil {
			t.Verification.CeilingPath, t.Verification.CeilingMB = l.Path, l.CeilingMB
			changed = true
		}
		if l.Killed {
			t.CeilingKill = &CeilingKill{Process: l.KilledProcess, PeakMB: l.PeakMB, CeilingMB: l.CeilingMB}
			changed = true
		}
	})
	if changed {
		_ = m.store.SaveTask(t)
	}
	if l.Killed {
		m.RecordCeilingKill(t.ID, l.KilledProcess, l.PeakMB, l.CeilingMB)
	}
}
