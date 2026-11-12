// Flag→request mapping tests for the `autonomous tag-*` and
// `autonomous prd-set-tags` commands. They reuse the captureDaemon /
// withCaptureServer helpers of cli_autonomous_subscriptions_test.go.
package main

import (
	"net/http"
	"reflect"
	"testing"
)

func TestTagCLIRegistered(t *testing.T) {
	auto := newAutonomousCmd()
	for _, name := range []string{"tag-list", "tag-create", "tag-rename", "tag-move", "tag-color", "tag-delete", "prd-set-tags"} {
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

func TestTagCLIRequests(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		method     string
		path       string
		query      string
		wantBody   map[string]any
		wantNoBody bool
	}{
		{"list", []string{"tag-list"}, http.MethodGet, "/api/autonomous/tags", "", nil, true},
		{"list-sub", []string{"tag-list", "--include-sub"}, http.MethodGet, "/api/autonomous/tags", "include_sub=true", nil, true},
		{"create-flags", []string{"tag-create", "infra", "--parent", "1a2b3c4d", "--color", "#6366f1", "--description", "core infra"},
			http.MethodPost, "/api/autonomous/tags", "",
			map[string]any{"name": "infra", "parent_id": "1a2b3c4d", "color": "#6366f1", "description": "core infra"}, false},
		{"create-bare", []string{"tag-create", "infra"}, http.MethodPost, "/api/autonomous/tags", "",
			map[string]any{"name": "infra", "parent_id": "", "color": "", "description": ""}, false},
		{"rename", []string{"tag-rename", "ab12cd34", "platform"}, http.MethodPatch, "/api/autonomous/tags/ab12cd34", "",
			map[string]any{"name": "platform"}, false},
		{"move", []string{"tag-move", "ab12cd34", "--parent", "ff00ff00"}, http.MethodPatch, "/api/autonomous/tags/ab12cd34", "",
			map[string]any{"parent_id": "ff00ff00"}, false},
		{"move-root", []string{"tag-move", "ab12cd34", "--parent", "root"}, http.MethodPatch, "/api/autonomous/tags/ab12cd34", "",
			map[string]any{"parent_id": ""}, false},
		{"color", []string{"tag-color", "ab12cd34", "#ff0000"}, http.MethodPatch, "/api/autonomous/tags/ab12cd34", "",
			map[string]any{"color": "#ff0000"}, false},
		{"delete", []string{"tag-delete", "ab12cd34"}, http.MethodDelete, "/api/autonomous/tags/ab12cd34", "", nil, true},
		{"set-tags", []string{"prd-set-tags", "prd1", "--tags", "aa11bb22, cc33dd44"}, http.MethodPost, "/api/autonomous/prds/prd1/set_tags", "",
			map[string]any{"tag_ids": []any{"aa11bb22", "cc33dd44"}}, false},
		{"set-tags-clear", []string{"prd-set-tags", "prd1", "--tags", ""}, http.MethodPost, "/api/autonomous/prds/prd1/set_tags", "",
			map[string]any{"tag_ids": []any{}}, false},
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
			if tc.wantNoBody {
				if len(c.gotRaw) != 0 {
					t.Errorf("unexpected body %q", c.gotRaw)
				}
				return
			}
			if got := c.body(t); !reflect.DeepEqual(got, tc.wantBody) {
				t.Errorf("body = %v, want %v", got, tc.wantBody)
			}
		})
	}
}

func TestTagCLIArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"tag-create"},
		{"tag-rename", "only-id"},
		{"tag-move", "ab12cd34"},
		{"tag-color", "ab12cd34"},
		{"tag-delete"},
		{"prd-set-tags", "prd1"},
	} {
		c := &captureDaemon{replyBody: `{}`}
		withCaptureServer(t, c)
		_ = runSubCmd(t, newAutonomousCmd(), args...)
		if c.gotMethod != "" {
			t.Errorf("%v: request was sent despite invalid arguments", args)
		}
	}
}
