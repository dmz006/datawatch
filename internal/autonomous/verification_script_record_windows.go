//go:build windows

package autonomous

// runVerifyRecorded is unavailable on Windows; the caller keeps the
// direct-child path.
func runVerifyRecorded(argv []string, dir, scriptPath string, l *verifyLaunch) ([]byte, int, error, bool) {
	return nil, 0, nil, false
}

// killVerifyGroup is a no-op on Windows: the durable record is not written
// there, so no run is ever waited on from a previous daemon.
func killVerifyGroup(pid int) {}
