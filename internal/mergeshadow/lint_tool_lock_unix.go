//go:build unix

package mergeshadow

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock on f, blocking until it is held.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) // #nosec G115 -- fd fits int
}

func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // #nosec G115 -- fd fits int
}
