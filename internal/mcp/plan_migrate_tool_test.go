package mcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestPlanMigrateMCP_Definition(t *testing.T) {
	s := &Server{}
	tl := s.toolPlanMigrate()
	if tl.Annotations.ReadOnlyHint == nil || *tl.Annotations.ReadOnlyHint {
		t.Errorf("plan_migrate must not be read-only: %v", tl.Annotations.ReadOnlyHint)
	}
	if got := federation.MCPToolCap["plan_migrate"]; got != federation.CapAutonomousWrite {
		t.Errorf("capability = %q, want autonomous write", got)
	}
	if !federation.IsNonConfigWriteTool("plan_migrate") {
		t.Error("plan_migrate is not classified as a non-config-write tool")
	}
	for _, banned := range []string{"PRD", "BL4", "GH#"} {
		if strings.Contains(tl.Description, banned) {
			t.Errorf("description mentions %q", banned)
		}
	}
}

func TestPlanMigrateMCP_PreviewByDefault(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"note":"Preview only","counts":{},"result":{}}`)
	res := d.call(t, d.s.handlePlanMigrate, map[string]any{})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/migrate" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["apply"] != false {
		t.Errorf("body = %v, want apply false by default", b)
	}
	d.call(t, d.s.handlePlanMigrate, map[string]any{"apply": true})
	if b := d.bodyMap(t); b["apply"] != true {
		t.Errorf("body = %v, want apply true", b)
	}
	d.call(t, d.s.handlePlanMigrate, map[string]any{"apply": "true"})
	if b := d.bodyMap(t); b["apply"] != true {
		t.Errorf("text true gave %v", b)
	}
	if res := d.call(t, d.s.handlePlanMigrate, map[string]any{"apply": "maybe"}); !res.IsError {
		t.Error("a bad apply value must be a tool error")
	}
}
