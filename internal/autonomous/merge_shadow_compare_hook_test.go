package autonomous

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// shadowFakeReader swaps the landed reader for one that returns a fixed
// Landed value and counts its calls. The swap is undone when the test ends.
func shadowFakeReader(t *testing.T, landed mergeshadow.Landed) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	prev := mergeShadowLandedReader
	mergeShadowLandedReader = func(_ context.Context, _, _ string, _ mergeshadow.LandedQuery) (mergeshadow.Landed, error) {
		calls.Add(1)
		return landed, nil
	}
	t.Cleanup(func() { mergeShadowLandedReader = prev })
	return calls
}

// shadowPlan is a stored plan for automatonID whose planned version is
// 1.2.3. Nothing in the fixture is a parity issue, so the parity field
// always matches.
func shadowPlan(automatonID string) *mergeshadow.MergePlan {
	return &mergeshadow.MergePlan{
		AutomatonID:      automatonID,
		Project:          "proj",
		Branch:           "automaton/work",
		BaseRef:          "main",
		ReleaseSize:      mergeshadow.ReleaseSizePatch,
		PlannedVersion:   "1.2.3",
		ChangelogHeading: "## v1.2.3",
		ReadmeLine:       "Current release: 1.2.3",
		RegistryRow:      "| automaton | shipped |",
		CreatedAt:        time.Now().UTC(),
	}
}

// shadowLanded is the landed read that matches shadowPlan exactly.
func shadowLanded(version string) mergeshadow.Landed {
	return mergeshadow.Landed{
		ChangelogHeading: mergeshadow.Value{Value: "## v1.2.3", Found: true},
		Versions: map[string]mergeshadow.Value{
			"internal/server/api.go": {Value: version, Found: true},
		},
		ReadmeLine:  mergeshadow.Value{Value: "Current release: 1.2.3", Found: true},
		RegistryRow: mergeshadow.Value{Value: "| automaton | shipped |", Found: true},
	}
}

// TestMergeShadowChangelogKindMatchesAutonomous keeps the changelog task-kind
// marker that mergeshadow reads equal to the one autonomous writes. mergeshadow
// cannot import autonomous, so only this side can check the pair.
func TestMergeShadowChangelogKindMatchesAutonomous(t *testing.T) {
	if mergeshadow.ChangelogTaskKind != DefectTaskKindChangelog {
		t.Fatalf("mergeshadow.ChangelogTaskKind = %q, want %q", mergeshadow.ChangelogTaskKind, DefectTaskKindChangelog)
	}
}

// TestMergeShadowCompareStoredPlan stores a plan and a landed value that
// disagrees on the version, runs the hook and expects one defect candidate.
func TestMergeShadowCompareStoredPlan(t *testing.T) {
	dir, m, _, auto := newShippedFixture(t)
	if err := mergeshadow.Save(dir, auto.ID, shadowPlan(auto.ID)); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	calls := shadowFakeReader(t, shadowLanded("1.2.2"))

	created, err := m.runMergeShadowCompare(context.Background(), auto.ID)
	if err != nil {
		t.Fatalf("runMergeShadowCompare: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("landed reader calls = %d, want 1", calls.Load())
	}
	if len(created) != 1 {
		t.Fatalf("created candidates = %d, want 1", len(created))
	}
	if created[0].Field != "version" || created[0].AutomatonID != auto.ID {
		t.Errorf("candidate = %+v, want version difference for %s", created[0], auto.ID)
	}
	stored, err := mergeshadow.ListDefectCandidates(dir, auto.ID)
	if err != nil {
		t.Fatalf("ListDefectCandidates: %v", err)
	}
	if len(stored) != 1 {
		t.Errorf("stored candidates = %d, want 1", len(stored))
	}
}

// TestMergeShadowCompareNoStoredPlan checks that an Automaton without a
// stored plan does no landed read and writes no candidates.
func TestMergeShadowCompareNoStoredPlan(t *testing.T) {
	dir, m, _, auto := newShippedFixture(t)
	calls := shadowFakeReader(t, shadowLanded("1.2.2"))

	created, err := m.runMergeShadowCompare(context.Background(), auto.ID)
	if err != nil {
		t.Fatalf("runMergeShadowCompare: %v", err)
	}
	if created != nil {
		t.Errorf("created = %+v, want nil", created)
	}
	if calls.Load() != 0 {
		t.Errorf("landed reader calls = %d, want 0", calls.Load())
	}
	stored, err := mergeshadow.ListDefectCandidates(dir, auto.ID)
	if err != nil {
		t.Fatalf("ListDefectCandidates: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("stored candidates = %d, want 0", len(stored))
	}
}

// TestMergeShadowCompareRepeatAddsNoDuplicates runs the hook twice over the
// same landed data. The second run creates nothing and the store keeps one
// candidate.
func TestMergeShadowCompareRepeatAddsNoDuplicates(t *testing.T) {
	dir, m, _, auto := newShippedFixture(t)
	if err := mergeshadow.Save(dir, auto.ID, shadowPlan(auto.ID)); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	shadowFakeReader(t, shadowLanded("1.2.2"))

	first, err := m.runMergeShadowCompare(context.Background(), auto.ID)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := m.runMergeShadowCompare(context.Background(), auto.ID)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first run created %d, want 1", len(first))
	}
	if len(second) != 0 {
		t.Errorf("second run created %d, want 0", len(second))
	}
	stored, err := mergeshadow.ListDefectCandidates(dir, auto.ID)
	if err != nil {
		t.Fatalf("ListDefectCandidates: %v", err)
	}
	if len(stored) != 1 {
		t.Errorf("stored candidates = %d after repeat, want 1", len(stored))
	}
}

// TestMergeShadowCompareReaderErrorIsReturned checks that a failing landed
// read is reported to the caller and stores nothing.
func TestMergeShadowCompareReaderErrorIsReturned(t *testing.T) {
	dir, m, _, auto := newShippedFixture(t)
	if err := mergeshadow.Save(dir, auto.ID, shadowPlan(auto.ID)); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	prev := mergeShadowLandedReader
	mergeShadowLandedReader = func(context.Context, string, string, mergeshadow.LandedQuery) (mergeshadow.Landed, error) {
		return mergeshadow.Landed{}, errors.New("ref gone")
	}
	t.Cleanup(func() { mergeShadowLandedReader = prev })

	if _, err := m.runMergeShadowCompare(context.Background(), auto.ID); err == nil {
		t.Fatal("expected the reader error to be returned")
	}
	stored, err := mergeshadow.ListDefectCandidates(dir, auto.ID)
	if err != nil {
		t.Fatalf("ListDefectCandidates: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("stored candidates = %d, want 0", len(stored))
	}
}

// TestMergeShadowCompareRunsAfterShippedRecord checks the wiring: recording
// the Automaton as shipped starts the comparison, which stores the candidate
// in the background.
func TestMergeShadowCompareRunsAfterShippedRecord(t *testing.T) {
	dir, m, _, auto := newShippedFixture(t)
	if err := mergeshadow.Save(dir, auto.ID, shadowPlan(auto.ID)); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	shadowFakeReader(t, shadowLanded("1.2.2"))

	if _, err := m.RecordAutomationShipped(auto.ID, "v1.2.3", shippedTestTime, "c0ffee42", ShippedSourceMergeStep); err != nil {
		t.Fatalf("RecordAutomationShipped: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		stored, err := mergeshadow.ListDefectCandidates(dir, auto.ID)
		if err != nil {
			t.Fatalf("ListDefectCandidates: %v", err)
		}
		if len(stored) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background compare stored %d candidates, want 1", len(stored))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
