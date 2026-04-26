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
//
// vmlinux.h sourcing — vmlinux.h is committed alongside netprobe.bpf.c.
// It is BTF-dumped from the build host's kernel (typically amd64) via:
//   bpftool btf dump file /sys/kernel/btf/vmlinux format c \
//     > internal/observer/ebpf/vmlinux.h
// The Debian/Ubuntu linux-bpf-dev package's stub vmlinux.h is
// architecture-incomplete (missing user_pt_regs etc.) so clang fails
// against it; the BTF-dumped version is the canonical source.
//
// Multi-arch note (BL177): the committed netprobe_bpfel.{go,o} is
// amd64-only — the same vmlinux.h cannot satisfy bpf_tracing.h's
// arm64 register layout (regs[0]/regs[1]/…). To produce the arm64
// object, regenerate vmlinux.h on an arm64 host and run
// `make ebpf-gen-arm64` (cilium-style separate per-arch vmlinux
// headers). On non-amd64 hosts without the regenerated object, the
// netprobe loader degrades to a noop and host.ebpf.message says so.
package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I." -target amd64 netprobe netprobe.bpf.c
