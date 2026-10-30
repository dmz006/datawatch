//go:build !linux

// diskStatCall — build-tag companion to disk_usage_linux.go for
// platforms without the Linux statfs syscall: the cleanup status
// then simply has no reading for either disk, instead of failing a
// poll.
package autonomous

import "errors"

func diskStatCall(path string) (diskStat, error) {
	return diskStat{}, errors.New("Automaton cleanup: disk usage is not available on this platform")
}
