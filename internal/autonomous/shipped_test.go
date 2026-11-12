package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// shippedTestTime is the fixed merge time the shipped tests record.
var shippedTestTime = time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)

// newShippedFixture builds a Manager under a tempdir with one PRD and one
// Automation of that PRD.
func newShippedFixture(t *testing.T) (dir string, m *Manager, prd *PRD, auto *Automation) {
	t.Helper()
	dir = t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd = makeAutomationMemberPRDs(t, m, 1)[0]
	auto, err = m.CreateAutomation(AutomationInput{
		Name:    "shipped-fixture",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	return dir, m, prd, auto
}

// TestShippedRecorderPersists records shipped state on a PRD and an
// Automation and checks the four fields on each, on disk after reload and
// IsShipped, without touching either Status.
func TestShippedRecorderPersists(t *testing.T) {
	dir, m, prd, auto := newShippedFixture(t)

	got, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceBackfill)
	if err != nil {
		t.Fatalf("RecordPRDShipped: %v", err)
	}
	assertShippedFieldsSame(t, "prd",
		got.MergedInRelease, got.MergeCommit, got.ShippedSource, got.MergedAt)

	gotA, err := m.RecordAutomationShipped(auto.ID, "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceMergeStep)
	if err != nil {
		t.Fatalf("RecordAutomationShipped: %v", err)
	}
	assertShippedFieldsSame(t, "automation",
		gotA.MergedInRelease, gotA.MergeCommit, gotA.ShippedSource, gotA.MergedAt)
	if gotA.ShippedSource != ShippedSourceMergeStep {
		t.Errorf("automation shipped_source = %q, want %q", gotA.ShippedSource, ShippedSourceMergeStep)
	}

	// Persists through the store save path: a fresh manager over the same
	// data dir reads the same shipped state back from disk.
	m2, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p2, ok := m2.store.GetPRD(prd.ID)
	if !ok {
		t.Fatalf("reloaded PRD %q not found", prd.ID)
	}
	if p2.MergedInRelease != "v1.2.0" || p2.MergeCommit != "c0ffee42" ||
		p2.ShippedSource != ShippedSourceBackfill || p2.MergedAt == nil || !p2.MergedAt.Equal(shippedTestTime) {
		t.Errorf("reloaded prd shipped = release %q commit %q source %q merged_at %v; want v1.2.0 c0ffee42 backfill %v",
			p2.MergedInRelease, p2.MergeCommit, p2.ShippedSource, p2.MergedAt, shippedTestTime)
	}
	if !p2.IsShipped() {
		t.Error("reloaded prd IsShipped = false, want true")
	}
	a2, ok2 := m2.automations.Get(auto.ID)
	if !ok2 {
		t.Fatalf("reloaded automation %q not found", auto.ID)
	}
	if a2.MergedInRelease != "v1.2.0" || a2.MergeCommit != "c0ffee42" ||
		a2.ShippedSource != ShippedSourceMergeStep || a2.MergedAt == nil || !a2.MergedAt.Equal(shippedTestTime) {
		t.Errorf("reloaded automation shipped = release %q commit %q source %q merged_at %v; want v1.2.0 c0ffee42 merge_step %v",
			a2.MergedInRelease, a2.MergeCommit, a2.ShippedSource, a2.MergedAt, shippedTestTime)
	}
	if !a2.IsShipped() {
		t.Error("reloaded automation IsShipped = false, want true")
	}
}

func assertShippedFieldsSame(t *testing.T, what, release, commit, source string, mergedAt *time.Time) {
	t.Helper()
	if release != "v1.2.0" {
		t.Errorf("%s merged_in_release = %q, want v1.2.0", what, release)
	}
	if commit != "c0ffee42" {
		t.Errorf("%s merge_commit = %q, want c0ffee42", what, commit)
	}
	if source != ShippedSourceBackfill && source != ShippedSourceMergeStep {
		t.Errorf("%s shipped_source = %q, want merge_step or backfill", what, source)
	}
	if mergedAt == nil || !mergedAt.Equal(shippedTestTime) {
		t.Errorf("%s merged_at = %v, want %v", what, mergedAt, shippedTestTime)
	}
}

// TestShippedIdempotentSameCommitNoop records twice with the same commit;
// the second call must not change the stored fields, the revision, or
// UpdatedAt, while a different commit updates everything (including a
// merge_step record replacing a backfill one).
func TestShippedIdempotentSameCommitNoop(t *testing.T) {
	_, m, prd, auto := newShippedFixture(t)

	if prd.IsShipped() {
		t.Fatal("fresh PRD reports IsShipped = true, want false")
	}
	p1, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceBackfill)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.RecordPRDShipped(prd.ID, "v9.9.9", shippedTestTime.Add(time.Hour), "c0ffee42", ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if again.MergeCommit != "c0ffee42" || again.MergedInRelease != "v1.2.0" || again.ShippedSource != ShippedSourceBackfill {
		t.Errorf("same-commit re-record changed fields: release %q source %q commit %q (want v1.2.0 backfill c0ffee42)",
			again.MergedInRelease, again.ShippedSource, again.MergeCommit)
	}
	if !reflect.DeepEqual(p1.Rev, again.Rev) {
		t.Errorf("same-commit re-record changed prd rev %v -> %v; the no-op must not bump it", p1.Rev, again.Rev)
	}

	// A different commit does update, and a merge_step record can replace
	// the backfill one.
	p2, err := m.RecordPRDShipped(prd.ID, "v1.3.0", shippedTestTime, "bead1", ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if p2.MergeCommit != "bead1" || p2.MergedInRelease != "v1.3.0" || p2.ShippedSource != ShippedSourceMergeStep {
		t.Errorf("different-commit record = release %q commit %q source %q; want v1.3.0 bead1 merge_step",
			p2.MergedInRelease, p2.MergeCommit, p2.ShippedSource)
	}
	if !p2.IsShipped() {
		t.Error("IsShipped = false after a different-commit record, want true")
	}

	// Same behavior on the Automation side.
	a1, err := m.RecordAutomationShipped(auto.ID, "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceBackfill)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := m.RecordAutomationShipped(auto.ID, "v9.9.9", shippedTestTime, "c0ffee42", ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a1, a2) {
		t.Errorf("same-commit automation re-record changed the automation:\nfirst  %+v\nsecond %+v", a1, a2)
	}
	a3, err := m.RecordAutomationShipped(auto.ID, "v1.3.0", shippedTestTime, "bead1", ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if a3.MergeCommit != "bead1" || a3.ShippedSource != ShippedSourceMergeStep || !a3.IsShipped() {
		t.Errorf("automation different-commit record = commit %q source %q; want bead1 merge_step", a3.MergeCommit, a3.ShippedSource)
	}
}

// TestShippedFromCancelledPRDIsStillCancelled records shipped state on a
// cancelled PRD and makes sure the status does not move.
func TestShippedFromCancelledPRDIsStillCancelled(t *testing.T) {
	_, m, prd, _ := newShippedFixture(t)

	prd.Status = PRDCancelled
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	got, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceMergeStep)
	if err != nil {
		t.Fatalf("RecordPRDShipped: %v", err)
	}
	if got.Status != PRDCancelled {
		t.Errorf("recorded shipped on a cancelled PRD: status = %q, must stay cancelled", got.Status)
	}
	if !got.IsShipped() {
		t.Error("cancelled + recorded PRD: IsShipped = false, want true — shipped state is orthogonal to status")
	}
}

// TestShippedSourceAndCommitValidation rejects empty commits and unknown
// sources on both record methods.
func TestShippedSourceAndCommitValidation(t *testing.T) {
	_, m, prd, auto := newShippedFixture(t)

	if _, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, "  ", ShippedSourceBackfill); err == nil {
		t.Error("RecordPRDShipped accepted an empty merge commit")
	}
	if _, err := m.RecordPRDShipped(prd.ID, "v1.2.0", shippedTestTime, "c0ffee42", "operator"); err == nil {
		t.Errorf("RecordPRDShipped accepted source %q; want only %q or %q", "operator", ShippedSourceMergeStep, ShippedSourceBackfill)
	}
	if _, err := m.RecordAutomationShipped(auto.ID, "v1.2.0", shippedTestTime, "", ShippedSourceMergeStep); err == nil {
		t.Error("RecordAutomationShipped accepted an empty merge commit")
	}
	if _, err := m.RecordAutomationShipped(auto.ID, "v1.2.0", shippedTestTime, "c0ffee42", "manual"); err == nil {
		t.Errorf("RecordAutomationShipped accepted source %q; want only %q or %q", "manual", ShippedSourceMergeStep, ShippedSourceBackfill)
	}
	// And nothing was persisted by the rejected calls.
	p, _ := m.store.GetPRD(prd.ID)
	if p != nil && p.IsShipped() {
		t.Error("rejected PRD records left shipped state behind")
	}
	a, _ := m.automations.Get(auto.ID)
	if a != nil && a.IsShipped() {
		t.Error("rejected automation records left shipped state behind")
	}
}

// TestShippedNotFounds reports store-not-found errors, not panics.
func TestShippedNotFounds(t *testing.T) {
	_, m, _, _ := newShippedFixture(t)

	if _, err := m.RecordPRDShipped("zzzzzzzz", "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceBackfill); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("RecordPRDShipped on a missing PRD: err = %v, want not found", err)
	}
	if _, err := m.RecordAutomationShipped("zzzzzzzz", "v1.2.0", shippedTestTime, "c0ffee42", ShippedSourceMergeStep); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("RecordAutomationShipped on a missing automation: err = %v, want not found", err)
	}
}
