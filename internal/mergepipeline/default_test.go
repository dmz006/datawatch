package mergepipeline

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := DefaultDefinition().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultOrder(t *testing.T) {
	want := []string{"rebase-onto-main", "check-branch-versions", "check-internal-refs", "merge-branches",
		"set-release-version", "insert-changelog-heading", "set-readme-release-line", "commit-release-bump",
		"tag-release", "validate-release-bump", "push-release", "deploy-latest"}
	var got []string
	for _, s := range DefaultDefinition().Steps {
		got = append(got, s.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order\n got %v\nwant %v", got, want)
	}
}

func TestDefaultRoundTrip(t *testing.T) {
	d := DefaultDefinition()
	data, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(data, "default")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, back) {
		t.Fatalf("round trip differs:\n%v\n%v", d, back)
	}
}

func TestDefaultsAreIndependentCopies(t *testing.T) {
	a := DefaultDefinition()
	a.Steps[0].Name = "changed"
	if DefaultDefinition().Steps[0].Name == "changed" {
		t.Fatal("default shared state")
	}
}

func TestDefaultFileName(t *testing.T) {
	if DefaultFileName != ".datawatch/merge-pipeline.yaml" {
		t.Fatalf("DefaultFileName = %q", DefaultFileName)
	}
}

func TestDefaultRebaseStepIsFirstAndStopsOnFailure(t *testing.T) {
	first := DefaultDefinition().Steps[0]
	if first.Name != "rebase-onto-main" || first.Kind != KindCustom || first.Action != ActionRebaseOntoMain ||
		first.EffectiveOnFailure() != OnFailureStop || first.Timeout != 5*time.Minute {
		t.Fatalf("first step %+v", first)
	}
}

func TestDefaultValidateStepSitsBeforePushAndStops(t *testing.T) {
	var step Step
	for _, s := range DefaultDefinition().Steps {
		if s.Name == "validate-release-bump" {
			step = s
		}
	}
	if step.Action != ActionValidateReleaseBump || step.EffectiveOnFailure() != OnFailureStop {
		t.Fatalf("validate step = %+v, want action %s with on_failure stop", step, ActionValidateReleaseBump)
	}
}
