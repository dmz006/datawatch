package main

import (
	"strings"
	"testing"
)

// The start-up log says which settings need a restart, so an operator reading
// the log knows the rest apply the moment they are saved.
func TestLiveSettingsStartupLine(t *testing.T) {
	line := liveSettingsStartupLine()
	if !strings.HasPrefix(line, "[config] ") {
		t.Fatalf("line %q does not start with the [config] tag", line)
	}
	for _, want := range []string{"apply as soon as they are saved", "restart", "server.port", "mcp.sse_port", "database.*"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not contain %q", line, want)
		}
	}
	if strings.Contains(line, "\n") {
		t.Errorf("the start-up line must be one line: %q", line)
	}
}
