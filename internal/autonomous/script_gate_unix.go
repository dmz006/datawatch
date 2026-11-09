//go:build unix

package autonomous

import (
	"os/exec"
	"syscall"
)

// configureGateProcessGroup starts the gate in its own process group and
// makes a context cancel kill the whole group, so a gate's children die with
// it instead of outliving the timeout.
func configureGateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// end-reason-ok: kills a gate check's own OS process group at its deadline, not a datawatch session
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
