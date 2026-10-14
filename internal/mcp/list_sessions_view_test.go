// list_sessions view / states / since params (BL432 / GH#242).

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestListSessions_ToolDeclaresViewParams(t *testing.T) {
	tool := (&Server{}).toolListSessions()
	for _, p := range []string{"view", "states", "since"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("list_sessions missing %q param", p)
		}
	}
}

func TestListSessionsViewQuery_LegacyPathWithoutParams(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"view": "full"},
		{"name": "worker-*", "state": "running"},
		{"orphaned": true},
	} {
		q, ok, errMsg := listSessionsViewQuery(callReq(args))
		if ok || errMsg != "" || q != nil {
			t.Errorf("args %v: got q=%v ok=%v err=%q, want legacy path", args, q, ok, errMsg)
		}
	}
}

func TestListSessionsViewQuery_BuildsRESTQuery(t *testing.T) {
	q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"view": "summary", "states": "active,failed", "since": "1700000000",
		"name": "w-*", "backend": "opencode",
	}))
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%q", ok, errMsg)
	}
	want := url.Values{
		"view": {"summary"}, "states": {"active,failed"}, "since": {"1700000000"},
		"name": {"w-*"}, "backend": {"opencode"},
	}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}
}

func TestListSessionsViewQuery_FullViewWithStatesOmitsView(t *testing.T) {
	q, ok, _ := listSessionsViewQuery(callReq(map[string]any{"view": "full", "states": "running"}))
	if !ok {
		t.Fatal("states alone must take the REST path")
	}
	if q.Has("view") {
		t.Errorf("view=full must not be forwarded, got %q", q.Encode())
	}
}

func TestListSessionsViewQuery_Errors(t *testing.T) {
	cases := map[string]map[string]any{
		"unknown view":       {"view": "tiny"},
		"orphaned + summary": {"view": "summary", "orphaned": true},
		"orphaned + since":   {"since": "1700000000", "orphaned": true},
	}
	for name, args := range cases {
		if _, ok, errMsg := listSessionsViewQuery(callReq(args)); ok || !strings.HasPrefix(errMsg, "Error:") {
			t.Errorf("%s: ok=%v err=%q, want an error", name, ok, errMsg)
		}
	}
}

func TestListSessions_SummaryForwardsToREST(t *testing.T) {
	var gotQuery url.Values
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		w.Header().Set("X-Datawatch-Sessions-Omitted", "7")
		_, _ = w.Write([]byte(`[{"id":"a1","state":"running","truncated_fields":["task"]}]`))
	})
	res, err := s.handleListSessions(context.Background(), callReq(map[string]any{"view": "summary", "states": "active"}))
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("view") != "summary" || gotQuery.Get("states") != "active" {
		t.Errorf("forwarded query = %v", gotQuery)
	}
	if len(res.Content) != 2 {
		t.Fatalf("want JSON + omitted note, got %d items", len(res.Content))
	}
	if body := res.Content[0].(mcpsdk.TextContent).Text; !strings.Contains(body, `"truncated_fields":["task"]`) {
		t.Errorf("JSON not passed through: %s", body)
	}
	if note := res.Content[1].(mcpsdk.TextContent).Text; !strings.HasPrefix(note, "7 older terminal") {
		t.Errorf("omitted note = %q", note)
	}
}

func TestListSessions_NoOmittedNoteWhenNothingDropped(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	res, _ := s.handleListSessions(context.Background(), callReq(map[string]any{"since": "2026-01-01T00:00:00Z"}))
	if len(res.Content) != 1 || res.Content[0].(mcpsdk.TextContent).Text != "[]" {
		t.Errorf("unexpected result: %+v", res.Content)
	}
}

func TestListSessions_RESTErrorSurfacesAsText(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `unknown state "nope" in states`, http.StatusBadRequest)
	})
	res, _ := s.handleListSessions(context.Background(), callReq(map[string]any{"states": "nope"}))
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "unknown state") {
		t.Errorf("error text = %q", text)
	}
}
