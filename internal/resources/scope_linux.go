//go:build linux

package resources

import "os/exec"

// probeScope checks that systemd-run is on PATH and that the user
// systemd manager is reachable. It does not run systemd-run.
func probeScope() bool {
	if _, err := exec.LookPath(ScopeBinary); err != nil {
		return false
	}
	ctl, err := exec.LookPath("systemctl")
	if err != nil {
		return false
	}
	return exec.Command(ctl, "--user", "show-environment").Run() == nil
}
