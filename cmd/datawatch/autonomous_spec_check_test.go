package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const cliSpecCheckDirty = `{"id":"p1","spec_check":[{"rule":"names_tracker_id","phrase":"BL123","severity":"warning","correctable":true,"suggestion":"remove 'BL123'"}]}`
const cliSpecCheckClean = `{"id":"p1","spec_check":[]}`

func specCheckCLIRun(t *testing.T, build func() *cobra.Command, args []string, reply string) string {
	t.Helper()
	c := &captureDaemon{replyCode: http.StatusOK, replyBody: reply}
	withCaptureServer(t, c)
	var runErr error
	out := captureStdout(t, func() { runErr = runCmd(t, build(), args...) })
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return out
}

func TestSpecCheckCLICreateAndReviseShowFindings(t *testing.T) {
	cases := map[string]struct {
		build func() *cobra.Command
		args  []string
	}{
		"create": {newAutonomousPRDCreateCmd, []string{"Fix BL123"}},
		"revise": {newAutonomousPRDRequestRevisionCmd, []string{"p1"}},
	}
	for name, tc := range cases {
		out := specCheckCLIRun(t, tc.build, tc.args, cliSpecCheckDirty)
		idAt := strings.Index(out, `"id"`)
		lineAt := strings.Index(out, "spec check: warning [names_tracker_id] \"BL123\"")
		if idAt < 0 || lineAt < 0 || lineAt < idAt {
			t.Fatalf("%s: findings must print after the id, out=%q", name, out)
		}
		if !strings.Contains(out, "remove 'BL123'") {
			t.Fatalf("%s: suggestion missing: %q", name, out)
		}
		if n := strings.Count(out, "spec check: "); n != 1 {
			t.Fatalf("%s: want one line per finding, got %d in %q", name, n, out)
		}
	}
}

func TestSpecCheckCLICleanSpecShowsNone(t *testing.T) {
	out := specCheckCLIRun(t, newAutonomousPRDCreateCmd, []string{"Add a retry"}, cliSpecCheckClean)
	if strings.Contains(out, "spec check:") {
		t.Fatalf("clean spec printed findings: %q", out)
	}
	out = specCheckCLIRun(t, newAutonomousPRDRequestRevisionCmd, []string{"p1"}, cliSpecCheckClean)
	if strings.Contains(out, "spec check:") {
		t.Fatalf("clean revise printed findings: %q", out)
	}
}
