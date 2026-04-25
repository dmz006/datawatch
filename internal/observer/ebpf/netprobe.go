// Package ebpf — `make ebpf-gen` codegen entry point.
//
// Operators / CI run `make ebpf-gen` to invoke bpf2go on
// netprobe.bpf.c. The generated files (netprobe_bpfel.go,
// netprobe_bpfeb.go, netprobe_bpfel.o, netprobe_bpfeb.o) become part
// of the build and the linuxKprobeProbe constructor in
// probe_linux.go is rewritten by a follow-up patch to use them.
//
// Without bpf2go output, NewNetProbe returns a noop probe and the
// daemon's host.ebpf.message field explains why.
//
// bpf2go is part of github.com/cilium/ebpf and requires `clang` +
// kernel headers (linux-headers-$(uname -r) on Debian/Ubuntu).
package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 netprobe netprobe.bpf.c
