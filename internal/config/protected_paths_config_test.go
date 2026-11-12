package config

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

var wantDefaultProtected = []string{
	"AGENT.md",
	"docs/plans/2026-10-17-supervisor-rules-and-lessons.md",
	"CODEOWNERS",
	".github/workflows/",
}

// Unset keys read the operator's defaults on a zero config and a loaded one.
func TestProtectedPathsConfigDefaults(t *testing.T) {
	for name, cfg := range map[string]AutonomousAutomationConfig{
		"zero_value":     {},
		"default_loaded": DefaultConfig().Autonomous.Automation,
		"partial_loaded": loadCleanupYAML(t, "autonomous:\n  enabled: true\n  automation:\n    idle_alert_minutes: 5\n").Autonomous.Automation,
	} {
		t.Run(name, func(t *testing.T) {
			if len(cfg.ProtectedPaths) != 0 || cfg.ProtectedPathsAction != "" || cfg.RulesReviewSource != "" {
				t.Errorf("keys must be unset; got %v %q %q", cfg.ProtectedPaths, cfg.ProtectedPathsAction, cfg.RulesReviewSource)
			}
			if got := cfg.EffectiveProtectedPaths(""); !reflect.DeepEqual(got, wantDefaultProtected) {
				t.Errorf("EffectiveProtectedPaths = %v; want %v", got, wantDefaultProtected)
			}
			if got := cfg.EffectiveProtectedPathsAction(); got != ProtectedPathsActionHold {
				t.Errorf("EffectiveProtectedPathsAction = %q; want %q", got, ProtectedPathsActionHold)
			}
			if got := cfg.EffectiveRulesReviewSource(); got != RulesReviewSourceBaseCommit {
				t.Errorf("EffectiveRulesReviewSource = %q; want %q", got, RulesReviewSourceBaseCommit)
			}
		})
	}
}

func TestProtectedPathsConfigExplicitActions(t *testing.T) {
	for _, want := range []string{ProtectedPathsActionHold, ProtectedPathsActionWarn, ProtectedPathsActionBlock, ProtectedPathsActionOff} {
		a := AutonomousAutomationConfig{ProtectedPathsAction: want}
		if got := a.EffectiveProtectedPathsAction(); got != want {
			t.Errorf("action %q: effective = %q", want, got)
		}
	}
}

func TestProtectedPathsConfigUnknownActionReadsDefault(t *testing.T) {
	for _, word := range []string{"enabled", "HOLD", "Block", "true", " warn ", "deny"} {
		a := AutonomousAutomationConfig{ProtectedPathsAction: word}
		if got := a.EffectiveProtectedPathsAction(); got != ProtectedPathsActionHold {
			t.Errorf("action %q: effective = %q; want %q", word, got, ProtectedPathsActionHold)
		}
	}
	cfg := loadCleanupYAML(t, "autonomous:\n  automation:\n    idle_alert_minutes: 30\n    protected_paths_action: sometimes\n")
	if got := cfg.Autonomous.Automation.EffectiveProtectedPathsAction(); got != ProtectedPathsActionHold {
		t.Errorf("loaded unknown action: effective = %q", got)
	}
}

func TestProtectedPathsConfigRulesFileChangesDefault(t *testing.T) {
	a := AutonomousAutomationConfig{}
	got := a.EffectiveProtectedPaths("RULES.md")
	want := append([]string{"RULES.md"}, wantDefaultProtected[1:]...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EffectiveProtectedPaths(RULES.md) = %v; want %v", got, want)
	}
	if got := DefaultProtectedPaths("docs/RULES.md"); got[0] != "docs/RULES.md" {
		t.Errorf("DefaultProtectedPaths(docs/RULES.md)[0] = %q", got[0])
	}
	if got := DefaultProtectedPaths("  "); got[0] != "AGENT.md" {
		t.Errorf("blank rules file: first = %q; want AGENT.md", got[0])
	}
}

func TestProtectedPathsConfigExplicitListReplacesDefault(t *testing.T) {
	a := AutonomousAutomationConfig{ProtectedPaths: []string{"SECURITY.md", "ci/"}}
	want := []string{"SECURITY.md", "ci/"}
	if got := a.EffectiveProtectedPaths("RULES.md"); !reflect.DeepEqual(got, want) {
		t.Errorf("explicit list = %v; want %v", got, want)
	}
	// A list with no usable entry reads the default.
	b := AutonomousAutomationConfig{ProtectedPaths: []string{"", "../x", "  "}}
	if got := b.EffectiveProtectedPaths(""); !reflect.DeepEqual(got, wantDefaultProtected) {
		t.Errorf("unusable list = %v; want the default %v", got, wantDefaultProtected)
	}
}

func TestProtectedPathsConfigNormalisation(t *testing.T) {
	in := []string{
		"./x", "/y", "a//b", "docs/", " ./docs/plans/ ", "docs/plans/", "x",
		"", "   ", "../escape", "a/../b", "a/..", "dir/**", "*.md", "src/[ab]?.go",
		"/", "./", ".", "**",
	}
	want := []string{"x", "y", "a/b", "docs/", "docs/plans/", "dir/", "*.md", "src/[ab]?.go"}
	got := AutonomousAutomationConfig{ProtectedPaths: in}.EffectiveProtectedPaths("")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalised = %v; want %v", got, want)
	}
	// Normalising twice changes nothing.
	if again := NormalizeProtectedPaths(got); !reflect.DeepEqual(again, got) {
		t.Errorf("not idempotent: %v then %v", got, again)
	}
}

func TestRulesReviewSourceConfig(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", RulesReviewSourceBaseCommit},
		{RulesReviewSourceBaseCommit, RulesReviewSourceBaseCommit},
		{RulesReviewSourceWorktree, RulesReviewSourceWorktree},
		{"WORKTREE", RulesReviewSourceBaseCommit},
		{"head", RulesReviewSourceBaseCommit},
		{" worktree ", RulesReviewSourceBaseCommit},
	} {
		a := AutonomousAutomationConfig{RulesReviewSource: tc.in}
		if got := a.EffectiveRulesReviewSource(); got != tc.want {
			t.Errorf("rules_review_source %q: effective = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestProtectedPathsConfigYAMLRoundTrip(t *testing.T) {
	in := AutonomousAutomationConfig{
		ProtectedPaths:       []string{"AGENT.md", "ci/", "*.lock"},
		ProtectedPathsAction: ProtectedPathsActionBlock,
		RulesReviewSource:    RulesReviewSourceWorktree,
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AutonomousAutomationConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in.ProtectedPaths, out.ProtectedPaths) ||
		in.ProtectedPathsAction != out.ProtectedPathsAction ||
		in.RulesReviewSource != out.RulesReviewSource {
		t.Errorf("round trip changed the keys: in %v %q %q, out %v %q %q", in.ProtectedPaths, in.ProtectedPathsAction, in.RulesReviewSource, out.ProtectedPaths, out.ProtectedPathsAction, out.RulesReviewSource)
	}

	cfg := loadCleanupYAML(t, "autonomous:\n  automation:\n    protected_paths:\n      - CODEOWNERS\n      - ci/\n    protected_paths_action: warn\n    rules_review_source: worktree\n")
	a := cfg.Autonomous.Automation
	if got := a.EffectiveProtectedPaths(""); !reflect.DeepEqual(got, []string{"CODEOWNERS", "ci/"}) {
		t.Errorf("loaded list = %v", got)
	}
	if got := a.EffectiveProtectedPathsAction(); got != ProtectedPathsActionWarn {
		t.Errorf("loaded action = %q", got)
	}
	if got := a.EffectiveRulesReviewSource(); got != RulesReviewSourceWorktree {
		t.Errorf("loaded source = %q", got)
	}
}
