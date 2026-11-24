//go:build !linux

package ebpfkver

import "github.com/cilium/ebpf"

// PrepareKprobes is a no-op off Linux: there are no kprobes to prepare.
func PrepareKprobes(*ebpf.CollectionSpec) error { return nil }
