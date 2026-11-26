//go:build !windows

package channel

import (
	"os/exec"
	"syscall"
)

// endProcessGroupOnCancel makes cancelling cmd also end what it started:
// `claude mcp list` starts the stdio MCP servers it checks.
func endProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
