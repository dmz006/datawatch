//go:build !windows

package session

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// flockAcquire acquires an exclusive advisory lock on the given lock file path.
// It first tries a non-blocking LOCK_EX | LOCK_NB; if the file is already
// locked it falls back to a blocking acquire with a 5-second deadline. If the
// deadline expires it logs a warning and returns (nil, nil) so the caller can
// skip the persist rather than deadlock.
//
// The returned *os.File must be passed to flockRelease when the persist is done.
func flockAcquire(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	fd := int(f.Fd())

	// Try non-blocking first (fast path — no contention).
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return f, nil
	}
	if err != syscall.EWOULDBLOCK {
		_ = f.Close()
		return nil, fmt.Errorf("flock non-blocking: %w", err)
	}

	// Contention detected — fall back to blocking with a 5-second timeout.
	done := make(chan error, 1)
	go func() {
		done <- syscall.Flock(fd, syscall.LOCK_EX)
	}()

	select {
	case lockErr := <-done:
		if lockErr != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flock blocking: %w", lockErr)
		}
		return f, nil
	case <-time.After(5 * time.Second):
		// Timed out — return (nil, nil) so caller skips this persist cycle.
		// We do NOT close f here; the goroutine above still holds a reference.
		// The goroutine will eventually receive the lock and release it by
		// letting the function return, but we abandon the *os.File. The OS
		// will release the lock when the file descriptor is GC'd / closed.
		fmt.Printf("[warn] sessions.json flock timeout after 5s — skipping persist to avoid deadlock\n")
		return nil, nil //nolint:nilerr // intentional: caller treats (nil,nil) as skip-persist
	}
}

// flockRelease unlocks and closes the lock file descriptor.
func flockRelease(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
