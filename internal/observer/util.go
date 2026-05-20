// BL171 (S9) — platform-agnostic helpers shared by the Linux and
// non-Linux builds. These used to live in procfs_linux.go and broke
// the darwin / windows cross-build when the collector started
// calling them outside the build-tag guard.

package observer

import (
	"runtime"
	"sync"
)

// runtimeNumCPU returns the host core count. Linux reads
// /proc/cpuinfo (see procfs_linux.go override); everything else
// falls back to runtime.NumCPU().
func runtimeNumCPUGeneric() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}

// round2 truncates a float to two decimal places. Used for every
// percent value we hand out so JSON payloads stay tight.
func round2(f float64) float64 {
	return float64(int(f*100)) / 100.0
}

// bpfCapOnce caches the CAP_BPF probe result. The capability status
// cannot change at runtime, so running the check (and its verbose log
// lines) once at first call is sufficient. Fixes #84 — was logging
// 3 lines per observer tick (~18KB/min) into daemon-crash.log.
var (
	bpfCapOnce   sync.Once
	bpfCapResult bool
)

// probeBPFCapability checks whether the running binary has CAP_BPF
// granted (Linux only). Result is cached after the first call.
func probeBPFCapability() bool {
	bpfCapOnce.Do(func() {
		bpfCapResult = probeBPFCapabilityPlatform()
	})
	return bpfCapResult
}

// ProbeBPFCapability is the public form of the package-private probe,
// callable from cmd/datawatch-stats's --setup-ebpf diagnostic.
// v6.22.6 — operator-facing eBPF setup helper.
func ProbeBPFCapability() bool {
	return probeBPFCapability()
}
