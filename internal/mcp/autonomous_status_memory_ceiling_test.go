package mcp

import (
	"strings"
	"testing"
)

func TestAutonomousStatusToolDescribesMemoryCeiling(t *testing.T) {
	s := &Server{}
	d := s.toolAutonomousStatus().Description
	for _, want := range []string{"memory_ceiling", "effective_mb", "source", "enforcement_path", "watchdog", "resource_limits.memory_cap_mb"} {
		if !strings.Contains(d, want) {
			t.Errorf("autonomous_status description lacks %q", want)
		}
	}
}
