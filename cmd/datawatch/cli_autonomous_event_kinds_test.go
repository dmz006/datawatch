// CLI tests for `datawatch autonomous event-kinds`, against an httptest daemon.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func runEventKindsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAutonomousEventKindsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

const eventKindsReply = `{"groups":[
 {"group":"automaton","kinds":[
  {"kind":"automaton.failed","description":"an Automaton failed","wake_default":true},
  {"kind":"automaton.started","description":"an Automaton started","wake_default":false}]},
 {"group":"automation","kinds":[
  {"kind":"automation.started","description":"an Automation started","wake_default":false}]}
]}`

func TestEventKindsCLIPrintsGroupedList(t *testing.T) {
	c := &captureDaemon{replyBody: eventKindsReply}
	withCaptureServer(t, c)

	out, err := runEventKindsCmd(t)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/autonomous/event-kinds" || c.gotQuery != "" {
		t.Errorf("request = %s %s?%s", c.gotMethod, c.gotPath, c.gotQuery)
	}
	for _, want := range []string{
		"automaton:", "automation:",
		"* automaton.failed  an Automaton failed",
		"  automaton.started  an Automaton started",
		"automation.started",
		"* wakes the supervisor by default",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "automaton:") > strings.Index(out, "automation:") {
		t.Errorf("groups must keep the daemon's order:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "prd") {
		t.Errorf("output must not mention PRD:\n%s", out)
	}
}

func TestEventKindsCLIGroupFilter(t *testing.T) {
	c := &captureDaemon{replyBody: eventKindsReply}
	withCaptureServer(t, c)

	out, err := runEventKindsCmd(t, "--group", "AUTOMATION")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "automation.started") {
		t.Errorf("wanted the automation group:\n%s", out)
	}
	if strings.Contains(out, "automaton.failed") || strings.Contains(out, "wakes the supervisor") {
		t.Errorf("other groups leaked through the filter:\n%s", out)
	}
}

func TestEventKindsCLIUnknownGroupListsGroups(t *testing.T) {
	c := &captureDaemon{replyBody: eventKindsReply}
	withCaptureServer(t, c)

	_, err := runEventKindsCmd(t, "--group", "nope")
	if err == nil {
		t.Fatal("want an error for an unknown group")
	}
	for _, want := range []string{`"nope"`, "automaton", "automation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestEventKindsCLIJSON(t *testing.T) {
	c := &captureDaemon{replyBody: eventKindsReply}
	withCaptureServer(t, c)

	out, err := runEventKindsCmd(t, "--json", "--group", "automaton")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, `"wake_default": true`) || !strings.Contains(out, `"automaton.failed"`) {
		t.Errorf("--json output wrong:\n%s", out)
	}
	if strings.Contains(out, "automation.started") {
		t.Errorf("--json ignored --group:\n%s", out)
	}
}

func TestEventKindsCLIEmpty(t *testing.T) {
	c := &captureDaemon{replyBody: `{"groups":[]}`}
	withCaptureServer(t, c)

	out, err := runEventKindsCmd(t)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no event kinds") {
		t.Errorf("empty listing output wrong:\n%s", out)
	}
}

func TestEventKindsCLIDaemonErrorSurfaces(t *testing.T) {
	c := &captureDaemon{replyCode: 403, replyBody: "forbidden"}
	withCaptureServer(t, c)

	if _, err := runEventKindsCmd(t); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want a 403 error, got %v", err)
	}
}

func TestEventKindsCLIRejectsArgs(t *testing.T) {
	c := &captureDaemon{replyBody: eventKindsReply}
	withCaptureServer(t, c)

	if _, err := runEventKindsCmd(t, "extra"); err == nil {
		t.Fatal("want an error for a stray argument")
	}
	if c.gotMethod != "" {
		t.Error("request reached the daemon despite a bad argument")
	}
}
