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
	"os"
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

// busyProbeApplies reports whether the claude-code busy probe governs
// sess. Claude-code (busy TUI, silent tool calls) is the only family that
// reads the TUI pane for work markers; every other recorded family goes
// through the liveness rule (livenessRuleApplies) instead.
func busyProbeApplies(sess *Session) bool {
	return sess != nil && sess.BackendFamily == busyProbeBackend && sess.TmuxSession != ""
}

// livenessRuleApplies reports whether the liveness rule (ClassifyLiveness)
// governs the family: every recorded family except claude-code. An
// unrecorded family ("") has nothing for the rule to classify, so it keeps
// the legacy gap-watcher flip; claude-code keeps the busy probe, which is
// the interactive reading of the same rule.
func livenessRuleApplies(family string) bool {
	if family == "" {
		return false
	}
	return family != busyProbeBackend
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

// paneProcessAlive reports whether sess's pane has a live process, through
// the same process check the busy probe's childActive signal uses
// (Manager.paneChildFn, defaulting to paneHasChildProcess). A session with
// no tmux pane reports dead — nothing is alive to check.
func (m *Manager) paneProcessAlive(sess *Session) bool {
	if m == nil || sess == nil || sess.TmuxSession == "" {
		return false
	}
	childFn := m.paneChildFn
	if childFn == nil {
		childFn = paneHasChildProcess
	}
	return childFn(sess.TmuxSession)
}

// sessionLastActivity is the freshest observable activity for sess:
// LastChannelEventAt, and (when a tracker exists) the output log file's
// mtime, whichever is newer. For print-mode backends the output log is the
// transcript — the thing whose mtime the liveness rule watches.
func (m *Manager) sessionLastActivity(sess *Session) time.Time {
	if m == nil || sess == nil {
		return time.Time{}
	}
	latest := sess.LastChannelEventAt
	if tr := m.getTracker(sess.FullID); tr != nil {
		if fi, err := os.Stat(tr.OutputLogPath()); err == nil {
			if fi.ModTime().After(latest) {
				latest = fi.ModTime()
			}
		}
	}
	return latest
}

// livenessWindowFor resolves the liveness silence window for family via the
// config accessor (0/unset → its built-in default); with no config the
// classifier's own DefaultLivenessWindow applies (window 0).
func (m *Manager) livenessWindowFor(family string) time.Duration {
	if m != nil && m.cfg != nil {
		if sec := m.cfg.LivenessWindowSecondsEffective(); sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 0
}

// paneAtPrompt reports whether sess's pane currently shows an input prompt
// — the busy probe's idle-prompt heuristic (an input box line or a shell
// prompt on one of the last three lines). That is the "at a real prompt"
// signal the liveness rule uses for interactive backends.
func (m *Manager) paneAtPrompt(sess *Session) bool {
	if m == nil || m.tmux == nil || sess == nil || sess.TmuxSession == "" {
		return false
	}
	capture, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err != nil || capture == "" {
		return false
	}
	return paneIdlePromptVisible(nonEmptyTail(StripANSI(capture), busyProbeWindowLines))
}

// livenessVerdictFor applies the liveness rule (ClassifyLiveness) to sess at
// now, supplying the caller facts: process-alive through the pane child
// check, the freshest observable activity, and whether the pane shows a
// real input prompt. StallOthers stays false — stalling silent
// non-claude-code interactive backends is an explicit opt-in, not the
// default here.
func (m *Manager) livenessVerdictFor(sess *Session, now time.Time) LivenessVerdict {
	if now.IsZero() {
		now = time.Now()
	}
	in := LivenessInput{
		BackendFamily: sess.BackendFamily,
		PrintMode:     IsPromptBackend(sess.BackendFamily),
		ProcessAlive:  m.paneProcessAlive(sess),
		LastActivity:  m.sessionLastActivity(sess),
		Now:           now,
		Window:        m.livenessWindowFor(sess.BackendFamily),
		AtPrompt:      m.paneAtPrompt(sess),
	}
	return ClassifyLiveness(in)
}

// LivenessVerdictFor is the verdict half of LivenessAssessmentFor: the
// same lookup, the same caller facts, the same StallOthers semantics, and
// the same no-verdict answers — without the alive-and-silent fact.
func (m *Manager) LivenessVerdictFor(id string, now time.Time) (LivenessVerdict, bool) {
	v, _, ok := m.LivenessAssessmentFor(id, now)
	return v, ok
}

// LivenessAssessmentFor applies the liveness rule (ClassifyLiveness, via
// AssessLiveness) to the session at id — full id or short id, the same
// lookup GetSession applies — and the caller's now, and reports the
// verdict with the alive-and-silent fact, both with true. An unknown or
// missing session reports (LivenessNoOpinion, false, false): the caller
// has no assessment to work with and keeps its own fallback. A session
// with no recorded backend family (legacy records) likewise reports
// nothing: the rule cannot say what kind of worker it is applied to, and
// reading that as death would flip the caller's judgement on a record the
// rule is not about.
//
// This is the exported entry point for cross-package consumers (the
// autonomous supervisor watch's silence check) and differs from the
// manager's internal verdict path (livenessVerdictFor, the busy probe and
// gap watcher) in one deliberate way: it passes StallOthers=true, so the
// caller's consent to reporting silent non-claude-code interactive
// backends as stalled is explicit to this view. Consequences of that view:
//
//   - an alive, silent print-mode or interactive non-claude-code worker is
//     reported LivenessStalled — silence for the silence check;
//
//   - an alive, silent claude-code worker keeps its LivenessRunning verdict
//     (rule (f) — the busy probe's and the gap watcher's semantics are
//     untouched); the silence is reported in the AliveSilent fact, which
//     the silence check reads as silence and the busy probe never sees.
//
// The fact plumbing itself is unchanged: process-alive through the pane
// child check, the freshest observable activity, and whether the pane
// shows a real input prompt.
func (m *Manager) LivenessAssessmentFor(id string, now time.Time) (LivenessVerdict, bool, bool) {
	if m == nil || m.store == nil {
		return LivenessNoOpinion, false, false
	}
	sess, ok := m.GetSession(id)
	if !ok || sess == nil || sess.BackendFamily == "" {
		return LivenessNoOpinion, false, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	a := AssessLiveness(LivenessInput{
		BackendFamily: sess.BackendFamily,
		PrintMode:     IsPromptBackend(sess.BackendFamily),
		ProcessAlive:  m.paneProcessAlive(sess),
		LastActivity:  m.sessionLastActivity(sess),
		Now:           now,
		Window:        m.livenessWindowFor(sess.BackendFamily),
		AtPrompt:      m.paneAtPrompt(sess),
		StallOthers:   true, // the cross-package view consents to stalling silent others
	})
	return a.Verdict, a.AliveSilent, true
}

// applyBusyProbe makes a Running or WaitingInput session Running when its
// backend is visibly working, and reports whether it did. Claude-code uses
// the pane busy probe; every other recorded family uses the liveness rule
// — live process with fresh writes, or live at a real prompt. The write
// goes through markChannelEventAt, so it refreshes LastChannelEventAt, which
// restarts the gap window, and the terminal and rate-limited states stay
// sticky. A zero ts means now.
func (m *Manager) applyBusyProbe(fullID string, ts time.Time) bool {
	if m == nil || m.store == nil {
		return false
	}
	sess, ok := m.store.Get(fullID)
	if !ok || sess == nil {
		return false
	}
	// StateStalled is deliberately out of scope here: a busy pane or a live
	// process alone does not prove the session is back — it stays stalled
	// until real output resumes it
	// (see the output-driven stalled→running restore) or a state override runs.
	if sess.State != StateRunning && sess.State != StateWaitingInput {
		return false
	}
	var busy bool
	switch {
	case sess.BackendFamily == busyProbeBackend:
		busy = m.paneBusy(sess)
	case livenessRuleApplies(sess.BackendFamily) && m.tmux != nil:
		// "Living and writing" reads as Running; at a real prompt the
		// verdict is WaitingInput, which is not busy — a waiting session is
		// where it already is. Stalled and no-opinion are not busy either.
		now := ts
		if now.IsZero() {
			now = time.Now()
		}
		busy = m.livenessVerdictFor(sess, now) == LivenessRunning
	}
	if !busy {
		return false
	}
	m.markChannelEventAt(fullID, EventRunning, ts)
	return true
}
