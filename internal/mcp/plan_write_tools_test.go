package mcp

import (
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestPlanWriteMCP_Definitions(t *testing.T) {
	s := &Server{}
	for name, tl := range map[string]mcpsdk.Tool{
		"plan_create": s.toolPlanCreate(), "plan_reorder": s.toolPlanReorder(),
		"plan_reorder_automata": s.toolPlanReorderAutomata(), "plan_attach": s.toolPlanAttach(), "plan_detach": s.toolPlanDetach(),
	} {
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
}

func TestPlanWriteMCP_ProxyRoutes(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"ok":true}`)
	if res := d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": " T ", "goal": "g"}); res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans" {
		t.Fatalf("create proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["project"] != "p" || b["title"] != "T" || b["goal"] != "g" {
		t.Fatalf("create body %v", b)
	}
	d.call(t, d.s.handlePlanReorder, map[string]any{"plans": []any{"b", "a"}})
	if d.method != http.MethodPut || d.path != "/api/plans/order" {
		t.Fatalf("reorder proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); len(b["plans"].([]any)) != 2 {
		t.Fatalf("reorder body %v", b)
	}
	d.call(t, d.s.handlePlanReorderAutomata, map[string]any{"plan": "my plan", "automata": `["x","y"]`})
	if d.method != http.MethodPut || d.rawPath != "/api/plans/my%20plan/automata/order" {
		t.Fatalf("reorder-automata proxied %s %s", d.method, d.rawPath)
	}
	d.call(t, d.s.handlePlanAttach, map[string]any{"plan": "p1", "kind": "Finding", "id": "f1"})
	if d.method != http.MethodPost || d.path != "/api/plans/p1/attach" {
		t.Fatalf("attach proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["kind"] != "finding" || b["id"] != "f1" {
		t.Fatalf("attach body %v", b)
	}
	d.call(t, d.s.handlePlanDetach, map[string]any{"plan": "p1", "kind": "automation", "id": "au"})
	if d.path != "/api/plans/p1/detach" {
		t.Fatalf("detach proxied %s", d.path)
	}
}

func TestPlanWriteMCP_RefusesBadArguments(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	bad := []struct {
		name string
		call func() *mcpsdk.CallToolResult
	}{
		{name: "create no title", call: func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p"}) }},
		{name: "create no project", call: func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanCreate, map[string]any{"title": "t"}) }},
		{name: "reorder empty", call: func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanReorder, map[string]any{"plans": []any{}})
		}},
		{name: "reorder non-text", call: func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanReorder, map[string]any{"plans": []any{1}})
		}},
		{name: "automata no plan", call: func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanReorderAutomata, map[string]any{"automata": []any{"a"}})
		}},
		{name: "attach bad kind", call: func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanAttach, map[string]any{"plan": "p", "kind": "row", "id": "x"})
		}},
		{name: "detach no id", call: func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanDetach, map[string]any{"plan": "p", "kind": "automaton"})
		}},
	}
	for _, b := range bad {
		if res := b.call(); !res.IsError {
			t.Errorf("%s: want a tool error", b.name)
		}
	}
	if d.method != "" {
		t.Errorf("a bad call reached the daemon: %s %s", d.method, d.path)
	}
}
