// Worker tmux kill guard (autonomous.tmux_kill_guard, default on).
//
// The pane env scrub (scrubTmuxEnvCommand) stops a worker from reaching the
// production tmux server by accident, but a worker that runs a bare server
// kill still reaches the default socket. This guard puts a "tmux" shim first
// on the PATH of the worker's pane shell. The pane shell is the one place
// every backend (claude, pi, opencode, ...) launches from, so one shim covers
// all of them, on first launch and on restart.
package session

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tmuxGuardShimSource is the shim script, written to <dataDir>/tmux-guard/tmux.
//
//go:embed tmux_guard_shim.sh
var tmuxGuardShimSource string

// tmuxGuardDirName is the private directory under the data dir holding the shim.
const tmuxGuardDirName = "tmux-guard"

// ensureTmuxGuardShim writes (or refreshes) the shim under dataDir and returns
// the directory that must go first on PATH.
func ensureTmuxGuardShim(dataDir string) (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("no data directory")
	}
	dir := filepath.Join(dataDir, tmuxGuardDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "tmux")
	if cur, err := os.ReadFile(path); err == nil && string(cur) == tmuxGuardShimSource {
		if fi, serr := os.Stat(path); serr == nil && fi.Mode().Perm()&0o111 != 0 {
			return dir, nil
		}
	}
	// Write beside, then rename, so a pane that runs it never sees a partial file.
	tmp, err := os.CreateTemp(dir, ".tmux-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(tmuxGuardShimSource); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return dir, nil
}

// shellSingleQuote quotes s for a POSIX shell.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tmuxGuardEnabled reports whether the guard applies to this manager. It needs
// a data directory to hold the shim; the setting defaults to on.
func (m *Manager) tmuxGuardEnabled() bool {
	if m == nil || m.dataDir == "" {
		return false
	}
	if m.cfg == nil {
		return true
	}
	return m.cfg.Autonomous.TmuxKillGuardEnabled()
}

// installPaneTmuxGuard types the PATH change that puts the shim first into the
// worker's pane shell. Failure is logged, not fatal: the launch proceeds with
// the env scrub alone.
func (m *Manager) installPaneTmuxGuard(sess *Session) {
	if !m.tmuxGuardEnabled() {
		return
	}
	dir, err := ensureTmuxGuardShim(m.dataDir)
	if err != nil {
		fmt.Printf("[warn] tmux kill guard for %s: %v\n", sess.ID, err)
		return
	}
	cmd := "export PATH=" + shellSingleQuote(dir) + `:"$PATH"`
	if prod := TmuxSocketDirFor(m.dataDir); prod != "" {
		cmd += " DATAWATCH_TMUX_GUARD_PROTECTED=" + shellSingleQuote(prod)
	}
	if err := m.tmux.SendKeys(sess.TmuxSession, cmd); err != nil {
		fmt.Printf("[warn] tmux kill guard for %s: %v\n", sess.ID, err)
	}
}
