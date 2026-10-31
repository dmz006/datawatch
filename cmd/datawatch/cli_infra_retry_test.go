package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// infraRetryCLISample is a status with one retried task and one held task.
const infraRetryCLISample = `{
  "enabled": true, "max_tries": 3, "backoff_seconds": [60, 300, 540],
  "categories": ["spawn_error", "storage_write", "session_killed_before_delivery", "daemon_restart"],
  "exhausted_action": "hold", "retry_dependents": true, "raise_question": true,
  "tasks": [
    {"automaton_id": "p1", "automaton_title": "Add paging", "task_id": "t1", "task_title": "Write handler",
     "status": "pending", "retry_count": 1, "max_tries": 3,
     "retries": [{"number": 1, "class": "spawn_error", "reason": "tmux refused the session",
                  "at": "2026-10-30T09:15:00Z", "delay_seconds": 60, "next_at": "2026-10-30T09:16:00Z"}],
     "held": false},
    {"automaton_id": "p2", "automaton_title": "Fix login", "task_id": "t7", "task_title": "Add test",
     "status": "review_needed", "retry_count": 3, "max_tries": 3,
     "retries": [{"number": 3, "class": "storage_write", "reason": "disk full",
                  "at": "2026-10-30T09:20:00Z", "delay_seconds": 540, "next_at": "2026-10-30T09:29:00Z"}],
     "held": true, "held_cause": "storage_write: disk full"}
  ],
  "count": 2
}`

func TestInfraRetryCLI_TextOutput(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, infraRetryCLISample)
	out, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/infra-retries" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	want := []string{
		"Infrastructure retry: on, 3 tries, back-off 60s/300s/540s, hold when used up\n",
		`Automaton p1 "Add paging", task t1 "Write handler": pending, retry 1 of 3` + "\n",
		"  retry 1: spawn_error: tmux refused the session (2026-10-30 09:15:00)\n",
		`Automaton p2 "Fix login", task t7 "Add test": review_needed, retry 3 of 3` + "\n",
		"  held after infrastructure retries: storage_write: disk full\n",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func TestInfraRetryCLI_EmptyStatus(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"enabled": true, "max_tries": 3, "backoff_seconds": [60], "categories": [], "exhausted_action": "hold", "tasks": [], "count": 0}`)
	out, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No infrastructure retries yet.\n") {
		t.Errorf("output = %q", out)
	}
}

func TestInfraRetryCLI_JSONPassesBodyUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, infraRetryCLISample)
	out, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != infraRetryCLISample {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, infraRetryCLISample)
	}
}

func TestInfraRetryCLI_AutomatonAndLimitAreSentAsQuery(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, infraRetryCLISample)
	if _, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"p2"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "automaton=p2" {
		t.Errorf("query = %q, want automaton=p2", d.query)
	}
	if _, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"p2", "--limit", "7"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "automaton=p2&limit=7" {
		t.Errorf("query = %q, want automaton=p2&limit=7", d.query)
	}
	if _, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"--limit", "1000"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "limit=1000" {
		t.Errorf("query = %q, want limit=1000", d.query)
	}
}

func TestInfraRetryCLI_LimitRefusedLocally(t *testing.T) {
	for _, bad := range []string{"0", "1001", "-3"} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, infraRetryCLISample)
		_, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"--limit", bad})
		if err == nil || !strings.Contains(err.Error(), "from 1 to 1000") {
			t.Errorf("--limit %s: expected a clear error, got %v", bad, err)
		}
		if d.calls != 0 {
			t.Errorf("--limit %s: nothing may be sent, saw %d calls", bad, d.calls)
		}
	}
}

func TestInfraRetryCLI_TooManyArgumentsRefused(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, infraRetryCLISample)
	if _, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), []string{"p1", "p2"}); err == nil {
		t.Error("two Automaton ids must be refused")
	}
	if d.calls != 0 {
		t.Errorf("nothing may be sent, saw %d calls", d.calls)
	}
}

func TestInfraRetryCLI_ErrorPassthrough(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "bad request: limit must be a whole number from 1 to 1000")
	_, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "limit must be a whole number") {
		t.Errorf("expected the daemon refusal to pass through, got %v", err)
	}
}

func TestInfraRetryCLI_BadBodyIsAnError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, "not json")
	_, err := runLaneFillCmd(t, newAutonomousInfraRetriesCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "infrastructure retry status") {
		t.Errorf("expected a parse error, got %v", err)
	}
}

func TestInfraRetryCLI_RegisteredAndListedInHelp(t *testing.T) {
	root := newAutonomousCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "infra-retries" {
			found = true
		}
	}
	if !found {
		t.Error("infra-retries is not registered under autonomous")
	}
	if !strings.Contains(root.Long, "infra-retries [automaton-id] [--limit N] [--json]") {
		t.Error("the autonomous help does not list infra-retries")
	}
}

// infraRetryConfigDaemon is a fake daemon for PUT /api/config that refuses a
// body that is not valid JSON, as the real one does, and keeps the keys of
// every accepted body.
type infraRetryConfigDaemon struct {
	config   map[string]any
	accepted int
}

func startInfraRetryConfigDaemon(t *testing.T) *infraRetryConfigDaemon {
	t.Helper()
	d := &infraRetryConfigDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		d.accepted++
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

// TestInfraRetryCLI_ConfigSetKeys: datawatch config set sends each of the
// seven autonomous.infra_retry keys to the daemon with the right JSON type.
// The command has no key list of its own, so a key reaches the daemon as typed.
func TestInfraRetryCLI_ConfigSetKeys(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.infra_retry.enabled", "false", false},
		{"autonomous.infra_retry.max_tries", "5", float64(5)},
		{"autonomous.infra_retry.backoff_seconds", "[30,60]", []any{float64(30), float64(60)}},
		{"autonomous.infra_retry.categories", `["spawn_error","daemon_restart"]`, []any{"spawn_error", "daemon_restart"}},
		{"autonomous.infra_retry.exhausted_action", "count", "count"},
		{"autonomous.infra_retry.retry_dependents", "false", false},
		{"autonomous.infra_retry.raise_question", "true", true},
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
		if got := d.config[c.key]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.want)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value) {
			t.Errorf("%s: output = %q", c.key, out)
		}
	}
}

// TestInfraRetryCLI_ConfigSetCommaList: a comma list is not valid JSON, so it
// goes on as text, which the daemon accepts for the two list keys.
func TestInfraRetryCLI_ConfigSetCommaList(t *testing.T) {
	d := startInfraRetryConfigDaemon(t)
	if _, err := captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, "autonomous.infra_retry.backoff_seconds", "60,300,540")
	}); err != nil {
		t.Fatal(err)
	}
	if got := d.config["autonomous.infra_retry.backoff_seconds"]; got != "60,300,540" {
		t.Errorf("stored %#v, want the comma list as text", got)
	}
}

// TestInfraRetryCLI_ConfigSetRefusalIsShown: a value the daemon refuses is
// reported with the daemon's own text.
func TestInfraRetryCLI_ConfigSetRefusalIsShown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "autonomous.infra_retry.max_tries: 0 is invalid; use a whole number from 1 to 1000", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	_, err := captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, "autonomous.infra_retry.max_tries", "0")
	})
	if err == nil || !strings.Contains(err.Error(), "whole number from 1 to 1000") {
		t.Errorf("expected the daemon refusal, got %v", err)
	}
}
