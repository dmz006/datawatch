package autonomous

import (
	"context"
	"strings"
	"testing"
)

// recordingGit answers Automaton safe-delete git calls from canned output
// keyed by the joined args and records every call.
type recordingGit struct {
	out   map[string]string
	calls []string
}

func (r *recordingGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	return r.out[key], nil
}

const sdWorktrees = "worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n" +
	"worktree /wt/one\nHEAD bbb\nbranch refs/heads/automaton/one\n\n" +
	"worktree /wt/two\nHEAD ccc\nbranch refs/heads/automaton/two\n\n"

// sdGit builds a fake with the listing answers and per-branch comparison
// answers: merged branches are 2 ahead with all patches on main, empty ones
// are 0 ahead, pending ones carry an unmerged patch with a new subject.
func sdGit(branches string, worktrees string) *recordingGit {
	return &recordingGit{out: map[string]string{
		"for-each-ref --format=%(refname:short) refs/heads/automaton/": branches,
		"worktree list --porcelain":                                    worktrees,
		"log main --format=%s":                                         "x\ny\n",
	}}
}

func setMerged(g *recordingGit, b string) {
	g.out["rev-list --count main.."+b] = "2\n"
	g.out["cherry main "+b] = "- a\n- b\n"
	g.out["log main.."+b+" --format=%s"] = "x\ny\n"
}

func setEmpty(g *recordingGit, b string) {
	g.out["rev-list --count main.."+b] = "0\n"
}

func setPending(g *recordingGit, b string) {
	g.out["rev-list --count main.."+b] = "1\n"
	g.out["cherry main "+b] = "+ a\n"
	g.out["log main.."+b+" --format=%s"] = "brand new work\n"
}

func TestSafeToDeleteListsBranchesAllOnMain(t *testing.T) {
	g := sdGit("automaton/one\nautomaton/two\nautomaton/three\n", sdWorktrees)
	setMerged(g, "automaton/one")
	setEmpty(g, "automaton/two")
	setMerged(g, "automaton/three")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 3 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want 3 safe", rep)
	}
	one, two, three := rep.Safe[0], rep.Safe[1], rep.Safe[2]
	if one.Branch != "automaton/one" || one.Worktree != "/wt/one" || one.Reason != "all commits on main" || one.Commits != 2 {
		t.Fatalf("one = %+v", one)
	}
	if two.Worktree != "/wt/two" || two.Reason != "no commits ahead of main" || two.Commits != 0 {
		t.Fatalf("two = %+v", two)
	}
	if three.Worktree != "" || three.Reason != "all commits on main" {
		t.Fatalf("three = %+v", three)
	}
}

func TestSafeToDeleteSkipsBaseAndCheckedOutBranch(t *testing.T) {
	wts := "worktree /repo\nHEAD aaa\nbranch refs/heads/automaton/current\n\n" +
		"worktree /wt/one\nHEAD bbb\nbranch refs/heads/automaton/one\n\n"
	g := sdGit("automaton/current\nautomaton/one\nmain\n", wts)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/current")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 1 || rep.Safe[0].Branch != "automaton/one" || len(rep.Held) != 0 {
		t.Fatalf("report = %+v, want only automaton/one", rep)
	}
	for _, c := range g.calls {
		if strings.Contains(c, "automaton/current") || strings.HasSuffix(c, "..main") {
			t.Fatalf("skipped branch was compared: %q", c)
		}
	}
}

func TestSafeToDeleteHoldsDirtyWorktree(t *testing.T) {
	g := sdGit("automaton/one\nautomaton/two\n", sdWorktrees)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/two")
	g.out["-C /wt/one status --porcelain"] = " M internal/file.go\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 1 || rep.Safe[0].Branch != "automaton/two" {
		t.Fatalf("safe = %+v, want only automaton/two", rep.Safe)
	}
	if rep.HeldDirty != 1 || len(rep.Held) != 1 || !rep.Held[0].Dirty || rep.Held[0].Branch != "automaton/one" {
		t.Fatalf("report = %+v, want automaton/one held dirty", rep)
	}
}

func TestSafeToDeleteHoldsPendingAndNeedsLook(t *testing.T) {
	g := sdGit("automaton/one\nautomaton/two\n", sdWorktrees)
	setPending(g, "automaton/one")
	// needs_look: patch missing from main but the subject is on main.
	g.out["rev-list --count main..automaton/two"] = "1\n"
	g.out["cherry main automaton/two"] = "+ a\n"
	g.out["log main..automaton/two --format=%s"] = "x\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 0 || rep.HeldDirty != 0 || len(rep.Held) != 2 {
		t.Fatalf("report = %+v, want 2 held", rep)
	}
	if rep.Held[0].State != MergeStatePending || rep.Held[1].State != MergeStateNeedsLook {
		t.Fatalf("held states = %q, %q", rep.Held[0].State, rep.Held[1].State)
	}
}

func TestSafeToDeleteNeverDeletes(t *testing.T) {
	g := sdGit("automaton/one\nautomaton/two\nautomaton/three\n", sdWorktrees)
	setMerged(g, "automaton/one")
	setPending(g, "automaton/two")
	setEmpty(g, "automaton/three")
	g.out["-C /wt/one status --porcelain"] = "?? stray\n"

	if _, err := SafeToDeleteReport(context.Background(), g, "/repo", "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.calls) == 0 {
		t.Fatal("expected git calls to be recorded")
	}
	for _, c := range g.calls {
		fields := strings.Fields(c)
		if len(fields) >= 2 && fields[0] == "-C" {
			fields = fields[2:]
		}
		sub := fields[0]
		switch sub {
		case "for-each-ref", "worktree", "rev-list", "cherry", "log", "status":
		default:
			t.Fatalf("non read-only git call: %q", c)
		}
		if sub == "worktree" && fields[1] != "list" {
			t.Fatalf("worktree call other than list: %q", c)
		}
		for _, bad := range []string{"delete", "remove", "push", "reset", "branch", "prune"} {
			if sub == bad {
				t.Fatalf("destructive git call: %q", c)
			}
		}
	}
}

func TestSafeToDeleteEmptyRepoReturnsEmptyReport(t *testing.T) {
	g := sdGit("", "worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n")
	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 0 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want empty", rep)
	}
}
