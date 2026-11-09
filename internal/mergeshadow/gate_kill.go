//go:build unix

package mergeshadow

import (
	"os/exec"
	"syscall"
)

// Gate process groups: a gate's make process starts its own process group, and
// every process the gate starts inherits that group. When the gate's timeout
// ends, the whole group is killed, so a hung test suite and everything it
// spawned end together and none survives the gate.

// configureGateProcessGroup makes cmd start in a new process group whose ID is
// the group leader's pid, and replaces cmd's cancel action so that a context
// end kills the whole group rather than only the leader. The leader's pipes
// are still bounded by cmd.WaitDelay.
func configureGateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid addresses the process group led by that pid.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
