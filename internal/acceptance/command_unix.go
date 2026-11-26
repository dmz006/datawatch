//go:build !windows

package acceptance

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts a check in its own process group so a timeout ends
// everything it started.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// end-reason-ok: ends a check's own process group, not a session
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		// end-reason-ok: ends what a check left running, not a session
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
