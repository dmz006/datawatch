// One launch failure the daemon can prove. Claude Code shows a start-up dialog,
// datawatch declines it, the agent quits, and the shell prints the launch
// command's completion line. That was read as finished work. The session ends
// failed instead, once, and its leftover shell is closed, only when all of this
// holds:
//
//   - datawatch declined a real dialog on this launch, a short while ago;
//   - it is a channel launch whose task the daemon types, and the task was
//     never delivered;
//   - nothing but start-up dialog answers was sent through datawatch;
//   - the agent has left: tmux says the terminal is gone, or the live screen
//     shows the exit line and only the pane's own shell remains.
//
// It is asked where a session would otherwise be completed or brought back.
// When the answer is no, that code goes on exactly as it would have.

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

// declineExitWindow is how soon after a declined dialog an exit is put down to
// that dialog. A declined dialog that ends the agent does so within seconds.
const declineExitWindow = 2 * time.Minute

const launchFailedPrefix = "launch failed: "

var settingsFileInDialog = regexp.MustCompile(`^/.*settings[^/]*\.json$`)

// startupDialogDetails reads from a screen what to call the dialog on it and the
// settings files it names. A long path the terminal wrapped is joined again.
func startupDialogDetails(screen string) (dialog string, files []string) {
	lines := strings.Split(StripANSI(screen), "\n")
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

// declinedMenuRow is one numbered choice of a dialog; group 1 is the cursor.
var declinedMenuRow = regexp.MustCompile(`^(❯[\s\x{00a0}]*)?\d+\.[\s\x{00a0}]+\S`)

// declinedDialogShape reports that the live screen ends with a dialog: its
// footer is the last line, numbered choices with one cursor sit right above it,
// and nothing on the screen shows an agent at work. It decides only whether a
// declined dialog is recorded, never which keys are sent or when.
func declinedDialogShape(screen string) bool {
	lines := nonEmptyTail(StripANSI(screen), 60)
	for len(lines) > 0 && isFrameOnlyLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 3 {
		return false
	}
	footer := strings.ToLower(lines[len(lines)-1])
	if !strings.HasPrefix(footer, "enter to confirm") || !strings.Contains(footer, "esc to cancel") {
		return false
	}
	rows, cursors := 0, 0
	for i := len(lines) - 2; i >= 0; i-- {
		row := declinedMenuRow.FindStringSubmatch(lines[i])
		if row == nil {
			break
		}
		rows++
		if row[1] != "" {
			cursors++
		}
	}
	return rows >= 2 && cursors == 1 && !showsAgentAtWork(lines)
}

// noteStartupDecline records the dialog that was just declined, if screen, read
// before the key was sent, really showed one. The first dialog of a launch is
// kept: a later decline of the same screen adds nothing.
func (m *Manager) noteStartupDecline(sess *Session, screen string) {
	if classifyStartupDialog(screen).Kind != StartupDialogConfirmUnsafe || !declinedDialogShape(screen) {
		return
	}
	dialog, files := startupDialogDetails(screen)
	now := m.now()
	m.store.Mutate(func() {
		if d := sess.StartupDecline; d != nil && now.Sub(d.At) <= declineExitWindow {
			return
		}
		sess.StartupDecline = &StartupDecline{Dialog: dialog, Files: files, At: now}
	})
	_ = m.store.Save(sess)
}

// noteInputSent records on the session that datawatch has sent it something
// besides a start-up dialog answer. It is saved: a restart of the daemon must
// not forget it.
func (m *Manager) noteInputSent(fullID string) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return
	}
	changed := false
	m.store.Mutate(func() {
		changed = sess.BackendFamily == "claude-code" && !sess.LaunchInputSent
		if changed {
			sess.LaunchInputSent = true
		}
	})
	if changed {
		_ = m.store.Save(sess)
	}
}

// taskReachedAgent reports a delivery the record proves.
func taskReachedAgent(snap *Session) bool {
	return snap.TaskVerifiedAt != nil || (snap.TaskDeliveredAt != nil && !snap.TaskDeliveryUnverified)
}

// launchFailureIfAgentLeft reports whether everything but the agent having left
// is proven for snap, and the reason it would then have failed.
func (m *Manager) launchFailureIfAgentLeft(snap *Session) (string, bool) {
	d := snap.StartupDecline
	if snap.BackendFamily != "claude-code" || snap.Subprocess || snap.TmuxSession == "" ||
		!snap.DaemonDeliversTask || snap.Task == "" ||
		d == nil || m.now().Sub(d.At) > declineExitWindow ||
		snap.LaunchInputSent || taskReachedAgent(snap) {
		return "", false
	}
	reason := fmt.Sprintf(launchFailedPrefix+"the agent exited at the start-up dialog %q that datawatch declined (sent Esc); no task was delivered", d.Dialog)
	if len(d.Files) > 0 {
		reason += "; settings file(s) named by the dialog: " + strings.Join(d.Files, ", ") +
			" (fix or move them, then restart the session)"
	}
	return reason, true
}

// showsAgentAtWork reports lines only a working or answering agent draws.
func showsAgentAtWork(lines []string) bool {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "●") {
			return true
		}
		for _, mark := range agentTurnMarkers {
			if strings.HasPrefix(t, mark) {
				return true
			}
		}
	}
	return paneShowsWork(nonEmptyTail(strings.Join(lines, "\n"), 40))
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

// forgetDecline drops the recorded dialog: the agent got past it.
func (m *Manager) forgetDecline(sess *Session) {
	changed := false
	m.store.Mutate(func() {
		changed = sess.StartupDecline != nil
		sess.StartupDecline = nil
	})
	if changed {
		_ = m.store.Save(sess)
	}
}

// onlyShellLeft reports that the pane's own shell is there and that nothing
// else is on its terminal or anywhere under it: no agent, no wrapper around
// one, nothing it left behind.
func onlyShellLeft(shellPID int, tty string, procs []paneProc) bool {
	shell, ok := paneShellRow(shellPID, tty, procs)
	if !ok {
		return false
	}
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.pid] = p.ppid
	}
	for _, p := range procs {
		if p.pid == shell.pid {
			continue
		}
		if p.tty == shell.tty {
			return false
		}
		for pid, hops := p.pid, 0; hops < len(procs) && pid > 1; hops++ {
			if pid = parent[pid]; pid == shell.pid {
				return false
			}
		}
	}
	return true
}

// terminalProvenGone reports that tmux answered that the terminal is not
// there. A tmux that cannot be asked proves nothing.
func (m *Manager) terminalProvenGone(tmuxSession string) bool {
	prober, ok := m.tmux.(paneGoneProber)
	return ok && prober.SessionGone(tmuxSession)
}

func liveForLaunch(s State) bool {
	return s == StateRunning || s == StateWaitingInput || s == StateRateLimited || s == StateStalled
}

// endIfLaunchFailed ends fullID as a failed launch if that is proven now. It
// reports true only when the session is (now) a failed launch; the caller then
// has nothing left to do. A screen that shows the agent got past the dialog
// drops the record of it.
func (m *Manager) endIfLaunchFailed(fullID string) bool {
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
	reason, candidate := m.launchFailureIfAgentLeft(&snap)
	if !candidate {
		return false
	}
	if !m.terminalProvenGone(snap.TmuxSession) {
		screen, read := m.launchScreen(snap.TmuxSession)
		if !read {
			return false
		}
		lines := strings.Split(screen, "\n")
		exit := agentExitIndex(lines)
		if exit < 0 {
			if showsAgentAtWork(lines) || showsFramedInputBox(screen) {
				m.forgetDecline(sess)
			}
			return false
		}
		if showsAgentAtWork(lines[:exit]) {
			m.forgetDecline(sess)
			return false
		}
		shellPID, tty, procs, listed := m.paneProcesses(snap.TmuxSession)
		if !listed || !onlyShellLeft(shellPID, tty, procs) {
			return false
		}
	}
	m.endAsLaunchFailure(sess, reason, EndReason{Caller: callerAt(1), Reason: reason})
	return true
}

// endAsLaunchFailure ends sess as a failed launch. More than one watcher sees an
// exit, so the state is checked and changed in one step and the session ends
// once. Nothing is committed or pushed: the agent was given no task.
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
	if onUndelivered != nil {
		onUndelivered(fullID, reason)
	}
	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	m.emitSessionEnd(sess)
}

// LaunchFailed returns why fullID's launch failed, or "" if it did not.
func (m *Manager) LaunchFailed(fullID string) string {
	sess, ok := m.GetSession(fullID)
	if !ok {
		return ""
	}
	return m.store.Snapshot(sess).LaunchFailure
}

// resetLaunchEvidence forgets what an earlier launch of this session showed. A
// relaunch is judged on its own.
func (m *Manager) resetLaunchEvidence(sess *Session) {
	m.store.Mutate(func() {
		sess.LaunchFailure = ""
		sess.StartupDecline = nil
		sess.LaunchInputSent = false
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.deliveryFailure, sess.FullID)
}
