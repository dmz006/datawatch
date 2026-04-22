//go:build linux

package observer

import (
	"os"
	"strings"
	"syscall"
)

// readDiskUsagePlatform reads /proc/mounts and stat'es each real
// filesystem. Deduplicates by device so a single disk shows up once.
func readDiskUsagePlatform() []Disk {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	seenDev := map[string]bool{}
	var out []Disk
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		dev, mount, fsType := f[0], f[1], f[2]
		// Skip pseudo filesystems.
		switch fsType {
		case "proc", "sysfs", "devpts", "tmpfs", "devtmpfs",
			"cgroup", "cgroup2", "pstore", "bpf", "tracefs",
			"securityfs", "debugfs", "autofs", "mqueue", "hugetlbfs",
			"fusectl", "configfs", "binfmt_misc", "rpc_pipefs",
			"nsfs", "squashfs", "overlay":
			continue
		}
		if !strings.HasPrefix(dev, "/") {
			continue
		}
		if seenDev[dev] {
			continue
		}
		seenDev[dev] = true
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - free
		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100.0
		}
		out = append(out, Disk{
			Mount:      mount,
			Pct:        round2(pct),
			UsedBytes:  used,
			TotalBytes: total,
			FSType:     fsType,
		})
		if len(out) > 16 {
			break
		}
	}
	return out
}
