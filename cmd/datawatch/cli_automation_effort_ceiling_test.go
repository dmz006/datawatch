// CLI coverage for `datawatch automation set-effort-ceiling`. The daemon side
// is the in-process capture server (captureDaemon).
package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestAutomationSetEffortCeilingCLI(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"nightly","status":"active","version":9,"members":[]}`}
	withCaptureServer(t, c)

	run := func(args ...string) error {
		var err error
		captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), args...) })
		return err
	}

	if err := run("set-effort-ceiling", "ab12", "--expected-version", "8", "docs=low", "final_gate=cheaper_backend"); err != nil {
		t.Fatal(err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/ab12/set_effort_ceiling" {
		t.Fatalf("proxied %s %s", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	want := map[string]any{"docs": "low", "final_gate": "cheaper_backend"}
	if !reflect.DeepEqual(body["effort_ceiling"], want) || body["expected_version"] != float64(8) {
		t.Errorf("body = %v", body)
	}

	if err := run("set-effort-ceiling", "ab12", "--expected-version", "9", "--clear"); err != nil {
		t.Fatal(err)
	}
	if m, _ := c.body(t)["effort_ceiling"].(map[string]any); m == nil || len(m) != 0 {
		t.Errorf("clear sent %v, want an empty map", c.body(t)["effort_ceiling"])
	}

	// A malformed pair never reaches the daemon.
	c.gotPath = ""
	err := run("set-effort-ceiling", "ab12", "--expected-version", "9", "docs")
	if err == nil || !strings.Contains(err.Error(), "kind=ceiling") || c.gotPath != "" {
		t.Errorf("malformed pair: err=%v path=%q", err, c.gotPath)
	}
	err = run("set-effort-ceiling", "ab12", "--expected-version", "9", "--clear", "docs=low")
	if err == nil || c.gotPath != "" {
		t.Errorf("--clear with pairs: err=%v path=%q", err, c.gotPath)
	}
}
