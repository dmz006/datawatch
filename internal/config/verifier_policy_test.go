package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVerifierPolicyNormalizeMode(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", VerifierPolicyFixed, false},
		{"  ", VerifierPolicyFixed, false},
		{"fixed", VerifierPolicyFixed, false},
		{"Prefer-Free", VerifierPolicyPreferFree, false},
		{"balance", VerifierPolicyBalance, false},
		{"gpu-only", VerifierPolicyGPUOnly, false},
		{" claude-only ", VerifierPolicyClaudeOnly, false},
		{"balanced", "", true},
		{"prefer_free", "", true},
	}
	for _, tc := range tests {
		got, err := NormalizeVerifierPolicyMode(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("NormalizeVerifierPolicyMode(%q) = (%q, %v), want (%q, err=%v)", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestVerifierPolicyNormalizedDefaults(t *testing.T) {
	got, err := (VerifierPolicyConfig{Backends: []string{" a ", "", "a", "b"}}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	want := VerifierPolicyConfig{
		Mode:                   VerifierPolicyFixed,
		Backends:               []string{"a", "b"},
		ClaudeCapWindowMinutes: VerifierPolicyClaudeCapWindowDefault,
		StrongerBackend:        "claude-code",
		StrongerModel:          "claude-sonnet-5-5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestVerifierPolicyValidate(t *testing.T) {
	for name, p := range map[string]VerifierPolicyConfig{
		"bad mode":      {Mode: "nope"},
		"negative cap":  {ClaudeSessionCap: -1},
		"negative wind": {ClaudeCapWindowMinutes: -5},
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
		if _, err := p.Normalized(); err == nil {
			t.Errorf("%s: Normalized want error", name)
		}
	}
	if err := (VerifierPolicyConfig{}).Validate(); err != nil {
		t.Errorf("zero value must be valid: %v", err)
	}
}

func TestVerifierPolicyValidateLoaded(t *testing.T) {
	cfg := DefaultConfig()
	applyDefaults(cfg)
	if err := validateLoaded(cfg); err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg.Autonomous.VerifierPolicy.Mode = "sideways"
	err := validateLoaded(cfg)
	if err == nil || !strings.Contains(err.Error(), "autonomous.invalid verifier_policy.mode") {
		t.Fatalf("want a verifier_policy.mode error, got %v", err)
	}
}

func TestVerifierPolicyEffectiveBackends(t *testing.T) {
	pool := []string{"ollama-a", "claude-code", "ollama-b"}
	tests := []struct {
		name   string
		p      VerifierPolicyConfig
		pool   []string
		single string
		want   []string
	}{
		{"own list wins", VerifierPolicyConfig{Backends: []string{"x"}}, pool, "s", []string{"x"}},
		{"pool next", VerifierPolicyConfig{}, pool, "s", pool},
		{"single last", VerifierPolicyConfig{}, nil, "s", []string{"s"}},
		{"nothing", VerifierPolicyConfig{}, nil, "", []string{}},
		{"gpu-only drops Claude", VerifierPolicyConfig{Mode: "gpu-only"}, pool, "", []string{"ollama-a", "ollama-b"}},
		{"claude-only keeps Claude", VerifierPolicyConfig{Mode: "claude-only"}, pool, "", []string{"claude-code"}},
		{"fixed keeps all", VerifierPolicyConfig{Mode: "fixed"}, pool, "", pool},
	}
	for _, tc := range tests {
		got := tc.p.EffectiveBackends(tc.pool, tc.single)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVerifierPolicyIsClaudeBackend(t *testing.T) {
	for in, want := range map[string]bool{
		"claude-code": true, "Claude-Haiku": true, " claude ": true,
		"ollama": false, "pi-prompt": false, "": false,
	} {
		if got := IsClaudeVerifierBackend(in); got != want {
			t.Errorf("IsClaudeVerifierBackend(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestVerifierPolicyYAMLAndJSONRoundTrip(t *testing.T) {
	src := `
verifier_policy:
  mode: balance
  backends: [ollama-a, claude-code]
  claude_session_cap: 3
  claude_cap_window_minutes: 30
  force_stronger_on_retry: true
  final_gate_stronger: true
  stronger_backend: claude-code
  stronger_model: claude-sonnet-5-5
`
	var a AutonomousConfig
	if err := yaml.Unmarshal([]byte(src), &a); err != nil {
		t.Fatal(err)
	}
	want := VerifierPolicyConfig{
		Mode: "balance", Backends: []string{"ollama-a", "claude-code"},
		ClaudeSessionCap: 3, ClaudeCapWindowMinutes: 30,
		ForceStrongerOnRetry: true, FinalGateStronger: true,
		StrongerBackend: "claude-code", StrongerModel: "claude-sonnet-5-5",
	}
	if !reflect.DeepEqual(a.VerifierPolicy, want) {
		t.Fatalf("yaml: got %+v, want %+v", a.VerifierPolicy, want)
	}

	// An explicit clear must survive the JSON merge-unmarshal the live config
	// round-trip does: no field may be omitted when zero.
	cleared, err := json.Marshal(AutonomousConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cleared, &a); err != nil {
		t.Fatal(err)
	}
	if a.VerifierPolicy.Mode != "" || len(a.VerifierPolicy.Backends) != 0 || a.VerifierPolicy.StrongerModel != "" {
		t.Fatalf("clear did not reach the policy: %+v", a.VerifierPolicy)
	}
	if a.VerifierPolicy.ClaudeSessionCap != 0 || a.VerifierPolicy.ForceStrongerOnRetry || a.VerifierPolicy.FinalGateStronger {
		t.Fatalf("clear did not reach the policy: %+v", a.VerifierPolicy)
	}
}
