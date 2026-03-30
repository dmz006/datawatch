//go:build linux

package stats

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HasCapBPF checks if the given binary has CAP_BPF capability set.
func HasCapBPF(binaryPath string) bool {
	out, err := exec.Command("getcap", binaryPath).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_bpf")
}

// SetCapBPF sets CAP_BPF and CAP_PERFMON on the binary using sudo.
// Prompts for password via sudo -S (reads from stdin).
func SetCapBPF(binaryPath string) error {
	// Verify binary exists
	if _, err := os.Stat(binaryPath); err != nil {
		return fmt.Errorf("binary not found: %s", binaryPath)
	}

	cmd := exec.Command("sudo", "-S", "setcap", "cap_bpf,cap_perfmon+ep", binaryPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setcap failed: %w (is sudo available?)", err)
	}

	// Verify
	if !HasCapBPF(binaryPath) {
		return fmt.Errorf("setcap succeeded but CAP_BPF not detected on %s", binaryPath)
	}
	return nil
}

// CheckEBPFReady verifies the system supports eBPF and the binary has capabilities.
func CheckEBPFReady(binaryPath string) error {
	// Check kernel version — need 5.8+ for ring buffer
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return fmt.Errorf("cannot read kernel version: %w", err)
	}
	version := string(data)

	// Check for BTF support
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err != nil {
		return fmt.Errorf("BTF not available (needed for CO-RE eBPF). Kernel may be too old or missing CONFIG_DEBUG_INFO_BTF")
	}

	// Check capabilities
	if !HasCapBPF(binaryPath) {
		return fmt.Errorf("binary %s missing CAP_BPF. Run: datawatch setup ebpf", binaryPath)
	}

	_ = version // checked but not parsed yet
	return nil
}
