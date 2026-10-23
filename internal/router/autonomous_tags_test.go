// Tests for the comm verbs "autonomous tags|tag-create|tag-rename|tag-move|
// tag-color|tag-delete|set-tags": argument parsing, the request each verb
// builds, and the dispatch through the router.

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestTagBuildRequest(t *testing.T) {
	cases := []struct {
		name   string
		verb   string
		args   []string
		method string
		path   string
		body   map[string]any
	}{
		{"list", "tags", []string{"tags"}, "GET", "/api/autonomous/tags", nil},
		{"create", "tag-create", []string{"tag-create", "infra", "parent=ab12cd34", "color=#6366f1", "description=core", "infra", "bits"},
			"POST", "/api/autonomous/tags",
			map[string]any{"name": "infra", "parent_id": "ab12cd34", "color": "#6366f1", "description": "core infra bits"}},
		{"create-underscore", "tag_create", []string{"tag_create", "x"}, "POST", "/api/autonomous/tags", map[string]any{"name": "x"}},
		{"rename", "tag-rename", []string{"tag-rename", "ab12cd34", "new", "name"}, "PATCH", "/api/autonomous/tags/ab12cd34", map[string]any{"name": "new name"}},
		{"move", "tag-move", []string{"tag-move", "ab12cd34", "ff00ff00"}, "PATCH", "/api/autonomous/tags/ab12cd34", map[string]any{"parent_id": "ff00ff00"}},
		{"move-root", "tag-move", []string{"tag-move", "ab12cd34", "root"}, "PATCH", "/api/autonomous/tags/ab12cd34", map[string]any{"parent_id": ""}},
		{"color", "tag-color", []string{"tag-color", "ab12cd34", "#ff0000"}, "PATCH", "/api/autonomous/tags/ab12cd34", map[string]any{"color": "#ff0000"}},
		{"delete", "tag-delete", []string{"tag-delete", "ab12cd34"}, "DELETE", "/api/autonomous/tags/ab12cd34", nil},
		{"set-tags", "set-tags", []string{"set-tags", "prd1", "aa11bb22,cc33dd44"}, "POST", "/api/autonomous/prds/prd1/set_tags",
			map[string]any{"tag_ids": []string{"aa11bb22", "cc33dd44"}}},
		{"set-tags-clear", "set_tags", []string{"set_tags", "prd1"}, "POST", "/api/autonomous/prds/prd1/set_tags",
			map[string]any{"tag_ids": []string{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, usage := buildCommTagRequest(tc.verb, tc.args)
			if usage != "" {
				t.Fatalf("unexpected usage: %s", usage)
			}
			if req.method != tc.method || req.path != tc.path {
				t.Errorf("route = %s %s, want %s %s", req.method, req.path, tc.method, tc.path)
			}
			if !reflect.DeepEqual(req.body, tc.body) {
				t.Errorf("body = %v, want %v", req.body, tc.body)
			}
		})
	}
}

func TestTagBuildRequestUsage(t *testing.T) {
	for _, args := range [][]string{
		{"tag-create"},
		{"tag-create", "x", "bogus"},
		{"tag-rename", "id"},
		{"tag-move", "id"},
		{"tag-color", "id"},
		{"tag-delete"},
		{"set-tags"},
	} {
		if _, usage := buildCommTagRequest(args[0], args); !strings.HasPrefix(usage, "usage:") {
			t.Errorf("%v: usage = %q", args, usage)
		}
	}
}

func TestTagVerbDispatch(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	r := bareSubsRouter()
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("autonomous set-tags prd1 aa11bb22"))
	if gotMethod != "POST" || gotPath != "/api/autonomous/prds/prd1/set_tags" || gotBody != `{"tag_ids":["aa11bb22"]}` {
		t.Errorf("request = %s %s %s", gotMethod, gotPath, gotBody)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("reply = %q", out)
	}

	if out = joinReplies(r.HandleTestMessage("autonomous tag-delete")); !strings.Contains(out, "usage") {
		t.Errorf("expected usage, got %q", out)
	}
}

func TestTagHelpMentionsVerbs(t *testing.T) {
	help := joinReplies((&Router{}).HandleTestMessage("autonomous"))
	for _, v := range []string{"tags", "tag-create", "tag-rename", "tag-move", "tag-color", "tag-delete", "set-tags"} {
		if !strings.Contains(help, v) {
			t.Errorf("help misses %q", v)
		}
	}
}
