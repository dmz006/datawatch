//go:build linux

// diskStatCall — the real statfs behind the cleanup status's small
// diskStatser interface (cleanup_status.go). Same pattern as
// internal/stats: platform-specific syscall.Statfs with a stable
// return type.
package autonomous

import "syscall"

func diskStatCall(path string) (diskStat, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return diskStat{}, err
	}
	return diskStat{Blocks: s.Blocks, Bavail: s.Bavail}, nil
}
