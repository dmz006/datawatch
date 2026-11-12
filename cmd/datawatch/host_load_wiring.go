package main

import (
	"context"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// hostLoadCheck — the daemon's single host load check instance: assigned
// once in the main.go stats wiring, then read by the stats snapshot, the
// autonomous status (and the autonomous_status MCP tool) through the
// manager provider, and run by startHostLoadCheck's tick loop.
// Package-level because the assignment and the loop sit in different
// blocks of main. Nil until wired.
var hostLoadCheck *autonomouspkg.HostLoadCheck

// hostLoadTickInterval is how often the host load check runs. The configured
// minutes decide how long a breach must last; this only sets the resolution.
const hostLoadTickInterval = time.Minute

// hostLoadConfigFrom converts the live autonomous.host_load_alert setting to
// the check's own config type. Defaults resolve in the check's accessors.
func hostLoadConfigFrom(cfg autonomouspkg.Config) autonomouspkg.HostLoadConfig {
	h := cfg.HostLoadAlert
	return autonomouspkg.HostLoadConfig{
		Mode:     autonomouspkg.HostLoadMode(h.Mode),
		Multiple: h.Multiple,
		Minutes:  h.Minutes,
	}
}

// hostLoadSessionsFrom names each session's folders for the check: the project
// folder is the worktree, the tracking folder the scratch folder. A session in
// a finished state is reported as not running.
func hostLoadSessionsFrom(list []*session.Session) []autonomouspkg.HostLoadSession {
	out := make([]autonomouspkg.HostLoadSession, 0, len(list))
	for _, s := range list {
		if s == nil {
			continue
		}
		hs := autonomouspkg.HostLoadSession{ID: s.FullID}
		switch s.State {
		case session.StateRunning, session.StateWaitingInput, session.StateRateLimited, session.StateStalled:
			hs.Running = true
		}
		if s.ProjectDir != "" {
			hs.WorktreeDirs = []string{s.ProjectDir}
		}
		if s.TrackingDir != "" {
			hs.ScratchDirs = []string{s.TrackingDir}
		}
		out = append(out, hs)
	}
	return out
}

// newHostLoadCheck builds the check over the live config, the session list and
// an alert sink. Every source is read at call time, so a runtime config change
// takes effect on the next tick with no restart.
func newHostLoadCheck(live func() autonomouspkg.Config, sessions func() []*session.Session,
	alert func(title, message string)) *autonomouspkg.HostLoadCheck {
	return &autonomouspkg.HostLoadCheck{
		Config:   func() autonomouspkg.HostLoadConfig { return hostLoadConfigFrom(live()) },
		Sessions: func() []autonomouspkg.HostLoadSession { return hostLoadSessionsFrom(sessions()) },
		Alert:    alert,
	}
}

// startHostLoadCheck runs the host load check on a ticker until ctx ends.
func startHostLoadCheck(ctx context.Context, interval time.Duration, check *autonomouspkg.HostLoadCheck) {
	go func() {
		defer func() { _ = recover() }()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				check.Check()
			}
		}
	}()
}

// hostLoadAlertSink raises the alert in the alert store at warning level.
func hostLoadAlertSink(store *alertspkg.Store) func(title, message string) {
	return func(title, message string) {
		_ = store.Add(alertspkg.LevelWarn, title, message, "")
	}
}
