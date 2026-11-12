package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// loadScriptGateYAML writes body to a temp config file and loads it through
// the real Load path, so validation runs exactly as it does at startup.
func loadScriptGateYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return Load(path)
}

func TestScriptGateTimeoutSettingDefault(t *testing.T) {
	if got := DefaultConfig().Autonomous.ScriptGateTimeoutSeconds; got != DefaultScriptGateTimeoutSeconds {
		t.Fatalf("default seconds = %d, want %d", got, DefaultScriptGateTimeoutSeconds)
	}
	if DefaultScriptGateTimeoutSeconds != 900 {
		t.Fatalf("DefaultScriptGateTimeoutSeconds = %d, want 900", DefaultScriptGateTimeoutSeconds)
	}
	if got := DefaultConfig().Autonomous.EffectiveScriptGateTimeout(); got != 900*time.Second {
		t.Fatalf("default timeout = %v, want 15m0s", got)
	}
	// A file that omits the key keeps the shipped default.
	cfg, err := loadScriptGateYAML(t, "autonomous:\n  enabled: false\n")
	if err != nil {
		t.Fatalf("Load(no key) = %v, want nil", err)
	}
	if got := cfg.Autonomous.ScriptGateTimeoutSeconds; got != 900 {
		t.Fatalf("loaded default = %d, want 900", got)
	}
}

func TestScriptGateTimeoutSettingZero(t *testing.T) {
	a := AutonomousConfig{ScriptGateTimeoutSeconds: 0}
	if got := a.EffectiveScriptGateTimeout(); got != 0 {
		t.Fatalf("zero seconds timeout = %v, want 0 (no cap)", got)
	}
	if err := validateScriptGateTimeout(0); err != nil {
		t.Fatalf("validateScriptGateTimeout(0) = %v, want nil", err)
	}
	cfg, err := loadScriptGateYAML(t, "autonomous:\n  script_gate_timeout_seconds: 0\n")
	if err != nil {
		t.Fatalf("Load(0) = %v, want nil", err)
	}
	if got := cfg.Autonomous.EffectiveScriptGateTimeout(); got != 0 {
		t.Fatalf("loaded zero timeout = %v, want 0", got)
	}
}

func TestScriptGateTimeoutSettingPositive(t *testing.T) {
	a := AutonomousConfig{ScriptGateTimeoutSeconds: 120}
	if got := a.EffectiveScriptGateTimeout(); got != 120*time.Second {
		t.Fatalf("timeout = %v, want 2m0s", got)
	}
	cfg, err := loadScriptGateYAML(t, "autonomous:\n  script_gate_timeout_seconds: 120\n")
	if err != nil {
		t.Fatalf("Load(120) = %v, want nil", err)
	}
	if got := cfg.Autonomous.EffectiveScriptGateTimeout(); got != 120*time.Second {
		t.Fatalf("loaded timeout = %v, want 2m0s", got)
	}
}

func TestScriptGateTimeoutSettingNegativeRefused(t *testing.T) {
	_, err := loadScriptGateYAML(t, "autonomous:\n  script_gate_timeout_seconds: -1\n")
	if err == nil {
		t.Fatal("Load(-1) = nil, want refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "autonomous.script_gate_timeout_seconds") {
		t.Errorf("error %q does not name the key", msg)
	}
	if !strings.Contains(msg, "0 means no cap") {
		t.Errorf("error %q does not say 0 means no cap", msg)
	}
	if err := validateScriptGateTimeout(-5); err == nil {
		t.Fatal("validateScriptGateTimeout(-5) = nil, want error")
	}
}

func TestScriptGateTimeoutSettingYAMLRoundTrip(t *testing.T) {
	for _, want := range []int{0, 45, DefaultScriptGateTimeoutSeconds} {
		in := AutonomousConfig{ScriptGateTimeoutSeconds: want}
		b, err := yaml.Marshal(in)
		if err != nil {
			t.Fatalf("yaml marshal: %v", err)
		}
		if !strings.Contains(string(b), "script_gate_timeout_seconds") {
			t.Fatalf("yaml output missing key for %d: %s", want, b)
		}
		var out AutonomousConfig
		if err := yaml.Unmarshal(b, &out); err != nil {
			t.Fatalf("yaml unmarshal: %v", err)
		}
		if out.ScriptGateTimeoutSeconds != want {
			t.Fatalf("yaml round trip = %d, want %d", out.ScriptGateTimeoutSeconds, want)
		}
	}
}

func TestScriptGateTimeoutSettingJSONRoundTrip(t *testing.T) {
	for _, want := range []int{0, 300} {
		in := AutonomousConfig{ScriptGateTimeoutSeconds: want}
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("json marshal: %v", err)
		}
		if !strings.Contains(string(b), `"script_gate_timeout_seconds":`) {
			t.Fatalf("json output missing key for %d: %s", want, b)
		}
		var out AutonomousConfig
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if out.ScriptGateTimeoutSeconds != want {
			t.Fatalf("json round trip = %d, want %d", out.ScriptGateTimeoutSeconds, want)
		}
	}
}
