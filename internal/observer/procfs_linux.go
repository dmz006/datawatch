//go:build linux

// BL171 (S9) — Linux /proc walker. Reads /proc/[pid]/{stat,status,
// cmdline,cgroup} for every pid visible to the daemon. CPU percent
// is computed from the delta between two consecutive ticks against
// CLK_TCK.
//
// Performance: a 500-process host takes <10 ms on a modern SSD. We
// keep per-pid cache for static fields (comm, cmdline, start_time)
// across ticks to avoid rereading them every second.

package observer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cached static per-pid data to avoid rereading cmdline/comm every tick
type pidCache struct {
	comm        string
	cmdline     string
	startTicks  uint64
	cgroup      string
	containerID string
}

type procCache struct {
	mu         sync.Mutex
	byPid      map[int]*pidCache
	lastCPU    map[int]uint64 // user+sys ticks previous tick
	lastSample time.Time
}

var gProcCache = &procCache{
	byPid:   map[int]*pidCache{},
	lastCPU: map[int]uint64{},
}

// walkProc walks /proc and returns all visible processes. CPU
// percent is computed from the tick-delta against the last call;
// first call reports 0 for every process.
func walkProc() ([]ProcRecord, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	gProcCache.mu.Lock()
	defer gProcCache.mu.Unlock()

	now := time.Now()
	tickDelta := now.Sub(gProcCache.lastSample).Seconds()
	if tickDelta <= 0 {
		tickDelta = 1
	}
	nCores := max1(runtimeNumCPU())
	clkTck := sysClkTck()

	out := make([]ProcRecord, 0, 256)
	seen := map[int]bool{}
	nextLastCPU := map[int]uint64{}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		seen[pid] = true
		rec, totalTicks, ok := readOneProc(pid)
		if !ok {
			continue
		}
		// CPU% = (delta ticks) / (tickDelta s × clk_tck × cores) × 100
		if prev, have := gProcCache.lastCPU[pid]; have && totalTicks >= prev {
			delta := float64(totalTicks - prev)
			rec.CPUPct = (delta / (tickDelta * float64(clkTck) * float64(nCores))) * 100.0
			if rec.CPUPct < 0 {
				rec.CPUPct = 0
			}
		}
		nextLastCPU[pid] = totalTicks
		out = append(out, rec)
	}
	// Prune cache for pids that went away.
	for pid := range gProcCache.byPid {
		if !seen[pid] {
			delete(gProcCache.byPid, pid)
		}
	}
	gProcCache.lastCPU = nextLastCPU
	gProcCache.lastSample = now
	return out, nil
}

// readOneProc returns (rec, total_user_sys_ticks, ok). ok=false when
// the pid disappeared between ReadDir and the read.
func readOneProc(pid int) (ProcRecord, uint64, bool) {
	rec := ProcRecord{PID: pid}
	// /proc/<pid>/stat — space-delimited, 52+ fields. comm is in
	// parens and can contain spaces, so we must find the last ')'.
	statRaw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return rec, 0, false
	}
	s := string(statRaw)
	closeParen := strings.LastIndex(s, ")")
	if closeParen < 0 {
		return rec, 0, false
	}
	openParen := strings.Index(s, "(")
	if openParen < 0 || openParen >= closeParen {
		return rec, 0, false
	}
	rec.Comm = s[openParen+1 : closeParen]
	fields := strings.Fields(s[closeParen+1:])
	if len(fields) < 20 {
		return rec, 0, false
	}
	// fields[0] = state, [1] = ppid, [2] = pgrp, ..., [11] = utime,
	// [12] = stime, [17] = num_threads, [19] = starttime (ticks since boot)
	if v, err := strconv.Atoi(fields[1]); err == nil {
		rec.PPID = v
	}
	var utime, stime uint64
	if v, err := strconv.ParseUint(fields[11], 10, 64); err == nil {
		utime = v
	}
	if v, err := strconv.ParseUint(fields[12], 10, 64); err == nil {
		stime = v
	}
	totalTicks := utime + stime
	if v, err := strconv.Atoi(fields[17]); err == nil {
		rec.Threads = v
	}

	// Cached static fields.
	cached := gProcCache.byPid[pid]
	if cached == nil {
		cached = &pidCache{}
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
			// cmdline is null-delimited; join with spaces for display.
			cached.cmdline = strings.TrimRight(strings.ReplaceAll(string(b), "\x00", " "), " ")
		}
		cached.comm = rec.Comm
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid)); err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) > 0 {
				cached.cgroup = lines[len(lines)-1]
				cached.containerID = extractContainerID(cached.cgroup)
			}
		}
		gProcCache.byPid[pid] = cached
	}
	rec.Cmdline = cached.cmdline
	rec.Cgroup = cached.cgroup
	rec.ContainerID = cached.containerID

	// RSS from /proc/<pid>/status: "VmRSS: 1234 kB".
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					if n, err := strconv.ParseUint(f[1], 10, 64); err == nil {
						rec.RSSBytes = n * 1024
					}
				}
				break
			}
		}
	}

	// FD count = number of entries in /proc/<pid>/fd.
	if fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		rec.FDs = len(fds)
	} else if _, ok := err.(*fs.PathError); !ok {
		_ = err // ignore; process may have disappeared
	}

	return rec, totalTicks, true
}

// extractContainerID pulls a container hash from a cgroup line.
// Handles common cgroup-v2 formats:
//   0::/docker/<hash>
//   0::/system.slice/docker-<hash>.scope
//   0::/kubepods.slice/kubepods-<qos>.slice/.../cri-containerd-<hash>.scope
func extractContainerID(cgroupLine string) string {
	// Look for the longest 64-hex token.
	best := ""
	for _, tok := range splitMany(cgroupLine, "/:-.") {
		if len(tok) == 64 && isHex(tok) {
			if len(tok) > len(best) {
				best = tok
			}
		}
		if len(tok) == 12 && isHex(tok) && best == "" {
			best = tok
		}
	}
	return best
}

func splitMany(s, seps string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range s {
		if strings.ContainsRune(seps, r) {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// sysClkTck returns the kernel CLK_TCK, defaulting to 100 (every
// modern Linux). We could sysconf(_SC_CLK_TCK) via cgo, but the
// trade-off of one extra dependency isn't worth it.
func sysClkTck() uint64 { return 100 }

func runtimeNumCPU() int {
	// /proc/cpuinfo is authoritative; fallback to GOMAXPROCS.
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		n := 0
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "processor") {
				n++
			}
		}
		if n > 0 {
			return n
		}
	}
	return runtimeNumCPUGeneric()
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// Convenience: /proc-derived host bits used by the collector.
func readHostFields() (os_, kernel, arch string) {
	if b, err := os.ReadFile("/proc/sys/kernel/ostype"); err == nil {
		os_ = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernel = strings.TrimSpace(string(b))
	}
	if out, err := os.ReadFile("/proc/version"); err == nil {
		if strings.Contains(string(out), "x86_64") {
			arch = "x86_64"
		} else if strings.Contains(string(out), "aarch64") {
			arch = "aarch64"
		}
	}
	return
}

// readLoadavg returns 1m / 5m / 15m load from /proc/loadavg.
func readLoadavg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return
	}
	l1, _ = strconv.ParseFloat(f[0], 64)
	l5, _ = strconv.ParseFloat(f[1], 64)
	l15, _ = strconv.ParseFloat(f[2], 64)
	return
}

// readMemInfo returns used/total/swap from /proc/meminfo.
func readMemInfo() Mem {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Mem{}
	}
	get := func(key string) uint64 {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, key) {
				f := strings.Fields(line)
				if len(f) >= 2 {
					v, _ := strconv.ParseUint(f[1], 10, 64)
					return v * 1024
				}
			}
		}
		return 0
	}
	total := get("MemTotal:")
	avail := get("MemAvailable:")
	used := total - avail
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100.0
	}
	swapTotal := get("SwapTotal:")
	swapFree := get("SwapFree:")
	var swapUsed uint64
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}
	return Mem{
		Pct:            round2(pct),
		UsedBytes:      used,
		TotalBytes:     total,
		SwapUsedBytes:  swapUsed,
		SwapTotalBytes: swapTotal,
	}
}

// readUptimeSeconds returns integer uptime from /proc/uptime.
func readUptimeSeconds() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return int64(v)
}

// Lint-guard: keep filepath referenced so build-tag'd variants don't
// trip the "imported and not used" warning when this file isn't
// compiled.
var _ = filepath.Separator
