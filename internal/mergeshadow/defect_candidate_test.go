package mergeshadow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var candidateTestTime = time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC)

// candidateTestComparison returns a comparison for id with two differences.
func candidateTestComparison(id string, at time.Time) *Comparison {
	return &Comparison{
		AutomatonID: id,
		ReleaseSize: ReleaseSizeMinor,
		Matched:     4,
		Different:   2,
		Differences: []Difference{
			{Field: "version", Planned: "9.113.0", Landed: "(missing)", Severity: SeverityMajor},
			{Field: "changelog_heading", Planned: "## [9.113.0]", Landed: "## [9.112.0]", Severity: SeverityMinor},
		},
		CreatedAt: at,
	}
}

func TestDefectCandidatesFromComparisonOnePerDifference(t *testing.T) {
	c := candidateTestComparison("auto-a", candidateTestTime)
	got, err := DefectCandidatesFromComparison(c)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(got) != len(c.Differences) {
		t.Fatalf("got %d candidates, want %d", len(got), len(c.Differences))
	}
	for i, d := range c.Differences {
		cand := got[i]
		if cand.AutomatonID != "auto-a" || cand.Field != d.Field || cand.Planned != d.Planned ||
			cand.Landed != d.Landed || cand.Severity != d.Severity {
			t.Errorf("candidate %d = %+v, does not match difference %+v", i, cand, d)
		}
		if cand.ReleaseSize != ReleaseSizeMinor || cand.Status != StatusCandidate {
			t.Errorf("candidate %d release/status = %q/%q", i, cand.ReleaseSize, cand.Status)
		}
		if !cand.CreatedAt.Equal(candidateTestTime) {
			t.Errorf("candidate %d created_at = %v", i, cand.CreatedAt)
		}
		if len(cand.Key) != 64 {
			t.Errorf("candidate %d key %q is not a sha256 hex digest", i, cand.Key)
		}
	}
	if got[0].Key == got[1].Key {
		t.Error("distinct differences share a key")
	}
}

func TestDefectCandidatesFromComparisonNoDifferences(t *testing.T) {
	c := &Comparison{AutomatonID: "auto-a", Differences: []Difference{}, CreatedAt: candidateTestTime}
	got, err := DefectCandidatesFromComparison(c)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}

func TestDefectCandidatesFromComparisonNil(t *testing.T) {
	if _, err := DefectCandidatesFromComparison(nil); err == nil {
		t.Fatal("nil comparison accepted")
	}
}

func TestDefectCandidateKeyStableAndSensitive(t *testing.T) {
	base := DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime)
	if again := DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime); again != base {
		t.Fatal("key is not deterministic")
	}
	// Same time in another zone is the same instant, so the same key.
	if inZone := DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime.In(time.FixedZone("x", 3600))); inZone != base {
		t.Fatal("key depends on the time zone, not the instant")
	}
	variants := map[string]string{
		"automaton": DefectCandidateKey("auto-b", "version", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime),
		"field":     DefectCandidateKey("auto-a", "readme_line", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime),
		"planned":   DefectCandidateKey("auto-a", "version", "1.0.1", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime),
		"landed":    DefectCandidateKey("auto-a", "version", "1.0.0", "1.0.2", ReleaseSizePatch, StatusCandidate, candidateTestTime),
		"size":      DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizeMajor, StatusCandidate, candidateTestTime),
		"status":    DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizePatch, "filed", candidateTestTime),
		"created":   DefectCandidateKey("auto-a", "version", "1.0.0", "(missing)", ReleaseSizePatch, StatusCandidate, candidateTestTime.Add(time.Nanosecond)),
	}
	for name, k := range variants {
		if k == base {
			t.Errorf("changing %s did not change the key", name)
		}
	}
}

func TestDefectCandidateKeyNoConcatenationCollision(t *testing.T) {
	a := DefectCandidateKey("ab", "c", "", "", ReleaseSizePatch, StatusCandidate, candidateTestTime)
	b := DefectCandidateKey("a", "bc", "", "", ReleaseSizePatch, StatusCandidate, candidateTestTime)
	if a == b {
		t.Fatal("different field splits collide")
	}
}

func TestRecordCreatesCandidates(t *testing.T) {
	dir := t.TempDir()
	c := candidateTestComparison("auto-a", candidateTestTime)

	created, err := RecordComparisonCandidates(dir, c)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created %d, want 2", len(created))
	}
	if _, err := os.Stat(filepath.Join(dir, defectCandidateDir, "auto-a.json")); err != nil {
		t.Fatalf("candidate file not written: %v", err)
	}
	list, err := ListDefectCandidates(dir, "auto-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d, want 2", len(list))
	}
	want := map[string]DefectCandidate{}
	for _, cand := range created {
		want[cand.Key] = cand
	}
	for _, got := range list {
		w, ok := want[got.Key]
		if !ok {
			t.Fatalf("listed key %s was not created", got.Key)
		}
		if got.Field != w.Field || got.Planned != w.Planned || got.Landed != w.Landed ||
			got.Severity != w.Severity || got.ReleaseSize != w.ReleaseSize ||
			got.Status != w.Status || !got.CreatedAt.Equal(w.CreatedAt) {
			t.Errorf("round trip changed candidate: got %+v want %+v", got, w)
		}
	}
}

func TestRecordDuplicateKeyNotDuplicated(t *testing.T) {
	dir := t.TempDir()
	c := candidateTestComparison("auto-a", candidateTestTime)
	if _, err := RecordComparisonCandidates(dir, c); err != nil {
		t.Fatalf("first record: %v", err)
	}
	created, err := RecordComparisonCandidates(dir, c)
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if created == nil || len(created) != 0 {
		t.Fatalf("second record created %d, want none", len(created))
	}
	list, err := ListDefectCandidates(dir, "auto-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d after repeat, want 2", len(list))
	}
}

func TestRecordNewKeyAddsOnlyNewCandidates(t *testing.T) {
	dir := t.TempDir()
	c := candidateTestComparison("auto-a", candidateTestTime)
	if _, err := RecordComparisonCandidates(dir, c); err != nil {
		t.Fatalf("first record: %v", err)
	}
	// A later comparison with one changed landed value: only that one is new,
	// because the key includes the landed value. The timestamp is the same.
	c2 := candidateTestComparison("auto-a", candidateTestTime)
	c2.Differences[1].Landed = "## [9.111.0]"
	created, err := RecordComparisonCandidates(dir, c2)
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if len(created) != 1 || created[0].Landed != "## [9.111.0]" {
		t.Fatalf("created %+v, want only the changed candidate", created)
	}
	list, _ := ListDefectCandidates(dir, "auto-a")
	if len(list) != 3 {
		t.Fatalf("listed %d, want 3", len(list))
	}
}

func TestRecordRerunNewTimestampNotDuplicated(t *testing.T) {
	// A fresh Compare run on unchanged data stamps a new comparison time. Its
	// candidates have the same content as the stored ones, so none is stored
	// again, and the stored records keep their first created_at and key.
	dir := t.TempDir()
	first, err := RecordComparisonCandidates(dir, candidateTestComparison("auto-a", candidateTestTime))
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	created, err := RecordComparisonCandidates(dir, candidateTestComparison("auto-a", candidateTestTime.Add(time.Minute)))
	if err != nil {
		t.Fatalf("rerun record: %v", err)
	}
	if created == nil || len(created) != 0 {
		t.Fatalf("rerun created %d, want none", len(created))
	}
	list, err := ListDefectCandidates(dir, "auto-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d after rerun, want 2", len(list))
	}
	for _, got := range list {
		if !got.CreatedAt.Equal(candidateTestTime) {
			t.Errorf("stored created_at changed to %v on rerun", got.CreatedAt)
		}
		found := false
		for _, f := range first {
			if f.Key == got.Key {
				found = true
			}
		}
		if !found {
			t.Errorf("stored key %s changed on rerun", got.Key)
		}
	}
}

func TestRecordChangedContentAfterRerunIsNew(t *testing.T) {
	// A rerun whose landed value changed is a different difference, so it is
	// stored as a new candidate with the new comparison's created_at.
	dir := t.TempDir()
	if _, err := RecordComparisonCandidates(dir, candidateTestComparison("auto-a", candidateTestTime)); err != nil {
		t.Fatalf("first record: %v", err)
	}
	later := candidateTestComparison("auto-a", candidateTestTime.Add(time.Minute))
	later.Differences[0].Landed = "9.112.0"
	created, err := RecordComparisonCandidates(dir, later)
	if err != nil {
		t.Fatalf("rerun record: %v", err)
	}
	if len(created) != 1 || created[0].Landed != "9.112.0" || !created[0].CreatedAt.Equal(later.CreatedAt) {
		t.Fatalf("created %+v, want one new candidate stamped with the later time", created)
	}
}

func TestRecordNoDifferencesWritesNothing(t *testing.T) {
	dir := t.TempDir()
	c := &Comparison{AutomatonID: "auto-a", Matched: 6, Differences: []Difference{}, CreatedAt: candidateTestTime}
	created, err := RecordComparisonCandidates(dir, c)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if created == nil || len(created) != 0 {
		t.Fatalf("created %#v, want empty non-nil", created)
	}
	if _, err := os.Stat(filepath.Join(dir, defectCandidateDir)); !os.IsNotExist(err) {
		t.Fatalf("store directory exists after no candidates: %v", err)
	}
}

func TestRecordRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	if _, err := RecordComparisonCandidates(dir, nil); err == nil {
		t.Error("nil comparison accepted")
	}
	if _, err := RecordComparisonCandidates(dir, candidateTestComparison("../escape", candidateTestTime)); err == nil {
		t.Error("path-like automaton id accepted")
	}
	if _, err := RecordComparisonCandidates(dir, candidateTestComparison("", candidateTestTime)); err == nil {
		t.Error("empty automaton id accepted")
	}
	if _, err := RecordComparisonCandidates("", candidateTestComparison("auto-a", candidateTestTime)); err == nil {
		t.Error("empty data directory accepted")
	}
}

func TestListDefectCandidatesEmptyAndSorted(t *testing.T) {
	dir := t.TempDir()
	got, err := ListDefectCandidates(dir, "auto-none")
	if err != nil {
		t.Fatalf("list missing: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("list missing = %#v, want empty non-nil", got)
	}
	if _, err := ListDefectCandidates(dir, "../x"); err == nil {
		t.Error("invalid id accepted by list")
	}

	old := candidateTestComparison("auto-a", candidateTestTime)
	newer := candidateTestComparison("auto-a", candidateTestTime.Add(time.Hour))
	newer.Differences = newer.Differences[:1]
	newer.Differences[0].Landed = "9.112.0" // new content, so a new candidate
	if _, err := RecordComparisonCandidates(dir, old); err != nil {
		t.Fatalf("record old: %v", err)
	}
	if _, err := RecordComparisonCandidates(dir, newer); err != nil {
		t.Fatalf("record newer: %v", err)
	}
	list, err := ListDefectCandidates(dir, "auto-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("listed %d, want 3", len(list))
	}
	if !list[0].CreatedAt.Equal(newer.CreatedAt) {
		t.Errorf("first candidate is not the newest: %v", list[0].CreatedAt)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].CreatedAt.Before(list[i].CreatedAt) {
			t.Errorf("list not newest first at %d", i)
		}
	}
}

func TestListAllDefectCandidatesAcrossAutomatons(t *testing.T) {
	dir := t.TempDir()
	got, err := ListAllDefectCandidates(dir)
	if err != nil {
		t.Fatalf("list all missing: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("list all missing = %#v, want empty non-nil", got)
	}
	if _, err := ListAllDefectCandidates(""); err == nil {
		t.Error("empty data directory accepted")
	}

	if _, err := RecordComparisonCandidates(dir, candidateTestComparison("auto-a", candidateTestTime)); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if _, err := RecordComparisonCandidates(dir, candidateTestComparison("auto-b", candidateTestTime.Add(time.Hour))); err != nil {
		t.Fatalf("record b: %v", err)
	}
	all, err := ListAllDefectCandidates(dir)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("listed %d across automatons, want 4", len(all))
	}
	if all[0].AutomatonID != "auto-b" {
		t.Errorf("newest candidate belongs to %q, want auto-b", all[0].AutomatonID)
	}
}
