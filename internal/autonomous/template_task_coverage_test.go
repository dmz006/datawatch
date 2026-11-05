package autonomous

import (
	"strings"
	"testing"
)

// Tasks appended from the task templates carry planned files and no
// verification script by design; the coverage gate must not reject a plan
// for them, and must still reject a planner task that lacks a script.
func TestVerificationCoverageIgnoresTemplateTasks(t *testing.T) {
	planned := Task{ID: "s1-t1", Title: "planner task", FilesPlanned: []string{"a.go"}, VerificationScript: ".datawatch/verify/a.sh"}
	tpl := Task{ID: "s1-tpl-changelog_entry", Title: "Changelog entry for X", FilesPlanned: []string{"CHANGELOG.md"}}
	ok := []Story{{ID: "s1", Title: "story", Tasks: []Task{planned, tpl}}}
	if err := validateVerificationScriptCoverage("/w/proj", ok); err != nil {
		t.Fatalf("template task must be exempt, got %v", err)
	}
	bad := []Story{{ID: "s1", Title: "story", Tasks: []Task{{ID: "s1-t2", Title: "unscripted", FilesPlanned: []string{"b.go"}}, tpl}}}
	err := validateVerificationScriptCoverage("/w/proj", bad)
	if err == nil || !strings.Contains(err.Error(), "unscripted") || strings.Contains(err.Error(), "Changelog entry") {
		t.Fatalf("a planner task without a script must still fail and name only it, got %v", err)
	}
}

// The templates name the work by the Automaton's title, never the whole spec.
func TestTemplateFeatureNameUsesTitleNotSpec(t *testing.T) {
	long := &PRD{Title: "Fix the tab strip", Spec: "Fix the strip.\nA long spec that bumps a version and edits a version variable."}
	if got := templateFeatureName(long); got != "Fix the tab strip" {
		t.Fatalf("title expected, got %q", got)
	}
	untitled := &PRD{Spec: "First line of the spec\nsecond line"}
	if got := templateFeatureName(untitled); got != "First line of the spec" {
		t.Fatalf("first spec line expected, got %q", got)
	}
	huge := &PRD{Title: strings.Repeat("x", 500)}
	if got := templateFeatureName(huge); len([]rune(got)) != 120 {
		t.Fatalf("name must be bounded to 120 runes, got %d", len([]rune(got)))
	}
}
