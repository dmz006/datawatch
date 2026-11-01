package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// requestWatchCLISample holds one generating, one idle and one unreachable
// observation, as GET /api/autonomous/request-watch returns them.
const requestWatchCLISample = `{
  "enabled": true,
  "poll_seconds": 30,
  "no_progress_minutes": 30,
  "observations": [
    {"session_id": "s-gen", "automaton_id": "abcd1234", "task_id": "t1", "role": "worker", "node": "node-a",
     "model": "m1", "state": "generating", "last_change": "2026-11-01T09:00:00Z", "no_progress_seconds": 0,
     "last_poll": "2026-11-01T09:00:30Z", "over_threshold": false,
     "summary": "waiting on a model request: generating"},
    {"session_id": "s-idle", "automaton_id": "abcd1234", "task_id": "t2", "role": "verifier", "node": "node-b",
     "model": "m2", "state": "idle", "last_change": "2026-11-01T08:00:00Z", "no_progress_seconds": 1900,
     "last_poll": "2026-11-01T08:31:40Z", "over_threshold": true,
     "summary": "waiting on a model request: no progress for 31 minutes"},
    {"session_id": "s-down", "automaton_id": "ef567890", "task_id": "t3", "role": "worker", "node": "node-c",
     "model": "m3", "state": "unreachable", "last_change": "2026-11-01T08:00:00Z", "no_progress_seconds": 600,
     "last_poll": "2026-11-01T08:10:00Z", "over_threshold": false, "error": "connection refused",
     "summary": "waiting on a model request: node unreachable, no progress for 10 minutes"}
  ]
}`

func TestRequestWatchCLIPlainOutput(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, requestWatchCLISample)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/request-watch" || d.query != "" {
		t.Errorf("path=%s query=%q; want the request-watch route with no filter", d.path, d.query)
	}
	want := "s-gen worker task t1 on node-a: waiting on a model request: generating\n" +
		"s-idle verifier task t2 on node-b: waiting on a model request: no progress for 31 minutes\n" +
		"s-down worker task t3 on node-c: waiting on a model request: node unreachable, no progress for 10 minutes\n"
	if out != want {
		t.Errorf("plain output differs\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestRequestWatchCLIGeneratingLine(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"observations":[{"session_id":"s1","role":"worker","task_id":"t9","node":"n1","state":"generating","summary":"waiting on a model request: generating"}]}`)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "s1 worker task t9 on n1: waiting on a model request: generating\n" {
		t.Errorf("generating line = %q", out)
	}
}

func TestRequestWatchCLIIdleLine(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"observations":[{"session_id":"s2","role":"verifier","task_id":"t4","node":"n2","state":"idle","no_progress_seconds":2400,"summary":"waiting on a model request: no progress for 40 minutes"}]}`)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "s2 verifier task t4 on n2: waiting on a model request: no progress for 40 minutes\n" {
		t.Errorf("idle line = %q", out)
	}
}

func TestRequestWatchCLIUnreachableLine(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"observations":[{"session_id":"s3","role":"worker","task_id":"t5","node":"n3","state":"unreachable","no_progress_seconds":300,"summary":"waiting on a model request: node unreachable, no progress for 5 minutes"}]}`)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "s3 worker task t5 on n3: waiting on a model request: node unreachable, no progress for 5 minutes\n" {
		t.Errorf("unreachable line = %q", out)
	}
}

func TestRequestWatchCLIEmptySentence(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"enabled":true,"poll_seconds":30,"no_progress_minutes":30,"observations":[]}`)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "No model requests are being watched.\n" {
		t.Errorf("empty case = %q; want the plain sentence", out)
	}
	if strings.Contains(out, "PRD") {
		t.Errorf("output must say Automaton, not PRD: %q", out)
	}
}

func TestRequestWatchCLIJson(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, requestWatchCLISample)
	out, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != requestWatchCLISample {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, requestWatchCLISample)
	}
}

func TestRequestWatchCLIFilters(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"observations":[]}`)
	if _, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), []string{"--session", "s1"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "session=s1" {
		t.Errorf("--session query = %q", d.query)
	}
	if _, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), []string{"--automaton", "abcd1234"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "automaton=abcd1234" {
		t.Errorf("--automaton query = %q", d.query)
	}
	if _, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), []string{"--session", "s1", "--automaton", "abcd1234", "--json"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "automaton=abcd1234&session=s1" {
		t.Errorf("both filters query = %q", d.query)
	}
}

func TestRequestWatchCLIDaemonErrorIsShown(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusServiceUnavailable, "request watch unavailable")
	_, err := runLaneFillCmd(t, newAutonomousRequestWatchCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "request watch unavailable") {
		t.Errorf("expected the daemon's reason, got %v", err)
	}
}

func TestRequestWatchCLIRegisteredUnderAutonomous(t *testing.T) {
	root := newAutonomousCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "request-watch" {
			found = true
		}
	}
	if !found {
		t.Error("request-watch is not registered under autonomous")
	}
	if !strings.Contains(root.Long, "request-watch [--session ID] [--automaton ID] [--json]") {
		t.Error("the autonomous help does not list request-watch")
	}
}

// TestRequestWatchCLIConfigSetKey: datawatch config set writes an
// autonomous.request_watch key through PUT /api/config with the right JSON
// type; the command has no key list of its own.
func TestRequestWatchCLIConfigSetKey(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.request_watch.no_progress_minutes", "45", float64(45)},
		{"autonomous.request_watch.raise_question", "false", false},
	}
	for _, c := range cases {
		d := startInfraRetryConfigDaemon(t)
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err != nil {
			t.Errorf("%s: %v", c.key, err)
			continue
		}
		if got := d.config[c.key]; got != c.want {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.want)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value) {
			t.Errorf("%s: output = %q", c.key, out)
		}
	}
}
