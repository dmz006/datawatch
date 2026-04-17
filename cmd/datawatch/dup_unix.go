//go:build linux || darwin || freebsd || netbsd || openbsd

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// dupStderrTo replaces fd 2 with the file's fd so the Go runtime's
// panic-stack writes (fatal error, throw, etc.) land in the crash log.
func dupStderrTo(f *os.File) {
	_ = unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
