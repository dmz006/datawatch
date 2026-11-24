//go:build !linux

package main

// peakRSSMB is not measured off Linux.
func peakRSSMB() int64 { return 0 }
