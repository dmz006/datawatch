package mcp

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// The structured tool docs list the "done when" tools exactly when the tools are
// registered.
func TestAcceptanceTools_AreInTheToolDocsOnlyInReportMode(t *testing.T) {
	names := []string{"acceptance_view", "acceptance_edit", "acceptance_confirm", "acceptance_accept", "acceptance_report", "acceptance_catalog"}
	for _, on := range []bool{false, true} {
		dir := t.TempDir()
		mgr, err := session.NewManager("testhost", dir, "echo", 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		allowTestTemp(mgr)
		trackSessionManager(t, mgr)
		mgr.WithFakeTmux()
		listed := map[string]int{}
		for _, d := range New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test", AcceptanceTools: on}).ToolDocs() {
			listed[d.Name]++
		}
		want := 0
		if on {
			want = 1
		}
		for _, n := range names {
			if listed[n] != want {
				t.Errorf("on=%v: %s listed %d times", on, n, listed[n])
			}
		}
	}
}
