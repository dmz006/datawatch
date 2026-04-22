// BL171 (S9) — platform-agnostic helpers shared by the Linux and
// non-Linux builds. These used to live in procfs_linux.go and broke
// the darwin / windows cross-build when the collector started
// calling them outside the build-tag guard.

package observer

import "runtime"

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

// probeBPFCapability checks whether the running binary has CAP_BPF
// granted (Linux only). Reused by the observer to populate
// host.ebpf.capability so the operator gets accurate feedback after
// running `datawatch setup ebpf`.
func probeBPFCapability() bool {
	return probeBPFCapabilityPlatform()
}
