package autonomous

import "time"

// startFailureTailLines is how many pane lines a failed start keeps for the
// task's StartFailureTail.
const startFailureTailLines = 20

// startTracker is the start-confirm clock of one wait on a worker session. A
// worker that never started parks in waiting_input without an assistant turn,
// but a worker mid tool call also shows no assistant bullet while it is busy.
// The bound therefore runs from the last evidence of work (StartEvidence), not
// from the first silent tick, so only real silence longer than the limit ends a
// session.
//
// The tracker reads no session state itself: the wait passes each tick's pane,
// state and probes in through Observe, which keeps it testable on a fake clock.
//
// A tracker runs in one of two modes. The launch mode is the start-confirm
// rule for a session the daemon launched: any evidence refreshes the clock.
// The adopted mode is the rule for a session found alive at boot: a probe
// outside waiting_input restarts the window, a changed pane or a tool call
// restarts it, and the first assistant turn latches the session as started.
type startTracker struct {
	adopted        bool          // adopted-session window rules; false for the launch rules
	limit          time.Duration // start-confirm bound; <= 0 disables it
	launchedAt     time.Time     // launch time, or adoption time when no launch is recorded
	lastEvidenceAt time.Time     // latest tick with evidence of work; zero until one is seen
	silentSince    time.Time     // first silent waiting_input tick; the anchor until evidence is seen
	prevPane       string        // pane captured on the previous observation
	havePrevPane   bool          // prevPane holds a capture a later pane can be compared with
	lastChannelAt  time.Time     // session LastChannelEventAt as of the previous observation
	prevChildren   map[int]bool  // runner-started pids on the previous observation
	haveChildren   bool          // prevChildren is a baseline a later set can be compared with
	started        bool          // adopted mode: a first assistant turn has been seen
	failureTail    []string      // pane tail recorded when the bound fires
}

// startInput is one tick's view of the worker, gathered by the wait loop.
type startInput struct {
	Now       time.Time
	State     string    // session state string, compared with sessionStateWaitingInput
	Pane      string    // visible pane capture; empty when the capture failed
	Delivered bool      // the task prompt was typed into the session
	ChannelAt time.Time // session LastChannelEventAt
	// ChildPIDs are the live processes the runner started (never the runner
	// itself). Nil when the process tree could not be read.
	ChildPIDs map[int]bool
}

// newStartTracker starts a launch-mode tracker with the given bound. launchedAt
// anchors the silent window until evidence is seen; lastLifeSeed is the last
// evidence of life of the session (zero when none is known), which becomes the
// anchor. Both are zero for a session launched in this wait.
func newStartTracker(limit time.Duration, launchedAt, lastLifeSeed time.Time) *startTracker {
	return &startTracker{limit: limit, launchedAt: launchedAt, silentSince: launchedAt, lastEvidenceAt: lastLifeSeed}
}

// newAdoptedStartTracker starts an adopted-mode tracker. The arguments mean
// what they mean for newStartTracker; the silent window runs from the last
// evidence of life when one is known, otherwise from launchedAt.
func newAdoptedStartTracker(limit time.Duration, launchedAt, lastLifeSeed time.Time) *startTracker {
	t := newStartTracker(limit, launchedAt, lastLifeSeed)
	t.adopted = true
	return t
}

// Observe folds one tick into the tracker and returns the start observation
// for it. WaitingFor is the silence since the anchor, so Stalled ends the
// session only once that silence exceeds the limit. In launch mode any
// evidence on this tick is reported in Evidence and refreshes the clock. In
// adopted mode the tick's pane must be a non-empty capture of a waiting session
// for the bound to be judged; a tick outside waiting_input only restarts the
// window.
func (t *startTracker) Observe(in startInput) SessionStartObservation {
	var obs SessionStartObservation
	if t.adopted {
		obs = t.observeAdopted(in)
	} else {
		obs = t.observeLaunch(in)
	}
	if stalled, _ := obs.Stalled(); stalled {
		t.failureTail = LastLines(in.Pane, startFailureTailLines)
	}
	return obs
}

// FailureTail returns the pane lines recorded when the bound fired, or nil when
// it has not fired.
func (t *startTracker) FailureTail() []string {
	return t.failureTail
}

// observeLaunch is the launch-mode rule. Any evidence on the tick refreshes the
// clock; the silent window starts at the first silent waiting_input tick when
// no evidence has been seen.
func (t *startTracker) observeLaunch(in startInput) SessionStartObservation {
	ev := StartEvidence{
		TaskDelivered:             in.Delivered,
		AgentChildProcess:         t.childWorkStarted(in.ChildPIDs),
		ChannelEventAfterDelivery: in.Delivered && in.ChannelAt.After(t.lastChannelAt),
	}
	if in.ChannelAt.After(t.lastChannelAt) {
		t.lastChannelAt = in.ChannelAt
	}
	if in.Pane != "" {
		ev.AssistantOutput = HasAssistantOutput(in.Pane)
		ev.ToolCallSeen = PaneShowsToolCall(in.Pane)
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
	if in.State != sessionStateWaitingInput || in.Pane == "" || ev.AssistantOutput {
		t.silentSince = time.Time{}
	} else if t.silentSince.IsZero() {
		t.silentSince = in.Now
	}
	obs := SessionStartObservation{
		State:              in.State,
		HasAssistantOutput: ev.AssistantOutput,
		Limit:              t.limit,
		Evidence:           ev,
	}
	if anchor := t.anchor(); !anchor.IsZero() && in.Now.After(anchor) {
		obs.WaitingFor = in.Now.Sub(anchor)
	}
	return obs
}

// observeAdopted is the adopted-mode rule. A tick outside waiting_input restarts
// the window and is never judged. A tick inside waiting_input with a pane is
// judged: the first assistant turn latches the session as started, which
// reports HasAssistantOutput for the rest of the wait so the bound never fails
// it; a changed pane or a tool call restarts the window; otherwise the silence
// since the anchor is what the bound measures.
func (t *startTracker) observeAdopted(in startInput) SessionStartObservation {
	if in.State != sessionStateWaitingInput {
		t.lastEvidenceAt = in.Now
		return SessionStartObservation{State: in.State, HasAssistantOutput: t.started, Limit: t.limit}
	}
	if in.Pane == "" {
		// No capture to judge this tick: the bound does not apply to it.
		return SessionStartObservation{State: in.State, HasAssistantOutput: t.started}
	}
	if HasAssistantOutput(in.Pane) {
		t.started = true
	}
	paneChanged := t.havePrevPane && in.Pane != t.prevPane
	t.prevPane, t.havePrevPane = in.Pane, true
	ev := StartEvidence{ToolCallSeen: PaneShowsToolCall(in.Pane)}
	if paneChanged {
		ev.PaneChangedAt = in.Now
	}
	if present, _ := ev.Latest(); present {
		t.lastEvidenceAt = in.Now
	}
	obs := SessionStartObservation{
		State:              in.State,
		HasAssistantOutput: t.started,
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
func (t *startTracker) childWorkStarted(pids map[int]bool) bool {
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
// work when there has been any, otherwise the silent-since time.
func (t *startTracker) anchor() time.Time {
	if !t.lastEvidenceAt.IsZero() {
		return t.lastEvidenceAt
	}
	return t.silentSince
}
