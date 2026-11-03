package autonomous

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func writeRegistryTree(t *testing.T, readme string, plans ...string) string {
	t.Helper()
	d := t.TempDir()
	pd := filepath.Join(d, "docs", "plans")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if err := os.WriteFile(filepath.Join(pd, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func kinds(fs []RegistryFinding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Kind]++
	}
	return m
}

const regHead = "| # | Item | Status | Depends on |\n|---|---|---|---|\n"

func TestRegistryReadmeClean(t *testing.T) {
	d := writeRegistryTree(t, regHead+"| 1 | [BL1](2026-10-01-a.md) a | done | - |\n| 2 | BL2 no link | done | - |\n", "2026-10-01-a.md")
	fs, err := CheckRegistryReadme(d)
	if err != nil || len(fs) != 0 {
		t.Fatalf("want clean, got %v %v", fs, err)
	}
}

func TestRegistryReadmeMissingRow(t *testing.T) {
	d := writeRegistryTree(t, regHead+"| 1 | [BL1](2026-10-01-a.md) a | done | - |\n", "2026-10-01-a.md", "2026-10-02-orphan.md")
	fs, _ := CheckRegistryReadme(d)
	if kinds(fs)[RegistryMissingRow] != 1 || fs[0].File != "2026-10-02-orphan.md" {
		t.Fatalf("got %+v", fs)
	}
}

func TestRegistryReadmeDuplicateAndMissingFile(t *testing.T) {
	d := writeRegistryTree(t, regHead+"| 1 | [BL1](2026-10-01-a.md) a | done | - |\n| 1 | BL1 again | done | - |\n| 3 | BL3 x | done | - |\n| 4 | [BL4](2026-10-09-gone.md) y | done | - |\n", "2026-10-01-a.md")
	fs, _ := CheckRegistryReadme(d)
	k := kinds(fs)
	if k[RegistryDuplicate] != 2 || k[RegistryMissingFile] != 1 {
		t.Fatalf("got %+v", fs)
	}
}

func TestRegistryReadmeUnreadable(t *testing.T) {
	if _, err := CheckRegistryReadme(t.TempDir()); err == nil {
		t.Fatal("want error for missing README")
	}
}

func TestRegistryReadmeWritesNothing(t *testing.T) {
	d := writeRegistryTree(t, regHead+"| 1 | BL1 | done | - |\n", "2026-10-02-orphan.md")
	_, _ = CheckRegistryReadme(d)
	es, _ := os.ReadDir(filepath.Join(d, "docs", "plans"))
	if len(es) != 2 {
		t.Fatalf("directory changed: %d entries", len(es))
	}
}

func TestRecordRegistryReadmeCheckModes(t *testing.T) {
	defer SetChecksConfig(config.AutonomousChecksConfig{})
	d := writeRegistryTree(t, regHead, "2026-10-02-orphan.md")
	for _, tc := range []struct {
		mode    string
		noted   bool
		alerted bool
	}{{config.ChecksModeOff, false, false}, {config.ChecksModeShadow, true, false}, {config.ChecksModeOn, true, true}} {
		SetChecksConfig(config.AutonomousChecksConfig{RegistryReadme: tc.mode})
		m := newManagerForPRDLLM(t)
		prd, err := m.CreatePRD("spec", d, "ollama", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.RecordRegistryReadmeCheck(prd, d); err != nil {
			t.Fatal(err)
		}
		s, _ := m.store.SnapshotPRD(prd.ID)
		if (s.Triage != nil && len(s.Triage.Notes) > 0) != tc.noted {
			t.Errorf("%s: noted mismatch", tc.mode)
		}
		if tc.noted && (s.Triage.AlertedAt != nil) != tc.alerted {
			t.Errorf("%s: alerted=%v want %v", tc.mode, s.Triage.AlertedAt != nil, tc.alerted)
		}
	}
}
