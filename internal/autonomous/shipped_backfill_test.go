package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// TestShippedBackfillMarkedAndCancelledUntouched builds a throwaway repo
// with merge commits naming automaton branches (both merge-subject
// forms), a version bump named in the subject, a CHANGELOG-heading
// release written by a version-less commit, one unreleased merge, one id
// with no PRD in the store, and one cancelled-and-unmerged PRD. The pass
// must mark exactly the three merged PRDs with the right releases, mark
// the all-merged Automation off the latest member merge, leave the
// Automation with a cancelled member and the cancelled PRD untouched
// (statuses never rewritten), and report the unmatched id — and a second
// pass must only count them as already marked.
func TestShippedBackfillMarkedAndCancelledUntouched(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	// The store keeps its own copies: capture the pre-run statuses now, and
	// re-read the records after the pass to assert on the stored state.
	autoOKBefore := getStoredAutomation(t, fx.m, fx.autoOK.ID).Status
	autoBlockedBefore := getStoredAutomation(t, fx.m, fx.autoBlocked.ID).Status

	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("BackfillShipped: %v", err)
	}
	if rep.Errors != nil {
		t.Fatalf("rep.Errors = %v, want empty", rep.Errors)
	}
	if rep.PrdsScanned != 4 {
		t.Errorf("PrdsScanned = %d, want 4", rep.PrdsScanned)
	}
	if rep.PrdsMarked != 3 {
		t.Errorf("PrdsMarked = %d, want 3", rep.PrdsMarked)
	}
	// The fixture's cancelled PRD has no merge, so this pass marks no
	// cancelled PRD.
	if rep.PrdsWouldMarkCancelled != 0 {
		t.Errorf("PrdsWouldMarkCancelled = %d, want 0", rep.PrdsWouldMarkCancelled)
	}
	if rep.PrdsAlreadyMarked != 0 {
		t.Errorf("PrdsAlreadyMarked = %d, want 0", rep.PrdsAlreadyMarked)
	}
	if rep.AutomationsMarked != 1 {
		t.Errorf("AutomationsMarked = %d, want 1", rep.AutomationsMarked)
	}
	if rep.LeftCancelled != 1 {
		t.Errorf("LeftCancelled = %d, want 1", rep.LeftCancelled)
	}
	if len(rep.UnmatchedIDs) != 1 || rep.UnmatchedIDs[0] != "deadbeef" {
		t.Errorf("UnmatchedIDs = %v, want [deadbeef]", rep.UnmatchedIDs)
	}

	want := []struct {
		p           *PRD
		release     string
		mergeCommit string
	}{
		{fx.released, "v9.8.0", fx.sha["released"]},
		{fx.notes, "v9.9.0", fx.sha["notes"]},
		{fx.unreleased, "", fx.sha["unreleased"]},
	}
	for _, w := range want {
		if !w.p.IsShipped() {
			t.Errorf("PRD %s: want shipped", w.p.ID)
			continue
		}
		if w.p.MergedInRelease != w.release {
			t.Errorf("PRD %s: MergedInRelease = %q, want %q", w.p.ID, w.p.MergedInRelease, w.release)
		}
		if w.p.MergeCommit != w.mergeCommit {
			t.Errorf("PRD %s: MergeCommit = %q, want %q", w.p.ID, w.p.MergeCommit, w.mergeCommit)
		}
		if w.p.ShippedSource != ShippedSourceBackfill {
			t.Errorf("PRD %s: ShippedSource = %q, want backfill", w.p.ID, w.p.ShippedSource)
		}
	}
	if fx.unreleased.MergedAt == nil {
		t.Error("unreleased PRD: MergedAt = nil, want the merge time")
	}

	// The cancelled PRD stays cancelled and unmarked; its status was
	// never rewritten.
	if fx.cancelled.Status != PRDCancelled || fx.cancelled.IsShipped() || fx.cancelled.MergeCommit != "" {
		t.Errorf("cancelled PRD changed: status=%s shipped=%v commit=%q",
			fx.cancelled.Status, fx.cancelled.IsShipped(), fx.cancelled.MergeCommit)
	}

	// The all-merged Automation is marked off the latest member merge
	// (notes: v9.9.0); its status was never rewritten.
	fx.autoOK = getStoredAutomation(t, fx.m, fx.autoOK.ID)
	fx.autoBlocked = getStoredAutomation(t, fx.m, fx.autoBlocked.ID)
	if !fx.autoOK.IsShipped() {
		t.Fatalf("autoOK: want shipped")
	}
	if fx.autoOK.MergedInRelease != "v9.9.0" || fx.autoOK.MergeCommit != fx.sha["notes"] {
		t.Errorf("autoOK: release=%q commit=%q, want v9.9.0 / notes merge %q",
			fx.autoOK.MergedInRelease, fx.autoOK.MergeCommit, fx.sha["notes"])
	}
	if fx.autoOK.Status != autoOKBefore {
		t.Errorf("autoOK status changed: %s", fx.autoOK.Status)
	}
	if fx.autoOK.ShippedSource != ShippedSourceBackfill {
		t.Errorf("autoOK: ShippedSource = %q, want backfill", fx.autoOK.ShippedSource)
	}
	// An Automation with a non-skipped member left cancelled is not
	// marked until that operator decision is resolved.
	if fx.autoBlocked.IsShipped() || fx.autoBlocked.Status != autoBlockedBefore {
		t.Errorf("autoBlocked changed: shipped=%v status=%s", fx.autoBlocked.IsShipped(), fx.autoBlocked.Status)
	}

	// A second pass marks nothing new.
	rep2, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("second BackfillShipped: %v", err)
	}
	if rep2.PrdsMarked != 0 || rep2.AutomationsMarked != 0 {
		t.Errorf("second pass marked %d prds, %d automations, want 0/0", rep2.PrdsMarked, rep2.AutomationsMarked)
	}
	if rep2.PrdsWouldMarkCancelled != 0 {
		t.Errorf("second pass PrdsWouldMarkCancelled = %d, want 0", rep2.PrdsWouldMarkCancelled)
	}
	if rep2.PrdsAlreadyMarked != 3 {
		t.Errorf("second pass PrdsAlreadyMarked = %d, want 3", rep2.PrdsAlreadyMarked)
	}
	if rep2.LeftCancelled != 1 || len(rep2.UnmatchedIDs) != 1 || rep2.UnmatchedIDs[0] != "deadbeef" {
		t.Errorf("second pass LeftCancelled=%d Unmatched=%v, want 1/[deadbeef]", rep2.LeftCancelled, rep2.UnmatchedIDs)
	}
}

// TestShippedBackfillDryRunMakesNoWrites runs the same history in DryRun
// mode: the report accounts for the work but the store is untouched.
func TestShippedBackfillDryRunMakesNoWrites(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{DryRun: true})
	if err != nil {
		t.Fatalf("BackfillShipped dry: %v", err)
	}
	if rep.Errors != nil {
		t.Fatalf("rep.Errors = %v, want empty", rep.Errors)
	}
	if rep.PrdsMarked != 3 {
		t.Errorf("dry run PrdsMarked = %d, want 3 (the PRDs it would mark)", rep.PrdsMarked)
	}
	if rep.AutomationsMarked != 0 {
		t.Errorf("dry run wrote %d automations, want 0", rep.AutomationsMarked)
	}
	if rep.PrdsWouldMarkCancelled != 0 {
		t.Errorf("dry run PrdsWouldMarkCancelled = %d, want 0", rep.PrdsWouldMarkCancelled)
	}
	if rep.PrdsAlreadyMarked != 0 || rep.LeftCancelled != 1 {
		t.Errorf("dry run report wrong: %+v", rep)
	}
	if len(rep.UnmatchedIDs) != 1 || rep.UnmatchedIDs[0] != "deadbeef" {
		t.Errorf("dry run UnmatchedIDs = %v, want [deadbeef]", rep.UnmatchedIDs)
	}
	for _, p := range []*PRD{fx.released, fx.notes, fx.unreleased} {
		if p.IsShipped() {
			t.Errorf("PRD %s: dry run must not write shipped state", p.ID)
		}
	}
	if getStoredAutomation(t, fx.m, fx.autoOK.ID).IsShipped() || getStoredAutomation(t, fx.m, fx.autoBlocked.ID).IsShipped() {
		t.Error("dry run changed an Automation")
	}
}

// TestShippedBackfillHonoursMainBranch scans the branch named by the
// MAIN_BRANCH environment variable instead of a hard-coded "main".
func TestShippedBackfillHonoursMainBranch(t *testing.T) {
	t.Setenv("MAIN_BRANCH", "release")
	fx := newShippedBackfillFixture(t, "release")
	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("BackfillShipped: %v", err)
	}
	if rep.PrdsMarked != 3 || rep.AutomationsMarked != 1 {
		t.Fatalf("MAIN_BRANCH=release: marked %d prds, %d automations, want 3/1: %+v",
			rep.PrdsMarked, rep.AutomationsMarked, rep)
	}
	if fx.released.MergedInRelease != "v9.8.0" || fx.unreleased.MergedInRelease != "" {
		t.Errorf("release attribution on the non-main branch: released=%q unreleased=%q",
			fx.released.MergedInRelease, fx.unreleased.MergedInRelease)
	}
}

// newShippedBackfillFixture builds a Manager holding four PRDs (three
// that get merged, one cancelled) and two Automations (one all merged,
// one with a cancelled member), plus a throwaway git repo on the branch
// named branch whose history is, in order:
//
//	init
//	merge automaton/deadbeef             (no such PRD in the store)
//	merge branch automaton/<released>    (subject form two)
//	commit "v9.8.0"                      → released's release (subject)
//	merge automaton/<notes>              (subject form one)
//	commit "write release notes"         → notes' release, read from the
//	                                       CHANGELOG heading v9.9.0 it
//	                                       writes
//	merge branch automaton/<unreleased>  → no release after it: empty
//	                                       merged_in_release
//
// HOME is pointed at a temp dir and identity lives only in the repo
// config, so the test never reads or writes the operator's real git
// configuration, repository, daemon or tmux sessions.
func newShippedBackfillFixture(t *testing.T, branch string) *backfillFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo := t.TempDir()
	fx := &backfillFixture{sha: map[string]string{}}
	fx.repo = repo
	g := func(args ...string) string {
		t.Helper()
		out, err := git.Run(context.Background(), repo, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	file := func(name, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(name, content, subject string) {
		file(name, content)
		g("add", "-A")
		g("commit", "-m", subject)
	}
	mergePRD := func(label, prdID, subject string) {
		g("checkout", "-b", "automaton/"+prdID)
		commit(label+".txt", label+" work\n", "work on "+label)
		g("checkout", branch)
		g("merge", "--no-ff", "automaton/"+prdID, "-m", subject)
		fx.sha[label] = strings.TrimSpace(g("rev-parse", "HEAD"))
	}

	g("init", "-b", branch)
	g("config", "user.email", "backfill-test@example.com")
	g("config", "user.name", "Backfill Test")
	commit("init.txt", "init\n", "init")

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = repo
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fx.m = m

	memberPRDs := makeAutomationMemberPRDs(t, m, 3)
	released, notes, unreleased := memberPRDs[0], memberPRDs[1], memberPRDs[2]
	fx.released, fx.notes, fx.unreleased = released, notes, unreleased
	cancelled, err := m.store.CreatePRD("will be cancelled", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	cancelled.Status = PRDCancelled
	if err := m.store.SavePRD(cancelled); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	fx.cancelled = cancelled
	in1, in2 := AutomationInput{
		Name:    "all merged",
		Members: []AutomationMember{{PRDID: released.ID}, {PRDID: notes.ID}},
	}, AutomationInput{
		Name:    "only a cancelled member",
		Members: []AutomationMember{{PRDID: cancelled.ID}},
	}
	fx.autoOK = mustCreateAutomation(t, m, in1)
	fx.autoBlocked = mustCreateAutomation(t, m, in2)

	// History on the branch, in the order documented above.
	mergePRD("unmatched", "deadbeef", "Merge automaton/deadbeef")
	mergePRD("released", released.ID, "Merge branch automaton/"+released.ID)
	commit("CHANGELOG.md",
		"# Changelog\n\n## v9.8.0 — bump\n\n- shipped the released PRD\n",
		"v9.8.0")
	mergePRD("notes", notes.ID, "Merge automaton/"+notes.ID)
	commit("CHANGELOG.md",
		"# Changelog\n\n## v9.9.0 — notes release\n\n- shipped the notes PRD\n\n## v9.8.0 — bump\n\n- shipped the released PRD\n",
		"write release notes")
	mergePRD("unreleased", unreleased.ID, "Merge branch automaton/"+unreleased.ID)

	return fx
}

func mustCreateAutomation(t *testing.T, m *Manager, in AutomationInput) *Automation {
	t.Helper()
	a, err := m.CreateAutomation(in, "tester")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	return a
}

// getStoredAutomation reads the automation back from the store: the store
// owns the record it saved, and the manager may mutate that stored copy
// rather than a pointer handed out earlier.
func getStoredAutomation(t *testing.T, m *Manager, id string) *Automation {
	t.Helper()
	a, ok := m.GetAutomation(id)
	if !ok {
		t.Fatalf("store lost automation %s", id)
	}
	return a
}

// TestShippedBackfillMergedThenCancelledIsMarked covers the case the
// fixture's own cancelled PRD does not hit: a PRD that main *did* merge
// and that was subsequently cancelled by the operator. The pass must
// record it shipped with source backfill and its merge commit — and
// leave its Status cancelled — and an Automation whose only non-skipped
// member is that PRD gets marked off the member's merge with its status
// untouched.
func TestShippedBackfillMergedThenCancelledIsMarked(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	// A PRD no Automation holds yet: main merges it in a commit whose
	// subject names its automaton branch, and then the operator
	// cancels it.
	p, err := fx.m.store.CreatePRD("merged then cancelled", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	only := mustCreateAutomation(t, fx.m, AutomationInput{
		Name:    "only the merged-then-cancelled member",
		Members: []AutomationMember{{PRDID: p.ID}},
	})
	autoBefore := getStoredAutomation(t, fx.m, only.ID).Status

	g := func(args ...string) string {
		t.Helper()
		out, err := git.Run(context.Background(), fx.repo, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	g("checkout", "-q", "-b", "automaton/"+p.ID)
	if err := os.WriteFile(filepath.Join(fx.repo, p.ID+".txt"), []byte("late work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g("add", "-A")
	g("commit", "-m", "late work")
	g("checkout", "-q", "main")
	g("merge", "--no-ff", "automaton/"+p.ID, "-m", "Merge automaton/"+p.ID)
	mergeSha := strings.TrimSpace(g("rev-parse", "HEAD"))

	p.Status = PRDCancelled
	if err := fx.m.store.SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("BackfillShipped: %v", err)
	}
	if rep.Errors != nil {
		t.Fatalf("rep.Errors = %v, want empty", rep.Errors)
	}
	if rep.PrdsMarked != 4 {
		t.Errorf("PrdsMarked = %d, want 4 (three fixture PRDs plus the merged-then-cancelled one)", rep.PrdsMarked)
	}
	// Exactly one cancelled PRD was marked: p, the one a merge names. The
	// fixture's other cancelled PRD has no merge and stays unmarked.
	if rep.PrdsWouldMarkCancelled != 1 {
		t.Errorf("PrdsWouldMarkCancelled = %d, want 1", rep.PrdsWouldMarkCancelled)
	}
	if rep.LeftCancelled != 1 {
		t.Errorf("LeftCancelled = %d, want 1", rep.LeftCancelled)
	}
	// autoOK (released+notes) and `only` (just p) are both fully merged.
	if rep.AutomationsMarked != 2 {
		t.Errorf("AutomationsMarked = %d, want 2", rep.AutomationsMarked)
	}

	// The merged-then-cancelled PRD: marked shipped, recorded with the
	// backfill source and its merge commit, Status still cancelled.
	stored, ok := fx.m.store.GetPRD(p.ID)
	if !ok {
		t.Fatalf("store lost PRD %s", p.ID)
	}
	if stored.Status != PRDCancelled {
		t.Errorf("PRD status = %s, want cancelled (never rewritten)", stored.Status)
	}
	if !stored.IsShipped() {
		t.Errorf("PRD: want shipped")
	}
	if stored.ShippedSource != ShippedSourceBackfill {
		t.Errorf("PRD ShippedSource = %q, want backfill", stored.ShippedSource)
	}
	if stored.MergeCommit != mergeSha {
		t.Errorf("PRD MergeCommit = %q, want %q", stored.MergeCommit, mergeSha)
	}

	// The solo-member Automation is marked off the member's merge; its
	// status was never rewritten.
	onlyAfter := getStoredAutomation(t, fx.m, only.ID)
	if !onlyAfter.IsShipped() {
		t.Fatalf("Automation: want shipped")
	}
	if onlyAfter.Status != autoBefore {
		t.Errorf("Automation status = %s, want %s (never rewritten)", onlyAfter.Status, autoBefore)
	}
	if onlyAfter.MergeCommit != mergeSha {
		t.Errorf("Automation MergeCommit = %q, want the member merge %q",
			onlyAfter.MergeCommit, mergeSha)
	}
	if onlyAfter.ShippedSource != ShippedSourceBackfill {
		t.Errorf("Automation ShippedSource = %q, want backfill", onlyAfter.ShippedSource)
	}
}

// TestShippedBackfillCancelledWithoutMergeUntouched: a cancelled PRD no
// merge on main names is left completely alone by the pass — no shipped
// state, no status rewrite — and shows up in left_cancelled.
func TestShippedBackfillCancelledWithoutMergeUntouched(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	if fx.cancelled.Status != PRDCancelled || fx.cancelled.IsShipped() || fx.cancelled.MergeCommit != "" {
		t.Fatalf("fixture invariant: cancelled PRD starts %s shipped=%v commit=%q",
			fx.cancelled.Status, fx.cancelled.IsShipped(), fx.cancelled.MergeCommit)
	}

	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("BackfillShipped: %v", err)
	}
	if rep.Errors != nil {
		t.Fatalf("rep.Errors = %v, want empty", rep.Errors)
	}
	if rep.LeftCancelled != 1 {
		t.Errorf("LeftCancelled = %d, want 1 (the unmerged cancelled PRD)", rep.LeftCancelled)
	}
	if rep.PrdsWouldMarkCancelled != 0 {
		t.Errorf("PrdsWouldMarkCancelled = %d, want 0", rep.PrdsWouldMarkCancelled)
	}

	p, ok := fx.m.store.GetPRD(fx.cancelled.ID)
	if !ok {
		t.Fatalf("store lost PRD %s", fx.cancelled.ID)
	}
	if p.Status != PRDCancelled {
		t.Errorf("PRD status = %s, want cancelled (unchanged)", p.Status)
	}
	if p.IsShipped() {
		t.Errorf("PRD: must not be marked shipped (no merge names it)")
	}
	if p.MergeCommit != "" || p.MergedAt != nil {
		t.Errorf("PRD MergeCommit=%q MergedAt=%v, want both unset", p.MergeCommit, p.MergedAt)
	}
}

// TestShippedBackfillDryRunCountsWritesNothing: a dry run accounts for
// exactly what the real run then reports — including the cancelled-marked
// count — and leaves every PRD and Automation without shipped state.
func TestShippedBackfillDryRunCountsWritesNothing(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	// Give the store a cancelled PRD that the merge also names so the
	// would-mark-cancelled accounting has something to move.
	fx.released.Status = PRDCancelled
	if err := fx.m.store.SavePRD(fx.released); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	rep, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{DryRun: true})
	if err != nil {
		t.Fatalf("BackfillShipped dry: %v", err)
	}
	if rep.Errors != nil {
		t.Fatalf("rep.Errors = %v, want empty", rep.Errors)
	}
	t.Log(backfillReportLine("dry run", rep))
	if rep.PrdsMarked < 1 || rep.PrdsWouldMarkCancelled < 1 {
		t.Fatalf("dry run report thin: %+v, want at least one marked and one cancelled-marked", rep)
	}
	if rep.PrdsMarked != 3 {
		t.Errorf("dry run PrdsMarked = %d, want 3", rep.PrdsMarked)
	}
	if rep.PrdsWouldMarkCancelled != 1 {
		t.Errorf("dry run PrdsWouldMarkCancelled = %d, want 1", rep.PrdsWouldMarkCancelled)
	}
	// The dry run counts only PRDs: it never writes Automations.
	if rep.AutomationsMarked != 0 {
		t.Errorf("dry run AutomationsMarked = %d, want 0", rep.AutomationsMarked)
	}

	// Nothing carries shipped state afterwards — every PRD and every
	// Automation in the fixture.
	for _, p := range []*PRD{fx.released, fx.notes, fx.unreleased, fx.cancelled} {
		s, ok := fx.m.store.GetPRD(p.ID)
		if !ok {
			t.Fatalf("store lost PRD %s", p.ID)
		}
		if s.IsShipped() {
			t.Errorf("PRD %s: dry run must not write shipped state", s.ID)
		}
	}
	for _, id := range []string{fx.autoOK.ID, fx.autoBlocked.ID} {
		a := getStoredAutomation(t, fx.m, id)
		if a.IsShipped() {
			t.Errorf("automation %s: dry run must not write shipped state", a.ID)
		}
	}

	// The real run then reports the same two counts: the dry run wrote
	// nothing, so it marks the very same PRDs it predicted.
	real, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("BackfillShipped: %v", err)
	}
	t.Log(backfillReportLine("real run", real))
	if real.PrdsMarked != rep.PrdsMarked {
		t.Errorf("prds_marked: dry=%d real=%d, want equal", rep.PrdsMarked, real.PrdsMarked)
	}
	if real.PrdsWouldMarkCancelled != rep.PrdsWouldMarkCancelled {
		t.Errorf("prds_would_mark_cancelled: dry=%d real=%d, want equal",
			rep.PrdsWouldMarkCancelled, real.PrdsWouldMarkCancelled)
	}
}

// TestShippedBackfillSecondRunMarksNothing: a second real run finds
// nothing left to mark — zero fresh marks, zero cancelled-marks, and the
// first run's work accounted for as already marked.
func TestShippedBackfillSecondRunMarksNothing(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")
	// A cancelled PRD among the merged ones so the first run's
	// prds_would_mark_cancelled is non-zero and the second run has to
	// report zero, not just inherit it.
	fx.released.Status = PRDCancelled
	if err := fx.m.store.SavePRD(fx.released); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	first, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("first BackfillShipped: %v", err)
	}
	if first.PrdsMarked != 3 || first.PrdsWouldMarkCancelled != 1 || first.PrdsAlreadyMarked != 0 {
		t.Fatalf("first run report = %+v, want 3 marked / 1 cancelled / 0 already", first)
	}

	second, err := fx.m.BackfillShipped(context.Background(), ShippedBackfillOpts{})
	if err != nil {
		t.Fatalf("second BackfillShipped: %v", err)
	}
	if second.PrdsMarked != 0 {
		t.Errorf("second run PrdsMarked = %d, want 0", second.PrdsMarked)
	}
	if second.PrdsWouldMarkCancelled != 0 {
		t.Errorf("second run PrdsWouldMarkCancelled = %d, want 0", second.PrdsWouldMarkCancelled)
	}
	if second.PrdsAlreadyMarked != first.PrdsMarked {
		t.Errorf("second run PrdsAlreadyMarked = %d, want %d (the first run's marks)",
			second.PrdsAlreadyMarked, first.PrdsMarked)
	}
	if second.AutomationsMarked != 0 {
		t.Errorf("second run AutomationsMarked = %d, want 0", second.AutomationsMarked)
	}
	// The left-behind cancelled PRD and the unmatched id are reported
	// again: they are still unmarked, still unmatched.
	if second.LeftCancelled != 1 || len(second.UnmatchedIDs) != 1 || second.UnmatchedIDs[0] != "deadbeef" {
		t.Errorf("second run LeftCancelled=%d Unmatched=%v, want 1/[deadbeef]",
			second.LeftCancelled, second.UnmatchedIDs)
	}
}

// backfillReportLine renders the dry-run/real-run report as plain
// labelled counts (with the report's UnmatchedIDs reduced to a count) so
// the -v test log is a quotable source for the gate report.
func backfillReportLine(kind string, r ShippedBackfillReport) string {
	return fmt.Sprintf("%s: prds_scanned=%d prds_marked=%d prds_would_mark_cancelled=%d prds_already_marked=%d automations_marked=%d left_cancelled=%d unmatched_ids=%d",
		kind, r.PrdsScanned, r.PrdsMarked, r.PrdsWouldMarkCancelled, r.PrdsAlreadyMarked, r.AutomationsMarked, r.LeftCancelled, len(r.UnmatchedIDs))
}

type backfillFixture struct {
	repo                                   string
	m                                      *Manager
	released, notes, unreleased, cancelled *PRD
	autoOK, autoBlocked                    *Automation
	sha                                    map[string]string
}
