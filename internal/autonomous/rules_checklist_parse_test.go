package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRulesChecklistParseLandsOnTasks(t *testing.T) {
	raw := `{"title":"x","stories":[{"title":"s","tasks":[
	 {"title":"a","spec":"do a","rules_applied":["Parity","Tests"]},
	 {"title":"b","spec":"do b"}]}]}`
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := stories[0].Tasks[0].RulesApplied; !reflect.DeepEqual(got, []string{"parity", "tests"}) {
		t.Errorf("got %v", got)
	}
	if got := stories[0].Tasks[1].RulesApplied; len(got) != 0 {
		t.Errorf("absent field should stay empty, got %v", got)
	}
}

func TestRulesChecklistParseNormalises(t *testing.T) {
	in := []string{"  Parity ", "PARITY", "Docs   and\tTests", "", "   ", "versioning"}
	want := []string{"parity", "docs and tests", "versioning"}
	if got := normalizeRulesApplied(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	var many []string
	for i := 0; i < 40; i++ {
		many = append(many, fmt.Sprintf("section %d", i))
	}
	if got := normalizeRulesApplied(many); len(got) != 24 || got[0] != "section 0" {
		t.Errorf("cap failed: %d", len(got))
	}
	got := normalizeRulesApplied([]string{strings.Repeat("é", 100)})
	if len(got) != 1 || len(got[0]) > 64 || len(got[0]) < 62 {
		t.Errorf("byte cap failed: %d bytes", len(got[0]))
	}
	if normalizeRulesApplied(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestRulesChecklistParsePromptsCarryField(t *testing.T) {
	for name, p := range map[string]string{"PlanningPrompt": PlanningPrompt, "PlanningPromptSession": PlanningPromptSession} {
		if !strings.Contains(p, `"rules_applied": ["<rules section name>"`) {
			t.Errorf("%s lacks the schema field", name)
		}
		if !strings.Contains(p, planRulesAppliedRuleBullet) {
			t.Errorf("%s lacks the rule bullet", name)
		}
	}
	if strings.Contains(planRulesAppliedRuleBullet, "%") {
		t.Error("bullet must not contain a percent sign")
	}
}

func TestRulesChecklistParseSprintfStillClean(t *testing.T) {
	out := fmt.Sprintf(PlanningPrompt, "FEATURE")
	if strings.Contains(out, "%!") || !strings.Contains(out, "FEATURE") {
		t.Errorf("bad format output")
	}
}

func TestRulesChecklistParseSectionNamesLine(t *testing.T) {
	dir := t.TempDir()
	body := "# Rules\n\n## Parity\ntext\n\n## Tests\ntext\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	line := rulesSectionNamesLine(dir, defaultRulesContext())
	if !strings.Contains(line, "Parity") || !strings.Contains(line, "Tests") {
		t.Errorf("line = %q", line)
	}
	if rulesSectionNamesLine(t.TempDir(), defaultRulesContext()) != "" {
		t.Error("missing file should give empty line")
	}
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "## Section number %d\nx\n", i)
	}
	_ = os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(sb.String()), 0o600)
	if l := rulesSectionNamesLine(dir, defaultRulesContext()); len(l) > 600 || l == "" {
		t.Errorf("cap failed: %d", len(l))
	}
}
