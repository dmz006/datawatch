// BL171 (S9) — /proc walker. Linux path lives in procfs_linux.go;
// non-linux gets a trimmed stub that returns an empty list so the
// rest of the observer surface (host / cpu / mem / disk / gpu /
// sessions / backends) still renders on macOS / Windows.

package observer

// ProcRecord is the shape returned by the per-platform walker. All
// fields except PID are best-effort — missing data = zero-value.
type ProcRecord struct {
	PID         int
	PPID        int
	Comm        string
	Cmdline     string
	CPUPct      float64 // 0-100 per-core sum (e.g. 200 on 2 pinned cores)
	RSSBytes    uint64
	Threads     int
	FDs         int
	StartUnixMs int64
	Cgroup      string // raw content of /proc/<pid>/cgroup last line
	ContainerID string // extracted from cgroup path when present
}

// walkProc returns one ProcRecord per process visible in /proc.
// Platform-specific implementation; see procfs_linux.go and
// procfs_other.go.
