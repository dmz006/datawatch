// BL427 (BL430 Phase 5) — CLI flag→request mapping tests for the
// lifecycle-event subscription commands. Each test serves the daemon side
// with an httptest server overriding the package-level serverURL (same
// pattern as llm_update_body_test.go).
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureDaemon records one request and replies with the given body.
type captureDaemon struct {
	gotMethod string
	gotPath   string
	gotQuery  string
	gotRaw    []byte
	replyCode int
	replyBody string
}

func (c *captureDaemon) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.gotMethod = r.Method
		c.gotPath = r.URL.Path
		c.gotQuery = r.URL.RawQuery
		c.gotRaw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if c.replyCode != 0 {
			w.WriteHeader(c.replyCode)
		}
		if c.replyBody != "" {
			_, _ = w.Write([]byte(c.replyBody))
		}
	}
}

func (c *captureDaemon) body(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.gotRaw, &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", c.gotRaw, err)
	}
	return m
}

// withCaptureServer points the CLI at an httptest daemon and returns the
// capture.
func withCaptureServer(t *testing.T, c *captureDaemon) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(c.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

// TestAutonomousSubscribe_WatchAndInjectShorthand covers the documented
// shorthand: --task + --on session_started + --send expands to kind
// task.session_started, task scope, a send_input action into a session
// sink, with one-shot on by default.
func TestAutonomousSubscribe_WatchAndInjectShorthand(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-cli-1"}`}
	withCaptureServer(t, c)

	cmd := newAutonomousSubscribeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--task", "task-7",
		"--on", "session_started",
		"--send", "Task task-7 is running — pick up the build.",
		"--sink", "session",
		"--target", "pi-host",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/subscriptions" {
		t.Fatalf("route = %s %s, want POST /api/autonomous/subscriptions", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	filter, _ := body["filter"].(map[string]any)
	if filter == nil {
		t.Fatalf("body missing filter: %s", c.gotRaw)
	}
	kinds := stringSliceOf(t, filter, "kinds")
	if len(kinds) != 1 || kinds[0] != "task.session_started" {
		t.Errorf("filter.kinds = %v, want [task.session_started]", kinds)
	}
	if filter["task_id"] != "task-7" {
		t.Errorf("filter.task_id = %v, want task-7", filter["task_id"])
	}
	sink, _ := body["sink"].(map[string]any)
	if sink == nil || sink["type"] != "session" || sink["target"] != "pi-host" {
		t.Errorf("sink = %v, want {session pi-host}", body["sink"])
	}
	action, _ := body["action"].(map[string]any)
	if action == nil || action["type"] != "send_input" || action["text"] != "Task task-7 is running — pick up the build." {
		t.Errorf("action = %v, want send_input with the --send text", body["action"])
	}
	if oneShot, _ := body["one_shot"].(bool); !oneShot {
		t.Errorf("one_shot should default to true when --task is given, body: %s", c.gotRaw)
	}
}

// TestAutonomousSubscribe_FullKindsAndCommSink covers full-kind passthrough,
// comm sink routing, coalesce and the expires mapping.
func TestAutonomousSubscribe_FullKindsAndCommSink(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-cli-2"}`}
	withCaptureServer(t, c)

	cmd := newAutonomousSubscribeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--on", "prd.blocked",
		"--on", "task.completed",
		"--prd", "prd-9",
		"--sink", "comm",
		"--target", "my-router",
		"--coalesce",
		"--expires", "3600",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	body := c.body(t)
	filter := mapOf(t, body, "filter")
	kinds := stringSliceOf(t, filter, "kinds")
	if len(kinds) != 2 || kinds[0] != "prd.blocked" || kinds[1] != "task.completed" {
		t.Errorf("filter.kinds = %v", kinds)
	}
	if filter["prd_id"] != "prd-9" {
		t.Errorf("filter.prd_id = %v, want prd-9", filter["prd_id"])
	}
	sink := mapOf(t, body, "sink")
	if sink["type"] != "comm" || sink["target"] != "my-router" {
		t.Errorf("sink = %v", sink)
	}
	if coalesce, _ := body["coalesce"].(bool); !coalesce {
		t.Errorf("coalesce should be true")
	}
	if exp, _ := body["expires_at"].(string); exp == "" {
		t.Errorf("expires_at should be set from --expires; body: %s", c.gotRaw)
	}
}

// TestAutonomousSubscribe_WarningKindsStayBare pins the new opt-in
// warning kinds as bare lifecycle kinds: --on scope_warning and
// --on guardrail_warn must pass through themselves, never expand to
// task.scope_warning / task.guardrail_warn (they are warnings, not task
// statuses), and be accepted where task.* statuses are.
func TestAutonomousSubscribe_WarningKindsStayBare(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-cli-3"}`}
	withCaptureServer(t, c)

	cmd := newAutonomousSubscribeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--on", "scope_warning",
		"--on", "guardrail_warn",
		"--prd", "prd-guard",
		"--sink", "alert",
		"--target", "router-1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	body := c.body(t)
	filter := mapOf(t, body, "filter")
	kinds := stringSliceOf(t, filter, "kinds")
	if len(kinds) != 2 || kinds[0] != "scope_warning" || kinds[1] != "guardrail_warn" {
		t.Errorf("filter.kinds = %v, want [scope_warning guardrail_warn] (bare, never task.*)", kinds)
	}
	if filter["prd_id"] != "prd-guard" {
		t.Errorf("filter.prd_id = %v, want prd-guard", filter["prd_id"])
	}
	sink := mapOf(t, body, "sink")
	if sink["type"] != "alert" || sink["target"] != "router-1" {
		t.Errorf("sink = %v, want {alert router-1}", sink)
	}
}

// TestAutonomousSubscribe_ValidationErrors covers client-side validation.
func TestAutonomousSubscribe_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no filter", []string{"--sink", "comm", "--target", "r"}, "at least one of --on (kinds) or --level"},
		{"bad sink", []string{"--on", "session_started", "--sink", "kubernetes", "--target", "x"}, "unknown --sink"},
		{"no target", []string{"--on", "session_started", "--sink", "alert"}, "--target is required"},
		{"send without session sink", []string{"--on", "session_started", "--sink", "comm", "--target", "r", "--send", "hi"}, "only valid with --sink session"},
		{"negative expires", []string{"--on", "session_started", "--sink", "comm", "--target", "r", "--expires", "-5"}, "positive seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{}`}
			withCaptureServer(t, c)
			cmd := newAutonomousSubscribeCmd()
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil (output: %s)", tc.wantErr, out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
			if c.gotMethod != "" {
				t.Errorf("no daemon request should be made, got %s", c.gotMethod)
			}
		})
	}
}

// TestAutonomousUnsubscribe_DeletesByID covers DELETE routing.
func TestAutonomousUnsubscribe_DeletesByID(t *testing.T) {
	c := &captureDaemon{}
	withCaptureServer(t, c)
	cmd := newAutonomousUnsubscribeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"sub-cli-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	if c.gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", c.gotMethod)
	}
	if c.gotPath != "/api/autonomous/subscriptions/sub-cli-1" {
		t.Errorf("path = %q", c.gotPath)
	}
}

// TestAutonomousUnsubscribe_RejectsMissingID.
func TestAutonomousUnsubscribe_RejectsMissingID(t *testing.T) {
	cmd := newAutonomousUnsubscribeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a missing id")
	}
}

// TestAutonomousSubscriptions_UsesOwnerQuery covers the GET + owner filter.
func TestAutonomousSubscriptions_UsesOwnerQuery(t *testing.T) {
	c := &captureDaemon{replyBody: `[{"id":"sub-cli-1"}]`}
	withCaptureServer(t, c)
	cmd := newAutonomousSubscriptionsCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--owner", "operator"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	if c.gotMethod != http.MethodGet || c.gotPath != "/api/autonomous/subscriptions" {
		t.Fatalf("route = %s %s", c.gotMethod, c.gotPath)
	}
	if !strings.Contains(c.gotQuery, "owner=operator") {
		t.Errorf("query = %q, want owner=operator", c.gotQuery)
	}
}

// TestAutonomousSubscription_HelpDocumentsShorthand guards the documented
// flag shorthand so the help text can't drift from the behavior.
func TestAutonomousSubscription_HelpDocumentsShorthand(t *testing.T) {
	long := newAutonomousSubscribeCmd().Long
	for _, want := range []string{
		"--on session_started",
		"--task <id> --on session_started --send \"<text>\"",
		"comm", "session", "topic", "alert",
		"--one-shot", "--coalesce", "--expires",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("subscribe help should mention %q:\n%s", want, long)
		}
	}
	if !strings.Contains(newAutonomousCmd().Long, "subscribe --on") {
		t.Error("autonomous help should list the subscribe subcommand")
	}
	if !strings.Contains(newAutonomousCmd().Long, "subscriptions") {
		t.Error("autonomous help should list the subscriptions subcommand")
	}
	if !strings.Contains(newAutonomousCmd().Long, "unsubscribe") {
		t.Error("autonomous help should list the unsubscribe subcommand")
	}
}

// --- tiny helpers -----------------------------------------------------------

func mapOf(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("body missing %q (got %T)", key, m[key])
	}
	return v
}

func stringSliceOf(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is %T, not an array", key, m[key])
	}
	out := make([]string, len(v))
	for i, e := range v {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("%s[%d] is %T", key, i, e)
		}
		out[i] = s
	}
	return out
}
