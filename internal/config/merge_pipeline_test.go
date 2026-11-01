package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func strp(s string) *string { return &s }

type fakeLLMs map[string]bool

func (f fakeLLMs) ResolveKind(name string) (string, bool) {
	ok, found := f[name]
	return "kind", found && ok
}

func TestMergePipelineConfig_Defaults(t *testing.T) {
	var c MergePipelineConfig
	if !c.IsZero() {
		t.Fatal("zero block not zero")
	}
	if c.EffectiveFileName("p") != ".datawatch/merge-pipeline.yaml" {
		t.Fatal("file name default")
	}
	if !reflect.DeepEqual(c.EffectiveSourceOrder("p"), []string{"file", "record"}) {
		t.Fatal("source order default")
	}
	if c.EffectiveFallbackOnBadFile("p") != "record" {
		t.Fatal("fallback default")
	}
	if c.EffectiveConflictPolicy("p") != ConflictPolicyMechanicalAndModel {
		t.Fatal("policy default")
	}
	if c.EffectiveConflictModel("p") != MergePipelineDefaultConflictModel || MergePipelineDefaultConflictModel == "" {
		t.Fatal("model default")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("zero block must validate: %v", err)
	}
	// The default order is a copy.
	c.EffectiveSourceOrder("p")[0] = "x"
	if MergePipelineDefaultSourceOrder()[0] != "file" {
		t.Fatal("default order aliased")
	}
}

func TestMergePipelineConfig_GlobalAndProjectOverride(t *testing.T) {
	c := MergePipelineConfig{
		FileName:          "ci/pipe.yaml",
		SourceOrder:       []string{"record", "file"},
		FallbackOnBadFile: MergePipelineFallbackDefault,
		ConflictPolicy:    ConflictPolicyMechanical,
		ConflictModel:     strp("global-model"),
		Projects: map[string]MergePipelineOverride{
			"a": {
				FileName:          "other.yaml",
				SourceOrder:       []string{"file"},
				FallbackOnBadFile: MergePipelineFallbackNone,
				ConflictPolicy:    ConflictPolicyStopAndAsk,
				ConflictModel:     strp("project-model"),
			},
			"empty": {ConflictModel: strp("")},
		},
	}
	if c.EffectiveFileName("b") != "ci/pipe.yaml" || c.EffectiveFileName("a") != "other.yaml" {
		t.Fatal("file name")
	}
	if !reflect.DeepEqual(c.EffectiveSourceOrder("b"), []string{"record", "file"}) ||
		!reflect.DeepEqual(c.EffectiveSourceOrder("a"), []string{"file"}) {
		t.Fatal("source order")
	}
	if c.EffectiveFallbackOnBadFile("b") != "default" || c.EffectiveFallbackOnBadFile("a") != "none" {
		t.Fatal("fallback")
	}
	if c.EffectiveConflictPolicy("b") != "mechanical" || c.EffectiveConflictPolicy("a") != "stop_and_ask" {
		t.Fatal("policy")
	}
	if c.EffectiveConflictModel("b") != "global-model" || c.EffectiveConflictModel("a") != "project-model" {
		t.Fatal("model")
	}
	// An explicit empty project model beats the global value.
	if c.EffectiveConflictModel("empty") != "" {
		t.Fatal("explicit empty project model must win")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMergePipelineConfig_ValidateEachKey(t *testing.T) {
	cases := []struct {
		name string
		c    MergePipelineConfig
		want string // substring of the error, "" for valid
	}{
		{"file name absolute", MergePipelineConfig{FileName: "/etc/x"}, "autonomous.merge_pipeline.file_name"},
		{"file name windows drive", MergePipelineConfig{FileName: "C:x"}, "file_name"},
		{"file name backslash", MergePipelineConfig{FileName: `a\b`}, "file_name"},
		{"file name escapes", MergePipelineConfig{FileName: "../x.yaml"}, "file_name"},
		{"file name directory", MergePipelineConfig{FileName: "dir/"}, "file_name"},
		{"file name dot", MergePipelineConfig{FileName: "."}, "file_name"},
		{"file name ok", MergePipelineConfig{FileName: "a/b.yaml"}, ""},
		{"source unknown", MergePipelineConfig{SourceOrder: []string{"file", "cloud"}}, "source_order[1]"},
		{"source duplicate", MergePipelineConfig{SourceOrder: []string{"file", "file"}}, "listed twice"},
		{"source ok", MergePipelineConfig{SourceOrder: []string{"record"}}, ""},
		{"fallback bad", MergePipelineConfig{FallbackOnBadFile: "ignore"}, "fallback_on_bad_file"},
		{"fallback ok", MergePipelineConfig{FallbackOnBadFile: "none"}, ""},
		{"policy bad", MergePipelineConfig{ConflictPolicy: "yolo"}, "conflict_policy"},
		{"policy ok", MergePipelineConfig{ConflictPolicy: "mechanical"}, ""},
		{"model spaces", MergePipelineConfig{ConflictModel: strp("a b")}, "conflict_model"},
		{"model padded", MergePipelineConfig{ConflictModel: strp(" a")}, "conflict_model"},
		{"model empty ok", MergePipelineConfig{ConflictModel: strp("")}, ""},
		{"project blank name", MergePipelineConfig{Projects: map[string]MergePipelineOverride{" ": {}}}, "project name is empty"},
		{"project file", MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {FileName: "/x"}}}, "projects.p.file_name"},
		{"project source", MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {SourceOrder: []string{"x"}}}}, "projects.p.source_order"},
		{"project fallback", MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {FallbackOnBadFile: "x"}}}, "projects.p.fallback_on_bad_file"},
		{"project policy", MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {ConflictPolicy: "x"}}}, "projects.p.conflict_policy"},
		{"project model", MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {ConflictModel: strp("a b")}}}, "projects.p.conflict_model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestMergePipelineConfig_ResolveConflictPolicy(t *testing.T) {
	reg := fakeLLMs{MergePipelineDefaultConflictModel: true, "off": false}
	var c MergePipelineConfig

	p, m, why := c.ResolveConflictPolicy("p", reg)
	if p != ConflictPolicyMechanicalAndModel || m != MergePipelineDefaultConflictModel || why != "" {
		t.Fatalf("default: %q %q %q", p, m, why)
	}
	// Registry lacks the model: stop and ask, never a weaker model.
	p, m, why = c.ResolveConflictPolicy("p", fakeLLMs{"other-model": true})
	if p != ConflictPolicyStopAndAsk || m != "" || why == "" {
		t.Fatalf("missing: %q %q %q", p, m, why)
	}
	// Disabled entry.
	c.ConflictModel = strp("off")
	if p, m, _ := c.ResolveConflictPolicy("p", reg); p != ConflictPolicyStopAndAsk || m != "" {
		t.Fatalf("disabled: %q %q", p, m)
	}
	// Explicit empty.
	c.ConflictModel = strp("")
	if p, _, why := c.ResolveConflictPolicy("p", reg); p != ConflictPolicyStopAndAsk || why == "" {
		t.Fatalf("empty: %q %q", p, why)
	}
	// No registry.
	c.ConflictModel = nil
	if p, _, why := c.ResolveConflictPolicy("p", nil); p != ConflictPolicyStopAndAsk || why == "" {
		t.Fatalf("nil registry: %q %q", p, why)
	}
	// Policies that need no model are untouched.
	for _, pol := range []string{ConflictPolicyStopAndAsk, ConflictPolicyMechanical} {
		c = MergePipelineConfig{ConflictPolicy: pol, ConflictModel: strp("")}
		if p, m, why := c.ResolveConflictPolicy("p", nil); p != pol || m != "" || why != "" {
			t.Fatalf("%s: %q %q %q", pol, p, m, why)
		}
	}
	// A per-project policy override applies.
	c = MergePipelineConfig{Projects: map[string]MergePipelineOverride{"a": {ConflictPolicy: ConflictPolicyMechanical}}}
	if p, _, _ := c.ResolveConflictPolicy("a", reg); p != ConflictPolicyMechanical {
		t.Fatalf("override: %q", p)
	}
}

func TestMergePipelineConfig_YAMLRoundTrip(t *testing.T) {
	in := MergePipelineConfig{
		FileName:          "x.yaml",
		SourceOrder:       []string{"record", "file"},
		FallbackOnBadFile: "none",
		ConflictPolicy:    "mechanical",
		ConflictModel:     strp(""),
		Projects: map[string]MergePipelineOverride{
			"a": {FileName: "y.yaml", SourceOrder: []string{"file"}, ConflictModel: strp("m")},
		},
	}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out MergePipelineConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed the block:\n%s\n%+v\n%+v", data, in, out)
	}
	if out.ConflictModel == nil || *out.ConflictModel != "" {
		t.Fatal("explicit empty conflict_model lost")
	}
}

func TestMergePipelineConfig_YAMLUnsetStaysUnset(t *testing.T) {
	data, err := yaml.Marshal(struct {
		M MergePipelineConfig `yaml:"merge_pipeline,omitempty"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "merge_pipeline") {
		t.Fatalf("unset block written: %s", data)
	}
	var out MergePipelineConfig
	if err := yaml.Unmarshal([]byte("conflict_policy: stop_and_ask\n"), &out); err != nil {
		t.Fatal(err)
	}
	if out.ConflictModel != nil || out.EffectiveConflictPolicy("") != ConflictPolicyStopAndAsk {
		t.Fatalf("got %+v", out)
	}
}

func TestMergePipelineConfig_UnderAutonomous(t *testing.T) {
	var a AutonomousConfig
	src := "merge_pipeline:\n  conflict_policy: mechanical\n  projects:\n    p:\n      file_name: z.yaml\n"
	if err := yaml.Unmarshal([]byte(src), &a); err != nil {
		t.Fatal(err)
	}
	if a.MergePipeline.EffectiveConflictPolicy("q") != "mechanical" || a.MergePipeline.EffectiveFileName("p") != "z.yaml" {
		t.Fatalf("got %+v", a.MergePipeline)
	}
}

func TestMergePipelineConfig_CloneIsDeep(t *testing.T) {
	in := MergePipelineConfig{
		SourceOrder:   []string{"file"},
		ConflictModel: strp("m"),
		Projects:      map[string]MergePipelineOverride{"a": {SourceOrder: []string{"record"}, ConflictModel: strp("n")}},
	}
	out := in.Clone()
	out.SourceOrder[0] = "x"
	*out.ConflictModel = "x"
	o := out.Projects["a"]
	o.SourceOrder[0] = "x"
	*o.ConflictModel = "x"
	if in.SourceOrder[0] != "file" || *in.ConflictModel != "m" ||
		in.Projects["a"].SourceOrder[0] != "record" || *in.Projects["a"].ConflictModel != "n" {
		t.Fatal("clone aliases the original")
	}
	var zero MergePipelineConfig
	z := zero.Clone()
	if z.SourceOrder != nil || z.ConflictModel != nil || z.Projects != nil {
		t.Fatal("clone of zero must stay unset")
	}
}
