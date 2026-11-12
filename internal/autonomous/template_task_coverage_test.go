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

// A parsed plan has no ids until the store assigns them. The hook gives it
// ids first, so the template tasks get unique ids per story and no empty
// depends_on entry, and the whole plan resolves like any other.
func TestTemplateTasksOnAnIdlessPlanResolveCleanly(t *testing.T) {
	stories := []Story{
		{Title: "story one", Tasks: []Task{{Title: "task a", FilesPlanned: []string{"a.go"}}, {Title: "task b", DependsOn: []string{"task a"}}}},
		{Title: "story two", Tasks: []Task{{Title: "task c", FilesPlanned: []string{"c.go"}}}},
	}
	ensurePlanIDs(stories)
	out, applied := ApplyTaskTemplates(stories, "the feature", "/w/proj", nil)
	if len(applied) == 0 {
		t.Fatalf("the built-in templates must apply, got none")
	}
	// The store maps are empty: this plan is not stored yet, exactly as in
	// SetStories, which resolves before it adds the plan to its maps.
	storyByID := map[string]*Story{}
	taskByID := map[string]*Task{}
	seen := map[string]bool{}
	for i := range out {
		for j := range out[i].Tasks {
			tk := &out[i].Tasks[j]
			if tk.ID == "" || seen[tk.ID] {
				t.Fatalf("task %q has an empty or duplicate id %q", tk.Title, tk.ID)
			}
			seen[tk.ID] = true
			for _, d := range tk.DependsOn {
				if strings.TrimSpace(d) == "" {
					t.Fatalf("task %q has an empty depends_on entry", tk.Title)
				}
			}
		}
	}
	if err := resolveDependsOn(out, storyByID, taskByID); err != nil {
		t.Fatalf("the templated plan must resolve: %v", err)
	}
}

func TestFinalGateTemplateReportPathBuiltInsNonReadOnlyUnchanged(t *testing.T) {
	for _, def := range BuiltInTaskTemplates() {
		if def.ReadOnly {
			if def.ReportPath == "" && def.VerificationScript == "" {
				t.Fatalf("read-only built-in %q is unverifiable", def.ID)
			}
			continue
		}
		if def.ReportPath != "" {
			t.Fatalf("non-read-only built-in %q must not carry a report path", def.ID)
		}
	}
}
