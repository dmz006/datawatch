// BL406 Phase 1 — one passing + one deliberately-failing fixture per
// rule type, per the plan's own checklist.

package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRulesScanner_ContentRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task_spec.md", "Update the howto doc with the new wording.")
	rule := ProjectRule{ID: "r1", Name: "no-code-in-doc-task", Type: RuleTypeContent,
		Pattern: "task_spec.md|(?i)implement |write code for"}

	// Passing: no forbidden phrase present.
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}

	// Failing: forbidden phrase present.
	writeFile(t, dir, "task_spec.md", "Implement the new feature in main.go.")
	findings = ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if findings[0].RuleID != "r1" || findings[0].File != "task_spec.md" {
		t.Fatalf("finding fields wrong: %+v", findings[0])
	}
}

func TestProjectRulesScanner_ConsistencyRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", `var Version = "1.2.3"`)
	writeFile(t, dir, "b.go", `var Version = "1.2.3"`)
	rule := ProjectRule{ID: "r2", Name: "version-sync", Type: RuleTypeConsistency,
		Pattern: `a.go|b.go|var Version = "([^"]+)"`}

	// Passing: both files agree.
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}

	// Failing: deliberately introduce a mismatch.
	writeFile(t, dir, "b.go", `var Version = "1.2.4"`)
	findings = ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if findings[0].RuleID != "r2" {
		t.Fatalf("finding fields wrong: %+v", findings[0])
	}
}

func TestProjectRulesScanner_ConsistencyRule_MissingPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", `var Version = "1.2.3"`)
	writeFile(t, dir, "b.go", `no version here`)
	rule := ProjectRule{ID: "r2b", Name: "version-sync", Type: RuleTypeConsistency,
		Pattern: `a.go|b.go|var Version = "([^"]+)"`}
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (pattern not found in b.go), got %+v", findings)
	}
}

func TestProjectRulesScanner_PresenceRule(t *testing.T) {
	dir := t.TempDir()
	rule := ProjectRule{ID: "r3", Name: "changelog-exists", Type: RuleTypePresence,
		Pattern: "CHANGELOG.md"}

	// Failing first: file doesn't exist yet.
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (missing file), got %+v", findings)
	}

	// Passing: create it.
	writeFile(t, dir, "CHANGELOG.md", "## Unreleased\n")
	findings = ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestProjectRulesScanner_MalformedRuleDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	cases := []ProjectRule{
		{ID: "bad1", Name: "bad content pattern", Type: RuleTypeContent, Pattern: "no-pipe-here"},
		{ID: "bad2", Name: "bad consistency pattern", Type: RuleTypeConsistency, Pattern: "a|b"},
		{ID: "bad3", Name: "bad consistency regex", Type: RuleTypeConsistency, Pattern: "a|b|(no-group"},
		{ID: "bad4", Name: "empty presence pattern", Type: RuleTypePresence, Pattern: ""},
		{ID: "bad5", Name: "unknown type", Type: RuleType("bogus")},
	}
	for _, rule := range cases {
		findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
		if len(findings) != 1 {
			t.Fatalf("rule %s: expected exactly 1 (self-reporting) finding, got %+v", rule.ID, findings)
		}
		if findings[0].RuleID != rule.ID {
			t.Fatalf("rule %s: finding doesn't name the broken rule: %+v", rule.ID, findings[0])
		}
	}
}

func TestProjectRulesScanner_ParityType_NoOpInPhase1(t *testing.T) {
	dir := t.TempDir()
	rule := ProjectRule{ID: "r4", Name: "parity-inheritance", Type: RuleTypeParity}
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 0 {
		t.Fatalf("parity rule type is Phase 2's job; expected no-op in Phase 1, got %+v", findings)
	}
}

func TestProjectRulesScanner_DefaultSeverity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "exists")
	rule := ProjectRule{ID: "r5", Name: "missing-file", Type: RuleTypePresence, Pattern: "does-not-exist.md"}
	findings := ProjectRulesScanner{Rules: []ProjectRule{rule}}.mustScan(t, dir)
	if len(findings) != 1 || findings[0].Severity != SeverityWarning {
		t.Fatalf("expected default severity=warning when unset, got %+v", findings)
	}
}

// mustScan is a tiny test helper so each case above reads as one line.
func (s ProjectRulesScanner) mustScan(t *testing.T, dir string) []Finding {
	t.Helper()
	findings, err := s.Scan(dir)
	if err != nil {
		t.Fatalf("Scan returned an error: %v", err)
	}
	return findings
}
