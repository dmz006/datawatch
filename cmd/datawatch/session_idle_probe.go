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
	roots, tree, ok := sessionProcessTree(tmuxSession)
	if !ok {
		return false
	}
	return processTreeHasLiveRunner(roots, tree, readProcessCommName)
}

// runnerDescendantPIDs returns the pids of the live processes a runner started
// under the panes of tmuxSession. The runner itself is never included: a worker
// parked at a startup dialog keeps its runner alive, and that alone is not
// work. It returns nil when the process tree cannot be read, which callers must
// treat as no observation rather than as an empty set.
func runnerDescendantPIDs(tmuxSession string) map[int]bool {
	roots, tree, ok := sessionProcessTree(tmuxSession)
	if !ok {
		return nil
	}
	return processTreeRunnerDescendants(roots, tree, readProcessCommName)
}

// sessionProcessTree walks down from the pane pids of tmuxSession and returns
// those roots with the child lists it discovered. ok is false when the tree
// cannot be read: no tmux, no pgrep, the session is gone, or it has no panes.
func sessionProcessTree(tmuxSession string) (roots []int, tree map[int][]int, ok bool) {
	if tmuxSession == "" {
		return nil, nil, false
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, nil, false
	}
	pgrepBin, err := exec.LookPath("pgrep")
	if err != nil {
		return nil, nil, false
	}
	out, err := exec.Command(tmuxBin, "list-panes", "-t", tmuxSession, "-F", "#{pane_pid}").Output()
	if err != nil {
		return nil, nil, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil && pid > 0 {
			roots = append(roots, pid)
		}
	}
	if len(roots) == 0 {
		return nil, nil, false
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
	return roots, buildSessionIdleProcessTree(roots, childrenOf), true
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

// processTreeRunnerDescendants is the pure walker behind runnerDescendantPIDs.
// It returns every pid that sits below a runner within the depth cap, and never
// a runner itself. A root whose own name is a runner counts as one, so the
// processes it started are included.
func processTreeRunnerDescendants(roots []int, children map[int][]int, nameOf func(int) string) map[int]bool {
	out := make(map[int]bool)
	visited := make(map[int]bool, len(roots))
	var walk func(pid, depth int, underRunner bool)
	walk = func(pid, depth int, underRunner bool) {
		if depth >= sessionIdleProbeMaxDepth {
			return
		}
		for _, c := range children[pid] {
			if visited[c] {
				continue
			}
			visited[c] = true
			if underRunner {
				out[c] = true
			}
			walk(c, depth+1, underRunner || sessionIdleRunnerNames[nameOf(c)])
		}
	}
	for _, r := range roots {
		if !visited[r] {
			visited[r] = true
			walk(r, 0, sessionIdleRunnerNames[nameOf(r)])
		}
	}
	return out
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
