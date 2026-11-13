package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var completedMigrationMergeTime = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// cmGit is a fake MergeGitRunner: branches listed as merged answer the merged
// flow of mergedMemberScript (proof commit = the tip), every other branch is
// not in the repository. Unscripted commands fail (an unknown proof).
type cmGit struct {
	script map[string]scriptStep
	calls  int
}

func newCMGit(mergedBranches ...string) *cmGit {
	g := &cmGit{script: map[string]scriptStep{}}
	for _, b := range mergedBranches {
		for k, v := range mergedMemberScript("main", b, true) {
			g.script[k] = v
		}
	}
	g.script[joinArgs("show", "-s", "--format=%cI", proofTipSHA)] = scriptStep{out: completedMigrationMergeTime.Format(time.RFC3339) + "\n"}
	g.script[joinArgs("symbolic-ref", "refs/remotes/origin/HEAD")] = scriptStep{out: "refs/remotes/origin/main\n"}
	return g
}

func (g *cmGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	g.calls++
	if s, ok := g.script[joinArgs(args...)]; ok {
		return s.out, s.err
	}
	return "", errors.New("not scripted")
}

func cmFixture(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// cmPRD creates a cancelled PRD whose branch is automaton/<id>.
func cmPRD(t *testing.T, m *Manager, title string, age time.Duration) *PRD {
	t.Helper()
	p, err := m.store.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Title = title
	p.Status = PRDCancelled
	p.CreatedAt = time.Now().Add(-age)
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func cmStatus(t *testing.T, m *Manager, id string) *PRD {
	t.Helper()
	p, ok := m.store.GetPRD(id)
	if !ok {
		t.Fatalf("prd %s missing", id)
	}
	return p
}

func cmDecisions(p *PRD) []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == DecisionCompletedMigration {
			out = append(out, d)
		}
	}
	return out
}

func cmOpts(g MergeGitRunner) CompletedMigrationOpts {
	return CompletedMigrationOpts{Repo: "/base/repo", Git: g}
}

func TestCompletedMigrationMergedBecomesCompleted(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "merged work", time.Hour)
	g := newCMGit("automaton/" + p.ID)
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(g))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 || len(rep.Completed) != 1 {
		t.Fatalf("report = %+v, want one change", rep)
	}
	got := cmStatus(t, m, p.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	ds := cmDecisions(got)
	if len(ds) != 1 {
		t.Fatalf("decision lines = %d, want 1", len(ds))
	}
	if !ds[0].At.Equal(completedMigrationMergeTime) {
		t.Errorf("completion time = %v, want merge commit time %v", ds[0].At, completedMigrationMergeTime)
	}
	if !rep.Completed[0].CompletedAt.Equal(completedMigrationMergeTime) {
		t.Errorf("report completed_at = %v", rep.Completed[0].CompletedAt)
	}
	if got.IsShipped() {
		t.Error("shipped fields must stay untouched")
	}
}

func TestCompletedMigrationUnmergedLeftCancelled(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "unmerged", time.Hour)
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(newCMGit()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.LeftCancelled != 1 || rep.Changed != 0 {
		t.Fatalf("report = %+v, want left_cancelled 1", rep)
	}
	got := cmStatus(t, m, p.ID)
	if got.Status != PRDCancelled || len(cmDecisions(got)) != 0 {
		t.Errorf("record changed: status %s decisions %d", got.Status, len(got.Decisions))
	}
}

func TestCompletedMigrationShippedUsesMergedAt(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "shipped", time.Hour)
	if _, err := m.RecordPRDShipped(p.ID, "v1.0.0", shippedTestTime, "abc123", ShippedSourceBackfill); err != nil {
		t.Fatal(err)
	}
	g := newCMGit() // no branch is merged: the shipped record alone qualifies
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(g))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 {
		t.Fatalf("report = %+v", rep)
	}
	got := cmStatus(t, m, p.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s", got.Status)
	}
	ds := cmDecisions(got)
	if len(ds) != 1 || !ds[0].At.Equal(shippedTestTime) {
		t.Errorf("decision = %+v, want time %v", ds, shippedTestTime)
	}
	if got.MergeCommit != "abc123" || got.MergedInRelease != "v1.0.0" {
		t.Errorf("shipped fields changed: %+v", got)
	}
	if g.calls != 0 {
		t.Errorf("git called %d times, want 0 when MergedAt is present", g.calls)
	}
}

func TestCompletedMigrationIdempotent(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "merged", time.Hour)
	g := newCMGit("automaton/" + p.ID)
	if _, err := m.MigrateCompleted(context.Background(), cmOpts(g)); err != nil {
		t.Fatal(err)
	}
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(g))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 0 || rep.AlreadyCompleted != 1 {
		t.Fatalf("second run report = %+v, want 0 changed and 1 already_completed", rep)
	}
	if n := len(cmDecisions(cmStatus(t, m, p.ID))); n != 1 {
		t.Errorf("decision lines = %d, want 1", n)
	}
}

func TestCompletedMigrationDryRunWritesNothing(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "merged", time.Hour)
	g := newCMGit("automaton/" + p.ID)
	opts := cmOpts(g)
	opts.DryRun = true
	dry, err := m.MigrateCompleted(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := cmStatus(t, m, p.ID)
	if got.Status != PRDCancelled || len(got.Decisions) != 0 {
		t.Fatalf("dry run wrote: status %s decisions %v", got.Status, got.Decisions)
	}
	if len(dry.WouldComplete) != 1 || dry.Changed != 0 || len(dry.Completed) != 0 {
		t.Fatalf("dry report = %+v", dry)
	}
	w := dry.WouldComplete[0]
	if w.ID != p.ID || w.Title != "merged" || w.Kind != "automaton" || w.From != "cancelled" || w.To != "completed" || w.Reason == "" || !w.CompletedAt.Equal(completedMigrationMergeTime) {
		t.Errorf("would_complete item = %+v", w)
	}
	real, err := m.MigrateCompleted(context.Background(), cmOpts(g))
	if err != nil {
		t.Fatal(err)
	}
	if len(real.Completed) != 1 || real.Completed[0] != w {
		t.Errorf("real list %+v does not match dry list %+v", real.Completed, dry.WouldComplete)
	}
}

func TestCompletedMigrationLimitOldestFirst(t *testing.T) {
	m := cmFixture(t)
	oldest := cmPRD(t, m, "oldest", 3*time.Hour)
	middle := cmPRD(t, m, "middle", 2*time.Hour)
	newest := cmPRD(t, m, "newest", time.Hour)
	g := newCMGit("automaton/"+oldest.ID, "automaton/"+middle.ID, "automaton/"+newest.ID)
	opts := cmOpts(g)
	opts.Limit = 2
	rep, err := m.MigrateCompleted(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 2 || rep.Deferred != 1 {
		t.Fatalf("report = %+v, want 2 changed 1 deferred", rep)
	}
	if cmStatus(t, m, oldest.ID).Status != PRDCompleted || cmStatus(t, m, middle.ID).Status != PRDCompleted {
		t.Error("the two oldest must be completed")
	}
	if cmStatus(t, m, newest.ID).Status != PRDCancelled {
		t.Error("the newest must be untouched")
	}
	rep, err = m.MigrateCompleted(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 || cmStatus(t, m, newest.ID).Status != PRDCompleted {
		t.Errorf("second limited run made no progress: %+v", rep)
	}
}

func TestCompletedMigrationSingleID(t *testing.T) {
	m := cmFixture(t)
	a := cmPRD(t, m, "a", 2*time.Hour)
	b := cmPRD(t, m, "b", time.Hour)
	b.Status = PRDBlocked
	if err := m.store.SavePRD(b); err != nil {
		t.Fatal(err)
	}
	g := newCMGit("automaton/"+a.ID, "automaton/"+b.ID)
	opts := cmOpts(g)
	opts.ID = b.ID
	rep, err := m.MigrateCompleted(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 || cmStatus(t, m, b.ID).Status != PRDCompleted {
		t.Fatalf("blocked record not completed in single mode: %+v", rep)
	}
	if cmStatus(t, m, a.ID).Status != PRDCancelled {
		t.Error("other record must not be touched in single mode")
	}
	// bulk mode ignores blocked records
	m2 := cmFixture(t)
	c := cmPRD(t, m2, "c", time.Hour)
	c.Status = PRDBlocked
	_ = m2.store.SavePRD(c)
	rep, _ = m2.MigrateCompleted(context.Background(), cmOpts(newCMGit("automaton/"+c.ID)))
	if rep.Changed != 0 || cmStatus(t, m2, c.ID).Status != PRDBlocked {
		t.Errorf("bulk mode touched a blocked record: %+v", rep)
	}
}

func TestCompletedMigrationBusyRefusal(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "merged", 2*time.Hour)
	runner := cmPRD(t, m, "running gate", time.Hour)
	runner.Status = PRDRunning
	runner.Story = []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Kind: TaskKindFinalGate, Status: TaskVerifying}}}}
	if err := m.store.SavePRD(runner); err != nil {
		t.Fatal(err)
	}
	g := newCMGit("automaton/" + p.ID)
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(g))
	if !errors.Is(err, ErrCompletedMigrationBusy) {
		t.Fatalf("err = %v, want ErrCompletedMigrationBusy", err)
	}
	if rep.Changed != 0 || cmStatus(t, m, p.ID).Status != PRDCancelled {
		t.Error("busy run changed something")
	}
	opts := cmOpts(g)
	opts.DryRun = true
	rep, err = m.MigrateCompleted(context.Background(), opts)
	if err != nil || !rep.Busy || len(rep.WouldComplete) != 1 {
		t.Errorf("dry run while busy: err %v report %+v", err, rep)
	}
}

func TestCompletedMigrationDecisionLine(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "the title", time.Hour)
	if _, err := m.MigrateCompleted(context.Background(), cmOpts(newCMGit("automaton/"+p.ID))); err != nil {
		t.Fatal(err)
	}
	ds := cmDecisions(cmStatus(t, m, p.ID))
	if len(ds) != 1 || ds[0].Actor != "operator" {
		t.Fatalf("decisions = %+v", ds)
	}
	for _, want := range []string{p.ID, "the title", "cancelled", "completed", proofTipSHA, "merged on main", "cancel command"} {
		if !strings.Contains(ds[0].Note, want) {
			t.Errorf("note %q lacks %q", ds[0].Note, want)
		}
	}
}

func TestCompletedMigrationNoSessionCalls(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "merged", time.Hour)
	var calls int32
	count := func() { atomic.AddInt32(&calls, 1) }
	m.SetSessionKillerFn(func(string, string, string) error { count(); return nil })
	m.SetSessionAliveFn(func(string) bool { count(); return false })
	m.SetSessionOutputFn(func(string) (string, bool) { count(); return "", false })
	m.SetSessionAgentStartedFn(func(string) bool { count(); return false })
	m.SetSessionWaitFn(func(context.Context, AdoptedWait) error { count(); return nil })
	m.SetSessionPaneFn(func(string) (string, error) { count(); return "", nil })
	m.SetSessionStateFn(func(string) string { count(); return "" })
	if _, err := m.MigrateCompleted(context.Background(), cmOpts(newCMGit("automaton/"+p.ID))); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Errorf("session manager saw %d calls, want 0", n)
	}
}

func TestCompletedMigrationAutomation(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "member", 2*time.Hour)
	a, err := m.CreateAutomation(AutomationInput{Name: "chain", Members: []AutomationMember{{PRDID: p.ID}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.automations.Update(a.ID, 0, func(x *Automation) error { x.Status = AutomationCancelled; return nil }); err != nil {
		t.Fatal(err)
	}
	rep, err := m.MigrateCompleted(context.Background(), cmOpts(newCMGit("automaton/"+p.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 2 {
		t.Fatalf("report = %+v, want the Automaton and the Automation", rep)
	}
	got, _ := m.automations.Get(a.ID)
	if got.Status != AutomationCompleted || got.FinishedAt == nil || !got.FinishedAt.Equal(completedMigrationMergeTime) {
		t.Errorf("automation = status %s finished %v", got.Status, got.FinishedAt)
	}
	if !hasCompletedMigrationDecision(got.Decisions) {
		t.Error("automation has no completed_migration decision")
	}
}

func TestCompletedMigrationMarkPRDCompleted(t *testing.T) {
	m := cmFixture(t)
	p := cmPRD(t, m, "blocked one", time.Hour)
	p.Status = PRDBlocked
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got, err := m.MarkPRDCompleted(p.ID, "v1", completedMigrationMergeTime, "abc", "operator", "merged")
	if err != nil || got.Status != PRDCompleted {
		t.Fatalf("from blocked: %v %+v", err, got)
	}
	if n := len(cmDecisions(got)); n != 1 {
		t.Fatalf("decision lines = %d", n)
	}
	got, err = m.MarkPRDCompleted(p.ID, "v1", completedMigrationMergeTime, "abc", "operator", "merged")
	if err != nil || got.Status != PRDCompleted || len(cmDecisions(got)) != 1 {
		t.Errorf("no-op on completed failed: %v %d", err, len(cmDecisions(got)))
	}
	q := cmPRD(t, m, "running", time.Hour)
	q.Status = PRDRunning
	_ = m.store.SavePRD(q)
	if _, err := m.MarkPRDCompleted(q.ID, "", completedMigrationMergeTime, "abc", "operator", "x"); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("running PRD error = %v, want a clear refusal", err)
	}
}
