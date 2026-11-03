package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLivenessWindowSecondsDefault(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.LivenessWindowSecondsEffective(); got != 120 {
		t.Errorf("LivenessWindowSecondsEffective() = %d, want 120 (DefaultConfig)", got)
	}
	// A bare/empty config must resolve the same way.
	if got := (&Config{}).LivenessWindowSecondsEffective(); got != 120 {
		t.Errorf("empty config LivenessWindowSecondsEffective() = %d, want 120", got)
	}
}

func TestLivenessWindowSecondsZeroMeansDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.LivenessWindowSeconds = 0
	if got := cfg.LivenessWindowSecondsEffective(); got != DefaultLivenessWindowSeconds {
		t.Errorf("0 window: got %d, want %d (0 means default)", got, DefaultLivenessWindowSeconds)
	}
	cfg.Session.LivenessWindowSeconds = -5
	if got := cfg.LivenessWindowSecondsEffective(); got != DefaultLivenessWindowSeconds {
		t.Errorf("negative window: got %d, want %d (negative treated like 0)", got, DefaultLivenessWindowSeconds)
	}
}

func TestLivenessWindowSecondsExplicit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.LivenessWindowSeconds = 90
	if got := cfg.LivenessWindowSecondsEffective(); got != 90 {
		t.Errorf("explicit window: got %d, want 90", got)
	}
}

func TestLivenessInputIdleTimeoutPrintModeDefaultOff(t *testing.T) {
	cfg := DefaultConfig()
	// Global interactive timeout is on (10)...
	if got := cfg.Session.InputIdleTimeout; got != 10 {
		t.Fatalf("sanity: InputIdleTimeout = %d, want 10", got)
	}
	// ...but every un-overridden print-mode backend defaults to off (0).
	for _, backend := range []string{"pi-prompt", "goose-prompt", "opencode-prompt", "claude-code-prompt"} {
		if got := cfg.LivenessInputIdleTimeoutEffective(backend); got != 0 {
			t.Errorf("%s: got %d, want 0 (print-mode default off)", backend, got)
		}
	}
	// Suffix matching is case-insensitive and trims, like IsPromptBackend.
	if got := cfg.LivenessInputIdleTimeoutEffective("  PI-PROMPT "); got != 0 {
		t.Errorf("trim/case: got %d, want 0", got)
	}
}

func TestLivenessInputIdleTimeoutInteractiveKeepsGlobal(t *testing.T) {
	cfg := DefaultConfig()
	for _, backend := range []string{"claude-code", "pi", "goose", "aider", "shell"} {
		if got := cfg.LivenessInputIdleTimeoutEffective(backend); got != 10 {
			t.Errorf("%s: got %d, want 10 (global interactive default)", backend, got)
		}
	}
	// A zero global (unset) still resolves to the built-in 10 for interactive.
	bare := &Config{}
	if got := bare.LivenessInputIdleTimeoutEffective("claude-code"); got != 10 {
		t.Errorf("bare config interactive: got %d, want 10 (0 → default)", got)
	}
	// And an explicit non-default global is honored.
	cfg.Session.InputIdleTimeout = 25
	if got := cfg.LivenessInputIdleTimeoutEffective("pi"); got != 25 {
		t.Errorf("explicit global: got %d, want 25", got)
	}
}

func TestLivenessInputIdleTimeoutOverrideMap(t *testing.T) {
	cfg := DefaultConfig()
	// Empty map = global rule applies (see the two defaults above).
	if got := cfg.LivenessInputIdleTimeoutEffective("pi-prompt"); got != 0 {
		t.Errorf("empty map pi-prompt: got %d, want 0", got)
	}

	cfg.Session.LivenessIdleTimeoutOverrides = map[string]int{"pi": 30}
	if got := cfg.LivenessInputIdleTimeoutEffective("pi-prompt"); got != 30 {
		t.Errorf("family-key override: got %d, want 30", got)
	}
	// The same key also matches when given as the full backend name.
	if got := cfg.LivenessInputIdleTimeoutEffective("pi"); got != 30 {
		t.Errorf("family name as backend: got %d, want 30", got)
	}
	// Other families are untouched.
	if got := cfg.LivenessInputIdleTimeoutEffective("goose-prompt"); got != 0 {
		t.Errorf("unrelated print-mode: got %d, want 0", got)
	}
	if got := cfg.LivenessInputIdleTimeoutEffective("claude-code"); got != 10 {
		t.Errorf("unrelated interactive: got %d, want 10", got)
	}

	// An explicit 0 in the map is a valid override (off-by-choice), not
	// indistinguishable from "missing".
	cfg.Session.LivenessIdleTimeoutOverrides = map[string]int{"pi": 0}
	if got := cfg.LivenessInputIdleTimeoutEffective("pi-prompt"); got != 0 {
		t.Errorf("explicit 0 override: got %d, want 0", got)
	}

	// Full-name keys also work; family keys are preferred and equal here.
	cfg.Session.LivenessIdleTimeoutOverrides = map[string]int{"pi-prompt": 45, "goose": 15}
	if got := cfg.LivenessInputIdleTimeoutEffective("pi-prompt"); got != 45 {
		t.Errorf("full-name key: got %d, want 45", got)
	}
	if got := cfg.LivenessInputIdleTimeoutEffective("goose-prompt"); got != 15 {
		t.Errorf("family-key for goose: got %d, want 15", got)
	}

	// An override can also switch an interactive backend's timeout.
	cfg.Session.LivenessIdleTimeoutOverrides = map[string]int{"claude-code": 0}
	if got := cfg.LivenessInputIdleTimeoutEffective("claude-code"); got != 0 {
		t.Errorf("interactive override to 0: got %d, want 0", got)
	}
}

func TestLivenessValidateSettings(t *testing.T) {
	known := []string{"claude-code", "pi", "pi-prompt", "goose-prompt"}
	cases := []struct {
		name      string
		window    int
		overrides map[string]int
		wantErr   string // substring; "" means valid
	}{
		{"unset", 0, nil, ""},
		{"window ok", 90, nil, ""},
		{"window negative", -1, nil, "liveness_window_seconds"},
		{"family key", 0, map[string]int{"pi": 30}, ""},
		{"full name key", 0, map[string]int{"pi-prompt": 0}, ""},
		{"interactive name", 0, map[string]int{"claude-code": 5}, ""},
		{"goose family via prompt", 0, map[string]int{"goose": 7}, ""},
		{"unknown key", 0, map[string]int{"nope": 5}, `"nope"`},
		{"negative override", 0, map[string]int{"pi": -3}, "pi"},
	}
	for _, tc := range cases {
		err := ValidateLivenessSettings(tc.window, tc.overrides, known)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want substring %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestLivenessYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Session.LivenessWindowSeconds = 75
	cfg.Session.LivenessIdleTimeoutOverrides = map[string]int{"pi": 0, "goose-prompt": 45}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{"liveness_window_seconds: 75", "liveness_idle_timeout_overrides:"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("saved YAML lacks %q:\n%s", want, raw)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.LivenessWindowSeconds != 75 {
		t.Errorf("window = %d, want 75", got.Session.LivenessWindowSeconds)
	}
	if v, ok := got.Session.LivenessIdleTimeoutOverrides["pi"]; !ok || v != 0 {
		t.Errorf("pi override = %d,%v, want explicit 0", v, ok)
	}
	if got.Session.LivenessIdleTimeoutOverrides["goose-prompt"] != 45 {
		t.Errorf("goose-prompt override = %v", got.Session.LivenessIdleTimeoutOverrides)
	}
}
