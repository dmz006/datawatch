package deploy

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// The status text the CLI and the chat verb share says whether merge run and
// deploy run requests are on, and names the setting. A reply from a daemon
// that does not report it gets no line.
func TestRenderStatusText_ShowsTheRunSwitch(t *testing.T) {
	for body, want := range map[string]string{
		`{"settings":{},"run_enabled":false}`: "Merge run and deploy run requests: off (" + config.MergeDeployRunKey + " is false).",
		`{"settings":{},"run_enabled":true}`:  "Merge run and deploy run requests: on (" + config.MergeDeployRunKey + " is true).",
	} {
		v, err := ParseStatusView([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderStatusText(v); !strings.Contains(got, want+"\n") {
			t.Errorf("%s: text %q lacks %q", body, got, want)
		}
	}
	v, err := ParseStatusView([]byte(`{"settings":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderStatusText(v); strings.Contains(got, "run requests") {
		t.Errorf("an unreported switch must add no line: %q", got)
	}
}
