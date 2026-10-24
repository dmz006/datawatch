// Package session — busy probe for silent claude-code tool calls.
//
// A claude-code worker that starts a long tool call emits no structural
// channel event: no MCP reply, no log line, and often no pane change while
// the TUI holds a static frame. The gap watcher then flips the busy session
// to WaitingInput, and nothing flips it back until output appears. The probe
// reads the pane and the pane process's children to decide whether the agent
// is visibly working, so the state engine keeps it Running.

package session

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// busyProbeBackend is the backend family the busy probe governs.
const busyProbeBackend = "claude-code"

// busyProbeWindowLines is how many non-empty pane lines from the bottom the
// probe inspects for tool-call and spinner lines.
const busyProbeWindowLines = 15

// spinnerGlyphs are the leading glyphs Claude Code draws before a spinner or
// turn-status line.
const spinnerGlyphs = "●✢✶✻✽✳·*"

// paneLooksBusy reports whether the agent behind a pane is visibly working.
// capture is the live pane text; childActive reports whether the pane's
// process has child processes. Pure: no tmux, no clock.
//
//   - A status bar with "esc to interrupt" is the strongest signal: Claude
//     Code draws it only while a turn is in progress.
//   - A tool line "⎿ Running…" or a spinner line "✻ Verb…" near the bottom
//     means a tool call or turn is in flight.
//   - An active child process counts as work unless the pane shows an idle
//     prompt. An idle claude-code pane still has MCP helper children, so the
//     prompt check keeps those from reading as busy.
func paneLooksBusy(capture string, childActive bool) bool {
	lines := nonEmptyTail(StripANSI(capture), busyProbeWindowLines)
	if paneShowsWork(lines) {
		return true
	}
	return childActive && !paneIdlePromptVisible(lines)
}

// paneShowsWork reports whether the pane text carries an explicit working
// marker. It mirrors the busy indicators matchPromptLines already suppresses
// prompt detection for, so the two agree on what "busy" looks like.
func paneShowsWork(lines []string) bool {
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		if strings.Contains(lines[i], "esc to interrupt") || strings.Contains(lines[i], "esc interrupt") {
			return true
		}
	}
	for _, l := range lines {
		if isToolRunningLine(l) || isSpinnerLine(l) {
			return true
		}
	}
	return false
}

// paneIdlePromptVisible reports whether one of the last three lines is an
// input prompt: the claude-code input box or a shell prompt.
func paneIdlePromptVisible(lines []string) bool {
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		l := lines[i]
		if strings.HasPrefix(l, "❯") || strings.HasSuffix(l, "$") || strings.HasSuffix(l, "%") {
			return true
		}
	}
	return false
}

// isToolRunningLine matches a tool status line such as "⎿  Running…".
func isToolRunningLine(l string) bool {
	return strings.HasPrefix(l, "⎿") && strings.Contains(l, "…")
}

// isSpinnerLine matches a turn status line such as "✻ Crunching… (12s)". A
// past-tense line such as "✻ Crunched for 52s" has no ellipsis and does not
// match, so a finished turn does not read as busy.
func isSpinnerLine(l string) bool {
	if !strings.Contains(l, "…") {
		return false
	}
	first, size := utf8.DecodeRuneInString(l)
	if !strings.ContainsRune(spinnerGlyphs, first) {
		return false
	}
	rest := strings.TrimLeft(l[size:], " ")
	return rest != "" && rest[0] >= 'A' && rest[0] <= 'Z'
}

// nonEmptyTail returns the last n non-empty, trimmed lines of text.
func nonEmptyTail(text string, n int) []string {
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		if l := strings.TrimSpace(raw); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// paneHasChildProcess reports whether the pane process of a tmux session has
// at least one child process. It is the real implementation behind
// Manager.paneChildFn; tests replace that field with a fake.
func paneHasChildProcess(tmuxSession string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", tmuxSession, "#{pane_pid}").Output()
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(out))
	if _, err := strconv.Atoi(pid); err != nil {
		return false
	}
	// pgrep exits 1 when nothing matches, which is the "no child" answer.
	return exec.Command("pgrep", "-P", pid).Run() == nil
}

// busyProbeApplies reports whether the busy probe governs sess.
func busyProbeApplies(sess *Session) bool {
	return sess != nil && sess.BackendFamily == busyProbeBackend && sess.TmuxSession != ""
}

// paneBusy probes sess's pane. It returns false when there is no tmux API
// (test managers without a fake), the session is outside the probe's scope,
// or the capture fails, so existing behaviour holds in those cases.
func (m *Manager) paneBusy(sess *Session) bool {
	if m == nil || m.tmux == nil || !busyProbeApplies(sess) {
		return false
	}
	capture, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err != nil {
		return false
	}
	childFn := m.paneChildFn
	if childFn == nil {
		childFn = paneHasChildProcess
	}
	return paneLooksBusy(capture, childFn(sess.TmuxSession))
}

// applyBusyProbe makes a Running or WaitingInput session Running when its pane
// shows work, and reports whether it did. The write goes through
// markChannelEventAt, so it refreshes LastChannelEventAt, which restarts the
// gap window, and the terminal and rate-limited states stay sticky. A zero ts
// means now.
func (m *Manager) applyBusyProbe(fullID string, ts time.Time) bool {
	if m == nil || m.store == nil {
		return false
	}
	sess, ok := m.store.Get(fullID)
	if !ok || sess == nil {
		return false
	}
	if sess.State != StateRunning && sess.State != StateWaitingInput {
		return false
	}
	if !m.paneBusy(sess) {
		return false
	}
	m.markChannelEventAt(fullID, EventRunning, ts)
	return true
}
