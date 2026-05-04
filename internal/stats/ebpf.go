//go:build linux

package stats

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cilium/ebpf/rlimit"
)

// HasCapBPF checks if the given binary has CAP_BPF capability set.
func HasCapBPF(binaryPath string) bool {
	out, err := exec.Command("getcap", binaryPath).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_bpf")
}

// SetCapBPF sets CAP_BPF, CAP_PERFMON, and CAP_SYS_RESOURCE on the binary using sudo.
// CAP_SYS_RESOURCE is required so rlimit.RemoveMemlock() can raise RLIMIT_MEMLOCK to
// unlimited at daemon start — without it the eBPF collector fails on systems where the
// hard memlock limit is not already infinite (BL253 / GH#37).
func SetCapBPF(binaryPath string) error {
	if _, err := os.Stat(binaryPath); err != nil {
		return fmt.Errorf("binary not found: %s", binaryPath)
	}

	cmd := exec.Command("sudo", "-S", "setcap", "cap_bpf,cap_perfmon,cap_sys_resource+ep", binaryPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setcap failed: %w (is sudo available?)", err)
	}

	if !HasCapBPF(binaryPath) {
		return fmt.Errorf("setcap succeeded but CAP_BPF not detected on %s", binaryPath)
	}
	return nil
}

// CheckEBPFReady verifies the system supports eBPF and the binary has capabilities.
func CheckEBPFReady(binaryPath string) error {
	// Bug 1 fix (BL253): parse kernel version from /proc/version and enforce ≥ 5.8.
	// Previously the version string was read but immediately discarded with "_ = version".
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return fmt.Errorf("cannot read kernel version: %w", err)
	}
	if err := checkKernelVersion(string(data), 5, 8); err != nil {
		return err
	}

	// Check for BTF support (CO-RE requires vmlinux BTF).
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err != nil {
		return fmt.Errorf("BTF not available (needed for CO-RE eBPF). Kernel may be too old or missing CONFIG_DEBUG_INFO_BTF")
	}

	// Bug 3 fix (BL253): check unprivileged_bpf_disabled sysctl.
	// Value 1 = disabled, 2 = locked disabled until reboot. CAP_BPF overrides the
	// user-namespace restriction but interacts with rlimit probes in cilium/ebpf.
	if raw, err := os.ReadFile("/proc/sys/kernel/unprivileged_bpf_disabled"); err == nil {
		val := strings.TrimSpace(string(raw))
		if val == "1" || val == "2" {
			fmt.Printf("[warn] unprivileged_bpf_disabled=%s — CAP_BPF overrides this for privileged processes, but runtime rlimit probes may still fail on some kernel builds\n", val)
		}
	}

	// Check capabilities.
	if !HasCapBPF(binaryPath) {
		return fmt.Errorf("binary %s missing CAP_BPF. Run: datawatch setup ebpf", binaryPath)
	}

	// Bug 2 fix (BL253): probe rlimit.RemoveMemlock() before declaring success.
	// Without CAP_SYS_RESOURCE the daemon cannot raise RLIMIT_MEMLOCK to unlimited,
	// causing NewEBPFCollector() to fail at startup even when caps look correct.
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("cannot raise RLIMIT_MEMLOCK: %w\n  Hint: run 'datawatch setup ebpf' again to add cap_sys_resource, or configure system memlock limits", err)
	}

	return nil
}

// checkKernelVersion parses a /proc/version string and returns an error if the
// kernel is older than the required major.minor.
func checkKernelVersion(procVersion string, requiredMajor, requiredMinor int) error {
	// /proc/version format: "Linux version X.Y.Z-... (gcc ...) #..."
	fields := strings.Fields(procVersion)
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			parts := strings.SplitN(fields[i+1], ".", 3)
			if len(parts) < 2 {
				break
			}
			major, err1 := strconv.Atoi(parts[0])
			minor, err2 := strconv.Atoi(strings.Split(parts[1], "-")[0])
			if err1 != nil || err2 != nil {
				break
			}
			if major < requiredMajor || (major == requiredMajor && minor < requiredMinor) {
				return fmt.Errorf("kernel %d.%d is too old for eBPF ring buffer: need %d.%d+", major, minor, requiredMajor, requiredMinor)
			}
			return nil
		}
	}
	// If we can't parse the version, warn but don't block.
	fmt.Printf("[warn] could not parse kernel version from /proc/version — skipping version check\n")
	return nil
}
