package mergeshadow

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// kindSizeFor returns the size the pipeline's kind map gives kind for the
// project. That map — the built-in defaults layered with the operator
// settings, exposed by config.MergePipelineConfig — is the same source the
// merge shadow hook resolves an Automaton's size from when the Automaton
// records no explicit size, so it is the size the runner reads.
func kindSizeFor(t *testing.T, pipeline config.MergePipelineConfig, project, kind string) ReleaseSize {
	t.Helper()
	size, ok := pipeline.EffectiveReleaseSizeMap(project)[kind]
	if !ok {
		t.Fatalf("pipeline names no size for kind %q", kind)
	}
	return ReleaseSize(size)
}

// entryTasking is a well-formed changelog entry task naming version.
func entryTasking(version string) []ChangelogTask {
	return []ChangelogTask{{
		ID:     "t9",
		Kind:   ChangelogTaskKind,
		Output: "## [v" + version + "] - 2026-11-09\n\n- entry names " + version + "\n",
	}}
}

// runAndStore drives the runner with fake gates and a fixed clock and
// returns the plan read back through the real store loader, so the
// assertions can hold the persisted record, not just the returned value.
func runAndStore(t *testing.T, in Input) *MergePlan {
	t.Helper()
	runner := Runner{Gates: fakeGates(t, GatePass, new(int)), Now: fixedClock()}
	if _, err := runner.Run(context.Background(), in); err != nil {
		t.Fatalf("run: %v", err)
	}
	stored, err := Load(in.DataDir, in.AutomatonID)
	if err != nil {
		t.Fatalf("plan not read back through the store: %v", err)
	}
	return stored
}

// TestSizingBugAutomatonWithoutExplicitSizePlansPatch pins the fix: an
// Automaton that planned a bug with no explicit release size ends up at the
// size its kind carries, patch, and the planned version is the next patch
// over the base ref's version.
func TestSizingBugAutomatonWithoutExplicitSizePlansPatch(t *testing.T) {
	repo := runnerRepo(t)
	pipeline := config.MergePipelineConfig{VersionFiles: []string{"version.go"}}
	size := kindSizeFor(t, pipeline, "demo", config.ReleaseKindBug)
	if size != ReleaseSizePatch {
		t.Fatalf("built-in size for a bug is %q, want patch", size)
	}

	in := runnerInput(t, repo, "work")
	in.ReleaseSize = size
	in.ChangelogTasks = entryTasking("1.2.1") // names the version the size plans
	in.ChangeRecord = &changerecord.ChangeRecord{AutomatonID: "a1", Packages: []string{"main"}}

	stored := runAndStore(t, in)
	if stored.ReleaseSize != ReleaseSizePatch {
		t.Errorf("size %q, want patch, the bug's built-in size", stored.ReleaseSize)
	}
	if stored.PlannedVersion != "1.2.1" {
		t.Errorf("planned %q, want 1.2.1, the next patch over the base 1.2.0", stored.PlannedVersion)
	}
	if len(stored.Findings) != 0 {
		t.Errorf("findings %+v, want none: the entry names 1.2.1, which the plan names", stored.Findings)
	}
	if !strings.Contains(stored.ReadmeLine, "1.2.1") || !strings.Contains(stored.ReadmeLine, "2026-11-09") {
		t.Errorf("readme line %q, want 1.2.1 and 2026-11-09", stored.ReadmeLine)
	}
}

// TestSizingFeatureAutomatonWithoutExplicitSizePlansMinor pins the fix: an
// Automaton that planned a feature with no explicit release size ends up at
// the size its kind carries, minor, and the planned version is the next
// minor over the base ref's version.
func TestSizingFeatureAutomatonWithoutExplicitSizePlansMinor(t *testing.T) {
	repo := runnerRepo(t)
	pipeline := config.MergePipelineConfig{VersionFiles: []string{"version.go"}}
	size := kindSizeFor(t, pipeline, "demo", config.ReleaseKindFeature)
	if size != ReleaseSizeMinor {
		t.Fatalf("built-in size for a feature is %q, want minor", size)
	}

	in := runnerInput(t, repo, "work")
	in.ReleaseSize = size
	in.ChangelogTasks = entryTasking("1.3.0")
	in.ChangeRecord = &changerecord.ChangeRecord{AutomatonID: "a1", Packages: []string{"main"}}

	stored := runAndStore(t, in)
	if stored.ReleaseSize != ReleaseSizeMinor {
		t.Errorf("size %q, want minor, the feature's built-in size", stored.ReleaseSize)
	}
	if stored.PlannedVersion != "1.3.0" {
		t.Errorf("planned %q, want 1.3.0, the next minor over the base 1.2.0", stored.PlannedVersion)
	}
	if len(stored.Findings) != 0 {
		t.Errorf("findings %+v, want none: the entry names 1.3.0, which the plan names", stored.Findings)
	}
	if !strings.Contains(stored.ReadmeLine, "1.3.0") || !strings.Contains(stored.ReadmeLine, "2026-11-09") {
		t.Errorf("readme line %q, want 1.3.0 and 2026-11-09", stored.ReadmeLine)
	}
}

// TestSizingExplicitReleaseSizeWinsOverKind pins the layering: the size
// recorded on the Automaton beats the size its kind carries, whichever way
// it differs from it.
func TestSizingExplicitReleaseSizeWinsOverKind(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		explicit ReleaseSize
		planned  string
	}{
		{"bug with explicit minor beats its patch", config.ReleaseKindBug, ReleaseSizeMinor, "1.3.0"},
		{"feature with explicit patch beats its minor", config.ReleaseKindFeature, ReleaseSizePatch, "1.2.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := runnerRepo(t)
			pipeline := config.MergePipelineConfig{VersionFiles: []string{"version.go"}}
			kindSize := kindSizeFor(t, pipeline, "demo", tc.kind)
			if kindSize == tc.explicit {
				t.Fatalf("the test needs an explicit size that differs from the kind's %q", kindSize)
			}

			in := runnerInput(t, repo, "work")
			in.ReleaseSize = tc.explicit
			in.ChangelogTasks = entryTasking(tc.planned)

			stored := runAndStore(t, in)
			if stored.ReleaseSize != tc.explicit {
				t.Errorf("size %q, want the explicit %q over the kind's %q", stored.ReleaseSize, tc.explicit, kindSize)
			}
			if stored.PlannedVersion != tc.planned {
				t.Errorf("planned %q, want %q, the base 1.2.0 bumped by the explicit size", stored.PlannedVersion, tc.planned)
			}
			if len(stored.Findings) != 0 {
				t.Errorf("findings %+v, want none: the entry names %s, which the plan names", stored.Findings, tc.planned)
			}
		})
	}
}

// TestSizingNoneKeepsTheUnreleasedState pins the fix: an Automaton whose
// release size is none keeps its no-version state — no planned version and
// no README line — whatever its kind carries, and without an entry that
// state is not a finding: there is no version for the entry to name.
func TestSizingNoneKeepsTheUnreleasedState(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	in.ReleaseSize = ReleaseSizeNone // explicit none beats the feature's built-in minor
	in.ChangelogTasks = nil          // no entry at all

	stored := runAndStore(t, in)
	if stored.ReleaseSize != ReleaseSizeNone {
		t.Errorf("size %q, want the explicit none", stored.ReleaseSize)
	}
	if stored.PlannedVersion != "" || stored.ReadmeLine != "" {
		t.Errorf("planned %q readme %q, want the unreleased state", stored.PlannedVersion, stored.ReadmeLine)
	}
	if len(stored.Findings) != 0 {
		t.Errorf("findings %+v, want none: with no planned version the changelog entry is not finding material", stored.Findings)
	}
}
