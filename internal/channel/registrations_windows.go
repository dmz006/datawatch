//go:build windows

package channel

import "os/exec"

// endProcessGroupOnCancel does nothing on Windows: cancelling cmd ends the
// claude process itself.
func endProcessGroupOnCancel(*exec.Cmd) {}
