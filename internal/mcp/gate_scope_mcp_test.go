// Gate scope setting (full | touched-plus-smoke | touched) and
// smoke_packages on the MCP surfaces:
//   - autonomous_config_set carries quality_gates_scope /
//     quality_gates_smoke_packages into the default_quality_gates body,
//     failing early on an invalid scope
//   - autonomous_prd_set_quality_gates carries scope / smoke_packages
//     into the per-PRD set_quality_gates body
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// jsonMust renders an any-typed schema property for readable error output.
func jsonMust(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	if s, ok := v.(string); ok {
		return s
	}
	return string(b)
}

func TestGateScope_MCPSchemaConfigSet(t *testing.T) {
	props := (&Server{}).toolAutonomousConfigSet().InputSchema.Properties
	scope, ok := props["quality_gates_scope"]
	if !ok {
		b, _ := json.Marshal(props)
		t.Fatalf("autonomous_config_set lacks quality_gates_scope: %s", b)
	}
	if !strings.Contains(jsonMust(scope), "full") {
		t.Errorf("quality_gates_scope schema should name the valid values: %v", scope)
	}
	if _, ok := props["quality_gates_smoke_packages"]; !ok {
		t.Errorf("autonomous_config_set lacks quality_gates_smoke_packages")
	}
}

func TestGateScope_MCPSchemaPRDSetQualityGates(t *testing.T) {
	props := (&Server{}).toolAutonomousPRDSetQualityGates().InputSchema.Properties
	scope, ok := props["scope"]
	if !ok {
		b, _ := json.Marshal(props)
		t.Fatalf("autonomous_prd_set_quality_gates lacks scope: %s", b)
	}
	if !strings.Contains(jsonMust(scope), "touched-plus-smoke") {
		t.Errorf("scope schema should name the valid values: %v", scope)
	}
	if _, ok := props["smoke_packages"]; !ok {
		t.Errorf("autonomous_prd_set_quality_gates lacks smoke_packages")
	}
}

func TestGateScope_MCPConfigSetForwardsScopeAndSmokePackages(t *testing.T) {
	var path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	// smoke_packages arrives as a JSON array (the schema accepts either a
	// comma-separated string or a list; the list form is asserted here).
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"quality_gates_enabled":        true,
		"quality_gates_scope":          "touched-plus-smoke",
		"quality_gates_smoke_packages": []any{"internal/pipeline", "internal/autonomous"},
	}))
	if err != nil {
		t.Fatalf("autonomous_config_set: %v", err)
	}
	if res == nil {
		t.Fatal("autonomous_config_set returned no result")
	}
	if path != "/api/autonomous/config" {
		t.Fatalf("path = %s, want /api/autonomous/config", path)
	}
	qg, _ := body["default_quality_gates"].(map[string]any)
	if qg == nil {
		b, _ := json.Marshal(body)
		t.Fatalf("body lacks default_quality_gates: %s", b)
	}
	if qg["scope"] != "touched-plus-smoke" {
		t.Errorf("scope = %v, want touched-plus-smoke", qg["scope"])
	}
	if got, _ := qg["smoke_packages"].([]any); len(got) != 2 || got[0] != "internal/pipeline" {
		t.Errorf("smoke_packages = %v", qg["smoke_packages"])
	}
}

func TestGateScope_MCPConfigSetSmokePackagesCommaString(t *testing.T) {
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"quality_gates_smoke_packages": " internal/pipeline , internal/server ,",
	})); err != nil {
		t.Fatalf("autonomous_config_set: %v", err)
	}
	qg, _ := body["default_quality_gates"].(map[string]any)
	if qg == nil {
		t.Fatal("body lacks default_quality_gates")
	}
	if got, _ := qg["smoke_packages"].([]any); len(got) != 2 || got[0] != "internal/pipeline" || got[1] != "internal/server" {
		t.Errorf("smoke_packages = %v, want [internal/pipeline internal/server]", qg["smoke_packages"])
	}
}

func TestGateScope_MCPConfigSetRejectsInvalidScope(t *testing.T) {
	s := planTierWebServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("REST request left the MCP layer for an invalid scope")
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"quality_gates_scope": "half-of-it",
	}))
	if err != nil {
		t.Fatalf("autonomous_config_set: %v", err)
	}
	if res == nil || res.Content == nil {
		t.Fatal("autonomous_config_set returned no error result")
	}
	joined := automationText(t, res)
	for _, v := range []string{"full", "touched-plus-smoke", "touched"} {
		if !strings.Contains(joined, v) {
			t.Errorf("error does not name %q: %s", v, joined)
		}
	}
}

func TestGateScope_MCPPRDSetQualityGatesForwardsScopeAndSmokePackages(t *testing.T) {
	var path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousPRDSetQualityGates(context.Background(), callReq(map[string]any{
		"id":             "prd_test",
		"enabled":        true,
		"test_command":   "go test ./...",
		"scope":          "touched",
		"smoke_packages": "internal/pipeline",
	})); err != nil {
		t.Fatalf("autonomous_prd_set_quality_gates: %v", err)
	}
	if path != "/api/autonomous/prds/prd_test/set_quality_gates" {
		t.Fatalf("path = %s", path)
	}
	if body["scope"] != "touched" {
		t.Errorf("scope = %v, want touched", body["scope"])
	}
	if got, _ := body["smoke_packages"].([]any); len(got) != 1 || got[0] != "internal/pipeline" {
		t.Errorf("smoke_packages = %v", body["smoke_packages"])
	}
}

func TestGateScope_MCPPRDSetQualityGatesRejectsInvalidScope(t *testing.T) {
	s := planTierWebServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("REST request left the MCP layer for an invalid scope")
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousPRDSetQualityGates(context.Background(), callReq(map[string]any{
		"id":    "prd_test",
		"scope": "almost",
	}))
	if err != nil {
		t.Fatalf("autonomous_prd_set_quality_gates: %v", err)
	}
	if res == nil || res.Content == nil {
		t.Fatal("returned no error result")
	}
	if !strings.Contains(automationText(t, res), "touched-plus-smoke") {
		t.Errorf("error does not name the valid values: %s", automationText(t, res))
	}
}
