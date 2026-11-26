// Ending finished interactive sessions (session.finished_idle_close_minutes,
// off by default).
//
// An interactive session that reports done stays at waiting_input and keeps
// its capacity slot. With the setting on, the daemon ends it, but only when
// every part of the proof holds on every sweep for the whole period: the
// screen ends on the session's own completion marker, nothing is working in
// the foreground or the background, nobody is attached, and nothing is due to
// type into it. Anything that cannot be read counts as not proven. The period
// runs from the first sweep that saw the present screen, never from a record
// timestamp, so it also starts again after a daemon restart.

package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// finishedIdleBackend is the only family whose screen can prove it finished.
const finishedIdleBackend = "claude-code"

const (
	// finishedIdleScrollback is how much of the pane a sweep reads.
	finishedIdleScrollback = 200
	// finishedIdleScreenLines is how many lines up to the completion marker
	// make up the screen a sweep compares with the one before.
	finishedIdleScreenLines = 50
)

// attachedClientCounter is the tmux query a sweep needs. It is not part of
// TmuxAPI: a terminal that cannot answer it proves nothing.
type attachedClientCounter interface {
	AttachedClients(session string) (int, error)
}

// The daemon's own terminal must answer it, or nothing is ever ended.
var _ attachedClientCounter = (*TmuxManager)(nil)

// backgroundWorkCount matches a status line counting work left running, such
// as "· 1 shell" or "2 background tasks still running".
var backgroundWorkCount = regexp.MustCompile(`\b[1-9]\d*\s+(shells?|bashes|monitors?|(local\s+|remote\s+)?agents?|background\s+\w+|tasks?\s+(still\s+)?running)\b`)

// finishedIdleMark is the screen a finished session showed and when a sweep
// first saw it.
type finishedIdleMark struct {
	screen string
	since  time.Time
}

// finishedIdleLimit is the configured period; 0 means the feature is off.
func (m *Manager) finishedIdleLimit() time.Duration {
	if m.cfg == nil || m.cfg.Session.FinishedIdleCloseMinutes <= 0 {
		return 0
	}
	return time.Duration(m.cfg.Session.FinishedIdleCloseMinutes) * time.Minute
}

// CloseFinishedIdle ends every session that has been proven finished and idle
// for the configured period, which frees its capacity slot, and returns their
// full IDs. The record, the output log and the work tree are kept.
func (m *Manager) CloseFinishedIdle() []string {
	limit := m.finishedIdleLimit()
	if limit == 0 {
		m.mu.Lock()
		m.finishedIdle = nil
		m.mu.Unlock()
		return nil
	}
	var ended []string
	for _, sess := range m.store.List() {
		fullID := sess.FullID
		screen, pane, proven := m.finishedIdleProof(sess)
		// A shell's completion line from an agent that never had its task
		// proves nothing.
		if proven && paneShowsAgentExit(strings.Split(StripANSI(pane), "\n")) && m.EndedBeforeTask(fullID) {
			proven = false
		}
		now := m.now()
		m.mu.Lock()
		if !proven {
			delete(m.finishedIdle, fullID)
			m.mu.Unlock()
			continue
		}
		mark, seen := m.finishedIdle[fullID]
		if !seen || mark.screen != screen {
			mark = finishedIdleMark{screen: screen, since: now}
			if m.finishedIdle == nil {
				m.finishedIdle = map[string]finishedIdleMark{}
			}
			m.finishedIdle[fullID] = mark
		}
		due := now.Sub(mark.since) >= limit
		if due {
			delete(m.finishedIdle, fullID)
		}
		m.mu.Unlock()
		if !due {
			continue
		}
		if m.store.Snapshot(sess).CompletionSummary == "" {
			if summary := ExtractCompletionSummary(pane); summary != "" {
				m.store.Mutate(func() { sess.CompletionSummary = summary })
			}
		}
		why := EndReason{Caller: callerAt(0), Reason: fmt.Sprintf(
			"finished and idle: the screen ended on its completion marker, unchanged and unattached, for %s (session.finished_idle_close_minutes)", limit)}
		if err := m.endSessionAs(fullID, why, StateComplete); err != nil {
			m.journalf("[warn] end finished idle session %s: %v\n", fullID, err)
			continue
		}
		ended = append(ended, fullID)
	}
	return ended
}

// finishedIdleProof reports whether sess is proven finished and idle right
// now, with the screen that proves it and the whole captured pane.
func (m *Manager) finishedIdleProof(sess *Session) (screen, pane string, proven bool) {
	s := m.store.Snapshot(sess)
	// Only an operator-style interactive session: one-shots have their own
	// end, an Automaton's sessions belong to its executor, and supervisors
	// wait to be woken.
	if s.State != StateWaitingInput || s.OneShot || s.PRDID != "" || s.TaskID != "" ||
		s.AgentID != "" || s.LaunchRole == launchRoleSupervisor ||
		s.BackendFamily != finishedIdleBackend || s.TmuxSession == "" ||
		s.AwaitingInputAfterRelaunch || (s.Hostname != "" && s.Hostname != m.hostname) {
		return "", "", false
	}
	m.mu.Lock()
	turnOpen := m.hookTurnOpen[s.FullID]
	m.mu.Unlock()
	if turnOpen {
		return "", "", false
	}
	if m.schedStore != nil && (len(m.schedStore.PendingForSession(s.FullID)) > 0 ||
		len(m.schedStore.PendingForSession(s.ID)) > 0) {
		return "", "", false // ending it would cancel what is due to be typed
	}
	for _, child := range m.GetChildren(s.FullID) {
		if !m.store.Snapshot(child).State.IsTerminal() {
			return "", "", false
		}
	}
	clients, ok := m.tmux.(attachedClientCounter)
	if !ok {
		return "", "", false
	}
	if n, err := clients.AttachedClients(s.TmuxSession); err != nil || n != 0 {
		return "", "", false
	}
	pane, err := m.tmux.CapturePaneScrollback(s.TmuxSession, finishedIdleScrollback)
	if err != nil {
		return "", "", false
	}
	screen, proven = finishedScreen(pane, s.Task)
	return screen, pane, proven
}

// finishedScreen reports whether pane shows a finished, quiet session. screen
// is what must stay the same for the period: the output up to the completion
// marker and the text in the input box. Footers and turn notes are left out,
// so a redraw of them is not activity.
func finishedScreen(pane, task string) (screen string, finished bool) {
	text := strings.ReplaceAll(StripANSI(pane), "\r\n", "\n")
	if !OutputEndsWithCompletion(text) {
		return "", false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n")
	above := dropInputBox(lines)
	marker := -1
	for i := len(above) - 1; i >= 0; i-- {
		l := strings.TrimSpace(above[i])
		if !strings.HasPrefix(l, "❯") && isRealCompletionLine(l, completionSummaryMarker) {
			marker = i
			break
		}
	}
	// The marker in the echoed task text is the instruction, not the report.
	if marker < 0 || !echoGate(marker, lastTaskEchoIndex(above, task)) {
		return "", false
	}
	for i := marker + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if numberedChoiceRow.MatchString(l) {
			return "", false // a dialog is waiting for an answer
		}
		status := i >= len(above) || strings.HasPrefix(l, "⏵") || startsWithSpinner(l)
		if strings.Contains(l, "still running") || (status && backgroundWorkCount.MatchString(l)) {
			return "", false
		}
	}

	from := marker - finishedIdleScreenLines
	if from < 0 {
		from = 0
	}
	var b strings.Builder
	for _, l := range above[from : marker+1] {
		b.WriteString(strings.TrimRight(l, " "))
		b.WriteByte('\n')
	}
	// lines[len(above)] is the rule above the input box; its text runs to the
	// next rule.
	for i := len(above) + 1; i < len(lines) && !isBoxSeparator(strings.TrimSpace(lines[i])); i++ {
		b.WriteString(strings.TrimRight(lines[i], " "))
		b.WriteByte('\n')
	}
	return b.String(), true
}

func startsWithSpinner(l string) bool {
	for _, r := range l {
		return strings.ContainsRune(spinnerGlyphs, r)
	}
	return false
}

// liftRelaunchHold records that a relaunched session got new input, so a
// completion marker seen from now on belongs to this run.
func (m *Manager) liftRelaunchHold(fullID string) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
	}
	if !ok || !m.store.Snapshot(sess).AwaitingInputAfterRelaunch {
		return
	}
	m.store.Mutate(func() { sess.AwaitingInputAfterRelaunch = false })
	if err := m.store.Save(sess); err != nil {
		m.journalf("[warn] save session %s after input: %v\n", sess.ID, err)
	}
}
