//go:build windows

package autonomous

// runVerifyRecorded is unavailable on Windows; the caller keeps the
// direct-child path.
func runVerifyRecorded(argv []string, dir, scriptPath string, l *verifyLaunch) ([]byte, int, error, bool) {
	return nil, 0, nil, false
}
