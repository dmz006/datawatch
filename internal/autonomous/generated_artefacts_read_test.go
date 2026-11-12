package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// readHarness stores a PRD with a merged record under the PRD id.
func readHarness(t *testing.T, s config.EffectiveGeneratedArtefactsSettings, withRecord bool) (*Manager, *PRD) {
	t.Helper()
	m, prd, _ := genArtHarness(t, s, fakeDiff{})
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if withRecord {
		rec := &changerecord.ChangeRecord{AutomatonID: prd.ID, Files: []changerecord.FileChange{{Path: "internal/foo/foo.go", Status: changerecord.StatusAdded}}}
		if err := changerecord.Save(m.dataDir, prd.ID, rec); err != nil {
			t.Fatal(err)
		}
	}
	return m, prd
}

func TestGeneratedRead_ChangeRecord(t *testing.T) {
	m, prd := readHarness(t, allOn(), true)
	rec, err := m.GeneratedChangeRecord(prd.ID)
	if err != nil || rec.AutomatonID != prd.ID || len(rec.Files) != 1 {
		t.Fatalf("record = %+v, err = %v", rec, err)
	}
	if _, err := m.GeneratedChangeRecord("nope"); !errors.Is(err, ErrGeneratedPRDNotFound) {
		t.Errorf("unknown id err = %v, want ErrGeneratedPRDNotFound", err)
	}
	m2, prd2 := readHarness(t, allOn(), false)
	if _, err := m2.GeneratedChangeRecord(prd2.ID); !errors.Is(err, ErrGeneratedNoChangeData) {
		t.Errorf("no record err = %v, want ErrGeneratedNoChangeData", err)
	}
}

func TestGeneratedRead_ChangeRecordMergesTaskRecords(t *testing.T) {
	m, prd := readHarness(t, allOn(), false)
	if err := changerecord.Save(m.dataDir, "t1", &changerecord.ChangeRecord{TaskID: "t1", AutomatonID: prd.ID,
		Files: []changerecord.FileChange{{Path: "a.go", Status: changerecord.StatusAdded}}}); err != nil {
		t.Fatal(err)
	}
	rec, err := m.GeneratedChangeRecord(prd.ID)
	if err != nil || len(rec.Files) != 1 || rec.AutomatonID != prd.ID {
		t.Fatalf("merged = %+v, err = %v", rec, err)
	}
}

func TestGeneratedRead_PreviewDoesNotWrite(t *testing.T) {
	m, prd := readHarness(t, allOn(), true)
	pv, err := m.PreviewGeneratedArtefact(prd.ID, "gate_report")
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Enabled || pv.Content == "" || !strings.HasSuffix(pv.Path, prd.ID+"-gate.md") {
		t.Fatalf("preview = %+v", pv)
	}
	if _, err := os.Stat(filepath.Join(prd.ProjectDir, filepath.FromSlash(pv.Path))); !os.IsNotExist(err) {
		t.Errorf("preview wrote the file (stat err = %v)", err)
	}
	if _, err := m.PreviewGeneratedArtefact(prd.ID, "bogus"); !errors.Is(err, ErrGeneratedKindUnknown) {
		t.Errorf("unknown kind err = %v", err)
	}
	if _, err := m.PreviewGeneratedArtefact("nope", "gate_report"); !errors.Is(err, ErrGeneratedPRDNotFound) {
		t.Errorf("unknown id err = %v", err)
	}
}

func TestGeneratedRead_PreviewDisabledKind(t *testing.T) {
	s := allOn()
	s.GateReport = false
	m, prd := readHarness(t, s, true)
	pv, err := m.PreviewGeneratedArtefact(prd.ID, "gate_report")
	if err != nil || pv.Enabled || pv.Content != "" {
		t.Fatalf("disabled preview = %+v, err = %v", pv, err)
	}
	res, err := m.RegenerateGeneratedArtefact(prd.ID, "gate_report")
	if err != nil || res.Status != "disabled" {
		t.Fatalf("disabled regenerate = %+v, err = %v", res, err)
	}
}

func TestGeneratedRead_RegenerateWritesThenSkipsEditedFile(t *testing.T) {
	m, prd := readHarness(t, allOn(), true)
	res, err := m.RegenerateGeneratedArtefact(prd.ID, "changelog_draft")
	if err != nil || res.Status != "written" {
		t.Fatalf("first regenerate = %+v, err = %v", res, err)
	}
	full := filepath.Join(prd.ProjectDir, filepath.FromSlash(res.Path))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	// Re-running over an unchanged file is allowed.
	if res, err = m.RegenerateGeneratedArtefact(prd.ID, "changelog_draft"); err != nil || res.Status != "written" {
		t.Fatalf("second regenerate = %+v, err = %v", res, err)
	}
	// A person's edit is kept.
	if err := os.WriteFile(full, append(b, []byte("\nhand-written line\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = m.RegenerateGeneratedArtefact(prd.ID, "changelog_draft")
	if err != nil || res.Status != "skipped" || res.Reason == "" {
		t.Fatalf("edited regenerate = %+v, err = %v", res, err)
	}
	after, _ := os.ReadFile(full)
	if !strings.Contains(string(after), "hand-written line") {
		t.Error("edited file was overwritten")
	}
}

func TestWriteGeneratedArtefacts_SavesMergedRecordUnderPRDID(t *testing.T) {
	m, prd, _ := genArtHarness(t, allOn(), fakeDiff{})
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if err := changerecord.Save(m.dataDir, "t1", &changerecord.ChangeRecord{TaskID: "t1", AutomatonID: prd.ID,
		Files: []changerecord.FileChange{{Path: "a.go", Status: changerecord.StatusAdded}}}); err != nil {
		t.Fatal(err)
	}
	m.writeGeneratedArtefacts(prd)
	rec, err := changerecord.Load(m.dataDir, prd.ID)
	if err != nil || len(rec.Files) != 1 {
		t.Fatalf("merged record not stored under prd id: %+v, %v", rec, err)
	}
}
