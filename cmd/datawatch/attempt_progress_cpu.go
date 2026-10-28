package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// parseProcStatCPUTicks returns the cumulative CPU time (utime + stime, in
// clock ticks) recorded in a /proc/<pid>/stat line. It splits the line after
// the LAST ')' so a comm field that itself contains spaces or parentheses
// (e.g. "my (weird) proc") does not shift the field positions: utime and
// stime are fields 14 and 15 of the whole line (1-based), i.e. the 12th and
// 13rd whitespace-separated tokens after the closing parenthesis.
func parseProcStatCPUTicks(stat string) (uint64, bool) {
	closing := strings.LastIndex(stat, ")")
	if closing < 0 {
		return 0, false
	}
	fields := strings.Fields(stat[closing+1:])
	const need = 13 // state through stime: fields 3..15 of the whole line
	if len(fields) < need {
		return 0, false
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, false
	}
	return utime + stime, true
}

// childCPUTicksFromTree is the pure walker: it sums readTicks over all
// descendants of roots (the roots themselves are never counted, since the
// pane shell's own CPU time is not worker progress). A pid for which readTicks
// reports false (process vanished or stat unreadable) contributes 0 and is
// still traversed, in case it had spawned children before exiting. A visit
// set stops cycles; the same depth cap as the idle probe bounds the walk.
func childCPUTicksFromTree(roots []int, children map[int][]int, readTicks func(int) (uint64, bool)) uint64 {
	if len(roots) == 0 {
		return 0
	}
	type frame struct{ pid, depth int }
	visited := make(map[int]bool, len(roots))
	var queue []frame
	for _, r := range roots {
		if !visited[r] {
			visited[r] = true
			queue = append(queue, frame{r, 0})
		}
	}
	var total uint64
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if f.depth >= sessionIdleProbeMaxDepth {
			continue
		}
		for _, c := range children[f.pid] {
			if visited[c] {
				continue
			}
			visited[c] = true
			if ticks, ok := readTicks(c); ok {
				total += ticks
			}
			queue = append(queue, frame{c, f.depth + 1})
		}
	}
	return total
}

// readProcStatCPUTicks returns the cumulative utime+stime clock ticks of pid
// from /proc/<pid>/stat, or (0, false) when the line cannot be read or parsed
// (the process exited while the sampler was running).
func readProcStatCPUTicks(pid int) (uint64, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	return parseProcStatCPUTicks(string(b))
}

// childCPUTicks reports the cumulative CPU time (utime+stime clock ticks) of
// the live descendant processes of the panes of tmuxSession, so a worker that
// is silent but whose child process is busy still counts as progressing. It
// reuses the idle probe's tree walker; the pane shells themselves (the roots)
// are excluded. Any failure (no tmux, no pgrep, session gone) returns 0, so a
// progress sampler never reports phantom activity on a probe error.
func childCPUTicks(tmuxSession string) uint64 {
	if tmuxSession == "" {
		return 0
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return 0
	}
	pgrepBin, err := exec.LookPath("pgrep")
	if err != nil {
		return 0
	}
	out, err := exec.Command(tmuxBin, "list-panes", "-t", tmuxSession, "-F", "#{pane_pid}").Output()
	if err != nil {
		return 0
	}
	var roots []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil && pid > 0 {
			roots = append(roots, pid)
		}
	}
	if len(roots) == 0 {
		return 0
	}
	childrenOf := func(pid int) []int {
		kids, pgrepErr := exec.Command(pgrepBin, "-P", strconv.Itoa(pid)).Output()
		if pgrepErr != nil {
			// pgrep exits non-zero when there are no matches; treat as a leaf.
			return nil
		}
		var pids []int
		for _, line := range strings.Fields(string(kids)) {
			if p, convErr := strconv.Atoi(line); convErr == nil && p > 0 {
				pids = append(pids, p)
			}
		}
		return pids
	}
	tree := buildSessionIdleProcessTree(roots, childrenOf)
	return childCPUTicksFromTree(roots, tree, readProcStatCPUTicks)
}
