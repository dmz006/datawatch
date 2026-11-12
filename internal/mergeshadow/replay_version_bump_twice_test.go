package mergeshadow

import (
	"reflect"
	"testing"
)

// The trial tree's version files, as the planner sees them. The base is the
// default branch version, 1.4.2.
const versionBumpBase = "1.4.2"

var versionBumpFiles = []string{"cmd/datawatch/main.go", "internal/server/api.go"}

func treeAt(value string) map[string]string {
	out := map[string]string{}
	for _, p := range versionBumpFiles {
		out[p] = value
	}
	return out
}

func TestPlanVersionSteps(t *testing.T) {
	cases := []struct {
		size ReleaseSize
		want string
	}{
		{ReleaseSizePatch, "1.4.3"},
		{ReleaseSizeMinor, "1.5.0"},
		{ReleaseSizeMajor, "2.0.0"},
	}
	for _, c := range cases {
		got, err := PlanVersion(versionBumpBase, c.size)
		if err != nil || got != c.want {
			t.Errorf("PlanVersion(%s, %s) = %q, %v; want %q", versionBumpBase, c.size, got, err, c.want)
		}
	}
}

func TestPlanVersionRejectsUnplannableInput(t *testing.T) {
	if _, err := PlanVersion("v1.4", ReleaseSizePatch); err == nil {
		t.Error("non X.Y.Z base accepted")
	}
	if _, err := PlanVersion(versionBumpBase, ReleaseSizeNone); err == nil {
		t.Error("release size none accepted")
	}
	if _, err := PlanVersion(versionBumpBase, ReleaseSize("huge")); err == nil {
		t.Error("unknown release size accepted")
	}
}

// Planning twice on the same inputs gives the same planned version, and the
// result never depends on the trial tree's version.
func TestPlanVersionBumpSamePlanTwice(t *testing.T) {
	first, err := PlanVersionBump(versionBumpBase, ReleaseSizeMinor, treeAt("1.4.2"))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := PlanVersionBump(versionBumpBase, ReleaseSizeMinor, treeAt("1.4.2"))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.Planned != "1.5.0" || second.Planned != first.Planned {
		t.Errorf("planned first %q second %q, want 1.5.0 both", first.Planned, second.Planned)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("runs differ: %+v vs %+v", first, second)
	}
}

func TestPlanVersionBumpIgnoresTrialVersion(t *testing.T) {
	fromBase, err := PlanVersion(versionBumpBase, ReleaseSizePatch)
	if err != nil {
		t.Fatal(err)
	}
	for _, trial := range []string{"1.4.2", "1.4.3", "1.5.0", "9.9.9", ""} {
		plan, err := PlanVersionBump(versionBumpBase, ReleaseSizePatch, treeAt(trial))
		if err != nil {
			t.Fatalf("trial %q: %v", trial, err)
		}
		if plan.Planned != fromBase {
			t.Errorf("trial %q changed planned to %q, want %q", trial, plan.Planned, fromBase)
		}
	}
}

// A second run over a tree that already carries the bump reports already at
// planned version and lists nothing to change.
func TestPlanVersionBumpAlreadyAppliedChangesNothing(t *testing.T) {
	plan, err := PlanVersionBump(versionBumpBase, ReleaseSizeMinor, treeAt("1.5.0"))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.AlreadyAtPlanned {
		t.Error("tree at planned version not reported as already at planned")
	}
	if len(plan.Stale) != 0 {
		t.Errorf("Stale = %v, want none", plan.Stale)
	}
	if plan.Planned != "1.5.0" {
		t.Errorf("Planned = %q, want 1.5.0", plan.Planned)
	}
}

// A tree at the base version needs the bump: both files are stale.
func TestPlanVersionBumpFreshTreeNeedsBump(t *testing.T) {
	plan, err := PlanVersionBump(versionBumpBase, ReleaseSizePatch, treeAt("1.4.2"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyAtPlanned {
		t.Error("base-version tree reported as already at planned")
	}
	want := []string{"cmd/datawatch/main.go", "internal/server/api.go"}
	if !reflect.DeepEqual(plan.Stale, want) {
		t.Errorf("Stale = %v, want %v", plan.Stale, want)
	}
}

// A tree where only one file carries the planned version is not already
// bumped; the other file is stale and is the only one to change.
func TestPlanVersionBumpPartialTreeIsStale(t *testing.T) {
	trial := map[string]string{
		"cmd/datawatch/main.go":  "1.4.3",
		"internal/server/api.go": "1.4.2",
	}
	plan, err := PlanVersionBump(versionBumpBase, ReleaseSizePatch, trial)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyAtPlanned {
		t.Error("partially bumped tree reported as already at planned")
	}
	if !reflect.DeepEqual(plan.Stale, []string{"internal/server/api.go"}) {
		t.Errorf("Stale = %v, want only internal/server/api.go", plan.Stale)
	}
}

// A tree with no version files at all is not "already at planned": there is
// nothing to confirm, so the planner must not claim the bump is present.
func TestPlanVersionBumpNoVersionFilesNotAlreadyAt(t *testing.T) {
	plan, err := PlanVersionBump(versionBumpBase, ReleaseSizePatch, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyAtPlanned {
		t.Error("tree with no version files reported as already at planned")
	}
}

// A file that holds no version value counts as stale, never as bumped.
func TestPlanVersionBumpMissingValueIsStale(t *testing.T) {
	trial := map[string]string{
		"cmd/datawatch/main.go":  "1.4.3",
		"internal/server/api.go": "",
	}
	plan, err := PlanVersionBump(versionBumpBase, ReleaseSizePatch, trial)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyAtPlanned || !reflect.DeepEqual(plan.Stale, []string{"internal/server/api.go"}) {
		t.Errorf("plan %+v: missing value should be stale", plan)
	}
}

func TestPlanVersionBumpPropagatesBaseError(t *testing.T) {
	if _, err := PlanVersionBump("bogus", ReleaseSizePatch, treeAt("1.4.2")); err == nil {
		t.Error("bad base accepted")
	}
}
