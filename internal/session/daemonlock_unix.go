//go:build !windows

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// TryDaemonLock takes the daemon lock in dir without waiting. ok is false when
// it is held: a datawatch daemon is running on this data folder. A caller that
// gets it must call release.
func TryDaemonLock(dir string) (release func(), ok bool, err error) {
	f, err := os.OpenFile(filepath.Join(dir, daemonLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock daemon lock: %w", err)
	}
	return func() { flockRelease(f) }, true, nil
}
