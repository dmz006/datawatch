package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// defaultKindMap is the kind map the caller passes for a project that keeps
// the built-in defaults.
func defaultKindMap() map[string]string {
	return config.MergePipelineDefaultReleaseSizeMap()
}

func TestResolveReleaseSize_ExplicitWins(t *testing.T) {
	ms := &ReleaseSizeAutomation{Milestone: true, Completed: true}
	cases := []struct {
		name string
		in   ReleaseSizeInput
		want string
	}{
		{"explicit patch over feature", ReleaseSizeInput{ReleaseSize: "patch", Kind: "feature", KindMap: defaultKindMap()}, ReleaseSizePatch},
		{"explicit minor over bug", ReleaseSizeInput{ReleaseSize: "minor", Kind: "bug", KindMap: defaultKindMap()}, ReleaseSizeMinor},
		{"explicit none over feature", ReleaseSizeInput{ReleaseSize: "none", Kind: "feature", KindMap: defaultKindMap()}, ReleaseSizeNone},
		{"explicit major", ReleaseSizeInput{ReleaseSize: "major", Kind: "bug"}, ReleaseSizeMajor},
		{"explicit major beats manual trigger", ReleaseSizeInput{ReleaseSize: "major", MajorTrigger: "manual"}, ReleaseSizeMajor},
		{"explicit major beats never trigger", ReleaseSizeInput{ReleaseSize: "major", MajorTrigger: "never"}, ReleaseSizeMajor},
		{"explicit minor beats completed milestone", ReleaseSizeInput{ReleaseSize: "minor", Automation: ms, MajorTrigger: "milestone"}, ReleaseSizeMinor},
		{"explicit value is normalized", ReleaseSizeInput{ReleaseSize: "  PATCH "}, ReleaseSizePatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.want || got.Source != ReleaseSizeSourceExplicit {
				t.Fatalf("got %+v, want size %q source explicit", got, c.want)
			}
		})
	}
}

func TestResolveReleaseSize_MilestoneRaisesMajor(t *testing.T) {
	done := &ReleaseSizeAutomation{Milestone: true, Completed: true}
	cases := []struct {
		name string
		in   ReleaseSizeInput
	}{
		{"completed milestone under milestone trigger", ReleaseSizeInput{Kind: "feature", Automation: done, MajorTrigger: "milestone", KindMap: defaultKindMap()}},
		{"bug member of completed milestone", ReleaseSizeInput{Kind: "bug", Automation: done, MajorTrigger: "milestone", KindMap: defaultKindMap()}},
		{"empty kind under completed milestone", ReleaseSizeInput{Automation: done, MajorTrigger: "milestone"}},
		{"trigger case and spacing normalized", ReleaseSizeInput{Kind: "feature", Automation: done, MajorTrigger: " Milestone "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != ReleaseSizeMajor || got.Source != ReleaseSizeSourceAutomationMilestone {
				t.Fatalf("got %+v, want major from automation_milestone", got)
			}
		})
	}
}

func TestResolveReleaseSize_MilestoneDoesNotRaiseMajor(t *testing.T) {
	cases := []struct {
		name string
		in   ReleaseSizeInput
		want string
		src  string
	}{
		{"not completed", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Milestone: true}, MajorTrigger: "milestone", KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"not a milestone", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Completed: true}, MajorTrigger: "milestone", KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"manual trigger", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Milestone: true, Completed: true}, MajorTrigger: "manual", KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"never trigger", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Milestone: true, Completed: true}, MajorTrigger: "never", KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"empty trigger is manual", ReleaseSizeInput{Kind: "feature", Automation: &ReleaseSizeAutomation{Milestone: true, Completed: true}, KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
		{"no automation", ReleaseSizeInput{Kind: "feature", MajorTrigger: "milestone", KindMap: defaultKindMap()}, ReleaseSizeMinor, ReleaseSizeSourceProjectMap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.want || got.Source != c.src {
				t.Fatalf("got %+v, want size %q source %q", got, c.want, c.src)
			}
		})
	}
}

func TestResolveReleaseSize_ProjectKindMap(t *testing.T) {
	custom := map[string]string{"feature": "patch", "bug": "minor", "docs": "none"}
	cases := []struct {
		name string
		in   ReleaseSizeInput
		want string
	}{
		{"map overrides feature", ReleaseSizeInput{Kind: "feature", KindMap: custom}, ReleaseSizePatch},
		{"map overrides bug", ReleaseSizeInput{Kind: "bug", KindMap: custom}, ReleaseSizeMinor},
		{"map adds a kind", ReleaseSizeInput{Kind: "docs", KindMap: custom}, ReleaseSizeNone},
		{"kind is case-insensitive", ReleaseSizeInput{Kind: " Feature ", KindMap: custom}, ReleaseSizePatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.want || got.Source != ReleaseSizeSourceProjectMap {
				t.Fatalf("got %+v, want size %q source project_map", got, c.want)
			}
		})
	}
}

func TestResolveReleaseSize_MajorInKindMapIsIgnored(t *testing.T) {
	m := map[string]string{"feature": "major", "docs": "major"}
	got := ResolveReleaseSize(ReleaseSizeInput{Kind: "feature", KindMap: m})
	if got.Size != ReleaseSizeMinor || got.Source != ReleaseSizeSourceBuiltInDefault {
		t.Fatalf("major map entry must fall through to built-in default, got %+v", got)
	}
	got = ResolveReleaseSize(ReleaseSizeInput{Kind: "docs", KindMap: m})
	if got.Size != ReleaseSizeNone || got.Source != ReleaseSizeSourceBuiltInDefault {
		t.Fatalf("major map entry on unknown kind must resolve to none, got %+v", got)
	}
}

func TestResolveReleaseSize_InvalidMapEntryFallsThrough(t *testing.T) {
	m := map[string]string{"feature": "huge", "bug": ""}
	got := ResolveReleaseSize(ReleaseSizeInput{Kind: "feature", KindMap: m})
	if got.Size != ReleaseSizeMinor || got.Source != ReleaseSizeSourceBuiltInDefault {
		t.Fatalf("invalid map entry must fall through, got %+v", got)
	}
	got = ResolveReleaseSize(ReleaseSizeInput{Kind: "bug", KindMap: m})
	if got.Size != ReleaseSizePatch || got.Source != ReleaseSizeSourceBuiltInDefault {
		t.Fatalf("empty map entry must fall through, got %+v", got)
	}
}

func TestResolveReleaseSize_BuiltInDefault(t *testing.T) {
	cases := []struct {
		name string
		in   ReleaseSizeInput
		want string
	}{
		{"feature with no map", ReleaseSizeInput{Kind: "feature"}, ReleaseSizeMinor},
		{"bug with no map", ReleaseSizeInput{Kind: "bug"}, ReleaseSizePatch},
		{"feature with empty map", ReleaseSizeInput{Kind: "feature", KindMap: map[string]string{}}, ReleaseSizeMinor},
		{"unknown kind resolves to none", ReleaseSizeInput{Kind: "other"}, ReleaseSizeNone},
		{"empty kind resolves to none", ReleaseSizeInput{}, ReleaseSizeNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.want || got.Source != ReleaseSizeSourceBuiltInDefault {
				t.Fatalf("got %+v, want size %q source built_in_default", got, c.want)
			}
		})
	}
}

func TestResolveReleaseSize_InvalidExplicitIsUnset(t *testing.T) {
	got := ResolveReleaseSize(ReleaseSizeInput{ReleaseSize: "giant", Kind: "bug"})
	if got.Size != ReleaseSizePatch || got.Source != ReleaseSizeSourceBuiltInDefault {
		t.Fatalf("invalid explicit size must be treated as unset, got %+v", got)
	}
}

func TestResolveReleaseSize_BreakingChangeFlagsApproval(t *testing.T) {
	cases := []struct {
		name string
		in   ReleaseSizeInput
		want string
	}{
		{"breaking patch keeps size", ReleaseSizeInput{ReleaseSize: "patch", BreakingChange: true}, ReleaseSizePatch},
		{"breaking default feature keeps minor", ReleaseSizeInput{Kind: "feature", BreakingChange: true}, ReleaseSizeMinor},
		{"breaking none keeps none", ReleaseSizeInput{ReleaseSize: "none", BreakingChange: true}, ReleaseSizeNone},
		{"breaking explicit major keeps major", ReleaseSizeInput{ReleaseSize: "major", BreakingChange: true}, ReleaseSizeMajor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveReleaseSize(c.in)
			if got.Size != c.want || !got.RaisesMajorApproval {
				t.Fatalf("got %+v, want size %q and RaisesMajorApproval", got, c.want)
			}
		})
	}
}

func TestResolveReleaseSize_NoBreakingChangeNoApproval(t *testing.T) {
	got := ResolveReleaseSize(ReleaseSizeInput{Kind: "feature", KindMap: defaultKindMap()})
	if got.RaisesMajorApproval {
		t.Fatalf("RaisesMajorApproval must be false without a breaking change, got %+v", got)
	}
}

func TestResolveReleaseSize_NeverInfersMajor(t *testing.T) {
	// Without an explicit major or a completed milestone under the milestone
	// trigger, no input may produce major, whatever the kind or the diff.
	kinds := []string{"feature", "bug", "other", "major", ""}
	triggers := []string{"", "manual", "milestone", "never"}
	for _, k := range kinds {
		for _, tr := range triggers {
			got := ResolveReleaseSize(ReleaseSizeInput{Kind: k, MajorTrigger: tr, BreakingChange: true, KindMap: map[string]string{k: "major"}})
			if got.Size == ReleaseSizeMajor {
				t.Fatalf("kind %q trigger %q inferred major: %+v", k, tr, got)
			}
		}
	}
}
