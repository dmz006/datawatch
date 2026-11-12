// Tests for the webhook and peer sinks on the CLI subscription commands: the
// --secret-ref flag reaches the request body, the client refuses the
// combinations the daemon also refuses, and the help text names the new sinks.
package main

import (
	"net/http"
	"strings"
	"testing"
)

const cliTestHookURL = "https://hooks.example.net/datawatch"

// TestAutonomousSubscribe_WebhookSendsSecretRef checks the webhook shape of
// `autonomous subscribe`: URL target and the reference in sink.secret_ref.
func TestAutonomousSubscribe_WebhookSendsSecretRef(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-hook"}`}
	withCaptureServer(t, c)

	err := runSubCmd(t, newAutonomousSubscribeCmd(),
		"--on", "task.completed",
		"--sink", "webhook",
		"--target", cliTestHookURL,
		"--secret-ref", "${secret:lifecycle-hook}",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/subscriptions" {
		t.Fatalf("route = %s %s, want POST /api/autonomous/subscriptions", c.gotMethod, c.gotPath)
	}
	sink := mapOf(t, c.body(t), "sink")
	if sink["type"] != "webhook" || sink["target"] != cliTestHookURL {
		t.Errorf("sink = %v, want webhook to the URL", sink)
	}
	if sink["secret_ref"] != "${secret:lifecycle-hook}" {
		t.Errorf("secret_ref = %v, want the reference text", sink["secret_ref"])
	}
}

// TestAutonomousSubscribe_PeerSendsNoSecret checks a peer sink names the
// registered peer and carries no secret field.
func TestAutonomousSubscribe_PeerSendsNoSecret(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-peer"}`}
	withCaptureServer(t, c)

	err := runSubCmd(t, newAutonomousSubscribeCmd(),
		"--on", "prd.blocked",
		"--sink", "peer",
		"--target", "laptop",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	sink := mapOf(t, c.body(t), "sink")
	if sink["type"] != "peer" || sink["target"] != "laptop" {
		t.Errorf("sink = %v, want peer laptop", sink)
	}
	if _, present := sink["secret_ref"]; present {
		t.Errorf("secret_ref sent with a peer sink: %v", sink)
	}
}

// TestAutonomousSubscribe_SecretRefRules checks the client refuses the
// combinations the daemon refuses, before any request is sent.
func TestAutonomousSubscribe_SecretRefRules(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"webhook needs the reference", []string{"--on", "task.completed", "--sink", "webhook", "--target", cliTestHookURL}, "requires --secret-ref"},
		{"reference refused on comm", []string{"--on", "task.completed", "--sink", "comm", "--target", "r", "--secret-ref", "${secret:x}"}, "only valid with --sink webhook"},
		{"reference refused on peer", []string{"--on", "task.completed", "--sink", "peer", "--target", "laptop", "--secret-ref", "${secret:x}"}, "only valid with --sink webhook"},
		{"webhook needs a target", []string{"--on", "task.completed", "--sink", "webhook", "--secret-ref", "${secret:x}"}, "--target is required"},
		{"unknown sink names the new types", []string{"--on", "task.completed", "--sink", "kafka", "--target", "x"}, "webhook, peer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{}`}
			withCaptureServer(t, c)
			err := runSubCmd(t, newAutonomousSubscribeCmd(), tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
			if c.gotMethod != "" {
				t.Errorf("no daemon request should be made, got %s", c.gotMethod)
			}
		})
	}
}

// TestAutonomousSubscriptionUpdate_WebhookSendsSecretRef checks the same flag
// on the PUT body, and that switching to a webhook keeps the reference out of
// any other sink.
func TestAutonomousSubscriptionUpdate_WebhookSendsSecretRef(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"sub-5"}`}
	withCaptureServer(t, c)

	err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
		"sub-5",
		"--on", "task.completed",
		"--sink", "webhook",
		"--target", cliTestHookURL,
		"--secret-ref", "${secret:lifecycle-hook-prod}",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if c.gotMethod != http.MethodPut || c.gotPath != "/api/autonomous/subscriptions/sub-5" {
		t.Fatalf("route = %s %s, want PUT .../sub-5", c.gotMethod, c.gotPath)
	}
	sink := mapOf(t, c.body(t), "sink")
	if sink["type"] != "webhook" || sink["secret_ref"] != "${secret:lifecycle-hook-prod}" {
		t.Errorf("sink = %v, want webhook with the reference", sink)
	}
}

// TestAutonomousSubscriptionUpdate_SecretRefRules checks the update command
// applies the same client rules as subscribe.
func TestAutonomousSubscriptionUpdate_SecretRefRules(t *testing.T) {
	c := &captureDaemon{replyBody: `{}`}
	withCaptureServer(t, c)

	err := runSubCmd(t, newAutonomousSubscriptionUpdateCmd(),
		"sub-5", "--on", "task.completed", "--sink", "peer", "--target", "laptop",
		"--secret-ref", "${secret:x}",
	)
	if err == nil || !strings.Contains(err.Error(), "only valid with --sink webhook") {
		t.Fatalf("err = %v, want the secret-ref rule", err)
	}
	if c.gotMethod != "" {
		t.Errorf("no daemon request should be made, got %s", c.gotMethod)
	}
}

// TestAutonomousSubscription_HelpNamesWebhookAndPeer checks the sink list,
// the flag and the help text on both commands name the new sinks.
func TestAutonomousSubscription_HelpNamesWebhookAndPeer(t *testing.T) {
	for _, tc := range []struct {
		name string
		help string
		flag string
	}{
		{"subscribe", newAutonomousSubscribeCmd().Long, "secret-ref"},
		{"subscription-update", newAutonomousSubscriptionUpdateCmd().Long, "secret-ref"},
	} {
		if !strings.Contains(tc.help, "webhook") || !strings.Contains(tc.help, "peer") {
			t.Errorf("%s help does not name the webhook and peer sinks", tc.name)
		}
	}
	for _, cmd := range []struct {
		name string
		flag func() bool
	}{
		{"subscribe", func() bool { return newAutonomousSubscribeCmd().Flags().Lookup("secret-ref") != nil }},
		{"subscription-update", func() bool { return newAutonomousSubscriptionUpdateCmd().Flags().Lookup("secret-ref") != nil }},
	} {
		if !cmd.flag() {
			t.Errorf("%s has no --secret-ref flag", cmd.name)
		}
	}
	for _, sink := range []string{"webhook", "peer"} {
		if !lifecycleSubscriptionSinkTypes[sink] {
			t.Errorf("sink %q is not an accepted --sink value", sink)
		}
	}
}
