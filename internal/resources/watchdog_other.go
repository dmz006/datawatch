//go:build !linux

package resources

import (
	"errors"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {}
func daemonGroup() int              { return 0 }
func killGroup(pgid int) error {
	return errors.New("watchdog: group kill unsupported on this platform")
}
