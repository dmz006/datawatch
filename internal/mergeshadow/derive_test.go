package mergeshadow

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
)

func rec(paths ...string) *changerecord.ChangeRecord {
	r := &changerecord.ChangeRecord{AutomatonID: "abc1"}
	for _, p := range paths {
		r.Files = append(r.Files, changerecord.FileChange{Path: p, Status: changerecord.StatusModified})
	}
	return r
}

func TestDerivePWAChange(t *testing.T) {
	d, err := Derive(rec("internal/server/web/app.js", "internal/x/y.go"), "v1.2.3", "2026-11-09", "436")
	if err != nil {
		t.Fatal(err)
	}
	if !d.ParityIssueNeeded {
		t.Error("parity_issue_needed should be true")
	}
	if !strings.HasPrefix(d.ReadmeLine, "**Current release: v1.2.3 (2026-11-09).**") {
		t.Errorf("readme line: %q", d.ReadmeLine)
	}
	if !strings.HasPrefix(d.RegistryRow, "| 436 |") {
		t.Errorf("registry row: %q", d.RegistryRow)
	}
}

// A locale-only change under internal/server/web/ is still a PWA change: the
// flag follows the same surface mapping as the Web UI section of the artefacts.
func TestDeriveLocaleOnlyPWAChange(t *testing.T) {
	d, err := Derive(rec("internal/server/web/locales/en.json"), "v1.2.3", "2026-11-09", "")
	if err != nil {
		t.Fatal(err)
	}
	if !d.ParityIssueNeeded {
		t.Error("a locale-only web change should set parity_issue_needed")
	}
}

func TestDeriveNoPWA(t *testing.T) {
	d, err := Derive(rec("internal/x/y.go", "docs/a.md"), "v1.2.3", "2026-11-09", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.ParityIssueNeeded {
		t.Error("parity_issue_needed should be false")
	}
	if !strings.HasPrefix(d.RegistryRow, "| NNN |") {
		t.Errorf("registry row: %q", d.RegistryRow)
	}
}

func TestDeriveNoRecord(t *testing.T) {
	d, err := Derive(nil, "v1.2.3", "2026-11-09", "1")
	if err != nil {
		t.Fatal(err)
	}
	if d != (Derived{}) {
		t.Errorf("want zero Derived, got %+v", d)
	}
}
