// CLI tests for `datawatch dashboard console` (GET /api/dashboard/console)
// and `datawatch dashboard console set <key> <value>` (PUT /api/config),
// served by an httptest daemon via the captureDaemon pattern.

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const dashboardConsoleFixture = `{
	"generated_at": "2026-11-01T09:00:00Z",
	"sections": {
		"questions_waiting": {"available": true, "count": 2, "summary": "2 sessions waiting on questions", "link": "/console?section=questions"},
		"held_blocked": {"available": true, "count": 0, "summary": "no held or blocked sessions", "link": "/console?section=held"},
		"lanes": {"available": false, "count": 0, "summary": "lane fill is off", "link": "/lanes"},
		"usage": {"available": true, "count": 5, "summary": "$12.50 spent today", "link": "/usage"},
		"ci_release": {"available": true, "count": 1, "summary": "1 CI red on main", "link": "/ci-release"}
	}
}`

func TestDashboardConsoleCLIHUMAN(t *testing.T) {
	c := &captureDaemon{replyBody: dashboardConsoleFixture}
	withCaptureServer(t, c)

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs(nil)
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/dashboard/console" {
		t.Errorf("request = %s %s, want GET /api/dashboard/console", c.gotMethod, c.gotPath)
	}
	if !strings.Contains(out, "Operator console (2026-11-01T09:00:00Z)") {
		t.Errorf("header missing:\n%s", out)
	}
	// Five sections, in the endpoint's order, available ones with a count,
	// the switched-off ones with their daemon explanation.
	for _, want := range []string{
		"questions_waiting",
		"2 sessions waiting on questions",
		"held_blocked",
		"no held or blocked sessions",
		"lanes",
		"off",
		"lane fill is off",
		"usage",
		"$12.50 spent today",
		"ci_release",
		"1 CI red on main",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if gi, hi, li, ui, ci := strings.Index(out, "questions_waiting"), strings.Index(out, "held_blocked"), strings.Index(out, "lanes"), strings.Index(out, "usage"), strings.Index(out, "ci_release"); gi > hi || hi > li || li > ui || ui > ci {
		t.Errorf("sections out of order:\n%s", out)
	}
}

func TestDashboardConsoleCLIJSONFlag(t *testing.T) {
	c := &captureDaemon{replyBody: dashboardConsoleFixture}
	withCaptureServer(t, c)

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs([]string{"--json"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	// --json prints the raw (pretty) endpoint JSON, not the rendered view.
	for _, want := range []string{`"generated_at": "2026-11-01T09:00:00Z"`, `"questions_waiting"`, `"link": "/ci-release"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Operator console (") {
		t.Errorf("--json must print raw JSON, got rendered view:\n%s", out)
	}
}

func TestDashboardConsoleCLIDisabledSurfacesRefusal(t *testing.T) {
	c := &captureDaemon{
		replyCode: http.StatusServiceUnavailable,
		replyBody: "the operator console is disabled — set dashboard.console.enabled to true",
	}
	withCaptureServer(t, c)

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs(nil)
		return cmd.Execute()
	})
	if err == nil {
		t.Fatalf("want a 503 error, out=%q", out)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error lacks 503: %v", err)
	}
}

func TestDashboardConsoleCLISetPutsConsoleKey(t *testing.T) {
	c := &captureDaemon{replyBody: `{}`}
	withCaptureServer(t, c)

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs([]string{"set", "dashboard.console.enabled", "false"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	if c.gotMethod != http.MethodPut || c.gotPath != "/api/config" {
		t.Errorf("request = %s %s, want PUT /api/config", c.gotMethod, c.gotPath)
	}
	if !strings.Contains(string(c.gotRaw), `"dashboard.console.enabled": false`) {
		t.Errorf("body = %s", c.gotRaw)
	}
	if !strings.Contains(out, "Set dashboard.console.enabled = false") {
		t.Errorf("confirmation missing: %q", out)
	}
}

// The daemon rejects a value in its raw type (e.g. "true" for a key it
// parses another way): runConfigSet retries quoting the value, and the
// quoted body is what the daemon accepted.
func TestDashboardConsoleCLISetQuotedFallback(t *testing.T) {
	var calls [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, raw)
		if len(calls) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`"must be a string"`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs([]string{"set", "dashboard.console.show_lanes", "true"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute: %v (out %q)", err, out)
	}
	if len(calls) != 2 {
		t.Fatalf("want raw then quoted attempt, got %d call(s): %v", len(calls), calls)
	}
	if !strings.Contains(string(calls[0]), `"dashboard.console.show_lanes": true`) {
		t.Errorf("raw attempt body = %s", calls[0])
	}
	if !strings.Contains(string(calls[1]), `"dashboard.console.show_lanes": "true"`) {
		t.Errorf("quoted attempt body = %s", calls[1])
	}
}

func TestDashboardConsoleCLISetRequiresKeyAndValue(t *testing.T) {
	c := &captureDaemon{}
	withCaptureServer(t, c)

	out, err := captureStdoutErr(t, func() error {
		cmd := newDashboardConsoleCmd()
		cmd.SetArgs([]string{"set", "dashboard.console.enabled"})
		return cmd.Execute()
	})
	if err == nil {
		t.Fatalf("want a usage error, out=%q", out)
	}
	if !strings.Contains(err.Error(), "set <key> <value>") {
		t.Errorf("usage error = %v", err)
	}
	if c.gotMethod != "" {
		t.Errorf("request reached the daemon: %s %s", c.gotMethod, c.gotPath)
	}
}
