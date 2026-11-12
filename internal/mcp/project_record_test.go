// MCP-surface tests for the project record tools: project_upsert's optional
// record fields (owner, repository, default_branch, arcs, target, lane_defaults,
// weekly_share, branch_retention_days, major_trigger) and the new
// project_rollup, project_history and project_set_arcs readers and writer.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// recordCapture is a loopback daemon stub that records the last request and
// answers with a fixed JSON body.
type recordCapture struct {
	method, path, rawQuery string
	body                   map[string]any
	bodyRaw                []byte
	calls                  int
}

func newRecordServer(t *testing.T, reply string) (*recordCapture, *Server, func()) {
	t.Helper()
	cap := &recordCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.calls++
		cap.method, cap.path, cap.rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		cap.bodyRaw, _ = io.ReadAll(r.Body)
		cap.body = nil
		_ = json.Unmarshal(cap.bodyRaw, &cap.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	return cap, &Server{webPort: bl427Port(t, srv)}, srv.Close
}

func callTool(t *testing.T, fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error), args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

func requiredSet(required []string) map[string]bool {
	m := map[string]bool{}
	for _, r := range required {
		m[r] = true
	}
	return m
}

// ----- schemas and names --------------------------------------------------

func TestProjectRecordToolNames(t *testing.T) {
	s := &Server{}
	cases := map[string]string{
		"project_rollup":   s.toolProjectRollup().Name,
		"project_history":  s.toolProjectHistory().Name,
		"project_timeline": s.toolProjectTimeline().Name,
		"project_set_arcs": s.toolProjectSetArcs().Name,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("tool name = %q, want %q", got, want)
		}
	}
}

func TestProjectRecordToolSchemas(t *testing.T) {
	s := &Server{}

	roll := s.toolProjectRollup()
	if req := requiredSet(roll.InputSchema.Required); !req["name"] || req["window"] {
		t.Errorf("project_rollup required = %v; want name only", roll.InputSchema.Required)
	}
	if roll.InputSchema.Properties["window"] == nil {
		t.Error("project_rollup missing window")
	}

	hist := s.toolProjectHistory()
	if req := requiredSet(hist.InputSchema.Required); !req["name"] || req["limit"] || req["cursor"] {
		t.Errorf("project_history required = %v; want name only", hist.InputSchema.Required)
	}
	for _, p := range []string{"limit", "cursor"} {
		if hist.InputSchema.Properties[p] == nil {
			t.Errorf("project_history missing %s", p)
		}
	}

	tl := s.toolProjectTimeline()
	if req := requiredSet(tl.InputSchema.Required); !req["name"] || len(req) != 1 {
		t.Errorf("project_timeline required = %v; want name only", tl.InputSchema.Required)
	}
	for _, p := range []string{"level", "parent", "automaton", "focus", "limit", "cursor"} {
		if tl.InputSchema.Properties[p] == nil {
			t.Errorf("project_timeline missing %s", p)
		}
	}

	arcs := s.toolProjectSetArcs()
	if req := requiredSet(arcs.InputSchema.Required); !req["name"] || !req["arcs"] {
		t.Errorf("project_set_arcs required = %v; want name and arcs", arcs.InputSchema.Required)
	}
}

func TestProjectUpsertToolHasRecordFields(t *testing.T) {
	s := &Server{}
	tool := s.toolProjectUpsert()
	for _, p := range []string{"owner", "repository", "default_branch", "arcs", "target",
		"lane_defaults", "weekly_share", "branch_retention_days", "major_trigger"} {
		if tool.InputSchema.Properties[p] == nil {
			t.Errorf("project_upsert missing %s", p)
		}
	}
	req := requiredSet(tool.InputSchema.Required)
	if req["owner"] || req["arcs"] || req["weekly_share"] {
		t.Errorf("record fields must be optional: %v", tool.InputSchema.Required)
	}
}

// ----- project_upsert record fields ---------------------------------------

func TestProjectUpsertForwardsRecordFields(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name":                  "acme",
		"dir":                   "/tmp/acme",
		"owner":                 "platform",
		"repository":            "dmz006/acme",
		"default_branch":        "main",
		"major_trigger":         "minor",
		"weekly_share":          float64(30),
		"branch_retention_days": float64(14),
		"target":                map[string]any{"name": "v1", "definition_of_done": "shipped"},
		"lane_defaults":         `{"lane":"pi-prompt","backend":"ollama","model":"qwen"}`,
		"arcs": []any{
			map[string]any{"automation_id": "a1", "priority": float64(1), "state": "active"},
			map[string]any{"automation_id": "a2"},
		},
	})
	if res.IsError {
		t.Fatalf("upsert failed: %s", resContentText(t, res))
	}
	if cap.method != http.MethodPost || cap.path != "/api/projects" {
		t.Fatalf("got %s %s", cap.method, cap.path)
	}
	b := cap.body
	if b["owner"] != "platform" || b["repository"] != "dmz006/acme" ||
		b["default_branch"] != "main" || b["major_trigger"] != "minor" {
		t.Errorf("string record fields not forwarded: %v", b)
	}
	if got, ok := b["weekly_share"].(float64); !ok || got != 30 {
		t.Errorf("weekly_share = %v", b["weekly_share"])
	}
	if got, ok := b["branch_retention_days"].(float64); !ok || got != 14 {
		t.Errorf("branch_retention_days = %v", b["branch_retention_days"])
	}
	target, ok := b["target"].(map[string]any)
	if !ok || target["definition_of_done"] != "shipped" {
		t.Errorf("target = %v", b["target"])
	}
	lane, ok := b["lane_defaults"].(map[string]any)
	if !ok || lane["model"] != "qwen" {
		t.Errorf("lane_defaults (JSON string) not decoded: %v", b["lane_defaults"])
	}
	arcs, ok := b["arcs"].([]any)
	if !ok || len(arcs) != 2 {
		t.Fatalf("arcs = %v", b["arcs"])
	}
	if first, _ := arcs[0].(map[string]any); first["automation_id"] != "a1" || first["state"] != "active" {
		t.Errorf("arcs[0] = %v", arcs[0])
	}
}

func TestProjectUpsertOmittedRecordFieldsNotSent(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name": "acme", "dir": "/tmp/acme",
	})
	if res.IsError {
		t.Fatalf("upsert failed: %s", resContentText(t, res))
	}
	for _, k := range []string{"owner", "repository", "default_branch", "major_trigger",
		"weekly_share", "branch_retention_days", "target", "lane_defaults", "arcs"} {
		if _, present := cap.body[k]; present {
			t.Errorf("%s sent although the caller omitted it: %v", k, cap.body[k])
		}
	}
}

func TestProjectUpsertEmptyStringClears(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name": "acme", "dir": "/tmp/acme", "owner": "",
		"arcs": []any{},
	})
	if res.IsError {
		t.Fatalf("upsert failed: %s", resContentText(t, res))
	}
	if v, ok := cap.body["owner"]; !ok || v != "" {
		t.Errorf("owner \"\" must be sent to clear it; body = %v", cap.body)
	}
	if arcs, ok := cap.body["arcs"].([]any); !ok || len(arcs) != 0 {
		t.Errorf("arcs [] must be sent to clear the list; body = %v", cap.body["arcs"])
	}
}

func TestProjectUpsertRejectsBadRecordFields(t *testing.T) {
	cases := map[string]map[string]any{
		"owner not a string":        {"owner": float64(7)},
		"weekly_share fractional":   {"weekly_share": 2.5},
		"weekly_share not a number": {"weekly_share": "lots"},
		"arcs not an array":         {"arcs": map[string]any{"automation_id": "a1"}},
		"arcs element not object":   {"arcs": []any{"a1"}},
		"arcs bad JSON string":      {"arcs": "[not json"},
		"target not an object":      {"target": []any{"x"}},
		"lane_defaults bad JSON":    {"lane_defaults": "{oops"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			cap, s, done := newRecordServer(t, `{}`)
			defer done()
			args := map[string]any{"name": "acme", "dir": "/tmp/acme"}
			for k, v := range extra {
				args[k] = v
			}
			res := callTool(t, s.handleProjectUpsert, args)
			if !res.IsError {
				t.Errorf("expected an error for %s", name)
			}
			if cap.calls != 0 {
				t.Errorf("rejected call reached the daemon (%d calls)", cap.calls)
			}
		})
	}
}

// ----- project_rollup ------------------------------------------------------

func TestProjectRollupHandler(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"project":"acme","window_days":7,"arcs":[]}`)
	defer done()

	res := callTool(t, s.handleProjectRollup, map[string]any{"name": "acme", "window": float64(14)})
	if res.IsError {
		t.Fatalf("rollup failed: %s", resContentText(t, res))
	}
	if cap.method != http.MethodGet || cap.path != "/api/projects/acme/rollup" {
		t.Fatalf("got %s %s", cap.method, cap.path)
	}
	if !strings.Contains(cap.rawQuery, "window=14") {
		t.Errorf("window not forwarded; query = %q", cap.rawQuery)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"window_days"`) {
		t.Errorf("response not returned: %s", out)
	}
}

func TestProjectRollupHandlerOmitsWindow(t *testing.T) {
	cap, s, done := newRecordServer(t, `{}`)
	defer done()
	res := callTool(t, s.handleProjectRollup, map[string]any{"name": "acme"})
	if res.IsError {
		t.Fatalf("rollup failed: %s", resContentText(t, res))
	}
	if cap.rawQuery != "" {
		t.Errorf("no window was given but query = %q", cap.rawQuery)
	}
}

func TestProjectRollupHandlerValidation(t *testing.T) {
	s := &Server{}
	if res := callTool(t, s.handleProjectRollup, map[string]any{}); !res.IsError {
		t.Error("missing name must be an error")
	}
	if res := callTool(t, s.handleProjectRollup, map[string]any{"name": "acme", "window": "week"}); !res.IsError {
		t.Error("non-numeric window must be an error")
	}
}

// ----- project_history -----------------------------------------------------

func TestProjectHistoryHandler(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"project":"acme","rows":[]}`)
	defer done()

	res := callTool(t, s.handleProjectHistory, map[string]any{
		"name": "my project", "limit": float64(25), "cursor": "abc123",
	})
	if res.IsError {
		t.Fatalf("history failed: %s", resContentText(t, res))
	}
	// The server sees the decoded path; the name is escaped on the way out.
	if cap.method != http.MethodGet || cap.path != "/api/projects/my project/history" {
		t.Errorf("got %s %s", cap.method, cap.path)
	}
	for _, want := range []string{"limit=25", "cursor=abc123"} {
		if !strings.Contains(cap.rawQuery, want) {
			t.Errorf("%s not forwarded; query = %q", want, cap.rawQuery)
		}
	}
}

func TestProjectHistoryHandlerValidation(t *testing.T) {
	s := &Server{}
	if res := callTool(t, s.handleProjectHistory, map[string]any{}); !res.IsError {
		t.Error("missing name must be an error")
	}
	if res := callTool(t, s.handleProjectHistory, map[string]any{"name": "acme", "limit": 2.5}); !res.IsError {
		t.Error("fractional limit must be an error")
	}
}

// ----- project_timeline ----------------------------------------------------

func TestProjectTimelineHandler(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"project":"acme","rows":[]}`)
	defer done()

	res := callTool(t, s.handleProjectTimeline, map[string]any{
		"name": "my project", "level": "tasks", "automaton": "p1", "parent": "none",
		"focus": "a1", "limit": float64(25), "cursor": "abc123",
	})
	if res.IsError {
		t.Fatalf("timeline failed: %s", resContentText(t, res))
	}
	if cap.method != http.MethodGet || cap.path != "/api/projects/my project/timeline" {
		t.Errorf("got %s %s", cap.method, cap.path)
	}
	for _, want := range []string{"level=tasks", "automaton=p1", "parent=none", "focus=a1", "limit=25", "cursor=abc123"} {
		if !strings.Contains(cap.rawQuery, want) {
			t.Errorf("%s not forwarded; query = %q", want, cap.rawQuery)
		}
	}
	if !strings.Contains(resContentText(t, res), `"project":"acme"`) {
		t.Errorf("body not returned: %s", resContentText(t, res))
	}
}

func TestProjectTimelineHandlerNoOptions(t *testing.T) {
	cap, s, done := newRecordServer(t, `{}`)
	defer done()
	if res := callTool(t, s.handleProjectTimeline, map[string]any{"name": "acme"}); res.IsError {
		t.Fatalf("timeline failed: %s", resContentText(t, res))
	}
	if cap.rawQuery != "" {
		t.Errorf("no options sent query %q", cap.rawQuery)
	}
}

func TestProjectTimelineHandlerValidation(t *testing.T) {
	s := &Server{}
	if res := callTool(t, s.handleProjectTimeline, map[string]any{}); !res.IsError {
		t.Error("missing name must be an error")
	}
	if res := callTool(t, s.handleProjectTimeline, map[string]any{"name": "acme", "limit": 2.5}); !res.IsError {
		t.Error("fractional limit must be an error")
	}
}

// ----- project_set_arcs ----------------------------------------------------

func TestProjectSetArcsHandler(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme","arcs":[{"automation_id":"a1"}]}`)
	defer done()

	res := callTool(t, s.handleProjectSetArcs, map[string]any{
		"name": "acme",
		"arcs": []any{
			map[string]any{"automation_id": "a2", "priority": float64(1), "state": "done"},
			map[string]any{"automation_id": "a1"},
		},
	})
	if res.IsError {
		t.Fatalf("set_arcs failed: %s", resContentText(t, res))
	}
	if cap.method != http.MethodPut || cap.path != "/api/projects/acme/arcs" {
		t.Fatalf("got %s %s", cap.method, cap.path)
	}
	arcs, ok := cap.body["arcs"].([]any)
	if !ok || len(arcs) != 2 {
		t.Fatalf("body arcs = %v", cap.body["arcs"])
	}
	if first, _ := arcs[0].(map[string]any); first["automation_id"] != "a2" {
		t.Errorf("array order not kept: %v", arcs[0])
	}
}

func TestProjectSetArcsHandlerEmptyClears(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme"}`)
	defer done()
	res := callTool(t, s.handleProjectSetArcs, map[string]any{"name": "acme", "arcs": []any{}})
	if res.IsError {
		t.Fatalf("set_arcs failed: %s", resContentText(t, res))
	}
	if arcs, ok := cap.body["arcs"].([]any); !ok || len(arcs) != 0 {
		t.Errorf("[] must be sent to clear; body = %v", cap.body)
	}
}

func TestProjectSetArcsHandlerValidation(t *testing.T) {
	cases := map[string]map[string]any{
		"name missing":       {"arcs": []any{}},
		"arcs missing":       {"name": "acme"},
		"arcs not an array":  {"name": "acme", "arcs": "a1"},
		"arcs element bogus": {"name": "acme", "arcs": []any{float64(1)}},
		"arcs bad JSON":      {"name": "acme", "arcs": "[{"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cap, s, done := newRecordServer(t, `{}`)
			defer done()
			if res := callTool(t, s.handleProjectSetArcs, args); !res.IsError {
				t.Error("expected an error")
			}
			if cap.calls != 0 {
				t.Error("rejected call reached the daemon")
			}
		})
	}
}

// The body of set_arcs must survive a round trip through JSON as an object
// with an arcs key, which is the shape the daemon's PUT handler accepts.
func TestProjectSetArcsBodyShape(t *testing.T) {
	cap, s, done := newRecordServer(t, `{}`)
	defer done()
	callTool(t, s.handleProjectSetArcs, map[string]any{
		"name": "acme",
		"arcs": `[{"automation_id":"a1","priority":3}]`,
	})
	var got struct {
		Arcs []map[string]any `json:"arcs"`
	}
	if err := json.Unmarshal(cap.bodyRaw, &got); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, cap.bodyRaw)
	}
	if len(got.Arcs) != 1 || got.Arcs[0]["automation_id"] != "a1" {
		t.Errorf("body = %s", cap.bodyRaw)
	}
}
