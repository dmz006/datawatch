// The typing guard. When a claude-code agent has left, what is in the pane is a
// shell, which would run typed text outside every check the agent has.
//
// Before typing a task or queued input, datawatch looks for two signs: the live
// screen shows the line the launch command prints after the agent, or the
// pane's shell alone is reading the terminal. It does not know whether the
// agent is there: it types whenever neither sign shows, also when the agent has
// stopped reading or has exited under a wrapper script still in the foreground.
// A person's own input, MCP send_input and send_saved_command, the daemon's
// single menu keys and the answers to start-up dialogs are not held to it.

package session

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrAgentLeft is returned when text is not typed because Claude has left the
// session's terminal.
var ErrAgentLeft = errors.New("not typed: Claude has left this session's terminal, so its shell would run the text")

// claudeExitSummary is the summary of the completion line the claude-code
// launch command types after the agent: it says the agent exited.
const claudeExitSummary = "claude done"

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

// paneProc is one process of the system's process table.
type paneProc struct {
	pid, ppid, pgid, tpgid int
	tty, comm              string
}

// paneProcesses lists the system's processes with the pane's shell and tty. ok
// is false when any of it cannot be read.
func (m *Manager) paneProcesses(tmuxSession string) (shellPID int, tty string, procs []paneProc, ok bool) {
	list := m.paneProcsFn
	if list != nil {
		return list(tmuxSession)
	}
	pider, can := m.tmux.(PanePIDer)
	if !can {
		return 0, "", nil, false
	}
	shellPID, tty = pider.PanePID(tmuxSession), m.tmux.PaneTTY(tmuxSession)
	if shellPID <= 0 || tty == "" {
		return 0, "", nil, false
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,tpgid=,tty=,comm=").Output()
	if err != nil {
		return 0, "", nil, false
	}
	return shellPID, tty, parseProcessTable(string(out)), true
}

func parseProcessTable(out string) []paneProc {
	var procs []paneProc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		var n [4]int
		bad := false
		for i := range n {
			v, err := strconv.Atoi(f[i])
			if err != nil {
				bad = true
				break
			}
			n[i] = v
		}
		if bad {
			continue
		}
		procs = append(procs, paneProc{pid: n[0], ppid: n[1], pgid: n[2], tpgid: n[3],
			tty: f[4], comm: strings.Join(f[5:], " ")})
	}
	return procs
}

// paneShellRow finds the pane's own shell on the pane's terminal.
func paneShellRow(shellPID int, tty string, procs []paneProc) (paneProc, bool) {
	tty = strings.TrimPrefix(tty, "/dev/")
	for _, p := range procs {
		if p.pid == shellPID {
			return p, p.tty == tty && isPlainShell(filepath.Base(p.comm))
		}
	}
	return paneProc{}, false
}

// shellReadsKeyboard reports that the pane's shell is alone in the terminal's
// foreground group: it reads whatever is typed. A running agent is the
// foreground group, also behind a wrapper script; where job control is off it
// shares the shell's group, and the answer is no.
func shellReadsKeyboard(shellPID int, tty string, procs []paneProc) bool {
	shell, ok := paneShellRow(shellPID, tty, procs)
	if !ok || shell.tpgid <= 0 || shell.tpgid != shell.pgid {
		return false
	}
	for _, p := range procs {
		if p.pid != shell.pid && p.pgid == shell.pgid {
			return false
		}
	}
	return true
}

// heldToTypingGuard reports input the daemon types on its own: the task, and
// queued and other automated input.
func heldToTypingGuard(source string) bool {
	return source != StartupAcceptSource && IsAutomatedInputSource(source)
}

// refuseTypingWithoutAgent returns ErrAgentLeft when text typed into snap now
// would reach its shell and not Claude.
func (m *Manager) refuseTypingWithoutAgent(snap *Session) error {
	if snap.BackendFamily != "claude-code" || snap.Subprocess || snap.TmuxSession == "" {
		return nil
	}
	if screen, ok := m.launchScreen(snap.TmuxSession); ok && agentExitIndex(strings.Split(screen, "\n")) >= 0 {
		return ErrAgentLeft
	}
	if shellPID, tty, procs, ok := m.paneProcesses(snap.TmuxSession); ok && shellReadsKeyboard(shellPID, tty, procs) {
		return ErrAgentLeft
	}
	return nil
}

// mayTypeRateLimitResume holds the line that resumes a rate-limited session,
// which goes to tmux directly, to the typing guard.
func (m *Manager) mayTypeRateLimitResume(sess *Session, msg string) bool {
	snap := m.store.Snapshot(sess)
	if err := m.refuseTypingWithoutAgent(&snap); err != nil {
		m.emitInputAudit(InputAuditEvent{Action: InputAuditRefused, SessionFullID: sess.FullID, Source: "rate-limit-resume", Length: len(msg), Reason: err.Error()})
		return false
	}
	m.noteInputSent(sess.FullID)
	return true
}
