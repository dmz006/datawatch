// CLI tests for `datawatch dashboard arc` (GET /api/dashboard/arc).

package main

import (
	"net/http"
	"strings"
	"testing"
)

const dashboardArcFixture = `{"generated_at":"2026-11-01T09:00:00Z","available":true,"arc":{"available":true,"id":"a1","name":"Release arc","stages":[{"stage":"bugs","total":4,"done":2,"running":1,"blocked":0,"percent":50},{"stage":"features","total":2,"done":0,"running":0,"blocked":1,"percent":0},{"stage":"final","total":1,"done":0,"running":0,"blocked":0,"percent":0}],"maintenance_queue":{"Total":3,"Done":1,"Running":0,"Blocked":0},"blocked_stage":{"stage":"features","reason":"Login is blocked: waiting on review"}},"lanes":{"available":true,"lanes":[{"kind":"claude","in_flight":1,"cap":2}],"queued_by_priority":[]},"usage":{"available":true,"points":{"used":12.5,"allowance":100,"remaining":87.5,"percent":12.5},"tokens":{"in":1000,"out":500,"total":1500}}}`

func TestDashboardArcCLIHuman(t *testing.T) {
	c := &captureDaemon{replyBody: dashboardArcFixture}
	withCaptureServer(t, c)
	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardArcCmd()
		cmd.SetArgs(nil)
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/dashboard/arc" {
		t.Errorf("request = %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"Release arc", "bugs", "2/4 done (50.0%)", "BLOCKED (features): Login is blocked", "lane claude", "12.5 of 100.0 used", "1500 total"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$") {
		t.Errorf("output must not show dollars:\n%s", out)
	}
}

func TestDashboardArcCLIJSONFlag(t *testing.T) {
	c := &captureDaemon{replyBody: dashboardArcFixture}
	withCaptureServer(t, c)
	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardArcCmd()
		cmd.SetArgs([]string{"--json"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	if !strings.Contains(out, `"name": "Release arc"`) || strings.Contains(out, "Arc Release arc (") {
		t.Errorf("--json must print raw JSON:\n%s", out)
	}
}

func TestDashboardArcCLIRefusal(t *testing.T) {
	c := &captureDaemon{replyCode: http.StatusServiceUnavailable, replyBody: "console disabled"}
	withCaptureServer(t, c)
	_, err := captureStdoutErr(t, func() error {
		cmd := newDashboardArcCmd()
		cmd.SetArgs(nil)
		return cmd.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want 503 error, got %v", err)
	}
}
