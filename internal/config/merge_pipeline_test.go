package config

import (
	"fmt"
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

func TestMergePipelineConfig_PostShipDefaultsOff(t *testing.T) {
	var c MergePipelineConfig
	if !c.IsZero() {
		t.Fatal("zero block must stay zero with post_ship unset")
	}
	var s PostShipConfig
	if !s.IsZero() {
		t.Fatal("zero post-ship block must be zero")
	}
	if s.HookEnabled("anything") {
		t.Fatal("no hook may read as enabled")
	}
	if len(s.EffectiveHooks()) != 0 {
		t.Fatal("unset hooks must read empty")
	}
	for name, got := range map[string]string{
		"registry_file":        s.EffectiveRegistryFile(),
		"registry_row_pattern": s.EffectiveRegistryRowPattern(),
		"companion_repository": s.EffectiveCompanionRepository(),
		"issue_title_template": s.EffectiveIssueTitleTemplate(),
		"notify_template":      s.EffectiveNotifyTemplate(),
	} {
		if got != "" {
			t.Fatalf("unset %s must read empty, got %q", name, got)
		}
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("unset block must validate: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("zero block must validate: %v", err)
	}

	// A valid configured block: every built-in with its targets, tag with
	// none, and a command hook.
	s = PostShipConfig{
		Enabled: true,
		Hooks: []PostShipHook{
			{Name: "row", Builtin: PostShipHookBuiltinRegistryRow, Enabled: true},
			{Name: "issue", Builtin: PostShipHookBuiltinParityIssue, Enabled: true},
			{Name: "tag", Builtin: PostShipHookBuiltinTag, Enabled: true},
			{Name: "say", Builtin: PostShipHookBuiltinNotify, Enabled: true},
			{Name: "custom", Command: "scripts/after-ship.sh", Enabled: true},
		},
		RegistryFile:        "{project}/docs/plans/README.md",
		RegistryRowPattern:  "v{version} shipped",
		CompanionRepository: "owner/companion",
		IssueTitleTemplate:  "parity for {project} v{version}",
		NotifyTemplate:      "shipped v{version}",
	}
	if s.IsZero() {
		t.Fatal("configured block is not zero")
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid block must validate: %v", err)
	}
	if !s.HookEnabled("say") || s.HookEnabled("absent") {
		t.Fatal("HookEnabled must read the named hook")
	}

	c.PostShip = s
	got := c.EffectivePostShip()
	got.Hooks[0].Enabled = false
	got.Hooks[0].Name = "mutated"
	if !c.PostShip.Hooks[0].Enabled || c.PostShip.Hooks[0].Name != "row" {
		t.Fatal("EffectivePostShip must not alias the original")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPostShipHook_ValidateEachKey(t *testing.T) {
	const p = "autonomous.merge_pipeline.post_ship."
	hook := func(name, builtin, command string, enabled bool) PostShipHook {
		return PostShipHook{Name: name, Builtin: builtin, Command: command, Enabled: enabled}
	}
	targets := PostShipConfig{
		RegistryFile:        "reg.md",
		RegistryRowPattern:  "v{version}",
		CompanionRepository: "owner/companion",
		IssueTitleTemplate:  "title {project} {version}",
		NotifyTemplate:      "v{version} out",
	}
	cases := []struct {
		name string
		s    PostShipConfig
		want string // substring of the error, "" for valid
	}{
		{"unknown builtin", PostShipConfig{Hooks: []PostShipHook{hook("h", "warp", "", true)}}, p + "hooks[0].builtin"},
		{"both builtin and command", PostShipConfig{Hooks: []PostShipHook{hook("h", "tag", "x.sh", true)}}, p + "hooks[0]"},
		{"neither builtin nor command", PostShipConfig{Hooks: []PostShipHook{hook("h", "", "", true)}}, "builtin or command"},
		{"empty name", PostShipConfig{Hooks: []PostShipHook{hook(" ", "tag", "", false)}}, "hooks[0].name"},
		{"duplicate name", PostShipConfig{Hooks: []PostShipHook{hook("h", "tag", "", false), hook("h", "tag", "", false)}}, "list"},
		{"too many hooks", PostShipConfig{Hooks: postShipHooks(21)}, "hooks: at most 20"},
		{"exactly 20 hooks ok", PostShipConfig{Hooks: postShipHooks(20)}, ""},
		{"registry row without file", PostShipConfig{Hooks: []PostShipHook{hook("h", PostShipHookBuiltinRegistryRow, "", true)}}, p + "registry_file"},
		{"registry row without pattern", PostShipConfig{
			RegistryFile: "reg.md", Hooks: []PostShipHook{hook("h", PostShipHookBuiltinRegistryRow, "", true)},
		}, p + "registry_row_pattern"},
		{"registry row with targets ok", PostShipConfig{
			RegistryFile: "reg.md", RegistryRowPattern: "v{version}",
			Hooks: []PostShipHook{hook("h", PostShipHookBuiltinRegistryRow, "", true)},
		}, ""},
		{"parity issue without repository", PostShipConfig{Hooks: []PostShipHook{hook("h", PostShipHookBuiltinParityIssue, "", true)}}, p + "companion_repository"},
		{"parity issue without title", PostShipConfig{
			CompanionRepository: "owner/companion",
			Hooks:               []PostShipHook{hook("h", PostShipHookBuiltinParityIssue, "", true)},
		}, p + "issue_title_template"},
		{"parity issue with targets ok", PostShipConfig{
			CompanionRepository: "owner/companion", IssueTitleTemplate: "t {version}",
			Hooks: []PostShipHook{hook("h", PostShipHookBuiltinParityIssue, "", true)},
		}, ""},
		{"notify without template", PostShipConfig{Hooks: []PostShipHook{hook("h", PostShipHookBuiltinNotify, "", true)}}, p + "notify_template"},
		{"notify with template ok", PostShipConfig{
			NotifyTemplate: "v{version}", Hooks: []PostShipHook{hook("h", PostShipHookBuiltinNotify, "", true)},
		}, ""},
		{"tag needs no target", PostShipConfig{Hooks: []PostShipHook{hook("h", PostShipHookBuiltinTag, "", true)}}, ""},
		{"disabled hook needs no target", PostShipConfig{
			Hooks: []PostShipHook{hook("h", PostShipHookBuiltinRegistryRow, "", false)},
		}, ""},
		{"command hook needs no builtin target", PostShipConfig{
			Hooks: []PostShipHook{hook("h", "", "x.sh --flag", true)},
		}, ""},
		{"registry file absolute", PostShipConfig{RegistryFile: "/etc/reg", Hooks: []PostShipHook{hook("h", "", "x.sh", false)}}, p + "registry_file"},
		{"registry file escapes project", PostShipConfig{RegistryFile: "../reg", Hooks: []PostShipHook{hook("h", "", "x.sh", false)}}, p + "registry_file"},
		{"registry file placeholder ok", targets, ""},
		{"first bad hook named first", PostShipConfig{Hooks: []PostShipHook{
			hook("ok", "", "x.sh", false), hook("bad", "warp", "", false)}},
			"hooks[1].builtin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
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

func postShipHooks(n int) []PostShipHook {
	hooks := make([]PostShipHook, 0, n)
	for i := 0; i < n; i++ {
		hooks = append(hooks, PostShipHook{
			Name: fmt.Sprintf("h%d", i), Command: "true", Enabled: true,
		})
	}
	return hooks
}

func TestPostShipConfig_YAMLRoundTrip(t *testing.T) {
	in := PostShipConfig{
		Enabled: true,
		Hooks: []PostShipHook{
			{Name: "row", Builtin: PostShipHookBuiltinRegistryRow, Enabled: true},
			{Name: "custom", Command: "scripts/after-ship.sh", Enabled: false},
		},
		RegistryFile:        "{project}/docs/plans/README.md",
		RegistryRowPattern:  "v{version} shipped",
		CompanionRepository: "owner/companion",
		IssueTitleTemplate:  "parity for {project} v{version}",
		NotifyTemplate:      "shipped v{version}",
	}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out PostShipConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed the block:\n%s\n%+v\n%+v", data, in, out)
	}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPostShipConfig_YAMLUnsetStaysUnset(t *testing.T) {
	data, err := yaml.Marshal(struct {
		M PostShipConfig `yaml:"post_ship,omitempty"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "post_ship") {
		t.Fatalf("unset block written: %s", data)
	}
	var out PostShipConfig
	if err := yaml.Unmarshal([]byte("enabled: true\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || len(out.Hooks) != 0 || out.EffectiveNotifyTemplate() != "" {
		t.Fatalf("partial block: %+v", out)
	}
	// Under the merge pipeline block, a block with hooks but no enabled
	// hooks reads back intact.
	var a AutonomousConfig
	src := `merge_pipeline:
  post_ship:
    enabled: true
    notify_template: "shipped v{version}"
    hooks:
      - name: say
        builtin: notify
        enabled: true
`
	if err := yaml.Unmarshal([]byte(src), &a); err != nil {
		t.Fatal(err)
	}
	ps := a.MergePipeline.EffectivePostShip()
	if !ps.Enabled || len(ps.Hooks) != 1 || ps.Hooks[0].Builtin != PostShipHookBuiltinNotify ||
		!ps.HookEnabled("say") || ps.EffectiveNotifyTemplate() != "shipped v{version}" {
		t.Fatalf("got %+v", ps)
	}
	if err := a.MergePipeline.Validate(); err != nil {
		t.Fatal(err)
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
