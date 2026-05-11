//go:build windows

package session

import "os"

// flockAcquire is a no-op on Windows — advisory file locking via syscall.Flock
// is not available. Windows callers still get the in-process mu protection;
// cross-process races on Windows are not guarded.
func flockAcquire(_ string) (*os.File, error) {
	return nil, nil //nolint:nilerr // intentional no-op on Windows
}

// flockRelease is a no-op on Windows.
func flockRelease(_ *os.File) {}
