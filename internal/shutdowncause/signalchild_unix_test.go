//go:build unix

package shutdowncause

import "syscall"

// signalProc delivers sig to pid (Unix kill(2)).
func signalProc(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
