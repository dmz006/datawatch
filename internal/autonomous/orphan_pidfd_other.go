//go:build !linux

package autonomous

import "syscall"

// orphanPidfd is a placeholder: pidfd is Linux only, so other platforms fall
// back to the verified pid signal.
type orphanPidfd struct{}

func openOrphanPidfd(pid int) (*orphanPidfd, bool) { return nil, false }

func (p *orphanPidfd) signal(sig syscall.Signal) error { return nil }

func (p *orphanPidfd) close() {}
