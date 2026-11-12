// decompose_lint_test.go — decompose-lint + task-template + guidance
// settings: defaults, normalization, the marshal/unmarshal round-trip
// guarantees (no omitempty on the fields whose explicit clear value
// must survive), and each validation error. File-based tests write
// only into temp dirs.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDecomposeLintDefaults: a fresh DefaultConfig behaves as shadow
// mode, retry budget 2, templates enabled, no rules, no guidance.
func TestDecomposeLintDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Autonomous.DecomposeLint.Mode; got != DecomposeLintModeShadow {
		t.Fatalf("default mode = %q, want %q", got, DecomposeLintModeShadow)
	}
	if got := cfg.Autonomous.DecomposeLint.MaxRetries; got != DefaultDecomposeLintMaxRetries {
		t.Fatalf("default max_retries = %d, want %d", got, DefaultDecomposeLintMaxRetries)
	}
	if len(cfg.Autonomous.DecomposeLint.Rules) != 0 {
		t.Fatalf("default rules = %v, want none", cfg.Autonomous.DecomposeLint.Rules)
	}
	if !cfg.Autonomous.TaskTemplates.Enabled {
		t.Fatal("default task_templates.enabled = false, want true")
	}
	if cfg.Autonomous.DecomposeGuidance != "" {
		t.Fatalf("default decompose_guidance = %q, want empty", cfg.Autonomous.DecomposeGuidance)
	}
	if err := cfg.Autonomous.DecomposeLint.Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
	if err := cfg.Autonomous.TaskTemplates.Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
}

// TestDecomposeLintAbsentKeyKeepsDefaults: a config file that does not
// mention decompose_lint / task_templates / decompose_guidance at all
// loads with the shipped defaults.
func TestDecomposeLintAbsentKeyKeepsDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "DecomposeLint")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("session:\n  max_sessions: 7\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.DecomposeLint.Mode; got != "shadow" {
		t.Fatalf("absent-key mode = %q, want shadow", got)
	}
	if got := cfg.Autonomous.DecomposeLint.MaxRetries; got != 2 {
		t.Fatalf("absent-key max_retries = %d, want 2", got)
	}
	if !cfg.Autonomous.TaskTemplates.Enabled {
		t.Fatal("absent-key task_templates.enabled = false, want true")
	}
	if cfg.Autonomous.DecomposeGuidance != "" {
		t.Fatalf("absent-key decompose_guidance = %q, want empty", cfg.Autonomous.DecomposeGuidance)
	}
}

func TestNormalizeDecomposeLintMode(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", DecomposeLintModeShadow, false}, // unset -> safe default
		{"off", "off", false},
		{"shadow", "shadow", false},
		{"enforce", "enforce", false},
		{"ENFORCE", "enforce", false}, // case-insensitive
		{" off ", "off", false},       // trimmed
		{"strict", "", true},          // unknown -> rejected, not guessed
		{"enforc", "", true},          // typo -> rejected
		{"SHADOW ON", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeDecomposeLintMode(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeDecomposeLintMode(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeDecomposeLintMode(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeDecomposeLintMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeDecomposeLintMaxRetries(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, // 0 (never retry) is a valid explicit value
		{1, 1},
		{2, 2},
		{5, 5}, // cap is allowed
		{6, 5}, // above cap clamps
		{100, 5},
		{-1, 0}, // negative clamps to 0
		{-40, 0},
	}
	for _, c := range cases {
		if got := NormalizeDecomposeLintMaxRetries(c.in); got != c.want {
			t.Errorf("NormalizeDecomposeLintMaxRetries(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNormalizeDecomposeLintSeverity(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "warn", false}, // unset -> lenient default
		{"warn", "warn", false},
		{"block", "block", false},
		{"BLOCK", "block", false},
		{"hard", "", true},
		{"stop", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeDecomposeLintSeverity(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeDecomposeLintSeverity(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeDecomposeLintSeverity(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeDecomposeLintSeverity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDecomposeLintSettingsRoundTrip proves the no-omitempty guarantee
// at the JSON layer (the exact path handleAutonomousConfig uses): an
// explicit off mode, an explicit 0 retry budget and an explicitly
// disabled template set all survive json.Marshal + json.Unmarshal onto
// a config that previously held a different value.
func TestDecomposeLintSettingsRoundTrip(t *testing.T) {
	prev := AutonomousConfig{
		DecomposeLint: DecomposeLintConfig{
			Mode:       "enforce",
			MaxRetries: 5,
			Rules:      []DecomposeLintRule{{ID: "a.b", Severity: "warn", Message: "m"}},
		},
		TaskTemplates: TaskTemplatesConfig{
			Enabled:   true,
			Templates: []TaskTemplateDef{{ID: "tpl-1", Spec: "implement {{feature}} across {{files_list}}"}},
		},
	}
	_, err := json.Marshal(prev)
	if err != nil {
		t.Fatalf("marshal previous config: %v", err)
	}

	// Clear to explicit zero/empty values over the previous set — must stick.
	orig := AutonomousConfig{
		DecomposeLint: DecomposeLintConfig{Mode: "off", MaxRetries: 0},
		TaskTemplates: TaskTemplatesConfig{Enabled: false},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dst := prev
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := dst.DecomposeLint.Mode; got != "off" {
		t.Fatalf("mode after round-trip = %q, want \"off\" (omitempty would have elided this)", got)
	}
	if got := dst.DecomposeLint.MaxRetries; got != 0 {
		t.Fatalf("max_retries after round-trip = %d, want 0", got)
	}
	if len(dst.DecomposeLint.Rules) != 0 {
		t.Fatalf("rules after round-trip = %v, want none", dst.DecomposeLint.Rules)
	}
	if dst.TaskTemplates.Enabled {
		t.Fatal("task_templates.enabled after round-trip = true, want false")
	}
	if len(dst.TaskTemplates.Templates) != 0 {
		t.Fatalf("templates after round-trip = %v, want none", dst.TaskTemplates.Templates)
	}
	if mode, err := dst.EffectiveDecomposeLintMode(); err != nil || mode != "off" {
		t.Fatalf("effective mode after round-trip = %q (err %v), want off", mode, err)
	}
	if n := dst.EffectiveDecomposeLintMaxRetries(); n != 0 {
		t.Fatalf("effective max_retries after round-trip = %d, want 0", n)
	}
}

// TestDecomposeLintSaveLoadRoundTrip: full Save -> Load over a YAML
// file in a temp dir. An explicit 0 retry budget, an explicit off mode
// and an explicit false enabled all survive, and absent keys resolve
// to the shipped defaults.
func TestDecomposeLintSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "DecomposeLint")
	path := filepath.Join(dir, "deep", "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.DecomposeLint = DecomposeLintConfig{
		Mode:       "off",
		MaxRetries: 0,
		Rules: []DecomposeLintRule{
			{
				ID:       "require.verification",
				Severity: "block",
				Match: DecomposeLintMatch{
					SpecRegex:  `^\s*implement`,
					TitleRegex: `(?:test|docs).*(?:test|docs)`,
					FilesRegex: `\.(go|md)$`,
					ReadOnly:   boolPtr(false),
				},
				Message: "task must declare a verification script",
			},
			{
				ID:       "template.readonly",
				Severity: "warn",
				Match:    DecomposeLintMatch{AllOf: []DecomposeLintMatch{{ReadOnly: boolPtr(true)}}},
				Message:  "read-only tasks must not list source files",
			},
		},
	}
	cfg.Autonomous.TaskTemplates = TaskTemplatesConfig{
		Enabled: false,
		Templates: []TaskTemplateDef{
			{
				ID:          "docs.update",
				Title:       "Update docs for {{feature}}",
				Spec:        "Update the docs covering {{feature}}.\n\nAreas:\n{{files_list}}",
				Files:       "docs/a.md, docs/b.md",
				Kind:        "docs",
				ReadOnly:    true,
				DependsOn:   []string{"impl.feature"},
				AppliesWhen: "spec mentions documentation",
			},
			{ID: "impl.feature", Spec: "Implement {{feature}} across {{files_list}}", Files: "internal/", Kind: "code"},
		},
		Projects: map[string]TaskTemplateProjectOverride{
			"project-a": {Enabled: boolPtr(true)},
			"alias-b":   {Enabled: nil},
		},
	}
	cfg.Autonomous.DecomposeGuidance = "prefer small, verification-backed tasks"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dl := back.Autonomous.DecomposeLint
	if dl.Mode != "off" {
		t.Fatalf("mode after Save/Load = %q, want off", dl.Mode)
	}
	if dl.MaxRetries != 0 {
		t.Fatalf("max_retries after Save/Load = %d, want 0 (an explicit 0 must round-trip)", dl.MaxRetries)
	}
	if len(dl.Rules) != 2 {
		t.Fatalf("rules after Save/Load = %d entries, want 2", len(dl.Rules))
	}
	if dl.Rules[0].ID != "require.verification" || dl.Rules[0].Severity != "block" || dl.Rules[0].Message != "task must declare a verification script" {
		t.Fatalf("rule[0] after Save/Load = %+v", dl.Rules[0])
	}
	if dl.Rules[0].Match.ReadOnly == nil || *dl.Rules[0].Match.ReadOnly != false {
		t.Fatalf("rule[0].match.read_only after Save/Load = %v, want explicit false", dl.Rules[0].Match.ReadOnly)
	}
	if got := dl.Rules[1].Match.AllOf[0].ReadOnly; got == nil || *got != true {
		t.Fatalf("rule[1].match.all_of[0].read_only after Save/Load = %v, want true", got)
	}
	if dl.Rules[0].Match.SpecRegex != `^\s*implement` || dl.Rules[0].Match.FilesRegex != `\.(go|md)$` {
		t.Fatalf("rule[0] regexes after Save/Load = %+v", dl.Rules[0].Match)
	}
	tt := back.Autonomous.TaskTemplates
	if tt.Enabled {
		t.Fatal("task_templates.enabled after Save/Load = true, want explicit false")
	}
	// docs.update (index 0) is the one that depends on impl.feature (index 1).
	if len(tt.Templates) != 2 || tt.Templates[0].ID != "docs.update" ||
		tt.Templates[1].ID != "impl.feature" ||
		tt.Templates[0].DependsOn[0] != "impl.feature" {
		t.Fatalf("templates after Save/Load = %+v", tt.Templates)
	}
	if got, ok := tt.Projects["project-a"]; !ok || got.Enabled == nil || *got.Enabled != true {
		t.Fatalf("projects[project-a] after Save/Load = %+v", tt.Projects["project-a"])
	}
	if got, ok := tt.Projects["alias-b"]; !ok {
		t.Fatal("projects[alias-b] missing after Save/Load")
	} else if got.Enabled != nil {
		t.Fatalf("projects[alias-b].enabled after Save/Load = %v, want nil (follow global)", *got.Enabled)
	}
	if back.Autonomous.DecomposeGuidance != "prefer small, verification-backed tasks" {
		t.Fatalf("decompose_guidance after Save/Load = %q", back.Autonomous.DecomposeGuidance)
	}
	if back.Autonomous.EffectiveTaskTemplatesEnabled() {
		t.Fatal("EffectiveTaskTemplatesEnabled after Save/Load = true, want false")
	}
	if !back.Autonomous.EffectiveTaskTemplatesEnabledForProject("project-a") {
		t.Fatal("EffectiveTaskTemplatesEnabledForProject(project-a) = false, want true (override wins)")
	}
	if back.Autonomous.EffectiveTaskTemplatesEnabledForProject("alias-b") {
		t.Fatal("EffectiveTaskTemplatesEnabledForProject(alias-b) = true, want false (nil override follows global off)")
	}
	if back.Autonomous.EffectiveTaskTemplatesEnabledForProject("unknown") {
		t.Fatal("EffectiveTaskTemplatesEnabledForProject(unknown) = true, want false (global)")
	}
}

// TestDecomposeLintValidateBadMode: an unknown mode is a config error,
// both at the sub-config level and through Load (refusal).
func TestDecomposeLintValidateBadMode(t *testing.T) {
	if err := (DecomposeLintConfig{Mode: "enforc"}).Validate(); err == nil {
		t.Fatal("Validate with mode \"enforc\" must error")
	}
	dir := filepath.Join(t.TempDir(), "DecomposeLint")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  decompose_lint:\n    mode: strict\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load with decompose_lint.mode=strict must refuse the config")
	}
}

func TestDecomposeLintValidateBadRuleID(t *testing.T) {
	cases := map[string]DecomposeLintRule{
		"empty": {ID: "", Severity: "warn", Message: "m"},
		"blank": {ID: "   ", Severity: "warn", Message: "m"},
		"upper": {ID: "Require.Verification", Severity: "warn", Message: "m"},
		"space": {ID: "require verification", Severity: "warn", Message: "m"},
		"star":  {ID: "require*verification", Severity: "warn", Message: "m"},
	}
	for name, r := range cases {
		if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{r}}).Validate(); err == nil {
			t.Errorf("rule id case %q: Validate must error", name)
		}
	}
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "a1_.-z", Severity: "warn", Message: "m"}}}).Validate(); err != nil {
		t.Errorf("lowercase [a-z0-9_.-] id must validate: %v", err)
	}
}

func TestDecomposeLintValidateDuplicateRuleIDs(t *testing.T) {
	rule := DecomposeLintRule{ID: "dup.id", Severity: "warn", Message: "m"}
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{rule, rule}}).Validate(); err == nil {
		t.Fatal("Validate with duplicate rule ids must error")
	}
	distinct := DecomposeLintConfig{Rules: []DecomposeLintRule{
		{ID: "a", Severity: "warn", Message: "m"},
		{ID: "b", Severity: "warn", Message: "n"},
	}}
	if err := distinct.Validate(); err != nil {
		t.Fatalf("distinct rule ids must validate: %v", err)
	}
}

func TestDecomposeLintValidateUnknownSeverity(t *testing.T) {
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "r1", Severity: "error", Message: "m"}}}).Validate(); err == nil {
		t.Fatal("Validate with severity \"error\" must error")
	}
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "r1", Severity: "BLOCK", Message: "m"}}}).Validate(); err != nil {
		t.Fatalf("severity is case-insensitive, BLOCK must validate: %v", err)
	}
}

func TestDecomposeLintValidateEmptyMessage(t *testing.T) {
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "r1", Severity: "warn", Message: ""}}}).Validate(); err == nil {
		t.Fatal("Validate with empty message must error")
	}
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "r1", Severity: "warn", Message: "   "}}}).Validate(); err == nil {
		t.Fatal("Validate with blank message must error")
	}
}

func TestDecomposeLintValidateBadRegex(t *testing.T) {
	cases := []struct {
		name string
		m    DecomposeLintMatch
	}{
		{"spec_regex", DecomposeLintMatch{SpecRegex: "("}},
		{"title_regex", DecomposeLintMatch{TitleRegex: `[unterminated`}},
		{"files_regex", DecomposeLintMatch{FilesRegex: `**`}},
		{"any_of[0]", DecomposeLintMatch{AnyOf: []DecomposeLintMatch{{TitleRegex: `(`}}}},
		{"all_of[1]", DecomposeLintMatch{AllOf: []DecomposeLintMatch{{SpecRegex: `.`}, {FilesRegex: `**`}}}},
	}
	for _, c := range cases {
		rule := DecomposeLintRule{ID: "r1", Severity: "warn", Message: "m", Match: c.m}
		if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{rule}}).Validate(); err == nil {
			t.Errorf("case %q: Validate with a bad regex must error", c.name)
		}
	}
	good := DecomposeLintRule{
		ID:       "r1",
		Severity: "block",
		Message:  "m",
		Match:    DecomposeLintMatch{SpecRegex: `^implement \{\{feature\}\}`, AnyOf: []DecomposeLintMatch{{FilesRegex: `\.(go|md)$`}}},
	}
	if err := (DecomposeLintConfig{Rules: []DecomposeLintRule{good}}).Validate(); err != nil {
		t.Fatalf("valid regexes must validate: %v", err)
	}
}

func TestTaskTemplatesValidateDuplicateIDs(t *testing.T) {
	dup := TaskTemplatesConfig{Enabled: true, Templates: []TaskTemplateDef{{ID: "same"}, {ID: "same"}}}
	if err := dup.Validate(); err == nil {
		t.Fatal("Validate with duplicate template ids must error")
	}
	distinct := TaskTemplatesConfig{Enabled: true, Templates: []TaskTemplateDef{{ID: "a"}, {ID: "b"}}}
	if err := distinct.Validate(); err != nil {
		t.Fatalf("distinct template ids must validate: %v", err)
	}
	empty := TaskTemplatesConfig{Enabled: false, Templates: []TaskTemplateDef{{ID: ""}}}
	if err := empty.Validate(); err == nil {
		t.Fatal("template with empty id must not validate")
	}
}

func TestTaskTemplatesValidateRejectedAtLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "TaskTemplates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  task_templates:\n    templates:\n      - id: same\n      - id: same\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load with duplicate template ids must refuse the config")
	}
}

func TestTaskTemplatesProjectOverrideResolution(t *testing.T) {
	off := false
	cfg := AutonomousConfig{
		TaskTemplates: TaskTemplatesConfig{
			Enabled: false,
			Projects: map[string]TaskTemplateProjectOverride{
				"p": {Enabled: boolPtr(true)},
			},
		},
	}
	if cfg.EffectiveTaskTemplatesEnabledForProject("p") != true {
		t.Fatal("project override should win over global off")
	}
	if cfg.EffectiveTaskTemplatesEnabledForProject("other") != false {
		t.Fatal("projects without override follow the global flag")
	}
	cfg.TaskTemplates.Projects["p"] = TaskTemplateProjectOverride{Enabled: nil}
	if cfg.EffectiveTaskTemplatesEnabledForProject("p") != false {
		t.Fatal("nil override.enabled must fall through to the global flag")
	}
	_ = off
}

func TestRenderTaskTemplateSpec(t *testing.T) {
	s := TaskTemplateSlots{Feature: "widget", Files: "src/widget.go, main.go", FilesList: "- src/widget.go\n- main.go"}
	got := RenderTaskTemplateSpec(
		"Implement {{feature}} across {{files}}.\nAreas:\n{{files_list}}\nSee {{unknown}} for details.", s)
	want := "Implement widget across src/widget.go, main.go.\nAreas:\n- src/widget.go\n- main.go\nSee {{unknown}} for details."
	if got != want {
		t.Fatalf("RenderTaskTemplateSpec = %q, want %q", got, want)
	}
	if got := RenderTaskTemplateSpec("", s); got != "" {
		t.Fatalf("RenderTaskTemplateSpec(\"\") = %q, want empty", got)
	}
}

func TestGenerateDecomposeGuidance(t *testing.T) {
	if got := GenerateDecomposeGuidance(nil); got != "" {
		t.Fatalf("GenerateDecomposeGuidance(no rules) = %q, want empty", got)
	}
	rules := []DecomposeLintRule{
		{ID: "a", Severity: "block", Message: "first"},
		{ID: "b", Severity: "", Message: "second"}, // empty severity -> warn in the rendering
	}
	got := GenerateDecomposeGuidance(rules)
	want := "a (block): first\nb (warn): second"
	if got != want {
		t.Fatalf("GenerateDecomposeGuidance = %q, want %q", got, want)
	}
}

func TestEffectiveDecomposeGuidance(t *testing.T) {
	// Unset guidance -> generated from the rules.
	cfg := AutonomousConfig{DecomposeLint: DecomposeLintConfig{Rules: []DecomposeLintRule{{ID: "r", Severity: "warn", Message: "m"}}}}
	if got := cfg.EffectiveDecomposeGuidance(); got != "r (warn): m" {
		t.Fatalf("EffectiveDecomposeGuidance (unset) = %q, want generated text", got)
	}
	// Explicit literal text or path wins verbatim.
	cfg.DecomposeGuidance = "/etc/datawatch/decompose-guidance.md"
	if got := cfg.EffectiveDecomposeGuidance(); got != "/etc/datawatch/decompose-guidance.md" {
		t.Fatalf("EffectiveDecomposeGuidance (set) = %q, want the configured text", got)
	}
	cfg.DecomposeGuidance = "    "
	if got := cfg.EffectiveDecomposeGuidance(); got != "r (warn): m" {
		t.Fatalf("EffectiveDecomposeGuidance (blank) = %q, want generated text", got)
	}
}
