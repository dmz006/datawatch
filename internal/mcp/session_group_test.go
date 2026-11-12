// list_sessions group_by / tag_prefix params — the session-list grouping
// read exposed over MCP, backed by the same REST handler as the PWA and the
// CLI --group-by flag.

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestSessionGroup_ToolDeclaresGroupParams(t *testing.T) {
	tool := (&Server{}).toolListSessions()
	for _, p := range []string{"group_by", "tag_prefix"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("list_sessions missing %q param", p)
		}
	}
}

func TestSessionGroupQuery_GroupByTakesRestPath(t *testing.T) {
	q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"group_by": "tag",
	}))
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%q", ok, errMsg)
	}
	if q.Get("group_by") != "tag" {
		t.Errorf("group_by = %q", q.Get("group_by"))
	}
	if q.Has("tag_prefix") {
		t.Errorf("empty prefix must not be forwarded: %q", q.Encode())
	}
}

func TestSessionGroupQuery_TagPrefixForwarded(t *testing.T) {
	q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"group_by":   "tag",
		"tag_prefix": "automation:",
	}))
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%q", ok, errMsg)
	}
	if q.Get("group_by") != "tag" || q.Get("tag_prefix") != "automation:" {
		t.Errorf("query = %q", q.Encode())
	}
}

func TestSessionGroupQuery_AutomatonAxis(t *testing.T) {
	for _, axis := range []string{"state", "node", "project", "automaton"} {
		q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{"group_by": axis}))
		if !ok || errMsg != "" {
			t.Fatalf("axis %s: ok=%v err=%q", axis, ok, errMsg)
		}
		if q.Get("group_by") != axis {
			t.Errorf("group_by = %q, want %q", q.Get("group_by"), axis)
		}
	}
}

func TestSessionGroupQuery_UnknownAxisRejected(t *testing.T) {
	if q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{"group_by": "colour"})); ok || q != nil || !strings.HasPrefix(errMsg, "Error:") {
		t.Errorf("got ok=%v err=%q, want an error", ok, errMsg)
	}
}

func TestSessionGroupQuery_OrphanedConflicts(t *testing.T) {
	if q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{"group_by": "tag", "orphaned": true})); ok || q != nil || !strings.HasPrefix(errMsg, "Error:") {
		t.Errorf("got ok=%v err=%q, want an error", ok, errMsg)
	}
}

func TestSessionGroupQuery_NoGroupByStaysLegacy(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"tag_prefix": "automation:"}, // prefix alone is dropped by the handler; no grouping
		{"name": "worker-*", "state": "running", "orphaned": true},
	} {
		if q, ok, errMsg := listSessionsViewQuery(callReq(args)); ok || errMsg != "" || q != nil {
			t.Errorf("args %v: got q=%v ok=%v err=%q, want legacy path", args, q, ok, errMsg)
		}
	}
}

func TestSessionGroupForwardsToREST(t *testing.T) {
	var gotQuery url.Values
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{
"group_by":"tag","tag_prefix":"automation:","total":4,
"groups":[
 {"key":"(none)","count":2,"state_mix":{"running":1,"complete":1},"sessions":[{"id":"a1","full_id":"h1-a1","name":"no tag","state":"running"},{"id":"b1","full_id":"h1-b1","name":"done","state":"complete"}],
  "stories":null,"progress":null},
 {"key":"automation:repo-ci-watch","count":2,"state_mix":{"running":1,"failed":1},"sessions":[{"id":"c1","full_id":"h1-c1","name":"watch","state":"running"},{"id":"d1","full_id":"h1-d1","name":"watch2","state":"failed"}],
  "stories":null,"progress":null}],
"state_totals":{"running":2,"complete":1,"failed":1}}`))
	})
	res, err := s.handleListSessions(context.Background(), callReq(map[string]any{
		"group_by": "tag", "tag_prefix": "automation:",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("group_by") != "tag" || gotQuery.Get("tag_prefix") != "automation:" {
		t.Errorf("forwarded query = %v", gotQuery)
	}
	body := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(body, `"group_by":"tag"`) || !strings.Contains(body, `"automation:repo-ci-watch"`) {
		t.Errorf("grouped JSON not passed through: %s", body)
	}
}

func TestSessionGroupHandleRejectsUnknownAxis(t *testing.T) {
	res, err := (&Server{}).handleListSessions(context.Background(), callReq(map[string]any{"group_by": "colour"}))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected an error result")
	}
	text := ""
	if c, ok := res.Content[0].(mcpsdk.TextContent); ok {
		text = c.Text
	}
	if !strings.Contains(text, "colour") {
		t.Errorf("error should name the bad axis, got %q", text)
	}
}
