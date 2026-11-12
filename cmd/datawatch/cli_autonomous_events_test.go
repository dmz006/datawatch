// CLI tests for `datawatch autonomous events`, against an httptest daemon.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func runEventsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAutonomousEventsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestPendingEventsCLIPrintsEventsAndNextCursor(t *testing.T) {
	c := &captureDaemon{replyBody: `{"events":[{"id":"e1","at":"2026-01-02T03:04:05Z","kind":"prd.blocked","prd_id":"prd_a","task_id":"","new_status":"blocked"}],"next_cursor":12,"dropped":false}`}
	withCaptureServer(t, c)

	out, err := runEventsCmd(t, "--queue", "ops", "--since", "7", "--limit", "5")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/autonomous/events" {
		t.Errorf("request = %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"queue=ops", "since=7", "limit=5"} {
		if !strings.Contains(c.gotQuery, want) {
			t.Errorf("query %q lacks %q", c.gotQuery, want)
		}
	}
	for _, want := range []string{"prd.blocked", "prd=prd_a", "status=blocked", "next cursor: 12"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dropped") {
		t.Errorf("unexpected dropped warning:\n%s", out)
	}
}

func TestPendingEventsCLIEmptyAndDropped(t *testing.T) {
	c := &captureDaemon{replyBody: `{"events":[],"next_cursor":3,"dropped":true}`}
	withCaptureServer(t, c)
	out, err := runEventsCmd(t, "--queue", "ops")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"no new events", "dropped", "next cursor: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(c.gotQuery, "since=0") || strings.Contains(c.gotQuery, "limit") {
		t.Errorf("default query = %q", c.gotQuery)
	}
}

func TestPendingEventsCLIRequiresQueue(t *testing.T) {
	c := &captureDaemon{}
	withCaptureServer(t, c)
	if _, err := runEventsCmd(t); err == nil || !strings.Contains(err.Error(), "--queue") {
		t.Fatalf("want a --queue error, got %v", err)
	}
	if c.gotMethod != "" {
		t.Error("request reached the daemon without a queue")
	}
}

func TestPendingEventsCLIDaemonErrorSurfaces(t *testing.T) {
	c := &captureDaemon{replyCode: 503, replyBody: "pending events queue disabled"}
	withCaptureServer(t, c)
	if _, err := runEventsCmd(t, "--queue", "ops"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want a 503 error, got %v", err)
	}
}

func TestPendingEventsCLIRegisteredAndSinkAccepted(t *testing.T) {
	found := false
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "events" {
			found = true
		}
	}
	if !found {
		t.Error("events is not registered under autonomous")
	}
	if !lifecycleSubscriptionSinkTypes["mcp_queue"] {
		t.Error("subscribe does not accept the mcp_queue sink")
	}
	for _, sink := range []string{"webhook", "peer"} {
		if !lifecycleSubscriptionSinkTypes[sink] {
			t.Errorf("subscribe does not accept the %s sink", sink)
		}
	}
	if lifecycleSubscriptionSinkTypes["kafka"] {
		t.Error("an unknown sink must stay rejected")
	}
}
