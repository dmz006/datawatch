//go:build !linux

// Command testisolation-guard needs Linux (bubblewrap and inotify).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "test-isolation: the guard needs Linux; tests are never run unguarded")
	os.Exit(1)
}
