//go:build !unix

package autonomous

import "os/exec"

// configureGateProcessGroup on platforms without process groups kills only
// the direct child when the context is cancelled.
func configureGateProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}
