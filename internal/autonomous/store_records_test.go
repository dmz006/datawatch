package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// persistedPRDRecords returns the concatenated record files of the store at
// dataDir (sorted by name), or the old prds.jsonl when no record exists. It
// stands in for "the bytes on disk" in tests that check what was persisted.
func persistedPRDRecords(t *testing.T, dataDir string) string {
	t.Helper()
	root := filepath.Join(dataDir, "autonomous")
	var sb strings.Builder
	entries, _ := os.ReadDir(filepath.Join(root, prdRecordsDirName))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, prdRecordsDirName, e.Name()))
		if err != nil {
			t.Fatalf("read record %s: %v", e.Name(), err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	if sb.Len() == 0 {
		if b, err := os.ReadFile(filepath.Join(root, prdLegacyFile)); err == nil {
			return string(b)
		}
	}
	return sb.String()
}

// seedRecordStore fills a store with n PRDs, each with stories x tasks tasks
// that carry judge text of judgeBytes. Returns the store and the PRD ids.
func seedRecordStore(t *testing.T, dir string, n, stories, tasks, judgeBytes int) (*Store, []string) {
	t.Helper()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	judge := strings.Repeat("verifier output line\n", judgeBytes/21+1)[:judgeBytes]
	var ids []string
	for i := 0; i < n; i++ {
		p, err := s.CreatePRD(fmt.Sprintf("spec %d", i), "/p", "", "", "")
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		var sts []Story
		for a := 0; a < stories; a++ {
			st := Story{ID: fmt.Sprintf("%s-s%d", p.ID, a), PRDID: p.ID, Title: "story", Status: StoryPending}
			for b := 0; b < tasks; b++ {
				st.Tasks = append(st.Tasks, Task{
					ID: fmt.Sprintf("%s-s%d-t%d", p.ID, a, b), StoryID: st.ID, PRDID: p.ID,
					Title: "task", Spec: "do the thing", Status: TaskPending,
					Verification: &VerificationResult{OK: true, Summary: "ok", RawJudge: judge},
				})
			}
			sts = append(sts, st)
		}
		if err := s.SetStories(p.ID, sts); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
		ids = append(ids, p.ID)
	}
	return s, ids
}

func recordSeedSize() (n, stories, tasks, judge int) {
	if os.Getenv("STORE_BENCH_SMALL") == "1" || testing.Short() {
		return 30, 3, 4, 20 << 10
	}
	return 300, 5, 6, 40 << 10
}

func TestPRDStoreIncremental(t *testing.T) {
	n, stories, tasks, judge := recordSeedSize()
	dir := t.TempDir()
	s, ids := seedRecordStore(t, dir, n, stories, tasks, judge)

	root := filepath.Join(dir, "autonomous")
	var total int64
	mtimes := map[string]time.Time{}
	entries, err := os.ReadDir(filepath.Join(root, prdRecordsDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			fi, _ := e.Info()
			total += fi.Size()
			mtimes[e.Name()] = fi.ModTime()
		}
	}
	if len(mtimes) != n {
		t.Fatalf("%d record files, want %d", len(mtimes), n)
	}
	for _, name := range []string{prdLegacyFile, "learnings.jsonl", "tags.jsonl"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Fatalf("%s exists; PRD writes must not touch it", name)
		}
	}
	prd, _ := s.GetPRD(ids[0])
	taskID := prd.Story[0].Tasks[0].ID
	oneRec, _, _, _ := s.encodePRDRecord(prd)

	_, before, writesBefore, _ := s.RecordStats()
	time.Sleep(20 * time.Millisecond)
	if !s.MutateTask(taskID, func(tk *Task) { tk.Status = TaskInProgress }) {
		t.Fatal("MutateTask: task not found")
	}
	_, after, writesAfter, _ := s.RecordStats()
	wrote := after - before

	// Whole-store rewrite would write every PRD with its text inline.
	full := int64(0)
	for _, id := range ids {
		p, _ := s.GetPRD(id)
		b, _ := json.Marshal(p)
		full += int64(len(b))
	}
	t.Logf("PRDs=%d record bytes=%d inline PRD bytes (old full rewrite)=%d one change wrote %d bytes in %d file(s); one record=%d",
		n, total, full, wrote, writesAfter-writesBefore, len(oneRec))
	if writesAfter-writesBefore != 1 {
		t.Fatalf("one task change wrote %d files, want 1", writesAfter-writesBefore)
	}
	if wrote > int64(len(oneRec))*11/10+64 {
		t.Fatalf("wrote %d bytes, want close to one record (%d)", wrote, len(oneRec))
	}
	if wrote*50 > full {
		t.Fatalf("wrote %d bytes of %d: not incremental", wrote, full)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fi, _ := os.Stat(filepath.Join(root, prdRecordsDirName, e.Name()))
		changed := !fi.ModTime().Equal(mtimes[e.Name()])
		if changed != (e.Name() == ids[0]+".json") {
			t.Fatalf("record %s changed=%v", e.Name(), changed)
		}
	}

	// A change that leaves the record identical writes nothing.
	s.MutateTask(taskID, func(tk *Task) {})
	// (MutateTask bumps UpdatedAt, so a write is expected here; use a PRD
	// no-op instead.)
	_, b1, w1, _ := s.RecordStats()
	s.MutatePRD(ids[1], func(p *PRD) bool { return false })
	_, b2, w2, _ := s.RecordStats()
	if b1 != b2 || w1 != w2 {
		t.Fatalf("no-op MutatePRD wrote %d bytes", b2-b1)
	}

	// Learnings are a separate file and a PRD change must not create it.
	if err := s.AddLearning(Learning{}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(root, "learnings.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.MutateTask(taskID, func(tk *Task) { tk.RetryCount++ })
	st2, _ := os.Stat(filepath.Join(root, "learnings.jsonl"))
	if !st2.ModTime().Equal(st.ModTime()) || st2.Size() != st.Size() {
		t.Fatal("learnings.jsonl rewritten by a PRD change")
	}

	// Reload sees the change.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.TaskSnapshot(taskID)
	if !ok || got.Status != TaskInProgress || got.RetryCount != 1 {
		t.Fatalf("reloaded task = %+v ok=%v", got, ok)
	}
	if got.Verification == nil || len(got.Verification.RawJudge) != judge {
		t.Fatalf("reloaded RawJudge length wrong")
	}
}

func TestPRDStoreMigrateOldFormat(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "autonomous")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("héllo wörld ", 20000) // > 64 KB, multi-byte
	var lines []string
	want := map[string]*PRD{}
	for i := 0; i < 6; i++ {
		p := &PRD{
			ID: fmt.Sprintf("mig%05d", i), Spec: "spec", Status: PRDDraft, Rev: i,
			CreatedAt: time.Unix(1700000000, 0).UTC(), UpdatedAt: time.Unix(1700000000, 0).UTC(),
			Story: []Story{{ID: fmt.Sprintf("st%d", i), PRDID: fmt.Sprintf("mig%05d", i), Tasks: []Task{
				{ID: fmt.Sprintf("tk%d", i), PRDID: fmt.Sprintf("mig%05d", i), Spec: "s", Error: big,
					Verification: &VerificationResult{RawJudge: "judge"}},
			}}},
		}
		if i == 3 {
			p.ID = "odd id/with.chars"
			p.Story[0].PRDID = p.ID
		}
		b, _ := json.Marshal(p)
		lines = append(lines, string(b))
		want[p.ID] = p
	}
	old := strings.Join(lines, "\n") + "\n"
	legacy := filepath.Join(root, prdLegacyFile)
	if err := os.WriteFile(legacy, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// A backup from an earlier run must never be overwritten.
	bak := legacy + prdBackupSuffix
	if err := os.WriteFile(bak, []byte("earlier backup"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if active, _, _, _ := s.RecordStats(); !active {
		t.Fatal("record layout not active after migration")
	}
	if len(s.ListPRDs()) != len(want) {
		t.Fatalf("%d PRDs after migration, want %d", len(s.ListPRDs()), len(want))
	}
	for id, w := range want {
		g, ok := s.SnapshotPRD(id)
		if !ok {
			t.Fatalf("PRD %q lost", id)
		}
		if g.Story[0].Tasks[0].Error != big || g.Rev != w.Rev || g.Story[0].Tasks[0].Verification.RawJudge != "judge" {
			t.Fatalf("PRD %q changed by migration", id)
		}
		if _, ok := s.GetTask(w.Story[0].Tasks[0].ID); !ok {
			t.Fatalf("task index for %q not rebuilt", id)
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("prds.jsonl still present: %v", err)
	}
	if b, _ := os.ReadFile(bak); string(b) != "earlier backup" {
		t.Fatalf("existing backup overwritten: %q", b)
	}
	if b, err := os.ReadFile(bak + ".1"); err != nil || string(b) != old {
		t.Fatalf("new backup missing or different: %v", err)
	}
	// Reopen: reads the records, makes no further backup.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.ListPRDs()) != len(want) {
		t.Fatalf("reopen: %d PRDs", len(s2.ListPRDs()))
	}
	if _, err := os.Stat(bak + ".2"); err == nil {
		t.Fatal("second backup taken on reopen")
	}
	// An old file next to the layout (rollback and upgrade again) is imported
	// without replacing existing records.
	extra := &PRD{ID: "late0001", Spec: "late", Status: PRDDraft}
	other := &PRD{ID: "mig00000", Spec: "stale copy", Status: PRDDraft}
	b1, _ := json.Marshal(extra)
	b2, _ := json.Marshal(other)
	if err := os.WriteFile(legacy, []byte(string(b1)+"\n"+string(b2)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s3, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := s3.GetPRD("late0001"); !ok || p.Spec != "late" {
		t.Fatal("PRD only in the old file not imported")
	}
	if p, _ := s3.GetPRD("mig00000"); p.Spec != "spec" {
		t.Fatalf("record replaced by old file: %q", p.Spec)
	}
	s4, _ := NewStore(dir)
	if _, ok := s4.GetPRD("late0001"); !ok {
		t.Fatal("imported PRD not persisted as a record")
	}
}

func TestPRDStoreSideFile(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSideFileCap(1000)
	s.SetSideFileMax(5000)
	p, _ := s.CreatePRD("spec", "/p", "", "", "")
	small := strings.Repeat("a", 200)
	large := strings.Repeat("ü", 1400) // 2800 bytes, over the cap
	huge := strings.Repeat("日本", 4000) // 24000 bytes, over the hard limit
	lookalike := sideRefPrefix + "not-a-real-file"
	err = s.SetStories(p.ID, []Story{{ID: "s1", PRDID: p.ID, Tasks: []Task{
		{ID: "t1", PRDID: p.ID, StoryID: "s1", Title: small, Spec: "x", Error: large,
			Verification: &VerificationResult{RawJudge: huge}},
		{ID: "t2", PRDID: p.ID, StoryID: "s1", Title: lookalike, Spec: "x"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	filesDir := filepath.Join(dir, "autonomous", prdRecordsDirName, p.ID+".files")
	side, err := os.ReadDir(filesDir)
	if err != nil || len(side) != 3 {
		t.Fatalf("side files = %v, %v; want 3 (large, truncated huge, lookalike)", len(side), err)
	}
	rec := persistedPRDRecords(t, dir)
	if strings.Contains(rec, large) || strings.Contains(rec, small) == false {
		t.Fatal("large text inline in the record, or small text moved out")
	}
	if !strings.Contains(rec, sideRefPrefix+p.ID+".files/") {
		t.Fatal("record has no side file reference")
	}
	if _, _, _, trunc := s.RecordStats(); trunc != 1 {
		t.Fatalf("truncated = %d, want 1", trunc)
	}
	// In memory the PRD is untouched.
	if live, _ := s.GetPRD(p.ID); live.Story[0].Tasks[0].Verification.RawJudge != huge {
		t.Fatal("live PRD altered by writing")
	}

	check := func(s *Store) {
		t.Helper()
		got, ok := s.SnapshotPRD(p.ID)
		if !ok {
			t.Fatal("PRD missing")
		}
		t1 := got.Story[0].Tasks[0]
		if t1.Title != small || t1.Error != large {
			t.Fatal("small or large text did not round trip")
		}
		j := t1.Verification.RawJudge
		if len(j) > 5000 || !strings.HasPrefix(j, huge[:1000]) || !strings.Contains(j, "\n[datawatch: truncated, ") || !strings.HasSuffix(j, "byte limit]") {
			t.Fatalf("truncation marker wrong: len=%d tail=%q", len(j), j[len(j)-80:])
		}
		if !strings.Contains(j[:strings.Index(j, "\n[datawatch")], "日") {
			t.Fatal("cut text lost its content")
		}
		if got.Story[0].Tasks[1].Title != lookalike {
			t.Fatalf("lookalike text changed: %q", got.Story[0].Tasks[1].Title)
		}
	}
	// Reload (with default cap) reads the same text.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(s2)

	// Unchanged text is not rewritten; changed text replaces its file.
	s2.SetSideFileCap(1000)
	s2.SetSideFileMax(5000)
	_, b0, w0, _ := s2.RecordStats()
	s2.MutateTask("t2", func(tk *Task) { tk.Status = TaskInProgress })
	_, b1, w1, _ := s2.RecordStats()
	if w1-w0 != 1 || b1-b0 > 3000 {
		t.Fatalf("mutation wrote %d files %d bytes; side files should be reused", w1-w0, b1-b0)
	}
	s2.MutateTask("t1", func(tk *Task) { tk.Error = strings.Repeat("z", 1500) })
	side, _ = os.ReadDir(filesDir)
	if len(side) != 3 {
		t.Fatalf("side files after change = %d, want 3 (old one removed, new added)", len(side))
	}
	s2.MutateTask("t1", func(tk *Task) { tk.Error = "short" })
	side, _ = os.ReadDir(filesDir)
	if len(side) != 2 {
		t.Fatalf("side files after shrink = %d, want 2", len(side))
	}

	// A lost side file reads as a visible marker, not a failed start.
	for _, e := range side {
		_ = os.Remove(filepath.Join(filesDir, e.Name()))
	}
	s3, err := NewStore(dir)
	if err != nil {
		t.Fatalf("open with missing side files: %v", err)
	}
	g, _ := s3.GetPRD(p.ID)
	if !strings.Contains(g.Story[0].Tasks[0].Verification.RawJudge, "side file missing") {
		t.Fatal("missing side file not marked")
	}
}

func TestPRDStoreConcurrent(t *testing.T) {
	dir := t.TempDir()
	s, ids := seedRecordStore(t, dir, 8, 2, 3, 6<<10)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				id := ids[(w+i)%len(ids)]
				taskID := fmt.Sprintf("%s-s%d-t%d", id, i%2, i%3)
				switch i % 5 {
				case 0:
					s.MutateTask(taskID, func(tk *Task) { tk.RetryCount++ })
				case 1:
					s.MutatePRD(id, func(p *PRD) bool { p.Title = fmt.Sprintf("w%d-%d", w, i); return true })
				case 2:
					if p, ok := s.SnapshotPRD(id); ok {
						_ = p.Story[0].Tasks[0].Verification.RawJudge
					}
				case 3:
					if tk, ok := s.TaskSnapshot(taskID); ok {
						tk.Spec = "copy"
					}
					_ = s.CommitPRDIfRev(id, -1, func(*PRD) bool { return true })
				case 4:
					if p, err := s.CreatePRD("extra", "/p", "", "", ""); err == nil && i%2 == 0 {
						_ = s.DeletePRD(p.ID)
					}
				}
			}
		}(w)
	}
	wg.Wait()

	want := map[string]string{}
	for _, p := range s.ListPRDs() {
		b, _ := json.Marshal(p)
		want[p.ID] = string(b)
	}
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.ListPRDs()
	if len(got) != len(want) {
		t.Fatalf("reloaded %d PRDs, want %d", len(got), len(want))
	}
	for _, p := range got {
		b, _ := json.Marshal(p)
		if want[p.ID] != string(b) {
			t.Fatalf("PRD %s differs after reload", p.ID)
		}
	}
	ids2, _ := s2.recordIDs()
	if len(ids2) != len(want) {
		t.Fatalf("%d record files for %d PRDs (deleted PRDs must lose their record)", len(ids2), len(want))
	}
}

// TestPRDStoreCrashHelper is the writer process of TestPRDStoreCrashSafety.
func TestPRDStoreCrashHelper(t *testing.T) {
	dir := os.Getenv("DW_PRD_CRASH_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if existing := s.ListPRDs(); len(existing) > 0 {
		for _, p := range existing {
			ids = append(ids, p.ID)
		}
	} else {
		s, ids = seedRecordStore(t, dir, 12, 2, 2, 1<<10)
	}
	s.SetSideFileCap(1 << 10)
	_ = os.WriteFile(filepath.Join(dir, "ready"), []byte("1"), 0o644)
	for gen := 0; ; gen++ {
		for _, id := range ids {
			text := fmt.Sprintf("gen-%08d-", gen) + strings.Repeat("x", 200<<10) + "-end"
			s.MutateTask(id+"-s0-t0", func(tk *Task) { tk.Verification.RawJudge = text })
		}
	}
}

func TestPRDStoreCrashSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocesses")
	}
	dir := t.TempDir()
	for round := 0; round < 5; round++ {
		_ = os.Remove(filepath.Join(dir, "ready"))
		cmd := exec.Command(os.Args[0], "-test.run=^TestPRDStoreCrashHelper$")
		cmd.Env = append(os.Environ(), "DW_PRD_CRASH_DIR="+dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatal("helper never became ready")
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(time.Duration(30+round*37) * time.Millisecond)
		_ = cmd.Process.Signal(syscall.SIGKILL)
		_ = cmd.Wait()

		// The killed writer's leftovers must be old enough to count as stale.
		recDir := filepath.Join(dir, "autonomous", prdRecordsDirName)
		if entries, err := os.ReadDir(recDir); err == nil {
			old := time.Now().Add(-2 * time.Hour)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".tmp-") {
					_ = os.Chtimes(filepath.Join(recDir, e.Name()), old, old)
				}
			}
		}

		s, err := NewStore(dir)
		if err != nil {
			t.Fatalf("round %d: reload: %v", round, err)
		}
		prds := s.ListPRDs()
		if len(prds) != 12 {
			t.Fatalf("round %d: %d PRDs after kill, want 12", round, len(prds))
		}
		for _, p := range prds {
			j := p.Story[0].Tasks[0].Verification.RawJudge
			orig := len(j) == 1<<10 // seed text, not yet mutated this round
			if !orig && (!strings.HasPrefix(j, "gen-") || !strings.HasSuffix(j, "-end") || len(j) != len("gen-00000000-")+200<<10+len("-end")) {
				t.Fatalf("round %d: PRD %s judge text torn (len %d)", round, p.ID, len(j))
			}
		}
		// Stale temp files from the kill are cleaned on open.
		entries, _ := os.ReadDir(filepath.Join(dir, "autonomous", prdRecordsDirName))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tmp-") {
				t.Fatalf("round %d: temp file %s left after open", round, e.Name())
			}
		}
	}
}

// A record name that is not a single safe path element must be refused before
// anything is removed, so a name carrying a slash or '..' cannot reach a file
// outside the records directory.
func TestRemoveRecordFilesRefusesUnsafeName(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.recordsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(s.recordsDir()), "victim")
	if err := os.MkdirAll(outside+".files", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside+".json", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../victim", "a/b", `a\b`, "..%", "x\x00y", "/etc/passwd"} {
		if err := s.removeRecordFiles(name); err == nil {
			t.Errorf("removeRecordFiles(%q) = nil, want a refusal", name)
		}
	}
	if _, err := os.Stat(outside + ".files"); err != nil {
		t.Errorf("side-file directory outside the store was touched: %v", err)
	}
	if _, err := os.Stat(outside + ".json"); err != nil {
		t.Errorf("record file outside the store was touched: %v", err)
	}
}

// A PRD id with a slash or '..' is stored under a hashed name, so removing its
// files stays inside the records directory.
func TestRemovePRDFilesUnsafeIDStaysInStore(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.recordsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(s.recordsDir()), "victim.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../victim", "a/../../victim", "a/b"} {
		if err := s.removePRDFiles(id); err != nil {
			t.Errorf("removePRDFiles(%q): %v", id, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside the store was removed: %v", err)
	}
}

// A PRD id used as a scratch directory name must be one safe path element:
// none of the scratch helpers may create, move or remove anything for an id
// with a slash or '..'.
func TestScratchHelpersRefuseUnsafePRDID(t *testing.T) {
	dataDir := t.TempDir()
	victimDir := filepath.Join(dataDir, "autonomous", "victim")
	if err := os.MkdirAll(victimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(victimDir, decomposeOutputName)
	if err := os.WriteFile(victim, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"..", "../victim", "a/../../victim", "a/b", `a\b`, "x\x00y"} {
		if _, err := ScratchDir(dataDir, id); err == nil {
			t.Errorf("ScratchDir(%q) = nil error, want a refusal", id)
		}
		if _, err := MarkDecomposeOutputArtifactFailed(dataDir, id); err == nil {
			t.Errorf("MarkDecomposeOutputArtifactFailed(%q) = nil error, want a refusal", id)
		}
		RemoveDecomposeOutputArtifact(dataDir, id)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("file outside the scratch directory was touched: %v", err)
	}
	if _, err := os.Stat(victim + ".failed"); err == nil {
		t.Errorf("file outside the scratch directory was renamed")
	}
}

// A normal PRD id still gets its scratch directory and its decompose output
// is removed.
func TestScratchHelpersAcceptSafePRDID(t *testing.T) {
	dataDir := t.TempDir()
	dir, err := ScratchDir(dataDir, "prd-1_a")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, decomposeOutputName)
	if err := os.WriteFile(out, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	RemoveDecomposeOutputArtifact(dataDir, "prd-1_a")
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("decompose output still present: %v", err)
	}
}
