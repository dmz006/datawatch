# How-to: Build the eBPF objects

The eBPF netprobe objects in `internal/observer/ebpf/` are generated, not hand-written.

## Pinned toolchain

The toolchain versions are pinned, and the Makefile target `ebpf-gen` and the
`ebpf-gen-drift` CI workflow use the same pinned versions, so a local regeneration
matches what CI checks:

- **bpf2go:** pinned to `github.com/cilium/ebpf` v0.22.0 in `go.mod`.
- **clang:** pinned to `clang-18` (`EBPF_CLANG` in the Makefile), with `llvm-strip-18` named in the `//go:generate` directives.

## Regenerate

Install clang-18 and llvm-18 plus the kernel headers for your running kernel
(`linux-headers-$(uname -r)` on Debian/Ubuntu), then run:

```bash
make ebpf-gen
```

`ebpf-gen` refuses to run if the local clang is not version 18. Without the generated
objects the netprobe loader degrades to noop with a clear message, so the observer still runs.
If you change either pin, change the Makefile, `.github/workflows/ebpf-gen-drift.yaml` and this note together.
