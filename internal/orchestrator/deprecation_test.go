package orchestrator

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeprecationNoticeNamesReplacement(t *testing.T) {
	n := DeprecationNotice("orchestrator_graph_run")
	for _, want := range []string{"orchestrator_graph_run", "deprecated", "import-graph", "Automation"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice %q missing %q", n, want)
		}
	}
}

func TestDeprecationWarnOncePerTool(t *testing.T) {
	resetDeprecationWarned()
	var msgs []string
	warn := func(format string, args ...any) { msgs = append(msgs, fmt.Sprintf(format, args...)) }
	tools := []string{"orchestrator_graph_create", "orchestrator_graph_plan", "orchestrator_graph_run"}
	for _, tool := range tools {
		if !WarnDeprecatedOnce(tool, warn) {
			t.Errorf("%s: first call should return true", tool)
		}
		if WarnDeprecatedOnce(tool, warn) {
			t.Errorf("%s: second call should return false", tool)
		}
	}
	if len(msgs) != 3 {
		t.Fatalf("warnings = %d, want 3: %v", len(msgs), msgs)
	}
	for i, tool := range tools {
		if !strings.Contains(msgs[i], tool) {
			t.Errorf("warning %d %q does not name %s", i, msgs[i], tool)
		}
	}
}
