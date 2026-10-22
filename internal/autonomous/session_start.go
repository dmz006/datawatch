package autonomous

import (
	"errors"
	"strings"
	"time"
)

// ErrSessionNeverStarted marks a claude-code one-shot session that sat in
// waiting_input without producing its first assistant turn within the
// autonomous.session_start_confirm_seconds bound (default 120s): the worker
// never actually started work (e.g. parked on a startup dialog that nobody
// dismissed). See SessionStartObservation and the daemon's autonomous
// worker wait loop.
var ErrSessionNeverStarted = errors.New("session never started")

// SessionStartFailureReason is the failure reason recorded on the task's
// Error when a spawned session fails the start-confirm bound.
const SessionStartFailureReason = "session never started"

// claudeCodeResponseBullet is the "•" bullet Claude Code renders at the
// start of an assistant response line — the same marker the pane fixtures
// under internal/session/testdata (e.g. paste/submitted_running.txt) use
// to mark the assistant's first turn.
const claudeCodeResponseBullet = "•"

// claudeCodePromptSelector is the "❯" prompt marker that starts the user
// prompt line in a Claude Code pane capture.
const claudeCodePromptSelector = "❯"

// SessionStartObservation is a point-in-time view of a worker session used
// to decide whether it has started (produced its first assistant turn) or
// simply stalled before ever doing so.
type SessionStartObservation struct {
	State              string        // session state string ("waiting_input", "running", ...)
	WaitingFor         time.Duration // how long the session has sat waiting_input with no assistant output
	HasAssistantOutput bool          // true when a Claude Code assistant response bullet appeared after the prompt
	Limit              time.Duration // effective start-confirm bound (0 disables the check)
}

// Stalled reports whether the session has never started: it is still parked
// in waiting_input, has produced no assistant output, and the wait has
// STRICTLY exceeded the limit (WaitingFor exactly equal to Limit does not
// stall — the bound is a bound, not a deadline hit at equality). The reason
// is non-empty only when it stalls.
func (o SessionStartObservation) Stalled() (bool, string) {
	if o.Limit <= 0 {
		return false, ""
	}
	// Plain string compare: internal/autonomous must not import
	// internal/session (that would create an import cycle); session_idle.go
	// shares the same convention via sessionStateWaitingInput.
	if o.State != sessionStateWaitingInput {
		return false, ""
	}
	if o.HasAssistantOutput {
		return false, ""
	}
	if o.WaitingFor <= o.Limit {
		return false, ""
	}
	return true, SessionStartFailureReason
}

// paneHasAssistantOutput reports whether a Claude Code assistant response
// bullet (claudeCodeResponseBullet) appears on a line AFTER the prompt
// selector (claudeCodePromptSelector) line in the pane capture — which is
// what "the session has produced its first assistant turn" looks like in a
// `tmux capture-pane` snapshot. Pure: pane in, bool out.
func paneHasAssistantOutput(pane string) bool {
	lines := strings.Split(pane, "\n")
	promptIdx := -1
	for i, line := range lines {
		if strings.Contains(line, claudeCodePromptSelector) {
			promptIdx = i
			break
		}
	}
	if promptIdx < 0 {
		return false
	}
	for _, line := range lines[promptIdx+1:] {
		if strings.Contains(line, claudeCodeResponseBullet) {
			return true
		}
	}
	return false
}

// HasAssistantOutput is the exported form of paneHasAssistantOutput for
// callers outside the autonomous package (the daemon's wait loop builds
// SessionStartObservation from a live pane capture).
func HasAssistantOutput(pane string) bool { return paneHasAssistantOutput(pane) }

// lastLines returns the last n non-empty lines of the pane capture, in
// original order. Blank lines (including the trailing one every tmux
// capture ends with) are skipped; if the pane has fewer than n non-empty
// lines, all of them are returned. Pure: pane in, slice out.
func lastLines(pane string, n int) []string {
	out := make([]string, 0, n)
	if n <= 0 || pane == "" {
		return out
	}
	lines := strings.Split(pane, "\n")
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		out = append(out, lines[i])
	}
	// collected newest-first; reverse to restore top-to-bottom order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// LastLines is the exported form of lastLines for callers outside the
// autonomous package — the daemon's wait loop stores the failure tail on
// the Task (StartFailureTail) when a session never starts.
func LastLines(pane string, n int) []string { return lastLines(pane, n) }
