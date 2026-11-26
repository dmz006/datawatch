// A claude-code agent can exit before it was ever given its task: Claude Code
// shows a start-up dialog, datawatch declines it, and the agent quits. The
// typed launch line then prints its completion line from the shell. That is not
// finished work. Such a session ends failed, with the reason on its record, and
// nothing completes or revives it. Every path that would end or complete a
// claude-code session asks here first.

package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// StartupDecline records a start-up dialog datawatch declined on a session.
type StartupDecline struct {
	Dialog string    `json:"dialog"`
	Files  []string  `json:"files,omitempty"` // settings files the dialog named
	At     time.Time `json:"at"`
}

// claudeExitSummary is the summary of the completion line the claude-code
// launch command types after the agent: it says the agent exited, not that the
// work is done.
const claudeExitSummary = "claude done"

// declineExitWindow is how soon after a declined dialog an exit is put down to
// that dialog. A declined dialog that ends the agent does so within seconds.
const declineExitWindow = 2 * time.Minute

// exitLineHoldLimit is how long the exit line may sit above a shell that still
// has a child process before the agent is taken to be gone anyway.
const exitLineHoldLimit = 15 * time.Second

const launchFailedPrefix = "launch failed: "

var settingsFileInDialog = regexp.MustCompile(`^/.*settings[^/]*\.json$`)

// startupDialogDetails reads from a pane what to call the dialog on it and the
// settings files it names. A long path the terminal wrapped is joined again.
func startupDialogDetails(pane string) (dialog string, files []string) {
	lines := strings.Split(StripANSI(pane), "\n")
	footer := len(lines)
	for i, l := range lines {
		if strings.Contains(strings.ToLower(l), "enter to confirm") {
			footer = i
			break
		}
	}
	// The dialog starts after the last rule above its footer.
	start := 0
	for i := 0; i < footer; i++ {
		if t := strings.TrimSpace(lines[i]); t != "" && isFrameOnlyLine(t) {
			start = i + 1
		}
	}
	for i := start; i < footer; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || isFrameOnlyLine(t) {
			continue
		}
		if dialog == "" {
			dialog = t
		}
		if !strings.HasPrefix(t, "/") {
			continue
		}
		path := t
		for j := i; !strings.HasSuffix(path, ".json") && j+1 < footer && j < i+4; j++ {
			next := strings.TrimSpace(lines[j+1])
			if next == "" || strings.ContainsAny(next, "└❯") {
				break
			}
			path += next
		}
		if settingsFileInDialog.MatchString(path) {
			files = append(files, path)
		}
	}
	if r := []rune(dialog); len(r) > 80 {
		dialog = string(r[:80]) + "…"
	}
	return dialog, files
}

// noteStartupDecline records the dialog about to be declined. The first dialog
// of a launch is kept: a later decline of the same screen adds nothing.
func (m *Manager) noteStartupDecline(sess *Session, pane string) {
	kind := classifyStartupDialog(pane).Kind
	if kind != StartupDialogConfirmUnsafe && kind != StartupDialogUpdatePrompt {
		return
	}
	dialog, files := startupDialogDetails(pane)
	if dialog == "" {
		dialog = string(kind)
	}
	now := m.now()
	m.store.Mutate(func() {
		if d := sess.StartupDecline; d != nil && now.Sub(d.At) <= declineExitWindow {
			return
		}
		sess.StartupDecline = &StartupDecline{Dialog: dialog, Files: files, At: now}
	})
	_ = m.store.Save(sess)
}

// taskReachedAgent reports a delivery the record proves.
func taskReachedAgent(snap *Session) bool {
	return snap.TaskVerifiedAt != nil || (snap.TaskDeliveredAt != nil && !snap.TaskDeliveryUnverified)
}

// exitWouldBeLaunchFailure reports whether an exit of snap's agent now would
// mean it never ran its task, and says why.
func (m *Manager) exitWouldBeLaunchFailure(snap *Session) (string, bool) {
	if snap.BackendFamily != "claude-code" || snap.Subprocess || snap.TmuxSession == "" || taskReachedAgent(snap) {
		return "", false
	}
	dialog := ""
	if d := snap.StartupDecline; d != nil {
		dialog = fmt.Sprintf("the start-up dialog %q that datawatch declined (sent Esc)", d.Dialog)
		files := ""
		if len(d.Files) > 0 {
			files = "; settings file(s) named by the dialog: " + strings.Join(d.Files, ", ") +
				" (fix or move them, then restart the session)"
		}
		if m.now().Sub(d.At) <= declineExitWindow {
			return launchFailedPrefix + "the agent exited before the task was delivered, at " + dialog + files, true
		}
		dialog = "; earlier it showed " + dialog + files
	}
	if !snap.DaemonDeliversTask || snap.Task == "" {
		return "", false
	}
	// A claim that was never verified may still have arrived: a person can
	// send the typed text themselves. Work seen on screen or in the recorded
	// output counts as arrival.
	m.mu.Lock()
	worked := m.agentSeenWorking[snap.FullID]
	m.mu.Unlock()
	if worked || (snap.TaskDeliveredAt != nil && logShowsAssistantTurn(snap.LogFile)) {
		return "", false
	}
	return launchFailedPrefix + "the agent exited before its task was delivered" + dialog, true
}

// LaunchFailed returns why fullID's launch failed, or "" if it did not.
func (m *Manager) LaunchFailed(fullID string) string {
	sess, ok := m.GetSession(fullID)
	if !ok {
		return ""
	}
	return m.store.Snapshot(sess).LaunchFailure
}

// EndedBeforeTask reports whether an exit of fullID's agent now would mean it
// never ran its task. A completion line from such a session proves nothing.
func (m *Manager) EndedBeforeTask(fullID string) bool {
	sess, ok := m.GetSession(fullID)
	if !ok {
		return false
	}
	snap := m.store.Snapshot(sess)
	if snap.LaunchFailure != "" {
		return true
	}
	_, never := m.exitWouldBeLaunchFailure(&snap)
	return never
}

func liveForLaunch(s State) bool {
	return s == StateRunning || s == StateWaitingInput || s == StateRateLimited || s == StateStalled
}

// failIfNeverStarted is called where the agent of fullID is known to be gone.
// If it never ran its task the session ends failed here. It reports whether the
// session is (now) a failed launch, in which case the caller must not complete
// it.
func (m *Manager) failIfNeverStarted(fullID string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	snap := m.store.Snapshot(sess)
	if snap.LaunchFailure != "" {
		return snap.State.IsTerminal()
	}
	if !liveForLaunch(snap.State) {
		return false
	}
	reason, never := m.exitWouldBeLaunchFailure(&snap)
	if !never {
		return false
	}
	m.endAsLaunchFailure(sess, reason, EndReason{Caller: callerAt(1), Reason: reason})
	return true
}

// endAsLaunchFailure ends sess as a failed launch. More than one watcher sees an
// exit, so the state is checked and changed in one step and the session ends
// once. Nothing is committed or pushed: the agent did no work.
func (m *Manager) endAsLaunchFailure(sess *Session, reason string, why EndReason) {
	var oldState State
	ended := false
	fullID := sess.FullID
	m.store.Mutate(func() {
		if sess.LaunchFailure != "" || !liveForLaunch(sess.State) {
			return
		}
		oldState, ended = sess.State, true
		now := time.Now()
		sess.State, sess.UpdatedAt = StateFailed, now
		sess.LaunchFailure = reason
		stampEnd(sess, now, endNoteFor(oldState, StateFailed, why))
		// A claim still held would read as a delivered task.
		sess.TaskDeliveredAt, sess.TaskDeliveryUnverified, sess.TaskVerifiedAt = nil, false, nil
		sess.PendingInput, sess.ClaudeAlive = "", nil
	})
	if !ended {
		return
	}
	m.mu.Lock()
	delete(m.taskDeliveredAt, fullID)
	delete(m.exitLineSince, fullID)
	if m.deliveryFailure == nil {
		m.deliveryFailure = make(map[string]string)
	}
	m.deliveryFailure[fullID] = reason
	if cancel, ok := m.monitors[fullID]; ok {
		cancel()
		delete(m.monitors, fullID)
	}
	if fifo, ok := m.encFIFOs[fullID]; ok {
		_ = fifo.Close()
		delete(m.encFIFOs, fullID)
	}
	if sp, ok := m.streamPipes[fullID]; ok {
		sp.Close()
		delete(m.streamPipes, fullID)
	}
	tracker := m.trackers[fullID]
	onUndelivered := m.onTaskDeliveryFailed
	m.mu.Unlock()
	_ = m.store.Save(sess)
	if m.schedStore != nil {
		m.schedStore.CancelBySession(fullID)
	}
	// What is left in the pane is a bare shell. Left open, it would take
	// typed text as commands.
	m.killPane(fullID, m.store.Snapshot(sess).TmuxSession, reason)
	m.releaseCommitGuard(fullID, m.store.Snapshot(sess).ProjectDir)
	if m.onLeaseRelease != nil {
		m.onLeaseRelease(sess)
	}
	m.upsertLedger(sess)
	m.recordEnd(sess, tracker, oldState, StateFailed, why)
	if onUndelivered != nil && sess.Task != "" {
		onUndelivered(fullID, reason)
	}
	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	m.emitSessionEnd(sess)
}

// resetLaunchEvidence forgets what an earlier launch of this session showed. A
// relaunch is judged on its own.
func (m *Manager) resetLaunchEvidence(sess *Session) {
	m.store.Mutate(func() {
		sess.LaunchFailure = ""
		sess.StartupDecline = nil
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agentSeenWorking, sess.FullID)
	delete(m.exitLineSince, sess.FullID)
	delete(m.deliveryFailure, sess.FullID)
}

// isAgentExitLine reports whether l is the line the claude-code launch command
// prints once the agent has exited. The typed command and its wrapped tail end
// with a quote and are not that line.
func isAgentExitLine(l string) bool {
	l = strings.TrimSpace(StripANSI(l))
	return matchesCompletionPattern(l, oneShotCompletionMarker) &&
		completionSummary(l, oneShotCompletionMarker) == claudeExitSummary
}

func paneShowsAgentExit(lines []string) bool {
	for i, l := range lines {
		if !isAgentExitLine(l) {
			continue
		}
		// A wrap just before the closing quote leaves the quote on the
		// next line: that is the typed command, not its output.
		wrapped := false
		if i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			wrapped = strings.HasPrefix(next, "'") || strings.HasPrefix(next, `"`)
		}
		if !wrapped {
			return true
		}
	}
	return false
}

// agentGone reports whether sess's pane has gone back to its shell. When tmux
// cannot say, the agent is taken to be there.
func (m *Manager) agentGone(sess *Session) bool {
	if !m.tmux.SessionExists(sess.TmuxSession) {
		return true
	}
	if !isPlainShell(m.tmux.PaneCommand(sess.TmuxSession)) {
		return false
	}
	shellHasChild := m.paneProcessAlive(sess)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !shellHasChild {
		return true
	}
	if m.exitLineSince == nil {
		m.exitLineSince = map[string]time.Time{}
	}
	since, seen := m.exitLineSince[sess.FullID]
	if !seen {
		m.exitLineSince[sess.FullID] = m.now()
		return false
	}
	return m.now().Sub(since) >= exitLineHoldLimit
}

// guardEarlyExit looks at lines of a claude-code session's screen or output
// before any of them may complete it. It reports true when they must not be
// read further: the agent exited without running its task (the session is ended
// failed here), or the exit line is up and the agent is not yet seen gone.
// Lines that show the agent at its input box or at work are remembered.
func (m *Manager) guardEarlyExit(fullID string, lines []string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	snap := m.store.Snapshot(sess)
	if snap.LaunchFailure != "" {
		return snap.State.IsTerminal()
	}
	if snap.BackendFamily != "claude-code" || taskReachedAgent(&snap) || !liveForLaunch(snap.State) {
		return false
	}
	if !paneShowsAgentExit(lines) {
		m.noteAgentPastStartup(sess, &snap, lines)
		return false
	}
	if _, never := m.exitWouldBeLaunchFailure(&snap); !never {
		return false
	}
	if m.agentGone(&snap) {
		m.failIfNeverStarted(fullID)
	}
	return true
}

// noteAgentPastStartup records what a screen proves: an agent at its input box
// or at work got past any dialog datawatch declined, and one at work has been
// given something to do.
func (m *Manager) noteAgentPastStartup(sess *Session, snap *Session, lines []string) {
	if len(lines) < 2 {
		return // a single output line is not a screen
	}
	m.mu.Lock()
	delete(m.exitLineSince, snap.FullID)
	m.mu.Unlock()
	if snap.StartupDecline == nil && !snap.DaemonDeliversTask {
		return // nothing hangs on what this screen shows
	}
	screen := strings.Join(lines, "\n")
	kind, _ := classifyInputBox(screen, snap.Task)
	switch kind {
	case inputBoxBusy:
		m.mu.Lock()
		if m.agentSeenWorking == nil {
			m.agentSeenWorking = map[string]bool{}
		}
		m.agentSeenWorking[snap.FullID] = true
		m.mu.Unlock()
	case inputBoxEmpty:
		if !showsFramedInputBox(screen) {
			return
		}
	default:
		return
	}
	if snap.StartupDecline == nil {
		return
	}
	m.store.Mutate(func() { sess.StartupDecline = nil })
	_ = m.store.Save(sess)
}

// showsFramedInputBox reports the agent's own input box: its last ❯ row sits
// under a rule and no dialog footer is up. A shell prompt or a choice row can
// start with ❯ too.
func showsFramedInputBox(screen string) bool {
	if strings.Contains(strings.ToLower(screen), "enter to confirm") {
		return false
	}
	lines := nonEmptyTail(StripANSI(screen), 40)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "❯") {
			return i > 0 && isBoxSeparator(lines[i-1])
		}
	}
	return false
}

// failMissedEarlyExits ends the failed launches whose exit no watcher saw, as
// the reaper does for a missed completion line.
func (m *Manager) failMissedEarlyExits() {
	for _, rec := range m.store.List() {
		snap := m.store.Snapshot(rec)
		if snap.Hostname != m.hostname || !liveForLaunch(snap.State) {
			continue
		}
		if _, never := m.exitWouldBeLaunchFailure(&snap); !never {
			continue
		}
		out, err := m.tmux.CapturePaneScrollback(snap.TmuxSession, 500)
		if err == nil {
			m.guardEarlyExit(snap.FullID, strings.Split(StripANSI(out), "\n"))
		}
	}
}
