//go:build linux

package resources

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func daemonGroup() int {
	g, err := syscall.Getpgid(0)
	if err != nil {
		return 0
	}
	return g
}

// killGroup sends SIGKILL to the whole group only (negative pid).
// end-reason-ok: signals a watched worker process group, not a datawatch session
func killGroup(pgid int) error { return syscall.Kill(-pgid, syscall.SIGKILL) }
