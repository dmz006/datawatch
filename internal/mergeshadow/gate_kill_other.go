//go:build !unix

package mergeshadow

import "os/exec"

// configureGateProcessGroup is a no-op where process groups are not available.
// On those platforms a timed-out gate kills only its make process, as
// exec.CommandContext does.
func configureGateProcessGroup(cmd *exec.Cmd) {}
