package autonomous

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// recordingGit answers Automaton safe-delete git calls from canned output
// keyed by the joined args, returns an error for keys in fail, and records
// every call. It is safe for concurrent use, matching the report's worker
// pool.
type recordingGit struct {
	mu    sync.Mutex
	out   map[string]string
	fail  map[string]string
	calls []string
}

func (r *recordingGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, key)
	if msg, ok := r.fail[key]; ok {
		r.mu.Unlock()
		return "", errors.New(msg)
	}
	out, ok := r.out[key]
	r.mu.Unlock()
	if !ok {
		return "", nil
	}
	return out, nil
}

const sdWorktrees = "worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n" +
	"worktree /wt/one\nHEAD bbb\nbranch refs/heads/automaton/one\n\n" +
	"worktree /wt/two\nHEAD ccc\nbranch refs/heads/automaton/two\n\n"

// sdListKey is the fake key of the branch listing call carrying each
// branch's ahead count; the line shape is "<branch> <behind> <ahead>".
const sdListKey = "for-each-ref --format=%(refname:short) %(ahead-behind:main) refs/heads/automaton/"

// sdListKeyPlain is the fallback plain-name branch listing call.
const sdListKeyPlain = "for-each-ref --format=%(refname:short) refs/heads/automaton/"

// sdGit builds a fake with the listing answers and the once-per-report base
// subject log; branches are added with sdAddBranch.
func sdGit(worktrees string) *recordingGit {
	return &recordingGit{out: map[string]string{
		sdListKey:                   "",
		sdListKeyPlain:              "",
		"worktree list --porcelain": worktrees,
		"log main --format=%s":      "x\ny\n",
	}}
}

// sdAddBranch registers one branch in the ahead-behind listing output with
// its ahead count.
func sdAddBranch(g *recordingGit, b string, ahead int) {
	g.out[sdListKey] += b + " 0 " + strconv.Itoa(ahead) + "\n"
}

func setMerged(g *recordingGit, b string) {
	// 2 ahead with all patches on main.
	sdAddBranch(g, b, 2)
	g.out["cherry main "+b] = "- a\n- b\n"
	g.out["log main.."+b+" --format=%s"] = "x\ny\n"
}

func setEmpty(g *recordingGit, b string) {
	sdAddBranch(g, b, 0)
}

func setPending(g *recordingGit, b string) {
	// 1 ahead with an unmerged patch and a new subject.
	sdAddBranch(g, b, 1)
	g.out["cherry main "+b] = "+ a\n"
	g.out["log main.."+b+" --format=%s"] = "brand new work\n"
}

func TestSafeToDeleteListsBranchesAllOnMain(t *testing.T) {
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setEmpty(g, "automaton/two")
	setMerged(g, "automaton/three")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
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
	g := sdGit(wts)
	setMerged(g, "automaton/current")
	setMerged(g, "automaton/one")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 1 || rep.Safe[0].Branch != "automaton/one" || len(rep.Held) != 0 {
		t.Fatalf("report = %+v, want only automaton/one", rep)
	}
	for _, c := range sdCalls(g) {
		if strings.Contains(c, "automaton/current") || strings.HasSuffix(c, "..main") {
			t.Fatalf("skipped branch was compared: %q", c)
		}
	}
}

func TestSafeToDeleteHoldsDirtyWorktree(t *testing.T) {
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/two")
	g.out["-C /wt/one status --porcelain"] = " M internal/file.go\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
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
	g := sdGit(sdWorktrees)
	setPending(g, "automaton/one")
	// needs_look: patch missing from main but the subject is on main.
	sdAddBranch(g, "automaton/two", 1)
	g.out["cherry main automaton/two"] = "+ a\n"
	g.out["log main..automaton/two --format=%s"] = "x\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
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
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setPending(g, "automaton/two")
	setEmpty(g, "automaton/three")
	g.out["-C /wt/one status --porcelain"] = "?? stray\n"

	if _, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.calls) == 0 {
		t.Fatal("expected git calls to be recorded")
	}
	for _, c := range sdCalls(g) {
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
		if sub == "status" && (len(fields) < 2 || fields[1] != "--porcelain") {
			t.Fatalf("status call other than --porcelain: %q", c)
		}
		for _, bad := range []string{"delete", "remove", "push", "reset", "branch", "prune"} {
			if sub == bad {
				t.Fatalf("destructive git call: %q", c)
			}
		}
	}
}

func TestSafeToDeleteEmptyRepoReturnsEmptyReport(t *testing.T) {
	g := sdGit("worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n")
	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 0 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want empty", rep)
	}
}

func sdCount(g *recordingGit, key string) int {
	n := 0
	for _, c := range sdCalls(g) {
		if c == key {
			n++
		}
	}
	return n
}

func sdCalls(g *recordingGit) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	calls := make([]string, len(g.calls))
	copy(calls, g.calls)
	return calls
}

func TestSafeToDeleteCountsGitCallsForThreeBranches(t *testing.T) {
	g := sdGit(sdWorktrees)
	setMerged(g, "automaton/one")
	setMerged(g, "automaton/two")
	setEmpty(g, "automaton/three")

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Safe) != 3 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want 3 safe", rep)
	}

	// The listing runs once, not once per branch.
	if got := sdCount(g, sdListKey); got != 1 {
		t.Fatalf("ahead-behind listing calls = %d, want 1", got)
	}
	if got := sdCount(g, sdListKeyPlain); got != 0 {
		t.Fatalf("plain listing calls = %d, want 0 (no fallback)", got)
	}
	for _, c := range sdCalls(g) {
		if strings.HasPrefix(c, "rev-list") {
			t.Fatalf("unexpected per-branch rev-list call on the atom path: %q", c)
		}
	}
	// The base subject log is fetched once per report.
	if got := sdCount(g, "log main --format=%s"); got != 1 {
		t.Fatalf("base subject log calls = %d, want 1", got)
	}
	// Ahead branches make at most two calls (cherry, branch subject log).
	if got := sdCount(g, "cherry main automaton/one"); got != 1 {
		t.Fatalf("cherry calls for automaton/one = %d, want 1", got)
	}
	if got := sdCount(g, "log main..automaton/one --format=%s"); got != 1 {
		t.Fatalf("branch subject log calls for automaton/one = %d, want 1", got)
	}
	// A zero-ahead branch makes no further git calls.
	if got := sdCount(g, "cherry main automaton/three"); got != 0 {
		t.Fatalf("cherry calls for automaton/three = %d, want 0", got)
	}
	if got := sdCount(g, "log main..automaton/three --format=%s"); got != 0 {
		t.Fatalf("branch subject log calls for automaton/three = %d, want 0", got)
	}
	// Total: 1 listing + 1 worktree list + 1 base log + 2 ahead branches x 2
	// + 2 status checks for the safe worktrees.
	if len(g.calls) != 9 {
		t.Fatalf("git calls = %d (%v), want 9", len(g.calls), g.calls)
	}
}

func TestSafeToDeleteFallsBackToRevListWhenAheadBehindFails(t *testing.T) {
	g := sdGit(sdWorktrees)
	g.fail = map[string]string{sdListKey: "error: unknown format item 'ahead-behind'"}
	g.out[sdListKeyPlain] = "automaton/one\nautomaton/two\n"
	g.out["rev-list --count main..automaton/one"] = "2\n"
	g.out["cherry main automaton/one"] = "- a\n- b\n"
	g.out["log main..automaton/one --format=%s"] = "x\ny\n"
	g.out["rev-list --count main..automaton/two"] = "0\n"

	rep, err := SafeToDeleteReport(context.Background(), g, "/repo", "main", SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Identical report to the ahead-behind atom path.
	if len(rep.Safe) != 2 || len(rep.Held) != 0 || rep.HeldDirty != 0 {
		t.Fatalf("report = %+v, want 2 safe", rep)
	}
	if rep.Safe[0].Branch != "automaton/one" || rep.Safe[0].Worktree != "/wt/one" || rep.Safe[0].Reason != "all commits on main" || rep.Safe[0].Commits != 2 {
		t.Fatalf("one = %+v", rep.Safe[0])
	}
	if rep.Safe[1].Branch != "automaton/two" || rep.Safe[1].Worktree != "/wt/two" || rep.Safe[1].Reason != "no commits ahead of main" || rep.Safe[1].Commits != 0 {
		t.Fatalf("two = %+v", rep.Safe[1])
	}

	// The failing atom call was attempted exactly once, followed by the
	// plain listing and one rev-list count per branch.
	if got := sdCount(g, sdListKey); got != 1 {
		t.Fatalf("ahead-behind listing attempts = %d, want 1", got)
	}
	if got := sdCount(g, sdListKeyPlain); got != 1 {
		t.Fatalf("plain listing calls = %d, want 1", got)
	}
	if got := sdCount(g, "rev-list --count main..automaton/one"); got != 1 {
		t.Fatalf("rev-list calls for automaton/one = %d, want 1", got)
	}
	if got := sdCount(g, "rev-list --count main..automaton/two"); got != 1 {
		t.Fatalf("rev-list calls for automaton/two = %d, want 1", got)
	}
	// A zero-ahead branch stays comparison-free after its count.
	if got := sdCount(g, "cherry main automaton/two"); got != 0 {
		t.Fatalf("cherry calls for automaton/two = %d, want 0", got)
	}
}

// TestRenderSafeToDeleteComputedAtAndStaleLines proves the renderer prints
// the computed-at stamp, as given, and the stale note only when the report
// is stale.
func TestRenderSafeToDeleteComputedAtAndStaleLines(t *testing.T) {
	rep := SafeDeleteReport{
		Safe:       []SafeDeleteEntry{{Branch: "automaton/done", Reason: "all commits on main"}},
		ComputedAt: "2026-01-02T09:30:00Z",
	}
	got := RenderSafeToDelete(rep)
	if !strings.Contains(got, "Computed at 2026-01-02T09:30:00Z") {
		t.Errorf("missing the computed-at line:\n%s", got)
	}
	if strings.Contains(got, "stale") {
		t.Errorf("fresh report must not carry the stale note:\n%s", got)
	}
	rep.Stale = true
	got = RenderSafeToDelete(rep)
	if !strings.Contains(got, "This report is stale; a refresh is under way or due.") {
		t.Errorf("stale report missing the stale note:\n%s", got)
	}
	if !strings.Contains(got, "Computed at 2026-01-02T09:30:00Z") {
		t.Errorf("stale report lost the computed-at line:\n%s", got)
	}
	// A report without either field renders exactly as before.
	if got := RenderSafeToDelete(SafeDeleteReport{Safe: rep.Safe}); strings.Contains(got, "Computed at") || strings.Contains(got, "stale") {
		t.Errorf("report without meta fields must not gain meta lines:\n%s", got)
	}
}

// TestParseSafeToDeleteAcceptsBodiesWithAndWithoutMetaFields proves parsing
// stays tolerant of the new fields: bodies from before they existed still
// parse, and stamped bodies carry the values through.
func TestParseSafeToDeleteAcceptsBodiesWithAndWithoutMetaFields(t *testing.T) {
	old, err := ParseSafeToDelete([]byte(`{"safe":[],"held":[],"held_dirty":0}`))
	if err != nil {
		t.Fatalf("a pre-field body must still parse: %v", err)
	}
	if old.ComputedAt != "" || old.Stale {
		t.Errorf("pre-field body must parse with empty meta: %+v", old)
	}
	rep, err := ParseSafeToDelete([]byte(`{"safe":[],"held":[],"held_dirty":0,"computed_at":"2026-01-02T09:30:00Z","stale":true}`))
	if err != nil {
		t.Fatalf("stamped body: %v", err)
	}
	if rep.ComputedAt != "2026-01-02T09:30:00Z" || !rep.Stale {
		t.Errorf("stamped body not carried through: %+v", rep)
	}
}
