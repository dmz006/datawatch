// Tests for the tag-registry commands: the status command that prints the
// registry object of GET /api/autonomous/tags, and the note dismiss command.
// They use the captureDaemon / withCaptureServer helpers of
// cli_autonomous_subscriptions_test.go.
package main

import (
	"net/http"
	"testing"
)

const tagRegistryReplyJSON = `{"tags":[{"id":"aa11","name":"ops"}],"registry":{"role":"keeper",` +
	`"keeper":"","stale":false,"revision":3,"pending_sync_count":0,"notes":[],"last_error":""}}`

func TestTagRegistryCLIRegistered(t *testing.T) {
	for _, name := range []string{"tag-registry", "tag-registry-dismiss"} {
		found := false
		for _, sub := range newAutonomousCmd().Commands() {
			if sub.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("autonomous %s is not registered", name)
		}
	}
}

func TestTagRegistryCLIRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		method string
		path   string
	}{
		{"status", []string{"tag-registry"}, http.MethodGet, "/api/autonomous/tags"},
		{"dismiss", []string{"tag-registry-dismiss", "note-7f"}, http.MethodPost, "/api/autonomous/tags/registry/notes/note-7f/dismiss"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: tagRegistryReplyJSON}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if c.gotMethod != tc.method || c.gotPath != tc.path || c.gotQuery != "" {
				t.Fatalf("route = %s %s ?%s, want %s %s", c.gotMethod, c.gotPath, c.gotQuery, tc.method, tc.path)
			}
			if len(c.gotRaw) != 0 {
				t.Errorf("unexpected body %q", c.gotRaw)
			}
		})
	}
}

func TestTagRegistryCLIExtractsRegistryObject(t *testing.T) {
	got, err := registryObjectFromTagsReply([]byte(tagRegistryReplyJSON))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := `{"role":"keeper","keeper":"","stale":false,"revision":3,"pending_sync_count":0,"notes":[],"last_error":""}`
	if string(got) != want {
		t.Errorf("registry = %s, want %s", got, want)
	}
}

func TestTagRegistryCLIRefusesReplyWithoutRegistry(t *testing.T) {
	if _, err := registryObjectFromTagsReply([]byte(`{"tags":[]}`)); err == nil {
		t.Errorf("reply without a registry object was accepted")
	}
	if _, err := registryObjectFromTagsReply([]byte(`not json`)); err == nil {
		t.Errorf("malformed reply was accepted")
	}
}

func TestTagRegistryCLIArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"tag-registry-dismiss"},
		{"tag-registry-dismiss", "a", "b"},
		{"tag-registry", "extra"},
	} {
		c := &captureDaemon{replyBody: `{}`}
		withCaptureServer(t, c)
		_ = runSubCmd(t, newAutonomousCmd(), args...)
		if c.gotMethod != "" {
			t.Errorf("%v: request was sent despite invalid arguments", args)
		}
	}
}
