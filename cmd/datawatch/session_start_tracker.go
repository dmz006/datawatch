package main

import (
	"os"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionStartTracker is the start-confirm clock of one autonomous wait loop.
// A claude-code worker that never started parks in waiting_input without an
// assistant turn, but a worker mid tool call also shows no assistant bullet
// while it is busy. The bound therefore runs from the last evidence of work
// (autonomouspkg.StartEvidence), not from the first silent tick, so only real
// silence longer than the limit ends a session. The tracker reads no tmux
// state itself: the wait loop passes each tick's pane, state and probes in,
// which keeps it testable with a fake clock.
type sessionStartTracker struct {
	limit          time.Duration
	lastEvidenceAt time.Time    // latest tick with evidence of work; zero until one is seen
	silentSince    time.Time    // first silent waiting_input tick; the anchor until evidence is seen
	prevPane       string       // pane captured on the previous observation
	havePrevPane   bool         // prevPane holds a capture a later pane can be compared with
	lastChannelAt  time.Time    // session LastChannelEventAt as of the previous observation
	prevChildren   map[int]bool // runner-started pids on the previous observation
	haveChildren   bool         // prevChildren is a baseline a later set can be compared with
}

// sessionStartInput is one tick's view of the worker, gathered by the wait loop.
type sessionStartInput struct {
	Now       time.Time
	State     session.State
	Pane      string    // visible pane capture; empty when the capture failed
	Delivered bool      // the task prompt was typed into the session
	ChannelAt time.Time // session LastChannelEventAt
	// ChildPIDs are the live processes the runner started (never the runner
	// itself). Nil when the process tree could not be read.
	ChildPIDs map[int]bool
}

// newSessionStartTracker starts a tracker with the given bound. seedAt is the
// last evidence of life of an adopted session (zero otherwise), so the bound
// reflects that evidence instead of the moment of adoption. channelAt is the
// session's LastChannelEventAt when the wait begins: only channel events
// newer than it count as evidence.
func newSessionStartTracker(limit time.Duration, seedAt, channelAt time.Time) *sessionStartTracker {
	return &sessionStartTracker{limit: limit, lastEvidenceAt: seedAt, lastChannelAt: channelAt}
}

// Observe folds one tick into the tracker and returns the start observation
// for it. WaitingFor is the silence since the last evidence of work, or since
// the first silent waiting_input tick when no evidence has ever been seen, so
// Stalled ends the session only once that silence exceeds the limit. Any
// evidence on this tick is reported in Evidence and refreshes the clock.
func (t *sessionStartTracker) Observe(in sessionStartInput) autonomouspkg.SessionStartObservation {
	ev := autonomouspkg.StartEvidence{
		TaskDelivered:             in.Delivered,
		AgentChildProcess:         t.childWorkStarted(in.ChildPIDs),
		ChannelEventAfterDelivery: in.Delivered && in.ChannelAt.After(t.lastChannelAt),
	}
	if in.ChannelAt.After(t.lastChannelAt) {
		t.lastChannelAt = in.ChannelAt
	}
	if in.Pane != "" {
		ev.AssistantOutput = autonomouspkg.HasAssistantOutput(in.Pane)
		ev.ToolCallSeen = autonomouspkg.PaneShowsToolCall(in.Pane)
		if t.havePrevPane && in.Pane != t.prevPane {
			ev.PaneChangedAt = in.Now
		}
		t.prevPane, t.havePrevPane = in.Pane, true
	} else {
		t.havePrevPane = false
	}
	if present, _ := ev.Latest(); present {
		t.lastEvidenceAt = in.Now
	}
	if in.State != session.StateWaitingInput || in.Pane == "" || ev.AssistantOutput {
		t.silentSince = time.Time{}
	} else if t.silentSince.IsZero() {
		t.silentSince = in.Now
	}
	obs := autonomouspkg.SessionStartObservation{
		State:              string(in.State),
		HasAssistantOutput: ev.AssistantOutput,
		Limit:              t.limit,
		Evidence:           ev,
	}
	if anchor := t.anchor(); !anchor.IsZero() && in.Now.After(anchor) {
		obs.WaitingFor = in.Now.Sub(anchor)
	}
	return obs
}

// childWorkStarted reports whether the runner started a process since the
// previous observation. Only new pids count. A worker parked at a dialog keeps
// the helpers it launched at startup (an MCP server, say) alive for as long as
// it is parked, and a helper that is merely alive is not work. A command the
// runner starts during a tool call is new, so it is. The first observation sets
// the baseline and reports nothing. Nil means the probe failed: it reports
// nothing and keeps the previous baseline.
func (t *sessionStartTracker) childWorkStarted(pids map[int]bool) bool {
	if pids == nil {
		return false
	}
	started := false
	if t.haveChildren {
		for pid := range pids {
			if !t.prevChildren[pid] {
				started = true
				break
			}
		}
	}
	t.prevChildren, t.haveChildren = pids, true
	return started
}

// anchor is the moment the silence is measured from: the last evidence of
// work when there has been any, otherwise the first silent waiting_input tick.
func (t *sessionStartTracker) anchor() time.Time {
	if !t.lastEvidenceAt.IsZero() {
		return t.lastEvidenceAt
	}
	return t.silentSince
}

// sessionStartAdoptionSeed is the evidence-of-life time an adopted session
// starts with: the latest of its UpdatedAt, its last channel event and its log
// file mtime. Zero when none is known, which leaves the clock to start at the
// first silent tick.
func sessionStartAdoptionSeed(updatedAt, channelAt, logMtime time.Time) time.Time {
	var seed time.Time
	for _, at := range []time.Time{updatedAt, channelAt, logMtime} {
		if at.After(seed) {
			seed = at
		}
	}
	return seed
}

// logFileModTime returns the mtime of the session log, or zero when the log
// cannot be read.
func logFileModTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
