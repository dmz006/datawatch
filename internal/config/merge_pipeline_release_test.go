package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Release settings of the autonomous.merge_pipeline block: the kind-to-size
// map, the major trigger, the release cadence and the version scheme.

func TestMergePipelineRelease_Defaults(t *testing.T) {
	var c MergePipelineConfig
	want := map[string]string{ReleaseKindFeature: ReleaseSizeMinor, ReleaseKindBug: ReleaseSizePatch}
	if got := c.EffectiveReleaseSizeMap("p"); !reflect.DeepEqual(got, want) {
		t.Fatalf("default size map = %v, want %v", got, want)
	}
	if _, ok := c.EffectiveReleaseSizeMap("p")[ReleaseSizeMajor]; ok {
		t.Fatal("a major must never be a built-in default kind")
	}
	if got := c.EffectiveMajorTrigger("p"); got != MajorTriggerManual {
		t.Fatalf("default major trigger = %q", got)
	}
	if got := c.EffectiveReleaseCadence("p"); got != ReleaseCadencePerMerge {
		t.Fatalf("default cadence = %q", got)
	}
	if got := c.EffectiveVersionScheme("p"); got != VersionSchemeSemver {
		t.Fatalf("default version scheme = %q", got)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("zero block must validate: %v", err)
	}
}

func TestMergePipelineRelease_DefaultMapIsFresh(t *testing.T) {
	m := MergePipelineDefaultReleaseSizeMap()
	m[ReleaseKindBug] = ReleaseSizeMajor
	if MergePipelineDefaultReleaseSizeMap()[ReleaseKindBug] != ReleaseSizePatch {
		t.Fatal("editing a returned default map changed the built-in default")
	}
}

func TestMergePipelineRelease_GlobalAndProjectLayers(t *testing.T) {
	c := MergePipelineConfig{
		ReleaseSizeMap: map[string]string{"refactor": ReleaseSizePatch, ReleaseKindFeature: ReleaseSizeMajor},
		MajorTrigger:   MajorTriggerNever,
		ReleaseCadence: ReleaseCadencePerMerge,
		VersionScheme:  VersionSchemeSemver,
		Projects: map[string]MergePipelineOverride{
			"web": {
				ReleaseSizeMap: map[string]string{"docs": ReleaseSizeNone, ReleaseKindBug: ReleaseSizeMinor},
				MajorTrigger:   MajorTriggerMilestone,
			},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid block refused: %v", err)
	}

	// A global key beats the built-in default; a project key beats the global.
	got := c.EffectiveReleaseSizeMap("other")
	want := map[string]string{
		ReleaseKindFeature: ReleaseSizeMajor, ReleaseKindBug: ReleaseSizePatch, "refactor": ReleaseSizePatch,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global layer = %v, want %v", got, want)
	}
	got = c.EffectiveReleaseSizeMap("web")
	want = map[string]string{
		ReleaseKindFeature: ReleaseSizeMajor, ReleaseKindBug: ReleaseSizeMinor,
		"refactor": ReleaseSizePatch, "docs": ReleaseSizeNone,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project layer = %v, want %v", got, want)
	}

	// Per-project override beats the global major trigger; an unset project
	// key inherits the global value.
	if got := c.EffectiveMajorTrigger("web"); got != MajorTriggerMilestone {
		t.Fatalf("project major trigger = %q, want milestone", got)
	}
	if got := c.EffectiveMajorTrigger("other"); got != MajorTriggerNever {
		t.Fatalf("global major trigger = %q, want never", got)
	}
}

func TestMergePipelineRelease_ProjectOverrideBeatsGlobal(t *testing.T) {
	c := MergePipelineConfig{
		MajorTrigger:   MajorTriggerNever,
		ReleaseCadence: ReleaseCadencePerMerge,
		VersionScheme:  VersionSchemeSemver,
		Projects: map[string]MergePipelineOverride{
			"p": {MajorTrigger: MajorTriggerMilestone, ReleaseCadence: ReleaseCadencePerMerge, VersionScheme: VersionSchemeSemver},
		},
	}
	if got := c.EffectiveMajorTrigger("p"); got != MajorTriggerMilestone {
		t.Fatalf("major trigger = %q, want project milestone", got)
	}
	if got := c.EffectiveMajorTrigger("q"); got != MajorTriggerNever {
		t.Fatalf("major trigger for another project = %q, want global never", got)
	}
}

func TestMergePipelineRelease_MajorTriggerNames(t *testing.T) {
	for _, v := range []string{MajorTriggerManual, MajorTriggerMilestone, MajorTriggerNever} {
		if err := (MergePipelineConfig{MajorTrigger: v}).Validate(); err != nil {
			t.Fatalf("major_trigger %q refused: %v", v, err)
		}
	}
	err := MergePipelineConfig{MajorTrigger: "always"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.major_trigger") {
		t.Fatalf("unknown major_trigger error = %v", err)
	}
}

func TestMergePipelineRelease_SizeMapValues(t *testing.T) {
	for _, v := range []string{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor, ReleaseSizeNone} {
		c := MergePipelineConfig{ReleaseSizeMap: map[string]string{"refactor": v}}
		if err := c.Validate(); err != nil {
			t.Fatalf("size %q refused: %v", v, err)
		}
	}
	c := MergePipelineConfig{ReleaseSizeMap: map[string]string{"refactor": "huge"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.release_size_map.refactor") {
		t.Fatalf("bad size error = %v", err)
	}
	c = MergePipelineConfig{Projects: map[string]MergePipelineOverride{
		"p": {ReleaseSizeMap: map[string]string{"docs": "gigantic"}},
	}}
	err = c.Validate()
	if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.projects.p.release_size_map.docs") {
		t.Fatalf("bad project size error = %v", err)
	}
	c = MergePipelineConfig{ReleaseSizeMap: map[string]string{"bad kind": ReleaseSizePatch}}
	if err := c.Validate(); err == nil {
		t.Fatal("a kind with a space was accepted")
	}
}

func TestMergePipelineRelease_CadenceRefusals(t *testing.T) {
	cases := map[string]string{
		ReleaseCadenceBatch:  "release cadence batch is not available yet",
		ReleaseCadenceManual: "release cadence manual is not available yet",
	}
	for name, want := range cases {
		err := MergePipelineConfig{ReleaseCadence: name}.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("global cadence %q error = %v, want containing %q", name, err, want)
		}
		err = MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {ReleaseCadence: name}}}.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("project cadence %q error = %v, want containing %q", name, err, want)
		}
	}
	if err := (MergePipelineConfig{ReleaseCadence: ReleaseCadencePerMerge}).Validate(); err != nil {
		t.Fatalf("per_merge refused: %v", err)
	}
	if err := (MergePipelineConfig{ReleaseCadence: "hourly"}).Validate(); err == nil {
		t.Fatal("an unknown cadence was accepted")
	}
}

func TestMergePipelineRelease_VersionSchemeRefusals(t *testing.T) {
	err := MergePipelineConfig{VersionScheme: "calver"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "version scheme calver is not available yet") {
		t.Fatalf("global scheme error = %v", err)
	}
	err = MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {VersionScheme: "calver"}}}.Validate()
	if err == nil || !strings.Contains(err.Error(), "version scheme calver is not available yet") {
		t.Fatalf("project scheme error = %v", err)
	}
	if err := (MergePipelineConfig{VersionScheme: VersionSchemeSemver}).Validate(); err != nil {
		t.Fatalf("semver refused: %v", err)
	}
}

func TestMergePipelineRelease_CloneIsolatesMaps(t *testing.T) {
	orig := MergePipelineConfig{
		ReleaseSizeMap: map[string]string{"refactor": ReleaseSizePatch},
		Projects: map[string]MergePipelineOverride{
			"p": {ReleaseSizeMap: map[string]string{"docs": ReleaseSizeNone}},
		},
	}
	cp := orig.Clone()
	cp.ReleaseSizeMap["refactor"] = ReleaseSizeMajor
	cp.ReleaseSizeMap["extra"] = ReleaseSizeMinor
	o := cp.Projects["p"]
	o.ReleaseSizeMap["docs"] = ReleaseSizeMajor
	cp.Projects["p"] = o

	if orig.ReleaseSizeMap["refactor"] != ReleaseSizePatch || len(orig.ReleaseSizeMap) != 1 {
		t.Fatalf("global map shared with clone: %v", orig.ReleaseSizeMap)
	}
	if orig.Projects["p"].ReleaseSizeMap["docs"] != ReleaseSizeNone {
		t.Fatalf("project map shared with clone: %v", orig.Projects["p"].ReleaseSizeMap)
	}
	if (MergePipelineConfig{}).Clone().ReleaseSizeMap != nil {
		t.Fatal("an unset map must stay nil in a clone")
	}
}

func TestMergePipelineRelease_IsZeroAndYAML(t *testing.T) {
	if (MergePipelineConfig{ReleaseSizeMap: map[string]string{"x": ReleaseSizeNone}}).IsZero() {
		t.Fatal("a set size map reads as zero")
	}
	if (MergePipelineConfig{MajorTrigger: MajorTriggerNever}).IsZero() {
		t.Fatal("a set major trigger reads as zero")
	}
	var c MergePipelineConfig
	src := "release_size_map:\n  refactor: patch\nmajor_trigger: milestone\nrelease_cadence: per_merge\nversion_scheme: semver\n"
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.ReleaseSizeMap["refactor"] != ReleaseSizePatch || c.MajorTrigger != MajorTriggerMilestone {
		t.Fatalf("yaml keys not read: %+v", c)
	}
}
