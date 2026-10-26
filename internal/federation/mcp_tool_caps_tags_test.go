package federation

import "testing"

// TestTagToolCapsMatchTheirRoutes checks that the tag-surface MCP tools are
// granted the capability of the route they proxy: reads need the read
// capability, writes need the write capability of their resource.
func TestTagToolCapsMatchTheirRoutes(t *testing.T) {
	want := map[string]string{
		"autonomous_tag_list":       CapAutonomousList,
		"autonomous_attention_list": CapAutonomousRead,
		"autonomous_prd_set_tags":   CapAutonomousWrite,
		"session_set_tags":          CapSessionsWrite,
	}
	for tool, cap := range want {
		got, ok := RequiredCapForMCPTool(tool)
		if !ok {
			t.Errorf("%s has no capability entry", tool)
			continue
		}
		if got != cap {
			t.Errorf("%s requires %q, want %q", tool, got, cap)
		}
	}
}
