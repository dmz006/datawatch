// Flag-to-request tests for the tag-surface commands: tag-list --counts, the
// attention list, prd-set-tags --primary and the session tags command. They
// use the captureDaemon / withCaptureServer helpers of
// cli_autonomous_subscriptions_test.go.
package main

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestTagCLIAttentionRegistered(t *testing.T) {
	found := false
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "attention" {
			found = true
		}
	}
	if !found {
		t.Errorf("autonomous attention is not registered")
	}
}

func TestTagCLICountsAndAttentionRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		method string
		path   string
		query  string
	}{
		{"tag-list-counts", []string{"tag-list", "--counts"}, http.MethodGet, "/api/autonomous/tags", "counts=true"},
		{"tags-alias-counts", []string{"tags", "--counts"}, http.MethodGet, "/api/autonomous/tags", "counts=true"},
		{"tags-alias-plain", []string{"tags"}, http.MethodGet, "/api/autonomous/tags", ""},
		{"tags-counts-and-sub", []string{"tags", "--counts", "--include-sub"}, http.MethodGet, "/api/autonomous/tags", "counts=true&include_sub=true"},
		{"attention", []string{"attention"}, http.MethodGet, "/api/autonomous/attention", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{"ok":true}`}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if c.gotMethod != tc.method || c.gotPath != tc.path || c.gotQuery != tc.query {
				t.Fatalf("route = %s %s ?%s, want %s %s ?%s", c.gotMethod, c.gotPath, c.gotQuery, tc.method, tc.path, tc.query)
			}
			if len(c.gotRaw) != 0 {
				t.Errorf("unexpected body %q", c.gotRaw)
			}
		})
	}
}

func TestTagCLIPRDSetTagsPrimaryRequests(t *testing.T) {
	cases := []struct {
		name string
		args []string
		body map[string]any
	}{
		{"tags-and-primary", []string{"prd-set-tags", "prd1", "--tags", "aa11bb22,cc33dd44", "--primary", "aa11bb22"},
			map[string]any{"tag_ids": []any{"aa11bb22", "cc33dd44"}, "primary_tag_id": "aa11bb22"}},
		{"primary-only", []string{"prd-set-tags", "prd1", "--primary", "cc33dd44"},
			map[string]any{"primary_tag_id": "cc33dd44"}},
		{"primary-cleared", []string{"prd-set-tags", "prd1", "--primary", ""},
			map[string]any{"primary_tag_id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{"ok":true}`}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/prds/prd1/set_tags" {
				t.Fatalf("route = %s %s, want POST /api/autonomous/prds/prd1/set_tags", c.gotMethod, c.gotPath)
			}
			if got := c.body(t); !reflect.DeepEqual(got, tc.body) {
				t.Errorf("body = %v, want %v", got, tc.body)
			}
		})
	}
}

func TestTagCLIPRDSetTagsNeedsAFlag(t *testing.T) {
	c := &captureDaemon{replyBody: `{"ok":true}`}
	withCaptureServer(t, c)
	_ = runSubCmd(t, newAutonomousCmd(), "prd-set-tags", "prd1")
	if c.gotMethod != "" {
		t.Errorf("sent %s %s with no --tags or --primary, want no request", c.gotMethod, c.gotPath)
	}
}

func TestTagCLISessionTagsRequests(t *testing.T) {
	cases := []struct {
		name string
		args []string
		body map[string]any
	}{
		{"set", []string{"tags", "sess1234", "--tags", "aa11bb22, cc33dd44"},
			map[string]any{"id": "sess1234", "tag_ids": []any{"aa11bb22", "cc33dd44"}}},
		{"clear", []string{"tags", "sess1234", "--tags", ""},
			map[string]any{"id": "sess1234", "tag_ids": []any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{"ok":true}`}
			withCaptureServer(t, c)
			parent := &cobra.Command{Use: "session"}
			parent.AddCommand(newSessionTagsCmd())
			if err := runSubCmd(t, parent, tc.args...); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if c.gotMethod != http.MethodPost || c.gotPath != "/api/sessions/tags" {
				t.Fatalf("route = %s %s, want POST /api/sessions/tags", c.gotMethod, c.gotPath)
			}
			if got := c.body(t); !reflect.DeepEqual(got, tc.body) {
				t.Errorf("body = %v, want %v", got, tc.body)
			}
		})
	}
}
