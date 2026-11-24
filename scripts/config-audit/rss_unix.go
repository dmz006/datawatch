//go:build linux

package main

import "syscall"

// peakRSSMB is the peak resident memory of this process in MB (Linux reports KB).
func peakRSSMB() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return int64(ru.Maxrss) / 1024
}
