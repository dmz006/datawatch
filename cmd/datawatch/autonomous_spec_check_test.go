package main

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const cliSpecCheckDirty = `{"id":"p1","spec_check":[{"rule":"names_tracker_id","phrase":"BL123","severity":"warning","correctable":true,"suggestion":"remove 'BL123'"}]}`
const cliSpecCheckClean = `{"id":"p1","spec_check":[]}`

// specCheckCLIRun runs the command and returns what it wrote to standard
// output (the JSON reply) and to standard error (the spec-check notes).
func specCheckCLIRun(t *testing.T, build func() *cobra.Command, args []string, reply string) (stdout, stderr string) {
	t.Helper()
	c := &captureDaemon{replyCode: http.StatusOK, replyBody: reply}
	withCaptureServer(t, c)
	var runErr error
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	stdout = captureStdout(t, func() { runErr = runCmd(t, build(), args...) })
	_ = w.Close()
	os.Stderr = oldErr
	data, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return stdout, string(data)
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
		out, notes := specCheckCLIRun(t, tc.build, tc.args, cliSpecCheckDirty)
		if !strings.Contains(out, `"id"`) {
			t.Fatalf("%s: the JSON reply must stay on standard output, out=%q", name, out)
		}
		if strings.Contains(out, "spec check: ") {
			t.Fatalf("%s: findings are notes and belong on standard error, out=%q", name, out)
		}
		if !strings.Contains(notes, "spec check: warning [names_tracker_id] \"BL123\"") {
			t.Fatalf("%s: findings missing on standard error: %q", name, notes)
		}
		if !strings.Contains(notes, "remove 'BL123'") {
			t.Fatalf("%s: suggestion missing: %q", name, notes)
		}
		if n := strings.Count(notes, "spec check: "); n != 1 {
			t.Fatalf("%s: want one line per finding, got %d in %q", name, n, notes)
		}
	}
}

func TestSpecCheckCLICleanSpecShowsNone(t *testing.T) {
	out, notes := specCheckCLIRun(t, newAutonomousPRDCreateCmd, []string{"Add a retry"}, cliSpecCheckClean)
	if strings.Contains(out+notes, "spec check:") {
		t.Fatalf("clean spec printed findings: %q %q", out, notes)
	}
	out, notes = specCheckCLIRun(t, newAutonomousPRDRequestRevisionCmd, []string{"p1"}, cliSpecCheckClean)
	if strings.Contains(out+notes, "spec check:") {
		t.Fatalf("clean revise printed findings: %q %q", out, notes)
	}
}
