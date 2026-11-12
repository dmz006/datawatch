package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// validWorkerProfile is a user profile that passes workerprofile.Validate.
func validWorkerProfile() workerprofile.WorkerProfile {
	return workerprofile.WorkerProfile{
		Name:    "reviewer",
		MCP:     workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"github"}},
		Tools:   workerprofile.ToolSelection{Mode: workerprofile.ToolsAllow, Allow: []string{"Read", "Grep"}},
		Context: workerprofile.ContextDigest,
	}
}

// workerProfileCfg returns a default config with the given autonomous
// worker-profile settings applied.
func workerProfileCfg(profiles map[string]workerprofile.WorkerProfile, defaults WorkerProfileDefaultsConfig, global string) *Config {
	cfg := DefaultConfig()
	cfg.Autonomous.WorkerProfiles = profiles
	cfg.Autonomous.WorkerProfileDefaults = defaults
	cfg.Autonomous.WorkerProfile = global
	return cfg
}

func TestValidateWorkerProfileConfig_ValidConfigPasses(t *testing.T) {
	profiles := map[string]workerprofile.WorkerProfile{"reviewer": validWorkerProfile()}
	defaults := WorkerProfileDefaultsConfig{
		Role: map[string]string{"planner": "full", "verifier": "reviewer", "judge": ""},
		Kind: map[string]string{"docs": "reviewer", "note": "minimal"},
	}
	cfg := workerProfileCfg(profiles, defaults, "reviewer")
	if err := ValidateWorkerProfileConfig(cfg); err != nil {
		t.Fatalf("valid worker profile config refused: %v", err)
	}
	// An unset config is also valid: no user profiles, built-in defaults.
	if err := ValidateWorkerProfileConfig(DefaultConfig()); err != nil {
		t.Fatalf("default config refused: %v", err)
	}
	if err := ValidateWorkerProfileConfig(nil); err != nil {
		t.Fatalf("nil config refused: %v", err)
	}
}

func TestValidateWorkerProfileConfig_RefusesBuiltinName(t *testing.T) {
	for _, name := range workerprofile.Names() {
		p := validWorkerProfile()
		p.Name = name
		cfg := workerProfileCfg(map[string]workerprofile.WorkerProfile{name: p}, WorkerProfileDefaultsConfig{}, "")
		err := ValidateWorkerProfileConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "built-in profile name") {
			t.Errorf("user profile %q: want built-in name refusal, got %v", name, err)
		}
	}
}

func TestValidateWorkerProfileConfig_RefusesInvalidProfile(t *testing.T) {
	bad := validWorkerProfile()
	bad.Context = "bogus"
	cfg := workerProfileCfg(map[string]workerprofile.WorkerProfile{"reviewer": bad}, WorkerProfileDefaultsConfig{}, "")
	err := ValidateWorkerProfileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "autonomous.worker_profiles.reviewer") {
		t.Fatalf("invalid context: want refusal naming the profile, got %v", err)
	}

	empty := validWorkerProfile()
	empty.Tools = workerprofile.ToolSelection{Mode: workerprofile.ToolsAllow}
	cfg = workerProfileCfg(map[string]workerprofile.WorkerProfile{"reviewer": empty}, WorkerProfileDefaultsConfig{}, "")
	if err := ValidateWorkerProfileConfig(cfg); err == nil {
		t.Fatal("allow tool mode with no entries: want refusal, got nil")
	}
}

func TestValidateWorkerProfileConfig_RefusesNameMismatch(t *testing.T) {
	p := validWorkerProfile()
	p.Name = "other"
	cfg := workerProfileCfg(map[string]workerprofile.WorkerProfile{"reviewer": p}, WorkerProfileDefaultsConfig{}, "")
	if err := ValidateWorkerProfileConfig(cfg); err == nil || !strings.Contains(err.Error(), "must match its key") {
		t.Fatalf("name differing from key: want refusal, got %v", err)
	}
}

func TestValidateWorkerProfileConfig_RefusesUnknownDefaultProfile(t *testing.T) {
	profiles := map[string]workerprofile.WorkerProfile{"reviewer": validWorkerProfile()}

	roleCfg := workerProfileCfg(profiles, WorkerProfileDefaultsConfig{
		Role: map[string]string{"worker": "ghost"},
	}, "")
	if err := ValidateWorkerProfileConfig(roleCfg); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("role default naming unknown profile: want refusal, got %v", err)
	}

	kindCfg := workerProfileCfg(profiles, WorkerProfileDefaultsConfig{
		Kind: map[string]string{"docs": "ghost"},
	}, "")
	if err := ValidateWorkerProfileConfig(kindCfg); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("kind default naming unknown profile: want refusal, got %v", err)
	}
}

func TestValidateWorkerProfileConfig_RefusesUnknownRoleAndKindKeys(t *testing.T) {
	roleCfg := workerProfileCfg(nil, WorkerProfileDefaultsConfig{
		Role: map[string]string{"reviewer": "digest"},
	}, "")
	if err := ValidateWorkerProfileConfig(roleCfg); err == nil || !strings.Contains(err.Error(), `unknown key "reviewer"`) {
		t.Fatalf("unknown role key: want refusal, got %v", err)
	}

	kindCfg := workerProfileCfg(nil, WorkerProfileDefaultsConfig{
		Kind: map[string]string{"planner": "digest"},
	}, "")
	if err := ValidateWorkerProfileConfig(kindCfg); err == nil || !strings.Contains(err.Error(), `unknown key "planner"`) {
		t.Fatalf("role name used as kind key: want refusal, got %v", err)
	}
}

func TestValidateWorkerProfileConfig_RefusesUnknownGlobalProfile(t *testing.T) {
	cfg := workerProfileCfg(nil, WorkerProfileDefaultsConfig{}, "ghost")
	if err := ValidateWorkerProfileConfig(cfg); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("global profile naming unknown profile: want refusal, got %v", err)
	}
	cfg = workerProfileCfg(nil, WorkerProfileDefaultsConfig{}, "minimal")
	if err := ValidateWorkerProfileConfig(cfg); err != nil {
		t.Fatalf("global profile naming a built-in: want accepted, got %v", err)
	}
}

// TestLoadWorkerProfileConfig covers the config load path: the snake_case
// YAML keys are read, and Load refuses a bad worker profile setting.
func TestLoadWorkerProfileConfig(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	writeFile(t, good, `
autonomous:
  worker_profiles:
    reviewer:
      mcp:
        mode: list
        servers: [github]
      tools:
        mode: allow
        allow: [Read, Grep]
      context: digest
  worker_profile_defaults:
    role:
      verifier: reviewer
    kind:
      docs: reviewer
  worker_profile: reviewer
`)
	cfg, err := Load(good)
	if err != nil {
		t.Fatalf("Load valid worker profile config: %v", err)
	}
	p, ok := cfg.Autonomous.WorkerProfiles["reviewer"]
	if !ok {
		t.Fatal("worker_profiles.reviewer not loaded")
	}
	if p.MCP.Mode != workerprofile.MCPList || len(p.MCP.Servers) != 1 || p.Tools.Mode != workerprofile.ToolsAllow || p.Context != workerprofile.ContextDigest {
		t.Fatalf("loaded profile = %+v", p)
	}
	if cfg.Autonomous.WorkerProfileDefaults.Role["verifier"] != "reviewer" ||
		cfg.Autonomous.WorkerProfileDefaults.Kind["docs"] != "reviewer" ||
		cfg.Autonomous.WorkerProfile != "reviewer" {
		t.Fatalf("loaded defaults = %+v, global = %q", cfg.Autonomous.WorkerProfileDefaults, cfg.Autonomous.WorkerProfile)
	}

	bad := filepath.Join(dir, "bad.yaml")
	writeFile(t, bad, `
autonomous:
  worker_profile_defaults:
    role:
      worker: ghost
`)
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("Load with unknown default profile: want refusal, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}
