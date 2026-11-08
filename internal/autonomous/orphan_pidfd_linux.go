//go:build linux

package autonomous

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// orphanPidfd is a handle on one exact process. Signalling through it can never
// reach a process that later reuses the pid (Linux 5.3 and later).
type orphanPidfd struct{ fd int }

// openOrphanPidfd pins the process currently holding pid. ok is false when the
// kernel has no pidfd support or the process is already gone.
func openOrphanPidfd(pid int) (*orphanPidfd, bool) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, false
	}
	return &orphanPidfd{fd: fd}, true
}

func (p *orphanPidfd) signal(sig syscall.Signal) error {
	return unix.PidfdSendSignal(p.fd, sig, nil, 0)
}

func (p *orphanPidfd) close() { _ = unix.Close(p.fd) }
