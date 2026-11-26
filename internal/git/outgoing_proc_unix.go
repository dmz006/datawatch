//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// configureCheckProcessGroup makes a cancelled project check take its child
// processes with it.
func configureCheckProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// end-reason-ok: kills the project check's own OS process group at its deadline, not a datawatch session
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
