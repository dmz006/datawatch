//go:build linux

// BL171 / v4.1.1 — Linux capability probe. Reads
// /proc/self/status and looks for CAP_BPF (bit 39) in the effective
// capability mask. Avoids a cgo dependency on libcap.

package observer

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const capBPFBit = 39

func probeBPFCapabilityPlatform() bool {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[observer] cap_linux: read /proc/self/status: %v\n", err)
		return false
	}

	// Log diagnostic info for issue #35 debugging
	exePath, _ := os.Executable()
	fmt.Fprintf(os.Stderr, "[observer] CAP_BPF check: running binary = %s\n", exePath)

	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		hex := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		fmt.Fprintf(os.Stderr, "[observer] CAP_BPF check: CapEff = 0x%s\n", hex)

		v, err := strconv.ParseUint(hex, 16, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[observer] CAP_BPF check: parse error: %v\n", err)
			return false
		}

		hasCAP := v&(1<<capBPFBit) != 0
		fmt.Fprintf(os.Stderr, "[observer] CAP_BPF check: bit %d set = %v\n", capBPFBit, hasCAP)
		return hasCAP
	}

	fmt.Fprintf(os.Stderr, "[observer] CAP_BPF check: CapEff line not found in /proc/self/status\n")
	return false
}
