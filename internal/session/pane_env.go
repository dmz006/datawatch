// The environment of a session's worker process.
//
// tmux set-environment only reaches panes opened afterwards. A worker is typed
// into the shell of the first pane, which is already running by then, so every
// value meant for it must also be loaded into that shell before the launch.
//
// The shell is told to read each variable back from its own tmux session
// (show-environment -s prints shell assignments) and to evaluate the result
// inside a command substitution. Only variable names are typed: no value, and
// so no secret, reaches the pane, its scrollback, the output log, the shell
// history or a file.
package session

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// paneEnvName matches the names a POSIX shell can export. A name is typed into
// the pane shell, so nothing else is ever sent.
var paneEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PaneSessionMarkerEnv marks an environment that got DATAWATCH_SESSION_ID from
// a session's tmux pane. Its value is PaneSessionMarker of that session. The
// process reaper does not take the id of a marked environment as proof that the
// session may end the process.
const PaneSessionMarkerEnv = "DATAWATCH_PANE_SESSION"

// PaneSessionMarker is the marker value for the session fullID. It never equals
// the id itself, which the reaper would match.
func PaneSessionMarker(fullID string) string { return "pane:" + fullID }

// paneEnvLineMax keeps each typed line well below the terminal's line limit,
// which applies while the pane shell is still starting.
const paneEnvLineMax = 1000

// SetPaneEnv sets variables for a session's tmux session and records their
// names, so the next launch in that session loads them into the pane shell.
// A name a shell cannot export is skipped.
func (m *Manager) SetPaneEnv(tmuxSession string, env map[string]string) error {
	valid := make(map[string]string, len(env))
	skipped := 0
	for k, v := range env {
		if paneEnvName.MatchString(k) {
			valid[k] = v
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		// The name is not printed: a malformed entry can hold a value.
		fmt.Printf("[warn] tmux session %s: %d environment variable(s) with an invalid name were not set\n", tmuxSession, skipped)
	}
	if len(valid) == 0 {
		return nil
	}
	if err := m.tmux.SetEnvironment(tmuxSession, valid); err != nil {
		return err
	}
	m.paneEnvMu.Lock()
	defer m.paneEnvMu.Unlock()
	if m.paneEnvNames == nil {
		m.paneEnvNames = map[string]map[string]bool{}
	}
	if m.paneEnvNames[tmuxSession] == nil {
		m.paneEnvNames[tmuxSession] = map[string]bool{}
	}
	for k := range valid {
		m.paneEnvNames[tmuxSession][k] = true
	}
	return nil
}

// takePaneEnvNames returns, sorted, the names recorded for tmuxSession and
// forgets them.
func (m *Manager) takePaneEnvNames(tmuxSession string) []string {
	m.paneEnvMu.Lock()
	defer m.paneEnvMu.Unlock()
	names := make([]string, 0, len(m.paneEnvNames[tmuxSession]))
	for k := range m.paneEnvNames[tmuxSession] {
		names = append(names, k)
	}
	delete(m.paneEnvNames, tmuxSession)
	sort.Strings(names)
	return names
}

// paneEnvCommands builds the lines typed into the pane shell to load names
// from tmuxSession. They turn shell tracing off first, so a shell that traces
// commands cannot print the values it evaluates.
func paneEnvCommands(tmuxBin, tmuxSession string, names []string) []string {
	head := "set +x +v; for dw_k in"
	tail := `; do eval "$(` + shellSingleQuote(tmuxBin) + " show-environment -s -t " +
		shellSingleQuote(tmuxSession) + ` "$dw_k")"; done; unset dw_k`
	var lines []string
	var chunk strings.Builder
	flush := func() {
		if chunk.Len() > 0 {
			lines = append(lines, head+chunk.String()+tail)
			chunk.Reset()
		}
	}
	for _, n := range names {
		if chunk.Len() > 0 && len(head)+chunk.Len()+len(n)+1+len(tail) > paneEnvLineMax {
			flush()
		}
		chunk.WriteString(" " + n)
	}
	flush()
	return lines
}

// loadPaneEnv loads the recorded variables into sess's pane shell. Call it
// after the last SetPaneEnv and before anything else is typed for a launch.
// It must run before the worker pane scrub, which removes TMUX from the shell.
// Failure is logged, not fatal: the launch proceeds.
func (m *Manager) loadPaneEnv(sess *Session) {
	if sess == nil || sess.TmuxSession == "" {
		return
	}
	names := m.takePaneEnvNames(sess.TmuxSession)
	if len(names) == 0 {
		return
	}
	// The pane shell may have another PATH, so name the daemon's own tmux.
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		tmuxBin = "tmux"
	}
	for _, line := range paneEnvCommands(tmuxBin, sess.TmuxSession, names) {
		if err := m.tmux.SendKeys(sess.TmuxSession, line); err != nil {
			fmt.Printf("[warn] load environment into the pane of %s: %v\n", sess.ID, err)
			return
		}
	}
}

// setProfileEnv records the session's profile environment for its pane.
func (m *Manager) setProfileEnv(sess *Session, env map[string]string) {
	if len(env) == 0 {
		return
	}
	if err := m.SetPaneEnv(sess.TmuxSession, env); err != nil {
		fmt.Printf("[warn] set profile env for %s: %v\n", sess.ID, err)
	}
}

// prepareSubprocessEnv holds what a subprocess session's task needs beyond
// subprocessEnv: its profile environment and, for a guarded session, the commit
// guard. A subprocess has no pane, so both go straight into its environment.
// Call it before the session's runner starts. The identity variables stay the
// daemon's own.
func (m *Manager) prepareSubprocessEnv(sess *Session, profile map[string]string) {
	var env []string
	for k, v := range profile {
		switch k {
		case "DATAWATCH_SESSION_ID", "DATAWATCH_BASE_URL", "DATAWATCH_RESULT_TOKEN":
			continue
		}
		if paneEnvName.MatchString(k) {
			env = append(env, k+"="+v)
		}
	}
	sort.Strings(env)
	m.armCommitGuard(sess, "")
	if g := m.commitGuardFor(sess.FullID); g != nil {
		env = append(env, g.Env()...) // last, so it wins
	}
	if len(env) == 0 {
		return
	}
	m.paneEnvMu.Lock()
	defer m.paneEnvMu.Unlock()
	if m.subprocessExtraEnv == nil {
		m.subprocessExtraEnv = map[string][]string{}
	}
	m.subprocessExtraEnv[sess.FullID] = env
}

// takeSubprocessEnv returns and forgets what prepareSubprocessEnv held.
func (m *Manager) takeSubprocessEnv(fullID string) []string {
	m.paneEnvMu.Lock()
	defer m.paneEnvMu.Unlock()
	env := m.subprocessExtraEnv[fullID]
	delete(m.subprocessExtraEnv, fullID)
	return env
}

// restartProfileEnv returns the environment of the configured profile a stored
// session names, or nil. Environment values are not stored with a session, so
// only a named profile can be applied again.
func (m *Manager) restartProfileEnv(sess *Session) map[string]string {
	if m.cfg == nil || sess == nil || sess.Profile == "" {
		return nil
	}
	return m.cfg.Profiles[sess.Profile].Env
}

// setSessionIdentityEnv records DATAWATCH_SESSION_ID and DATAWATCH_BASE_URL,
// which hook scripts and session tools use to reach the daemon. Call it after
// setProfileEnv so a profile cannot change them.
func (m *Manager) setSessionIdentityEnv(sess *Session) {
	port := 8080
	if m.cfg != nil && m.cfg.Server.Port > 0 {
		port = m.cfg.Server.Port
	}
	if err := m.SetPaneEnv(sess.TmuxSession, map[string]string{
		"DATAWATCH_SESSION_ID": sess.FullID,
		"DATAWATCH_BASE_URL":   fmt.Sprintf("http://127.0.0.1:%d", port),
		PaneSessionMarkerEnv:   PaneSessionMarker(sess.FullID),
	}); err != nil {
		fmt.Printf("[warn] set DATAWATCH_SESSION_ID for %s: %v\n", sess.ID, err)
	}
}
