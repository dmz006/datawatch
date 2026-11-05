package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const planningRoutingMCPView = `{"enabled":false,"shadow":false,"rules":[],"ladders":{"default":{"name":"default","touch_up_max":1}},` +
	`"classes":{"security":{"name":"security"}},"validation":{"valid":true}}`

func TestPlanningRoutingToolsRegisteredWithCaps(t *testing.T) {
	s := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, tl := range tools {
		listed[tl.Name] = true
	}
	want := map[string]string{
		"planning_routing_get":  federation.CapAutonomousRead,
		"planning_routing_set":  federation.CapConfigWrite,
		"planning_scorecard":    federation.CapAutonomousRead,
		"planning_routing_test": federation.CapAutonomousRead,
	}
	for name, cap := range want {
		if !listed[name] {
			t.Errorf("%s is not registered", name)
		}
		if got := federation.MCPToolCap[name]; got != cap {
			t.Errorf("MCPToolCap[%s] = %q, want %q", name, got, cap)
		}
	}
}

func TestPlanningRoutingToolSchemas(t *testing.T) {
	s := &Server{}
	cases := []struct {
		tool  mcpsdk.Tool
		props []string
		ro    bool
	}{
		{s.toolPlanningRoutingGet(), nil, true},
		{s.toolPlanningRoutingSet(), []string{"enabled", "shadow", "rules", "ladders", "classes"}, false},
		{s.toolPlanningScorecard(), []string{"window"}, true},
		{s.toolPlanningRoutingTest(), []string{"kind", "tags", "spec_bytes", "files", "project"}, true},
	}
	for _, c := range cases {
		for _, p := range c.props {
			if _, ok := c.tool.InputSchema.Properties[p]; !ok {
				t.Errorf("%s lacks the %s parameter", c.tool.Name, p)
			}
		}
		if len(c.tool.InputSchema.Required) != 0 {
			t.Errorf("%s: every parameter must be optional, got %v", c.tool.Name, c.tool.InputSchema.Required)
		}
		ro := c.tool.Annotations.ReadOnlyHint != nil && *c.tool.Annotations.ReadOnlyHint
		if ro != c.ro {
			t.Errorf("%s read-only = %v, want %v", c.tool.Name, ro, c.ro)
		}
	}
}

func TestPlanningRoutingGetProxies(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(planningRoutingMCPView))
	})
	res, err := s.handlePlanningRoutingGet(context.Background(), callReq(nil))
	out := resultText(t, res, err)
	if method != http.MethodGet || path != "/api/autonomous/planning-routing" {
		t.Errorf("proxied %s %s", method, path)
	}
	if !strings.Contains(out, `"validation"`) {
		t.Errorf("body not returned: %s", out)
	}
}

func TestPlanningRoutingSetMergesOntoCurrent(t *testing.T) {
	var putBody map[string]any
	var putMethod string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putMethod = r.Method
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &putBody)
		}
		_, _ = w.Write([]byte(planningRoutingMCPView))
	})
	rules := `[{"name":"a","planners":[{"backend":"b","model":"m"}]}]`
	res, err := s.handlePlanningRoutingSet(context.Background(), callReq(map[string]any{
		"enabled": true, "shadow": false, "rules": rules,
	}))
	resultText(t, res, err)
	if putMethod != http.MethodPut {
		t.Fatal("set must PUT")
	}
	if putBody["enabled"] != true || putBody["shadow"] != false {
		t.Errorf("flags wrong: %v", putBody)
	}
	if rs, _ := putBody["rules"].([]any); len(rs) != 1 {
		t.Errorf("rules not sent: %v", putBody["rules"])
	}
	if _, ok := putBody["ladders"]; !ok {
		t.Error("a field left out must keep its current value (ladders)")
	}
	if _, ok := putBody["validation"]; ok {
		t.Error("the read-only validation member must not be sent back")
	}
}

func TestPlanningRoutingSetRejectsBadArgs(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(planningRoutingMCPView))
	})
	for name, args := range map[string]map[string]any{
		"enabled not bool": {"enabled": "yes"},
		"rules not json":   {"rules": "{"},
		"ladders not json": {"ladders": "nope"},
	} {
		if _, err := s.handlePlanningRoutingSet(context.Background(), callReq(args)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestPlanningRoutingSetSurfacesRESTRejection(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			http.Error(w, "planning_routing: rule \"a\" has no planners", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(planningRoutingMCPView))
	})
	_, err := s.handlePlanningRoutingSet(context.Background(), callReq(map[string]any{"rules": `[{"name":"a","planners":[]}]`}))
	if err == nil || !strings.Contains(err.Error(), "no planners") {
		t.Errorf("err = %v, want the validation message", err)
	}
}

func TestPlanningRoutingScorecardProxiesWindow(t *testing.T) {
	var path, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"window":"36h0m0s","report":"Planning gate report"}`))
	})
	res, err := s.handlePlanningScorecard(context.Background(), callReq(map[string]any{"window": "36h"}))
	out := resultText(t, res, err)
	if path != "/api/autonomous/planning-routing/scorecard" || query != "window=36h" {
		t.Errorf("path=%s query=%q", path, query)
	}
	if !strings.Contains(out, "Planning gate report") {
		t.Errorf("body not returned: %s", out)
	}
	_, _ = s.handlePlanningScorecard(context.Background(), callReq(nil))
	if query != "" {
		t.Errorf("no window must send no query, got %q", query)
	}
}

func TestPlanningRoutingTestProxiesInput(t *testing.T) {
	var path, method string
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"enabled":true,"rule":"sec","reason":"rule-matched"}`))
	})
	res, err := s.handlePlanningRoutingTest(context.Background(), callReq(map[string]any{
		"kind": "feature", "tags": "security, ui", "spec_bytes": float64(900), "files": "a.go,b.go", "project": "p",
	}))
	out := resultText(t, res, err)
	if method != http.MethodPost || path != "/api/autonomous/planning-routing/test" {
		t.Errorf("proxied %s %s", method, path)
	}
	if body["kind"] != "feature" || body["project"] != "p" || body["spec_bytes"] != float64(900) {
		t.Errorf("body wrong: %v", body)
	}
	if tags, _ := body["tags"].([]any); len(tags) != 2 || tags[1] != "ui" {
		t.Errorf("tags not split and trimmed: %v", body["tags"])
	}
	if files, _ := body["files"].([]any); len(files) != 2 {
		t.Errorf("files not split: %v", body["files"])
	}
	if !strings.Contains(out, "rule-matched") {
		t.Errorf("body not returned: %s", out)
	}
	// Empty lists go out as [] not null.
	_, _ = s.handlePlanningRoutingTest(context.Background(), callReq(nil))
	if tags, ok := body["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("empty tags must be []: %v", body["tags"])
	}
}
