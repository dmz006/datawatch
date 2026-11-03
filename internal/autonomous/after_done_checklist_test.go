package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func writeChecklistTree(t *testing.T, mainV, apiV, changelog, readme string) string {
	t.Helper()
	d := t.TempDir()
	w := func(rel, s string) {
		p := filepath.Join(d, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("cmd/datawatch/main.go", "package main\n\nvar Version = \""+mainV+"\"\n")
	w("internal/server/api.go", "package server\n\nvar Version = \""+apiV+"\"\n")
	w("CHANGELOG.md", changelog)
	w("docs/plans/README.md", readme)
	return d
}

func byItem(items []ChecklistItem, name string) ChecklistItem {
	for _, it := range items {
		if it.Item == name {
			return it
		}
	}
	return ChecklistItem{}
}

const clTwo = "# Changelog\n\n## v1.2.0 — new\n\n## v1.1.0 — old\n"
const readmeRow = "| # | Item | Status | Depends on |\n|---|---|---|---|\n| 1 | BL900 thing | done | - |\n"

func TestAfterDoneChecklistAllPass(t *testing.T) {
	d := writeChecklistTree(t, "1.2.0", "1.2.0", clTwo, readmeRow)
	items := RunAfterDoneChecklist(AfterDoneInput{Dir: d, ConfigKeyChanged: true,
		ChangedFiles: []string{"docs/config-reference.yaml"}, ParityRef: "datawatch-app#42", RegistryKey: "BL900"})
	for _, it := range items {
		if it.Outcome != ChecklistPass || it.Reason == "" {
			t.Errorf("%s: want pass with a reason, got %+v", it.Item, it)
		}
	}
	if len(items) != 5 {
		t.Fatalf("want 5 items, got %d", len(items))
	}
}

func TestAfterDoneChecklistFail(t *testing.T) {
	d := writeChecklistTree(t, "1.1.0", "1.0.9", "# Changelog\n\n## v1.0.0\n", readmeRow)
	items := RunAfterDoneChecklist(AfterDoneInput{Dir: d, ConfigKeyChanged: true, RegistryKey: "BL404"})
	for _, name := range []string{ChecklistVersionSync, ChecklistChangelog, ChecklistConfigRef, ChecklistParityIssue, ChecklistRegistryRow} {
		if it := byItem(items, name); it.Outcome != ChecklistFail || it.Reason == "" {
			t.Errorf("%s: want fail with a reason, got %+v", name, it)
		}
	}
}

func TestAfterDoneChecklistVersionNotHigher(t *testing.T) {
	d := writeChecklistTree(t, "1.1.0", "1.1.0", clTwo, readmeRow)
	it := byItem(RunAfterDoneChecklist(AfterDoneInput{Dir: d, PreviousVersion: "1.1.0"}), ChecklistVersionSync)
	if it.Outcome != ChecklistFail || !strings.Contains(it.Reason, "not higher") {
		t.Fatalf("got %+v", it)
	}
}

func TestAfterDoneChecklistNA(t *testing.T) {
	d := writeChecklistTree(t, "1.0.0", "1.0.0", "## v1.0.0\n", readmeRow)
	items := RunAfterDoneChecklist(AfterDoneInput{Dir: d, ParityNotApplicable: true})
	for _, name := range []string{ChecklistVersionSync, ChecklistConfigRef, ChecklistParityIssue, ChecklistRegistryRow} {
		if it := byItem(items, name); it.Outcome != ChecklistNA || it.Reason == "" {
			t.Errorf("%s: want n-a with a reason, got %+v", name, it)
		}
	}
	if it := byItem(items, ChecklistChangelog); it.Outcome != ChecklistPass {
		t.Errorf("changelog: %+v", it)
	}
}

func TestAfterDoneChecklistWritesNothing(t *testing.T) {
	d := writeChecklistTree(t, "1.2.0", "1.2.0", clTwo, readmeRow)
	snap := func() string {
		var sb strings.Builder
		_ = filepath.Walk(d, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				b, _ := os.ReadFile(p)
				sb.WriteString(p + string(b))
			}
			return nil
		})
		return sb.String()
	}
	before := snap()
	RunAfterDoneChecklist(AfterDoneInput{Dir: d, RegistryKey: "BL900"})
	if before != snap() {
		t.Fatal("checklist modified files")
	}
}

func TestRecordAfterDoneChecklistModes(t *testing.T) {
	defer SetChecksConfig(config.AutonomousChecksConfig{})
	d := writeChecklistTree(t, "1.2.0", "1.2.0", clTwo, readmeRow)
	for _, tc := range []struct {
		mode    string
		noted   bool
		alerted bool
	}{{config.ChecksModeOff, false, false}, {config.ChecksModeShadow, true, false}, {config.ChecksModeOn, true, true}} {
		SetChecksConfig(config.AutonomousChecksConfig{AfterDone: tc.mode})
		m := newManagerForPRDLLM(t)
		prd, err := m.CreatePRD("spec", d, "ollama", "", "")
		if err != nil {
			t.Fatal(err)
		}
		m.RecordAfterDoneChecklist(prd, AfterDoneInput{Dir: d}) // parity ref missing => a fail
		s, _ := m.store.SnapshotPRD(prd.ID)
		if (s.Triage != nil && len(s.Triage.Notes) > 0) != tc.noted {
			t.Errorf("%s: noted=%v want %v", tc.mode, s.Triage != nil, tc.noted)
		}
		if tc.noted && (s.Triage.AlertedAt != nil) != tc.alerted {
			t.Errorf("%s: alerted=%v want %v", tc.mode, s.Triage.AlertedAt != nil, tc.alerted)
		}
	}
}
