package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sessionIdleProbeMaxDepth caps how far below a tmux pane pid the probe walks.
// Runner processes sit a few levels under the pane shell; the cap stops a
// runaway tree from turning every idle tick into an unbounded process walk.
const sessionIdleProbeMaxDepth = 8

// sessionIdleRunnerNames are the exact process command names of the agent
// runners. pi's runner surfaces as a bare "pi" process.
var sessionIdleRunnerNames = map[string]bool{
	"claude":   true,
	"pi":       true,
	"opencode": true,
}

// hasLiveChildProcess reports whether a runner process (claude, pi or
// opencode) is still alive anywhere under the panes of tmuxSession. A session
// parked in waiting_input with a live runner is still working; one without is
// a candidate for the idle stall detector. Any failure to probe (no tmux, no
// pgrep, session gone) reports false, so the detector never kills a session
// on a probe error alone.
func hasLiveChildProcess(tmuxSession string) bool {
	if tmuxSession == "" {
		return false
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return false
	}
	pgrepBin, err := exec.LookPath("pgrep")
	if err != nil {
		return false
	}
	out, err := exec.Command(tmuxBin, "list-panes", "-t", tmuxSession, "-F", "#{pane_pid}").Output()
	if err != nil {
		return false
	}
	var roots []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil && pid > 0 {
			roots = append(roots, pid)
		}
	}
	if len(roots) == 0 {
		return false
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
	return processTreeHasLiveRunner(roots, tree, readProcessCommName)
}

// buildSessionIdleProcessTree walks down from roots using childrenOf and
// returns the child lists it discovered, keyed by parent pid. A visited set
// stops cycles and the depth cap bounds the walk.
func buildSessionIdleProcessTree(roots []int, childrenOf func(int) []int) map[int][]int {
	tree := make(map[int][]int)
	visited := make(map[int]bool, len(roots))
	type frame struct{ pid, depth int }
	var queue []frame
	for _, r := range roots {
		if !visited[r] {
			visited[r] = true
			queue = append(queue, frame{r, 0})
		}
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if f.depth >= sessionIdleProbeMaxDepth {
			continue
		}
		for _, c := range childrenOf(f.pid) {
			tree[f.pid] = append(tree[f.pid], c)
			if visited[c] {
				continue
			}
			visited[c] = true
			queue = append(queue, frame{c, f.depth + 1})
		}
	}
	return tree
}

// processTreeHasLiveRunner is the pure walker: it reports whether any
// descendant of roots (not the roots themselves) within the depth cap has a
// command name in sessionIdleRunnerNames. children maps a pid to its direct
// children; nameOf returns a pid's command name.
func processTreeHasLiveRunner(roots []int, children map[int][]int, nameOf func(int) string) bool {
	type frame struct{ pid, depth int }
	visited := make(map[int]bool, len(roots))
	var queue []frame
	for _, r := range roots {
		if !visited[r] {
			visited[r] = true
			queue = append(queue, frame{r, 0})
		}
	}
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
			if sessionIdleRunnerNames[nameOf(c)] {
				return true
			}
			queue = append(queue, frame{c, f.depth + 1})
		}
	}
	return false
}

// readProcessCommName returns the kernel command name of pid from
// /proc/<pid>/comm, or "" when it cannot be read (the process exited).
func readProcessCommName(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
