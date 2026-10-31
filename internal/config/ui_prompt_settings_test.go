// session.dismiss_ui_prompts and the three session.ui_prompt_* keys:
// defaults when unset, effective helpers, validators, load-time validation
// and YAML/JSON round trip.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUIPromptSettings_DefaultWhenAbsent(t *testing.T) {
	for name, c := range map[string]SessionConfig{
		"zero value":     {},
		"default config": DefaultConfig().Session,
	} {
		t.Run(name, func(t *testing.T) {
			if got := c.EffectiveDismissUIPrompts(); !got {
				t.Errorf("%s: dismiss_ui_prompts = %v, want true (default on)", name, got)
			}
			if got := c.EffectiveUIPromptDismissDelaySeconds(); got != 5 {
				t.Errorf("%s: ui_prompt_dismiss_delay_seconds = %d, want 5", name, got)
			}
			if got := c.EffectiveUIPromptClearBoundSeconds(); got != 30 {
				t.Errorf("%s: ui_prompt_clear_bound_seconds = %d, want 30", name, got)
			}
			if got := c.EffectiveUIPromptMaxAttempts(); got != 2 {
				t.Errorf("%s: ui_prompt_max_attempts = %d, want 2", name, got)
			}
			if c.DismissUIPrompts != nil || c.UIPromptDismissDelaySeconds != nil ||
				c.UIPromptClearBoundSeconds != nil || c.UIPromptMaxAttempts != nil {
				t.Errorf("%s: stored values must be absent (nil), got %+v", name, c)
			}
		})
	}
	if DefaultUIPromptDismissDelaySeconds != 5 || DefaultUIPromptClearBoundSeconds != 30 || DefaultUIPromptMaxAttempts != 2 {
		t.Errorf("constants = delay %d, bound %d, attempts %d; want 5, 30, 2",
			DefaultUIPromptDismissDelaySeconds, DefaultUIPromptClearBoundSeconds, DefaultUIPromptMaxAttempts)
	}
}

func TestUIPromptSettings_EffectiveReadsExplicit(t *testing.T) {
	b := false
	d45, b60, a50 := 45, 60, 50
	in := &SessionConfig{
		DismissUIPrompts:            &b,
		UIPromptDismissDelaySeconds: &d45,
		UIPromptClearBoundSeconds:   &b60,
		UIPromptMaxAttempts:         &a50,
	}
	if got := in.EffectiveDismissUIPrompts(); got {
		t.Errorf("explicit dismiss_ui_prompts=false read back as true")
	}
	if got := in.EffectiveUIPromptDismissDelaySeconds(); got != 45 {
		t.Errorf("explicit delay = %d, want 45", got)
	}
	if got := in.EffectiveUIPromptClearBoundSeconds(); got != 60 {
		t.Errorf("explicit bound = %d, want 60", got)
	}
	if got := in.EffectiveUIPromptMaxAttempts(); got != 50 {
		t.Errorf("explicit attempts = %d, want 50", got)
	}
}

func TestUIPromptSettings_Validator(t *testing.T) {
	for _, n := range []int{-1, -100} {
		err := ValidateUIPromptDismissDelaySeconds(n)
		if err == nil {
			t.Errorf("delay %d must be refused", n)
			continue
		}
		for _, frag := range []string{"session.ui_prompt_dismiss_delay_seconds", ">= 0", "default 5"} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("delay %d: message %q lacks %q", n, err, frag)
			}
		}
	}
	for _, n := range []int{0, 5, 3600} {
		if err := ValidateUIPromptDismissDelaySeconds(n); err != nil {
			t.Errorf("delay %d must be accepted (0 = send at once), got %v", n, err)
		}
	}
	for _, n := range []int{0, -1, -100} {
		err := ValidateUIPromptClearBoundSeconds(n)
		if err == nil {
			t.Errorf("bound %d must be refused", n)
			continue
		}
		for _, frag := range []string{"session.ui_prompt_clear_bound_seconds", ">= 1", "default 30"} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("bound %d: message %q lacks %q", n, err, frag)
			}
		}
	}
	for _, n := range []int{1, 30, 3600} {
		if err := ValidateUIPromptClearBoundSeconds(n); err != nil {
			t.Errorf("bound %d must be accepted, got %v", n, err)
		}
	}
	for _, n := range []int{0, -1, -100} {
		err := ValidateUIPromptMaxAttempts(n)
		if err == nil {
			t.Errorf("attempts %d must be refused", n)
			continue
		}
		for _, frag := range []string{"session.ui_prompt_max_attempts", ">= 1", "default 2"} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("attempts %d: message %q lacks %q", n, err, frag)
			}
		}
	}
	for _, n := range []int{1, 2, 100} {
		if err := ValidateUIPromptMaxAttempts(n); err != nil {
			t.Errorf("attempts %d must be accepted, got %v", n, err)
		}
	}
}

func TestUIPromptSettings_RoundTrip(t *testing.T) {
	b := false
	d45, b60, a50 := 45, 60, 50
	in := &SessionConfig{
		DismissUIPrompts:            &b,
		UIPromptDismissDelaySeconds: &d45,
		UIPromptClearBoundSeconds:   &b60,
		UIPromptMaxAttempts:         &a50,
	}
	y, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML SessionConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatalf("yaml round trip: %v\n%s", err, y)
	}
	if fromYAML.EffectiveDismissUIPrompts() {
		t.Errorf("yaml round trip dismiss: want false, got true")
	}
	if fromYAML.EffectiveUIPromptDismissDelaySeconds() != 45 || fromYAML.EffectiveUIPromptClearBoundSeconds() != 60 || fromYAML.EffectiveUIPromptMaxAttempts() != 50 {
		t.Errorf("yaml round trip: %+v", fromYAML)
	}
	// An absent key stays absent.
	var empty SessionConfig
	if err := yaml.Unmarshal([]byte("{}"), &empty); err != nil {
		t.Fatalf("unmarshal {}: %v", err)
	}
	if empty.DismissUIPrompts != nil || empty.UIPromptDismissDelaySeconds != nil ||
		empty.UIPromptClearBoundSeconds != nil || empty.UIPromptMaxAttempts != nil {
		t.Errorf("absent keys must stay nil: %+v", empty)
	}
}

func TestUIPromptSettings_LoadFromFile(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("session:\n" +
		"  dismiss_ui_prompts: false\n" +
		"  ui_prompt_dismiss_delay_seconds: 0\n" +
		"  ui_prompt_clear_bound_seconds: 60\n" +
		"  ui_prompt_max_attempts: 4\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Session.EffectiveDismissUIPrompts() {
		t.Errorf("loaded dismiss_ui_prompts = true, want false")
	}
	if got := cfg.Session.EffectiveUIPromptDismissDelaySeconds(); got != 0 {
		t.Errorf("loaded delay = %d, want 0", got)
	}
	if got := cfg.Session.EffectiveUIPromptClearBoundSeconds(); got != 60 {
		t.Errorf("loaded bound = %d, want 60", got)
	}
	if got := cfg.Session.EffectiveUIPromptMaxAttempts(); got != 4 {
		t.Errorf("loaded attempts = %d, want 4", got)
	}
	for _, bad := range []struct {
		key, value string
	}{
		{"ui_prompt_dismiss_delay_seconds", "-1"},
		{"ui_prompt_clear_bound_seconds", "0"},
		{"ui_prompt_max_attempts", "-5"},
	} {
		_, err := Load(write("session:\n  " + bad.key + ": " + bad.value + "\n"))
		if err == nil || !strings.Contains(err.Error(), "session."+bad.key) {
			t.Errorf("Load with %s: want a refusal naming the key, got %v", bad.key, err)
		}
	}
	// dismiss_ui_prompts is a boolean; a number is refused at unmarshal.
	if _, err := Load(write("session:\n  dismiss_ui_prompts: 3\n")); err == nil {
		t.Errorf("Load with dismiss_ui_prompts: 3: want an error, got none")
	}
}
