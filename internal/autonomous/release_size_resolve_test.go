package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// projectKindMap is the kind map resolved for the project "datawatch". It
// starts from the built-in map (feature minor, bug patch), adds the global
// override refactor=patch, and adds the project override docs=none, through
// the same EffectiveReleaseSizeMap call the merge pipeline makes.
func projectKindMap() map[string]string {
	cfg := config.MergePipelineConfig{
		ReleaseSizeMap: map[string]string{"refactor": "patch"},
		Projects: map[string]config.MergePipelineOverride{
			"datawatch": {ReleaseSizeMap: map[string]string{"docs": "none"}},
		},
	}
	return cfg.EffectiveReleaseSizeMap("datawatch")
}

// completedMilestone is an Automation marked as a milestone that has finished.
func completedMilestone() *ReleaseSizeAutomation {
	return &ReleaseSizeAutomation{Milestone: true, Completed: true}
}

// TestResolveReleaseSizeExplicit covers layer 1: an explicit release_size on
// the Automaton beats every layer below it, including a completed milestone
// under the milestone trigger.
func TestResolveReleaseSizeExplicit(t *testing.T) {
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
	}{
		{"explicit patch beats feature minor", ReleaseSizeInput{ReleaseSize: "patch", Kind: "feature", KindMap: projectKindMap()}, ReleaseSizePatch, ReleaseSizeSourceExplicit},
		{"explicit minor beats bug patch", ReleaseSizeInput{ReleaseSize: "minor", Kind: "bug", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceExplicit},
		{"explicit major beats bug patch", ReleaseSizeInput{ReleaseSize: "major", Kind: "bug", KindMap: projectKindMap()}, ReleaseSizeMajor, ReleaseSizeSourceExplicit},
		{"explicit none beats feature minor", ReleaseSizeInput{ReleaseSize: "none", Kind: "feature", KindMap: projectKindMap()}, ReleaseSizeNone, ReleaseSizeSourceExplicit},
		{"explicit patch beats completed milestone", ReleaseSizeInput{ReleaseSize: "patch", Kind: "feature", Automation: completedMilestone(), MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizePatch, ReleaseSizeSourceExplicit},
		{"explicit minor beats completed milestone", ReleaseSizeInput{ReleaseSize: "minor", Automation: completedMilestone(), MajorTrigger: "milestone"}, ReleaseSizeMinor, ReleaseSizeSourceExplicit},
		{"explicit none beats completed milestone", ReleaseSizeInput{ReleaseSize: "none", Automation: completedMilestone(), MajorTrigger: "milestone"}, ReleaseSizeNone, ReleaseSizeSourceExplicit},
		{"explicit major under manual trigger", ReleaseSizeInput{ReleaseSize: "major", MajorTrigger: "manual"}, ReleaseSizeMajor, ReleaseSizeSourceExplicit},
		{"explicit major under never trigger", ReleaseSizeInput{ReleaseSize: "major", MajorTrigger: "never"}, ReleaseSizeMajor, ReleaseSizeSourceExplicit},
		{"explicit value is trimmed and lowercased", ReleaseSizeInput{ReleaseSize: "  PATCH "}, ReleaseSizePatch, ReleaseSizeSourceExplicit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource {
				t.Fatalf("got size %q source %q, want size %q source %q", got.Size, got.Source, c.wantSize, c.wantSource)
			}
		})
	}
}

// TestResolveReleaseSizeMilestone covers layer 2: a completed milestone
// Automation raises the major step only under the milestone trigger. Every
// other combination falls through to the project kind map.
func TestResolveReleaseSizeMilestone(t *testing.T) {
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
	}{
		{"completed milestone raises major for feature", ReleaseSizeInput{Kind: "feature", Automation: completedMilestone(), MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMajor, ReleaseSizeSourceAutomationMilestone},
		{"completed milestone raises major for bug", ReleaseSizeInput{Kind: "bug", Automation: completedMilestone(), MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMajor, ReleaseSizeSourceAutomationMilestone},
		{"completed milestone raises major with empty kind", ReleaseSizeInput{Automation: completedMilestone(), MajorTrigger: "milestone"}, ReleaseSizeMajor, ReleaseSizeSourceAutomationMilestone},
		{"trigger case and spacing are normalized", ReleaseSizeInput{Kind: "feature", Automation: completedMilestone(), MajorTrigger: " Milestone ", KindMap: projectKindMap()}, ReleaseSizeMajor, ReleaseSizeSourceAutomationMilestone},
		{"incomplete milestone does not raise major", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Milestone: true}, MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"incomplete milestone bug stays patch", ReleaseSizeInput{Kind: "bug", Automation: &ReleaseSizeAutomation{Milestone: true}, MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizePatch, ReleaseSizeSourceProjectMap},
		{"completed Automation that is not a milestone does not raise major", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Completed: true}, MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"manual trigger does not raise major", ReleaseSizeInput{Kind: "feature", Automation: completedMilestone(), MajorTrigger: "manual", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"never trigger does not raise major", ReleaseSizeInput{Kind: "feature", Automation: completedMilestone(), MajorTrigger: "never", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"empty trigger means manual and does not raise major", ReleaseSizeInput{Kind: "feature", Automation: completedMilestone(), KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"no Automation does not raise major", ReleaseSizeInput{Kind: "feature", MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource {
				t.Fatalf("got size %q source %q, want size %q source %q", got.Size, got.Source, c.wantSize, c.wantSource)
			}
		})
	}
}

// TestResolveReleaseSizeProjectMap covers layer 3: the project kind map,
// including the global override refactor=patch and the project override
// docs=none, both resolved by EffectiveReleaseSizeMap.
func TestResolveReleaseSizeProjectMap(t *testing.T) {
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
	}{
		{"feature from the map is minor", ReleaseSizeInput{Kind: "feature", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"bug from the map is patch", ReleaseSizeInput{Kind: "bug", KindMap: projectKindMap()}, ReleaseSizePatch, ReleaseSizeSourceProjectMap},
		{"global override refactor is patch", ReleaseSizeInput{Kind: "refactor", KindMap: projectKindMap()}, ReleaseSizePatch, ReleaseSizeSourceProjectMap},
		{"project override docs is none", ReleaseSizeInput{Kind: "docs", KindMap: projectKindMap()}, ReleaseSizeNone, ReleaseSizeSourceProjectMap},
		{"kind is case-insensitive", ReleaseSizeInput{Kind: " Feature ", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"map entry overrides built-in feature", ReleaseSizeInput{Kind: "feature", KindMap: map[string]string{"feature": "patch"}}, ReleaseSizePatch, ReleaseSizeSourceProjectMap},
		{"map entry overrides built-in bug", ReleaseSizeInput{Kind: "bug", KindMap: map[string]string{"bug": "minor"}}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource {
				t.Fatalf("got size %q source %q, want size %q source %q", got.Size, got.Source, c.wantSize, c.wantSource)
			}
		})
	}
}

// TestResolveReleaseSizeBuiltInDefault covers layer 4: with no map entry the
// built-in default applies, feature is minor, bug is patch, and any other
// kind is none.
func TestResolveReleaseSizeBuiltInDefault(t *testing.T) {
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
	}{
		{"feature with no map is minor", ReleaseSizeInput{Kind: "feature"}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"bug with no map is patch", ReleaseSizeInput{Kind: "bug"}, ReleaseSizePatch, ReleaseSizeSourceBuiltInDefault},
		{"feature with empty map is minor", ReleaseSizeInput{Kind: "feature", KindMap: map[string]string{}}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"unknown kind is none", ReleaseSizeInput{Kind: "other"}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"empty kind is none", ReleaseSizeInput{}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"refactor with no map is none", ReleaseSizeInput{Kind: "refactor"}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"invalid map entry falls through to feature minor", ReleaseSizeInput{Kind: "feature", KindMap: map[string]string{"feature": "huge"}}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"major map entry falls through to bug patch", ReleaseSizeInput{Kind: "bug", KindMap: map[string]string{"bug": "major"}}, ReleaseSizePatch, ReleaseSizeSourceBuiltInDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource {
				t.Fatalf("got size %q source %q, want size %q source %q", got.Size, got.Source, c.wantSize, c.wantSource)
			}
		})
	}
}

// TestResolveReleaseSizeBreakingChange covers the breaking-change flag. The
// flag never changes Size; it sets RaisesMajorApproval, so the release goes
// through the major approval mode.
func TestResolveReleaseSizeBreakingChange(t *testing.T) {
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
		wantRaises bool
	}{
		{"breaking explicit patch keeps patch", ReleaseSizeInput{ReleaseSize: "patch", BreakingChange: true}, ReleaseSizePatch, ReleaseSizeSourceExplicit, true},
		{"breaking explicit none keeps none", ReleaseSizeInput{ReleaseSize: "none", BreakingChange: true}, ReleaseSizeNone, ReleaseSizeSourceExplicit, true},
		{"breaking explicit major keeps major", ReleaseSizeInput{ReleaseSize: "major", BreakingChange: true}, ReleaseSizeMajor, ReleaseSizeSourceExplicit, true},
		{"breaking feature from map keeps minor", ReleaseSizeInput{Kind: "feature", BreakingChange: true, KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap, true},
		{"breaking feature with no map keeps minor", ReleaseSizeInput{Kind: "feature", BreakingChange: true}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault, true},
		{"breaking milestone keeps major from milestone", ReleaseSizeInput{Kind: "feature", BreakingChange: true, Automation: completedMilestone(), MajorTrigger: "milestone", KindMap: projectKindMap()}, ReleaseSizeMajor, ReleaseSizeSourceAutomationMilestone, true},
		{"not breaking feature does not raise approval", ReleaseSizeInput{Kind: "feature", KindMap: projectKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap, false},
		{"not breaking explicit major does not raise approval", ReleaseSizeInput{ReleaseSize: "major"}, ReleaseSizeMajor, ReleaseSizeSourceExplicit, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource || got.RaisesMajorApproval != c.wantRaises {
				t.Fatalf("got %+v, want size %q source %q raises %v", got, c.wantSize, c.wantSource, c.wantRaises)
			}
		})
	}
}

// TestResolveReleaseSizeNeverInfersMajor is the guard. A kind alone, a kind
// map entry of major, or any diff-like text in the kind can never produce a
// major release. Only an explicit major or a completed milestone under the
// milestone trigger can.
func TestResolveReleaseSizeNeverInfersMajor(t *testing.T) {
	diffLike := []string{
		"diff --git a/internal/x.go b/internal/x.go",
		"+++ b/go.mod\n@@ -1 +1 @@ BREAKING CHANGE: removed API",
		"major: 3 files changed",
		"BREAKING CHANGE",
	}

	// Table rows with exact expectations, every one asserting Source.
	cases := []struct {
		name       string
		in         ReleaseSizeInput
		wantSize   string
		wantSource string
	}{
		{"feature alone is minor", ReleaseSizeInput{Kind: "feature"}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"feature map entry major is ignored", ReleaseSizeInput{Kind: "feature", KindMap: map[string]string{"feature": "major"}}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"bug breaking change is patch", ReleaseSizeInput{Kind: "bug", BreakingChange: true, KindMap: map[string]string{"bug": "major"}}, ReleaseSizePatch, ReleaseSizeSourceBuiltInDefault},
		{"unknown kind map entry major is ignored", ReleaseSizeInput{Kind: "other", KindMap: map[string]string{"other": "major"}}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"kind named major is not major", ReleaseSizeInput{Kind: "major", KindMap: map[string]string{"major": "major"}}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"empty kind is none", ReleaseSizeInput{}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"feature under never trigger with completed milestone", ReleaseSizeInput{Kind: "feature", MajorTrigger: "never", Automation: completedMilestone()}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"feature under manual trigger with completed milestone", ReleaseSizeInput{Kind: "feature", MajorTrigger: "manual", Automation: completedMilestone()}, ReleaseSizeMinor, ReleaseSizeSourceBuiltInDefault},
		{"diff header as kind is none", ReleaseSizeInput{Kind: diffLike[0], BreakingChange: true}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"diff hunk with breaking text as kind is none", ReleaseSizeInput{Kind: diffLike[1], BreakingChange: true, KindMap: map[string]string{diffLike[1]: "major"}}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"diffstat text as kind is none", ReleaseSizeInput{Kind: diffLike[2], KindMap: map[string]string{diffLike[2]: "major"}}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
		{"breaking text as kind is none", ReleaseSizeInput{Kind: diffLike[3], BreakingChange: true}, ReleaseSizeNone, ReleaseSizeSourceBuiltInDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.wantSize || got.Source != c.wantSource {
				t.Fatalf("got size %q source %q, want size %q source %q", got.Size, got.Source, c.wantSize, c.wantSource)
			}
		})
	}

	// Exhaustive sweep: every kind, diff-like text and trigger, with and
	// without a breaking change, must never resolve to major unless the
	// input is an explicit major or a completed milestone under milestone.
	kinds := append([]string{"feature", "bug", "refactor", "docs", "other", "major", ""}, diffLike...)
	triggers := []string{"", "manual", "milestone", "never"}
	automations := []*ReleaseSizeAutomation{nil, {Milestone: true}, {Completed: true}, completedMilestone()}
	for _, k := range kinds {
		for _, tr := range triggers {
			for _, breaking := range []bool{false, true} {
				for _, auto := range automations {
					in := ReleaseSizeInput{
						Kind:           k,
						MajorTrigger:   tr,
						BreakingChange: breaking,
						Automation:     auto,
						KindMap:        map[string]string{k: "major"},
					}
					got := ResolveReleaseSize(in)
					milestoneRaises := auto != nil && auto.Milestone && auto.Completed && tr == "milestone"
					if got.Size == ReleaseSizeMajor && !milestoneRaises {
						t.Fatalf("kind %q trigger %q breaking %v automation %+v inferred major: %+v", k, tr, breaking, auto, got)
					}
					if got.Source == ReleaseSizeSourceExplicit {
						t.Fatalf("kind %q resolved from explicit layer with no explicit size: %+v", k, got)
					}
					if got.Source == ReleaseSizeSourceAutomationMilestone && !milestoneRaises {
						t.Fatalf("kind %q trigger %q took the milestone layer without a completed milestone: %+v", k, tr, got)
					}
				}
			}
		}
	}
}
