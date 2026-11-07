// plugins.services.* — applyConfigPatch keys, the block-level PUT
// validation (same rules and sentences as config load), and the GET/PUT
// /api/config round-trip.
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newPluginServicesServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func TestApplyConfigPatch_PluginServicesKeys(t *testing.T) {
	cfg := &config.Config{}
	patch := map[string]interface{}{
		"plugins.services.health_interval_seconds":            float64(90),
		"plugins.services.backlog_growth_pulls":               float64(5),
		"plugins.services.node_load_ratio":                    2.5,
		"plugins.services.small_model_max_params_billions":    float64(7),
		"plugins.services.small_model_context_tokens":         float64(16384),
		// per_service as a single object...
		"plugins.services.per_service": map[string]interface{}{
			"a string value":   "attach_only",
			"an object value":  map[string]interface{}{"lifecycle_mode": "managed"},
		},
		// ...and as one dynamic key per plugin.
		"plugins.services.per_service.vault": "attach_only",
	}
	applyConfigPatch(cfg, patch)

	svc := cfg.Plugins.Services
	if svc.HealthIntervalSeconds != 90 {
		t.Errorf("HealthIntervalSeconds = %d, want 90", svc.HealthIntervalSeconds)
	}
	if svc.BacklogGrowthPulls != 5 {
		t.Errorf("BacklogGrowthPulls = %d, want 5", svc.BacklogGrowthPulls)
	}
	if svc.NodeLoadRatio != 2.5 {
		t.Errorf("NodeLoadRatio = %v, want 2.5", svc.NodeLoadRatio)
	}
	if svc.SmallModelMaxParamsBillions != 7 {
		t.Errorf("SmallModelMaxParamsBillions = %v, want 7", svc.SmallModelMaxParamsBillions)
	}
	if svc.SmallModelContextTokens != 16384 {
		t.Errorf("SmallModelContextTokens = %d, want 16384", svc.SmallModelContextTokens)
	}
	if got := svc.LifecycleMode("a string value"); got != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(%q) = %q, want attach_only", "a string value", got)
	}
	if got := svc.LifecycleMode("an object value"); got != config.PluginServiceLifecycleManaged {
		t.Errorf("LifecycleMode(%q) = %q, want managed", "an object value", got)
	}
	if got := svc.LifecycleMode("vault"); got != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(vault) = %q, want attach_only", got)
	}
}

func TestApplyConfigPatch_PluginServicesRefusesBadValues(t *testing.T) {
	cfg := &config.Config{}
	patch := map[string]interface{}{
		"plugins.services.health_interval_seconds":       float64(4),  // below the minimum
		"plugins.services.backlog_growth_pulls":          float64(-1), // negative
		"plugins.services.node_load_ratio":               float64(-1), // negative
		"plugins.services.small_model_max_params_billions": float64(0), // zero = unset
		"plugins.services.small_model_context_tokens":    float64(-1),  // negative
		"plugins.services.per_service.mcp":               "nope",      // misspelled mode
		"plugins.services.per_service.bad":               map[string]interface{}{"lifecycle_mode": "attach"},
	}
	applyConfigPatch(cfg, patch)
	svc := cfg.Plugins.Services
	if svc.HealthIntervalSeconds != 0 {
		t.Errorf("a refused interval must not be stored: %d", svc.HealthIntervalSeconds)
	}
	if svc.BacklogGrowthPulls != 0 {
		t.Errorf("a refused streak must not be stored: %d", svc.BacklogGrowthPulls)
	}
	if svc.NodeLoadRatio != 0 {
		t.Errorf("a refused ratio must not be stored: %v", svc.NodeLoadRatio)
	}
	if svc.SmallModelMaxParamsBillions != 0 {
		t.Errorf("a refused params-boundary must not be stored: %v", svc.SmallModelMaxParamsBillions)
	}
	if svc.SmallModelContextTokens != 0 {
		t.Errorf("a refused context boundary must not be stored: %d", svc.SmallModelContextTokens)
	}
	if len(svc.PerService) != 0 {
		t.Errorf("a refused lifecycle mode must not be stored: %v", svc.PerService)
	}
}

func TestValidatePluginServicesPatch(t *testing.T) {
	// A fully valid block passes, on stored defaults.
	if err := validatePluginServicesPatch(config.PluginServicesConfig{}, map[string]interface{}{
		"plugins.services.health_interval_seconds":        float64(90),
		"plugins.services.backlog_growth_pulls":           float64(4),
		"plugins.services.node_load_ratio":                2.5,
		"plugins.services.small_model_max_params_billions": float64(7),
		"plugins.services.small_model_context_tokens":     float64(16384),
		"plugins.services.per_service":                    map[string]interface{}{"mcp": "attach_only", "ollama": map[string]interface{}{"lifecycle_mode": "managed"}},
		"plugins.services.per_service.vault":              "attach_only",
	}); err != nil {
		t.Fatalf("a valid block must pass: %v", err)
	}

	bad := []struct {
		name    string
		patch   map[string]interface{}
		wantErr string
	}{
		{"below-minimum interval", map[string]interface{}{"plugins.services.health_interval_seconds": float64(4)}, "health_interval_seconds"},
		{"negative streak", map[string]interface{}{"plugins.services.backlog_growth_pulls": float64(-1)}, "backlog_growth_pulls"},
		{"negative ratio", map[string]interface{}{"plugins.services.node_load_ratio": float64(-1)}, "node_load_ratio"},
		{"misspelled mode", map[string]interface{}{"plugins.services.per_service.mcp": "nope"}, "mcp"},
		{"misspelled mode in the object form", map[string]interface{}{"plugins.services.per_service": map[string]interface{}{"mcp": "attach"}}, "mcp"},
	}
	for _, tc := range bad {
		if err := validatePluginServicesPatch(config.PluginServicesConfig{}, tc.patch); err == nil {
			t.Errorf("%s: must fail", tc.name)
		} else if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error should contain %q, got: %v", tc.name, tc.wantErr, err)
		} else if !strings.Contains(err.Error(), "plugins.services") {
			t.Errorf("%s: error should name the config path, got: %v", tc.name, err)
		}
	}

	// Zero values read the documented defaults: a PUT of 0 is a legal
	// no-op (the stored zero already means "use the default"), not an
	// error — same zero-as-default rule as the model-use thresholds.
	if err := validatePluginServicesPatch(config.PluginServicesConfig{}, map[string]interface{}{"plugins.services.backlog_growth_pulls": float64(0)}); err != nil {
		t.Errorf("a zero value reads the documented default and must pass: %v", err)
	}

	// The block rule: the stored value plus a patch that breaks it is
	// refused even though the stored value alone is a legal default — the
	// simulated result is what Load would see.
	stored := config.PluginServicesConfig{HealthIntervalSeconds: 60}
	if err := validatePluginServicesPatch(stored, map[string]interface{}{
		"plugins.services.per_service": map[string]interface{}{"mcp": "nope"},
	}); err == nil {
		t.Fatal("a PUT that leaves an invalid block must be refused")
	}
}

func TestPluginServices_PutGetRoundTrip(t *testing.T) {
	s := newPluginServicesServer(t)

	// PUT the whole block.
	body, _ := json.Marshal(map[string]any{
		"plugins.services.health_interval_seconds":         60,
		"plugins.services.backlog_growth_pulls":            3,
		"plugins.services.node_load_ratio":                 1.5,
		"plugins.services.small_model_max_params_billions": 8,
		"plugins.services.small_model_context_tokens":      8192,
		"plugins.services.per_service": map[string]any{
			"mcp":    "attach_only",
			"ollama": map[string]any{"lifecycle_mode": "managed"},
		},
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	svc := s.cfg.Plugins.Services
	if svc.HealthIntervalSeconds != 60 || svc.BacklogGrowthPulls != 3 {
		t.Fatalf("stored block after PUT = %+v", svc)
	}
	if got := svc.LifecycleMode("mcp"); got != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(mcp) after PUT = %q, want attach_only", got)
	}
	if got := svc.LifecycleMode("ollama"); got != config.PluginServiceLifecycleManaged {
		t.Errorf("LifecycleMode(ollama) after PUT = %q, want managed", got)
	}

	// GET exposes the block with the stored values.
	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	plugins, ok := got["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("GET response has no plugins object: %s", rr.Body.String())
	}
	surface, ok := plugins["services"].(map[string]any)
	if !ok {
		t.Fatalf("plugins.services object missing: %s", rr.Body.String())
	}
	if v, ok := surface["health_interval_seconds"].(float64); !ok || v != 60 {
		t.Errorf("GET services.health_interval_seconds = %v, want 60", surface["health_interval_seconds"])
	}
	if v, ok := surface["node_load_ratio"].(float64); !ok || v != 1.5 {
		t.Errorf("GET services.node_load_ratio = %v, want 1.5", surface["node_load_ratio"])
	}
	per, ok := surface["per_service"].(map[string]any)
	if !ok {
		t.Fatalf("plugins.services.per_service object missing: %s", rr.Body.String())
	}
	mcp, ok := per["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("per_service.mcp object missing: %s", rr.Body.String())
	}
	if mcp["lifecycle_mode"] != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("GET per_service.mcp.lifecycle_mode = %v, want attach_only", mcp["lifecycle_mode"])
	}
}

func TestPluginServices_PutRefusesBadValues(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"below-minimum interval", map[string]any{"plugins.services.health_interval_seconds": 4}, "plugins.services.health_interval_seconds"},
		{"misspelled mode (dynamic key)", map[string]any{"plugins.services.per_service.mcp": "nope"}, "mcp"},
		{"misspelled mode (object form)", map[string]any{"plugins.services.per_service": map[string]any{"mcp": "attach"}}, "mcp"},
		{"negative ratio", map[string]any{"plugins.services.node_load_ratio": -1}, "plugins.services.node_load_ratio"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newPluginServicesServer(t)
			b, _ := json.Marshal(tc.body)
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("error body should contain %q: %s", tc.want, rr.Body.String())
			}
			// A refused PUT applies nothing.
			if len(s.cfg.Plugins.Services.PerService) != 0 || s.cfg.Plugins.Services.HealthIntervalSeconds != 0 || s.cfg.Plugins.Services.NodeLoadRatio != 0 {
				t.Fatalf("a refused block must not be applied: %+v", s.cfg.Plugins.Services)
			}
		})
	}
}

func TestPluginServices_PutValidatesTheCombinedBlock(t *testing.T) {
	s := newPluginServicesServer(t)
	// A stored default plus a patch that breaks the combined state must be
	// refused: Load would reject the block at the next startup.
	b, _ := json.Marshal(map[string]any{
		"plugins.services.health_interval_seconds": 20,
		"plugins.services.per_service":             map[string]any{"mcp": "attach_only", "ollama": "nope"},
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}

	// ...while the same PUT without the bad entry applies cleanly, and a
	// dynamic-key PUT lands the per-plugin entry on top of an object-form
	// block already stored by the same handler.
	b, _ = json.Marshal(map[string]any{"plugins.services.per_service": map[string]any{"mcp": "attach_only"}})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("valid PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	b, _ = json.Marshal(map[string]any{"plugins.services.health_interval_seconds": 20, "plugins.services.per_service.vault": "attach_only"})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("valid PUT 2: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Plugins.Services.LifecycleMode("mcp"); got != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(mcp) = %q, want attach_only", got)
	}
	if got := s.cfg.Plugins.Services.LifecycleMode("vault"); got != config.PluginServiceLifecycleAttachOnly {
		t.Errorf("LifecycleMode(vault) = %q, want attach_only", got)
	}
	if s.cfg.Plugins.Services.HealthIntervalSeconds != 20 {
		t.Errorf("HealthIntervalSeconds = %d, want 20", s.cfg.Plugins.Services.HealthIntervalSeconds)
	}
}
