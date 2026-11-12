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
		// end-reason-ok: kills a gate check's own OS process at its deadline, not a datawatch session
		return cmd.Process.Kill()
	}
}
