package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var archiveTestNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

const archiveTestRetention = 30 * 24 * time.Hour

// fatArchiveFixture stores one finished Automaton that carries a decision
// log, session history and raw judge text on every task.
func fatArchiveFixture(t *testing.T, s *Store, status PRDStatus, age time.Duration) string {
	t.Helper()
	p, err := s.CreatePRD("fat spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	raw := strings.Repeat("judge output line\n", 3000)
	var sts []Story
	for a := 0; a < 2; a++ {
		st := Story{ID: fmt.Sprintf("%s-s%d", p.ID, a), PRDID: p.ID, Title: "story", Status: StoryCompleted,
			Verdicts: []GuardrailVerdict{{Guardrail: "rules", Outcome: "pass", JudgeSessionID: "js-story", RawJudge: raw}}}
		for b := 0; b < 3; b++ {
			done := archiveTestNow.Add(-age)
			st.Tasks = append(st.Tasks, Task{
				ID: fmt.Sprintf("%s-s%d-t%d", p.ID, a, b), StoryID: st.ID, PRDID: p.ID, Title: "task", Spec: "short spec",
				Status: TaskCompleted, SessionID: fmt.Sprintf("w-%d-%d", a, b), CompletedAt: &done,
				Error:            strings.Repeat("e", 4000),
				Verification:     &VerificationResult{OK: true, Summary: "ok", VerifierID: "v-1", RawJudge: raw},
				Verdicts:         []GuardrailVerdict{{Guardrail: "security", Outcome: "pass", JudgeSessionID: "js-task", RawJudge: raw}},
				History:          []TaskHistoryEntry{{At: done, Note: strings.Repeat("h", 500)}, {At: done, Note: "second"}},
				Attempts:         []TaskAttempt{{Number: 1, Outcome: "failed", FailureSummary: strings.Repeat("f", 800)}},
				SessionDecisions: []SessionDecision{},
				StartFailureTail: []string{strings.Repeat("tail", 300)},
			})
		}
		sts = append(sts, st)
	}
	if err := s.SetStories(p.ID, sts); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	s.WithLock(func() {
		cur := s.prds[p.ID]
		cur.Status = status
		cur.Title = "Fat Automaton"
		cur.UpdatedAt = archiveTestNow.Add(-age)
		cur.MemoryReport = strings.Repeat("report ", 2000)
		cur.Verdicts = []GuardrailVerdict{{Guardrail: "prd", Outcome: "pass", JudgeSessionID: "js-prd", RawJudge: raw}}
		for i := 0; i < 200; i++ {
			cur.Decisions = append(cur.Decisions, Decision{At: archiveTestNow.Add(-age), Kind: "run", Actor: "autonomous",
				Note: strings.Repeat("n", 300), JudgeSessionID: fmt.Sprintf("js-%d", i%7), CostUSD: 0.01})
		}
		if status == PRDCompleted {
			cur.MergeCommit = "abc123"
		}
		if err := s.persistPRD(p.ID); err != nil {
			t.Fatalf("persistPRD: %v", err)
		}
	})
	return p.ID
}

func newArchiveStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

func marshalPRDForTest(t *testing.T, s *Store, id string) []byte {
	t.Helper()
	var b []byte
	var err error
	s.WithLock(func() { b, err = json.Marshal(s.prds[id]) })
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestArchiveThenRestoreIsByteEqual(t *testing.T) {
	s, _ := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	before := marshalPRDForTest(t, s, id)

	if _, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention); err != nil {
		t.Fatalf("archive: %v", err)
	}
	mid := marshalPRDForTest(t, s, id)
	if string(mid) == string(before) {
		t.Fatal("archive changed nothing")
	}
	if err := s.RestoreFromArchive(id); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after := marshalPRDForTest(t, s, id)
	if string(after) != string(before) {
		t.Fatalf("restored record differs from the original (%d vs %d bytes)", len(after), len(before))
	}
}

func TestArchiveReadArchiveOnAskAndSurvivesReopen(t *testing.T) {
	s, dir := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	before := marshalPRDForTest(t, s, id)
	if _, err := s.ReadArchive(id); !errors.Is(err, ErrNotArchived) {
		t.Fatalf("ReadArchive on a live record: %v", err)
	}
	if _, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention); err != nil {
		t.Fatal(err)
	}
	a, err := s.ReadArchive(id)
	if err != nil || len(a.Decisions) != 200 || len(a.Tasks) != 6 {
		t.Fatalf("ReadArchive: %v %+v", err, a)
	}
	// A new Store over the same directory reads the slim record and the same
	// archive, and restores to the original bytes.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.RestoreFromArchive(id); err != nil {
		t.Fatalf("restore after reopen: %v", err)
	}
	if got := marshalPRDForTest(t, s2, id); string(got) != string(before) {
		t.Fatal("restore after reopen differs from the original")
	}
}

func TestArchiveKeepsSummaryIndexUnchanged(t *testing.T) {
	s, dir := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	before, _ := s.SummaryEntry(id)
	// Baseline as a restart reads it (times decode in their stored zone).
	s0, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	baseline, _ := s0.SummaryEntry(id)
	rev := s.Revision()
	if _, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention); err != nil {
		t.Fatal(err)
	}
	after, _ := s.SummaryEntry(id)
	if !reflect.DeepEqual(before, after) {
		b1, _ := json.Marshal(before.Row)
		b2, _ := json.Marshal(after.Row)
		t.Fatalf("summary entry changed by archiving:\nrow before %s\nrow after  %s", b1, b2)
	}
	if s.Revision() != rev {
		t.Fatal("archiving bumped the index revision")
	}
	// The entry rebuilt from the slim record after a restart is the same.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _ := s2.SummaryEntry(id)
	if !reflect.DeepEqual(baseline, reopened) {
		t.Fatal("summary entry after reopen differs from the entry before archiving")
	}
}

func TestArchiveShrinksFatRecord(t *testing.T) {
	s, _ := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	res, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention)
	if err != nil {
		t.Fatal(err)
	}
	if res.BytesBefore == 0 || res.BytesAfter*10 > res.BytesBefore {
		t.Fatalf("live record did not drop to a small fraction: before %d after %d", res.BytesBefore, res.BytesAfter)
	}
	info, err := os.Stat(filepath.Join(s.dir, prdRecordsDirName, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size()*10 > res.BytesBefore {
		t.Fatalf("record file is %d bytes against %d before", info.Size(), res.BytesBefore)
	}
	// Archived data is all still on disk.
	if _, err := os.Stat(filepath.Join(s.dir, archiveDirName, id+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveDryRunWritesNothing(t *testing.T) {
	s, dir := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	snap := snapshotTree(t, dir)
	plan := s.PlanArchive(archiveTestNow, archiveTestRetention)
	if !plan.DryRun || len(plan.Eligible) != 1 || plan.Eligible[0].ID != id {
		t.Fatalf("plan: %+v", plan)
	}
	if plan.BytesAfter >= plan.BytesBefore || plan.ArchiveBytes == 0 {
		t.Fatalf("plan sizes: %+v", plan)
	}
	if got := snapshotTree(t, dir); !reflect.DeepEqual(snap, got) {
		t.Fatalf("dry run changed the data directory:\n%v\n%v", snap, got)
	}
	if _, err := os.Stat(filepath.Join(dir, archiveDirName)); !os.IsNotExist(err) {
		t.Fatal("dry run created the archive directory")
	}
}

// snapshotTree maps every file under dir to its size and mod time.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			out[p] = fmt.Sprintf("%d %d %v", info.Size(), info.ModTime().UnixNano(), info.IsDir())
		}
		return nil
	})
	return out
}

func TestArchiveFailedBackupVerificationChangesNothing(t *testing.T) {
	s, _ := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	before := marshalPRDForTest(t, s, id)
	recBefore, err := os.ReadFile(filepath.Join(s.dir, prdRecordsDirName, id+".json"))
	if err != nil || len(recBefore) == 0 {
		t.Fatalf("read record: %v", err)
	}
	// Corrupt each backup copy right after it is written.
	s.archiveBackupHook = func(dst string) { _ = os.WriteFile(dst, []byte("corrupt"), 0o644) }
	_, err = s.RunArchive(archiveTestNow, archiveTestRetention)
	if err == nil || !strings.Contains(err.Error(), "verify backup") {
		t.Fatalf("want a backup verification error, got %v", err)
	}
	recAfter, _ := os.ReadFile(filepath.Join(s.dir, prdRecordsDirName, id+".json"))
	if string(recAfter) != string(recBefore) {
		t.Fatal("record file changed after a failed backup verification")
	}
	if string(marshalPRDForTest(t, s, id)) != string(before) {
		t.Fatal("in-memory record changed after a failed backup verification")
	}
	if _, err := os.Stat(filepath.Join(s.dir, archiveDirName)); !os.IsNotExist(err) {
		t.Fatal("an archive file was written after a failed backup verification")
	}
}

func TestArchiveInterruptedLeavesReadableRecord(t *testing.T) {
	for _, stage := range []string{archiveStageFileWritten, archiveStageRecordWriten} {
		t.Run(stage, func(t *testing.T) {
			s, dir := newArchiveStore(t)
			id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
			before := marshalPRDForTest(t, s, id)
			s.archiveHook = func(st string) error {
				if st == stage {
					return errors.New("interrupted")
				}
				return nil
			}
			if _, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention); err == nil {
				t.Fatal("hook did not stop the archive")
			}
			// A fresh store reads the record from disk: old or new generation.
			s2, err := NewStore(dir)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			p, ok := s2.GetPRD(id)
			if !ok {
				t.Fatal("record lost after an interrupted archive")
			}
			if p.Archive == nil {
				if got := marshalPRDForTest(t, s2, id); string(got) != string(before) {
					t.Fatal("old generation differs from the original")
				}
			} else if err := s2.RestoreFromArchive(id); err != nil {
				t.Fatalf("new generation cannot be restored: %v", err)
			} else if got := marshalPRDForTest(t, s2, id); string(got) != string(before) {
				t.Fatal("new generation restores to different bytes")
			}
			// And a second archive run finishes the job.
			s2.archiveHook = nil
			if _, err := s2.RunArchive(archiveTestNow, archiveTestRetention); err != nil {
				t.Fatalf("rerun: %v", err)
			}
		})
	}
}

func TestArchiveSkipsAutomatonInUse(t *testing.T) {
	s, _ := newArchiveStore(t)
	running := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	s.WithLock(func() {
		s.prds[running].Story[0].Tasks[0].Status = TaskInProgress
	})
	openSession := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	unmerged := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	s.WithLock(func() {
		s.prds[unmerged].MergeCommit = ""
		s.prds[unmerged].Git.Branch = "automaton/x"
	})
	young := fatArchiveFixture(t, s, PRDCompleted, 2*24*time.Hour)
	s.SetArchiveSessionCheck(func(id string) bool { return id == "w-0-0" && false || id == "open" })
	s.WithLock(func() { s.prds[openSession].Story[1].Tasks[2].SessionID = "open" })

	rep, err := s.RunArchive(archiveTestNow, archiveTestRetention)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Archived) != 0 {
		t.Fatalf("archived Automata that are in use: %+v", rep.Archived)
	}
	skipped := map[string]string{}
	for _, sk := range rep.Skipped {
		skipped[sk.ID] = sk.Reason
	}
	for _, id := range []string{running, openSession, unmerged} {
		if skipped[id] == "" {
			t.Fatalf("%s not reported as skipped: %+v", id, rep.Skipped)
		}
	}
	if _, in := skipped[young]; in {
		t.Fatal("a young Automaton is reported as in use")
	}
	for _, id := range []string{running, openSession, unmerged, young} {
		p, _ := s.GetPRD(id)
		if p.Archive != nil {
			t.Fatalf("%s was archived", id)
		}
	}
}

func TestArchiveSecondRunIsIdempotent(t *testing.T) {
	s, dir := newArchiveStore(t)
	a := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	b := fatArchiveFixture(t, s, PRDCancelled, 60*24*time.Hour)
	rep, err := s.RunArchive(archiveTestNow, archiveTestRetention)
	if err != nil || len(rep.Archived) != 2 || len(rep.Failed) != 0 {
		t.Fatalf("first run: %v %+v", err, rep)
	}
	if rep.BackupFiles == 0 || rep.BackupDir == "" {
		t.Fatalf("no backup recorded: %+v", rep)
	}
	snap := snapshotTree(t, dir)
	rep2, err := s.RunArchive(archiveTestNow.Add(time.Hour), archiveTestRetention)
	if err != nil || len(rep2.Archived) != 0 || len(rep2.Eligible) != 0 || rep2.AlreadyArchive != 2 {
		t.Fatalf("second run: %v %+v", err, rep2)
	}
	if got := snapshotTree(t, dir); !reflect.DeepEqual(snap, got) {
		t.Fatal("second run changed the data directory")
	}
	if _, err := s.ArchiveAutomaton(a, archiveTestNow, archiveTestRetention); err == nil {
		t.Fatal("archiving twice must refuse")
	}
	_ = b
}

func TestArchiveBackupHoldsOriginalRecords(t *testing.T) {
	s, _ := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	orig, err := os.ReadFile(filepath.Join(s.dir, prdRecordsDirName, id+".json"))
	if err != nil || len(orig) == 0 {
		t.Fatalf("read record: %v", err)
	}
	rep, err := s.RunArchive(archiveTestNow, archiveTestRetention)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(rep.BackupDir, prdRecordsDirName, id+".json"))
	if err != nil || len(got) == 0 || string(got) != string(orig) {
		t.Fatalf("backup of the record differs from the original: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rep.BackupDir, "MANIFEST.json")); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveCompactRestoreKeepsLaterWrites(t *testing.T) {
	s, _ := newArchiveStore(t)
	id := fatArchiveFixture(t, s, PRDCompleted, 90*24*time.Hour)
	if _, err := s.ArchiveAutomaton(id, archiveTestNow, archiveTestRetention); err != nil {
		t.Fatal(err)
	}
	s.WithLock(func() {
		s.prds[id].Decisions = append(s.prds[id].Decisions, Decision{Kind: "resume", Actor: "operator"})
	})
	if err := s.RestoreFromArchive(id); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetPRD(id)
	if len(p.Decisions) != 201 || p.Decisions[200].Kind != "resume" {
		t.Fatalf("decisions after restore: %d", len(p.Decisions))
	}
}
