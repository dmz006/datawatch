//go:build !linux

package stats

import "errors"

type syscallStatfs struct {
	Blocks uint64
	Bsize  uint64
	Bavail uint64
}

func statfs(path string, out *syscallStatfs) error {
	return errors.New("disk stats not available on this platform")
}
