//go:build linux

package ebpfkver

import (
	"fmt"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// Release returns the running kernel's release string (uname -r).
func Release() (string, error) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "", fmt.Errorf("uname: %w", err)
	}
	return unix.ByteSliceToString(u.Release[:]), nil
}

// PrepareKprobes sets the running kernel's version on the spec's kprobe
// programs (see [SetKprobeVersions]).
func PrepareKprobes(spec *ebpf.CollectionSpec) error {
	rel, err := Release()
	if err != nil {
		return err
	}
	return SetKprobeVersions(spec, rel)
}
