//go:build !windows

package plugins

import (
	"errors"
	"os/exec"
	"syscall"
)

// setServiceProcessGroup puts a managed service child in its own process
// group so Stop can signal it and any grandchildren together.
func setServiceProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalServiceProcessGroup sends SIGTERM (or SIGKILL when force) to the
// child's whole process group. A group that is already gone is not an error.
func signalServiceProcessGroup(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	// end-reason-ok: signals a plugin service's OS process group, not a datawatch session
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
