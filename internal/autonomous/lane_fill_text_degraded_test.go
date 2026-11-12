package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestRenderLaneFillTextDegradedSection(t *testing.T) {
	st := LaneFillStatus{Mode: "shadow"}
	if out := RenderLaneFillText(st, 0); strings.Contains(out, "Degraded hosts") {
		t.Fatalf("no degraded hosts must print no section, got:\n%s", out)
	}
	st.Evidence.Degraded = []LaneFillDegradedHost{{Host: "node-a", Error: "connection refused", Since: time.Now().Add(-12 * time.Minute)}}
	out := RenderLaneFillText(st, 0)
	for _, want := range []string{"Degraded hosts (not blocking the lanes):", "node-a: connection refused (unreachable for 12 min)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Pool evidence error") {
		t.Fatalf("a degraded host is not a blocking error:\n%s", out)
	}
	st.Evidence.Error = "pool unreadable"
	if out := RenderLaneFillText(st, 0); !strings.Contains(out, "Pool evidence error: pool unreadable") {
		t.Fatalf("the blocking line must stay:\n%s", out)
	}
}
