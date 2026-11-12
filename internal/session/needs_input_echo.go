package session

import (
	"strings"
	"unicode/utf8"
)

// needsInputKind classifies a line against the input-needed protocol markers.
type needsInputKind int

const (
	// needsInputNone: the line carries no marker.
	needsInputNone needsInputKind = iota
	// needsInputInstruction: the line mentions a marker but is not a request,
	// e.g. the task text "When blocked, print DATAWATCH_NEEDS_INPUT: <your
	// question>", a wrapped "DATAWATCH_NEEDS_INPUT: <your" continuation, or a
	// shell echo of the marker. Task text reaches the pane and the log verbatim,
	// so an instruction must never read as the worker asking something.
	needsInputInstruction
	// needsInputReal: a standalone line that starts with the marker and carries
	// a concrete question, the way a worker prints it.
	needsInputReal
)

// classifyNeedsInput reports whether l is a real needs-input request. Like the
// completion marker, a real request starts the line (after TUI indicator runes
// such as ●) and is followed by a non-placeholder question. It returns the
// question for a real request.
func classifyNeedsInput(l string, pats []string) (needsInputKind, string) {
	kind := needsInputNone
	for _, pat := range pats {
		if !strings.Contains(l, pat) {
			continue
		}
		kind = needsInputInstruction
		for _, seg := range strings.Split(l, "\r") {
			seg = strings.TrimSpace(StripANSI(strings.TrimSpace(seg)))
			for len(seg) > 0 {
				r, size := utf8.DecodeRuneInString(seg)
				if r <= 127 {
					break
				}
				seg = strings.TrimLeft(seg[size:], " \t")
			}
			if !strings.HasPrefix(seg, pat) {
				continue
			}
			q := strings.TrimSpace(seg[len(pat):])
			if q != "" && !strings.HasPrefix(q, "<") {
				return needsInputReal, q
			}
		}
	}
	return kind, ""
}

// maskTaskEcho blanks every line up to and including the echoed task text, so
// the task the worker was given is never scanned for prompts or markers. The
// lines are returned unchanged when the task cannot be located.
func maskTaskEcho(lines []string, task string) []string {
	end := lastTaskEchoIndex(lines, task)
	if end < 0 {
		return lines
	}
	out := make([]string, len(lines))
	copy(out[end+1:], lines[end+1:]) // lines up to end stay "" so indices are stable
	return out
}

// markersTrusted reports whether a real needs-input marker on the pane may be
// believed right now: not during the post-restart grace (the pane replays
// prior history) and not inside the task-echo window (the TUI is still
// rendering the task text).
func (m *Manager) markersTrusted(sess *Session) bool {
	now := m.now()
	m.mu.Lock()
	restartTime, wasRestarted := m.restartedAt[sess.FullID]
	m.mu.Unlock()
	if wasRestarted && now.Sub(restartTime) < m.restartGraceWindow() {
		return false
	}
	if deliveredAt, ok := m.taskDeliveredTime(sess); ok && now.Sub(deliveredAt) < m.echoWindow() {
		return false
	}
	return true
}

// matchPromptForSession is matchPromptInLines for a session's pane: the echoed
// task text is masked out, and a real needs-input marker only counts once the
// grace and echo windows have passed.
func (m *Manager) matchPromptForSession(sess *Session, lines []string, n int) (string, string) {
	return m.matchPromptLines(maskTaskEcho(lines, sess.Task), n, m.markersTrusted(sess))
}

// transitionOnPane moves a session to waiting_input for a pane match. A real
// needs-input marker is an explicit protocol signal and bypasses the prompt
// debounce, like the log-stream path.
func (m *Manager) transitionOnPane(fullID, matched, promptCtx string, getTracker func() *Tracker) bool {
	if kind, q := classifyNeedsInput(matched, m.effectiveInputNeededPatterns()); kind == needsInputReal {
		return m.tryTransitionToWaiting(fullID, q, promptCtx, getTracker, true)
	}
	return m.tryTransitionToWaiting(fullID, matched, promptCtx, getTracker)
}
