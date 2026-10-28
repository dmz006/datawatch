package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// piBackendFamily is the backend family whose own session log drives the
// idle and no-progress decisions.
const piBackendFamily = "pi-prompt"

// piSessionsRoot returns the directory pi writes its session logs under:
// $PI_CODING_AGENT_DIR/sessions, or ~/.pi/agent/sessions.
func piSessionsRoot() string {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// piLogLastEventAge returns how long ago the newest event was written to the
// pi session log of a pi-prompt session started in workDir at started, or 0
// when it is unknown: another backend, no log found, an unreadable log, or a
// log with no timestamped event. Callers then fall back to the terminal
// output signal. A known age is never 0, so a timestamp at or after now
// reports 1ns.
func piLogLastEventAge(backendFamily, sessionsRoot, workDir string, started, now time.Time) time.Duration {
	if backendFamily != piBackendFamily || sessionsRoot == "" || workDir == "" {
		return 0
	}
	path, err := pilog.LocateLog(sessionsRoot, workDir, started)
	if err != nil {
		return 0
	}
	p, err := pilog.Read(path)
	if err != nil || p.LastEventAt.IsZero() {
		return 0
	}
	if age := now.Sub(p.LastEventAt); age > 0 {
		return age
	}
	return time.Nanosecond
}
