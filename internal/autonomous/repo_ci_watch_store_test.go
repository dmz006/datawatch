// Repo CI watch store — persistence and Manager accessor tests.
package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRepoCIWatchManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.repoCIWatch == nil {
		t.Fatal("NewManager did not wire the repo CI watch store")
	}
	return m, filepath.Join(m.store.dir, RepoCIWatchFileName)
}

func testRepoCIWatchRec(t *testing.T, runID, status string) *RepoCIWatchRecord {
	t.Helper()
	in := testRepoCIWatchInputs(status, "")
	in["run_id"] = runID
	rec, err := BuildRepoCIWatchRecord(
		in["repo"], in["ref_kind"], in["ref_name"], in["sha"], in["workflow"], in["run_id"],
		in["run_url"], status, "", time.Now())
	if err != nil {
		t.Fatalf("BuildRepoCIWatchRecord: %v", err)
	}
	return rec
}

// TestRepoCIWatchRecordStoreRoundTrip saves records through the
// Manager accessors, refreshes one in place, and checks everything
// survives the JSONL round trip through a reopened store.
func TestRepoCIWatchRecordStoreRoundTrip(t *testing.T) {
	m, path := newTestRepoCIWatchManager(t)

	r1 := testRepoCIWatchRec(t, "111", "failing")
	now := time.Now()
	r1.AlertedAt = &now
	r1.RerunLog = []RepoCIWatchRerun{{At: now, Action: "rerun", RunID: "111"}}
	r1.WouldDo = []RepoCIWatchWouldDo{{Action: "cancel", RunID: "110", Reason: "shadow mode"}}
	r1.SupersededRunIDs = []RepoCIWatchSupersededRun{{RunID: "110", RunURL: "https://github.com/x/a/runs/110"}}
	r1.JudgeVerdict = &RepoCIWatchJudgeVerdict{RunID: "111", Verdict: "infra blip"}

	r2 := testRepoCIWatchRec(t, "222", "green")
	r2.RefKind = "tag"
	r2.RefName = "v2.1.0"
	r2.JudgeVerdict = &RepoCIWatchJudgeVerdict{RunID: "222", Verdict: "pass"}

	saved1, err := m.SaveRepoCIWatchRecord(r1)
	if err != nil {
		t.Fatalf("SaveRepoCIWatchRecord(r1): %v", err)
	}
	if saved1.RunID != "111" || saved1.Status != "failing" {
		t.Fatalf("saved1 = %+v", saved1)
	}
	if _, err := m.SaveRepoCIWatchRecord(r2); err != nil {
		t.Fatalf("SaveRepoCIWatchRecord(r2): %v", err)
	}

	// A refresh under the same key must keep the stored list at two.
	// Save is a full replace (like the Automation store), so the refresh
	// carries the fields it must persist.
	refresh := testRepoCIWatchRec(t, "111", "green")
	refresh.RerunLog = r1.RerunLog
	refresh.WouldDo = r1.WouldDo
	refresh.SupersededRunIDs = r1.SupersededRunIDs
	refresh.JudgeVerdict = r1.JudgeVerdict
	if _, err := m.SaveRepoCIWatchRecord(refresh); err != nil {
		t.Fatalf("refresh Save: %v", err)
	}
	if got := m.ListRepoCIWatchRecords(); len(got) != 2 {
		t.Fatalf("List = %d records, want 2", len(got))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save did not persist repo_ci_watch.jsonl: %v", err)
	}

	// Reopen from disk: same list, same contents.
	m2, err := NewManager(m.dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("reopen NewManager: %v", err)
	}
	got1, ok := m2.repoCIWatch.Get(r1.Key())
	if !ok {
		t.Fatal("reopened store lost r1")
	}
	if got1.Repo != "dmz006/datawatch" || got1.Status != "green" {
		t.Fatalf("r1 rounded trip = %+v", got1)
	}
	if len(got1.RerunLog) != 1 || got1.RerunLog[0].Action != "rerun" {
		t.Fatalf("r1 rerun log = %+v", got1.RerunLog)
	}
	if len(got1.WouldDo) != 1 || got1.WouldDo[0].Reason != "shadow mode" {
		t.Fatalf("r1 would-do log = %+v", got1.WouldDo)
	}
	if len(got1.SupersededRunIDs) != 1 || got1.SupersededRunIDs[0].RunID != "110" {
		t.Fatalf("r1 superseded = %+v", got1.SupersededRunIDs)
	}
	if got1.JudgeVerdict == nil || got1.JudgeVerdict.Verdict != "infra blip" {
		t.Fatalf("r1 judge verdict = %+v", got1.JudgeVerdict)
	}
	got2, ok := m2.repoCIWatch.Get(r2.Key())
	if !ok {
		t.Fatal("reopened store lost r2")
	}
	if got2.RefKind != "tag" || got2.RefName != "v2.1.0" || got2.Status != "green" {
		t.Fatalf("r2 round trip = %+v", got2)
	}
	if saved := m2.ListRepoCIWatchRecords(); len(saved) != 2 {
		t.Fatalf("reopened List = %d, want 2", len(saved))
	}
}

// TestRepoCIWatchRecordMissingFileIsEmpty opens the store under a
// directory with no repo_ci_watch.jsonl: it must start empty, and the
// Manager accessors must behave against the empty store.
func TestRepoCIWatchRecordMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := NewRepoCIWatchStore(dir)
	if err != nil {
		t.Fatalf("NewRepoCIWatchStore over a missing file should succeed: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("fresh store List = %d records, want 0", len(got))
	}
	if _, ok := s.Get("anything"); ok {
		t.Fatal("fresh store Get must miss")
	}
	if err := s.Delete("anything"); err == nil {
		t.Fatal("Delete(missing) must error")
	}

	m, _ := newTestRepoCIWatchManager(t)
	if got := m.ListRepoCIWatchRecords(); len(got) != 0 {
		t.Fatalf("fresh Manager list = %d records, want 0", len(got))
	}
	if err := m.ClearRepoCIWatchRecord("missing"); err == nil {
		t.Fatal("ClearRepoCIWatchRecord(missing) must error")
	}
}

// TestRepoCIWatchRecordAppendNotes appends annotations through the
// Manager accessor and checks the append-only shape: notes accumulate
// in order and appending to a missing record errors.
func TestRepoCIWatchRecordAppendNotes(t *testing.T) {
	m, _ := newTestRepoCIWatchManager(t)
	rec := testRepoCIWatchRec(t, "333", "failing")
	if _, err := m.SaveRepoCIWatchRecord(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No record under this key: append must refuse to create one.
	if _, err := m.AppendRepoCIWatchNote(RepoCIWatchKey("nope", "main", "main", "sha", "w", "1"), "alert", "boo"); err == nil {
		t.Fatal("AppendRepoCIWatchNote on a missing record must error")
	}

	a, err := m.AppendRepoCIWatchNote(rec.Key(), "alert", "alerted the owner")
	if err != nil {
		t.Fatalf("AppendNote(1): %v", err)
	}
	if len(a.Notes) != 1 || a.Notes[0].Source != "alert" || a.Notes[0].Text != "alerted the owner" {
		t.Fatalf("notes after first append = %+v", a.Notes)
	}
	if a.Notes[0].At.IsZero() {
		t.Fatal("note At must be set")
	}

	b, err := m.AppendRepoCIWatchNote(rec.Key(), "rerun", "automatic rerun out")
	if err != nil {
		t.Fatalf("AppendNote(2): %v", err)
	}
	if len(b.Notes) != 2 || b.Notes[1].Source != "rerun" || b.Notes[0].Text != "alerted the owner" {
		t.Fatalf("notes after second append = %+v", b.Notes)
	}
}

// TestRepoCIWatchRecordGreenClears walks one record from failing to
// green: the record is cleared once the run is green, and the store —
// including after a reopen — no longer carries it.
func TestRepoCIWatchRecordGreenClears(t *testing.T) {
	m, path := newTestRepoCIWatchManager(t)
	rec := testRepoCIWatchRec(t, "444", "failing")
	rec.Classification = "code_or_test"
	if _, err := m.SaveRepoCIWatchRecord(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := m.AppendRepoCIWatchNote(rec.Key(), "alert", "page owner"); err != nil {
		t.Fatalf("AppendNote: %v", err)
	}
	if got := m.ListRepoCIWatchRecords(); len(got) != 1 {
		t.Fatalf("List = %d, want 1", len(got))
	}

	// The run goes green: the record is cleared for good.
	if err := m.ClearRepoCIWatchRecord(rec.Key()); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := m.ListRepoCIWatchRecords(); len(got) != 0 {
		t.Fatalf("after clear List = %d, want 0", len(got))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file should persist after clear: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var reloaded RepoCIWatchRecord
		if err := json.Unmarshal([]byte(line), &reloaded); err != nil {
			t.Fatalf("line in cleared file: %v", err)
		}
		if reloaded.RunID == "444" {
			t.Fatal("cleared record survived in the file")
		}
	}

	// A reopen agrees: the record is gone from disk.
	m2, err := NewManager(m.dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, ok := m2.repoCIWatch.Get(rec.Key()); ok {
		t.Fatal("reopened store still carries the cleared record")
	}
}
