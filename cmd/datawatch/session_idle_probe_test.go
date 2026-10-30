package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
)

// fakeProcessNames builds a nameOf func over a fixed pid -> command map.
func fakeProcessNames(names map[int]string) func(int) string {
	return func(pid int) string { return names[pid] }
}

// fakeProcessArgv builds a cmdlineOf func over a fixed pid -> argv map.
func fakeProcessArgv(argv map[int][]string) func(int) []string {
	return func(pid int) []string { return argv[pid] }
}

func TestSessionIdleProbe_IdleClaudeWithOwnServersReportsNoWork(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> { channel 400, mcp-search 500 }.
	// The session is idle at an empty prompt: its runner and its own servers are
	// alive, and nothing else. The session's own mcp-search spec is known; the
	// channel is recognised by its binary name alone.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400, 500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "datawatch-chann", 500: "datawatch"}
	argv := map[int][]string{
		100: {"bash"},
		200: {"bash"},
		300: {"claude"},
		400: {"/home/user/.datawatch/channel/datawatch-channel"},
		500: {"/usr/local/bin/datawatch", "mcp-search"},
	}
	ownServers := []channel.MCPServerSpec{{Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if len(got) != 0 {
		t.Fatalf("idle claude with only its own servers: got work children %v, want none", got)
	}
}

func TestSessionIdleProbe_BashChildUnderRunnerIsWork(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> bash 400 -> sleep 500.
	// The runner started a command that is still running: both are work.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}, 400: {500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "bash", 500: "sleep"}
	argv := map[int][]string{400: {"bash", "-c", "sleep 600"}, 500: {"sleep", "600"}}
	ownServers := []channel.MCPServerSpec{{Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if !got[400] || !got[500] || len(got) != 2 {
		t.Fatalf("bash child with sleep grandchild under runner: got %v, want {400, 500}", got)
	}
}

func TestSessionIdleProbe_GrandchildUnderMCPServerIsNotWork(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> mcp-search 500 -> node 600.
	// The MCP server and everything it started belong to the session, not to work.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {500}, 500: {600}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 500: "datawatch", 600: "node"}
	argv := map[int][]string{
		300: {"claude"},
		500: {"/usr/local/bin/datawatch", "mcp-search"},
		600: {"node", "helper.js"},
	}
	ownServers := []channel.MCPServerSpec{{Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if len(got) != 0 {
		t.Fatalf("grandchild under own MCP server: got %v, want none", got)
	}
}

func TestSessionIdleProbe_NoRunnerReportsNoWork(t *testing.T) {
	// pane 100 -> bash 200 -> bash 300 -> sleep 400, and no runner anywhere.
	// Work is only counted below a runner, so none is reported.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}}
	names := map[int]string{100: "bash", 200: "bash", 300: "bash", 400: "sleep"}
	argv := map[int][]string{300: {"bash"}, 400: {"sleep", "600"}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), nil)
	if len(got) != 0 {
		t.Fatalf("no runner: got %v, want none", got)
	}
}

func TestSessionIdleProbe_WorkChildTreeCycleTerminates(t *testing.T) {
	// claude 200 -> bash 300 -> bash 400 -> bash 300 cycle under the runner.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}, 400: {300}}
	names := map[int]string{100: "bash", 200: "claude", 300: "bash", 400: "bash"}
	argv := map[int][]string{300: {"bash"}, 400: {"bash"}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), nil)
	if !got[300] || !got[400] || len(got) != 2 {
		t.Fatalf("cyclic tree under runner: got %v, want {300, 400}", got)
	}
}

func TestSessionIdleProbe_OwnServerMatchingRules(t *testing.T) {
	bridge := channel.MCPServerSpec{Command: "/opt/dw/datawatch-bridge"}
	search := channel.MCPServerSpec{Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}}
	launcher := channel.MCPServerSpec{Command: "npx"}
	npxServer := channel.MCPServerSpec{Command: "npx", Args: []string{"-y", "some-tool"}}
	jsBridge := channel.MCPServerSpec{Command: "/usr/bin/node", Args: []string{"/home/u/.datawatch/channel/channel.js"}}
	cases := []struct {
		name  string
		argv  []string
		specs []channel.MCPServerSpec
		want  bool
	}{
		{"channel by binary name alone", []string{"/home/u/.datawatch/channel/datawatch-channel"}, nil, true},
		{"bare bridge spec matches its exact path", []string{"/opt/dw/datawatch-bridge"}, []channel.MCPServerSpec{bridge}, true},
		{"bare bridge spec ignores a launch with args", []string{"/opt/dw/datawatch-bridge", "x"}, []channel.MCPServerSpec{bridge}, false},
		{"args spec matches by base name and args", []string{"datawatch", "mcp-search"}, []channel.MCPServerSpec{search}, true},
		{"args spec rejects other args", []string{"datawatch", "serve"}, []channel.MCPServerSpec{search}, false},
		{"launcher spec does not claim work it starts", []string{"npx", "-y", "some-tool"}, []channel.MCPServerSpec{launcher}, false},
		{"empty argv is work", nil, []channel.MCPServerSpec{search}, false},
		{"node channel bridge matches its spec", []string{"/usr/bin/node", "/home/u/.datawatch/channel/channel.js"}, []channel.MCPServerSpec{jsBridge}, true},
		{"node running another script is work", []string{"/usr/bin/node", "/srv/other.js"}, []channel.MCPServerSpec{jsBridge}, false},
		{"npx-cli run of the spec's package matches", []string{"node", "/usr/lib/node_modules/npm/bin/npx-cli.js", "-y", "some-tool"}, []channel.MCPServerSpec{npxServer}, true},
		{"npx-cli run of another package is work", []string{"node", "/usr/lib/node_modules/npm/bin/npx-cli.js", "-y", "other-tool"}, []channel.MCPServerSpec{npxServer}, false},
		{"npx-cli run without the spec's args is work", []string{"node", "/usr/lib/node_modules/npm/bin/npx-cli.js"}, []channel.MCPServerSpec{npxServer}, false},
	}
	for _, tc := range cases {
		if got := isOwnMCPServer(tc.argv, tc.specs); got != tc.want {
			t.Errorf("%s: isOwnMCPServer(%v) = %v, want %v", tc.name, tc.argv, got, tc.want)
		}
	}
}

func TestSessionIdleProbe_NpxLaunchedServerIsNotWork(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> node 400 (npx-cli.js) -> node 500.
	// npx started the session's own MCP server. The node process running
	// npx-cli.js matches the spec's npx argv, and the server it started sits in
	// that subtree, so an idle worker reports no work.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}, 400: {500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "node", 500: "node"}
	argv := map[int][]string{
		300: {"claude"},
		400: {"node", "/usr/lib/node_modules/npm/bin/npx-cli.js", "-y", "some-tool"},
		500: {"node", "/home/u/.npm/_npx/some-tool/index.js"},
	}
	ownServers := []channel.MCPServerSpec{{Command: "npx", Args: []string{"-y", "some-tool"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if len(got) != 0 {
		t.Fatalf("npx-launched own server and its subtree: got %v, want none", got)
	}
}

func TestSessionIdleProbe_IdleClaudeWithOnlyMCPChildReportsNoWork(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> datawatch mcp-search 400.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "datawatch"}
	argv := map[int][]string{300: {"claude"}, 400: {"/usr/local/bin/datawatch", "mcp-search"}}
	ownServers := []channel.MCPServerSpec{{Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if len(got) != 0 {
		t.Fatalf("idle claude with only its own MCP child: got %v, want none", got)
	}
}

func TestSessionIdleProbe_WorkBeyondDepthCapNotReported(t *testing.T) {
	// pane 100 -> claude 101 -> a chain of bash processes longer than the cap.
	// The walk stops at the cap, so the first link under the runner is reported
	// as work and the last link of the chain is not reached.
	roots := []int{100}
	names := map[int]string{100: "bash", 101: "claude"}
	children := map[int][]int{100: {101}}
	last := 101
	for pid := 102; pid < 102+sessionIdleProbeMaxDepth+4; pid++ {
		children[last] = []int{pid}
		names[pid] = "bash"
		last = pid
	}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(nil), nil)
	if !got[102] {
		t.Fatalf("first work child under runner not reported: got %v", got)
	}
	if got[last] {
		t.Fatalf("work child %d beyond the depth cap was reported", last)
	}
}

func TestSessionIdleProbe_LauncherServerMatchedSkipsItsSubtree(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> npx 400 -> node 500. The npx argv
	// matches its spec, so npx and the node process it started are both the
	// session's own server and not work.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}, 400: {500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "npx", 500: "node"}
	argv := map[int][]string{300: {"claude"}, 400: {"npx", "-y", "some-tool"}, 500: {"node", "some-tool.js"}}
	ownServers := []channel.MCPServerSpec{{Command: "npx", Args: []string{"-y", "some-tool"}}}
	got := workChildPIDs(roots, children, fakeProcessNames(names), fakeProcessArgv(argv), ownServers)
	if len(got) != 0 {
		t.Fatalf("matched launcher server and its subtree: got %v, want none", got)
	}
}

func TestSessionIdleProbe_OwnServersReadFromProjectMCPConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := channel.MCPProjectConfig{MCPServers: map[string]channel.MCPServerSpec{
		"web_search": {Command: "/usr/local/bin/datawatch", Args: []string{"mcp-search"}},
	}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	specs := sessionOwnMCPServers(dir)
	if len(specs) != 1 || specs[0].Command != "/usr/local/bin/datawatch" || len(specs[0].Args) != 1 || specs[0].Args[0] != "mcp-search" {
		t.Fatalf("sessionOwnMCPServers(project with .mcp.json) = %+v, want the web_search spec", specs)
	}
	if got := sessionOwnMCPServers(filepath.Join(dir, "missing")); len(got) != 0 {
		t.Fatalf("sessionOwnMCPServers(missing dir) = %+v, want none", got)
	}
}

func TestSessionIdleProbe_RunnerUnderShellIsLive(t *testing.T) {
	// pane pid 100 -> bash 200 -> pi 300
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}}
	names := map[int]string{100: "bash", 200: "bash", 300: "pi"}
	if !processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("expected a live runner for pane -> bash -> pi, got none")
	}
}

func TestSessionIdleProbe_BashLeafIsNotLive(t *testing.T) {
	// pane pid 100 -> bash 200 (leaf)
	roots := []int{100}
	children := map[int][]int{100: {200}}
	names := map[int]string{100: "bash", 200: "bash"}
	if processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("expected no live runner for a bash-only leaf, got one")
	}
}

func TestSessionIdleProbe_ClaudeDeepInTreeIsLive(t *testing.T) {
	// pane 100 -> bash 200 -> env 300 -> node 400 -> claude 500
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {400}, 400: {500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "env", 400: "node", 500: "claude"}
	if !processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("expected claude four levels down to count as live")
	}
}

func TestSessionIdleProbe_OpencodeChildIsLive(t *testing.T) {
	roots := []int{100}
	children := map[int][]int{100: {200}}
	names := map[int]string{100: "bash", 200: "opencode"}
	if !processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("expected opencode child to count as live")
	}
}

func TestSessionIdleProbe_RootNameIsNotCounted(t *testing.T) {
	// The pane pid itself is not a descendant, so its name alone is not live.
	roots := []int{100}
	children := map[int][]int{}
	names := map[int]string{100: "pi"}
	if processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("pane pid name must not count as a live child")
	}
}

func TestSessionIdleProbe_RunnerBeyondDepthCapIgnored(t *testing.T) {
	// A chain of bash processes with pi at depth sessionIdleProbeMaxDepth+1.
	roots := []int{1}
	children := map[int][]int{}
	names := map[int]string{1: "bash"}
	last := 1
	for depth := 1; depth <= sessionIdleProbeMaxDepth; depth++ {
		next := last + 1
		children[last] = []int{next}
		names[next] = "bash"
		last = next
	}
	names[last+1] = "pi"
	children[last] = append(children[last], last+1)
	if processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("runner beyond the depth cap must not be reported")
	}
}

func TestSessionIdleProbe_CycleTerminates(t *testing.T) {
	// 100 -> 200 -> 100 cycle with no runner anywhere.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {100}}
	names := map[int]string{100: "bash", 200: "bash"}
	if processTreeHasLiveRunner(roots, children, fakeProcessNames(names)) {
		t.Fatal("expected no live runner in a bash-only cycle")
	}
}

func TestSessionIdleProbe_BuildTreeRespectsVisitedAndDepth(t *testing.T) {
	calls := 0
	childrenOf := func(pid int) []int {
		calls++
		switch pid {
		case 100:
			return []int{200}
		case 200:
			return []int{100} // cycle back to the root
		}
		return nil
	}
	tree := buildSessionIdleProcessTree([]int{100}, childrenOf)
	if len(tree[100]) != 1 || tree[100][0] != 200 {
		t.Fatalf("tree[100] = %v, want [200]", tree[100])
	}
	if calls > 2*sessionIdleProbeMaxDepth+2 {
		t.Fatalf("childrenOf called %d times; visited set should bound it", calls)
	}
}

func TestSessionIdleProbe_RunnerAloneHasNoDescendants(t *testing.T) {
	// A worker parked at a dialog: pane 100 -> claude 200, nothing below it.
	// The runner is alive, but it is not work, so the set is empty.
	roots := []int{100}
	children := map[int][]int{100: {200}}
	names := map[int]string{100: "bash", 200: "claude"}
	got := processTreeRunnerDescendants(roots, children, fakeProcessNames(names))
	if got == nil || len(got) != 0 {
		t.Fatalf("runner alone: got %v, want an empty non-nil set", got)
	}
}

func TestSessionIdleProbe_RunnerDescendantsExcludeRunnerAndNonRunnerBranches(t *testing.T) {
	// pane 100 -> bash 200 -> claude 300 -> { node 400 (MCP helper), bash 500 }
	// and pane 100 -> vim 600 (outside any runner). Only the runner's children
	// count: 400 and 500, never the runner 300, and never vim 600.
	roots := []int{100}
	children := map[int][]int{100: {200, 600}, 200: {300}, 300: {400, 500}}
	names := map[int]string{100: "bash", 200: "bash", 300: "claude", 400: "node", 500: "bash", 600: "vim"}
	got := processTreeRunnerDescendants(roots, children, fakeProcessNames(names))
	want := map[int]bool{400: true, 500: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for pid := range want {
		if !got[pid] {
			t.Fatalf("got %v, missing %d", got, pid)
		}
	}
}

func TestSessionIdleProbe_RootRunnerCountsItsChildren(t *testing.T) {
	// The pane pid is the runner itself (exec'd), so the commands it starts count
	// and the root does not.
	roots := []int{100}
	children := map[int][]int{100: {200}}
	names := map[int]string{100: "claude", 200: "bash"}
	got := processTreeRunnerDescendants(roots, children, fakeProcessNames(names))
	if !got[200] || got[100] || len(got) != 1 {
		t.Fatalf("got %v, want only the child 200", got)
	}
}

func TestSessionIdleProbe_RunnerDescendantsCycleTerminates(t *testing.T) {
	// claude 200 -> bash 300 -> claude 200 cycle: the visited set stops it.
	roots := []int{100}
	children := map[int][]int{100: {200}, 200: {300}, 300: {200}}
	names := map[int]string{100: "bash", 200: "claude", 300: "bash"}
	got := processTreeRunnerDescendants(roots, children, fakeProcessNames(names))
	if !got[300] || got[200] || len(got) != 1 {
		t.Fatalf("got %v, want only 300", got)
	}
}

func TestSessionIdleProbe_RunnerNameFromProcess(t *testing.T) {
	cases := []struct {
		name string
		comm string
		argv []string
		want string
	}{
		{"bare pi comm", "pi", []string{"/usr/local/bin/pi"}, "pi"},
		{"node-hosted pi", "MainThread", []string{"/usr/bin/node", "/opt/tools/bin/pi"}, "pi"},
		{"node with option then claude", "MainThread", []string{"node", "--no-warnings", "/opt/tools/bin/claude"}, "claude"},
		{"node running another script", "MainThread", []string{"node", "server.js"}, "MainThread"},
		{"empty argv", "MainThread", nil, "MainThread"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runnerNameFromProcess(tc.comm, tc.argv); got != tc.want {
				t.Errorf("runnerNameFromProcess(%q, %v) = %q, want %q", tc.comm, tc.argv, got, tc.want)
			}
		})
	}
}
