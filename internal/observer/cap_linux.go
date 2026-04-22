//go:build linux

// BL171 / v4.1.1 — Linux capability probe. Reads
// /proc/self/status and looks for CAP_BPF (bit 39) in the effective
// capability mask. Avoids a cgo dependency on libcap.

package observer

import (
	"os"
	"strconv"
	"strings"
)

const capBPFBit = 39

func probeBPFCapabilityPlatform() bool {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		hex := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		v, err := strconv.ParseUint(hex, 16, 64)
		if err != nil {
			return false
		}
		return v&(1<<capBPFBit) != 0
	}
	return false
}
