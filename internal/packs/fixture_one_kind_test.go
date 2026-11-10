package packs

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureOneKindDir is the testdata fixture pack that targets only the
// "feature" kind and contributes a single task, "Extra check".
const fixtureOneKindDir = "testdata/fixture-one-kind"

const fixtureOneKindTask = "Extra check"

// The six built-in standard tasks, in manifest order.
var builtinTitles = []string{
	"Changelog entry for {{feature}}",
	"Parity issue draft for {{feature}}",
	"Docs set for {{feature}}",
	"Bounded load-step rule for {{feature}}",
	"Final gate for {{feature}}",
	"Release version note for {{feature}}",
}

var allKinds = []string{KindFeature, KindBug, KindOther, KindMaintenance}

func TestFixtureOneKindAddsTaskToFeature(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	fixture, err := LoadPackFolder(filepath.Join(".", fixtureOneKindDir))
	if err != nil {
		t.Fatalf("LoadPackFolder(fixture-one-kind): %v", err)
	}

	// The fixture names only the "feature" kind.
	if len(fixture.Kinds) != 1 || fixture.Kinds[0] != KindFeature {
		t.Fatalf("fixture Kinds = %v, want [feature]", fixture.Kinds)
	}

	// Resolving "feature" with both packs gives the built-in list followed
	// by the fixture task.
	got, err := ResolveTaskList(KindFeature, []*Pack{builtin, fixture})
	if err != nil {
		t.Fatalf("ResolveTaskList(feature): %v", err)
	}
	wantTitles := append(append([]string{}, builtinTitles...), fixtureOneKindTask)
	gotTitles := titlesOf(got)
	if len(gotTitles) != len(wantTitles) {
		t.Fatalf("feature: got %d tasks %v, want %d tasks %v",
			len(gotTitles), gotTitles, len(wantTitles), wantTitles)
	}
	for i, w := range wantTitles {
		if gotTitles[i] != w {
			t.Errorf("feature task %d = %q, want %q", i, gotTitles[i], w)
		}
	}

	// The last task must come from the fixture pack.
	last := got[len(got)-1]
	if last.PackName != "fixture-one-kind" {
		t.Errorf("last task PackName = %q, want fixture-one-kind", last.PackName)
	}
}

func TestFixtureOneKindLeavesOtherKindsUnchanged(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	fixture, err := LoadPackFolder(filepath.Join(".", fixtureOneKindDir))
	if err != nil {
		t.Fatalf("LoadPackFolder(fixture-one-kind): %v", err)
	}

	for _, kind := range allKinds {
		if kind == KindFeature {
			continue // covered by TestFixtureOneKindAddsTaskToFeature
		}
		got, err := ResolveTaskList(kind, []*Pack{builtin, fixture})
		if err != nil {
			t.Fatalf("ResolveTaskList(%s): %v", kind, err)
		}
		gotTitles := titlesOf(got)
		if len(gotTitles) != len(builtinTitles) {
			t.Fatalf("%s: got %d tasks %v, want %d tasks %v",
				kind, len(gotTitles), gotTitles, len(builtinTitles), builtinTitles)
		}
		for i, w := range builtinTitles {
			if gotTitles[i] != w {
				t.Errorf("%s task %d = %q, want %q", kind, i, gotTitles[i], w)
			}
		}
	}
}

// TestFixtureOneKindLoadPackFolder verifies that the fixture pack folder
// loads cleanly through the standard path (manifest, prompt file existence,
// digest, executable verification script).
func TestFixtureOneKindLoadPackFolder(t *testing.T) {
	p, err := LoadPackFolder(filepath.Join(".", fixtureOneKindDir))
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	if p.Name != "fixture-one-kind" {
		t.Errorf("Name = %q, want fixture-one-kind", p.Name)
	}
	if p.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", p.Version)
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("Tasks = %d, want 1", len(p.Tasks))
	}
	task := p.Tasks[0]
	if task.Title != fixtureOneKindTask {
		t.Errorf("task Title = %q, want %q", task.Title, fixtureOneKindTask)
	}
	if task.Prompt != "prompts/extra-check.md" {
		t.Errorf("task Prompt = %q, want prompts/extra-check.md", task.Prompt)
	}
	if task.VerificationScript != "verify/extra-check.sh" {
		t.Errorf("task VerificationScript = %q, want verify/extra-check.sh", task.VerificationScript)
	}
	if !strings.HasSuffix(p.Digest, p.Digest[len(p.Digest)-64:]) || len(p.Digest) != 64 {
		t.Errorf("Digest = %q, want a 64-char hex sha256", p.Digest)
	}
}
