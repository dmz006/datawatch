//go:build windows

package session

import "errors"

// TryDaemonLock cannot tell whether a daemon is running here: there is no
// advisory file lock. Callers must treat the daemon as possibly running.
func TryDaemonLock(string) (release func(), ok bool, err error) {
	return nil, false, errors.New("file locks are not available on this system")
}
