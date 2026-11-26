// A claude-code agent can exit before it was ever given its task: Claude Code
// shows a start-up dialog, datawatch declines it, and the agent quits. The
// typed launch line then prints its completion line from the shell. That is not
// finished work. Such a session ends failed, with the reason on its record, and
// nothing completes or revives it. Every path that would end or complete a
// claude-code session asks here first.
//
// Failing closes a pane, so it needs proof, and what cannot be told apart is
// left alone:
//   - either datawatch declined a real dialog on this launch, or this daemon
//     process is still to type the session's first task;
//   - what the agent showed is read from the visible live screen only;
//   - the agent is gone only when tmux says the terminal is not there, or when
//     nothing but the pane's own shell is left on it.

package session

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

const launchFailedPrefix = "launch failed: "

// paneProc is one process of the system's process table.
type paneProc struct {
	pid, ppid int
	tty, comm string
}

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

// declinedMenuRow is one numbered choice of a dialog; group 1 is the cursor.
var declinedMenuRow = regexp.MustCompile(`^(❯[\s\x{00a0}]*)?\d+\.[\s\x{00a0}]+\S`)

// declinedDialogShape reports that the live screen ends with a dialog: its
// footer is the last line, numbered choices with one cursor sit right above it,
// and nothing on the screen shows an agent at work. It decides only whether a
// declined dialog is recorded, never which keys are sent or when. Text that
// merely names a dialog's words does not have this shape.
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
	kind := classifyStartupDialog(screen).Kind
	if kind != StartupDialogConfirmUnsafe || !declinedDialogShape(screen) {
		return
	}
	snap := m.store.Snapshot(sess)
	if taskReachedAgent(&snap) {
		return
	}
	dialog, files := startupDialogDetails(screen)
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

// armFirstDelivery marks a fresh start whose task this daemon process is going
// to type. The mark is not saved: a restarted daemon cannot know who has used
// the session since.
func (m *Manager) armFirstDelivery(fullID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deliveryArmed == nil {
		m.deliveryArmed = map[string]bool{}
	}
	m.deliveryArmed[fullID] = true
}

// noteOtherInput records input that is not the daemon typing the task or
// answering a start-up dialog: someone else is using the session.
func (m *Manager) noteOtherInput(fullID, source string) {
	if source == "channel-task" || source == "auto-accept-disclaimer" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.deliveryArmed, fullID)
}

// taskReachedAgent reports a delivery the record proves.
func taskReachedAgent(snap *Session) bool {
	return snap.TaskVerifiedAt != nil || (snap.TaskDeliveredAt != nil && !snap.TaskDeliveryUnverified)
}

// exitWouldBeLaunchFailure reports whether an exit of snap's agent now would
// mean its launch failed, and says why.
func (m *Manager) exitWouldBeLaunchFailure(snap *Session) (string, bool) {
	if snap.BackendFamily != "claude-code" || snap.Subprocess || snap.TmuxSession == "" || taskReachedAgent(snap) {
		return "", false
	}
	// The daemon is still to type the first task of this start, and nothing
	// shows that the agent has been given anything else to do.
	m.mu.Lock()
	awaiting := m.deliveryArmed[snap.FullID] && !m.agentSeenWorking[snap.FullID] &&
		snap.DaemonDeliversTask && snap.Task != ""
	m.mu.Unlock()
	dialog := ""
	if d := snap.StartupDecline; d != nil {
		dialog = fmt.Sprintf("the start-up dialog %q that datawatch declined (sent Esc)", d.Dialog)
		files := ""
		if len(d.Files) > 0 {
			files = "; settings file(s) named by the dialog: " + strings.Join(d.Files, ", ") +
				" (fix or move them, then restart the session)"
		}
		if m.now().Sub(d.At) <= declineExitWindow {
			if awaiting {
				return launchFailedPrefix + "the agent exited before the task was delivered, at " + dialog + files, true
			}
			return launchFailedPrefix + "the agent exited at " + dialog + files, true
		}
		dialog = "; earlier it showed " + dialog + files
	}
	if !awaiting {
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
	delete(m.deliveryArmed, fullID)
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
// relaunch is judged on its own, and the daemon does not type its task for the
// first time.
func (m *Manager) resetLaunchEvidence(sess *Session) {
	m.store.Mutate(func() {
		sess.LaunchFailure = ""
		sess.StartupDecline = nil
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agentSeenWorking, sess.FullID)
	delete(m.deliveryArmed, sess.FullID)
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

// agentExitIndex is the index of the exit line in lines, or -1.
func agentExitIndex(lines []string) int {
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
			return i
		}
	}
	return -1
}

// launchScreen is the visible live screen of a pane: not the view someone has
// scrolled back to, and nothing from the scrollback. ok is false when tmux
// cannot be read.
func (m *Manager) launchScreen(tmuxSession string) (string, bool) {
	pane, err := m.tmux.CapturePaneLiveTail(tmuxSession)
	if err != nil {
		return "", false
	}
	return StripANSI(pane), true
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

func (m *Manager) noteAgentWorked(sess *Session) {
	m.mu.Lock()
	if m.agentSeenWorking == nil {
		m.agentSeenWorking = map[string]bool{}
	}
	m.agentSeenWorking[sess.FullID] = true
	m.mu.Unlock()
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

// paneProcesses lists the system's processes with the pane's shell and tty. ok
// is false when any of it cannot be read.
func (m *Manager) paneProcesses(tmuxSession string) (shellPID int, tty string, procs []paneProc, ok bool) {
	pider, can := m.tmux.(PanePIDer)
	if !can {
		return 0, "", nil, false
	}
	shellPID, tty = pider.PanePID(tmuxSession), m.tmux.PaneTTY(tmuxSession)
	if shellPID <= 0 || tty == "" {
		return 0, "", nil, false
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,tty=,comm=").Output()
	if err != nil {
		return 0, "", nil, false
	}
	return shellPID, tty, parseProcessTable(string(out)), true
}

func parseProcessTable(out string) []paneProc {
	var procs []paneProc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, paneProc{pid: pid, ppid: ppid, tty: f[2], comm: strings.Join(f[3:], " ")})
	}
	return procs
}

// onlyShellLeft reports that the pane's own shell is there and that nothing
// else is on its terminal or anywhere under it: no agent, no wrapper around
// one, nothing it left behind.
func onlyShellLeft(shellPID int, tty string, procs []paneProc) bool {
	tty = strings.TrimPrefix(tty, "/dev/")
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.pid] = p.ppid
	}
	underShell := func(pid int) bool {
		for hops := 0; hops < len(procs) && pid > 1; hops++ {
			if pid = parent[pid]; pid == shellPID {
				return true
			}
		}
		return false
	}
	shellSeen := false
	for _, p := range procs {
		if p.pid == shellPID {
			shellSeen = p.tty == tty && isPlainShell(filepath.Base(p.comm))
			continue
		}
		if p.tty == tty || underShell(p.pid) {
			return false
		}
	}
	return shellSeen
}

// terminalProvenGone reports that tmux answered that the terminal is not
// there. A tmux that cannot be asked proves nothing.
func (m *Manager) terminalProvenGone(tmuxSession string) bool {
	prober, ok := m.tmux.(paneGoneProber)
	return ok && prober.SessionGone(tmuxSession)
}

// agentGone reports proof that no agent is left on sess's terminal.
func (m *Manager) agentGone(sess *Session) bool {
	if m.terminalProvenGone(sess.TmuxSession) {
		return true
	}
	if !m.tmux.SessionExists(sess.TmuxSession) {
		return false
	}
	list := m.paneProcsFn
	if list == nil {
		list = m.paneProcesses
	}
	shellPID, tty, procs, ok := list(sess.TmuxSession)
	return ok && onlyShellLeft(shellPID, tty, procs)
}

// endMissingTerminal is called where a session's terminal looks gone. It
// reports true when the caller must not complete the session: its launch
// failed and it is ended here, or it would have and tmux cannot yet say that
// the terminal is gone, so it is left for a later look.
func (m *Manager) endMissingTerminal(fullID string) bool {
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
	if _, never := m.exitWouldBeLaunchFailure(&snap); !never {
		return false
	}
	if m.terminalProvenGone(snap.TmuxSession) {
		m.failIfNeverStarted(fullID)
	}
	return true
}

// exitLineNeedsScreen reports a session whose exit line in the output log must
// not complete it: the watchers decide, from the live screen.
func (m *Manager) exitLineNeedsScreen(fullID string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	snap := m.store.Snapshot(sess)
	if snap.LaunchFailure != "" {
		return snap.State.IsTerminal()
	}
	return snap.BackendFamily == "claude-code" && !taskReachedAgent(&snap) && liveForLaunch(snap.State) &&
		(snap.StartupDecline != nil || snap.DaemonDeliversTask)
}

// guardEarlyExit looks at the live screen of a claude-code session before
// anything may complete it. It reports true when nothing on it may: its launch
// failed (and it is ended here once the agent is proven gone), or it is an
// interactive session still waiting for a typed task whose agent has left.
// A screen without the exit line is remembered for what it shows of the agent.
func (m *Manager) guardEarlyExit(fullID string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	snap := m.store.Snapshot(sess)
	if snap.LaunchFailure != "" {
		return snap.State.IsTerminal()
	}
	if snap.BackendFamily != "claude-code" || snap.TmuxSession == "" || taskReachedAgent(&snap) || !liveForLaunch(snap.State) {
		return false
	}
	if snap.StartupDecline == nil && !snap.DaemonDeliversTask {
		return false // nothing hangs on what this screen shows
	}
	screen, read := m.launchScreen(snap.TmuxSession)
	if !read {
		_, never := m.exitWouldBeLaunchFailure(&snap)
		return never
	}
	lines := strings.Split(screen, "\n")
	exit := agentExitIndex(lines)
	before := lines
	if exit >= 0 {
		before = lines[:exit]
	}
	if showsAgentAtWork(before) {
		m.noteAgentWorked(sess)
		m.forgetDecline(sess)
	}
	if exit < 0 {
		if snap.StartupDecline != nil && showsFramedInputBox(screen) {
			m.forgetDecline(sess)
		}
		return false
	}
	snap = m.store.Snapshot(sess)
	if _, never := m.exitWouldBeLaunchFailure(&snap); never {
		if m.agentGone(&snap) {
			m.failIfNeverStarted(fullID)
		}
		return true
	}
	// Not judged a failed launch. Completing an interactive session here
	// would only have the reconciler set it running again, over and over.
	return snap.DaemonDeliversTask && !snap.OneShot
}

// exitHeld reports, without changing anything, that a session whose launch
// would count as failed shows the exit line: its completion line is not
// finished work.
func (m *Manager) exitHeld(fullID, tmuxSession string) bool {
	if !m.EndedBeforeTask(fullID) {
		return false
	}
	screen, read := m.launchScreen(tmuxSession)
	return !read || agentExitIndex(strings.Split(screen, "\n")) >= 0
}

// failMissedEarlyExits ends the failed launches whose exit no watcher saw, as
// the reaper does for a missed completion line.
func (m *Manager) failMissedEarlyExits() {
	for _, rec := range m.store.List() {
		snap := m.store.Snapshot(rec)
		if snap.Hostname != m.hostname || !liveForLaunch(snap.State) {
			continue
		}
		if _, never := m.exitWouldBeLaunchFailure(&snap); never {
			m.guardEarlyExit(snap.FullID)
		}
	}
}
