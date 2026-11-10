package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func intp(n int) *int { return &n }

// TestMergePipelineConflictMode covers conflict_mode: its default, a project
// override, and invalid values naming the key.
func TestMergePipelineConflictMode(t *testing.T) {
	t.Run("default is shadow", func(t *testing.T) {
		var c MergePipelineConfig
		if got := c.EffectiveConflictMode("p"); got != ConflictModeShadow {
			t.Fatalf("default mode = %q, want %q", got, ConflictModeShadow)
		}
	})
	t.Run("global on is read", func(t *testing.T) {
		c := MergePipelineConfig{ConflictMode: ConflictModeOn}
		if got := c.EffectiveConflictMode("p"); got != ConflictModeOn {
			t.Fatalf("global mode = %q, want on", got)
		}
	})
	t.Run("project override wins over global", func(t *testing.T) {
		c := MergePipelineConfig{
			ConflictMode: ConflictModeShadow,
			Projects:     map[string]MergePipelineOverride{"p": {ConflictMode: ConflictModeOn}},
		}
		if got := c.EffectiveConflictMode("p"); got != ConflictModeOn {
			t.Fatalf("project mode = %q, want on", got)
		}
		if got := c.EffectiveConflictMode("other"); got != ConflictModeShadow {
			t.Fatalf("other project mode = %q, want shadow", got)
		}
	})
	t.Run("valid values validate", func(t *testing.T) {
		for _, v := range []string{"", ConflictModeShadow, ConflictModeOn} {
			c := MergePipelineConfig{ConflictMode: v}
			if err := c.Validate(); err != nil {
				t.Errorf("mode %q: unexpected error %v", v, err)
			}
		}
	})
	t.Run("invalid global value names the key", func(t *testing.T) {
		c := MergePipelineConfig{ConflictMode: "maybe"}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.conflict_mode") {
			t.Fatalf("err = %v, want one naming autonomous.merge_pipeline.conflict_mode", err)
		}
	})
	t.Run("invalid project value names the key", func(t *testing.T) {
		c := MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {ConflictMode: "Shadow"}}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.projects.p.conflict_mode") {
			t.Fatalf("err = %v, want one naming the project conflict_mode key", err)
		}
	})
	t.Run("unset block is zero and clone keeps the value", func(t *testing.T) {
		var zero MergePipelineConfig
		if !zero.IsZero() {
			t.Fatal("empty config should be zero")
		}
		c := MergePipelineConfig{ConflictMode: ConflictModeOn}
		if c.IsZero() {
			t.Fatal("config with conflict_mode should not be zero")
		}
		if got := c.Clone().ConflictMode; got != ConflictModeOn {
			t.Fatalf("clone mode = %q", got)
		}
	})
}

// TestMergePipelineConfirmFirstN covers conflict_confirm_first_n: its default
// of 5, a project override, an explicit 0, and negative values naming the key.
func TestMergePipelineConfirmFirstN(t *testing.T) {
	t.Run("unset reads 5", func(t *testing.T) {
		var c MergePipelineConfig
		if got := c.EffectiveConflictConfirmFirstN("p"); got != 5 {
			t.Fatalf("default = %d, want 5", got)
		}
	})
	t.Run("global value is read", func(t *testing.T) {
		c := MergePipelineConfig{ConflictConfirmFirstN: intp(9)}
		if got := c.EffectiveConflictConfirmFirstN("p"); got != 9 {
			t.Fatalf("global = %d, want 9", got)
		}
	})
	t.Run("project override wins over global", func(t *testing.T) {
		c := MergePipelineConfig{
			ConflictConfirmFirstN: intp(9),
			Projects:              map[string]MergePipelineOverride{"p": {ConflictConfirmFirstN: intp(2)}},
		}
		if got := c.EffectiveConflictConfirmFirstN("p"); got != 2 {
			t.Fatalf("project = %d, want 2", got)
		}
		if got := c.EffectiveConflictConfirmFirstN("other"); got != 9 {
			t.Fatalf("other project = %d, want global 9", got)
		}
	})
	t.Run("explicit zero turns confirmation off", func(t *testing.T) {
		c := MergePipelineConfig{ConflictConfirmFirstN: intp(0)}
		if got := c.EffectiveConflictConfirmFirstN("p"); got != 0 {
			t.Fatalf("global 0 read as %d", got)
		}
		c = MergePipelineConfig{
			ConflictConfirmFirstN: intp(9),
			Projects:              map[string]MergePipelineOverride{"p": {ConflictConfirmFirstN: intp(0)}},
		}
		if got := c.EffectiveConflictConfirmFirstN("p"); got != 0 {
			t.Fatalf("project 0 read as %d, want 0 (not inherited)", got)
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("0 should be valid: %v", err)
		}
	})
	t.Run("negative global is invalid and names the key", func(t *testing.T) {
		c := MergePipelineConfig{ConflictConfirmFirstN: intp(-1)}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.conflict_confirm_first_n") {
			t.Fatalf("err = %v, want one naming conflict_confirm_first_n", err)
		}
	})
	t.Run("negative project is invalid and names the key", func(t *testing.T) {
		c := MergePipelineConfig{Projects: map[string]MergePipelineOverride{"p": {ConflictConfirmFirstN: intp(-3)}}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.projects.p.conflict_confirm_first_n") {
			t.Fatalf("err = %v, want one naming the project key", err)
		}
	})
	t.Run("clone copies the pointer", func(t *testing.T) {
		c := MergePipelineConfig{ConflictConfirmFirstN: intp(4)}
		out := c.Clone()
		*out.ConflictConfirmFirstN = 7
		if *c.ConflictConfirmFirstN != 4 {
			t.Fatal("clone shares the pointer with the original")
		}
	})
	t.Run("yaml round trip keeps explicit zero", func(t *testing.T) {
		var c MergePipelineConfig
		if err := yaml.Unmarshal([]byte("conflict_confirm_first_n: 0\nconflict_mode: on\n"), &c); err != nil {
			t.Fatal(err)
		}
		if c.ConflictConfirmFirstN == nil || *c.ConflictConfirmFirstN != 0 {
			t.Fatalf("explicit 0 lost in yaml: %v", c.ConflictConfirmFirstN)
		}
		if c.IsZero() {
			t.Fatal("config with explicit 0 should not be zero")
		}
	})
}
