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
// vmlinux.h sourcing — per-arch headers committed under
// vmlinux_amd64/vmlinux.h and vmlinux_arm64/vmlinux.h. Each `-target`
// invocation passes its own `-cflags -I <dir>` so the bpf_tracing.h
// register-layout macros find the right `pt_regs` / `user_pt_regs`
// definitions for that arch.
//
// vmlinux_amd64/vmlinux.h is locally BTF-dumped via:
//   bpftool btf dump file /sys/kernel/btf/vmlinux format c
// vmlinux_arm64/vmlinux.h is sourced from libbpf/vmlinux.h
//   (https://github.com/libbpf/vmlinux.h) — community-maintained
//   per-arch BTF dumps. Refresh by replacing the file.
//
// On hosts without the regenerated object for that arch, the
// netprobe loader degrades to a noop and host.ebpf.message says so.
package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I./vmlinux_amd64" -target amd64 netprobe netprobe.bpf.c
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I./vmlinux_arm64" -target arm64 netprobe netprobe.bpf.c
