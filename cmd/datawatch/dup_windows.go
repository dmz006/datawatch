//go:build windows

package main

import "os"

// dupStderrTo is a no-op on Windows; Go's runtime panic output cannot be
// redirected via fd duplication on this platform.
func dupStderrTo(_ *os.File) {}
