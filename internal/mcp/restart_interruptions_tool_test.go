package mcp

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestRestartInterruptionsToolIsReadOnlyAndCapped(t *testing.T) {
	s := &Server{}
	tool := s.toolRestartInterruptions()
	if tool.Name != "restart_interruptions" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatal("restart_interruptions must be read-only")
	}
	if federation.MCPToolCap["restart_interruptions"] != federation.CapAutonomousRead {
		t.Fatalf("capability = %q", federation.MCPToolCap["restart_interruptions"])
	}
}
