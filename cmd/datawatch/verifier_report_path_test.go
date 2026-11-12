package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestReadOnlyReportPresent(t *testing.T) {
	const good = ".datawatch/reports/r.md"
	tests := []struct {
		name     string
		readOnly bool
		path     string
		setup    func(t *testing.T, root string)
		want     bool
	}{
		{"present non-empty accepted", true, good, func(t *testing.T, root string) { writeReport(t, root, good, "findings") }, true},
		{"empty refused", true, good, func(t *testing.T, root string) { writeReport(t, root, good, "") }, false},
		{"missing refused", true, good, func(t *testing.T, root string) {}, false},
		{"non-read-only ignored", false, good, func(t *testing.T, root string) { writeReport(t, root, good, "x") }, false},
		{"outside reports dir refused", true, "docs/r.md", func(t *testing.T, root string) { writeReport(t, root, "docs/r.md", "x") }, false},
		{"dotdot refused", true, ".datawatch/reports/../../x.md", func(t *testing.T, root string) { writeReport(t, root, "x.md", "x") }, false},
		{"symlink refused", true, good, func(t *testing.T, root string) {
			target := filepath.Join(t.TempDir(), "outside.md")
			if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".datawatch/reports"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, good)); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"directory refused", true, good, func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, good), 0o755); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)
			task := &autonomouspkg.Task{ReadOnly: tt.readOnly, ReportPath: tt.path}
			got, _ := readOnlyReportPresent(task, root)
			if got != tt.want {
				t.Fatalf("present = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadOnlyReportVerdictSummary(t *testing.T) {
	const p = ".datawatch/reports/r.md"
	ok, handled := verdictForReadOnlyReport(true, p)
	if !handled || !ok.OK || !strings.Contains(ok.Summary, p) {
		t.Fatalf("present verdict = %+v handled=%v", ok, handled)
	}
	bad, handled := verdictForReadOnlyReport(false, p)
	if !handled || bad.OK || !strings.Contains(bad.Summary, p) || !strings.Contains(bad.Summary, "missing or empty") {
		t.Fatalf("missing verdict = %+v", bad)
	}
	if _, handled := verdictForReadOnlyReport(false, ""); handled {
		t.Fatal("no declared report must fall through")
	}
}

func writeReport(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
