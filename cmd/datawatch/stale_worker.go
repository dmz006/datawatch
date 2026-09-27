package main

import (
	"os"
	"time"
)

// staleWorkerCheck reports whether a running worker's log file has stopped
// growing for at least threshold — genuine evidence the worker is stalled,
// independent of session liveness signals.
//
// The autonomous verify wait loop previously relied only on session state
// (complete/failed/killed) and an opencode/SSE-specific text-pattern scan, with
// no general staleness bound: a session that stopped producing output but
// never exited could wait indefinitely, and the PRD showed "running" with no
// sign anything was wrong. The session-level staleness signal
// (session.UpdatedAt / LastChannelEventAt) is not reliable evidence either — a
// cosmetic tmux pane change (cursor blink, a background LSP process touching
// the terminal) bumps it without any new output, which is exactly what
// happened on PRD 0fb4e302 task "eval DAG node" (2026-09-27): the session's
// last_channel_event_at kept advancing every few minutes while its log file
// had not gained a byte in 50+ minutes. The log file's own mtime is the one
// signal that only moves when the worker actually writes something, so this
// check reads that directly rather than trusting session state.
//
// threshold <= 0 disables the check (matches session.IsStale's convention).
func staleWorkerCheck(logPath string, threshold time.Duration, now time.Time) (stale bool, since time.Duration, err error) {
	if threshold <= 0 || logPath == "" {
		return false, 0, nil
	}
	fi, err := os.Stat(logPath)
	if err != nil {
		return false, 0, err
	}
	since = now.Sub(fi.ModTime())
	return since > threshold, since, nil
}
