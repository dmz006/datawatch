// plugins.services — the service-plugin watch settings: defaults,
// load-time validation (with the shared lifecycle-mode rule), and a full
// Load from YAML.
package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginServices_EffectiveDefaultsFromZero(t *testing.T) {
	s := PluginServicesConfig{}
	if got := s.EffectiveHealthInterval(); got != DefaultPluginServicesHealthInterval {
		t.Errorf("EffectiveHealthInterval = %d, want default %d", got, DefaultPluginServicesHealthInterval)
	}
	if got := s.EffectiveBacklogGrowthPulls(); got != DefaultPluginServicesBacklogGrowthPulls {
		t.Errorf("EffectiveBacklogGrowthPulls = %d, want default %d", got, DefaultPluginServicesBacklogGrowthPulls)
	}
	if got := s.EffectiveNodeLoadRatio(); got != DefaultPluginServicesNodeLoadRatio {
		t.Errorf("EffectiveNodeLoadRatio = %v, want default %v", got, DefaultPluginServicesNodeLoadRatio)
	}
	if got := s.EffectiveSmallModelMaxParamsBillions(); got != DefaultPluginServicesSmallModelMaxParamsBillions {
		t.Errorf("EffectiveSmallModelMaxParamsBillions = %v, want default %v", got, DefaultPluginServicesSmallModelMaxParamsBillions)
	}
	if got := s.EffectiveSmallModelContextTokens(); got != DefaultPluginServicesSmallModelContextTokens {
		t.Errorf("EffectiveSmallModelContextTokens = %d, want default %d", got, DefaultPluginServicesSmallModelContextTokens)
	}
	if s.LifecycleMode("mcp") != "" {
		t.Errorf("an absent per-service entry must read as an unset lifecycle mode, got %q", s.LifecycleMode("mcp"))
	}
	if s.LifecycleMode("missing") != "" {
		t.Errorf("an unknown plugin must read as an unset lifecycle mode, got %q", s.LifecycleMode("missing"))
	}
}

func TestPluginServices_EffectiveHonoursStoredValues(t *testing.T) {
	s := PluginServicesConfig{
		HealthIntervalSeconds:       90,
		BacklogGrowthPulls:          5,
		NodeLoadRatio:               2.5,
		SmallModelMaxParamsBillions: 7,
		SmallModelContextTokens:     16384,
		PerService: map[string]PluginServiceOverride{
			"mcp":    {LifecycleMode: PluginServiceLifecycleAttachOnly},
			"ollama": {LifecycleMode: PluginServiceLifecycleManaged},
			"auto":   {},
		},
	}
	if got := s.EffectiveHealthInterval(); got != 90 {
		t.Errorf("EffectiveHealthInterval = %d, want 90", got)
	}
	if got := s.EffectiveBacklogGrowthPulls(); got != 5 {
		t.Errorf("EffectiveBacklogGrowthPulls = %d, want 5", got)
	}
	if got := s.EffectiveNodeLoadRatio(); got != 2.5 {
		t.Errorf("EffectiveNodeLoadRatio = %v, want 2.5", got)
	}
	if got := s.EffectiveSmallModelMaxParamsBillions(); got != 7 {
		t.Errorf("EffectiveSmallModelMaxParamsBillions = %v, want 7", got)
	}
	if got := s.EffectiveSmallModelContextTokens(); got != 16384 {
		t.Errorf("EffectiveSmallModelContextTokens = %d, want 16384", got)
	}
	if got := s.LifecycleMode("mcp"); got != PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(mcp) = %q, want %q", got, PluginServiceLifecycleAttachOnly)
	}
	if got := s.LifecycleMode("ollama"); got != PluginServiceLifecycleManaged {
		t.Errorf("LifecycleMode(ollama) = %q, want %q", got, PluginServiceLifecycleManaged)
	}
	if got := s.LifecycleMode("auto"); got != "" {
		t.Errorf("LifecycleMode(auto) = %q, want unset", got)
	}
	// The stored value itself is normalized on read — case and spaces.
	s.PerService["x"] = PluginServiceOverride{LifecycleMode: "  ATTACH_ONLY "}
	if got := s.LifecycleMode("x"); got != "attach_only" {
		t.Errorf("LifecycleMode normalizes case and spaces on read: got %q, want attach_only", got)
	}
	// A value that slipped past the validator (a direct struct write) with
	// an unknown mode normalizes to unset — the fail-safe, so the plugins
	// package never sees a mode it does not implement.
	s.PerService["bad"] = PluginServiceOverride{LifecycleMode: "auto"}
	if got := s.LifecycleMode("bad"); got != "" {
		t.Errorf("an unknown mode must normalize to unset, got %q", got)
	}
}

func TestValidatePluginServiceMode_SharedRule(t *testing.T) {
	for _, ok := range []string{"", "attach_only", "managed", " MANAGED ", "Attach_Only"} {
		if err := ValidatePluginServiceMode("mcp", ok); err != nil {
			t.Errorf("ValidatePluginServiceMode(%q) = %v, want ok", ok, err)
		}
	}
	err := ValidatePluginServiceMode("mcp", "auto")
	if err == nil {
		t.Fatal("a misspelled mode must be rejected")
	}
	msg := err.Error()
	for _, want := range []string{"mcp", "attach_only", "managed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error must name the service and the valid modes: %q", msg)
		}
	}
}

func TestValidateLoaded_PluginServices(t *testing.T) {
	cases := []struct {
		name    string
		svc     PluginServicesConfig
		wantErr string // required substring; "" = must pass
	}{
		{"default (all unset) passes", PluginServicesConfig{}, ""},
		{"valid values and all lifecycle modes pass", PluginServicesConfig{
			HealthIntervalSeconds:       5,
			BacklogGrowthPulls:          1,
			NodeLoadRatio:               0.1,
			SmallModelMaxParamsBillions: 0.5,
			SmallModelContextTokens:     1,
			PerService: map[string]PluginServiceOverride{
				"mcp":    {LifecycleMode: PluginServiceLifecycleAttachOnly},
				"ollama": {LifecycleMode: PluginServiceLifecycleManaged},
				"auto":   {},
			},
		}, ""},
		{"health below minimum", PluginServicesConfig{HealthIntervalSeconds: 4}, "health_interval_seconds"},
		{"health zero reads the default", PluginServicesConfig{HealthIntervalSeconds: 0}, ""},
		{"backlog negative", PluginServicesConfig{BacklogGrowthPulls: -1}, "backlog_growth_pulls"},
		{"backlog zero reads the default", PluginServicesConfig{BacklogGrowthPulls: 0}, ""},
		{"ratio negative", PluginServicesConfig{NodeLoadRatio: -0.2}, "node_load_ratio"},
		{"ratio NaN", PluginServicesConfig{NodeLoadRatio: math.NaN()}, "node_load_ratio"},
		{"params negative", PluginServicesConfig{SmallModelMaxParamsBillions: -8}, "small_model_max_params_billions"},
		{"params NaN", PluginServicesConfig{SmallModelMaxParamsBillions: math.NaN()}, "small_model_max_params_billions"},
		{"context tokens zero", PluginServicesConfig{SmallModelContextTokens: 0}, ""},
		{"context tokens negative", PluginServicesConfig{SmallModelContextTokens: -1}, "small_model_context_tokens"},
		{"per-service misspelled mode names the service", PluginServicesConfig{
			PerService: map[string]PluginServiceOverride{"mcp": {LifecycleMode: "attach"}},
		}, "mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.svc.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("must pass, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("must fail, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error should contain %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidatePluginServicesErrorNamesFieldRuleAndDefault(t *testing.T) {
	err := PluginServicesConfig{HealthIntervalSeconds: 4}.Validate()
	if err == nil {
		t.Fatal("must fail")
	}
	msg := err.Error()
	for _, want := range []string{"plugins.services.health_interval_seconds", "4", "5", "60"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name the field, value, rule and default: %q", msg)
		}
	}
}

func TestLoad_PluginServicesFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
plugins:
  services:
    health_interval_seconds: 30
    backlog_growth_pulls: 4
    node_load_ratio: 2
    small_model_max_params_billions: 4
    small_model_context_tokens: 16384
    per_service:
      mcp:
        lifecycle_mode: attach_only
      ollama:
        lifecycle_mode: managed
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	svc := cfg.Plugins.Services
	if svc.HealthIntervalSeconds != 30 {
		t.Errorf("health_interval_seconds = %d, want 30", svc.HealthIntervalSeconds)
	}
	if svc.BacklogGrowthPulls != 4 {
		t.Errorf("backlog_growth_pulls = %d, want 4", svc.BacklogGrowthPulls)
	}
	if svc.NodeLoadRatio != 2 {
		t.Errorf("node_load_ratio = %v, want 2", svc.NodeLoadRatio)
	}
	if svc.SmallModelMaxParamsBillions != 4 {
		t.Errorf("small_model_max_params_billions = %v, want 4", svc.SmallModelMaxParamsBillions)
	}
	if svc.SmallModelContextTokens != 16384 {
		t.Errorf("small_model_context_tokens = %d, want 16384", svc.SmallModelContextTokens)
	}
	if got := svc.LifecycleMode("mcp"); got != PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(mcp) = %q, want attach_only", got)
	}
	if got := svc.LifecycleMode("ollama"); got != PluginServiceLifecycleManaged {
		t.Errorf("LifecycleMode(ollama) = %q, want managed", got)
	}

	// A value below the minimum is a load error with the full rule sentence.
	bad := `
plugins:
  services:
    health_interval_seconds: 4
`
	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(badPath); err == nil {
		t.Fatal("a health interval below the minimum must fail Load")
	} else if !strings.Contains(err.Error(), "health_interval_seconds") {
		t.Fatalf("the load error must name the field: %v", err)
	}
}
