package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/channel"
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

// channelBinaryName is the native channel bridge's binary name. A process whose
// argv[0] has this base name is the session's own channel server, whichever spec
// it was launched from.
const channelBinaryName = "datawatch-channel"

// hasLiveChildProcess reports whether a runner (claude, pi or opencode) under the
// panes of tmuxSession is doing work: it has a child process that is not one of
// the session's own MCP or channel servers. An idle worker's runner and its own
// servers stay alive at an empty prompt, so a live runner alone is not work.
// projectDir is the session's project directory, where its MCP specs are written.
// Any failure to probe (no tmux, no pgrep, session gone) reports false, so the
// detector never kills a session on a probe error alone.
func hasLiveChildProcess(tmuxSession, projectDir string) bool {
	roots, tree, ok := sessionProcessTree(tmuxSession)
	if !ok {
		return false
	}
	return len(workChildPIDs(roots, tree, readProcessCommName, readProcessCmdline, sessionOwnMCPServers(projectDir))) > 0
}

// hasLiveRunnerProcess reports whether a runner process (claude, pi or opencode)
// is alive anywhere under the panes of tmuxSession, whatever it is doing. The
// pi-prompt exit-silence detector needs this rather than hasLiveChildProcess: a
// one-shot run is stalled only once its runner has gone, and a runner that is
// generating may have no child process at all. Any probe failure reports false.
func hasLiveRunnerProcess(tmuxSession string) bool {
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

// workChildPIDs is the pure walker behind hasLiveChildProcess. It returns the
// descendants of a runner under roots that are work: anything below a runner
// except the session's own MCP and channel servers and their subtrees. A process
// is an own server when its argv matches one of ownServers (see isOwnMCPServer)
// or it is the channel binary. children maps a pid to its direct children;
// nameOf returns a pid's command name; cmdlineOf returns a pid's argv.
func workChildPIDs(roots []int, children map[int][]int, nameOf func(int) string, cmdlineOf func(int) []string, ownServers []channel.MCPServerSpec) map[int]bool {
	isOwn := func(pid int) bool {
		return isOwnMCPServer(cmdlineOf(pid), ownServers)
	}
	return runnerSubtreePIDs(roots, children, nameOf, isOwn)
}

// processTreeRunnerDescendants is the pure walker behind runnerDescendantPIDs.
// It returns every pid that sits below a runner within the depth cap, and never
// a runner itself. A root whose own name is a runner counts as one, so the
// processes it started are included.
func processTreeRunnerDescendants(roots []int, children map[int][]int, nameOf func(int) string) map[int]bool {
	return runnerSubtreePIDs(roots, children, nameOf, nil)
}

// runnerSubtreePIDs is the walk shared by processTreeRunnerDescendants and
// workChildPIDs. It returns every pid below a runner within the depth cap, and
// never a runner itself. A child for which skip reports true is left out along
// with its whole subtree; a nil skip leaves nothing out.
func runnerSubtreePIDs(roots []int, children map[int][]int, nameOf func(int) string, skip func(int) bool) map[int]bool {
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
			if underRunner && skip != nil && skip(c) {
				continue
			}
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

// isOwnMCPServer reports whether argv, a process's command line, is one of the
// session's own servers. Each spec matches when its Command is argv[0], or shares
// its base name, or npm's launcher script for it runs (see npmCLIRunsProgram),
// and every one of its Args appears later in argv. A spec with no args matches
// only a process started with no arguments at all, so a launcher such as npx
// cannot claim the unrelated work it starts. The channel bridge's binary name is
// the fallback when no spec matches. An empty argv is never an own server, so a
// process that cannot be read counts as work.
func isOwnMCPServer(argv []string, ownServers []channel.MCPServerSpec) bool {
	if len(argv) == 0 {
		return false
	}
	for _, spec := range ownServers {
		if spec.Command == "" {
			continue
		}
		if len(spec.Args) == 0 {
			if len(argv) == 1 && argv[0] == spec.Command {
				return true
			}
			continue
		}
		sameProgram := argv[0] == spec.Command ||
			filepath.Base(argv[0]) == filepath.Base(spec.Command) ||
			npmCLIRunsProgram(argv, spec.Command)
		if sameProgram && argsContainAll(argv[1:], spec.Args) {
			return true
		}
	}
	return filepath.Base(argv[0]) == channelBinaryName
}

// npmCLIRunsProgram reports whether argv is node running npm's launcher script
// for program, such as npx-cli.js for npx. npx does not exec the server it
// starts: the node process running npx-cli.js keeps npx's args and stays alive
// as the parent of the server, so that process is what the spec's argv matches.
func npmCLIRunsProgram(argv []string, program string) bool {
	if len(argv) < 2 {
		return false
	}
	return filepath.Base(argv[1]) == filepath.Base(program)+"-cli.js"
}

// argsContainAll reports whether every element of want appears in argv.
func argsContainAll(argv, want []string) bool {
	for _, w := range want {
		found := false
		for _, a := range argv {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sessionOwnMCPServers returns the MCP server specs a session was launched with:
// the entries in <projectDir>/.mcp.json, plus the native channel bridge when it
// is in use. A missing or unreadable file yields no entries. The native channel
// is then still recognised by its binary name, but a node-run channel.js bridge
// has no name to match and counts as work. Any other server counts as work too,
// which keeps the probe on the side of not killing a session.
func sessionOwnMCPServers(projectDir string) []channel.MCPServerSpec {
	var specs []channel.MCPServerSpec
	if projectDir != "" {
		if raw, err := os.ReadFile(filepath.Join(projectDir, ".mcp.json")); err == nil {
			var cfg channel.MCPProjectConfig
			if json.Unmarshal(raw, &cfg) == nil {
				for _, spec := range cfg.MCPServers {
					specs = append(specs, spec)
				}
			}
		}
	}
	if bin := channel.BridgePath(); bin != "" {
		specs = append(specs, channel.MCPServerSpec{Command: bin})
	}
	return specs
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

// readProcessCmdline returns the argv of pid from /proc/<pid>/cmdline, or nil
// when it cannot be read (the process exited or the cmdline is empty).
func readProcessCmdline(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}
