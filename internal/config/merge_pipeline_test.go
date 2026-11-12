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

// releaseApprovalModes builds a ReleaseApprovalModes for the table tests.
func releaseApprovalModes(patch, minor, major string) ReleaseApprovalModes {
	return ReleaseApprovalModes{Patch: patch, Minor: minor, Major: major}
}

func TestMergePipelineConfig_ReleaseApprovalDefaults(t *testing.T) {
	var c MergePipelineConfig
	cases := []struct {
		size, want string
	}{
		{ReleaseSizePatch, ReleaseApprovalAuto},
		{ReleaseSizeMinor, ReleaseApprovalAuto},
		{ReleaseSizeMajor, ReleaseApprovalAsk},
	}
	for _, tc := range cases {
		if got := c.EffectiveReleaseApproval("p", tc.size); got != tc.want {
			t.Fatalf("default %s = %q, want %q", tc.size, got, tc.want)
		}
	}
	if got := c.EffectiveReleaseApprovalModes("p"); got != releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAuto, ReleaseApprovalAsk) {
		t.Fatalf("default modes = %+v", got)
	}
}

func TestMergePipelineConfig_ReleaseApprovalGlobalOverridesDefault(t *testing.T) {
	modes := []string{ReleaseApprovalAuto, ReleaseApprovalAsk, ReleaseApprovalManual}
	sizes := []string{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor}
	for _, sz := range sizes {
		for _, m := range modes {
			var c MergePipelineConfig
			switch sz {
			case ReleaseSizePatch:
				c.ReleaseApproval.Patch = m
			case ReleaseSizeMinor:
				c.ReleaseApproval.Minor = m
			case ReleaseSizeMajor:
				c.ReleaseApproval.Major = m
			}
			if got := c.EffectiveReleaseApproval("p", sz); got != m {
				t.Fatalf("global %s=%s resolved %q", sz, m, got)
			}
			// Other sizes stay on their defaults.
			for _, other := range sizes {
				if other == sz {
					continue
				}
				want := DefaultReleaseApprovalPatch
				switch other {
				case ReleaseSizeMajor:
					want = DefaultReleaseApprovalMajor
				case ReleaseSizeMinor:
					want = DefaultReleaseApprovalMinor
				}
				if got := c.EffectiveReleaseApproval("p", other); got != want {
					t.Fatalf("global %s=%s leaked into %s: %q, want %q", sz, m, other, got, want)
				}
			}
		}
	}
}

func TestMergePipelineConfig_ReleaseApprovalProjectOverridePrecedence(t *testing.T) {
	modes := []string{ReleaseApprovalAuto, ReleaseApprovalAsk, ReleaseApprovalManual}
	sizes := []string{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor}
	for _, sz := range sizes {
		for _, proj := range modes {
			for _, glob := range modes {
				var c MergePipelineConfig
				c.ReleaseApproval = releaseApprovalModes(glob, glob, glob)
				o := MergePipelineOverride{}
				switch sz {
				case ReleaseSizePatch:
					o.ReleaseApproval.Patch = proj
				case ReleaseSizeMinor:
					o.ReleaseApproval.Minor = proj
				case ReleaseSizeMajor:
					o.ReleaseApproval.Major = proj
				}
				c.Projects = map[string]MergePipelineOverride{"a": o}
				if got := c.EffectiveReleaseApproval("a", sz); got != proj {
					t.Fatalf("project %s=%s over global %s resolved %q", sz, proj, glob, got)
				}
				// Another project still sees the global value.
				if got := c.EffectiveReleaseApproval("b", sz); got != glob {
					t.Fatalf("project b %s resolved %q, want global %q", sz, got, glob)
				}
			}
		}
	}
	// Project override beats the built-in default when there is no global.
	c := MergePipelineConfig{Projects: map[string]MergePipelineOverride{
		"a": {ReleaseApproval: releaseApprovalModes(ReleaseApprovalManual, ReleaseApprovalAsk, ReleaseApprovalAuto)},
	}}
	if got := c.EffectiveReleaseApprovalModes("a"); got != releaseApprovalModes(ReleaseApprovalManual, ReleaseApprovalAsk, ReleaseApprovalAuto) {
		t.Fatalf("project over default = %+v", got)
	}
}

func TestMergePipelineConfig_ReleaseApprovalPartialOverrideFallsThrough(t *testing.T) {
	c := MergePipelineConfig{
		ReleaseApproval: releaseApprovalModes("", ReleaseApprovalManual, ""),
		Projects: map[string]MergePipelineOverride{
			"a": {ReleaseApproval: releaseApprovalModes(ReleaseApprovalAsk, "", "")},
		},
	}
	want := releaseApprovalModes(ReleaseApprovalAsk, ReleaseApprovalManual, DefaultReleaseApprovalMajor)
	if got := c.EffectiveReleaseApprovalModes("a"); got != want {
		t.Fatalf("partial override = %+v, want %+v", got, want)
	}
}

func TestMergePipelineConfig_ReleaseApprovalUnknownSizeIsAsk(t *testing.T) {
	c := MergePipelineConfig{
		ReleaseApproval: releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAuto, ReleaseApprovalAuto),
		Projects: map[string]MergePipelineOverride{
			"a": {ReleaseApproval: releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAuto, ReleaseApprovalAuto)},
		},
	}
	for _, sz := range []string{"", "huge", "PATCH", " minor"} {
		if got := c.EffectiveReleaseApproval("a", sz); got != ReleaseApprovalAsk {
			t.Fatalf("size %q resolved %q, want ask", sz, got)
		}
		if got := c.EffectiveReleaseApproval("", sz); got != ReleaseApprovalAsk {
			t.Fatalf("size %q with no project resolved %q, want ask", sz, got)
		}
	}
}

func TestMergePipelineConfig_ReleaseApprovalNoOverrideFallsThroughToGlobal(t *testing.T) {
	c := MergePipelineConfig{
		ReleaseApproval: releaseApprovalModes(ReleaseApprovalManual, ReleaseApprovalAsk, ReleaseApprovalAuto),
		Projects: map[string]MergePipelineOverride{
			"other": {ReleaseApproval: releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAuto, ReleaseApprovalAuto)},
		},
	}
	want := releaseApprovalModes(ReleaseApprovalManual, ReleaseApprovalAsk, ReleaseApprovalAuto)
	for _, proj := range []string{"", "unlisted", "other-thing"} {
		if got := c.EffectiveReleaseApprovalModes(proj); got != want {
			t.Fatalf("project %q = %+v, want global %+v", proj, got, want)
		}
	}
}

func TestMergePipelineConfig_ReleaseApprovalCloneIsDeep(t *testing.T) {
	in := MergePipelineConfig{
		ReleaseApproval: releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAsk, ReleaseApprovalManual),
		Projects: map[string]MergePipelineOverride{
			"a": {ReleaseApproval: releaseApprovalModes(ReleaseApprovalManual, "", "")},
		},
	}
	out := in.Clone()
	out.ReleaseApproval.Patch = ReleaseApprovalManual
	o := out.Projects["a"]
	o.ReleaseApproval.Patch = ReleaseApprovalAsk
	out.Projects["a"] = o
	out.Projects["b"] = MergePipelineOverride{ReleaseApproval: releaseApprovalModes(ReleaseApprovalAsk, "", "")}

	if in.ReleaseApproval != releaseApprovalModes(ReleaseApprovalAuto, ReleaseApprovalAsk, ReleaseApprovalManual) {
		t.Fatalf("global block aliased: %+v", in.ReleaseApproval)
	}
	if in.Projects["a"].ReleaseApproval.Patch != ReleaseApprovalManual {
		t.Fatalf("project override aliased: %+v", in.Projects["a"].ReleaseApproval)
	}
	if _, ok := in.Projects["b"]; ok {
		t.Fatal("project map aliased")
	}
}

func TestMergePipelineConfig_ReleaseApprovalValidateRejectsInvalidGlobal(t *testing.T) {
	for _, sz := range []string{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor} {
		var c MergePipelineConfig
		key := "autonomous.merge_pipeline.release_approval." + sz
		switch sz {
		case ReleaseSizePatch:
			c.ReleaseApproval.Patch = "yolo"
		case ReleaseSizeMinor:
			c.ReleaseApproval.Minor = "yolo"
		case ReleaseSizeMajor:
			c.ReleaseApproval.Major = "yolo"
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s: error = %v, want it to name %q", sz, err, key)
		}
	}
}

func TestMergePipelineConfig_ReleaseApprovalValidateRejectsInvalidProjectOverride(t *testing.T) {
	for _, sz := range []string{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor} {
		var o MergePipelineOverride
		switch sz {
		case ReleaseSizePatch:
			o.ReleaseApproval.Patch = "yolo"
		case ReleaseSizeMinor:
			o.ReleaseApproval.Minor = "yolo"
		case ReleaseSizeMajor:
			o.ReleaseApproval.Major = "yolo"
		}
		c := MergePipelineConfig{Projects: map[string]MergePipelineOverride{"proj": o}}
		key := "autonomous.merge_pipeline.projects.proj.release_approval." + sz
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s: error = %v, want it to name %q", sz, err, key)
		}
	}
}

func TestMergePipelineConfig_ReleaseApprovalValidateAcceptsValidModes(t *testing.T) {
	for _, m := range []string{ReleaseApprovalAuto, ReleaseApprovalAsk, ReleaseApprovalManual, ""} {
		global := MergePipelineConfig{ReleaseApproval: releaseApprovalModes(m, m, m)}
		if err := global.Validate(); err != nil {
			t.Fatalf("global mode %q rejected: %v", m, err)
		}
		proj := MergePipelineConfig{Projects: map[string]MergePipelineOverride{
			"p": {ReleaseApproval: releaseApprovalModes(m, m, m)},
		}}
		if err := proj.Validate(); err != nil {
			t.Fatalf("project mode %q rejected: %v", m, err)
		}
	}
	// An unset block validates and is zero.
	var unset MergePipelineConfig
	if err := unset.Validate(); err != nil || !unset.IsZero() {
		t.Fatalf("unset release_approval: err=%v zero=%v", err, unset.IsZero())
	}
}

// TestMergePipelineReleaseApprovalValidateLoaded checks that a bad release
// approval mode in config.yaml is refused on load, not only on PUT.
func TestMergePipelineReleaseApprovalValidateLoaded(t *testing.T) {
	cfg := DefaultConfig()
	applyDefaults(cfg)
	if err := validateLoaded(cfg); err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg.Autonomous.MergePipeline.ReleaseApproval.Major = "sometimes"
	err := validateLoaded(cfg)
	if err == nil {
		t.Fatal("validateLoaded with a bad global release_approval.major: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "release_approval.major") {
		t.Errorf("error %q does not name the key", err)
	}
}
