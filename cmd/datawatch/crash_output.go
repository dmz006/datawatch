package main

import (
	"os"
	"runtime/debug"
)

// installCrashOutput configures the Go runtime (Go 1.23+) to write its fatal
// diagnostics — "fatal error:" messages, panic stacks, all-goroutine dumps —
// to f in addition to fd 2. Unlike the old dup2 approach, fd 2 is left
// untouched, so everything the runtime prints on a fatal error still reaches
// stderr and, with it, the service journal. Passing nil restores the default
// (stderr only).
func installCrashOutput(f *os.File) error {
	return debug.SetCrashOutput(f, debug.CrashOptions{})
}
