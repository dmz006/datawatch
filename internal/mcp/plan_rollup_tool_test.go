package mcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestPlanRollupMCP_Definition(t *testing.T) {
	s := &Server{}
	tl := s.toolPlanRollup()
	if tl.Name != "plan_rollup" {
		t.Fatalf("name = %q", tl.Name)
	}
	if tl.Annotations.ReadOnlyHint == nil || *tl.Annotations.ReadOnlyHint {
		t.Errorf("plan_rollup writes unless dry_run, so it must not be read-only: %v", tl.Annotations.ReadOnlyHint)
	}
	if got := federation.MCPToolCap["plan_rollup"]; got != federation.CapAutonomousWrite {
		t.Errorf("capability = %q, want autonomous write", got)
	}
	if !federation.IsNonConfigWriteTool("plan_rollup") {
		t.Error("plan_rollup is not classified as a non-config-write tool")
	}
	for _, arg := range []string{"dry_run", "project"} {
		if _, ok := tl.InputSchema.Properties[arg]; !ok {
			t.Errorf("plan_rollup has no %s argument", arg)
		}
	}
	for _, banned := range []string{"PRD", "BL4", "GH#"} {
		if strings.Contains(tl.Description, banned) {
			t.Errorf("description mentions %q", banned)
		}
	}
	for _, want := range []string{"set by hand", "dry_run"} {
		if !strings.Contains(tl.Description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}

// PlanRecordToolNames feeds TestPlanRecordMCP_Registered,
// which checks each name is registered on a real server, so listing plan_rollup
// there keeps the tool count in step.
func TestPlanRollupMCP_InTheParityList(t *testing.T) {
	for _, n := range PlanRecordToolNames() {
		if n == "plan_rollup" {
			return
		}
	}
	t.Error("plan_rollup is missing from PlanRecordToolNames")
}

func TestPlanRollupMCP_DryRunUsesTheReadRoute(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"dry_run":true,"applied":false,"changed":1,"kept":0,"changes":[{"title":"Ship","plan":"ship","from":"open","to":"done","reason":"all 1 linked items finished"}]}`)
	res := d.call(t, d.s.handlePlanRollup, map[string]any{"dry_run": true, "project": "acme"})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plans/rollup" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	if q := d.query["dry_run"]; len(q) != 1 || q[0] != "true" {
		t.Errorf("dry_run query = %v", q)
	}
	if q := d.query["project"]; len(q) != 1 || q[0] != "acme" {
		t.Errorf("project query = %v", q)
	}
	if !strings.Contains(planResultText(res), `"to":"done"`) {
		t.Errorf("reply not passed through: %s", planResultText(res))
	}
	// Text true is accepted too.
	d.call(t, d.s.handlePlanRollup, map[string]any{"dry_run": "true"})
	if d.method != http.MethodGet {
		t.Errorf("text true proxied %s", d.method)
	}
	if _, has := d.query["project"]; has {
		t.Errorf("no project given, but the query has %v", d.query["project"])
	}
}

func TestPlanRollupMCP_ApplyPosts(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"dry_run":false,"applied":true,"changed":0,"kept":0,"changes":[]}`)
	res := d.call(t, d.s.handlePlanRollup, map[string]any{})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/rollup" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["dry_run"] != false || b["project"] != nil {
		t.Errorf("body = %v, want dry_run false and no project", b)
	}
	d.call(t, d.s.handlePlanRollup, map[string]any{"dry_run": false, "project": "acme"})
	if b := d.bodyMap(t); b["dry_run"] != false || b["project"] != "acme" {
		t.Errorf("body = %v", b)
	}
}

func TestPlanRollupMCP_BadArgumentAndDaemonError(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	if res := d.call(t, d.s.handlePlanRollup, map[string]any{"dry_run": "maybe"}); !res.IsError {
		t.Error("a bad dry_run text must be a tool error")
	}
	if res := d.call(t, d.s.handlePlanRollup, map[string]any{"dry_run": 3}); !res.IsError {
		t.Error("a numeric dry_run must be a tool error")
	}
	d = newPlanRecordTestDaemon(t, http.StatusBadRequest, `{"error":"unknown project"}`)
	res := d.call(t, d.s.handlePlanRollup, map[string]any{"project": "ghost"})
	if !res.IsError || !strings.Contains(planResultText(res), "unknown project") {
		t.Errorf("daemon refusal not surfaced: %v %s", res.IsError, planResultText(res))
	}
}

func TestPlanRecordMCP_DescriptionsShowStatusAndSetByHand(t *testing.T) {
	s := &Server{}
	for name, d := range map[string]string{"plan_record_list": s.toolPlanRecordList().Description, "plan_record_get": s.toolPlanRecordGet().Description} {
		for _, want := range []string{"set_by_hand", "status"} {
			if !strings.Contains(d, want) {
				t.Errorf("%s description lacks %q", name, want)
			}
		}
	}
}

func TestPlanRecordMCP_GetPassesSetByHandThrough(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"record":{"title":"T","id":"t","state":"done","set_by_hand":true}}`)
	res := d.call(t, d.s.handlePlanRecordGet, map[string]any{"kind": "registry-row", "ref": "t"})
	if res.IsError || !strings.Contains(planResultText(res), `"set_by_hand":true`) || !strings.Contains(planResultText(res), `"state":"done"`) {
		t.Fatalf("get output: %v %s", res.IsError, planResultText(res))
	}
}
