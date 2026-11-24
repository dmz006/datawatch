package ebpfkver

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
)

// ParseRelease turns a kernel release string such as "7.0.0-43-generic" or
// "6.8.12" into the LINUX_VERSION_CODE value, KERNEL_VERSION(major, minor,
// patch) = major<<16 | minor<<8 | min(patch, 255).
func ParseRelease(release string) (uint32, error) {
	rest := strings.TrimSpace(release)
	var nums [3]uint64
	for i := 0; i < 3; i++ {
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			if i < 2 {
				return 0, fmt.Errorf("kernel release %q: no number at position %d", release, i+1)
			}
			break // patch level may be absent ("6.8-rc1")
		}
		n, err := strconv.ParseUint(rest[:end], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("kernel release %q: %w", release, err)
		}
		nums[i] = n
		rest = rest[end:]
		if i < 2 {
			if !strings.HasPrefix(rest, ".") {
				if i == 0 {
					return 0, fmt.Errorf("kernel release %q: no minor number", release)
				}
				break
			}
			rest = rest[1:]
		}
	}
	if nums[0] > 255 || nums[1] > 255 {
		return 0, fmt.Errorf("kernel release %q: version out of range", release)
	}
	patch := nums[2]
	if patch > 255 {
		patch = 255
	}
	return uint32(nums[0])<<16 | uint32(nums[1])<<8 | uint32(patch), nil
}

// SetKprobeVersions sets the kernel version on every Kprobe-type program in
// the spec that has none, so the library does not detect it through
// /proc/self/mem. Kernels from 5.0 ignore the value, but the library
// requires a non-zero one to skip detection. A release that cannot be
// parsed returns an error and leaves the specs untouched.
func SetKprobeVersions(spec *ebpf.CollectionSpec, release string) error {
	code, err := ParseRelease(release)
	if err != nil {
		return err
	}
	for _, ps := range spec.Programs {
		if ps.Type == ebpf.Kprobe && (ps.KernelVersion == 0 || ps.KernelVersion == 0xFFFFFFFE) {
			ps.KernelVersion = code
		}
	}
	return nil
}
