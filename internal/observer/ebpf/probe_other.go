//go:build !linux

package ebpf

// NewNetProbe on non-Linux always returns a noop probe — eBPF kprobes
// are a Linux-only feature.
func NewNetProbe() (NetProbe, error) {
	return NewNoopProbe("eBPF per-process net is Linux-only"), nil
}
