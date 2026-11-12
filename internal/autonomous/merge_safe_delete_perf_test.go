// Automaton safe-delete fixture/perf tests. These build a generated
// fixture of 300 automaton/* branches in 300 linked worktrees — about one
// third merged, one third empty, one third pending, and every fourth
// worktree dirty — behind a fake MergeGitRunner that never shells out to
// git. The fake enforces the report's call budget (listing calls, per-
// ahead-branch calls, one status call per candidate worktree, and no
// repeated base subject log) by failing the test if a bound is exceeded,
// and adds a fixed per-call latency inside the fake only.
package autonomous

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sdPerfFixtureBranches is the generated fixture size: this many
// automaton/<id> branches, each linked to its own worktree, plus the
// primary checkout.
const sdPerfFixtureBranches = 300

// sdPerfWallBound is the wall-time budget for a cold safe-to-delete report
// over the full fixture, measured with the fake's per-call latency in
// place. Named constant per the task spec.
const sdPerfWallBound = 5 * time.Second

// sdPerfFakeLatency is the fixed latency the fake adds per git call. It
// lives inside the fake only; the test never sleeps to simulate git.
const sdPerfFakeLatency = 2 * time.Millisecond

// sdPerfBase is the fixture's base branch; sdPerfRepo is its path.
const (
	sdPerfBase = "main"
	sdPerfRepo = "/fixture/base-repo"
)

// sdPerfCallLimits are the fake's call budget bounds: at most 4 listing
// calls (branch listing + worktree listing, with headroom for a fallback
// listing), at most 2 calls per branch that is ahead, at most 1 status
// call per candidate worktree, and never more than one base subject log
// for the whole report.
const (
	sdPerfMaxListing     = 4
	sdPerfMaxCallsAhead  = 2
	sdPerfMaxStatusCand  = 1
	sdPerfMaxBaseSubject = 1
)

// sdPerfBranchName is the branch name of fixture slot i (1-based).
func sdPerfBranchName(i int) string { return fmt.Sprintf("automaton/b%03d", i) }

// sdPerfCategorize gives slot i its class: about one third merged, one
// third empty, one third pending.
func sdPerfCategorize(i int) string {
	switch i % 3 {
	case 1:
		return "merged"
	case 2:
		return "empty"
	default:
		return "pending"
	}
}

// sdPerfFake is a MergeGitRunner for the fixture tests: canned output per
// call key, optional failing keys, a fixed per-call latency (the only
// sleep anywhere in these tests), record-every-call counting with hard
// budget bounds, and a per-key call map so the report's call shape is
// checkable. It is safe for concurrent use, matching the report's worker
// pool.
type sdPerfFake struct {
	mu      sync.Mutex
	out     map[string]string
	fail    map[string]string
	calls   []string       // every call, in order, keyed
	perKey  map[string]int // call count per exact key
	counts  map[string]int // listing / ahead / status / base
	limits  sdPerfLimits
	maxBase int // max base subject log calls (one per computation)
	t       *testing.T
}

// sdPerfLimits is the fixture's budget handed to the fake when it is
// built: how many branches are ahead and how many candidate worktrees
// (safe branches that own a worktree) the report is allowed to classify.
type sdPerfLimits struct {
	ahead      int
	candidates int
}

func newSDPerfFake(t *testing.T, limits sdPerfLimits) *sdPerfFake {
	return &sdPerfFake{
		out:     map[string]string{},
		fail:    map[string]string{},
		calls:   []string{},
		perKey:  map[string]int{},
		counts:  map[string]int{"listing": 0, "ahead": 0, "status": 0, "base": 0},
		limits:  limits,
		maxBase: sdPerfMaxBaseSubject,
		t:       t,
	}
}

// Run answers one git call (canned output, or the failing key's error),
// adds the fixed latency, records the call, and fails the test as soon as
// a budget bound is exceeded — from whichever goroutine the report made
// the call in, using the goroutine-safe t.Errorf rather than t.Fatalf.
func (f *sdPerfFake) Run(_ context.Context, _ string, args ...string) (string, error) {
	if sdPerfFakeLatency > 0 {
		time.Sleep(sdPerfFakeLatency)
	}
	key := strings.Join(args, " ")
	class := sdPerfClassifyCall(args)

	f.mu.Lock()
	f.calls = append(f.calls, key)
	f.perKey[key]++
	f.counts[class]++
	msg, failed := f.fail[key]
	over := f.boundsViolated()
	f.mu.Unlock()

	if failed {
		return "", fmt.Errorf("%s (fixture failure for %q)", msg, key)
	}
	if over != "" {
		f.t.Errorf("safe-delete fixture budget exceeded at call %q (%s): %s", key, class, over)
	}
	out, _ := f.snapshotOut(key)
	return out, nil
}

// boundsViolated reports the first exceeded budget bound (caller holds
// the fake lock).
func (f *sdPerfFake) boundsViolated() string {
	if f.counts["listing"] > sdPerfMaxListing {
		return fmt.Sprintf("listing calls = %d, want at most %d", f.counts["listing"], sdPerfMaxListing)
	}
	if want := sdPerfMaxCallsAhead * f.limits.ahead; f.counts["ahead"] > want {
		return fmt.Sprintf("per-ahead-branch calls = %d, want at most %d (%d ahead x %d)", f.counts["ahead"], want, f.limits.ahead, sdPerfMaxCallsAhead)
	}
	if want := sdPerfMaxStatusCand * f.limits.candidates; f.counts["status"] > want {
		return fmt.Sprintf("status calls = %d, want at most %d (%d candidates x %d)", f.counts["status"], want, f.limits.candidates, sdPerfMaxStatusCand)
	}
	if want := f.maxBase; f.counts["base"] > want {
		return fmt.Sprintf("base subject log calls = %d, want at most %d (no repeated per-branch base log)", f.counts["base"], want)
	}
	return ""
}

// sdPerfClassifyCall tags one git call for the budget counters: listing
// (for-each-ref, worktree list), per-ahead-branch (cherry, branch subject
// log "log base..branch"), worktree status ("--porcelain" status with a
// -C worktree), or the base subject log ("log base").
func sdPerfClassifyCall(args []string) string {
	if len(args) >= 3 && args[0] == "-C" && args[2] == "status" {
		return "status"
	}
	if len(args) == 0 {
		return "unclassified"
	}
	switch args[0] {
	case "for-each-ref", "worktree", "rev-list":
		return "listing"
	case "cherry":
		return "ahead"
	case "log":
		if len(args) >= 2 && args[1] == sdPerfBase {
			return "base"
		}
		return "ahead"
	default:
		return "unclassified"
	}
}

// totalCalls returns the number of git calls the fake has recorded.
func (f *sdPerfFake) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// counts returns a snapshot of the budget counters.
func (f *sdPerfFake) countsSnap() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.counts {
		out[k] = v
	}
	return out
}

func (f *sdPerfFake) snapshotOut(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, ok := f.out[key]
	return out, ok
}

// sdPerfExpected are the fixture's expected report tallies and call
// budget, computed from the fixture size: one third merged, one third
// empty, one third pending; every fourth worktree dirty (held dirty when
// safe); status reads only for safe branches with a worktree.
type sdPerfExpected struct {
	safe, held, heldDirty int
	ahead, candidates     int
}

// sdPerfBuildFixture wires the fake's canned output and limits for n
// fixture slots, and returns the expected tallies.
func sdPerfBuildFixture(t *testing.T, n int) (*sdPerfFake, sdPerfExpected) {
	t.Helper()
	// Limits are computed below once the fixture size is applied.
	f := newSDPerfFake(t, sdPerfLimits{})

	var (
		listing   strings.Builder
		worktrees strings.Builder
	)
	// Primary checkout: the base repo with the base branch checked out.
	worktrees.WriteString("worktree " + sdPerfRepo + "\nHEAD aaaa\nbranch refs/heads/" + sdPerfBase + "\n\n")

	exp := sdPerfExpected{}
	for i := 1; i <= n; i++ {
		b := sdPerfBranchName(i)
		branch := "refs/heads/" + b
		path := "/fixture/wt/" + b

		// One linked worktree per branch.
		worktrees.WriteString("worktree " + path + "\nHEAD bbbb\nbranch " + branch + "\n\n")

		switch sdPerfCategorize(i) {
		case "merged":
			exp.safe++
			exp.ahead++
			exp.candidates++
			listing.WriteString(b + " 0 2\n")
			// Two ahead, both patches present on base, base subjects.
			f.out["cherry "+sdPerfBase+" "+b] = "- m1\n- m2\n"
			f.out["log "+sdPerfBase+".."+b+" --format=%s"] = "base subject one\nbase subject two\n"
		case "empty":
			exp.safe++
			exp.candidates++
			listing.WriteString(b + " 0 0\n")
		default: // pending
			exp.held++
			exp.ahead++
			listing.WriteString(b + " 0 1\n")
			// One ahead, the patch missing from base, a new subject.
			f.out["cherry "+sdPerfBase+" "+b] = "+ p" + strconv.Itoa(i) + "\n"
			f.out["log "+sdPerfBase+".."+b+" --format=%s"] = "unmerged work " + strconv.Itoa(i) + "\n"
		}

		// Every fourth worktree is dirty.
		if i%4 == 0 {
			f.out["-C "+path+" status --porcelain"] = " M fixture/file.go\n"
			if sdPerfCategorize(i) == "pending" {
				// A pending branch is held already; the dirty mark is
				// invisible in the report but the worktree is dirty.
			} else {
				// A safe branch with a dirty worktree: held dirty.
				exp.safe--
				exp.held++
				exp.heldDirty++
			}
		}
	}

	f.limits = sdPerfLimits{ahead: exp.ahead, candidates: exp.candidates}
	f.out["for-each-ref --format=%(refname:short) %(ahead-behind:"+sdPerfBase+") refs/heads/automaton/"] = listing.String()
	f.out["for-each-ref --format=%(refname:short) refs/heads/automaton/"] = ""
	f.out["worktree list --porcelain"] = strings.TrimSuffix(worktrees.String(), "\n")
	f.out["log "+sdPerfBase+" --format=%s"] = "base subject one\nbase subject two\n"
	return f, exp
}

// sdPerfRepoReport is one report computation over the fixture, used both
// directly and as the cache's compute function.
func sdPerfRepoReport(t *testing.T, f *sdPerfFake, repo string) func(context.Context) (SafeDeleteReport, error) {
	return func(ctx context.Context) (SafeDeleteReport, error) {
		return SafeToDeleteReport(ctx, f, repo, sdPerfBase, SafeDeleteOptions{})
	}
}

// TestSafeDeleteFixtureColdReportUnderFiveSeconds covers (1): a cold
// SafeToDeleteReport over the full 300-worktree fixture finishes inside
// the named wall-time bound with the fake's simulated latency in place,
// stays inside the fake's call budget, and has the expected safe and
// held tallies.
func TestSafeDeleteFixtureColdReportUnderFiveSeconds(t *testing.T) {
	f, exp := sdPerfBuildFixture(t, sdPerfFixtureBranches)

	// The report's worker pool joins before it returns, so measuring
	// around the call captures every fake latency; the fresh-fixture
	// fake means this is a cold report end to end.
	start := time.Now()
	report, err := SafeToDeleteReport(context.Background(), f, sdPerfRepo, sdPerfBase, SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("SafeToDeleteReport over the fixture: %v", err)
	}
	if dur := time.Since(start); dur >= sdPerfWallBound {
		t.Fatalf("a cold report over %d worktrees took %v, want under the bound %v",
			sdPerfFixtureBranches, dur, sdPerfWallBound)
	}

	if len(report.Safe) != exp.safe {
		t.Fatalf("safe = %d, want %d", len(report.Safe), exp.safe)
	}
	if len(report.Held) != exp.held {
		t.Fatalf("held = %d, want %d", len(report.Held), exp.held)
	}
	if report.HeldDirty != exp.heldDirty {
		t.Fatalf("held_dirty = %d, want %d", report.HeldDirty, exp.heldDirty)
	}

	// The budget: at most 4 listing calls, at most 2 per ahead branch,
	// at most 1 status call per candidate worktree, and exactly one base
	// subject log (no repeated per-branch base log).
	counts := f.countsSnap()
	if counts["listing"] > sdPerfMaxListing {
		t.Errorf("listing calls = %d, want at most %d", counts["listing"], sdPerfMaxListing)
	}
	if want := sdPerfMaxCallsAhead * exp.ahead; counts["ahead"] > want {
		t.Errorf("per-ahead-branch calls = %d, want at most %d", counts["ahead"], want)
	}
	if want := sdPerfMaxStatusCand * exp.candidates; counts["status"] > want {
		t.Errorf("status calls = %d, want at most %d", counts["status"], want)
	}
	if counts["base"] > sdPerfMaxBaseSubject {
		t.Errorf("base subject log calls = %d, want at most %d (one per report)", counts["base"], sdPerfMaxBaseSubject)
	}
	if counts["listing"] == 0 || counts["ahead"] == 0 {
		t.Fatalf("budget counters unexpectedly empty: %v", counts)
	}
}

// TestSafeDeleteFixtureCacheWarmReadMakesNoGitCalls covers (2): through
// the safe-delete cache (with its injectable compute and clock), the
// second read makes zero new git calls and reports computed_at equal to
// the fixed clock value.
func TestSafeDeleteFixtureCacheWarmReadMakesNoGitCalls(t *testing.T) {
	f, exp := sdPerfBuildFixture(t, sdPerfFixtureBranches)
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base) // from merge_safe_delete_cache_test.go
	compute := sdPerfRepoReport(t, f, sdPerfRepo)

	cache := newSafeDeleteCache()
	cache.now = clock.now
	cache.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		return compute(ctx)
	}

	first, err := cache.get(context.Background(), sdPerfRepo)
	if err != nil {
		t.Fatalf("cold cache read: %v", err)
	}
	if len(first.Safe) != exp.safe || len(first.Held) != exp.held || first.HeldDirty != exp.heldDirty {
		t.Fatalf("cold read tallies = safe %d / held %d / held_dirty %d, want %d / %d / %d",
			len(first.Safe), len(first.Held), first.HeldDirty, exp.safe, exp.held, exp.heldDirty)
	}
	afterCold := f.totalCalls()
	if afterCold == 0 {
		t.Fatalf("the cold read made no git calls; the fake was not used as the compute")
	}
	// computed_at is the fixed clock value, stamped by the cache at compute time.
	if want := base.Format(time.RFC3339); first.ComputedAt != want {
		t.Fatalf("cold read ComputedAt = %q, want the fixed clock %q", first.ComputedAt, want)
	}

	// A second read inside the lifetime: zero new git calls, same tally,
	// same computed_at (the fixed clock value), served fresh.
	second, err := cache.get(context.Background(), sdPerfRepo)
	if err != nil {
		t.Fatalf("warm cache read: %v", err)
	}
	if got := f.totalCalls(); got != afterCold {
		t.Fatalf("git calls = %d after the warm read, want %d (zero new git calls)", got, afterCold)
	}
	if second.Stale {
		t.Errorf("a warm read within the lifetime reports stale = true")
	}
	if want := base.Format(time.RFC3339); second.ComputedAt != want {
		t.Errorf("warm read ComputedAt = %q, want the fixed clock value %q", second.ComputedAt, want)
	}
	if len(second.Safe) != exp.safe || len(second.Held) != exp.held {
		t.Fatalf("warm read tallies = safe %d / held %d, want %d / %d", len(second.Safe), len(second.Held), exp.safe, exp.held)
	}
}

// TestSafeDeleteFixtureCacheStaleReadReturnsAtOnce covers (3): advancing
// the fixed clock past the cache lifetime makes the next read return the
// cached report at once with stale=true, and exactly one background
// refresh recomputes (no refresh loop, no repeated computation).
func TestSafeDeleteFixtureCacheStaleReadReturnsAtOnce(t *testing.T) {
	f, exp := sdPerfBuildFixture(t, sdPerfFixtureBranches)
	// This test runs exactly two computations (the cold read plus the one
	// background refresh), so the fake's budget bounds are scaled for two
	// such reports; the exactly-one-refresh assertion below is what pins
	// down that no refresh loop runs.
	f.limits = sdPerfLimits{ahead: 2 * exp.ahead, candidates: 2 * exp.candidates}
	f.maxBase = 2 // two computations, one subject log each
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	compute := sdPerfRepoReport(t, f, sdPerfRepo)
	computeCalls := &sdComputeCalls{} // locked counter, cache_test helper

	cache := newSafeDeleteCache()
	cache.now = clock.now
	cache.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		computeCalls.inc()
		return compute(ctx)
	}

	first, err := cache.get(context.Background(), sdPerfRepo)
	if err != nil {
		t.Fatalf("cold cache read: %v", err)
	}
	if computeCalls.snap() != 1 {
		t.Fatalf("compute calls after cold read = %d, want 1", computeCalls.snap())
	}

	clock.advance(safeDeleteCacheTTL + time.Second) // past the lifetime

	stale, err := cache.get(context.Background(), sdPerfRepo)
	if err != nil {
		t.Fatalf("stale cache read: %v", err)
	}
	// The stale read returned the cached report at once, marked stale,
	// with the original computed_at.
	if !stale.Stale {
		t.Errorf("a stale read reports stale = false")
	}
	if want := base.Format(time.RFC3339); stale.ComputedAt != want {
		t.Errorf("stale read ComputedAt = %q, want the original %q", stale.ComputedAt, want)
	}
	if !sameReport(stale, first) {
		t.Fatalf("stale read = %+v, want the cached report %+v", stale, first)
	}
	// The stale read returned before the background refresh finished
	// recomputing (exactly one refresh, started after the read).
	if computeCalls.snap() != 1 {
		t.Errorf("compute calls right after the stale read = %d, want 1 (no synchronous recompute)", computeCalls.snap())
	}

	// The exactly-one background refresh then recomputes once more.
	sdWaitUntil(t, "the single background refresh to finish", func() bool {
		return computeCalls.snap() == 2
	})
	// Let the entry settle (refreshing cleared) before the final read.
	sdWaitUntil(t, "the refresh to be marked done", func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		return !cache.refreshing[sdPerfRepo]
	})
	if computeCalls.snap() != 2 {
		t.Fatalf("compute calls after the refresh = %d, want 2 (exactly one background refresh)", computeCalls.snap())
	}
	// A final read is warm against the refreshed entry: no third compute.
	refreshed, err := cache.get(context.Background(), sdPerfRepo)
	if err != nil {
		t.Fatalf("post-refresh cache read: %v", err)
	}
	if computeCalls.snap() != 2 {
		t.Errorf("compute calls after the post-refresh read = %d, want 2", computeCalls.snap())
	}
	if refreshed.Stale {
		t.Errorf("the refreshed report is served stale")
	}
}

// TestSafeDeleteFixtureEmptyRepoIsAnEmptyReport covers (4a): a fixture
// with zero worktrees (no automaton/* branches, only the primary
// checkout) produces an empty report and no per-branch git work.
func TestSafeDeleteFixtureEmptyRepoIsAnEmptyReport(t *testing.T) {
	f, exp := sdPerfBuildFixture(t, 0)
	if exp.safe != 0 || exp.held != 0 || exp.heldDirty != 0 {
		t.Fatalf("empty fixture expectations are non-zero: %+v", exp)
	}

	report, err := SafeToDeleteReport(context.Background(), f, sdPerfRepo, sdPerfBase, SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("SafeToDeleteReport over an empty fixture: %v", err)
	}
	if len(report.Safe) != 0 || len(report.Held) != 0 || report.HeldDirty != 0 {
		t.Fatalf("report over an empty fixture = %+v, want empty", report)
	}
	// Only the listing calls (branch listing + worktree listing) were
	// made; no per-branch, status, or base subject log calls.
	counts := f.countsSnap()
	if counts["ahead"] != 0 || counts["status"] != 0 || counts["base"] != 0 {
		t.Fatalf("per-branch/status/base calls over an empty fixture = %v, want all zero", counts)
	}
	if counts["listing"] > sdPerfMaxListing {
		t.Errorf("listing calls = %d, want at most %d", counts["listing"], sdPerfMaxListing)
	}
}

// TestSafeDeleteFixtureBranchErrorHoldsUnknown covers (4b): a fixture
// where one branch's git call errors holds that branch as unknown (with
// the error in the note) and does not fail the whole report — the other
// branches are classified as usual.
func TestSafeDeleteFixtureBranchErrorHoldsUnknown(t *testing.T) {
	// Four slots: 1 merged, 2 empty, 3 pending, 4 merged (+dirty). The
	// pending branch's cherry call is made to fail.
	const n = 4
	f, exp := sdPerfBuildFixture(t, n)
	broken := sdPerfBranchName(3)
	f.fail["cherry "+sdPerfBase+" "+broken] = "cherry boom"

	report, err := SafeToDeleteReport(context.Background(), f, sdPerfRepo, sdPerfBase, SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("a single branch git error must not fail the report: %v", err)
	}

	// The broken branch is held as unknown with the error in the note.
	var found *SafeDeleteHeld
	for i := range report.Held {
		if report.Held[i].Branch == broken {
			found = &report.Held[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("the branch whose git call errored is not held: report = %+v", report)
	}
	if found.State != MergeStateUnknown {
		t.Fatalf("held state = %q, want %q", found.State, MergeStateUnknown)
	}
	if !strings.Contains(found.Note, "cherry") {
		t.Errorf("held note = %q, want it to carry the cherry failure", found.Note)
	}
	if found.Dirty {
		t.Errorf("a held-unknown branch is not read for status and must not be marked dirty")
	}

	// The expected tallies hold: the error branch is held, the others
	// are classified (merged/empty safe or held dirty as the fixture
	// says).
	if len(report.Held) != exp.held {
		t.Fatalf("held = %d, want %d (report = %+v)", len(report.Held), exp.held, report)
	}
	if len(report.Safe) != exp.safe {
		t.Fatalf("safe = %d, want %d (report = %+v)", len(report.Safe), exp.safe, report)
	}
	if report.HeldDirty != exp.heldDirty {
		t.Fatalf("held_dirty = %d, want %d", report.HeldDirty, exp.heldDirty)
	}
}
