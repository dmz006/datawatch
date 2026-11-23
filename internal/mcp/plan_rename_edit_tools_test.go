package mcp

import (
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestPlanRenameEditMCP_Definitions(t *testing.T) {
	s := &Server{}
	for name, tl := range map[string]mcpsdk.Tool{"plan_rename": s.toolPlanRename(), "plan_edit": s.toolPlanEdit()} {
		if tl.Name != name {
			t.Errorf("tool name %q, want %q", tl.Name, name)
		}
		if tl.Annotations.ReadOnlyHint == nil || *tl.Annotations.ReadOnlyHint {
			t.Errorf("%s must not be read-only", name)
		}
		if got := federation.MCPToolCap[name]; got != federation.CapAutonomousWrite {
			t.Errorf("%s capability = %q, want autonomous write", name, got)
		}
		if !federation.IsNonConfigWriteTool(name) {
			t.Errorf("%s is not classified as a non-config-write tool", name)
		}
		for _, banned := range []string{"PRD", "BL4", "GH#"} {
			if strings.Contains(tl.Description, banned) {
				t.Errorf("%s description mentions %q", name, banned)
			}
		}
	}
	listed := map[string]bool{}
	for _, n := range PlanRecordToolNames() {
		listed[n] = true
	}
	for _, n := range []string{"plan_rename", "plan_edit"} {
		if !listed[n] {
			t.Errorf("%s is missing from PlanRecordToolNames", n)
		}
	}
}

func TestPlanRenameEditMCP_ProxyRoutes(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"ok":true}`)
	res := d.call(t, d.s.handlePlanRename, map[string]any{"plan": "my plan", "title": "  Better name "})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPatch || d.rawPath != "/api/plans/my%20plan" {
		t.Fatalf("rename proxied %s %s", d.method, d.rawPath)
	}
	if b := d.bodyMap(t); b["title"] != "Better name" || len(b) != 1 {
		t.Fatalf("rename body %v", b)
	}
	if res = d.call(t, d.s.handlePlanEdit, map[string]any{"plan": "p1", "goal": " ship it "}); res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPatch || d.path != "/api/plans/p1" {
		t.Fatalf("edit proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["goal"] != "ship it" || len(b) != 1 {
		t.Fatalf("edit body %v", b)
	}
	// An empty goal is sent, so it clears the goal.
	d.call(t, d.s.handlePlanEdit, map[string]any{"plan": "p1", "goal": ""})
	if b := d.bodyMap(t); b["goal"] != "" {
		t.Fatalf("clear body %v", b)
	}
}

func TestPlanRenameEditMCP_RefusesBadArguments(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	bad := map[string]func() *mcpsdk.CallToolResult{
		"rename no plan":  func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanRename, map[string]any{"title": "t"}) },
		"rename no title": func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanRename, map[string]any{"plan": "p"}) },
		"rename blank": func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanRename, map[string]any{"plan": "p", "title": "  "})
		},
		"edit no plan": func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanEdit, map[string]any{"goal": "g"}) },
		"edit no goal": func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanEdit, map[string]any{"plan": "p"}) },
		"edit non-text": func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanEdit, map[string]any{"plan": "p", "goal": 3})
		},
	}
	for name, call := range bad {
		if res := call(); !res.IsError {
			t.Errorf("%s: want a tool error", name)
		}
	}
	if d.method != "" {
		t.Errorf("a bad call reached the daemon: %s %s", d.method, d.path)
	}
}

func TestPlanRenameEditMCP_SurfacesTheDaemonConflict(t *testing.T) {
	d := newPlanRecordTestDaemon(t, http.StatusConflict, `{"error":"the title \"Alpha\" is already used in project datawatch by \"Alpha\" (plan-1)"}`)
	res := d.call(t, d.s.handlePlanRename, map[string]any{"plan": "b", "title": "alpha"})
	if !res.IsError || !strings.Contains(planResultText(res), "Alpha") {
		t.Fatalf("conflict not surfaced: %v %s", res.IsError, planResultText(res))
	}
}
