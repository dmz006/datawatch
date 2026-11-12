package mcp

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestChecksShipToolsSchemaAndCaps(t *testing.T) {
	s := &Server{}
	for _, tool := range []struct {
		name string
		got  string
		ro   *bool
		prop map[string]any
	}{
		{"checks_status", s.toolChecksStatus().Name, s.toolChecksStatus().Annotations.ReadOnlyHint, s.toolChecksStatus().InputSchema.Properties},
		{"ship_status", s.toolShipStatus().Name, s.toolShipStatus().Annotations.ReadOnlyHint, s.toolShipStatus().InputSchema.Properties},
	} {
		if tool.got != tool.name {
			t.Errorf("name = %q, want %q", tool.got, tool.name)
		}
		if tool.ro == nil || !*tool.ro {
			t.Errorf("%s must be read-only", tool.name)
		}
		for _, p := range []string{"prd_id", "project_dir"} {
			if _, ok := tool.prop[p]; !ok {
				t.Errorf("%s lacks %s", tool.name, p)
			}
		}
		if federation.MCPToolCap[tool.name] != federation.CapAutonomousRead {
			t.Errorf("%s capability = %q", tool.name, federation.MCPToolCap[tool.name])
		}
	}
	if len(ChecksShipToolNames()) != 2 {
		t.Error("ChecksShipToolNames must list both tools")
	}
}
