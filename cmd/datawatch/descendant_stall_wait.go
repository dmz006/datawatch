package main

import (
	"strconv"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// workDescendantPIDs returns the PIDs of the worker session's work
// descendants: the same pure walk as hasLiveChildProcess (the runner's
// subtree with the runner itself removed, minus the session's own MCP and
// channel servers and everything below them). It returns nil when the process
// tree cannot be read — no tmux, no pgrep, session gone — so callers treat it
// as an unreadable observation, never as an empty set: a nil observation must
// not touch the stall clock.
func workDescendantPIDs(tmuxSession, projectDir string) map[int]bool {
	roots, tree, ok := sessionProcessTree(tmuxSession)
	if !ok {
		return nil
	}
	return workChildPIDs(roots, tree, resolveRunnerName, readProcessCmdline, sessionOwnMCPServers(projectDir))
}

// descendantStallTick advances the descendant-only stall tracker for one
// wait-loop observation and reports whether it has declared the run stalled.
// Mirrors exitSilenceTick's family gate: it applies to the pi-prompt family
// only, and it returns (false, "") without calling tr.Observe for any other
// family or a nil tracker, so non-pi workers never stall through this
// detector. workDescendants is the number of work descendants the caller's
// walk found on this observation.
func descendantStallTick(tr *autonomouspkg.DescendantStallTracker, backendFamily string, now time.Time,
	workDescendants int,
	logModTime time.Time, workdirNewest time.Time) (stalled bool, reason string) {
	if tr == nil || backendFamily != "pi-prompt" {
		return false, ""
	}
	return tr.Observe(now, workDescendants, logModTime, workdirNewest)
}

// appendDescendantEvidence appends the recorded descendants' identity lines to
// the runner evidence's process list, deduplicated by PID: a recorded PID
// already present in Processes (for example the process walk's own entry) is
// not appended a second time. A nil receiver is tolerated so the call can be
// made unconditionally after newRunnerEvidence.
func appendDescendantEvidence(ev *autonomouspkg.RunnerEvidence, recs map[int]descendantProcess) {
	if ev == nil || len(recs) == 0 {
		return
	}
	seen := make(map[int]bool, len(ev.Processes))
	for _, line := range ev.Processes {
		pid, err := strconv.Atoi(strings.SplitN(line, " ", 2)[0])
		if err == nil {
			seen[pid] = true
		}
	}
	for pid, rec := range recs {
		if rec.Line == "" || seen[pid] {
			continue
		}
		seen[pid] = true
		ev.Processes = append(ev.Processes, rec.Line)
	}
}

// descendantEvidenceLines maps an ended-or-survivor slice onto the PID-keyed
// form appendDescendantEvidence expects.
func descendantEvidenceLines(list []descendantProcess) map[int]descendantProcess {
	if len(list) == 0 {
		return nil
	}
	m := make(map[int]descendantProcess, len(list))
	for _, rec := range list {
		m[rec.PID] = rec
	}
	return m
}
