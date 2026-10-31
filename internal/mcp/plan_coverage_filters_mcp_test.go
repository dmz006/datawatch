// MCP surface for the plan-requirements coverage gate filters:
//   - autonomous_prd_plan_coverage_get proxies GET /api/autonomous/prds/{id}/plan_coverage
//   - autonomous_config_set forwards the four filter keys to
//     PUT /api/autonomous/config (presence-aware, so an explicit false lands)
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestPlanCoverageFilters_MCP_GetTool_ProxiesRoute: the read tool proxies the
// stored plan-coverage report for one Automaton verbatim.
func TestPlanCoverageFilters_MCP_GetTool_ProxiesRoute(t *testing.T) {
	fake := `{"id":"abc","coverage":0.5,"pass":false,"unaddressed":[{"number":3,"text":"x"}],"suppressed":{"created_files":7},"ack":null}`
	var method, path string
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(fake))
	})
	res, err := s.handleAutonomousPRDPlanCoverageGet(context.Background(), callReq(map[string]any{"id": "abc"}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if method != http.MethodGet || path != "/api/autonomous/prds/abc/plan_coverage" {
		t.Fatalf("route = %s %s, want GET /api/autonomous/prds/abc/plan_coverage", method, path)
	}
	if res == nil || res.IsError {
		t.Fatalf("result = %+v, want success", res)
	}
	if tc, ok := res.Content[0].(mcpsdk.TextContent); !ok || !strings.Contains(tc.Text, fake) {
		t.Errorf("body not passed through: %v", res.Content)
	}
}

// TestPlanCoverageFilters_MCP_GetTool_MissingID: a blank id is refused before
// anything is proxied.
func TestPlanCoverageFilters_MCP_GetTool_MissingID(t *testing.T) {
	hit := false
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{}`))
	})
	for _, args := range []map[string]any{nil, {}, {"id": "  "}} {
		res, err := s.handleAutonomousPRDPlanCoverageGet(context.Background(), callReq(args))
		if err != nil {
			t.Fatalf("handle(%v): %v", args, err)
		}
		if res == nil || !res.IsError {
			t.Errorf("args %v: want an error result, got %+v", args, res)
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(mcpsdk.TextContent); ok {
				text = tc.Text
			}
		}
		if !strings.Contains(text, "id") {
			t.Errorf("args %v: message %q does not name the missing id", args, text)
		}
		if hit {
			t.Errorf("args %v: request proxied despite a blank id", args)
		}
	}
}

// TestPlanCoverageFilters_MCP_ConfigSet_ForwardsAll: an explicit false is
// sent (presence-aware, not value-aware), and all four filters land in the
// PUT body under their key names.
func TestPlanCoverageFilters_MCP_ConfigSet_ForwardsAll(t *testing.T) {
	var method, path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"plan_coverage_suppress_created_files": false, // explicit off must land
		"plan_coverage_scoped_requirements":    true,
		"plan_coverage_requirement_sections":   "work,notes",
		"plan_coverage_min_requirement_chars":  40,
	}))
	if err != nil {
		t.Fatalf("config set: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("config set: error result %+v", res)
	}
	if method != http.MethodPut || path != "/api/autonomous/config" {
		t.Fatalf("route = %s %s, want PUT /api/autonomous/config", method, path)
	}
	if body["plan_coverage_suppress_created_files"] != false {
		t.Errorf("suppress_created_files = %v, want false", body["plan_coverage_suppress_created_files"])
	}
	if body["plan_coverage_scoped_requirements"] != true {
		t.Errorf("scoped_requirements = %v, want true", body["plan_coverage_scoped_requirements"])
	}
	if body["plan_coverage_requirement_sections"] != "work,notes" {
		t.Errorf("requirement_sections = %v, want work,notes", body["plan_coverage_requirement_sections"])
	}
	if f, ok := body["plan_coverage_min_requirement_chars"].(float64); !ok || f != 40 {
		t.Errorf("min_requirement_chars = %v, want 40", body["plan_coverage_min_requirement_chars"])
	}
}

// TestPlanCoverageFilters_MCP_ConfigSet_ABsentParamsNotSent: without the
// filter arguments the PUT body carries none of the filter keys, so existing
// values are left untouched.
func TestPlanCoverageFilters_MCP_ConfigSet_ABsentParamsNotSent(t *testing.T) {
	var method, path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"plan_coverage_check": "block"})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if method != http.MethodPut || path != "/api/autonomous/config" {
		t.Fatalf("route = %s %s, want PUT /api/autonomous/config", method, path)
	}
	if body["plan_coverage_check"] != "block" {
		t.Errorf("plan_coverage_check = %v, want block", body["plan_coverage_check"])
	}
	for _, key := range []string{
		"plan_coverage_suppress_created_files",
		"plan_coverage_scoped_requirements",
		"plan_coverage_requirement_sections",
		"plan_coverage_min_requirement_chars",
	} {
		if _, ok := body[key]; ok {
			t.Errorf("%s sent although the argument was absent", key)
		}
	}
}

// TestPlanCoverageFilters_MCP_GetTool_ReadCapability: the tool is registered
// with the autonomous read capability (not the write one).
func TestPlanCoverageFilters_MCP_GetTool_ReadCapability(t *testing.T) {
	if got, ok := federation.MCPToolCap["autonomous_prd_plan_coverage_get"]; !ok || got != federation.CapAutonomousRead {
		t.Errorf("autonomous_prd_plan_coverage_get capability = %q (present=%v), want %q", got, ok, federation.CapAutonomousRead)
	}
}
