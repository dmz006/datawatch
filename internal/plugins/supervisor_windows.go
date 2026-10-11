//go:build windows

package plugins

import "os/exec"

// setServiceProcessGroup is a no-op on Windows: there are no POSIX
// process groups to signal.
func setServiceProcessGroup(cmd *exec.Cmd) {}

// signalServiceProcessGroup kills the child on Windows. There is no
// portable graceful signal, so the graceful step is a no-op and the stop
// timeout then falls through to the forced kill.
func signalServiceProcessGroup(cmd *exec.Cmd, force bool) error {
	if !force || cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
