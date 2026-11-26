//go:build !linux

package proclimit

import "errors"

var errNoTreeLimit = errors.New("a hard memory limit over a command's whole process tree needs Linux with systemd, which this system is not")

// Host is the limit a shadow runs under on this system: there is none to be
// had here, so a shadow's commands are not run.
func Host() Starter { return None(errNoTreeLimit) }
