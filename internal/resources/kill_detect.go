package resources

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UnknownProcess is the process name used when the largest process is not known.
const UnknownProcess = "unknown"

// KillReason is the failure reason recorded when a run was stopped for memory.
const KillReason = "the process used more memory than the limit"

// KillInfo is the memory-kill verdict for one finished run.
type KillInfo struct {
	Killed      bool
	PeakMB      int
	ProcessName string
}

// CgroupReader reads a file from a scope's cgroup directory. Tests inject a fake.
type CgroupReader interface {
	ReadFile(path string) ([]byte, error)
}

// FSCgroupReader reads real files.
type FSCgroupReader struct{}

// ReadFile implements CgroupReader.
func (FSCgroupReader) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// DetectScopeKill reads memory.events (oom_kill) and memory.peak from the
// scope's cgroup directory. A missing counter reads as not killed / no peak.
// A nil reader uses the real filesystem. The process name is unknown on the
// scope path because the cgroup does not record it.
func DetectScopeKill(r CgroupReader, cgroupDir string) KillInfo {
	if r == nil {
		r = FSCgroupReader{}
	}
	info := KillInfo{ProcessName: UnknownProcess}
	if b, err := r.ReadFile(filepath.Join(cgroupDir, "memory.events")); err == nil {
		info.Killed = oomKillCount(string(b)) > 0
	}
	if b, err := r.ReadFile(filepath.Join(cgroupDir, "memory.peak")); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && v > 0 {
			info.PeakMB = int(v / (1024 * 1024))
		}
	}
	return info
}

func oomKillCount(events string) int64 {
	for _, line := range strings.Split(events, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "oom_kill" {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			return n
		}
	}
	return 0
}

// DetectWatchdogKill derives the verdict from a watchdog Result: the kill
// decision and the recorded peak.
func DetectWatchdogKill(r Result) KillInfo {
	name := r.LargestName
	if name == "" {
		name = UnknownProcess
	}
	return KillInfo{Killed: r.Killed, PeakMB: int(r.PeakKB / 1024), ProcessName: name}
}
