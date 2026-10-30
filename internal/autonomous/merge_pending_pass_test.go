package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// scriptedMergeGit answers the git queries of the merge-pending pass from a fixed
// description of an Automaton branch and main, and counts every call.
type scriptedMergeGit struct {
	mu         sync.Mutex
	calls      [][]string
	branchTip  string
	baseTip    string
	ahead      int
	cherry     string
	branchLog  string
	baseLog    string
	failOnVerb string
	// tipsUnreadable makes the ref-file tip lookup give up, so the pass falls
	// back to git rev-parse.
	tipsUnreadable bool
	tipReads       int
}

// refTips is the fake ref-file lookup: it answers from the scripted tips and
// counts the lookups, which run no git.
func (f *scriptedMergeGit) refTips(_, _, _ string) (string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tipReads++
	if f.tipsUnreadable {
		return "", "", false
	}
	return f.branchTip, f.baseTip, true
}

func (f *scriptedMergeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.failOnVerb == args[0] {
		return "fatal: simulated failure", errors.New("exit status 128")
	}
	switch args[0] {
	case "rev-parse":
		return f.branchTip + "\n" + f.baseTip + "\n", nil
	case "rev-list":
		return strconv.Itoa(f.ahead) + "\n", nil
	case "cherry":
		return f.cherry, nil
	case "log":
		if strings.Contains(args[1], "..") {
			return f.branchLog, nil
		}
		return f.baseLog, nil
	}
	return "", errors.New("unexpected git call " + args[0])
}

func (f *scriptedMergeGit) count(verb string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c[0] == verb {
			n++
		}
	}
	return n
}

func (f *scriptedMergeGit) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// setPending describes a branch with two commits whose patches and subjects
// are not on main.
func (f *scriptedMergeGit) setPending(branchTip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branchTip, f.ahead = branchTip, 2
	f.cherry = "+ aaa\n+ bbb\n"
	f.branchLog = "Add the first part\nAdd the second part\n"
	f.baseLog = "An unrelated commit\n"
}

// setMerged describes the same branch after it landed on main.
func (f *scriptedMergeGit) setMerged(branchTip, baseTip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branchTip, f.baseTip, f.ahead = branchTip, baseTip, 2
	f.cherry = "- aaa\n- bbb\n"
	f.branchLog = "Add the first part\nAdd the second part\n"
	f.baseLog = "Add the first part\nAdd the second part\nAn unrelated commit\n"
}

const mergeTestBranch = "automaton/alpha-work"

// newMergePendingHarness builds a triage harness with one completed Automaton
// ("alpha") on a work branch, the merge-pending mode and alert threshold set,
// and the fake git runner installed.
func newMergePendingHarness(t *testing.T, mode string, alertMinutes int) (*triageHarness, *scriptedMergeGit) {
	t.Helper()
	th := newTriageHarness(t, 1, 10)
	g := th.h.m.Config()
	g.Automation.MergePendingMode = mode
	g.Automation.MergePendingAlertMinutes = &alertMinutes
	g.WorktreeBaseRepo = "/repos/base"
	th.h.m.SetConfig(g)

	git := &scriptedMergeGit{baseTip: "base-1"}
	git.setPending("branch-1")
	th.h.c.mp.git = git
	th.h.c.mp.refTips = git.refTips

	p, ok := th.h.m.store.GetPRD(th.h.prds[0].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Status = PRDCompleted
	p.Git.Branch = mergeTestBranch
	if err := th.h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return th, git
}

const mergeAlertPrefix = "warn|Automaton alpha merge pending|"

func TestMergePendingPass_BranchWithCommitsNotOnMain_AlertsAfterThreshold(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)

	th.h.c.triagePass()
	rec := th.record(t, 0)
	if rec == nil {
		t.Fatal("no merge_pending record after the first pass")
	}
	if rec.Status != TriageStatusMergePending || rec.Branch != mergeTestBranch || rec.CommitsAhead != 2 {
		t.Fatalf("record = %+v, want merge_pending on %s with 2 commits ahead", rec, mergeTestBranch)
	}
	th.assertNoAlerts(t, "")

	th.advance(5 * time.Minute)
	th.h.c.triagePass()
	th.assertNoAlerts(t, "")

	th.advance(5 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 1, mergeAlertPrefix)
	body := th.alertEntry(t, mergeAlertPrefix)
	containsAll(t, body, "Automaton", mergeTestBranch, "2 commit(s)", "main")
	if rec := th.record(t, 0); rec == nil || rec.AlertedAt == nil {
		t.Fatalf("record after the alert = %+v, want AlertedAt stamped", rec)
	}
	if git.count("rev-list") != 1 {
		t.Fatalf("rev-list ran %d times, want 1 (tips never moved)", git.count("rev-list"))
	}
}

func TestMergePendingPass_EmptyBranch_NoAlertNoRecord(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	git.mu.Lock()
	git.ahead, git.cherry, git.branchLog = 0, "", ""
	git.mu.Unlock()

	for i := 0; i < 4; i++ {
		th.h.c.triagePass()
		th.advance(10 * time.Minute)
	}
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want none for an empty branch", rec)
	}
	th.assertNoAlerts(t, "")
}

func TestMergePendingPass_PatchChangedDuringMerge_NeedsLookNoAlert(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	// the subjects landed on main but a patch differs: a human has to look
	git.mu.Lock()
	git.ahead, git.cherry = 1, "+ aaa\n"
	git.branchLog = "Add the first part\n"
	git.baseLog = "Add the first part\n"
	git.mu.Unlock()

	for i := 0; i < 4; i++ {
		th.h.c.triagePass()
		th.advance(10 * time.Minute)
	}
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want none for needs_look", rec)
	}
	th.assertNoAlerts(t, "")
}

func TestMergePendingPass_ShadowRecordsWouldAlertWithoutAlert(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeShadow, 10)

	th.h.c.triagePass()
	got := th.h.c.MergeShadowDecisions(0)
	if len(got) != 1 {
		t.Fatalf("shadow decisions = %+v, want 1", got)
	}
	d := got[0]
	if d.AutomatonID != th.h.prds[0].ID || d.Name != "alpha" || d.State != MergeStatePending || d.CommitsAhead != 2 || d.WouldAlert {
		t.Fatalf("first decision = %+v, want alpha pending, 2 ahead, would_alert false", d)
	}

	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	got = th.h.c.MergeShadowDecisions(0)
	if len(got) != 2 || !got[0].WouldAlert || got[1].WouldAlert {
		t.Fatalf("shadow decisions = %+v, want a newer would_alert=true record over the first", got)
	}

	// further identical passes only advance the newest record
	th.h.c.triagePass()
	got = th.h.c.MergeShadowDecisions(0)
	if len(got) != 2 || got[0].RepeatCount != 2 {
		t.Fatalf("shadow decisions = %+v, want 2 records and the newest repeated twice", got)
	}

	th.assertNoAlerts(t, "")
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want none in shadow mode", rec)
	}
}

func TestMergePendingPass_AlertOncePerEpisode(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)

	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 1, mergeAlertPrefix)
	for i := 0; i < 3; i++ {
		th.advance(10 * time.Minute)
		th.h.c.triagePass() // a refresh never re-alerts
	}
	th.assertAlerts(t, 1, mergeAlertPrefix)

	// the branch lands: the episode ends and the record goes
	git.setMerged("branch-1", "base-2")
	th.h.c.triagePass()
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want it cleared once merged", rec)
	}

	// a new batch of commits is a new episode
	git.mu.Lock()
	git.baseTip = "base-3"
	git.mu.Unlock()
	git.setPending("branch-2")
	th.h.c.triagePass()
	th.assertAlerts(t, 1, mergeAlertPrefix)
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 2, mergeAlertPrefix)
}

func TestMergePendingPass_ThresholdZeroNoAlert(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 0)

	for i := 0; i < 4; i++ {
		th.h.c.triagePass()
		th.advance(time.Hour)
	}
	if rec := th.record(t, 0); rec == nil || rec.Status != TriageStatusMergePending || rec.AlertedAt != nil {
		t.Fatalf("record = %+v, want a merge_pending record that was never alerted", rec)
	}
	th.assertNoAlerts(t, "")
}

func TestMergePendingPass_CacheReusedWhenTipsUnchanged(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)

	th.h.c.triagePass()
	first := git.total()
	if first == 0 {
		t.Fatal("the first pass ran no git comparison")
	}
	for i := 0; i < 4; i++ {
		th.advance(time.Minute)
		th.h.c.triagePass()
	}
	if n := git.total(); n != first {
		t.Fatalf("git ran %d commands over 5 passes with unchanged tips, want %d (all from the first pass)", n, first)
	}
	if n := git.count("rev-parse"); n != 0 {
		t.Fatalf("rev-parse ran %d times, want none while the ref files answer", n)
	}

	git.mu.Lock()
	git.branchTip = "branch-2"
	git.mu.Unlock()
	th.h.c.triagePass()
	if n := git.count("rev-list"); n != 2 {
		t.Fatalf("rev-list ran %d times after the branch tip moved, want 2", n)
	}

	git.mu.Lock()
	git.baseTip = "base-2"
	git.mu.Unlock()
	th.h.c.triagePass()
	if n := git.count("rev-list"); n != 3 {
		t.Fatalf("rev-list ran %d times after the main tip moved, want 3", n)
	}
}

func TestMergePendingPass_GitErrorIsUnknownNoAlert(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	git.tipsUnreadable = true // tips come from git rev-parse
	git.failOnVerb = "rev-parse"

	for i := 0; i < 3; i++ {
		th.h.c.triagePass()
		th.advance(10 * time.Minute)
	}
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want none while the state is unknown", rec)
	}
	th.assertNoAlerts(t, "")

	// an unknown state also leaves an existing record alone
	git.failOnVerb = ""
	th.h.c.triagePass()
	before := th.record(t, 0)
	if before == nil {
		t.Fatal("no record once git answered")
	}
	git.failOnVerb = "rev-parse"
	th.advance(time.Hour)
	th.h.c.triagePass()
	after := th.record(t, 0)
	if after == nil || after.AlertedAt != nil || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("record after an unknown pass = %+v, want it unchanged from %+v", after, before)
	}
	th.assertNoAlerts(t, "")

	// a failure after the tips resolved is unknown too, and is not cached
	git.tipsUnreadable = false
	git.failOnVerb = "rev-list"
	git.mu.Lock()
	git.branchTip = "branch-9"
	git.mu.Unlock()
	th.h.c.triagePass()
	th.assertNoAlerts(t, "")
	git.failOnVerb = ""
	th.h.c.triagePass()
	if n := git.count("rev-list"); n != 3 {
		t.Fatalf("rev-list ran %d times, want the failed comparison retried (3)", n)
	}
}

func TestMergePendingPass_ClearsWhenMerged(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)

	th.h.c.triagePass()
	if th.record(t, 0) == nil {
		t.Fatal("no record while pending")
	}
	git.setMerged("branch-1", "base-2")
	th.h.c.triagePass()
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want it cleared once merged", rec)
	}
}

func TestMergePendingPass_ModeOffNeverRunsGit(t *testing.T) {
	for _, mode := range []string{config.MergePendingModeOff, ""} {
		th, git := newMergePendingHarness(t, mode, 10)
		// a leftover record on a completed Automaton is still cleared, as before
		if err := th.h.m.SaveTriageRecord(th.h.prds[0].ID, &TriageRecord{Status: TriageStatusMergePending, FirstSeenAt: th.clock}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			th.h.c.triagePass()
			th.advance(time.Hour)
		}
		if n := git.total(); n != 0 {
			t.Fatalf("mode %q ran %d git commands, want none", mode, n)
		}
		if rec := th.record(t, 0); rec != nil {
			t.Fatalf("mode %q left record %+v, want it cleared as before", mode, rec)
		}
		th.assertNoAlerts(t, "")
		if got := th.h.c.MergeShadowDecisions(0); len(got) != 0 {
			t.Fatalf("mode %q recorded shadow decisions %+v", mode, got)
		}
	}
}

func TestMergePendingPass_ShadowUnchangedPollsDoNotRewriteFile(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeShadow, 0)
	path := filepath.Join(th.h.m.store.dir, mergeShadowRecordsFile)

	th.h.c.triagePass()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shadow decision file after the first pass: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		th.advance(time.Minute)
		th.h.c.triagePass()
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("shadow decision file was rewritten by an unchanged poll (stat err = %v)", err)
	}
	got := th.h.c.MergeShadowDecisions(0)
	if len(got) != 1 || got[0].RepeatCount != 4 {
		t.Fatalf("shadow decisions = %+v, want one record repeated 4 times in memory", got)
	}
}

func TestMergePendingPass_ClearOnlyRemovesMergePendingRecord(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	id := th.h.prds[0].ID

	if err := th.h.m.SaveTriageRecord(id, &TriageRecord{Status: "failed", FirstSeenAt: th.clock}); err != nil {
		t.Fatal(err)
	}
	prd, _ := th.h.m.store.GetPRD(id)
	th.h.c.mergePendingClear(prd)
	if rec := th.record(t, 0); rec == nil || rec.Status != "failed" {
		t.Fatalf("record = %+v, want the failed record left alone", rec)
	}

	if err := th.h.m.SaveTriageRecord(id, &TriageRecord{Status: TriageStatusMergePending, FirstSeenAt: th.clock}); err != nil {
		t.Fatal(err)
	}
	prd, _ = th.h.m.store.GetPRD(id)
	th.h.c.mergePendingClear(prd)
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want the merge_pending record cleared", rec)
	}
}

func TestMergePendingPass_LeftoverFailedRecordOnCompletedAutomatonStillCleared(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	id := th.h.prds[0].ID
	old := th.clock.Add(-24 * time.Hour)

	// the branch already landed: the old failed record goes, as before
	git.setMerged("branch-1", "base-2")
	if err := th.h.m.SaveTriageRecord(id, &TriageRecord{Status: "failed", FirstSeenAt: old}); err != nil {
		t.Fatal(err)
	}
	th.h.c.triagePass()
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want the leftover failed record cleared", rec)
	}

	// the branch is pending: the merge_pending record starts its own episode
	git.setPending("branch-2")
	if err := th.h.m.SaveTriageRecord(id, &TriageRecord{Status: "failed", FirstSeenAt: old}); err != nil {
		t.Fatal(err)
	}
	th.h.c.triagePass()
	rec := th.record(t, 0)
	if rec == nil || rec.Status != TriageStatusMergePending || !rec.FirstSeenAt.After(old) {
		t.Fatalf("record = %+v, want a fresh merge_pending record that did not inherit the failed one's first-seen time", rec)
	}
}

func TestMergePendingPass_TipsUnreadableFallsBackToGit(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	git.tipsUnreadable = true
	for i := 0; i < 3; i++ {
		th.h.c.triagePass()
	}
	if n := git.count("rev-parse"); n != 3 {
		t.Fatalf("rev-parse ran %d times, want one per pass (3) when the ref files cannot answer", n)
	}
	if n := git.count("rev-list"); n != 1 {
		t.Fatalf("rev-list ran %d times, want 1 (tips never moved)", n)
	}
}

// blockingMergeGit blocks every call until released and reports when the first
// call has started.
type blockingMergeGit struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingMergeGit) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return "", errors.New("blocked")
}

func TestMergePendingPass_SlowGitDoesNotBlockShadowList(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeShadow, 10)
	b := &blockingMergeGit{started: make(chan struct{}), release: make(chan struct{})}
	th.h.c.mp.git = b
	th.h.c.mp.refTips = func(_, _, _ string) (string, string, bool) { return "tip-a", "tip-b", true }

	done := make(chan struct{})
	go func() {
		th.h.c.mergeCompare("/repos/base", mergeTestBranch)
		close(done)
	}()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("git never started")
	}

	listed := make(chan struct{})
	go func() {
		th.h.c.MergeShadowDecisions(0)
		close(listed)
	}()
	select {
	case <-listed:
	case <-time.After(5 * time.Second):
		t.Fatal("MergeShadowDecisions blocked behind a running git comparison")
	}
	close(b.release)
	<-done
}

func TestMergeGitRunner_HungGitIsKilledAtDeadline(t *testing.T) {
	dir := t.TempDir()
	// a git that hangs and leaves a child holding its output open
	script := "#!/bin/sh\nsleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil { // #nosec G306 -- test stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewMergeGitRunner().Run(ctx, t.TempDir(), "rev-list", "--count", "main..x")
	if err == nil {
		t.Fatal("hung git returned no error")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("hung git took %s to stop, want it cut off near the deadline", took)
	}
}

func TestReadBranchTips_LooseAndPackedRefs(t *testing.T) {
	repo := gitFixture(t)
	gitT(t, repo, "branch", "automaton/alpha-work")
	want := strings.TrimSpace(gitT(t, repo, "rev-parse", "main"))

	bt, mt, ok := readBranchTips(repo, "automaton/alpha-work", "main")
	if !ok || bt != want || mt != want {
		t.Fatalf("loose refs: got %q %q ok=%v, want %q for both", bt, mt, ok, want)
	}

	writeFileT(t, repo, "more.txt", "more\n")
	gitT(t, repo, "checkout", "-q", "automaton/alpha-work")
	gitT(t, repo, "add", "more.txt")
	gitT(t, repo, "commit", "-m", "Automaton second commit")
	gitT(t, repo, "checkout", "-q", "main")
	branchWant := strings.TrimSpace(gitT(t, repo, "rev-parse", "automaton/alpha-work"))
	gitT(t, repo, "pack-refs", "--all")

	bt, mt, ok = readBranchTips(repo, "automaton/alpha-work", "main")
	if !ok || bt != branchWant || mt != want {
		t.Fatalf("packed refs: got %q %q ok=%v, want %q and %q", bt, mt, ok, branchWant, want)
	}

	if _, _, ok := readBranchTips(repo, "automaton/missing", "main"); ok {
		t.Fatal("a missing branch resolved")
	}
	if _, _, ok := readBranchTips(repo, "../escape", "main"); ok {
		t.Fatal("a ref name with .. resolved")
	}
}
