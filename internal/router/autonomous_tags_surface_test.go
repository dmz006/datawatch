// Tests for the comm verbs that surface the tag browser: "autonomous tags
// [counts]", "attention", "session-tags" and "set-primary". They cover the
// request each verb builds, the usage refusals and the dispatch through the
// router to a fake REST server.

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

func TestTagSurfaceBuildRequest(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		method string
		path   string
		body   map[string]any
	}{
		{"counts", []string{"tags", "counts"}, "GET", "/api/autonomous/tags?counts=true", nil},
		{"attention", []string{"attention"}, "GET", "/api/autonomous/attention", nil},
		{"session-tags-set", []string{"session-tags", "sess1234", "aa11bb22,cc33dd44"}, "POST", "/api/sessions/tags",
			map[string]any{"id": "sess1234", "tag_ids": []string{"aa11bb22", "cc33dd44"}}},
		{"session-tags-clear", []string{"session_tags", "sess1234"}, "POST", "/api/sessions/tags",
			map[string]any{"id": "sess1234", "tag_ids": []string{}}},
		{"set-primary", []string{"set-primary", "prd1", "aa11bb22"}, "POST", "/api/autonomous/prds/prd1/set_tags",
			map[string]any{"primary_tag_id": "aa11bb22"}},
		{"set-primary-none", []string{"set_primary", "prd1", "none"}, "POST", "/api/autonomous/prds/prd1/set_tags",
			map[string]any{"primary_tag_id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, usage := buildCommTagRequest(tc.args[0], tc.args)
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

func TestTagSurfaceBuildRequestUsage(t *testing.T) {
	for _, args := range [][]string{
		{"tags", "bogus"},
		{"tags", "counts", "extra"},
		{"attention", "extra"},
		{"session-tags"},
		{"session-tags", "sess1234", "aa11", "extra"},
		{"set-primary", "prd1"},
		{"set-primary", "prd1", "aa11", "extra"},
	} {
		if _, usage := buildCommTagRequest(args[0], args); !strings.HasPrefix(usage, "usage:") {
			t.Errorf("%v: usage = %q", args, usage)
		}
	}
}

func TestTagSurfaceDispatch(t *testing.T) {
	type seen struct{ method, path, query, body string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	r := bareSubsRouter()
	r.SetWebPort(port)

	cases := []struct {
		line string
		want seen
	}{
		{"autonomous tags counts", seen{"GET", "/api/autonomous/tags", "counts=true", ""}},
		{"autonomous attention", seen{"GET", "/api/autonomous/attention", "", ""}},
		{"autonomous session-tags sess1234 aa11bb22", seen{"POST", "/api/sessions/tags", "", `{"id":"sess1234","tag_ids":["aa11bb22"]}`}},
		{"autonomous set-primary prd1 none", seen{"POST", "/api/autonomous/prds/prd1/set_tags", "", `{"primary_tag_id":""}`}},
	}
	for _, tc := range cases {
		got = nil
		out := joinReplies(r.HandleTestMessage(tc.line))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q: request = %+v, want %+v", tc.line, got, tc.want)
		}
		if !strings.Contains(out, "ok") {
			t.Errorf("%q: reply = %q", tc.line, out)
		}
	}
}

func TestTagSurfaceHelpMentionsNewVerbs(t *testing.T) {
	help := joinReplies((&Router{}).HandleTestMessage("autonomous"))
	for _, v := range []string{"tags [counts]", "attention", "session-tags", "set-primary"} {
		if !strings.Contains(help, v) {
			t.Errorf("help misses %q", v)
		}
	}
}
