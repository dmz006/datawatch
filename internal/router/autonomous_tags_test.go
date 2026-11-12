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
		{"sweep", "tags", []string{"tags", "sweep"}, "POST", "/api/autonomous/tags/sweep", nil},
		{"sweep-dry-run", "tags", []string{"tags", "sweep", "dry-run"}, "POST", "/api/autonomous/tags/sweep?dry_run=true", nil},
		{"sweep-dry-run-underscore", "tags", []string{"tags", "sweep", "dry_run"}, "POST", "/api/autonomous/tags/sweep?dry_run=true", nil},
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
		{"tags", "sweep", "bogus"},
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

func TestTagListVerbSurfacesSystemMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/autonomous/tags" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tags":[{"id":"da39a3ee","name":"unsorted","parent_id":"","system":true,"color":"#888888"}]}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	r := bareSubsRouter()
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("autonomous tags"))
	if !strings.Contains(out, `"system": true`) && !strings.Contains(out, `"system":true`) {
		t.Errorf("tag list reply does not surface the system marker: %q", out)
	}
}

func TestTagHelpMentionsVerbs(t *testing.T) {
	help := joinReplies((&Router{}).HandleTestMessage("autonomous"))
	for _, v := range []string{"tags", "tags sweep [dry-run]", "tag-create", "tag-rename", "tag-move", "tag-color", "tag-delete", "set-tags"} {
		if !strings.Contains(help, v) {
			t.Errorf("help misses %q", v)
		}
	}
}

// TestTagSweepVerbDispatch sends the sweep through the router and checks the
// REST call it makes: a plain sweep POSTs /api/autonomous/tags/sweep with no
// query, and the dry-run form adds dry_run=true.
func TestTagSweepVerbDispatch(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotQuery, gotBody = r.Method, r.URL.Path, r.URL.RawQuery, string(b)
		_, _ = w.Write([]byte(`{"sessions":{"examined":12,"changed":3,"unchanged":9,"failed":0},"automata":{"examined":5,"changed":2,"unchanged":3,"failed":0},"automations":{"examined":4,"changed":0,"unchanged":4,"failed":0},"tasks":{"examined":30,"changed":4,"unchanged":26,"failed":0},"templates":{"examined":2,"changed":1,"unchanged":1,"failed":0},"decision_records":{"examined":7,"changed":0,"unchanged":7,"failed":1},"questions":{"examined":1,"changed":1,"unchanged":0,"failed":0}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	r := bareSubsRouter()
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("autonomous tags sweep"))
	if gotMethod != "POST" || gotPath != "/api/autonomous/tags/sweep" || gotQuery != "" {
		t.Errorf("request = %s %s ?%s, want POST /api/autonomous/tags/sweep with no query", gotMethod, gotPath, gotQuery)
	}
	if gotBody != "" {
		t.Errorf("unexpected body %q", gotBody)
	}
	if !strings.Contains(out, "sessions") || !strings.Contains(out, "decision_records") {
		t.Errorf("sweep reply does not surface the per-kind tally: %q", out)
	}

	out = joinReplies(r.HandleTestMessage("autonomous tags sweep dry-run"))
	if gotMethod != "POST" || gotPath != "/api/autonomous/tags/sweep" || gotQuery != "dry_run=true" {
		t.Errorf("dry-run request = %s %s ?%s, want POST /api/autonomous/tags/sweep?dry_run=true", gotMethod, gotPath, gotQuery)
	}
	if strings.Contains(out, "usage") {
		t.Errorf("dry-run sweep refused: %q", out)
	}
}

// TestTagSweepUsageBadForm confirms a sweep with an unexpected third word
// falls back to the tags usage line naming the sweep form.
func TestTagSweepUsageBadForm(t *testing.T) {
	r := bareSubsRouter()
	out := joinReplies(r.HandleTestMessage("autonomous tags sweep bogus"))
	if !strings.Contains(out, "usage") || !strings.Contains(out, "sweep [dry-run]") {
		t.Errorf("expected the tags sweep usage line, got %q", out)
	}
}
