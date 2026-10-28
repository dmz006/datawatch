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

// descendantOutputObservation remembers the wait loop's last look at the
// pane and the channel so that fresh output — the spec's "output resets the
// silence clock" liveness signal — resets a worker's DescendantStallTracker
// before the next Observe. The pane is the worker's own stdout (tmux
// scrollback); the channel event time is the model's output. Neither is
// available to internal/autonomous, so the wait loop feeds this observation
// and resets the tracker when either side has moved since the last look.
type descendantOutputObservation struct {
	lastLookAt  time.Time
	lastPaneLen int
}

// outputIsFresh reports whether, since the last look, the pane grew or the
// channel emitted a newer event, and then records this look (lastLookAt =
// now; lastPaneLen = the larger of the previous and the current value, so a
// temporarily empty pane from a failed capture does not clear the baseline
// and a later full pane counts as growth).
//
// The very first observation of a zero-value reporter never reports fresh:
// the clock must not be reset by output that is already old, which would
// silently extend the silence window. A nil pane and a zero channel time
// (both available) report not-fresh without touching the baseline.
func (o *descendantOutputObservation) outputIsFresh(now time.Time, pane string, channelEventAt time.Time) bool {
	fresh := false
	paneGrew := pane != "" && len(pane) > o.lastPaneLen
	if !o.lastLookAt.IsZero() {
		fresh = paneGrew
		if !channelEventAt.IsZero() && channelEventAt.After(o.lastLookAt) {
			fresh = true
		}
	}
	o.lastLookAt = now
	if paneGrew {
		o.lastPaneLen = len(pane)
	}
	return fresh
}

// reset discards the baseline so the next observation re-establishes it
// without reporting fresh against output we have already accounted for.
// The wait loop calls it alongside DescendantStallTracker.Reset when the
// session is parked on an external condition, so the parked session's output
// does not double-reset the clock on re-admission.
func (o *descendantOutputObservation) reset() {
	o.lastLookAt = time.Time{}
	o.lastPaneLen = 0
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

// descendantWaitStep is one wait-loop observation of the descendant-only stall
// detector, in the order the loop runs it: fresh output (pane growth, or a
// channel event newer than the last look) resets the silence clock first, then
// the tracker observes the log and work-directory mtimes. paneOK is false when
// the pane capture failed, which counts as not-fresh, never as a reset.
// workDescendants is the size of a readable work-descendant set; the caller
// makes no observation at all when the tree cannot be read.
func descendantWaitStep(tr *autonomouspkg.DescendantStallTracker, obs *descendantOutputObservation, backendFamily string,
	now time.Time, workDescendants int, logModTime, workdirNewest time.Time,
	pane string, paneOK bool, channelEventAt time.Time) (stalled bool, reason string) {
	if tr == nil || backendFamily != "pi-prompt" {
		return false, ""
	}
	if paneOK && obs.outputIsFresh(now, pane, channelEventAt) {
		tr.Reset(now)
	}
	return descendantStallTick(tr, backendFamily, now, workDescendants, logModTime, workdirNewest)
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
