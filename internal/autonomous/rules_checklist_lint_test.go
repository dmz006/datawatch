package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func gapCount(prd *PRD, st []Story) int {
	return ruleKinds(CheckPlanRules(prd, st))[FindingRulesChecklistGap]
}

func prdWithRulesFile(present bool) *PRD {
	return &PRD{RulesContext: &RulesContextRecord{Files: []RulesContextFileRef{{Name: "CLAUDE.md", Kind: "rules", Present: present}}}}
}

func TestRulesChecklistGapUITaskMissingLocalization(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Update panel", FilesPlanned: []string{"internal/server/web/app.js"},
		RulesApplied: []string{"tests", "parity and config"}})
	f := CheckPlanRules(nil, st)
	if ruleKinds(f)[FindingRulesChecklistGap] != 1 {
		t.Fatalf("expected one gap, got %v", f)
	}
	for _, x := range f {
		if x.Kind == FindingRulesChecklistGap && (!strings.Contains(x.Detail, "Update panel") || !strings.Contains(x.Detail, "localization")) {
			t.Fatalf("detail: %s", x.Detail)
		}
	}
}

func TestRulesChecklistGapCompleteListClean(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Update panel", FilesPlanned: []string{"internal/server/web/app.js"},
		RulesApplied: []string{"localization", "tests", "parity", "documentation"}})
	if n := gapCount(nil, st); n != 0 {
		t.Fatalf("unexpected %d", n)
	}
}

func TestRulesChecklistGapDocsTask(t *testing.T) {
	bad := story(Task{ID: "t1", Title: "Document the flow", FilesPlanned: []string{"docs/flow.md"}, RulesApplied: []string{"tests"}})
	if gapCount(nil, bad) != 1 {
		t.Fatal("expected gap")
	}
	ok := story(Task{ID: "t1", Title: "Document the flow", FilesPlanned: []string{"docs/flow.md"}, RulesApplied: []string{"Documentation rule"}})
	if gapCount(nil, ok) != 0 {
		t.Fatal("unexpected gap")
	}
}

func TestRulesChecklistGapReadOnlyExempt(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Survey", ReadOnly: true, FilesPlanned: []string{"internal/server/web/app.js"}})
	if gapCount(prdWithRulesFile(true), st) != 0 {
		t.Fatal("read-only task flagged")
	}
}

func TestRulesChecklistGapEmptyListNeedsRulesFile(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Rename thing"})
	if gapCount(prdWithRulesFile(true), st) != 1 {
		t.Fatal("expected gap with rules file present")
	}
	if gapCount(prdWithRulesFile(false), st) != 0 {
		t.Fatal("flagged with rules file missing")
	}
	if gapCount(&PRD{}, st) != 0 || gapCount(nil, st) != 0 {
		t.Fatal("flagged without a record")
	}
}

func TestRulesChecklistGapCaseInsensitive(t *testing.T) {
	for _, name := range []string{"Localization Rule", "LOCALISATION", "localization"} {
		st := story(Task{ID: "t1", Title: "Update panel", FilesPlanned: []string{"internal/server/web/app.js"},
			RulesApplied: []string{name, "TESTS", "Parity", "DOCS"}})
		if gapCount(nil, st) != 0 {
			t.Fatalf("%q not accepted", name)
		}
	}
}

func TestRulesChecklistGapModes(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Cut release", RulesApplied: []string{"tests"}})
	for _, mode := range []string{config.ChecksModeOff, config.ChecksModeShadow, config.ChecksModeOn} {
		ResetShadowPlanRuleFindings()
		r := &PlanStructureReport{Pass: true}
		applyPlanRulesMode(mode, nil, st, r)
		surfaced := ruleKinds(r.Advisory)[FindingRulesChecklistGap]
		recorded := ruleKinds(RecordedShadowPlanRuleFindings())[FindingRulesChecklistGap]
		want := map[string][2]int{config.ChecksModeOff: {0, 0}, config.ChecksModeShadow: {0, 1}, config.ChecksModeOn: {1, 0}}[mode]
		if surfaced != want[0] || recorded != want[1] {
			t.Fatalf("%s: surfaced=%d recorded=%d", mode, surfaced, recorded)
		}
	}
	ResetShadowPlanRuleFindings()
}
