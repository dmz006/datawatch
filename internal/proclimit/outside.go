// What a command under the limit is not told.
//
// A process leaves the limit when a service outside its tree runs something
// for it. tmux, for one, asks the user's service manager to put each new pane
// in a scope of its own. So a command is not given the variables through which
// tools find such services. This keeps ordinary tools inside the limit. It is
// not a barrier: a command that looks for a socket itself still finds it.

package proclimit

import "strings"

// outsideVars are not handed to a command under the limit.
var outsideVars = []string{
	// The user's service manager and message bus, which also start desktop
	// and service programs.
	"XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "DBUS_SYSTEM_BUS_ADDRESS",
	"DBUS_STARTER_ADDRESS", "DBUS_STARTER_BUS_TYPE",
	// What tells a program it belongs to a service, and lets it report to
	// the manager.
	"NOTIFY_SOCKET", "MANAGERPID", "SYSTEMD_EXEC_PID", "INVOCATION_ID", "JOURNAL_STREAM",
	// A display, from which the message bus can be found or started.
	"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY",
	// A tmux server already running, an SSH agent and container engines.
	"TMUX", "TMUX_PANE", "SSH_AUTH_SOCK", "DOCKER_HOST", "CONTAINER_HOST",
}

// Inside returns env without the variables a command under the limit is not
// given.
func Inside(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		kept := true
		for _, v := range outsideVars {
			kept = kept && name != v
		}
		if kept {
			out = append(out, kv)
		}
	}
	return out
}
