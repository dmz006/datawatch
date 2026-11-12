package autonomous

import "time"

// WorkerNudgeText is the single message sent to an idle claude-code worker.
const WorkerNudgeText = "If you are finished, print DATAWATCH_COMPLETE followed by a one-line summary; otherwise continue with the task."

// WorkerNudgeFamily is the only backend family the nudge applies to.
const WorkerNudgeFamily = "claude-code"

// NudgeForDelivery picks the text for an idle-worker nudge. A worker whose
// task was never typed into its session gets the task itself again, because
// the completion-line prompt cannot help a worker that never received the
// task. A delivered worker, or one with no task text to re-send, gets
// WorkerNudgeText. It is pure: the caller supplies the delivery state and the
// task text.
func NudgeForDelivery(delivered bool, task string) (text string, isRedelivery bool) {
	if !delivered && task != "" {
		return task, true
	}
	return WorkerNudgeText, false
}

// WorkerNudgeAction is the decision returned by WorkerNudgeTracker.Observe.
type WorkerNudgeAction string

const (
	WorkerNudgeNone  WorkerNudgeAction = "none"
	WorkerNudgeSend  WorkerNudgeAction = "send-nudge"
	WorkerNudgeStall WorkerNudgeAction = "stall"
)

// WorkerNudgeObservation is a point-in-time view of a worker session.
type WorkerNudgeObservation struct {
	State               string        // session state string; only waiting_input/idle count as idle
	OutputAge           time.Duration // age of the last output (log mtime), read after a nudge; 0 when unknown
	HasLiveChildProcess bool          // a runner child process is alive
	CompletionSeen      bool          // the completion marker has been seen
}

// WorkerNudgeTracker decides when an idle worker gets one nudge and when it is
// declared stalled. It is pure and clock-injected: the caller passes now.
type WorkerNudgeTracker struct {
	nudgeAfter time.Duration
	stallAfter time.Duration

	idleSince  time.Time     // start of the current unbroken idle run
	nudgedAt   time.Time     // when the one nudge was sent (zero before)
	idleAtSend time.Duration // idle time that earned the nudge, for the log line
	done       bool          // completion seen: tracking has ended
}

// NewWorkerNudgeTracker returns a tracker with the given windows. A window
// <= 0 disables the corresponding decision.
func NewWorkerNudgeTracker(nudgeAfter, stallAfter time.Duration) *WorkerNudgeTracker {
	return &WorkerNudgeTracker{nudgeAfter: nudgeAfter, stallAfter: stallAfter}
}

// Nudged reports whether the nudge has been sent.
func (t *WorkerNudgeTracker) Nudged() bool { return !t.nudgedAt.IsZero() }

// SeedNudgedAt adopts a nudge that was already sent at the given time, such as
// one persisted on the task before a daemon restart. The tracker then reports
// Nudged, never sends again, and counts the stall window from at. A zero time
// leaves the tracker unseeded.
func (t *WorkerNudgeTracker) SeedNudgedAt(at time.Time) {
	if at.IsZero() {
		return
	}
	t.nudgedAt = at
}

// Observe feeds one observation and returns the decision. Only claude-code
// PRD-owned sessions are tracked; family and prdOwned gate everything else to
// none.
func (t *WorkerNudgeTracker) Observe(now time.Time, family string, prdOwned bool, o WorkerNudgeObservation) WorkerNudgeAction {
	if family != WorkerNudgeFamily || !prdOwned || t.done {
		return WorkerNudgeNone
	}
	if o.CompletionSeen {
		t.done = true
		t.idleSince = time.Time{}
		return WorkerNudgeNone
	}
	idleState := o.State == sessionStateWaitingInput || o.State == "idle"
	if !idleState || o.HasLiveChildProcess {
		t.idleSince = time.Time{}
		return WorkerNudgeNone
	}
	if t.Nudged() {
		// New output after the nudge ends tracking: the worker is responding.
		if o.OutputAge > 0 && now.Add(-o.OutputAge).After(t.nudgedAt) {
			t.done = true
			return WorkerNudgeNone
		}
		if t.stallAfter > 0 && now.Sub(t.nudgedAt) >= t.stallAfter {
			return WorkerNudgeStall
		}
		return WorkerNudgeNone
	}
	if t.idleSince.IsZero() {
		t.idleSince = now
	}
	// The idle clock runs from the start of the idle run. The log file's age
	// is not used here: a cosmetic pane redraw touches the log on every tick
	// and would hold the clock below the window forever. Real output after
	// the nudge is still detected above and ends tracking as answered.
	idleFor := now.Sub(t.idleSince)
	if t.nudgeAfter > 0 && idleFor >= t.nudgeAfter {
		t.nudgedAt = now
		t.idleAtSend = idleFor
		return WorkerNudgeSend
	}
	return WorkerNudgeNone
}
