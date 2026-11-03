// Package session — per-backend liveness classifier.
//
// The gap watcher in state_engine.go (runChannelStateWatcherTick) flips a
// Running session to WaitingInput after a silence window with no channel
// event. That works for interactive TUI backends, which keep producing pane
// and channel activity while they work. A print-mode backend such as
// pi-prompt (IsPromptBackend in backend_family.go) is different: `pi
// --print` writes nothing to the pane until it finishes, so the gap watcher
// misreads the working session as "waiting for input" even though the
// process is alive and mid-turn.
//
// ClassifyLiveness is a pure decision function that answers, for one backend
// and one moment in time, whether the session is running, waiting for
// input, or stalled. It owns no tmux, filesystem, or clock: the caller
// supplies the process-alive bit, the last-activity time (transcript or
// output-file mtime, or LastChannelEventAt), the "now" time, the liveness
// window, and whether the interactive pane is showing a real input prompt.
// Keeping it pure means every branch is directly unit-testable with fake
// inputs and no daemon.

package session

import "time"

// DefaultLivenessWindow is the default silence window the liveness classifier
// uses when the caller passes a non-positive window. A backend whose process
// is alive but has produced no activity for longer than this is no longer
// "writing" and is treated as stalled (for print-mode) or deferred to the
// existing gap watcher (for interactive). Longer than the 15 s gap watcher
// default because a print-mode turn can legitimately be silent for a while.
const DefaultLivenessWindow = 30 * time.Second

// LivenessVerdict is the classifier's answer for one backend at one instant.
// The three real liveness verdicts are LivenessRunning, LivenessWaitingInput,
// and LivenessStalled; LivenessNoOpinion is a sentinel meaning "the classifier
// has no opinion, keep the existing gap-watcher behaviour for this session".
type LivenessVerdict int

const (
	// LivenessNoOpinion: the caller's existing gap watcher path governs this
	// session unchanged (interactive, non-claude-code, silent beyond the
	// window, and no opt-in to stall others).
	LivenessNoOpinion LivenessVerdict = iota
	// LivenessRunning: the backend is alive and (writing or, for claude-code,
	// still busy-probe-busy).
	LivenessRunning
	// LivenessWaitingInput: the interactive backend is at a real input prompt.
	LivenessWaitingInput
	// LivenessStalled: the process is dead, or a print-mode backend is alive
	// but silent, or a non-claude-code interactive backend is silent and the
	// caller opted in to stalling others.
	LivenessStalled
)

// String names the verdict for logs and journal reasons.
func (v LivenessVerdict) String() string {
	switch v {
	case LivenessRunning:
		return "running"
	case LivenessWaitingInput:
		return "waiting_input"
	case LivenessStalled:
		return "stalled"
	}
	return "no_opinion"
}

// LivenessInput carries the caller-supplied facts the classifier needs. None
// of these are derived here — no tmux, no filesystem, no clock.
type LivenessInput struct {
	// BackendFamily is the session's backend family string (pi-prompt, pi,
	// opencode, opencode-acp, goose, gemini, aider, ollama, openwebui,
	// claude-code, ...).
	BackendFamily string

	// PrintMode reports whether this is a print-mode / one-shot backend
	// (IsPromptBackend, or an explicit one-shot flag).
	PrintMode bool

	// ProcessAlive reports whether the pane's child process is still alive.
	ProcessAlive bool

	// LastActivity is the last observable activity: transcript or output-file
	// mtime, or LastChannelEventAt. Zero means "no activity observed".
	LastActivity time.Time

	// Now is the reference time for the silence check (caller's clock).
	Now time.Time

	// Window is the liveness silence window. Non-positive selects
	// DefaultLivenessWindow.
	Window time.Duration

	// AtPrompt reports whether the interactive pane is showing a real input
	// prompt. Meaningful only for interactive backends; ignored for
	// print-mode and for the dead-process case.
	AtPrompt bool

	// StallOthers opts the caller in to reporting non-claude-code interactive
	// backends as stalled when they are silent beyond the window. When false,
	// the classifier returns LivenessNoOpinion in that case and the existing
	// gap watcher keeps its current behaviour.
	StallOthers bool
}

// LivenessAssessment pairs the classifier's verdict with the silence fact
// it carries for callers that need both — the supervisor watch's silence
// check (internal/autonomous).
//
// The two fields matter because the alive-and-silent case — the worker's
// process is alive, the pane is not at an input prompt, and there has been
// no activity beyond the liveness window — lands on a different verdict per
// family: print-mode and stall-consent interactive families report it as
// LivenessStalled, while claude-code reports LivenessRunning (rule (f)
// preserves the busy probe's semantics for the busy probe and the gap
// watcher). AliveSilent is the family-independent fact that the worker is
// alive yet silent beyond the window — exactly what a silence check is
// watching for.
type LivenessAssessment struct {
	// Verdict is the classifier's answer (ClassifyLiveness) for the same
	// input.
	Verdict LivenessVerdict

	// AliveSilent reports that in.ProcessAlive is true, in.AtPrompt is
	// false, and in.LastActivity is zero or older than in.Window (a
	// non-positive Window selects DefaultLivenessWindow) at in.Now. For
	// families whose verdict for that case is LivenessStalled it is already
	// carried in the verdict; for claude-code it is the verdict's only
	// companion — the silence the verdict deliberately does not report.
	AliveSilent bool
}

// AssessLiveness is ClassifyLiveness plus the alive-and-silent fact, in
// one pass over in. It is pure like the classifier: it only reads in and
// does no I/O, so it can be driven entirely by fake inputs in tests. The
// caller supplies the same facts as before; this function derives nothing
// and adds no policy of its own — the silence fact is the caller's window,
// not a different one.
func AssessLiveness(in LivenessInput) LivenessAssessment {
	assessment := LivenessAssessment{Verdict: ClassifyLiveness(in)}
	window := in.Window
	if window <= 0 {
		window = DefaultLivenessWindow
	}
	// Same within-window test the classifier uses: no observable activity
	// (zero LastActivity) counts as silent, as does activity older than the
	// window.
	within := !in.LastActivity.IsZero() && in.Now.Sub(in.LastActivity) <= window
	assessment.AliveSilent = in.ProcessAlive && !in.AtPrompt && !within
	return assessment
}

// ClassifyLiveness applies the liveness rules to in and returns a verdict.
// It is pure: it only reads in and does no I/O, so it can be driven entirely
// by fake inputs in tests.
//
// Rules, in priority order:
//
//   - (c) process not alive while the record is open → LivenessStalled,
//     never LivenessWaitingInput.
//   - (a) print-mode, alive, activity within the window → LivenessRunning.
//   - (b) print-mode, alive, silent beyond the window → LivenessStalled.
//   - (d) interactive, alive, at a real prompt → LivenessWaitingInput.
//   - (e) interactive, alive, not at prompt, activity within the window →
//     LivenessRunning.
//   - (f) interactive, alive, not at prompt, silent beyond the window →
//     LivenessRunning for claude-code (preserves the busy-probe semantics);
//     LivenessStalled for other families only when in.StallOthers is set;
//     otherwise LivenessNoOpinion so the existing gap watcher path is
//     unchanged.
func ClassifyLiveness(in LivenessInput) LivenessVerdict {
	// A dead process is never running or waiting for input.
	if !in.ProcessAlive {
		return LivenessStalled
	}

	window := in.Window
	if window <= 0 {
		window = DefaultLivenessWindow
	}
	// withinWindow is false when there is no observable activity yet, so a
	// brand-new session with a zero LastActivity is treated as silent.
	withinWindow := !in.LastActivity.IsZero() && in.Now.Sub(in.LastActivity) <= window

	if in.PrintMode {
		if withinWindow {
			return LivenessRunning
		}
		return LivenessStalled
	}

	// Interactive backend, alive.
	if in.AtPrompt {
		return LivenessWaitingInput
	}
	if withinWindow {
		return LivenessRunning
	}
	// Silent beyond the window.
	if in.BackendFamily == busyProbeBackend {
		// claude-code keeps its existing busy-probe semantics: silence alone
		// does not stall it.
		return LivenessRunning
	}
	if in.StallOthers {
		return LivenessStalled
	}
	return LivenessNoOpinion
}
