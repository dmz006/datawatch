package main

import (
	"context"
	"fmt"
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
// the restart function is wired.
func guardRestart(restart func(), mgrFn func() restartPolicyManager, setFn func() restartPolicySettings) func() {
	return func() {
		dec := runRestartPolicy(context.Background(), mgrFn(), setFn(), autonomous.RealClock(),
			func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
		if !dec.Proceed {
			fmt.Printf("[restart] not restarting: %s\n", dec.Reason)
			return
		}
		restart()
	}
}

