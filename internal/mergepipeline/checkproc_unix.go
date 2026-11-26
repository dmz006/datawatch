//go:build unix

package mergepipeline

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup runs cmd in a process group of its own, so a timeout stops
// everything it started. The returned function stops whatever the command
// left running.
func ownProcessGroup(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // end-reason-ok: stops a check command's own process group, not a session
	}
	cmd.Cancel = kill
	return func() { _ = kill() }
}
