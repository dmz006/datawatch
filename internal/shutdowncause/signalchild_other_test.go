//go:build !unix

package shutdowncause

import (
	"errors"
	"syscall"
)

// signalProc is unavailable on non-Unix platforms; the test that would
// use it skips itself.
func signalProc(pid int, sig syscall.Signal) error {
	return errors.New("signalProc: not available on this platform")
}
