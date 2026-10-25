package autonomous

import (
	"fmt"
	"time"
)

// SlotState reports the verifier's relationship to a capacity slot at the
// moment of an observation.
type SlotState int

// SlotState values. SlotNone is the zero value, so a zeroed ProgressSignals
// means "no slot involvement".
const (
	// SlotNone means the verifier is neither holding nor waiting for a
	// capacity slot.
	SlotNone SlotState = iota
	// SlotWaiting means the verifier is queued and waiting for a capacity
	// slot. Per the confirmed rule, time spent in this state is excluded
	// from the no-progress limit: the limit measures the run, not the
	// queue.
	SlotWaiting
	// SlotHeld means the verifier's holder has acquired the capacity slot.
	// Holding a slot counts as progress: it is a real compute claim for
	// the verifier.
	SlotHeld
)

// ProgressSignals carries every liveness signal available at one
// observation of a verifier session. All fields are plain data: the
// decision function takes time as a parameter and never reads the clock
// itself, so tests inject it.
type ProgressSignals struct {
	// OutputPresent is true when the verifier output file exists.
	OutputPresent bool
	// Marker is a stable fingerprint of the captured pane output; a change
	// from the previously observed marker counts as activity (the first
	// marker seen is the baseline and does not).
	Marker string
	// RunnerAlive is true when the verifier runner process (claude, pi,
	// opencode) is still alive under the session. A live runner counts as
	// progress.
	RunnerAlive bool
	// ActiveRequest is true when the model server reports an active
	// request for this session. The signal is accepted for plumbing but
	// not yet counted as progress: the confirmed rule defers it until a
	// reliable per-session request probe exists.
	ActiveRequest bool
	// Slot is the verifier's capacity-slot state (none, waiting, held).
	Slot SlotState
}

// VerifierWatchdog decides when a verifier session has made no progress. A
// session counts as progressing when its output file exists, its pane
// output marker changed since the previous observation, its runner process
// is alive, or it holds a capacity slot; any such activity resets the
// timer. Time spent waiting for a capacity slot never counts toward the
// limit. The clock is injected through the observation so the decision is
// deterministic in tests.
type VerifierWatchdog struct {
	limit        time.Duration
	lastActivity time.Time
	lastMarker   string
	seenMarker   bool
	waitingSince time.Time
	excluded     time.Duration
}

// NewVerifierWatchdog returns a watchdog whose silence is measured from
// start. limit <= 0 disables it (never exceeded).
func NewVerifierWatchdog(start time.Time, limit time.Duration) *VerifierWatchdog {
	return &VerifierWatchdog{limit: limit, lastActivity: start}
}

// Decide records one poll with the full set of liveness signals and
// reports whether the no-progress limit is exceeded. now is injected by
// the caller; Decide never reads the wall clock. Progress — output file
// present, a changed pane marker, a live runner process, or a held
// capacity slot — resets the silence baseline. While the verifier waits
// for a capacity slot the decision is always "not exceeded" and the time
// spent waiting is excluded from the limit once the slot is acquired.
// ActiveRequest is not counted as progress yet (deferred signal). A
// non-positive limit disables the watchdog.
func (w *VerifierWatchdog) Decide(now time.Time, s ProgressSignals) bool {
	paneChanged := w.seenMarker && s.Marker != w.lastMarker
	w.lastMarker, w.seenMarker = s.Marker, true

	if s.OutputPresent || s.RunnerAlive || s.Slot == SlotHeld || paneChanged {
		w.lastActivity = now
	}

	if w.limit <= 0 {
		return false
	}

	if s.Slot == SlotWaiting {
		if w.waitingSince.IsZero() {
			w.waitingSince = now
		}
		// Queue wait is excluded from the limit and never trips it.
		return false
	}

	if !w.waitingSince.IsZero() {
		w.excluded += now.Sub(w.waitingSince)
		w.waitingSince = time.Time{}
	}

	silence := now.Sub(w.lastActivity) - w.excluded
	return silence >= w.limit
}

// Observe records one poll carrying only the output-file and pane-marker
// signals (dead runner, no slot involvement) and reports whether the
// no-progress limit is exceeded. It is a convenience wrapper over Decide
// for call sites that do not have the process and slot signals at hand.
func (w *VerifierWatchdog) Observe(now time.Time, outputPresent bool, marker string) bool {
	return w.Decide(now, ProgressSignals{OutputPresent: outputPresent, Marker: marker})
}

// VerifierWatchdogMessage is the OneShotNoOutputError text for a tripped
// watchdog.
func VerifierWatchdogMessage(limit time.Duration) string {
	return fmt.Sprintf("verifier no-progress watchdog: no output file or pane activity for %v", limit)
}
