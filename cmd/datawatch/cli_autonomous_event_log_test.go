// CLI tests for `datawatch autonomous event-log`, against an httptest daemon.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func runEventLogCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAutonomousEventLogCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

const eventLogReply = `{"events":[
 {"id":"a","time":"2026-11-09T10:00:00Z","kind":"automaton.failed","project":"datawatch","automaton":"auto-1","automation":"","summary":"verify failed","detail":"","reason":"","next_action":"open the failed task","quiet":false,"severity":"error"},
 {"id":"b","time":"2026-11-09T09:00:00Z","kind":"automation.started","project":"","automaton":"","automation":"flow-9","summary":"flow started","detail":"","reason":"","next_action":"","quiet":false,"severity":"info"}
],"next_cursor":"CUR1","total":7}`

func TestCLIAutonomousEventLogPrintsReadableList(t *testing.T) {
	c := &captureDaemon{replyBody: eventLogReply}
	withCaptureServer(t, c)

	out, err := runEventLogCmd(t)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/autonomous/event-log" {
		t.Errorf("request = %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{
		"2026-11-09T10:00:00Z", "automaton.failed", "project=datawatch",
		"automaton=auto-1", "verify failed", "next: open the failed task",
		"automation.started", "automaton=flow-9", "flow started",
		"total: 7", "next cursor: CUR1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "next:") != 1 {
		t.Errorf("only the event with a next action should print one:\n%s", out)
	}
	if strings.Contains(out, "PRD") || strings.Contains(strings.ToLower(out), "prd") {
		t.Errorf("output must not mention PRD:\n%s", out)
	}
}

func TestCLIAutonomousEventLogBuildsQuery(t *testing.T) {
	c := &captureDaemon{replyBody: `{"events":[],"next_cursor":"","total":0}`}
	withCaptureServer(t, c)

	out, err := runEventLogCmd(t,
		"--kind", "automaton.failed", "--kind", "automation.started,automaton.blocked",
		"--project", "datawatch", "--q", "verify", "--limit", "5", "--before", "CUR0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{
		"kind=automaton.failed%2Cautomation.started%2Cautomaton.blocked",
		"project=datawatch", "q=verify", "limit=5", "before=CUR0",
	} {
		if !strings.Contains(c.gotQuery, want) {
			t.Errorf("query %q lacks %q", c.gotQuery, want)
		}
	}
	if !strings.Contains(out, "no events") || strings.Contains(out, "next cursor") {
		t.Errorf("empty page output wrong:\n%s", out)
	}
}

func TestCLIAutonomousEventLogDefaultQueryIsEmpty(t *testing.T) {
	c := &captureDaemon{replyBody: `{"events":[],"next_cursor":"","total":0}`}
	withCaptureServer(t, c)
	if _, err := runEventLogCmd(t, "--project", "  "); err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.gotQuery != "" {
		t.Errorf("blank project should send no query, got %q", c.gotQuery)
	}
}

func TestCLIAutonomousEventLogJSONPrintsRawBody(t *testing.T) {
	c := &captureDaemon{replyBody: eventLogReply}
	withCaptureServer(t, c)
	out, err := runEventLogCmd(t, "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, `"next_cursor"`) || !strings.Contains(out, `"automaton.failed"`) {
		t.Errorf("--json output is not the raw body:\n%s", out)
	}
}

func TestCLIAutonomousEventLogRejectsNegativeLimit(t *testing.T) {
	c := &captureDaemon{}
	withCaptureServer(t, c)
	if _, err := runEventLogCmd(t, "--limit", "-1"); err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Fatalf("want a --limit error, got %v", err)
	}
	if c.gotMethod != "" {
		t.Error("request reached the daemon with a negative limit")
	}
}

func TestCLIAutonomousEventLogDaemonErrorSurfaces(t *testing.T) {
	c := &captureDaemon{replyCode: 400, replyBody: "bad request: unknown kind \"nope\""}
	withCaptureServer(t, c)
	if _, err := runEventLogCmd(t, "--kind", "nope"); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want a 400 error, got %v", err)
	}
}

func TestCLIAutonomousEventLogRegistered(t *testing.T) {
	root := newAutonomousCmd()
	for _, sub := range root.Commands() {
		if sub.Name() == "event-log" {
			return
		}
	}
	t.Fatal("event-log is not registered under autonomous")
}

func TestCLIAutonomousEventLogRenderBadBody(t *testing.T) {
	var out bytes.Buffer
	if err := renderAutomataEventLog(&out, []byte("not json")); err == nil {
		t.Fatal("want an unexpected-response error")
	}
}
