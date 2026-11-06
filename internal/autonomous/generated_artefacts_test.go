package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

type fakeDiff struct{ err error }

func (f fakeDiff) ChangedFiles() ([]changerecord.FileChange, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []changerecord.FileChange{
		{Path: "internal/foo/foo.go", Status: changerecord.StatusAdded},
		{Path: "internal/foo/foo_test.go", Status: changerecord.StatusAdded},
	}, nil
}

func (f fakeDiff) FileDiff(p string) (changerecord.FileDiffData, error) {
	if strings.HasSuffix(p, "_test.go") {
		return changerecord.FileDiffData{Unified: "+func TestFoo(t *testing.T) {\n"}, nil
	}
	return changerecord.FileDiffData{Unified: "+package foo\n"}, nil
}

var fixedNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func genArtHarness(t *testing.T, s config.EffectiveGeneratedArtefactsSettings, diff fakeDiff) (*Manager, *PRD, *Task) {
	t.Helper()
	st, err := NewStore(filepath.Join(t.TempDir(), "prds.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{store: st, dataDir: t.TempDir()}
	m.SetGeneratedArtefacts(func() config.EffectiveGeneratedArtefactsSettings { return s })
	m.genArt.headRef = func(context.Context, string) string { return "abc1234def" }
	m.genArt.diffSource = func(string, string) changerecord.DiffSource { return diff }
	m.genArt.now = func() time.Time { return fixedNow }
	task := Task{ID: "t1", Status: TaskCompleted, VerificationScript: "scripts/v.sh"}
	prd := &PRD{ID: "prd1", ProjectDir: t.TempDir(), Story: []Story{{Tasks: []Task{task}}}}
	return m, prd, &prd.Story[0].Tasks[0]
}

func allOn() config.EffectiveGeneratedArtefactsSettings {
	return config.GeneratedArtefacts{}.Effective()
}

func TestRecordTaskChange_BuiltAndSavedAtFinish(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{})
	m.noteTaskStart(context.Background(), prd, task)
	m.noteTaskCommand(task.ID, "scripts/v.sh", 0, "ok  \texample/foo\t0.1s\n--- PASS: TestFoo\n--- PASS: TestBar\n--- FAIL: TestBaz\n")
	m.recordTaskChange(prd, task)
	rec, err := changerecord.Load(m.dataDir, "t1")
	if err != nil {
		t.Fatalf("record not saved: %v", err)
	}
	if rec.TaskID != "t1" || rec.AutomatonID != "prd1" || !rec.Timestamp.Equal(fixedNow) {
		t.Errorf("record identity/clock wrong: %+v", rec)
	}
	if len(rec.Files) != 2 || len(rec.TestsAdded) != 1 || rec.TestsAdded[0] != "TestFoo" {
		t.Errorf("record content wrong: %+v", rec)
	}
	if len(rec.Commands) != 1 || rec.Commands[0].Pass != 2 || rec.Commands[0].Fail != 1 || rec.Commands[0].Command != "scripts/v.sh" {
		t.Errorf("commands wrong: %+v", rec.Commands)
	}
}

func TestRecordTaskChange_ErrorDoesNotFailTask(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{err: errors.New("boom")})
	m.noteTaskStart(context.Background(), prd, task)
	m.recordTaskChange(prd, task) // must not panic or alter the task
	if task.Status != TaskCompleted {
		t.Errorf("task status changed to %s", task.Status)
	}
	if _, err := changerecord.Load(m.dataDir, "t1"); err == nil {
		t.Error("no record should be saved on error")
	}
	// No base ref recorded at all is also just logged.
	m2, prd2, task2 := genArtHarness(t, allOn(), fakeDiff{})
	m2.recordTaskChange(prd2, task2)
	if _, err := changerecord.Load(m2.dataDir, "t1"); err == nil {
		t.Error("no record expected without a base ref")
	}
}

func TestRecordTaskChange_OffAndUnwired(t *testing.T) {
	s := allOn()
	s.ChangeRecord = false
	m, prd, task := genArtHarness(t, s, fakeDiff{})
	m.noteTaskStart(context.Background(), prd, task)
	m.recordTaskChange(prd, task)
	if _, err := changerecord.Load(m.dataDir, "t1"); err == nil {
		t.Error("record written although change_record is off")
	}
	m.SetGeneratedArtefacts(nil)
	m.writeGeneratedArtefacts(prd)
	if len(prd.Decisions) != 0 || m.generatedArtefactReviewText(prd) != "" {
		t.Error("unwired feature must be dormant")
	}
}

func completeWithRecord(t *testing.T, m *Manager, prd *PRD, task *Task) {
	t.Helper()
	m.noteTaskStart(context.Background(), prd, task)
	m.recordTaskChange(prd, task)
	m.writeGeneratedArtefacts(prd)
}

func TestWriteGeneratedArtefacts_WrittenAtCompletion(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{})
	completeWithRecord(t, m, prd, task)
	rec := &changerecord.ChangeRecord{AutomatonID: "prd1"}
	opts := genartifacts.Options{Root: prd.ProjectDir}
	for _, k := range genartifacts.Kinds() {
		p := filepath.Join(prd.ProjectDir, genartifacts.TrackedPath(k, rec, opts))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not written: %v", k, err)
		}
	}
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == generatedArtefactDecisionKind && strings.Contains(d.Note, "written") {
			n++
		}
	}
	if n != len(genartifacts.Kinds()) {
		t.Errorf("want %d result lines, got %d: %+v", len(genartifacts.Kinds()), n, prd.Decisions)
	}
}

func TestWriteGeneratedArtefacts_DisabledKindWritesNothing(t *testing.T) {
	s := allOn()
	s.ParityDraft = false
	m, prd, task := genArtHarness(t, s, fakeDiff{})
	completeWithRecord(t, m, prd, task)
	rec := &changerecord.ChangeRecord{AutomatonID: "prd1"}
	opts := genartifacts.Options{Root: prd.ProjectDir}
	if _, err := os.Stat(filepath.Join(prd.ProjectDir, genartifacts.TrackedPath(genartifacts.ParityDraft, rec, opts))); err == nil {
		t.Error("disabled parity draft was written")
	}
	if _, err := os.Stat(filepath.Join(prd.ProjectDir, genartifacts.TrackedPath(genartifacts.ChangelogDraft, rec, opts))); err != nil {
		t.Error("enabled changelog draft missing")
	}
	for _, d := range prd.Decisions {
		if strings.HasPrefix(d.Note, string(genartifacts.ParityDraft)+":") {
			t.Errorf("disabled kind has a result line: %q", d.Note)
		}
	}
}

func TestWriteGeneratedArtefacts_EditPreservedOnSecondCompletion(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{})
	completeWithRecord(t, m, prd, task)
	rec := &changerecord.ChangeRecord{AutomatonID: "prd1"}
	p := filepath.Join(prd.ProjectDir, genartifacts.TrackedPath(genartifacts.ChangelogDraft, rec, genartifacts.Options{}))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(b) + "\nA person wrote this line.\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	m.writeGeneratedArtefacts(prd) // second completion
	after, _ := os.ReadFile(p)
	if string(after) != edited {
		t.Errorf("edited artefact was overwritten:\n%s", after)
	}
	skipped := false
	for _, d := range prd.Decisions {
		if strings.HasPrefix(d.Note, "changelog_draft: skipped") {
			skipped = true
		}
	}
	if !skipped {
		t.Error("second completion should record a skipped line for the edited file")
	}
}

func TestRulesReviewSuffix_ListsGeneratedArtefactsAsDocumentation(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{})
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Before generation every enabled kind is expected but missing.
	before := m.rulesReviewSuffix(context.Background(), prd)
	if !strings.Contains(before, "is a finding") || !strings.Contains(before, "none exist yet") {
		t.Errorf("missing artefacts not flagged: %q", before)
	}
	completeWithRecord(t, m, prd, task)
	got := m.rulesReviewSuffix(context.Background(), prd)
	for _, want := range []string{"prd1-changelog.md", "prd1-registry-row.md", "documentation checks", "changelog, parity, tracker and registry"} {
		if !strings.Contains(got, want) {
			t.Errorf("suffix lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "expected but missing") {
		t.Errorf("nothing should be missing after generation: %q", got)
	}
}

func TestRulesReviewSuffix_DisabledKindNotAFinding(t *testing.T) {
	s := allOn()
	s.RegistryRow = false
	m, prd, _ := genArtHarness(t, s, fakeDiff{})
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.rulesReviewSuffix(context.Background(), prd)
	if strings.Contains(got, "registry-row") {
		t.Errorf("disabled kind must not be listed or expected: %q", got)
	}
	if !strings.Contains(got, "switched off is not a finding") {
		t.Errorf("suffix should say disabled kinds are not findings: %q", got)
	}
}

func TestParseTestCounts(t *testing.T) {
	cases := []struct {
		name, out  string
		pass, fail int
	}{
		{"go verbose", "--- PASS: A\n    --- PASS: A/b\n--- FAIL: C\n", 2, 1},
		{"go packages", "ok  \ta\t0.1s\nok  \tb\t0.2s\nFAIL\tc\t0.3s\n", 2, 1},
		{"node", "# tests 5\n# pass 4\n# fail 1\n", 4, 1},
		{"node no fail line", "# pass 3\n", 3, 0},
		{"jest", "Tests:       1 failed, 3 passed, 4 total\n", 3, 1},
		{"unknown", "something else entirely", 0, 0},
		{"empty", "", 0, 0},
	}
	for _, c := range cases {
		p, f := parseTestCounts(c.out)
		if p != c.pass || f != c.fail {
			t.Errorf("%s: got %d/%d want %d/%d", c.name, p, f, c.pass, c.fail)
		}
	}
}

func TestNoteTaskStart_KeepsFirstBaseAndIgnoresEmpty(t *testing.T) {
	m, prd, task := genArtHarness(t, allOn(), fakeDiff{})
	calls := 0
	m.genArt.headRef = func(context.Context, string) string { calls++; return "sha" + string(rune('0'+calls)) + "xxxxx" }
	m.noteTaskStart(context.Background(), prd, task)
	m.noteTaskStart(context.Background(), prd, task)
	if m.genArt.bases["t1"] != "sha1xxxxx" || calls != 1 {
		t.Errorf("base should be kept from first start: %v calls=%d", m.genArt.bases, calls)
	}
	m.noteTaskCommand("", "x", 0, "")
	m.noteTaskCommand("t1", "", 0, "")
	if len(m.genArt.cmds) != 0 {
		t.Error("empty command or task id must be ignored")
	}
}
