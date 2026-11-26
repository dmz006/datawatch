//go:build unix

package autonomous

import (
	"errors"
	"syscall"
)

// processExists asks whether pid names a process. Only "no such process"
// counts as gone.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0) // end-reason-ok: signal 0 only asks whether the process exists
	return !errors.Is(err, syscall.ESRCH)
}
