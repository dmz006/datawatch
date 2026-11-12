// Flag→request mapping tests for `autonomous subscription-update` and
// `autonomous subscription-test`. They reuse the captureDaemon /
// withCaptureServer helpers of cli_autonomous_subscriptions_test.go.
package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// runSubCmd executes cmd with args, discarding its output.
func runSubCmd(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestSubscriptionSurface_CLIRegistered(t *testing.T) {
	auto := newAutonomousCmd()
	for _, name := range []string{"subscription-update", "subscription-test"} {
		found := false
		for _, sub := range auto.Commands() {
			if sub.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("autonomous %s is not registered", name)
		}
	}
}

func TestSubscriptionSurface_CLIUpdateProxiesPut(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-5"}`}
	withCaptureServer(t, c)

	before := time.Now().UTC()
	err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
		"sub-5",
		"--on", "session_started",
		"--on", "prd.blocked",
		"--task", "task-7",
		"--prd", "prd-1",
		"--send", "pick up the build",
		"--target", "pi-host",
		"--coalesce",
		"--expires", "3600",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if c.gotMethod != http.MethodPut || c.gotPath != "/api/autonomous/subscriptions/sub-5" {
		t.Fatalf("route = %s %s, want PUT /api/autonomous/subscriptions/sub-5", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	filter := mapOf(t, body, "filter")
	kinds := stringSliceOf(t, filter, "kinds")
	if len(kinds) != 2 || kinds[0] != "task.session_started" || kinds[1] != "prd.blocked" {
		t.Errorf("filter.kinds = %v", kinds)
	}
	if filter["task_id"] != "task-7" || filter["prd_id"] != "prd-1" {
		t.Errorf("filter = %v", filter)
	}
	sink := mapOf(t, body, "sink")
	if sink["type"] != "session" || sink["target"] != "pi-host" {
		t.Errorf("--send must imply a session sink, sink = %v", sink)
	}
	action := mapOf(t, body, "action")
	if action["type"] != "send_input" || action["text"] != "pick up the build" {
		t.Errorf("action = %v", action)
	}
	if body["coalesce"] != true {
		t.Errorf("coalesce = %v", body["coalesce"])
	}
	// Unlike subscribe, --task must not silently turn on one_shot.
	if _, ok := body["one_shot"]; ok {
		t.Errorf("one_shot should not be implied by --task on update, body: %s", c.gotRaw)
	}
	exp, _ := body["expires_at"].(string)
	at, perr := time.Parse(time.RFC3339, exp)
	if perr != nil {
		t.Fatalf("expires_at %q: %v", exp, perr)
	}
	if d := at.Sub(before); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expires_at is %v from now, want about 1h", d)
	}
}

func TestSubscriptionSurface_CLIUpdateExplicitOneShot(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-6"}`}
	withCaptureServer(t, c)
	err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
		"sub-6", "--on", "prd.blocked", "--sink", "comm", "--target", "r", "--one-shot")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := c.body(t)["one_shot"]; got != true {
		t.Errorf("one_shot = %v, want true", got)
	}
}

func TestSubscriptionSurface_CLIUpdateEscapesID(t *testing.T) {
	c := &captureDaemon{replyBody: `{}`}
	withCaptureServer(t, c)
	err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
		"a?b", "--on", "prd.blocked", "--sink", "comm", "--target", "r")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Unescaped, the "?" would start a query string and cut the path short.
	if c.gotPath != "/api/autonomous/subscriptions/a?b" || c.gotQuery != "" {
		t.Errorf("id was not path-escaped: path = %q query = %q", c.gotPath, c.gotQuery)
	}
}

func TestSubscriptionSurface_CLIUpdateArgumentValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no id", nil, "accepts 1 arg"},
		{"two ids", []string{"a", "b", "--on", "prd.blocked", "--sink", "comm", "--target", "r"}, "accepts 1 arg"},
		{"blank id", []string{" ", "--on", "prd.blocked", "--sink", "comm", "--target", "r"}, "id must be non-empty"},
		{"no filter", []string{"a", "--sink", "comm", "--target", "r"}, "at least one of --on"},
		{"no sink", []string{"a", "--on", "prd.blocked", "--target", "r"}, "--sink is required"},
		{"bad sink", []string{"a", "--on", "prd.blocked", "--sink", "kafka", "--target", "r"}, "unknown --sink"},
		{"no target", []string{"a", "--on", "prd.blocked", "--sink", "comm"}, "--target is required"},
		{"send on comm", []string{"a", "--on", "prd.blocked", "--sink", "comm", "--target", "r", "--send", "x"}, "only valid with --sink session"},
		{"negative expires", []string{"a", "--on", "prd.blocked", "--sink", "comm", "--target", "r", "--expires", "-1"}, "--expires must be positive"},
	}
	for _, tc := range cases {
		c := &captureDaemon{replyBody: `{}`}
		withCaptureServer(t, c)
		err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
		if c.gotMethod != "" {
			t.Errorf("%s: daemon was called (%s %s) for invalid input", tc.name, c.gotMethod, c.gotPath)
		}
	}
}

func TestSubscriptionSurface_CLIUpdateErrorPassthrough(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{
		{http.StatusBadRequest, "filter kind \"nope\" is not a known event kind"},
		{http.StatusNotFound, "subscription gone: subscription not found"},
	} {
		c := &captureDaemon{replyCode: tc.code, replyBody: tc.body}
		withCaptureServer(t, c)
		err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
			"gone", "--on", "prd.blocked", "--sink", "comm", "--target", "r")
		if err == nil || !strings.Contains(err.Error(), tc.body) {
			t.Errorf("code %d: err = %v, want containing %q", tc.code, err, tc.body)
		}
	}
}

func TestSubscriptionSurface_CLITestProxiesPost(t *testing.T) {
	c := &captureDaemon{replyBody: `{"ok":true,"sink":"comm:r"}`}
	withCaptureServer(t, c)
	if err := runSubCmd(t, newAutonomousSubscriptionTestCmd(), "sub-8"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/subscriptions/sub-8/test" {
		t.Errorf("route = %s %s, want POST /api/autonomous/subscriptions/sub-8/test", c.gotMethod, c.gotPath)
	}
}

func TestSubscriptionSurface_CLITestArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no id", nil, "accepts 1 arg"},
		{"two ids", []string{"a", "b"}, "accepts 1 arg"},
		{"blank id", []string{"  "}, "id must be non-empty"},
	} {
		c := &captureDaemon{replyBody: `{}`}
		withCaptureServer(t, c)
		err := runSubCmd(t, newAutonomousSubscriptionTestCmd(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
		if c.gotMethod != "" {
			t.Errorf("%s: daemon was called for invalid input", tc.name)
		}
	}
}

func TestSubscriptionSurface_CLITestErrorPassthrough(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		c := &captureDaemon{replyCode: code, replyBody: "subscription nope: subscription not found"}
		withCaptureServer(t, c)
		err := runSubCmd(t, newAutonomousSubscriptionTestCmd(), "nope")
		if err == nil || !strings.Contains(err.Error(), "subscription not found") {
			t.Errorf("code %d: err = %v", code, err)
		}
	}
}
