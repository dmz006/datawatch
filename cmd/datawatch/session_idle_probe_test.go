package main

import "testing"

// fakeProcessNames builds a nameOf func over a fixed pid -> command map.
func fakeProcessNames(names map[int]string) func(int) string {
	return func(pid int) string { return names[pid] }
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
