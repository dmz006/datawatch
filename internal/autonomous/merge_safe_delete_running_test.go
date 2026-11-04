package autonomous

import (
	"context"
	"strings"
	"testing"
)

// sdIsRunning builds the report options with IsRunning true for exactly the
// named branches and false for the rest.
func sdIsRunning(running ...string) SafeDeleteOptions {
	set := make(map[string]bool, len(running))
	for _, b := range running {
		set[b] = true
	}
	return SafeDeleteOptions{
		IsRunning: func(branch string) bool { return set[branch] },
	}
}

func TestSafeToDeleteSkipsStatusCallForRunningAutomaton(t *testing.T) {
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/two")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", sdIsRunning("automaton/one"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both entries keep their safe classification.
	if len(rep.Safe) != 2 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want both safe", rep)
	}
	one, two := rep.Safe[0], rep.Safe[1]
	if one.Branch != "automaton/one" || one.Worktree != "/wt/one" || one.Commits != 2 {
		t.Fatalf("one = %+v", one)
	}
	if !one.DirtyCheckSkipped {
		t.Fatalf("one.DirtyCheckSkipped = false, want true: %+v", one)
	}
	if !strings.Contains(one.Reason, "dirty check skipped") || !strings.Contains(one.Reason, "running") {
		t.Fatalf("one.Reason = %q, want it to say the dirty check was skipped because the Automaton is running", one.Reason)
	}
	// The non-running Automaton keeps its status check and flag.
	if two.Branch != "automaton/two" || two.DirtyCheckSkipped {
		t.Fatalf("two = %+v, want non-running entry without the flag", two)
	}

	// Exactly one status call: for the non-running worktree only.
	var statusCalls []string
	for _, c := range sdCalls(g) {
		if strings.Contains(c, "status --porcelain") {
			statusCalls = append(statusCalls, c)
		}
	}
	if len(statusCalls) != 1 || statusCalls[0] != "-C /wt/two status --porcelain" {
		t.Fatalf("status calls = %v, want only [-C /wt/two status --porcelain]", statusCalls)
	}
}

func TestSafeToDeleteNonRunningAutomatonStillHeldWhenDirty(t *testing.T) {
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/two")
	g.out["-C /wt/one status --porcelain"] = " M internal/file.go\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", sdIsRunning("automaton/two"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// One's Automaton is not running: the status call ran and its dirty
	// worktree is still held.
	if got := sdCount(g, "-C /wt/one status --porcelain"); got != 1 {
		t.Fatalf("status calls for /wt/one = %d, want 1", got)
	}
	if rep.HeldDirty != 1 || len(rep.Held) != 1 || !rep.Held[0].Dirty || rep.Held[0].Branch != "automaton/one" {
		t.Fatalf("report = %+v, want automaton/one held dirty", rep)
	}
	if len(rep.Safe) != 1 || rep.Safe[0].Branch != "automaton/two" {
		t.Fatalf("safe = %+v, want only automaton/two", rep.Safe)
	}
	if !rep.Safe[0].DirtyCheckSkipped {
		t.Fatalf("two = %+v, want DirtyCheckSkipped true", rep.Safe[0])
	}
	if got := sdCount(g, "-C /wt/two status --porcelain"); got != 0 {
		t.Fatalf("status calls for /wt/two = %d, want 0 (running)", got)
	}
}
