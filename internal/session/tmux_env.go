package session

import (
	"os"
	"os/exec"
)

// TmuxCommand builds an exec.Cmd for tmux that inherits the parent
// environment except for the TMUX variable. TMUX marks a process as
// already running inside a tmux server; tmux interprets its presence on
// the server side as a nested server and can refuse to serve client
// commands. The daemon's dedicated socket is selected via TMUX_TMPDIR,
// which is a separate variable and must be kept intact, as must PATH so
// the tmux binary remains resolvable.
func TmuxCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", args...)
	cmd.Env = os.Environ()
	filtered := cmd.Env[:0]
	for _, kv := range cmd.Env {
		if len(kv) > 4 && kv[:4] == "TMUX" && kv[4] == '=' {
			// TMUX=... : strip; TMUX_TMPDIR=... and other TMUX_* vars are kept.
			continue
		}
		filtered = append(filtered, kv)
	}
	cmd.Env = filtered
	return cmd
}
