package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func ruleKinds(f []StructuralFinding) map[string]int {
	m := map[string]int{}
	for _, x := range f {
		m[x.Kind]++
	}
	return m
}

func story(tasks ...Task) []Story { return []Story{{ID: "s1", Tasks: tasks}} }

func TestPlanRules_FeatureWithoutTest(t *testing.T) {
	bad := story(Task{ID: "t1", Title: "Add widget", FilesPlanned: []string{"internal/x/widget.go"}})
	if ruleKinds(CheckPlanRules(nil, bad))[FindingFeatureWithoutTest] != 1 {
		t.Fatal("expected finding")
	}
	for name, ok := range map[string][]Story{
		"test file":   story(Task{ID: "t1", Title: "Add widget", FilesPlanned: []string{"internal/x/widget.go", "internal/x/widget_test.go"}}),
		"test task":   story(Task{ID: "t1", Title: "Add widget"}, Task{ID: "t2", Title: "Tests for widget"}),
		"non-feature": story(Task{ID: "t1", Title: "Rename widget"}),
	} {
		if ruleKinds(CheckPlanRules(nil, ok))[FindingFeatureWithoutTest] != 0 {
			t.Fatalf("%s: unexpected finding", name)
		}
	}
}

func TestPlanRules_TrackerID(t *testing.T) {
	for _, n := range []string{"bl123_widget.go", "GH45_fix.go", "B7_x.go"} {
		st := story(Task{ID: "t1", Title: "Rename", FilesPlanned: []string{"a/" + n}})
		if ruleKinds(CheckPlanRules(nil, st))[FindingTrackerIDInNewName] != 1 {
			t.Fatalf("%s: expected finding", n)
		}
	}
	st := story(Task{ID: "t1", Title: "Rename", Spec: "func HandleBL42Thing() {}"})
	if ruleKinds(CheckPlanRules(nil, st))[FindingTrackerIDInNewName] != 1 {
		t.Fatal("func name should fire")
	}
	ok := story(Task{ID: "t1", Title: "Rename", FilesPlanned: []string{"a/bundle.go", "a/web.go"}, Spec: "type Builder struct{}"})
	if ruleKinds(CheckPlanRules(nil, ok))[FindingTrackerIDInNewName] != 0 {
		t.Fatal("clean names fired")
	}
}

func TestPlanRules_ReleaseVersion(t *testing.T) {
	bad := story(Task{ID: "t1", Title: "Cut release", Spec: "Tag v1.2.3"})
	if ruleKinds(CheckPlanRules(nil, bad))[FindingReleaseFixedVersion] != 1 {
		t.Fatal("expected finding")
	}
	ok := story(Task{ID: "t1", Title: "Cut release", Spec: "Use the next release above main"})
	if ruleKinds(CheckPlanRules(nil, ok))[FindingReleaseFixedVersion] != 0 {
		t.Fatal("unexpected finding")
	}
	other := story(Task{ID: "t1", Title: "Bump dep", Spec: "upgrade to 1.2.3"})
	if ruleKinds(CheckPlanRules(nil, other))[FindingReleaseFixedVersion] != 0 {
		t.Fatal("non-release task fired")
	}
}

func TestPlanRules_ModelPin(t *testing.T) {
	prd := &PRD{Spec: "Every task needs a model pin."}
	st := story(Task{ID: "t1", Title: "Rename"})
	if ruleKinds(CheckPlanRules(prd, st))[FindingMissingModelPin] != 1 {
		t.Fatal("expected finding")
	}
	st[0].Tasks[0].Model = "m"
	if ruleKinds(CheckPlanRules(prd, st))[FindingMissingModelPin] != 0 {
		t.Fatal("pinned task fired")
	}
	if ruleKinds(CheckPlanRules(&PRD{Spec: "nothing"}, story(Task{ID: "t1", Title: "Rename"})))[FindingMissingModelPin] != 0 {
		t.Fatal("fired without requirement")
	}
}

func TestPlanRules_EmptyPlan(t *testing.T) {
	if f := CheckPlanRules(nil, nil); f == nil || len(f) != 0 {
		t.Fatalf("got %v", f)
	}
	r := &PlanStructureReport{Pass: true}
	applyPlanRulesMode(config.ChecksModeOn, nil, nil, r)
	if len(r.Advisory) != 0 {
		t.Fatal("advisory on empty plan")
	}
}

func TestPlanRules_ModeSwitch(t *testing.T) {
	st := story(Task{ID: "t1", Title: "Add widget"})
	for _, mode := range []string{config.ChecksModeOff, config.ChecksModeShadow, config.ChecksModeOn} {
		ResetShadowPlanRuleFindings()
		r := &PlanStructureReport{Pass: true}
		applyPlanRulesMode(mode, nil, st, r)
		surfaced, recorded := len(r.Advisory), len(RecordedShadowPlanRuleFindings())
		switch mode {
		case config.ChecksModeOff:
			if surfaced != 0 || recorded != 0 {
				t.Fatalf("off: %d %d", surfaced, recorded)
			}
		case config.ChecksModeShadow:
			if surfaced != 0 || recorded != 1 {
				t.Fatalf("shadow: %d %d", surfaced, recorded)
			}
		case config.ChecksModeOn:
			if surfaced != 1 || recorded != 0 {
				t.Fatalf("on: %d %d", surfaced, recorded)
			}
		}
		if !r.Pass || r.Count() != 0 {
			t.Fatalf("%s: advisory changed Pass/Count", mode)
		}
	}
	ResetShadowPlanRuleFindings()
}

func TestPlanRules_ApplyUsesPolicy(t *testing.T) {
	defer SetChecksConfig(config.AutonomousChecksConfig{})
	SetChecksConfig(config.AutonomousChecksConfig{PlanRules: "off"})
	r := &PlanStructureReport{Pass: true}
	ApplyPlanRules(&PRD{ProjectDir: "/p"}, story(Task{ID: "t1", Title: "Add widget"}), r)
	if len(r.Advisory) != 0 {
		t.Fatal("off should not surface")
	}
	SetChecksConfig(config.AutonomousChecksConfig{PlanRules: "on"})
	ApplyPlanRules(&PRD{ProjectDir: "/p"}, story(Task{ID: "t1", Title: "Add widget"}), r)
	if len(r.Advisory) != 1 {
		t.Fatal("on should surface")
	}
}
