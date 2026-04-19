//go:build linux || darwin || freebsd || netbsd || openbsd

package server

import (
	"fmt"
	"syscall"
)

func checkDiskSpace(name, path string, minFreeBytes uint64) DiagnoseCheck {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiagnoseCheck{Name: name, OK: false, Detail: err.Error()}
	}
	free := stat.Bavail * uint64(stat.Bsize)
	ok := free >= minFreeBytes
	return DiagnoseCheck{
		Name:   name,
		OK:     ok,
		Detail: fmt.Sprintf("%d MB free on %s", free/1024/1024, path),
	}
}
