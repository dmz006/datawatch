//go:build linux

package stats

import "syscall"

type syscallStatfs struct {
	Blocks uint64
	Bsize  uint64
	Bavail uint64
}

func statfs(path string, out *syscallStatfs) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	out.Blocks = stat.Blocks
	out.Bsize = uint64(stat.Bsize)
	out.Bavail = stat.Bavail
	return nil
}
