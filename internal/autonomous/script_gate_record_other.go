//go:build !unix

package autonomous

// launchGateProcess is unavailable here; the caller keeps the direct-child path.
func launchGateProcess(argv []string, dir, outFile, exitFile string) (int, error) {
	return 0, errGateDurableUnsupported
}

// killGateGroup is a no-op: no durable gate run exists on this platform.
func killGateGroup(pid int) {}
