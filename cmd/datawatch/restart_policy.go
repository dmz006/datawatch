package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// restartDrainCap bounds how long a drain waits for running tasks, so a task
// that never ends cannot hold the launch gate shut for good. It is a safety
// cap, not a setting; wait_quiet has its own configured timeout.
const restartDrainCap = 6 * time.Hour

// restartPolicyManager is the slice of the autonomous manager the restart
// entry point needs.
type restartPolicyManager interface {
	RestartInterruptions() autonomous.RestartInterruption
	SetLaunchDraining(on bool)
}

// restartPolicySettings is the live autonomous.restart_* configuration.
type restartPolicySettings struct {
	Policy       string
	QuietTimeout time.Duration
	OnTimeout    string
}

func restartSettingsFrom(a config.AutonomousConfig) restartPolicySettings {
	return restartPolicySettings{
		Policy:       config.NormalizeRestartPolicy(a.RestartPolicy),
		QuietTimeout: time.Duration(config.NormalizeRestartQuietTimeoutSeconds(a.RestartQuietTimeoutSeconds)) * time.Second,
		OnTimeout:    config.NormalizeRestartOnTimeout(a.RestartOnTimeout),
	}
}

// runRestartPolicy applies autonomous.restart_policy to a requested restart.
// It logs what the restart would interrupt and returns that count with the
// decision. With no manager there is nothing to wait for, so it proceeds.
func runRestartPolicy(ctx context.Context, mgr restartPolicyManager, set restartPolicySettings, clk autonomous.Clock, logf func(string, ...any)) autonomous.RestartDecision {
	if mgr == nil {
		return autonomous.RestartDecision{Proceed: true, Policy: config.RestartPolicyImmediate, Reason: "no autonomous manager"}
	}
	if set.Policy == config.RestartPolicyDrain {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, restartDrainCap)
		defer cancel()
	}
	return autonomous.ApplyRestartPolicy(ctx, autonomous.RestartPolicyDeps{
		Policy:         set.Policy,
		QuietTimeout:   set.QuietTimeout,
		OnTimeout:      set.OnTimeout,
		Clock:          clk,
		Count:          mgr.RestartInterruptions,
		SetLaunchDrain: mgr.SetLaunchDraining,
		Logf:           logf,
	})
}

// guardRestart wraps a restart function so the restart policy runs first. The
// restart function itself is not changed. mgrFn and setFn are read at call
// time, because the autonomous manager and the live config are created after
// the restart function is wired. A caller with no force concept gets the
// guard with force unset, so a restart that would interrupt running work is
// refused.
func guardRestart(restart func(), mgrFn func() restartPolicyManager, setFn func() restartPolicySettings) func() {
	forced := guardRestartForce(func(bool) { restart() }, mgrFn, setFn)
	return func() { forced(false) }
}

// guardRestartForce is guardRestart with the force flag. When the interruption
// report says NeedsForce and force is not set, the restart is not performed:
// the guard prints the report and returns. With force set, or when NeedsForce
// is false, the restart policy runs and then restart(force) is called.
func guardRestartForce(restart func(force bool), mgrFn func() restartPolicyManager, setFn func() restartPolicySettings) func(force bool) {
	return func(force bool) {
		logf := func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
		if mgr := mgrFn(); !force && mgr != nil {
			if rep := mgr.RestartInterruptions(); rep.NeedsForce {
				fmt.Print(restartGuardReport(rep))
				fmt.Printf("[restart] not restarting: running work would be interrupted; pass force to restart anyway\n")
				return
			}
		}
		dec := runRestartPolicy(context.Background(), mgrFn(), setFn(), autonomous.RealClock(), logf)
		if !dec.Proceed {
			fmt.Printf("[restart] not restarting: %s\n", dec.Reason)
			return
		}
		restart(force)
	}
}

// restartGuardReport renders the read-only report a refused restart prints:
// the planning Automata, verifying tasks, running judges, running tasks and
// live sessions a restart would interrupt, the policy, and how to pass force.
func restartGuardReport(rep autonomous.RestartInterruption) string {
	return restartGuardReportWith(rep, `POST /api/restart?force=true (or {"force": true})`)
}

// restartGuardReportWith is restartGuardReport with the line that says how to
// restart anyway supplied by the surface that prints it.
func restartGuardReportWith(rep autonomous.RestartInterruption, howToForce string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[restart] refused: a restart now would interrupt work that needs force\n")
	fmt.Fprintf(&b, "  restart policy: %s (quiet timeout %ds, on timeout %s)\n", rep.Policy, rep.QuietTimeoutSec, rep.OnTimeout)
	fmt.Fprintf(&b, "  planning Automata (%d):\n", len(rep.PlanningAutomata))
	for _, p := range rep.PlanningAutomata {
		fmt.Fprintf(&b, "    - %s %s\n", p.ID, p.Title)
	}
	fmt.Fprintf(&b, "  verifying tasks (%d):\n", len(rep.VerifyingTasks))
	for _, t := range rep.VerifyingTasks {
		fmt.Fprintf(&b, "    - %s/%s session=%s verifier=%s covered=%t\n", t.PRDID, t.TaskID, t.SessionID, t.VerifierSessionID, t.Covered)
	}
	fmt.Fprintf(&b, "  running judges (%d):\n", len(rep.RunningJudges))
	for _, j := range rep.RunningJudges {
		fmt.Fprintf(&b, "    - session=%s unit=%s covered=%t\n", j.SessionID, j.UnitID, j.Covered)
	}
	fmt.Fprintf(&b, "  running tasks (%d):\n", len(rep.RunningTasks))
	for _, t := range rep.RunningTasks {
		fmt.Fprintf(&b, "    - %s/%s status=%s session=%s\n", t.PRDID, t.TaskID, t.Status, t.SessionID)
	}
	fmt.Fprintf(&b, "  live sessions (%d): %s\n", len(rep.LiveSessions), strings.Join(rep.LiveSessions, ", "))
	fmt.Fprintf(&b, "  to restart anyway: %s\n", howToForce)
	return b.String()
}
