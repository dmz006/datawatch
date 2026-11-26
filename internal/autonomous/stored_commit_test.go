// A stored shipped-record commit is text someone sent. These tests cover
// every place it reaches git.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// victim is a file a stored "--output=" value would overwrite.
func victim(t *testing.T) (path, value string, intact func(when string)) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(path, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, "--output=" + path, func(when string) {
		t.Helper()
		if b, err := os.ReadFile(path); err != nil || string(b) != "keep me\n" {
			t.Fatalf("%s: the file was changed: %q %v", when, b, err)
		}
	}
}

// storeShipped puts a shipped record straight on the stored Automaton, as a
// record written before values were checked.
func (r *meRig) storeShipped(p *PRD, commit string, mergedAt time.Time) {
	r.t.Helper()
	setPRDShipped(p, "v1.0.1", mergedAt, commit, ShippedSourceBackfill)
	r.save(p)
}

func (r *meRig) storedCommit(id string) string {
	r.t.Helper()
	p, _ := r.m.store.GetPRD(id)
	return p.MergeCommit
}

// hexBranches makes branches whose names look like commit ids: git would
// read such a name as the branch.
func (r *meRig) hexBranches() []string {
	r.t.Helper()
	names := []string{"deadbeef", "cafe1234cafe"}
	for _, n := range names {
		gitRun(r.t, r.repo, "branch", n, "main")
	}
	return names
}

func TestCompletedMigrationCommitTimeTakesOnlyAFullID(t *testing.T) {
	r := newMERig(t)
	_, value, intact := victim(t)
	g := &meGit{}
	for _, v := range []string{value, "HEAD", "main", r.rev("HEAD")[:12], "", r.rev("HEAD") + "\n--output=x"} {
		if _, err := completedMigrationCommitTime(context.Background(), g, r.repo, v); err == nil {
			t.Errorf("%q: accepted", v)
		}
		intact(v)
	}
	if len(g.calls) != 0 {
		t.Fatalf("git ran: %q", g.calls)
	}
	if at, err := completedMigrationCommitTime(context.Background(), g, r.repo, r.rev("HEAD")); err != nil || at.IsZero() {
		t.Fatalf("a full id: %v %v", at, err)
	}
	// A stored value is read with the option terminators.
	if at, err := storedCommitTime(context.Background(), g, r.repo, r.rev("HEAD")); err != nil || at.IsZero() {
		t.Fatalf("a stored full id: %v %v", at, err)
	}
	last := g.calls[len(g.calls)-1]
	if n := len(last); last[0] != "show" || last[n-3] != "--end-of-options" || last[n-1] != "--" {
		t.Fatalf("git show of a stored commit has no option terminators: %q", last)
	}
}

// The migration reads the commit time of a shipped record with no merge time,
// on a dry run too.
func TestCompletedMigrationNeverGivesGitAStoredNonCommit(t *testing.T) {
	r := newMERig(t)
	r.hexBranches()
	path, value, intact := victim(t)
	fresh := filepath.Join(filepath.Dir(path), "created")
	for _, v := range []string{value, "--output=" + fresh, "HEAD", "main", "origin/main", "HEAD~1", "deadbeef", "cafe1234cafe"} {
		t.Run(strings.ReplaceAll(v, "/", "_"), func(t *testing.T) {
			p, _ := r.automaton(PRDCancelled)
			r.storeShipped(p, v, time.Time{})
			for _, dry := range []bool{true, false} {
				rep, err := r.m.MigrateCompleted(context.Background(), CompletedMigrationOpts{ID: p.ID, DryRun: dry})
				if err != nil {
					t.Fatal(err)
				}
				intact(v)
				if _, err := os.Stat(fresh); err == nil {
					t.Fatal("a file was created")
				}
				if rep.Changed != 0 || len(rep.WouldComplete) != 0 || !strings.Contains(strings.Join(rep.Errors, ";"), "stored shipped-record commit") {
					t.Fatalf("dry=%v: report = %+v, want an error naming the stored commit and no change", dry, rep)
				}
				got, _ := r.m.store.GetPRD(p.ID)
				if got.Status != PRDCancelled || got.MergeCommit != v || got.MergedAt != nil {
					t.Fatalf("dry=%v: the record changed: %s %q %v", dry, got.Status, got.MergeCommit, got.MergedAt)
				}
			}
		})
	}
}

// Stored full and abbreviated ids keep working.
func TestCompletedMigrationStillReadsTheTimeOfAStoredCommit(t *testing.T) {
	r := newMERig(t)
	for _, n := range []int{40, 12, 9} {
		p, _ := r.automaton(PRDCancelled)
		commit := r.pushed(p.Git.Branch)
		r.storeShipped(p, commit[:n], time.Time{})
		rep, err := r.m.MigrateCompleted(context.Background(), CompletedMigrationOpts{ID: p.ID})
		if err != nil || rep.Changed != 1 {
			t.Fatalf("%d characters: report = %+v %v", n, rep, err)
		}
		if got := r.storedCommit(p.ID); got != commit[:n] {
			t.Fatalf("the stored value was rewritten: %q", got)
		}
	}
}

func TestCompletedMigrationNeverGivesGitAStoredAutomationNonCommit(t *testing.T) {
	r := newMERig(t)
	_, value, intact := victim(t)
	p, _ := r.automaton(PRDCompleted)
	a, err := r.m.CreateAutomation(AutomationInput{Name: "chain", Members: []AutomationMember{{PRDID: p.ID}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationCancelled
		setAutomationShipped(a, "v1.0.1", time.Time{}, value, ShippedSourceMergeStep)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		rep, err := r.m.MigrateCompleted(context.Background(), CompletedMigrationOpts{ID: a.ID, DryRun: dry})
		if err != nil {
			t.Fatal(err)
		}
		intact("automation")
		if rep.Changed != 0 || !strings.Contains(strings.Join(rep.Errors, ";"), "stored shipped-record commit") {
			t.Fatalf("dry=%v: report = %+v", dry, rep)
		}
		if got, _ := r.m.automations.Get(a.ID); got.MergeCommit != value || got.Status != AutomationCancelled {
			t.Fatalf("the record changed: %s %q", got.Status, got.MergeCommit)
		}
	}
}

// A restart finishes an Automaton whose recorded merge commit is in main. A
// name such as HEAD or main is always "in main".
func TestMergedBranchFinishNeedsAStoredCommitID(t *testing.T) {
	r := newMERig(t)
	r.hexBranches()
	_, value, intact := victim(t)
	run := func(commit string) bool {
		t.Helper()
		p, _ := r.automaton(PRDRunning)
		p.ProjectDir = r.repo
		r.storeShipped(p, commit, time.Now())
		done := r.m.finishIfMerged(p)
		intact(commit)
		got, _ := r.m.store.GetPRD(p.ID)
		if got.MergeCommit != commit || (got.Status == PRDCompleted) != done {
			t.Fatalf("%q: done=%v status=%s stored=%q", commit, done, got.Status, got.MergeCommit)
		}
		return done
	}
	for _, v := range []string{"HEAD", "main", "origin/main", "refs/heads/main", "HEAD~1", value, "deadbeef", "cafe1234cafe", "0123456789ab"} {
		if run(v) {
			t.Errorf("%q: finished the Automaton", v)
		}
	}
	head := r.rev("main")
	for _, v := range []string{head, head[:9], strings.ToUpper(head[:12])} {
		if !run(v) {
			t.Errorf("%q: a commit in main did not finish the Automaton", v)
		}
	}
}

// The unlanded-work list takes a shipped record on the default branch as
// proof of delivery.
func TestDeliveredOnBaseNeedsAStoredCommitID(t *testing.T) {
	r := newMERig(t)
	r.hexBranches()
	p, _ := r.automaton(PRDCompleted) // its branch holds a file main lacks
	g := &meGit{}
	proven := func(commit string) bool {
		r.storeShipped(p, commit, time.Now())
		_, ok := deliveredOnBase(context.Background(), g, r.repo, "main", p.Git.Branch, p)
		return ok
	}
	for _, v := range []string{"deadbeef", "cafe1234cafe", "HEAD", "--output=x", "0123456789ab"} {
		if proven(v) {
			t.Errorf("%q: taken as proof of delivery", v)
		}
	}
	head := r.rev("main")
	for _, v := range []string{head, head[:9]} {
		g.calls = nil
		if !proven(v) {
			t.Errorf("%q: a commit on main is not taken as proof", v)
		}
		asked := false
		for _, c := range g.calls {
			if c[0] == "merge-base" {
				asked = true
				if len(c) != 5 || c[2] != "--end-of-options" || c[3] != head {
					t.Errorf("merge-base = %q, want the option terminator and the full id", c)
				}
			}
		}
		if !asked {
			t.Error("merge-base did not run")
		}
	}
}

// No source may write a value git would read as an option.
func TestShippedRecordRefusesAnOptionLikeCommitForEverySource(t *testing.T) {
	_, m, prd, auto := newShippedFixture(t)
	for _, source := range []string{ShippedSourceBackfill, ShippedSourceMergeStep} {
		for _, v := range []string{"--output=/tmp/x", " -n1", "-"} {
			if _, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, v, source); err == nil || !strings.Contains(err.Error(), "option") {
				t.Errorf("Automaton %s %q: err = %v", source, v, err)
			}
			if _, err := m.RecordAutomationShipped(auto.ID, "v1.2.0", shippedTestTime, v, source); err == nil || !strings.Contains(err.Error(), "option") {
				t.Errorf("Automation %s %q: err = %v", source, v, err)
			}
		}
	}
	if p, _ := m.store.GetPRD(prd.ID); p.IsShipped() {
		t.Errorf("stored: %q", p.MergeCommit)
	}
	if a, _ := m.automations.Get(auto.ID); a.IsShipped() {
		t.Errorf("stored: %q", a.MergeCommit)
	}
	// What is stored today stays accepted: full and abbreviated ids.
	for _, v := range []string{strings.Repeat("ab", 20), "abc123456", "abc123456789"} {
		if _, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, v, ShippedSourceBackfill); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
}

// The history backfill copies a member's stored commit onto its Automation.
// A bad stored value is reported; nothing crashes or is rewritten.
func TestShippedBackfillReportsAStoredOptionLikeMemberCommit(t *testing.T) {
	_, m, prd, auto := newShippedFixture(t)
	at := shippedTestTime
	setPRDShipped(prd, "v1.2.0", at, "--output=x", ShippedSourceBackfill)
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	var rep ShippedBackfillReport
	m.backfillAutomationsShipped(ShippedBackfillOpts{}, &rep, map[string]bool{}, map[string]int{prd.ID: 0})
	if rep.AutomationsMarked != 0 || len(rep.Errors) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if a, _ := m.automations.Get(auto.ID); a.IsShipped() {
		t.Fatalf("copied onto the Automation: %q", a.MergeCommit)
	}
	if p, _ := m.store.GetPRD(prd.ID); p.MergeCommit != "--output=x" {
		t.Fatalf("the member was rewritten: %q", p.MergeCommit)
	}
}
