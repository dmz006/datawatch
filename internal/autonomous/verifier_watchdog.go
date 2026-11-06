package autonomous

import (
	"errors"
	"fmt"
	"strings"
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
	// ActiveRequest is true when a model-server probe confirms the
	// session's model is loaded and serving (an active request for the
	// session's model); it counts as progress. The loop must pass false
	// when the probe is unknown (error, unreachable, host not set): that is
	// "no evidence either way" and must never end a session on its own or
	// be treated as evidence of inactivity.
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
// A confirmed active model-server request (ActiveRequest) counts as
// progress exactly like output or pane activity; the loop must pass
// false when the probe is unknown, so a probe failure neither ends a
// session on its own nor saves one. A non-positive limit disables the
// watchdog.
func (w *VerifierWatchdog) Decide(now time.Time, s ProgressSignals) bool {
	paneChanged := w.seenMarker && s.Marker != w.lastMarker
	w.lastMarker, w.seenMarker = s.Marker, true

	if s.OutputPresent || s.RunnerAlive || s.ActiveRequest || s.Slot == SlotHeld || paneChanged {
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

// verifierIdlePrefix is the stable leading fragment of the watchdog error
// text: it is what makes a tripped-watchdog failure recognizable in the
// OneShotNoOutputError text the cmd layer builds for it, and what the
// IsVerifierIdleError classifier matches on.
const verifierIdlePrefix = "verifier no-progress watchdog: "

// VerifierWatchdogMessage is the OneShotNoOutputError text for a tripped
// watchdog.
func VerifierWatchdogMessage(limit time.Duration) string {
	return fmt.Sprintf("%sno output file or pane activity for %v", verifierIdlePrefix, limit)
}

// VerifierIdleError marks a verifier failure caused by the no-progress
// watchdog tripping: the verifier session went idle (no output file, no
// pane activity, dead runner, no slot, no active request) past the limit,
// as opposed to a worker failure such as a session that ended in a broken
// state or hit its time limit. NewVerifierIdleError carries the same text
// as the OneShotNoOutputError the cmd layer builds for the same trip, so
// the two spellings of the same event classify identically.
type VerifierIdleError struct {
	Msg string
}

func (e *VerifierIdleError) Error() string { return e.Msg }

// NewVerifierIdleError returns a VerifierIdleError for a watchdog that
// tripped after limit of silence, with the standard watchdog text.
func NewVerifierIdleError(limit time.Duration) *VerifierIdleError {
	return &VerifierIdleError{Msg: VerifierWatchdogMessage(limit)}
}

// IsVerifierIdleError reports whether err is a tripped-verifier-watchdog
// failure, so a caller can tell a verifier that went idle apart from a
// worker failure. It is true for a *VerifierIdleError (or anything that
// wraps one), and for a *OneShotNoOutputError whose text is the watchdog
// text — the spelling the one-shot loop currently raises — at any point
// in the error chain. Timeouts, other one-shot no-output ends and plain
// errors are not idle.
func IsVerifierIdleError(err error) bool {
	var idle *VerifierIdleError
	if errors.As(err, &idle) {
		return true
	}
	var noOutput *OneShotNoOutputError
	if errors.As(err, &noOutput) {
		return strings.HasPrefix(noOutput.Msg, verifierIdlePrefix)
	}
	return false
}
