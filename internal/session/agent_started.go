// AgentStarted — reports whether the agent behind a session has started.
//
// A session counts as started when either its agent process is running in
// the tmux pane, or its recorded output (or the session record) shows the
// agent got going: a first assistant turn, a channel-ready event, or a
// delivered task. It is read-only: it sends no keys and changes no state.

package session

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
)

// agentPromptMarker is the "❯" prompt that starts the user's line in a
// Claude Code pane. The assistant's first turn follows it.
const agentPromptMarker = "❯"

// agentTurnMarkers are the lines that open an assistant turn: "•" is the
// response bullet and "⏺" the tool-use marker of current Claude Code.
var agentTurnMarkers = []string{"•", "⏺"}

// plainShells are the login/interactive shells that count as "no agent"
// when they are the pane's foreground command.
var plainShells = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "fish": true,
	"dash": true, "ksh": true, "tcsh": true,
}

// AgentStarted reports whether the agent for fullID has started. An unknown
// session reports false. Checks run cheapest first: the session record
// (channel-ready, delivered task), then the recorded output, then the pane's
// process.
func (m *Manager) AgentStarted(fullID string) bool {
	m.mu.Lock()
	sess, ok := m.store.Get(fullID)
	if !ok {
		m.mu.Unlock()
		return false
	}
	tmuxSession, logFile, channelReady := sess.TmuxSession, sess.LogFile, sess.ChannelReady
	m.mu.Unlock()

	if channelReady || m.TaskDelivered(fullID) {
		return true
	}
	if logShowsAssistantTurn(logFile) {
		return true
	}
	return m.agentProcessRunning(tmuxSession)
}

// agentProcessRunning reports whether the agent process runs in the pane. A
// pane whose foreground command is not a shell is taken as the agent. A plain
// shell in front counts only when it has an agent child on the pane's tty.
func (m *Manager) agentProcessRunning(tmuxSession string) bool {
	if tmuxSession == "" {
		return false // virtual sessions (council, agent, ...) have no pane
	}
	cmd := m.tmux.PaneCommand(tmuxSession)
	if cmd == "" {
		return false // no such pane, or tmux could not answer
	}
	if !isPlainShell(cmd) {
		return true
	}
	return ttyRunsAgent(m.tmux.PaneTTY(tmuxSession))
}

// ttyRunsAgent lists the processes on the pane's tty and reports whether any
// is not a plain shell. tty is a device path such as "/dev/pts/7".
func ttyRunsAgent(tty string) bool {
	name := strings.TrimPrefix(tty, "/dev/")
	if name == "" {
		return false
	}
	out, err := exec.Command("ps", "-t", name, "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return commsRunAgent(string(out))
}

// commsRunAgent reports whether a newline-separated list of process names
// holds any name that is not a plain shell. Pure: no I/O.
func commsRunAgent(comms string) bool {
	for _, c := range strings.Split(comms, "\n") {
		c = strings.TrimSpace(c)
		if c != "" && !isPlainShell(c) {
			return true
		}
	}
	return false
}

// isPlainShell reports whether a process or pane command name is a plain
// shell. A login shell may report its argv0 with a leading "-" ("-bash").
func isPlainShell(name string) bool {
	return plainShells[strings.TrimPrefix(name, "-")]
}

// logShowsAssistantTurn reports whether the recorded output file at path
// shows an assistant turn. A missing or unreadable file reports false.
func logShowsAssistantTurn(path string) bool {
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck
	return outputShowsAssistantTurn(f)
}

// outputShowsAssistantTurn reports whether the output contains an assistant
// turn marker on a line after the first prompt line. It streams the input and
// stops at the first match. ANSI escapes are stripped first. Pure apart from
// reading r.
func outputShowsAssistantTurn(r io.Reader) bool {
	br := bufio.NewReader(r)
	promptSeen := false
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			plain := StripANSI(line)
			if !promptSeen {
				promptSeen = strings.Contains(plain, agentPromptMarker)
			} else if containsAnyMarker(plain, agentTurnMarkers) {
				return true
			}
		}
		if err != nil {
			return false
		}
	}
}

// containsAnyMarker reports whether s contains any of markers.
func containsAnyMarker(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
